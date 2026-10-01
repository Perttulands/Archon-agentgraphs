package formations

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type interposingLedgerReadSeeker struct {
	passes [][]byte
	reader *bytes.Reader
	seeks  int
}

func (r *interposingLedgerReadSeeker) Read(destination []byte) (int, error) {
	if r.reader == nil {
		return 0, io.EOF
	}
	return r.reader.Read(destination)
}

func (r *interposingLedgerReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if offset != 0 || whence != io.SeekStart {
		return 0, errors.New("interposing ledger only supports rewind")
	}
	index := r.seeks
	if index >= len(r.passes) {
		index = len(r.passes) - 1
	}
	r.reader = bytes.NewReader(r.passes[index])
	r.seeks++
	return 0, nil
}

func TestRunLedgerValidationAndDecodeConsumeSameBytes(t *testing.T) {
	runID := newPrefixedID("run")
	legacy := testRunLedgerBytes(t, testRunStartedEvent(runID, "session-search"))
	other := []byte(`{"ts":"2026-07-18T12:00:00Z","runId":"` + runID + `","seq":1,"type":"run_failed","actor":"agent:test"}` + "\n")
	ledger := &interposingLedgerReadSeeker{passes: [][]byte{legacy, other}}

	events, err := readRunEventsFrom(ledger, runID)
	if err != nil {
		t.Fatalf("validate and decode interposed ledger: %v", err)
	}
	if ledger.seeks != 1 {
		t.Fatalf("ledger rewind count = %d, want one validation and decode pass", ledger.seeks)
	}
	if len(events) != 1 || events[0].Type != RunEventStarted {
		t.Fatalf("decoded events = %#v, want the bytes validated on the first pass", events)
	}
}

func TestConfiguredWorkspaceSymlinkStillSupportsRunInspection(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	workspaceAlias := filepath.Join(root, "workspace-alias")
	runID := newPrefixedID("run")
	events := testLegacyRunEvents(runID, "session-search")
	ledgerPath := filepath.Join(workspace, runArtifactPath("session-search", runID, ".ndjson"))
	writeFixture(t, ledgerPath, string(testRunLedgerBytes(t, events...)))
	if err := os.Symlink(workspace, workspaceAlias); err != nil {
		t.Fatalf("symlink configured workspace: %v", err)
	}

	store := NewStore(workspaceAlias)
	gotEvents, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatalf("read run through configured workspace symlink: %v", err)
	}
	if len(gotEvents) != len(events) {
		t.Fatalf("event count = %d, want %d", len(gotEvents), len(events))
	}
	projection, err := store.ProjectRun(runID)
	if err != nil {
		t.Fatalf("project run through configured workspace symlink: %v", err)
	}
	if projection.RunID != runID || projection.BoardSlug != "session-search" || projection.Status != RunStatusBlocked {
		t.Fatalf("projection through configured workspace symlink = %+v", projection)
	}
	runs, err := store.ListRuns(RunListFilter{})
	if err != nil {
		t.Fatalf("list runs through configured workspace symlink: %v", err)
	}
	if len(runs) != 1 || runs[0].RunID != runID {
		t.Fatalf("listed runs through configured workspace symlink = %+v", runs)
	}
}

func TestConfiguredWorkspaceSymlinkStillSupportsRunCreation(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	workspaceAlias := filepath.Join(root, "workspace-alias")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.Symlink(workspace, workspaceAlias); err != nil {
		t.Fatalf("symlink configured workspace: %v", err)
	}
	store := NewStore(workspaceAlias)
	store.Now = fixedClock()
	personas := NewPersonaStore(filepath.Join(root, "agents"))
	personas.Now = fixedClock()
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board through configured workspace symlink: %v", err)
	}

	started, err := store.StartRun("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
		Personas:          personas,
	})
	if err != nil {
		t.Fatalf("start run through configured workspace symlink: %v", err)
	}
	events, err := store.ReadRunEvents(started.RunID)
	if err != nil {
		t.Fatalf("read created run through configured workspace symlink: %v", err)
	}
	if len(events) != 1 || events[0].Type != RunEventStarted {
		t.Fatalf("created run events = %+v", events)
	}
}

// A state directory under the workspace, such as .archon/runs moved to
// another disk, may be a symlink; it is followed (archon-4m4j).
func TestRunInspectionFollowsASymlinkedRunsDirectory(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	externalRuns := filepath.Join(root, "external-runs")
	runID := newPrefixedID("run")
	externalLedger := filepath.Join(externalRuns, "session-search", runID+".ndjson")
	writeFixture(t, externalLedger, string(testRunLedgerBytes(t, testRunStartedEvent(runID, "session-search"))))
	if err := os.MkdirAll(filepath.Join(workspace, ".archon"), 0o755); err != nil {
		t.Fatalf("create workspace archon directory: %v", err)
	}
	if err := os.Symlink(externalRuns, filepath.Join(workspace, ".archon", "runs")); err != nil {
		t.Fatalf("symlink external runs directory: %v", err)
	}

	events, err := NewStore(workspace).ReadRunEvents(runID)
	if err != nil {
		t.Fatalf("read a run through a symlinked runs directory: %v", err)
	}
	if len(events) != 1 || events[0].Type != RunEventStarted {
		t.Fatalf("events = %+v, want the linked run's ledger", events)
	}
}

func TestTerminalEventsStillValidateCanonicalLedgerIdentity(t *testing.T) {
	store, _ := s4RunFixture(t)
	store.Now = fixedClock()
	runID := newPrefixedID("run")
	ledgerPath := filepath.Join(store.Workspace, runArtifactPath("session-search", runID, ".ndjson"))
	ledgerBefore := testLegacyLedgerBytes(t, runID, "../outside")
	writeFixture(t, ledgerPath, string(ledgerBefore))

	for _, eventType := range []string{RunEventCanceled, RunEventFailed} {
		err := store.AppendRunEvent(runID, RunEvent{
			Type: eventType, Actor: "human:test", Data: map[string]any{"final": true},
		})
		if !errors.Is(err, ErrRunLedgerInvalid) {
			t.Fatalf("%s with forged board identity error = %v, want ErrRunLedgerInvalid", eventType, err)
		}
	}
	if got := readFile(t, ledgerPath); got != string(ledgerBefore) {
		t.Fatalf("rejected terminal containment events mutated forged ledger")
	}
}

func TestRunLedgerReaderBoundsEachEvent(t *testing.T) {
	store, _ := s4RunFixture(t)
	runID := newPrefixedID("run")
	ledgerPath := filepath.Join(store.Workspace, runArtifactPath("session-search", runID, ".ndjson"))
	events := testLegacyRunEvents(runID, "session-search")
	events[0].Data["oversized"] = strings.Repeat("x", runEventMaxBytes)
	writeFixture(t, ledgerPath, string(testRunLedgerBytes(t, events...)))

	if _, err := store.ReadRunEvents(runID); !errors.Is(err, ErrRunLedgerInvalid) {
		t.Fatalf("oversized ledger event error = %v, want ErrRunLedgerInvalid", err)
	}
}

func TestRunSnapshotReaderIsBoundedBeforeAuthorizingAppend(t *testing.T) {
	store, _ := s4RunFixture(t)
	store.Now = fixedClock()
	runID := newPrefixedID("run")
	snapshotPath := runArtifactPath("session-search", runID, ".snapshot.toml")
	ledgerPath := filepath.Join(store.Workspace, runArtifactPath("session-search", runID, ".ndjson"))
	oversizedBoard := s4MissionOnlyBoardFixture() + "\n# " + strings.Repeat("x", int(runRecordMaxBytes)) + "\n"
	writeFixture(t, filepath.Join(store.Workspace, snapshotPath), oversizedBoard)
	ledgerBefore := testRunLedgerBytes(t, testRunStartedEvent(runID, "session-search"))
	writeFixture(t, ledgerPath, string(ledgerBefore))

	err := store.AppendRunEvent(runID, RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work"})
	if !errors.Is(err, ErrRunLedgerInvalid) {
		t.Fatalf("append with oversized snapshot error = %v, want ErrRunLedgerInvalid", err)
	}
	if got := readFile(t, ledgerPath); got != string(ledgerBefore) {
		t.Fatalf("oversized snapshot rejection mutated ledger")
	}
}

func TestRunSnapshotIdentityRequiresStartedEventAndCanonicalBindingsSnapshot(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(event *RunEvent, runID string)
	}{
		{
			name: "first event must be run started",
			mutate: func(event *RunEvent, _ string) {
				event.Type = RunEventNodeStarted
			},
		},
		{
			name: "bindings snapshot is required",
			mutate: func(event *RunEvent, _ string) {
				delete(event.Data, "bindingsSnapshot")
			},
		},
		{
			name: "bindings snapshot must be canonical",
			mutate: func(event *RunEvent, runID string) {
				event.Data["bindingsSnapshot"] = runArtifactPath("other-board", runID, ".bindings.toml")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, _ := s4RunFixture(t)
			store.Now = fixedClock()
			runID := newPrefixedID("run")
			ledgerPath := filepath.Join(store.Workspace, runArtifactPath("session-search", runID, ".ndjson"))
			started := testRunStartedEvent(runID, "session-search")
			started.Data["bindingsSnapshot"] = runArtifactPath("session-search", runID, ".bindings.toml")
			test.mutate(&started, runID)
			ledgerBefore := testRunLedgerBytes(t, started)
			writeFixture(t, ledgerPath, string(ledgerBefore))

			err := store.AppendRunEvent(runID, RunEvent{Type: RunEventCanceled, Actor: "human:test"})
			if !errors.Is(err, ErrRunLedgerInvalid) {
				t.Fatalf("append with forged first event error = %v, want ErrRunLedgerInvalid", err)
			}
			if got := readFile(t, ledgerPath); got != string(ledgerBefore) {
				t.Fatalf("rejected first-event identity mutated ledger")
			}
		})
	}
}

func TestRunEngineDoesNotTrustSnapshotPathInput(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	engine := NewRunEngine(store, personas, &fakeRunExecutor{})
	runID := newPrefixedID("run")
	snapshotPath := runArtifactPath("session-search", runID, ".snapshot.toml")
	outside := filepath.Join(filepath.Dir(store.Workspace), "snapshot-victim.toml")
	writeFixture(t, outside, s4MissionOnlyBoardFixture())
	if err := os.MkdirAll(filepath.Dir(filepath.Join(store.Workspace, snapshotPath)), 0o770); err != nil {
		t.Fatalf("create snapshot directory: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(store.Workspace, snapshotPath)); err != nil {
		t.Fatalf("symlink snapshot: %v", err)
	}

	if _, err := engine.readRunBoard(snapshotPath); !errors.Is(err, ErrRunLedgerInvalid) {
		t.Fatalf("engine read with caller-supplied snapshot path error = %v, want ErrRunLedgerInvalid", err)
	}
}

func testLegacyLedgerBytes(t *testing.T, runID, boardSlug string) []byte {
	t.Helper()
	events := testLegacyRunEvents(runID, boardSlug)
	return testRunLedgerBytes(t, events...)
}

func testLegacyRunEvents(runID, boardSlug string) []RunEvent {
	started := testRunStartedEvent(runID, boardSlug)
	blocked := RunEvent{
		Timestamp: "2026-07-18T12:00:01Z",
		RunID:     runID,
		Seq:       2,
		Type:      RunEventBlocked,
		Actor:     "agent:test",
		BoardID:   "brd_01J9_sesssearch",
		BoardRev:  7,
		MissionID: "mis_showcase",
		Data:      map[string]any{"reason": "interrupted", "resumeAllowed": true},
	}
	return []RunEvent{started, blocked}
}

func testRunStartedEvent(runID, boardSlug string) RunEvent {
	return RunEvent{
		Timestamp: "2026-07-18T12:00:00Z",
		RunID:     runID,
		Seq:       1,
		Type:      RunEventStarted,
		Actor:     "agent:test",
		BoardID:   "brd_01J9_sesssearch",
		BoardRev:  7,
		MissionID: "mis_showcase",
		Data: map[string]any{
			"missionSlug":      boardSlug,
			"snapshot":         runArtifactPath(boardSlug, runID, ".snapshot.toml"),
			"bindingsSnapshot": runArtifactPath(boardSlug, runID, ".bindings.toml"),
		},
	}
}

func testRunLedgerBytes(t *testing.T, events ...RunEvent) []byte {
	t.Helper()
	var builder strings.Builder
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil {
			t.Fatalf("marshal run event: %v", err)
		}
		builder.Write(raw)
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}
