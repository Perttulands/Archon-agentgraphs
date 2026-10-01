package formations

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const legacyRunEventTimestamp = "2026-06-03T17:00:00Z"

func TestS4JudgeChainVerdictRoutesGate(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4JudgeChainRunBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	executor := &fakeRunExecutor{outputs: map[string]string{
		"fmn_j1": "review notes",
		"fmn_j2": "```archon-verdict\n{\"verdict\":\"pass\",\"reason\":\"reviewed\",\"evidence\":[]}\n```",
	}}
	engine := NewRunEngine(store, personas, executor)
	engine.SetGateEvaluator(NewCodeGateEvaluator())

	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
		Limits:            RunLimits{MaxDispatch: 8, MaxAttempts: 2},
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, want succeeded from judge pass verdict", status)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_work", "fmn_j1", "fmn_j2", "fmn_ship"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes = %v, want work, judge chain, ship", got)
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	verdict := eventOfType(t, events, RunEventGateVerdict)
	if verdict.Data["verdict"] != "pass" || verdict.Data["routePort"] != "pass" {
		t.Fatalf("gate verdict = %+v, want pass route from judge output", verdict)
	}
}

func TestRunSnapshotReadRejectsNoncanonicalLedgerPath(t *testing.T) {
	store, _ := s4RunFixture(t)
	store.Now = fixedClock()
	raw := s4MissionOnlyBoardFixture()
	runID := newPrefixedID("run")
	noncanonicalSnapshot := filepath.ToSlash(filepath.Join(".archon", "runs", "quarry", runID+".snapshot.toml"))
	ledger := filepath.Join(store.Workspace, runArtifactPath("session-search", runID, ".ndjson"))
	writeFixture(t, filepath.Join(store.Workspace, noncanonicalSnapshot), raw)
	if err := writeInitialRunEvent(ledger, RunEvent{
		Timestamp: legacyRunEventTimestamp, RunID: runID, Seq: 1, Type: RunEventStarted, BoardID: "brd_01J9_sesssearch", BoardRev: 7,
		MissionID: "mis_showcase", Actor: "agent:test", Data: map[string]any{
			"missionSlug": "session-search", "snapshot": noncanonicalSnapshot,
			"bindingsSnapshot": runArtifactPath("session-search", runID, ".bindings.toml"),
		},
	}); err != nil {
		t.Fatalf("write forged run start: %v", err)
	}
	before, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatalf("read forged run: %v", err)
	}
	err = store.AppendRunEvent(runID, RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work"})
	if !errors.Is(err, ErrRunLedgerInvalid) {
		t.Fatalf("noncanonical snapshot append error = %v, want ErrRunLedgerInvalid", err)
	}
	after, readErr := store.ReadRunEvents(runID)
	if readErr != nil {
		t.Fatalf("read rejected forged run: %v", readErr)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("noncanonical snapshot rejection mutated ledger\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestRunSnapshotReadRejectsLedgerControlledIdentityBeforeMutation(t *testing.T) {
	tests := []struct {
		name          string
		ledgerSlug    string
		rejectsOnRead bool
		setup         func(t *testing.T, store *Store, requestedRunID string) RunEvent
	}{
		{
			name: "traversal board slug",
			setup: func(t *testing.T, store *Store, requestedRunID string) RunEvent {
				t.Helper()
				outside := filepath.Join(filepath.Dir(store.Workspace), "outside")
				runsRoot := filepath.Join(store.Workspace, ".archon", "runs")
				slug, err := filepath.Rel(runsRoot, outside)
				if err != nil {
					t.Fatalf("derive traversal slug: %v", err)
				}
				snapshot := runArtifactPath(filepath.ToSlash(slug), requestedRunID, ".snapshot.toml")
				resolved := filepath.Clean(filepath.Join(store.Workspace, snapshot))
				wantResolved := filepath.Join(outside, requestedRunID+".snapshot.toml")
				if resolved != wantResolved {
					t.Fatalf("resolved traversal snapshot = %q, want %q", resolved, wantResolved)
				}
				writeFixture(t, resolved, s4MissionOnlyBoardFixture())
				return runStartedFixture(requestedRunID, filepath.ToSlash(slug), snapshot)
			},
		},
		{
			name:          "first event run id differs from ledger",
			rejectsOnRead: true,
			setup: func(t *testing.T, store *Store, _ string) RunEvent {
				t.Helper()
				startedRunID := newPrefixedID("run")
				snapshot := runArtifactPath("session-search", startedRunID, ".snapshot.toml")
				writeFixture(t, filepath.Join(store.Workspace, snapshot), s4MissionOnlyBoardFixture())
				return runStartedFixture(startedRunID, "session-search", snapshot)
			},
		},
		{
			name: "board slug differs from ledger directory",
			setup: func(t *testing.T, store *Store, requestedRunID string) RunEvent {
				t.Helper()
				snapshot := runArtifactPath("other-board", requestedRunID, ".snapshot.toml")
				writeFixture(t, filepath.Join(store.Workspace, snapshot), s4MissionOnlyBoardFixture())
				return runStartedFixture(requestedRunID, "other-board", snapshot)
			},
		},
		{
			name: "snapshot symlink",
			setup: func(t *testing.T, store *Store, requestedRunID string) RunEvent {
				t.Helper()
				outside := filepath.Join(filepath.Dir(store.Workspace), "outside.snapshot.toml")
				writeFixture(t, outside, s4MissionOnlyBoardFixture())
				snapshot := runArtifactPath("session-search", requestedRunID, ".snapshot.toml")
				if err := os.MkdirAll(filepath.Dir(filepath.Join(store.Workspace, snapshot)), 0o755); err != nil {
					t.Fatalf("create snapshot directory: %v", err)
				}
				if err := os.Symlink(outside, filepath.Join(store.Workspace, snapshot)); err != nil {
					t.Fatalf("symlink snapshot: %v", err)
				}
				return runStartedFixture(requestedRunID, "session-search", snapshot)
			},
		},
		{
			name:       "snapshot board slug differs from canonical run directory",
			ledgerSlug: "other-board",
			setup: func(t *testing.T, store *Store, requestedRunID string) RunEvent {
				t.Helper()
				snapshot := runArtifactPath("other-board", requestedRunID, ".snapshot.toml")
				writeFixture(t, filepath.Join(store.Workspace, snapshot), s4MissionOnlyBoardFixture())
				return runStartedFixture(requestedRunID, "other-board", snapshot)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			if err := os.MkdirAll(workspace, 0o755); err != nil {
				t.Fatalf("create workspace: %v", err)
			}
			store := NewStore(workspace)
			store.Now = fixedClock()
			requestedRunID := newPrefixedID("run")
			started := test.setup(t, store, requestedRunID)
			ledgerSlug := test.ledgerSlug
			if ledgerSlug == "" {
				ledgerSlug = "session-search"
			}
			ledger := filepath.Join(store.Workspace, runArtifactPath(ledgerSlug, requestedRunID, ".ndjson"))
			if err := writeInitialRunEvent(ledger, started); err != nil {
				t.Fatalf("write forged run start: %v", err)
			}
			ledgerBefore := readFile(t, ledger)
			before, err := store.ReadRunEvents(requestedRunID)
			if test.rejectsOnRead {
				if !errors.Is(err, ErrRunLedgerInvalid) {
					t.Fatalf("read forged run error = %v, want ErrRunLedgerInvalid", err)
				}
				err = store.AppendRunEvent(requestedRunID, RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work"})
				if !errors.Is(err, ErrRunLedgerInvalid) {
					t.Fatalf("forged snapshot append error = %v, want ErrRunLedgerInvalid", err)
				}
				if after := readFile(t, ledger); after != ledgerBefore {
					t.Fatalf("forged identity rejection mutated ledger bytes")
				}
				return
			}
			if err != nil {
				t.Fatalf("read forged run: %v", err)
			}
			err = store.AppendRunEvent(requestedRunID, RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work"})
			if !errors.Is(err, ErrRunLedgerInvalid) {
				t.Fatalf("forged snapshot append error = %v, want ErrRunLedgerInvalid", err)
			}
			after, readErr := store.ReadRunEvents(requestedRunID)
			if readErr != nil {
				t.Fatalf("read rejected forged run: %v", readErr)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("forged snapshot rejection mutated ledger\nbefore=%+v\nafter=%+v", before, after)
			}
		})
	}
}

func runStartedFixture(runID, boardSlug, snapshot string) RunEvent {
	return RunEvent{
		Timestamp: legacyRunEventTimestamp, RunID: runID, Seq: 1, Type: RunEventStarted, BoardID: "brd_01J9_sesssearch", BoardRev: 7,
		MissionID: "mis_showcase", Actor: "agent:test", Data: map[string]any{
			"missionSlug": boardSlug, "snapshot": snapshot,
			"bindingsSnapshot": runArtifactPath(boardSlug, runID, ".bindings.toml"),
		},
	}
}

func s4JudgeChainRunBoardFixture() string {
	return s4MissionOnlyBoardFixture() + `
[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.input]]
id = "port_work_in"
label = "Input"

[[formation.output]]
id = "port_work_out"
label = "Output"

[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["code", "formation"]
criterion = "Judge the work"
check = "output_contains"
checkVersion = "1"
checkValue = "output from"

[[formation]]
id = "fmn_j1"
type = "solo"
title = "Judge 1"

[[formation.input]]
id = "port_j1_in"
label = "Input"

[[formation.output]]
id = "port_j1_out"
label = "Output"

[[formation.slot]]
id = "slot_j1"
label = "Judge"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[formation]]
id = "fmn_j2"
type = "solo"
title = "Judge 2"

[[formation.input]]
id = "port_j2_in"
label = "Input"

[[formation.output]]
id = "port_j2_out"
label = "Output"

[[formation.slot]]
id = "slot_j2"
label = "Judge"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[formation]]
id = "fmn_ship"
type = "solo"
title = "Ship"

[[formation.input]]
id = "port_ship_in"
label = "Input"

[[formation.output]]
id = "port_ship_out"
label = "Output"

[[formation.slot]]
id = "slot_ship"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[connection]]
id = "edge_mission_work"
from = "mis_showcase:out"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_work_gate"
from = "fmn_work:port_work_out"
to = "gate_review:in"

[[connection]]
id = "edge_gate_j1"
from = "gate_review:judge"
to = "fmn_j1:port_j1_in"

[[connection]]
id = "edge_j1_j2"
from = "fmn_j1:port_j1_out"
to = "fmn_j2:port_j2_in"

[[connection]]
id = "edge_j2_gate"
from = "fmn_j2:port_j2_out"
to = "gate_review:judge"

[[connection]]
id = "edge_gate_pass_ship"
from = "gate_review:pass"
to = "fmn_ship:port_ship_in"
`
}

func s4VerificationBoardFixture(onFail string) string {
	return s4MissionOnlyBoardFixture() + `
[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.input]]
id = "port_work_in"
label = "Input"

[[formation.output]]
id = "port_work_out"
label = "Output"

[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[formation.verification]
id = "ver_work"
kinds = ["code"]
criterion = "Work is ready"
onFail = "` + onFail + `"

[[formation]]
id = "fmn_ship"
type = "solo"
title = "Ship"

[[formation.input]]
id = "port_ship_in"
label = "Input"

[[formation.output]]
id = "port_ship_out"
label = "Output"

[[formation.slot]]
id = "slot_ship"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[connection]]
id = "edge_mission_work"
from = "mis_showcase:out"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_work_ship"
from = "fmn_work:port_work_out"
to = "fmn_ship:port_ship_in"
`
}
