package formations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const RunStorageBudgetBytes int64 = 32 << 20
const RunStoragePoolBudgetBytes int64 = 256 << 20

var (
	ErrRunStorageActive     = errors.New("run evidence belongs to a nonfinal run or an agent seat whose absence is unconfirmed")
	ErrRunStorageReferenced = errors.New("run evidence is referenced by a nonfinal run")
	ErrRunStorageExpired    = errors.New("expired evidence cannot be archived; archive only keeps files still present")
	ErrRunEvidenceExpired   = errors.New("run evidence expired under its storage policy")
	ErrRunStorageBudget     = errors.New("run output snapshot exceeds its 32 MiB capture budget: narrow the output folder, return the project path as text, or explicitly archive this run then resume")
)

type RunStorageReport struct {
	ActiveSeats     []int    `json:"activeSeats"`
	Final           bool     `json:"final"`
	Trimmed         bool     `json:"trimmed"`
	RunID           string   `json:"runId"`
	Policy          string   `json:"policy"`
	Archived        bool     `json:"archived"`
	State           string   `json:"state"`
	Bytes           int64    `json:"bytes"`
	AllocatedBytes  int64    `json:"allocatedBytes"`
	RetainedBytes   int64    `json:"retainedBytes"`
	BudgetBytes     int64    `json:"budgetBytes"`
	PoolBudgetBytes int64    `json:"poolBudgetBytes"`
	ProtectedBy     []string `json:"protectedBy"`
	LastError       string   `json:"lastError,omitempty"`
}

type runStorageManifest struct {
	Version  int    `json:"version"`
	Archived bool   `json:"archived"`
	State    string `json:"state"`
	Trimmed  bool   `json:"trimmed,omitempty"`
	// Keep is persisted before removal. Restart retries the same selection.
	Keep      []string `json:"keep,omitempty"`
	LastError string   `json:"lastError,omitempty"`
}

func frozenRunStoragePolicy() map[string]any {
	return map[string]any{"version": 1, "budgetBytes": RunStorageBudgetBytes, "poolBudgetBytes": RunStoragePoolBudgetBytes}
}

func boundedRunStorage(events []RunEvent) bool {
	if len(events) == 0 {
		return false
	}
	policy, ok := events[0].Data["storagePolicy"].(map[string]any)
	return ok && fmt.Sprint(policy["version"]) == "1"
}

// All storage mutation, admission and resume use the same host/process lock.
// Canonicalizing the workspace also joins stores opened through symlink aliases.
func (s *Store) withRunStorageLock(fn func() error) error {
	root, err := s.workspaceAbsolutePath()
	if err != nil {
		return err
	}
	if real, e := filepath.EvalSymlinks(root); e == nil {
		root = real
	}
	return withFileLock(filepath.Join(root, ".archon", "storage", "lifecycle"), fn)
}
func (s *Store) storageManifestPath(id string) (string, error) {
	if !validRunID(id) {
		return "", ErrNotFound
	}
	root, err := s.workspaceAbsolutePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".archon", "storage", id+".json"), nil
}
func (s *Store) readStorageManifest(id string) (runStorageManifest, error) {
	m := runStorageManifest{Version: 1, State: "retained"}
	p, err := s.storageManifestPath(id)
	if err != nil {
		return m, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	if err == nil && (m.Version != 1 || (m.State != "retained" && m.State != "partial" && m.State != "expired")) {
		err = fmt.Errorf("invalid run storage manifest %s", id)
	}
	return m, err
}
func (s *Store) writeStorageManifest(id string, m runStorageManifest) error {
	p, err := s.storageManifestPath(id)
	if err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeAtomic(p, append(b, '\n'))
}
func (s *Store) runStorageRoot(id string) (string, error) {
	root, err := s.workspaceAbsolutePath()
	if err != nil {
		return "", err
	}
	root = filepath.Join(root, ".archon", "artifacts")
	if real, e := filepath.EvalSymlinks(root); e == nil {
		root = real
	}
	return filepath.Join(root, id), nil
}

type storageFile struct {
	name            string
	size, allocated int64
	directory       bool
}

func storageFiles(ctx context.Context, root string) ([]storageFile, error) {
	var files []storageFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			if p == root && errors.Is(e, fs.ErrNotExist) {
				return nil
			}
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if p != root {
				rel, e := filepath.Rel(root, p)
				if e != nil {
					return e
				}
				files = append(files, storageFile{name: filepath.ToSlash(rel), directory: true})
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		// Never follow descendant links for accounting/deletion. They are entries,
		// not ownership of the tree they point at.
		if info.Mode().IsRegular() {
			allocated := int64(0)
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				allocated = st.Blocks * 512
			}
			files = append(files, storageFile{name: filepath.ToSlash(rel), size: info.Size(), allocated: allocated})
		} else if info.Mode()&fs.ModeSymlink != 0 {
			files = append(files, storageFile{name: filepath.ToSlash(rel)})
		}
		return nil
	})
	return files, err
}
func storageBytes(files []storageFile) int64 {
	var n int64
	for _, f := range files {
		n += f.size
	}
	return n
}

func (s *Store) storageViews() ([]RunLedgerView, error) {
	views, unreadable, err := s.ListRunLedgerViews(RunListFilter{})
	if err != nil {
		return nil, err
	}
	if len(unreadable) > 0 {
		return nil, fmt.Errorf("storage maintenance deferred: %d unreadable run ledgers prevent reliable reference discovery", len(unreadable))
	}
	return views, nil
}

// Known references protect whole source runs. Include every absolute path
// appearing in a nonfinal ledger (also prose), plus symlink resolution of paths.
// This is deliberately conservative; external consumers must export/archive.
func (s *Store) storageProtection(ctx context.Context, views []RunLedgerView, busyIDs ...string) map[string][]string {
	busy := map[string]bool{}
	for _, id := range busyIDs {
		busy[id] = true
	}
	protected := map[string][]string{}
	roots := map[string]string{}
	for _, v := range views {
		r, e := s.runStorageRoot(v.Status.RunID)
		if e == nil {
			roots[v.Status.RunID] = r
			if real, e := filepath.EvalSymlinks(r); e == nil {
				roots[v.Status.RunID] = real
			}
		}
	}
	for _, v := range views {
		if v.Status.Final && !busy[v.Status.RunID] && !s.storageRunBusy(v.Status.RunID) && len(s.storageActiveSeats(ctx, v.Events)) == 0 {
			continue
		}
		found := map[string]bool{}
		var scan func(any)
		scan = func(x any) {
			switch x := x.(type) {
			case map[string]any:
				for _, y := range x {
					scan(y)
				}
			case []any:
				for _, y := range x {
					scan(y)
				}
			case []string:
				for _, y := range x {
					scan(y)
				}
			case string:
				paths := []string{x}
				paths = append(paths, strings.FieldsFunc(x, func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' || r == '"' || r == '\'' || r == '`' })...)
				for _, p := range paths {
					p = strings.TrimRight(p, ",;:.)]}")
					if !filepath.IsAbs(p) {
						continue
					}
					if real, e := filepath.EvalSymlinks(p); e == nil {
						p = real
					}
					for id, root := range roots {
						if id != v.Status.RunID && pathWithin(p, root) {
							found[id] = true
						}
					}
				}
			}
		}
		for _, e := range v.Events {
			scan(e.Data)
		}
		for id := range found {
			protected[id] = append(protected[id], v.Status.RunID)
		}
	}
	for id := range protected {
		sort.Strings(protected[id])
	}
	return protected
}

func (s *Store) storageReport(id string, events []RunEvent, m runStorageManifest, protected []string) (*RunStorageReport, error) {
	root, err := s.runStorageRoot(id)
	if err != nil {
		return nil, err
	}
	files, err := storageFiles(context.Background(), root)
	if err != nil {
		return nil, err
	}
	r := &RunStorageReport{RunID: id, Policy: "legacy", Archived: m.Archived, State: m.State, Bytes: storageBytes(files), RetainedBytes: 0, BudgetBytes: RunStorageBudgetBytes, PoolBudgetBytes: RunStoragePoolBudgetBytes, ProtectedBy: append([]string{}, protected...), LastError: m.LastError}
	r.ActiveSeats = s.storageActiveSeats(context.Background(), events)
	r.Final = len(events) > 0 && isFinalRunEvent(events[len(events)-1].Type)
	r.Trimmed = m.Trimmed
	kept := map[string]bool{}
	for _, name := range m.Keep {
		kept[name] = true
	}
	for _, f := range files {
		if m.Archived || (m.Trimmed && kept[f.name]) || (!m.Trimmed && strings.Contains("/"+f.name, "/outputs/")) {
			r.RetainedBytes += f.size
		}
	}
	if boundedRunStorage(events) {
		r.Policy = "bounded"
	}
	seen := map[[2]uint64]bool{}
	for _, f := range files {
		info, e := os.Lstat(filepath.Join(root, filepath.FromSlash(f.name)))
		if e != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			k := [2]uint64{uint64(st.Dev), st.Ino}
			if !seen[k] {
				r.AllocatedBytes += st.Blocks * 512
				seen[k] = true
			}
		}
	}
	return r, nil
}
func (s *Store) RunStorage(id string) (report *RunStorageReport, err error) {
	err = s.withRunStorageLock(func() error {
		events, e := s.ReadRunEvents(id)
		if e != nil {
			return e
		}
		m, e := s.readStorageManifest(id)
		if e != nil {
			return e
		}
		views, e := s.storageViews()
		if e != nil {
			return e
		}
		report, e = s.storageReport(id, events, m, s.storageProtection(context.Background(), views)[id])
		return e
	})
	return
}
func (s *Store) SetRunArchive(id string, archive bool) (report *RunStorageReport, err error) {
	err = s.withRunStorageLock(func() error {
		events, e := s.ReadRunEvents(id)
		if e != nil {
			return e
		}
		m, e := s.readStorageManifest(id)
		if e != nil {
			return e
		}
		if archive && !m.Archived && m.State != "retained" {
			return ErrRunStorageExpired
		}
		m.Archived = archive
		if e = s.writeStorageManifest(id, m); e != nil {
			return e
		}
		views, e := s.storageViews()
		if e != nil {
			return e
		}
		report, e = s.storageReport(id, events, m, s.storageProtection(context.Background(), views)[id])
		return e
	})
	return
}

// removal persists intent first and finishes idempotently after interruption.
// The caller establishes final/quiescent state and holds the storage lock.
func (s *Store) removeStorage(ctx context.Context, id string, m runStorageManifest, keep []string, expired bool) error {
	root, err := s.runStorageRoot(id)
	if err != nil {
		return err
	}
	m.State = "partial"
	m.LastError = ""
	m.Keep = keep
	m.Trimmed = true
	if err = s.writeStorageManifest(id, m); err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, name := range keep {
		wanted[name] = true
	}
	var dirs []string
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			if p == root && errors.Is(e, fs.ErrNotExist) {
				return nil
			}
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.IsDir() {
			dirs = append(dirs, p)
			return thawDir(p)
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		if wanted[filepath.ToSlash(rel)] {
			return nil
		}
		return os.Remove(p)
	})
	if err == nil {
		for i := len(dirs) - 1; i >= 0; i-- {
			if e := ctx.Err(); e != nil {
				err = e
				break
			}
			rel, _ := filepath.Rel(root, dirs[i])
			rel = filepath.ToSlash(rel)
			preserve := wanted[rel]
			for name := range wanted {
				if strings.HasPrefix(name, rel+"/") {
					preserve = true
					break
				}
			}
			if preserve {
				if strings.Contains("/"+rel+"/", "/outputs/") {
					if e := freezeDir(dirs[i]); e != nil {
						err = e
						break
					}
				}
				continue
			}
			e := os.Remove(dirs[i])
			if errors.Is(e, syscall.ENOTEMPTY) && strings.Contains("/"+rel+"/", "/outputs/") {
				if freezeErr := freezeDir(dirs[i]); freezeErr != nil {
					err = freezeErr
					break
				}
			}
			if e != nil && !errors.Is(e, syscall.ENOTEMPTY) && !errors.Is(e, fs.ErrNotExist) {
				err = e
				break
			}
		}
	}
	if err != nil {
		m.LastError = err.Error()
		return errors.Join(err, s.writeStorageManifest(id, m))
	}
	m.State = "retained"
	if expired {
		m.State = "expired"
	}
	m.LastError = ""
	return s.writeStorageManifest(id, m)
}
func (s *Store) DisposeRunArtifacts(id string) (report *RunStorageReport, err error) {
	err = s.withRunStorageLock(func() error {
		events, e := s.ReadRunEvents(id)
		if e != nil {
			return e
		}
		if len(events) == 0 || !isFinalRunEvent(events[len(events)-1].Type) || s.storageRunBusy(id) || len(s.storageActiveSeats(context.Background(), events)) > 0 {
			return ErrRunStorageActive
		}
		views, e := s.storageViews()
		if e != nil {
			return e
		}
		protection := s.storageProtection(context.Background(), views)[id]
		if len(protection) > 0 {
			return ErrRunStorageReferenced
		}
		m, e := s.readStorageManifest(id)
		if e != nil {
			return e
		}
		m.Archived = false
		if e = s.removeStorage(context.Background(), id, m, nil, true); e != nil {
			return e
		}
		m, e = s.readStorageManifest(id)
		if e != nil {
			return e
		}
		report, e = s.storageReport(id, events, m, protection)
		return e
	})
	return
}

// Select complete declared outputs before small textual diagnostics. Outputs
// are already capture-budgeted; no output folder is silently partly retained.
func (s *Store) selectStorageEvidence(root string, events []RunEvent, files []storageFile) []string {
	selected := map[string]bool{}
	var used int64
	add := func(names []storageFile) {
		var total int64
		for _, f := range names {
			if !selected[f.name] {
				total += f.size
			}
		}
		if total > RunStorageBudgetBytes-used {
			return
		}
		for _, f := range names {
			if !selected[f.name] {
				selected[f.name] = true
				used += f.size
			}
		}
	}
	declared := map[string]bool{}
	for eventIndex := len(events) - 1; eventIndex >= 0; eventIndex-- {
		e := events[eventIndex]
		if e.Type != RunEventNodeOutput {
			continue
		}
		payloads := outputPayloadsFromAny(e.Data["outputs"])
		ports := make([]string, 0, len(payloads))
		for id := range payloads {
			ports = append(ports, id)
		}
		sort.Strings(ports)
		for _, port := range ports {
			p := payloads[port]
			ref := p.Ref
			if real, e := filepath.EvalSymlinks(ref); e == nil {
				ref = real
			}
			if !filepath.IsAbs(ref) || !pathWithin(ref, root) {
				continue
			}
			rel, _ := filepath.Rel(root, ref)
			rel = filepath.ToSlash(rel)
			var matches []storageFile
			for _, f := range files {
				if f.name == rel || strings.HasPrefix(f.name, rel+"/") {
					matches = append(matches, f)
					declared[f.name] = true
				}
			}
			add(matches)
		}
	}
	reportFiles := append([]storageFile(nil), files...)
	rank := map[string]int{}
	for i, e := range events {
		if e.Type == RunEventNodeStarted {
			rank[filepath.ToSlash(filepath.Join(e.NodeID, fmt.Sprint(e.Attempt)))] = i
		}
	}
	reportRank := func(name string) int {
		parts := strings.Split(name, "/")
		if len(parts) < 2 {
			return -1
		}
		return rank[parts[0]+"/"+parts[1]]
	}
	sort.SliceStable(reportFiles, func(i, j int) bool { return reportRank(reportFiles[i].name) > reportRank(reportFiles[j].name) })
	for _, f := range reportFiles {
		ext := strings.ToLower(filepath.Ext(f.name))
		if !f.directory && !declared[f.name] && f.size <= 256<<10 && (ext == ".md" || ext == ".txt" || ext == ".json") {
			add([]storageFile{f})
		}
	}
	out := make([]string, 0, len(selected))
	for n := range selected {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (s *Store) SweepRunStorage(ctx context.Context, excludedRunIDs ...string) error {
	return s.withRunStorageLock(func() error {
		views, err := s.storageViews()
		if err != nil {
			return err
		}
		protected := s.storageProtection(ctx, views, excludedRunIDs...)
		exclude := map[string]bool{}
		for _, id := range excludedRunIDs {
			exclude[id] = true
		}
		sort.Slice(views, func(i, j int) bool {
			a, b := views[i].Events[len(views[i].Events)-1].Timestamp, views[j].Events[len(views[j].Events)-1].Timestamp
			if a == b {
				return views[i].Status.RunID < views[j].Status.RunID
			}
			return a < b
		})
		type retained struct {
			id string
			m  runStorageManifest
			n  int64
		}
		var pool []retained
		var total int64
		var errs []error
		for _, v := range views {
			if err := ctx.Err(); err != nil {
				return err
			}
			id := v.Status.RunID
			if !v.Status.Final || !boundedRunStorage(v.Events) || exclude[id] || s.storageRunBusy(id) || len(s.storageActiveSeats(ctx, v.Events)) > 0 {
				continue
			}
			m, e := s.readStorageManifest(id)
			if e != nil {
				errs = append(errs, e)
				continue
			}
			if m.Archived || len(protected[id]) > 0 {
				continue
			}
			root, e := s.runStorageRoot(id)
			if e != nil {
				errs = append(errs, e)
				continue
			}
			files, e := storageFiles(ctx, root)
			if e != nil {
				errs = append(errs, e)
				continue
			}
			if m.State == "partial" {
				e = s.removeStorage(ctx, id, m, m.Keep, len(m.Keep) == 0)
			} else if !m.Trimmed && m.State != "expired" {
				e = s.removeStorage(ctx, id, m, s.selectStorageEvidence(root, v.Events, files), false)
			}
			if e != nil {
				errs = append(errs, e)
				continue
			}
			m, e = s.readStorageManifest(id)
			if e != nil {
				errs = append(errs, e)
				continue
			}
			files, e = storageFiles(ctx, root)
			if e != nil {
				errs = append(errs, e)
				continue
			}
			n := storageBytes(files)
			total += n
			pool = append(pool, retained{id, m, n})
		}
		for _, r := range pool {
			if total <= RunStoragePoolBudgetBytes {
				break
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if e := s.removeStorage(ctx, r.id, r.m, nil, true); e != nil {
				errs = append(errs, e)
				continue
			}
			total -= r.n
		}
		return errors.Join(errs...)
	})
}

// Evidence readers use persisted selection, not leftover files after a partial
// delete. A missing path never pretends to be retained simply because it exists.
func (s *Store) checkStorageEvidence(id, name string) error {
	m, err := s.readStorageManifest(id)
	if err != nil {
		return err
	}
	if m.State == "expired" {
		return ErrRunEvidenceExpired
	}
	if m.Trimmed {
		for _, kept := range m.Keep {
			if kept == filepath.ToSlash(name) {
				return nil
			}
		}
		return ErrRunEvidenceExpired
	}
	return nil
}

// Snapshot budgets count logical copied bytes, including materialized external
// links. Preflight and streaming enforce the same traversal and byte ceiling.
type snapshotBudgetKey struct{}
type snapshotByteBudget struct{ remaining int64 }

func (s *Store) outputCopyContext(ctx context.Context, id, dir, port string) (context.Context, error) {
	if err := copyStopped(ctx); err != nil {
		return ctx, err
	}
	events, err := s.ReadRunEvents(id)
	if err != nil {
		return ctx, err
	}
	if !boundedRunStorage(events) {
		return ctx, nil
	}
	m, err := s.readStorageManifest(id)
	if err != nil {
		return ctx, err
	}
	if m.Archived {
		return ctx, nil
	}
	root, err := s.runStorageRoot(id)
	if err != nil {
		return ctx, err
	}
	files, err := storageFiles(ctx, root)
	if err != nil {
		return ctx, err
	}
	replace := filepath.Join(dir, attemptOutputsDir, port)
	if real, e := filepath.EvalSymlinks(filepath.Dir(replace)); e == nil {
		replace = filepath.Join(real, port)
	}
	var used int64
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f.name))
		if strings.Contains("/"+f.name, "/outputs/") && !pathWithin(p, replace) {
			used += f.size
		}
	}
	if used > RunStorageBudgetBytes {
		return ctx, ErrRunStorageBudget
	}
	return context.WithValue(ctx, snapshotBudgetKey{}, &snapshotByteBudget{RunStorageBudgetBytes - used}), nil
}
func preflightSnapshot(ctx context.Context, source, parent string, exclude []string) error {
	budget, _ := ctx.Value(snapshotBudgetKey{}).(*snapshotByteBudget)
	if budget == nil {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if info.Size() > budget.remaining {
			return ErrRunStorageBudget
		}
		return nil
	}
	c := &treeCopier{ctx: ctx, measure: true, skipped: map[string]bool{}}
	target := filepath.Join(parent, filepath.Base(source))
	for _, p := range append(exclude, target) {
		if real, e := filepath.EvalSymlinks(p); e == nil {
			p = real
		}
		c.skipped[p] = true
	}
	return c.copyTree(resolved, filepath.Join(parent, ".preflight"))
}
func (c *treeCopier) copyFile(source, target string, info fs.FileInfo) error {
	if !c.measure {
		return cloneOrCopyFile(c.ctx, source, target, info.Mode().Perm())
	}
	c.measured += info.Size()
	budget, _ := c.ctx.Value(snapshotBudgetKey{}).(*snapshotByteBudget)
	if budget != nil && c.measured > budget.remaining {
		return ErrRunStorageBudget
	}
	return nil
}
func (c *treeCopier) symlink(link, target string) error {
	if c.measure {
		return nil
	}
	return os.Symlink(link, target)
}

// Track every creation, not just the newest seat of a reused slot: an earlier
// cleanup failure still owns live work. A cleanup belongs to the latest creation
// matching its node/slot/session, as in the runtime ledger contract.
func (s *Store) storageActiveSeats(ctx context.Context, events []RunEvent) []int {
	seats := map[int]KeptSeat{}
	ended := map[int]bool{}
	latest := map[string]int{}
	for _, e := range events {
		key := e.NodeID + "\x00" + e.SlotID
		if e.Type == RunEventSeatCreated {
			seats[e.Seq] = KeptSeat{RunID: e.RunID, NodeID: e.NodeID, SlotID: e.SlotID, CreatedSeq: e.Seq, SessionName: stringFromAny(e.Data["sessionName"]), SessionID: stringFromAny(e.Data["sessionId"]), PaneID: stringFromAny(e.Data["paneId"]), SocketIdentity: stringFromAny(e.Data["socketIdentity"]), Harness: stringFromAny(e.Data["harness"])}
			latest[key] = e.Seq
		}
		if e.Type == RunEventSeatCleanup {
			seq := latest[key]
			seat, ok := seats[seq]
			if !ok {
				continue
			}
			if name := stringFromAny(e.Data["sessionName"]); name != "" && name != seat.SessionName {
				continue
			}
			outcome := stringFromAny(e.Data["outcome"])
			if outcome == SeatOutcomeEnded || outcome == SeatOutcomeGone {
				ended[seq] = true
			}
		}
	}
	active := []int{}
	for seq, seat := range seats {
		if ended[seq] {
			continue
		}
		if s.RunStorageSeatAbsent != nil && ctx.Err() == nil && s.RunStorageSeatAbsent(ctx, seat) {
			continue
		}
		active = append(active, seq)
	}
	sort.Ints(active)
	return active
}

// Capture serialization is per run: an explicitly archived large copy must not
// hold admission of unrelated missions. Nonfinal runs are never swept; runtime
// maintenance also excludes final runs whose worker has not settled yet.
func (s *Store) withRunCaptureLock(id string, fn func() error) error {
	p, err := s.storageManifestPath(id)
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err == nil {
		p = filepath.Join(root, filepath.Base(p))
	}
	return withFileLock(p+".capture", fn)
}

func (s *Store) storageRunBusy(id string) bool {
	return s.RunStorageRunBusy != nil && s.RunStorageRunBusy(id)
}
