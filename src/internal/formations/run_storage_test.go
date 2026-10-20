package formations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func storageRun(t *testing.T) (*Store, string) {
	t.Helper()
	s, p := s4RunFixture(t)
	writeFixture(t, s.BoardPath("session-search"), s4RunBoardFixture())
	r, e := s.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: p})
	if e != nil {
		t.Fatal(e)
	}
	return s, r.RunID
}
func storageFinish(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.AppendRunEvent(id, RunEvent{Type: RunEventCanceled, Data: map[string]any{"reason": "test", "final": true}}); err != nil {
		t.Fatal(err)
	}
}
func sparseFile(t *testing.T, path string, size int64) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	f, e := os.Create(path)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(size); e != nil {
		t.Fatal(e)
	}
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
}

func TestRunStorageBudgetRejectsExpandedFolderBeforeCopyAndArchiveAllowsIt(t *testing.T) {
	s, id := storageRun(t)
	dir, e := s.createAttemptArtifactDir(id, "fmn_research", 1)
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(t.TempDir(), "deliverable")
	if e = os.Mkdir(source, 0700); e != nil {
		t.Fatal(e)
	}
	large := filepath.Join(t.TempDir(), "large.bin")
	sparseFile(t, large, RunStorageBudgetBytes+1)
	if e = os.Symlink(large, filepath.Join(source, "external.bin")); e != nil {
		t.Fatal(e)
	}
	ctx, e := s.outputCopyContext(context.Background(), id, dir, "port_out")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = keepOutput(ctx, source, dir, "port_out", s.runArtifactRoots(id)); !errors.Is(e, ErrRunStorageBudget) {
		t.Fatalf("expanded output: %v", e)
	}
	if _, e = os.Stat(filepath.Join(dir, "outputs", "port_out")); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("oversize preflight created output: %v", e)
	}
	if _, e = s.SetRunArchive(id, true); e != nil {
		t.Fatal(e)
	}
	ctx, e = s.outputCopyContext(context.Background(), id, dir, "port_out")
	if e != nil {
		t.Fatal(e)
	}
	if e = preflightSnapshot(ctx, source, filepath.Join(dir, "outputs", "port_out"), nil); e != nil {
		t.Fatalf("explicit archive: %v", e)
	}
	// Exercise stream guard separately at a tiny budget rather than writing MiBs.
	ctx = context.WithValue(context.Background(), snapshotBudgetKey{}, &snapshotByteBudget{remaining: 3})
	small := filepath.Join(t.TempDir(), "small")
	writeFixture(t, small, "four")
	if e = cloneOrCopyFile(ctx, small, filepath.Join(t.TempDir(), "copy"), 0600); !errors.Is(e, ErrRunStorageBudget) {
		t.Fatalf("stream guard: %v", e)
	}
}

func TestRunStorageTrimDisposeAndLegacyHistory(t *testing.T) {
	s, id := storageRun(t)
	root, _ := s.runStorageRoot(id)
	scratch := filepath.Join(root, "fmn_research", "1", "render.bin")
	writeFixture(t, scratch, "temporary render")
	report := filepath.Join(root, "fmn_research", "1", "report.md")
	writeFixture(t, report, "useful report")
	out := filepath.Join(root, "fmn_research", "1", "outputs", "port_out", "result.bin")
	writeFixture(t, out, "kept output")
	if e := s.AppendRunEvent(id, RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_research", Attempt: 1, Data: map[string]any{"outputs": map[string]any{"port_out": map[string]any{"ref": out, "kind": "file"}}}}); e != nil {
		t.Fatal(e)
	}
	storageFinish(t, s, id)
	before, e := s.RunStorage(id)
	if e != nil {
		t.Fatal(e)
	}
	if before.Policy != "bounded" || !before.Final {
		t.Fatalf("policy %+v", before)
	}
	if e = s.SweepRunStorage(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(scratch); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("scratch remains: %v", e)
	}
	if readBody(t, out) != "kept output" || readBody(t, report) != "useful report" {
		t.Fatal("selection lost")
	}
	if _, e = s.ReadRunArtifact(id, "fmn_research/1/render.bin"); !errors.Is(e, ErrRunEvidenceExpired) {
		t.Fatalf("expired scratch: %v", e)
	}
	got, e := s.DisposeRunArtifacts(id)
	if e != nil {
		t.Fatal(e)
	}
	if got.State != "expired" || got.Bytes != 0 {
		t.Fatalf("disposed %+v", got)
	}
	if _, e = s.ReadRunArtifact(id, "fmn_research/1/outputs/port_out/result.bin"); !errors.Is(e, ErrRunEvidenceExpired) {
		t.Fatalf("expired output: %v", e)
	}
	if _, e = s.SetRunArchive(id, true); !errors.Is(e, ErrRunStorageExpired) {
		t.Fatalf("resurrect archive: %v", e)
	}
	if _, e = s.DisposeRunArtifacts(id); e != nil {
		t.Fatal(e)
	}
	if events, e := s.ReadRunEvents(id); e != nil || len(events) != 3 {
		t.Fatalf("history changed %d %v", len(events), e)
	}
}

func TestRunStorageReferencesBlockDisposalAndSweep(t *testing.T) {
	s, id := storageRun(t)
	root, _ := s.runStorageRoot(id)
	writeFixture(t, filepath.Join(root, "scratch.bin"), "protected")
	storageFinish(t, s, id)
	// A real admitted consumer in the same store can carry source paths in a
	// frozen context reference. This reference survives blocked/resume states.
	// Start with fixture personas so bindings remain valid.
	_, personas := s4RunFixture(t)
	consumer, e := s.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas, ContextPaths: []string{root}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.RunStorage(id)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.ProtectedBy) != 1 || r.ProtectedBy[0] != consumer.RunID {
		t.Fatalf("protection %+v", r)
	}
	if _, e = s.DisposeRunArtifacts(id); !errors.Is(e, ErrRunStorageReferenced) {
		t.Fatalf("referenced: %v", e)
	}
	if e = s.SweepRunStorage(context.Background()); e != nil {
		t.Fatal(e)
	}
	if readBody(t, filepath.Join(root, "scratch.bin")) != "protected" {
		t.Fatal("source removed")
	}
	storageFinish(t, s, consumer.RunID)
	if _, e = s.DisposeRunArtifacts(id); e != nil {
		t.Fatal(e)
	}
}

func TestRunStoragePartialRemovalRetriesAfterRestartAndPreservesExternalLink(t *testing.T) {
	s, id := storageRun(t)
	root, _ := s.runStorageRoot(id)
	outside := filepath.Join(t.TempDir(), "project.md")
	writeFixture(t, outside, "project stays")
	writeFixture(t, filepath.Join(root, "scratch.bin"), "scratch")
	if e := os.Symlink(outside, filepath.Join(root, "linked.md")); e != nil {
		t.Fatal(e)
	}
	storageFinish(t, s, id)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := runStorageManifest{Version: 1, State: "retained"}
	if e := s.removeStorage(ctx, id, m, nil, true); !errors.Is(e, context.Canceled) {
		t.Fatalf("cancel %v", e)
	}
	m, e := s.readStorageManifest(id)
	if e != nil || m.State != "partial" {
		t.Fatalf("partial %+v %v", m, e)
	}
	restarted := &Store{Workspace: s.Workspace}
	if e = restarted.SweepRunStorage(context.Background()); e != nil {
		t.Fatal(e)
	}
	r, e := restarted.RunStorage(id)
	if e != nil || r.State != "expired" || r.Bytes != 0 {
		t.Fatalf("restart %+v %v", r, e)
	}
	if readBody(t, outside) != "project stays" {
		t.Fatal("external link target changed")
	}
}

func TestRunStorageEmptyOutputsAndOversizedGroupAreIndivisible(t *testing.T) {
	s, id := storageRun(t)
	root, _ := s.runStorageRoot(id)
	empty := filepath.Join(root, "work", "1", "outputs", "empty", "folder")
	if e := os.MkdirAll(empty, 0700); e != nil {
		t.Fatal(e)
	}
	large := filepath.Join(root, "work", "1", "outputs", "large", "folder")
	sparseFile(t, filepath.Join(large, "big.bin"), RunStorageBudgetBytes+1)
	writeFixture(t, filepath.Join(large, "report.md"), "must not retain a fragment")
	files, e := storageFiles(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	events := []RunEvent{{Type: RunEventNodeOutput, Data: map[string]any{"outputs": map[string]any{"a": map[string]any{"ref": empty}, "z": map[string]any{"ref": large}}}}}
	keep := s.selectStorageEvidence(root, events, files)
	hasEmpty := false
	for _, name := range keep {
		if name == "work/1/outputs/empty/folder" {
			hasEmpty = true
		}
		if name == "work/1/outputs/large/folder/report.md" {
			t.Fatal("partial oversized output")
		}
	}
	if !hasEmpty {
		t.Fatalf("indivisible selection %v", keep)
	}
	if e = s.removeStorage(context.Background(), id, runStorageManifest{Version: 1, State: "retained"}, keep, false); e != nil {
		t.Fatal(e)
	}
	if info, e := os.Stat(empty); e != nil || !info.IsDir() || info.Mode().Perm()&0222 != 0 {
		t.Fatalf("empty output: %v %v", info, e)
	}
	if _, e = os.Stat(large); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("oversize group survives %v", e)
	}
}

func TestRunStorageUnconfirmedOlderSeatProtectsFinalEvidence(t *testing.T) {
	s, id := storageRun(t)
	root, _ := s.runStorageRoot(id)
	writeFixture(t, filepath.Join(root, "scratch.bin"), "live work")
	events := []RunEvent{{Type: RunEventSeatCreated, NodeID: "fmn_research", SlotID: "slot", Data: map[string]any{"sessionName": "old", "sessionId": "$1", "paneId": "%1"}}, {Type: RunEventSeatCleanup, NodeID: "fmn_research", SlotID: "slot", Data: map[string]any{"sessionName": "old", "outcome": SeatOutcomeLeftCleanupFailed}}, {Type: RunEventSeatCreated, NodeID: "fmn_research", SlotID: "slot", Data: map[string]any{"sessionName": "new", "sessionId": "$2", "paneId": "%2"}}, {Type: RunEventSeatCleanup, NodeID: "fmn_research", SlotID: "slot", Data: map[string]any{"sessionName": "new", "outcome": SeatOutcomeEnded}}}
	for _, e := range events {
		if err := s.AppendRunEvent(id, e); err != nil {
			t.Fatal(err)
		}
	}
	storageFinish(t, s, id)
	if _, e := s.DisposeRunArtifacts(id); !errors.Is(e, ErrRunStorageActive) {
		t.Fatalf("unconfirmed seat %v", e)
	}
	r, e := s.RunStorage(id)
	if e != nil || len(r.ActiveSeats) != 1 {
		t.Fatalf("seats %+v %v", r, e)
	}
	s.RunStorageSeatAbsent = func(context.Context, KeptSeat) bool { return true }
	if _, e = s.DisposeRunArtifacts(id); e != nil {
		t.Fatal(e)
	}
}

func TestRunStoragePoolEvictsOldestAndSkipsLegacyArchiveBusyAndProject(t *testing.T) {
	s, first := storageRun(t)
	_, p := s4RunFixture(t)
	ids := []string{first}
	for i := 0; i < 9; i++ {
		r, e := s.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: p})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, r.RunID)
	}
	for _, id := range ids {
		root, _ := s.runStorageRoot(id)
		out := filepath.Join(root, "work", "1", "outputs", "out", "result.bin")
		sparseFile(t, out, RunStorageBudgetBytes)
		if e := s.AppendRunEvent(id, RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_research", Attempt: 1, Data: map[string]any{"outputs": map[string]any{"out": map[string]any{"ref": out, "kind": "file"}}}}); e != nil {
			t.Fatal(e)
		}
		storageFinish(t, s, id)
	}
	// Archive excludes one full output, while an excluded final worker protects
	// another output. Nine remaining32MiB runs put ordinary pool over256MiB.
	if _, e := s.SetRunArchive(ids[9], true); e != nil {
		t.Fatal(e)
	}
	if e := s.SweepRunStorage(context.Background()); e != nil {
		t.Fatal(e)
	}
	old, e := s.RunStorage(first)
	if e != nil || old.State != "expired" {
		t.Fatalf("oldest %+v %v", old, e)
	}
	archived, e := s.RunStorage(ids[9])
	if e != nil || archived.State != "retained" || !archived.Archived {
		t.Fatalf("archive %+v %v", archived, e)
	}
	// Project cwd is never in the disposal unit.
	project := filepath.Join(t.TempDir(), "project")
	writeFixture(t, filepath.Join(project, "keep.txt"), "keep")
	r, e := s.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: p, Cwd: project})
	if e != nil {
		t.Fatal(e)
	}
	storageFinish(t, s, r.RunID)
	if _, e = s.DisposeRunArtifacts(r.RunID); e != nil {
		t.Fatal(e)
	}
	if readBody(t, filepath.Join(project, "keep.txt")) != "keep" {
		t.Fatal("project removed")
	}
}

func TestRunStorageBusyFinalConsumerProtectsItsUpstream(t *testing.T) {
	s, source := storageRun(t)
	root, _ := s.runStorageRoot(source)
	writeFixture(t, filepath.Join(root, "scratch.bin"), "upstream")
	storageFinish(t, s, source)
	_, p := s4RunFixture(t)
	r, e := s.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: p, ContextPaths: []string{root}})
	if e != nil {
		t.Fatal(e)
	}
	storageFinish(t, s, r.RunID)
	if e = s.SweepRunStorage(context.Background(), r.RunID); e != nil {
		t.Fatal(e)
	}
	if readBody(t, filepath.Join(root, "scratch.bin")) != "upstream" {
		t.Fatal("busy final consumer lost upstream")
	}
	s.RunStorageRunBusy = func(id string) bool { return id == r.RunID }
	if _, e = s.DisposeRunArtifacts(source); !errors.Is(e, ErrRunStorageReferenced) {
		t.Fatalf("busy final ref: %v", e)
	}
}

func TestRunStorageLegacyAndSymlinkedArtifactRoot(t *testing.T) {
	s, id := storageRun(t)
	root, _ := s.runStorageRoot(id)
	writeFixture(t, filepath.Join(root, "scratch.bin"), "legacy work")
	storageFinish(t, s, id)
	paths, e := filepath.Glob(filepath.Join(s.Workspace, ".archon", "runs", "*", id+".ndjson"))
	if e != nil || len(paths) != 1 {
		t.Fatalf("ledger %v %v", paths, e)
	}
	raw, e := os.ReadFile(paths[0])
	if e != nil {
		t.Fatal(e)
	}
	lines := bytes.Split(raw, []byte("\n"))
	var event RunEvent
	if e = json.Unmarshal(lines[0], &event); e != nil {
		t.Fatal(e)
	}
	delete(event.Data, "storagePolicy")
	lines[0], e = json.Marshal(event)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(paths[0], bytes.Join(lines, []byte("\n")), 0600); e != nil {
		t.Fatal(e)
	}
	artifacts := filepath.Dir(root)
	moved := filepath.Join(t.TempDir(), "artifact-disk")
	if e = os.Rename(artifacts, moved); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(moved, artifacts); e != nil {
		t.Fatal(e)
	}
	s = NewStore(s.Workspace)
	if e = s.SweepRunStorage(context.Background()); e != nil {
		t.Fatal(e)
	}
	r, e := s.RunStorage(id)
	if e != nil || r.Policy != "legacy" || r.Bytes == 0 {
		t.Fatalf("legacy %+v %v", r, e)
	}
	if _, e = s.DisposeRunArtifacts(id); e != nil {
		t.Fatal(e)
	}
	if info, e := os.Lstat(artifacts); e != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("artifact disk link lost %v %v", info, e)
	}
	if _, e = os.Stat(filepath.Join(moved, id)); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("legacy bytes remain %v", e)
	}
}

func TestRunStorageAdmissionAndDisposalSerializeAcrossWorkspaceAliases(t *testing.T) {
	for i := 0; i < 6; i++ {
		s, source := storageRun(t)
		root, _ := s.runStorageRoot(source)
		writeFixture(t, filepath.Join(root, "evidence.bin"), "source")
		storageFinish(t, s, source)
		_, p := s4RunFixture(t)
		alias := filepath.Join(t.TempDir(), "workspace-link")
		if e := os.Symlink(s.Workspace, alias); e != nil {
			t.Fatal(e)
		}
		other := NewStore(alias)
		start := make(chan struct{})
		admitted := make(chan error, 1)
		removed := make(chan error, 1)
		go func() {
			<-start
			_, e := other.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: p, ContextPaths: []string{root}})
			admitted <- e
		}()
		go func() { <-start; _, e := s.DisposeRunArtifacts(source); removed <- e }()
		close(start)
		admitErr, removeErr := <-admitted, <-removed
		if admitErr == nil {
			if !errors.Is(removeErr, ErrRunStorageReferenced) {
				t.Fatalf("admitted consumer lost source: dispose=%v", removeErr)
			}
			if _, e := os.Stat(filepath.Join(root, "evidence.bin")); e != nil {
				t.Fatal(e)
			}
		} else if removeErr != nil {
			t.Fatalf("neither admission nor disposal completed: %v %v", admitErr, removeErr)
		}
	}
}

func TestRunStorageEntryBudgetRejectsEmptyTreesWithoutCopying(t *testing.T) {
	source := filepath.Join(t.TempDir(), "many")
	if e := os.Mkdir(source, 0700); e != nil {
		t.Fatal(e)
	}
	for i := int64(0); i < RunStorageEntryLimit; i++ {
		if e := os.WriteFile(filepath.Join(source, fmt.Sprint(i)), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	ctx := context.WithValue(context.Background(), snapshotBudgetKey{}, &snapshotByteBudget{remaining: RunStorageBudgetBytes, entries: RunStorageEntryLimit})
	target := filepath.Join(t.TempDir(), "outputs")
	if e := preflightSnapshot(ctx, source, target, nil); !errors.Is(e, ErrRunStorageBudget) {
		t.Fatalf("entry preflight %v", e)
	}
	if _, e := os.Stat(target); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("entry preflight wrote copy: %v", e)
	}
}

func TestRunStorageFrozenUndispatchedBriefProtectsItsReferences(t *testing.T) {
	for _, kind := range []string{"files", "markdown"} {
		t.Run(kind, func(t *testing.T) {
			s, source := storageRun(t)
			root, _ := s.runStorageRoot(source)
			file := filepath.Join(root, "result.bin")
			writeFixture(t, file, "needed before dispatch")
			storageFinish(t, s, source)
			brief := "[formation.brief]\n"
			if kind == "files" {
				brief += "goal = " + renderString("Read the attached file") + "\nfiles = [" + renderString(file) + "]\n"
			} else {
				brief += "goal = " + renderString("Read [source]("+file+") before writing") + "\n"
			}
			fixture := strings.Replace(s4RunBoardFixture(), "title = "+renderString("Research")+"\n", "title = "+renderString("Research")+"\n"+brief, 1)
			writeFixture(t, s.BoardPath("session-search"), fixture)
			_, personas := s4RunFixture(t)
			consumer, e := s.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
			if e != nil {
				t.Fatal(e)
			}
			r, e := s.RunStorage(source)
			if e != nil || len(r.ProtectedBy) != 1 || r.ProtectedBy[0] != consumer.RunID {
				t.Fatalf("frozen brief protection %+v %v", r, e)
			}
			if _, e = s.DisposeRunArtifacts(source); !errors.Is(e, ErrRunStorageReferenced) {
				t.Fatalf("frozen brief disposed %v", e)
			}
		})
	}
}

func TestRunStorageAcceptedEntryBoundarySurvivesCompaction(t *testing.T) {
	s, id := storageRun(t)
	dir, e := s.createAttemptArtifactDir(id, "fmn_research", 1)
	if e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(t.TempDir(), "deliverable")
	if e = os.Mkdir(source, 0700); e != nil {
		t.Fatal(e)
	}
	// Four destination wrappers plus the output folder and4091 empty files are
	// exactly4096 retained entries. One extra file must fail before acceptance.
	for i := int64(0); i < RunStorageEntryLimit-5; i++ {
		if e = os.WriteFile(filepath.Join(source, fmt.Sprint(i)), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	extra := filepath.Join(source, "one-too-many")
	writeFixture(t, extra, "")
	ctx, e := s.outputCopyContext(context.Background(), id, dir, "port_out")
	if e != nil {
		t.Fatal(e)
	}
	if e = preflightSnapshot(ctx, source, filepath.Join(dir, "outputs", "port_out"), nil); !errors.Is(e, ErrRunStorageBudget) {
		t.Fatalf("boundary+1 accepted: %v", e)
	}
	if e = os.Remove(extra); e != nil {
		t.Fatal(e)
	}
	formation := FormationNode{ID: "fmn_research", Outputs: []FormationPort{{ID: "port_out", Kind: PortKindFolder}}}
	accepted, e := s.acceptAttemptOutputs(context.Background(), id, formation, 1, map[string]FormationOutputPayload{"port_out": {Ref: source}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.AppendRunEvent(id, RunEvent{Type: RunEventNodeOutput, NodeID: formation.ID, Attempt: 1, Data: map[string]any{"outputs": map[string]any{"port_out": map[string]any{"ref": accepted["port_out"].Ref, "kind": PortKindFolder}}}}); e != nil {
		t.Fatal(e)
	}
	storageFinish(t, s, id)
	root, _ := s.runStorageRoot(id)
	before, e := storageFiles(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if int64(len(before)) != RunStorageEntryLimit {
		t.Fatalf("accepted entries=%d", len(before))
	}
	if e = s.SweepRunStorage(context.Background()); e != nil {
		t.Fatal(e)
	}
	after, e := storageFiles(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	if len(after) != len(before) {
		t.Fatalf("accepted output lost entries on compaction: before=%d after=%d", len(before), len(after))
	}
	m, e := s.readStorageManifest(id)
	if e != nil || int64(len(m.Keep)) != RunStorageEntryLimit {
		t.Fatalf("retained manifest entries=%d %v", len(m.Keep), e)
	}
}
