package formations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Every attempt of every step, judges included, has its own artifact
// directory, .archon/artifacts/<run>/<node>/<attempt>/, created at dispatch
// (archon-5tg1). The seat is told to write the files it produces there. When
// the attempt's output is accepted, each file or folder output is copied to
// outputs/<port>/<basename> in that directory, wherever the seat wrote it, and
// the copy is made read-only, files and directories alike. The copy is what
// the ledger records and routes, so neither a later attempt rewriting the same
// workspace file nor a later step handed the path can change what an earlier
// attempt produced. The copy follows links that leave the folder, so it holds
// their content as it was.

// attemptOutputsDir holds an attempt's snapshots of its file and folder outputs.
const attemptOutputsDir = "outputs"

// ficlone is the Linux FICLONE ioctl, a copy-on-write clone of a whole file.
const ficlone = 0x40049409

// copyChunkBytes is how much a copy moves between checks for a cancel or a
// shutdown.
const copyChunkBytes = 8 << 20

// attemptArtifactDir is the absolute artifact directory of one attempt.
func (s *Store) attemptArtifactDir(runID, nodeID string, attempt int) (string, error) {
	if !validRunID(runID) || !validPathComponent(nodeID) {
		return "", fmt.Errorf("%w: attempt directory for %q in %q", ErrRunLedgerInvalid, nodeID, runID)
	}
	workspace, err := s.workspaceAbsolutePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(workspace, ".archon", "artifacts", runID, nodeID, strconv.Itoa(attempt)), nil
}

// createAttemptArtifactDir makes an attempt's artifact directory.
func (s *Store) createAttemptArtifactDir(runID, nodeID string, attempt int) (string, error) {
	dir, err := s.attemptArtifactDir(runID, nodeID, attempt)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o770); err != nil {
		return "", runExecutionError("unavailable_artifact_dir", fmt.Sprintf("attempt artifact directory %s could not be created", dir), "engine", err)
	}
	return dir, nil
}

// latestNodeAttempt is the attempt of a node's latest node_started.
func latestNodeAttempt(events []RunEvent, nodeID string) int {
	attempt := 0
	for _, event := range events {
		if event.Type == RunEventNodeStarted && event.NodeID == nodeID {
			attempt = event.Attempt
		}
	}
	return attempt
}

// isJudgeFormation reports whether a step judges for a gate.
func isJudgeFormation(board *BoardDocument, nodeID string) bool {
	return len(judgedGates(board)[nodeID]) > 0
}

// judgedGates maps each judge step to the gates it judges for.
func judgedGates(board *BoardDocument) map[string][]string {
	gates := map[string][]string{}
	for _, gate := range board.Gates {
		for _, judge := range judgeChainForGate(board, gate.ID) {
			gates[judge.ID] = append(gates[judge.ID], gate.ID)
		}
	}
	return gates
}

// outputNotKeptError is a file or folder output that was valid but could not
// be copied: the copy failed, or a cancel or shutdown interrupted it.
type outputNotKeptError struct {
	message string
	err     error
}

func (e *outputNotKeptError) Error() string { return e.message }
func (e *outputNotKeptError) Unwrap() error { return e.err }

// pendingOutputError is an attempt whose seats finished but whose outputs
// were not kept (outputNotKeptError). Output is the attempt's node_output
// data as its seats returned it; the block it leaves carries it, so a resume
// keeps the outputs without running the step again.
type pendingOutputError struct {
	NodeID  string
	Attempt int
	Output  map[string]any
	Err     error
}

func (e *pendingOutputError) Error() string { return e.Err.Error() }
func (e *pendingOutputError) Unwrap() error { return e.Err }

// pendingOutput is the attempt result a block carries (pendingOutputError).
type pendingOutput struct {
	NodeID  string
	Attempt int
	Output  map[string]any
}

func pendingOutputFromEvent(event RunEvent) *pendingOutput {
	fields, ok := event.Data["pendingOutput"].(map[string]any)
	if !ok {
		return nil
	}
	output, _ := fields["output"].(map[string]any)
	pending := &pendingOutput{NodeID: stringFromAny(fields["nodeId"]), Attempt: intFromRunEventData(fields["attempt"]), Output: output}
	if pending.NodeID == "" || output == nil {
		return nil
	}
	return pending
}

// appendPendingOutputBlock blocks a run whose attempt finished without its
// outputs kept. Resume keeps them (keepPendingOutput); resume in redispatch
// mode runs the step again instead.
func (e *RunEngine) appendPendingOutputBlock(runID string, pending pendingOutputError) error {
	carried := map[string]any{"nodeId": pending.NodeID, "attempt": pending.Attempt, "output": pending.Output}
	if errors.Is(pending.Err, ErrCoordinatorShutdown) {
		block := runBlockedEvent("coordinator shutdown while keeping "+pending.NodeID+"'s outputs; resume keeps them without running the step again", pending.NodeID, "", "", nil)
		block.Data["code"] = RunBlockCoordinatorShutdown
		block.Data["pendingOutput"] = carried
		return e.store.AppendRunEvent(runID, block)
	}
	message := redactLedgerText(pending.Err.Error())
	reason := message + "; resume keeps the outputs without running the step again"
	if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventError, NodeID: pending.NodeID, Data: map[string]any{
		"code": RunBlockOutputNotKept, "message": message, "reason": reason, "boundary": "engine", "nodeId": pending.NodeID, "recoverable": true,
	}}); err != nil {
		return err
	}
	block := runBlockedEvent(reason, pending.NodeID, "", "", nil)
	block.Data["code"] = RunBlockOutputNotKept
	block.Data["pendingOutput"] = carried
	return e.store.AppendRunEvent(runID, block)
}

// RunBlockOutputNotKept is the code of the block an attempt leaves when its
// seats finished and its file or folder outputs could not be copied.
const RunBlockOutputNotKept = "output_not_kept"

// keepPendingOutput keeps the outputs of an attempt a block left pending and
// records its node_output, as the attempt would have when it finished. It
// reports whether the run stopped again.
func (e *RunEngine) keepPendingOutput(runID string, board *BoardDocument, pending pendingOutput) (bool, error) {
	formation, ok := findFormation(board.Formations, pending.NodeID)
	if !ok {
		return false, fmt.Errorf("%w: formation %q", ErrNotFound, pending.NodeID)
	}
	reason := stringFromAny(pending.Output["reason"])
	result, err := e.keepAttemptOutputs(runID, formation, pending.Attempt, reason == "judge", formationResultFromEventData(pending.Output))
	if err != nil {
		return true, e.appendExecutionFailureAndBlock(runID, formation.ID, err)
	}
	if result.Status == "" {
		result.Status = "done"
	}
	if err := e.ensureFormationOutputPayloads(runID, formation, result); err != nil {
		if errors.Is(err, errRunStopped) {
			return true, nil
		}
		return true, err
	}
	data := formationOutputEventData(result)
	if reason != "" {
		data["reason"] = reason
	}
	return false, e.store.AppendRunEvent(runID, RunEvent{Type: RunEventNodeOutput, NodeID: formation.ID, Attempt: pending.Attempt, Data: data})
}

// formationResultFromEventData reads an attempt's result back from its
// node_output data.
func formationResultFromEventData(data map[string]any) FormationExecutionResult {
	return FormationExecutionResult{
		Status:    stringFromAny(data["status"]),
		ReportRef: stringFromAny(data["reportRef"]),
		Text:      stringFromAny(data["text"]),
		Outputs:   outputPayloadsFromAny(data["outputs"]),
	}
}

// acceptAttemptOutputs checks an attempt's file and folder outputs and keeps
// each as a read-only snapshot in the attempt's directory. It returns the
// outputs with each kept path as its ref and the port's kind; a text output
// is returned as it is. An invalid ref fails like an unreadable ref does,
// and the step blocks; a valid output the copy could not keep is an
// outputNotKeptError.
func (s *Store) acceptAttemptOutputs(ctx context.Context, runID string, formation FormationNode, attempt int, outputs map[string]FormationOutputPayload) (accepted map[string]FormationOutputPayload, err error) {
	err = s.withRunCaptureLock(runID, func() error {
		var e error
		accepted, e = s.acceptAttemptOutputsStorageLocked(ctx, runID, formation, attempt, outputs)
		return e
	})
	return
}

func (s *Store) acceptAttemptOutputsStorageLocked(ctx context.Context, runID string, formation FormationNode, attempt int, outputs map[string]FormationOutputPayload) (map[string]FormationOutputPayload, error) {
	accepted := make(map[string]FormationOutputPayload, len(outputs))
	for portID, payload := range outputs {
		accepted[portID] = payload
	}
	var dir string
	var kept []string
	for _, port := range formation.Outputs {
		payload, ok := accepted[port.ID]
		if !ok {
			continue
		}
		kind := PortKind(port)
		if kind == PortKindText {
			continue
		}
		path, err := outputPortPath(formation.ID, port, payload.Ref)
		if err != nil {
			return nil, err
		}
		if dir == "" {
			if dir, err = s.createAttemptArtifactDir(runID, formation.ID, attempt); err != nil {
				return nil, err
			}
		}
		copyCtx, budgetErr := s.outputCopyContext(ctx, runID, dir, port.ID)
		if budgetErr != nil {
			return nil, &outputNotKeptError{message: budgetErr.Error(), err: budgetErr}
		}
		if path, err = keepOutput(copyCtx, path, dir, port.ID, s.runArtifactRoots(runID)); err != nil {
			return nil, &outputNotKeptError{message: fmt.Sprintf("%s output %s at %s could not be kept in the attempt directory: %v", formation.ID, port.ID, payload.Ref, err), err: err}
		}
		kept = append(kept, filepath.Join(dir, attemptOutputsDir, port.ID))
		payload.Ref = path
		payload.Kind = kind
		accepted[port.ID] = payload
	}
	if dir != "" {
		for _, path := range append(kept, filepath.Join(dir, attemptOutputsDir)) {
			if err := freezeDir(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, &outputNotKeptError{message: fmt.Sprintf("%s outputs could not be made read-only: %v", formation.ID, err), err: err}
			}
		}
	}
	return accepted, nil
}

// keepOutput keeps one file or folder output in the attempt's outputs
// directory, outputs/<port>/, read-only, and returns the kept path. Even an
// output already at that path is copied before replacement: its files may
// share inodes with mutable work outside the attempt.
func keepOutput(ctx context.Context, path, dir, portID string, artifactRoots []string) (string, error) {
	outputsDir := filepath.Join(dir, attemptOutputsDir)
	// An attempt accepted again, on resume, replaces its earlier copy.
	parent := filepath.Join(outputsDir, portID)
	for _, kept := range []string{outputsDir, parent} {
		if err := thawDir(kept); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
	}
	exclude := append(artifactRoots, dir, outputsDir)
	if err := preflightSnapshot(ctx, path, parent, exclude); err != nil {
		return "", err
	}
	return snapshotOutput(ctx, path, parent, exclude)
}

// outputPortPath checks a file or folder output's ref: an absolute path to an
// existing regular file, or directory, as the port's kind says.
func outputPortPath(nodeID string, port FormationPort, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	kind := PortKind(port)
	switch {
	case ref == "":
		return "", runExecutionError("invalid_output_ref", fmt.Sprintf("formation %q output %q is a %s port; its ref must be the absolute path of the %s", nodeID, port.ID, kind, kind), "engine", nil)
	case strings.ContainsRune(ref, 0) || strings.Contains(ref, "://") || !filepath.IsAbs(ref):
		return "", runExecutionError("invalid_output_ref", fmt.Sprintf("formation %q output %q ref %q must be an absolute path", nodeID, port.ID, ref), "engine", nil)
	}
	path := filepath.Clean(ref)
	info, err := os.Stat(path)
	if err != nil {
		return "", runExecutionError("unavailable_output_ref", fmt.Sprintf("formation %q output %q names %s, which does not exist or cannot be read", nodeID, port.ID, path), "engine", err)
	}
	if kind == PortKindFolder && !info.IsDir() {
		return "", runExecutionError("invalid_output_ref", fmt.Sprintf("formation %q output %q is a folder port; %s is not a directory", nodeID, port.ID, path), "engine", nil)
	}
	if kind == PortKindFile && !info.Mode().IsRegular() {
		return "", runExecutionError("invalid_output_ref", fmt.Sprintf("formation %q output %q is a file port; %s is not a regular file", nodeID, port.ID, path), "engine", nil)
	}
	return path, nil
}

func pathWithin(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// snapshotOutput copies a file or a directory tree into parent, keeping its
// base name, makes the copy read-only and returns its path. A copy already
// there is replaced. In a directory, a link to something inside the tree
// stays a link, pointing at the copy; a link that leads out of the tree is
// replaced by a copy of what it points at, so the snapshot holds that content
// as it was; a dangling link is kept as it is. Special files are left out.
// The excluded directories inside the tree, such as the run's artifact
// directory and the attempt's own, are not copied, so a folder output that
// holds the workspace cannot copy itself. A cancel or shutdown on ctx stops
// the copy.
func snapshotOutput(ctx context.Context, source, parent string, exclude []string) (string, error) {
	if err := os.MkdirAll(parent, 0o770); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", err
	}
	name := filepath.Base(source)
	if name == string(filepath.Separator) || name == "." {
		name = "root"
	}
	target := filepath.Join(parent, name)
	// Stage the independent copy before touching target, which may itself be
	// the source. Keep staging outside that target and exclude it from a
	// source tree that contains parent, so accepting outputs/ cannot recurse.
	stage, err := os.MkdirTemp(parent, ".snapshot-")
	if err != nil {
		return "", err
	}
	defer func() { _ = removeKept(stage) }()
	staged := filepath.Join(stage, name)
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		if err := cloneOrCopyFile(ctx, resolved, staged, info.Mode().Perm()); err != nil {
			return "", err
		}
	} else {
		copier := &treeCopier{ctx: ctx, skipped: map[string]bool{}}
		for _, dir := range append(exclude, target, stage) {
			if real, err := filepath.EvalSymlinks(dir); err == nil {
				dir = real
			}
			copier.skipped[dir] = true
		}
		if err := copier.copyTree(resolved, staged); err != nil {
			return "", err
		}
	}
	if err := copyStopped(ctx); err != nil {
		return "", err
	}
	if err := removeKept(target); err != nil {
		return "", err
	}
	if err := os.Rename(staged, target); err != nil {
		return "", err
	}
	return target, freezeTree(target)
}

// treeCopier copies a directory tree for snapshotOutput. frames are the trees
// being copied, the folder first and then each tree a link led out to, so a
// link back into any of them becomes a link to its copy rather than a cycle.
type treeCopier struct {
	ctx      context.Context
	skipped  map[string]bool
	frames   []copyFrame
	measure  bool
	measured int64
}

type copyFrame struct{ source, target string }

func (c *treeCopier) copyTree(source, target string) error {
	c.frames = append(c.frames, copyFrame{source: source, target: target})
	defer func() { c.frames = c.frames[:len(c.frames)-1] }()
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := copyStopped(c.ctx); err != nil {
			return err
		}
		if path != source && c.skipped[path] {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		switch mode := entry.Type(); {
		case mode.IsDir():
			if c.measure {
				return nil
			}
			return os.MkdirAll(dest, 0o770)
		case mode&fs.ModeSymlink != 0:
			return c.copyLink(path, dest)
		case mode.IsRegular():
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return c.copyFile(path, dest, info)
		}
		return nil
	})
}

// copyLink copies the link at path to dest: as a relative link to the copy of
// a target inside a tree being copied, as a copy of a target outside them, or
// as it is when it dangles or leads into a directory the copy leaves out.
func (c *treeCopier) copyLink(path, dest string) error {
	link, err := os.Readlink(path)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return c.symlink(link, dest)
	}
	for _, frame := range c.frames {
		if !pathWithin(real, frame.source) {
			continue
		}
		if c.leftOut(real, frame.source) {
			return c.symlink(link, dest)
		}
		rel, err := filepath.Rel(frame.source, real)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(filepath.Dir(dest), filepath.Join(frame.target, rel))
		if err != nil {
			return err
		}
		return c.symlink(relative, dest)
	}
	info, err := os.Stat(real)
	switch {
	case err != nil:
		return c.symlink(link, dest)
	case info.IsDir():
		return c.copyTree(real, dest)
	case info.Mode().IsRegular():
		return c.copyFile(real, dest, info)
	}
	return nil
}

// leftOut reports whether path lies in an excluded directory inside source,
// which the copy of source leaves out.
func (c *treeCopier) leftOut(path, source string) bool {
	for dir := range c.skipped {
		if dir != source && pathWithin(dir, source) && pathWithin(path, dir) {
			return true
		}
	}
	return false
}

// copyStopped is the error a cancel or shutdown on ctx stops a copy with.
func copyStopped(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	if shutdownRequested(ctx) {
		return ErrCoordinatorShutdown
	}
	return context.Cause(ctx)
}

// cloneOrCopyFile writes a copy of source to a new file at target: a
// copy-on-write clone where the file system supports FICLONE, otherwise an
// ordinary copy, checked for a cancel or shutdown between chunks. Never a hard
// link, so rewriting the source leaves it intact.
func cloneOrCopyFile(ctx context.Context, source, target string, perm fs.FileMode) error {
	if err := copyStopped(ctx); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	budget, _ := ctx.Value(snapshotBudgetKey{}).(*snapshotByteBudget)
	if budget != nil {
		info, e := in.Stat()
		if e != nil {
			return e
		}
		if info.Size() > budget.remaining {
			return ErrRunStorageBudget
		}
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm|0o600)
	if err != nil {
		return err
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, out.Fd(), ficlone, in.Fd()); errno != 0 {
		for {
			if err := copyStopped(ctx); err != nil {
				_ = out.Close()
				return err
			}
			chunk := int64(copyChunkBytes)
			if budget != nil {
				pos, e := out.Seek(0, io.SeekCurrent)
				if e != nil {
					_ = out.Close()
					return e
				}
				if left := budget.remaining - pos; left < chunk {
					chunk = left
				}
				if chunk == 0 {
					var one [1]byte
					n, e := in.Read(one[:])
					if n > 0 {
						_ = out.Close()
						return ErrRunStorageBudget
					}
					if e == io.EOF {
						break
					}
					if e != nil {
						_ = out.Close()
						return e
					}
				}
			}
			n, err := io.Copy(out, io.LimitReader(in, chunk))
			if err != nil {
				_ = out.Close()
				return err
			}
			if n < chunk {
				break
			}
		}
	}
	if budget != nil {
		info, e := out.Stat()
		if e != nil {
			_ = out.Close()
			return e
		}
		if info.Size() > budget.remaining {
			_ = out.Close()
			return ErrRunStorageBudget
		}
		budget.remaining -= info.Size()
	}
	return out.Close()
}

// freezeTree makes a kept file, or every file and directory in a kept tree,
// read-only. Links are left as they are.
func freezeTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.Chmod(path, info.Mode().Perm()&^0o222)
	})
}

// freezeDir makes one directory read-only.
func freezeDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return os.Chmod(path, info.Mode().Perm()&^0o222)
}

// thawDir gives the owner write access to one directory again.
func thawDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return os.Chmod(path, info.Mode().Perm()|0o700)
}

// removeKept removes a kept file or tree, giving its directories write access
// first.
func removeKept(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := filepath.WalkDir(path, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return thawDir(path)
		}
		return nil
	}); err != nil {
		return err
	}
	return os.RemoveAll(path)
}
