package formations

import (
	"reflect"
	"strings"
	"testing"
)

// branchingBoardFormation, branchingBoardConnection and branchingBoardHumanGate
// build the small branching boards the engine and gate-route tests share.
func branchingBoardFormation(id, title string) string {
	return `
[[formation]]
id = "` + id + `"
type = "solo"
title = "` + title + `"

[[formation.input]]
id = "port_` + id + `_in"
label = "Input"

[[formation.output]]
id = "port_` + id + `_out"
label = "Output"

[[formation.slot]]
id = "slot_` + id + `"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
effort = "medium"
controller = true
`
}

func branchingBoardConnection(id, from, to string) string {
	return `
[[connection]]
id = "` + id + `"
from = "` + from + `"
to = "` + to + `"
`
}

func branchingBoardHumanGate(id string) string {
	return `
[[gate]]
id = "` + id + `"
title = "Review"
kinds = ["human"]
criterion = "Good enough"
`
}

// mission -> A -> terminal human gate, and mission -> B -> C.
func branchingGateBoardFixture() string {
	return s4MissionOnlyBoardFixture() +
		branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_b", "B") + branchingBoardFormation("fmn_c", "C") +
		branchingBoardHumanGate("gate_review") +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_review:in") +
		branchingBoardConnection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in") +
		branchingBoardConnection("edge_b_c", "fmn_b:port_fmn_b_out", "fmn_c:port_fmn_c_in")
}

// mission -> A -> terminal human gate 1, and mission -> B -> terminal human gate 2.
func twoTerminalGatesBoardFixture() string {
	return s4MissionOnlyBoardFixture() +
		branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_b", "B") +
		branchingBoardHumanGate("gate_one") + branchingBoardHumanGate("gate_two") +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_one:in") +
		branchingBoardConnection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in") +
		branchingBoardConnection("edge_b_gate", "fmn_b:port_fmn_b_out", "gate_two:in")
}

func startBranchingRun(t *testing.T, fixture string, executor FormationExecutor) (*Store, *PersonaStore, *RunEngine, *RunStatusProjection) {
	t.Helper()
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), fixture)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
		Limits: RunLimits{MaxDispatch: 10, MaxAttempts: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, personas, engine, status
}

// succeededAfterOutputs fails unless the ledger ends in run_succeeded and every
// named node produced output before it.
func requireSucceededAfterOutputs(t *testing.T, store *Store, runID string, nodes ...string) {
	t.Helper()
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Type != RunEventSucceeded {
		t.Fatalf("ledger ends in %s, want %s: %s", last.Type, RunEventSucceeded, eventTypeTrail(events))
	}
	for _, node := range nodes {
		found := false
		for _, event := range events {
			if event.Type == RunEventNodeOutput && event.NodeID == node {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("run succeeded without output from %s: %s", node, eventTypeTrail(events))
		}
	}
}

func pendingHumanGates(t *testing.T, store *Store, runID string) []string {
	t.Helper()
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	var gates []string
	seen := map[string]bool{}
	for _, event := range events {
		if event.Type != RunEventHumanInputRequested || seen[event.GateID] {
			continue
		}
		seen[event.GateID] = true
		if _, ok := latestHumanRequest(events, event.GateID); ok {
			gates = append(gates, event.GateID)
		}
	}
	return gates
}

func eventTypeTrail(events []RunEvent) string {
	trail := make([]string, 0, len(events))
	for _, event := range events {
		entry := event.Type
		if event.NodeID != "" {
			entry += ":" + event.NodeID
		}
		trail = append(trail, entry)
	}
	return strings.Join(trail, ", ")
}

// form-n7u.53: approving a terminal human gate continues every branch that
// is still to run, and the run succeeds only after them. A second engine on
// the same store replays the ledger to the same outcome.
func TestApprovingATerminalHumanGateRunsTheOtherBranchesBeforeSuccess(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "same engine", true: "restarted engine"}[restart], func(t *testing.T) {
			executor := &fakeRunExecutor{}
			store, personas, engine, status := startBranchingRun(t, branchingGateBoardFixture(), executor)
			if got := pendingHumanGates(t, store, status.RunID); status.Final || !reflect.DeepEqual(got, []string{"gate_review"}) {
				t.Fatalf("status = %+v, pending %v, want waiting for the gate", status, got)
			}
			if got, want := executor.nodeIDs(), []string{"fmn_a"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("nodes before the verdict = %v, want %v", got, want)
			}
			if restart {
				engine = NewRunEngine(store, personas, executor)
			}
			status, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"})
			if err != nil {
				t.Fatal(err)
			}
			if status.Final {
				t.Fatalf("recording the verdict finished the run: %+v", status)
			}
			if restart {
				engine = NewRunEngine(store, personas, executor)
			}
			executor.calls = nil
			status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
			if err != nil {
				t.Fatal(err)
			}
			if got, want := executor.nodeIDs(), []string{"fmn_b", "fmn_c"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("nodes after approval = %v, want %v", got, want)
			}
			if status.Status != RunStatusSucceeded || !status.Final {
				t.Fatalf("status = %+v, want succeeded", status)
			}
			requireSucceededAfterOutputs(t, store, status.RunID, "fmn_a", "fmn_b", "fmn_c")
		})
	}
}

// A branch that fails after the approval leaves a resumable block; the
// terminal pass already in the ledger must not let the next resume skip it.
func TestATerminalPassInTheLedgerDoesNotSkipABranchThatFailedAfterIt(t *testing.T) {
	executor := &seatLossOnceExecutor{failNodeID: "fmn_b"}
	store, _, engine, status := startBranchingRun(t, branchingGateBoardFixture(), executor)
	status, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"})
	if err != nil {
		t.Fatal(err)
	}
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusBlocked || !status.ResumeAllowed {
		t.Fatalf("status = %+v, want a resumable block for B's lost seat", status)
	}
	executor.calls = nil
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "redispatch", Reason: "seat lost; run B again"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_b", "fmn_c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nodes after the second resume = %v, want %v", got, want)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, want succeeded", status)
	}
	requireSucceededAfterOutputs(t, store, status.RunID, "fmn_a", "fmn_b", "fmn_c")
}

// A terminal judge (formation-kind) pass on one branch must not let a resume
// succeed past another branch that blocked after it.
func TestATerminalJudgePassDoesNotSkipABlockedBranchOnResume(t *testing.T) {
	fixture := strings.Replace(s4JudgeChainRunBoardFixture(), `kinds = ["code", "formation"]`, `kinds = ["formation"]`, 1)
	fixture = strings.Replace(fixture, "from = \"gate_review:pass\"\nto = \"fmn_ship:port_ship_in\"", "from = \"mis_showcase:out\"\nto = \"fmn_ship:port_ship_in\"", 1)
	if !strings.Contains(fixture, "from = \"mis_showcase:out\"\nto = \"fmn_ship:port_ship_in\"") {
		t.Fatal("fixture rewrite failed")
	}
	executor := &seatLossOnceExecutor{failNodeID: "fmn_ship"}
	executor.outputs = map[string]string{"fmn_j2": judgeBlock("pass", "fine")}
	store, _, engine, status := startBranchingRun(t, fixture, executor)
	if status.Status != RunStatusBlocked || !status.ResumeAllowed {
		t.Fatalf("status = %+v, want a resumable block for ship's lost seat", status)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !terminalPassReached(events) {
		t.Fatalf("setup: the judge gate did not pass terminally: %s", eventTypeTrail(events))
	}
	executor.calls = nil
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "redispatch", Reason: "seat lost; run ship again"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_ship"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nodes after resume = %v, want %v", got, want)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, want succeeded", status)
	}
	requireSucceededAfterOutputs(t, store, status.RunID, "fmn_work", "fmn_ship")
}

// With two terminal human gates on separate branches, approving the first
// runs the second branch up to its own gate and waits there; only the second
// approval ends the run.
func TestTwoTerminalHumanGatesOnSeparateBranchesEachWaitForTheirVerdict(t *testing.T) {
	executor := &fakeRunExecutor{}
	store, personas, engine, status := startBranchingRun(t, twoTerminalGatesBoardFixture(), executor)
	if got := pendingHumanGates(t, store, status.RunID); !reflect.DeepEqual(got, []string{"gate_one"}) {
		t.Fatalf("waiting gates = %+v, want gate_one", got)
	}
	status, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_one", Verdict: "pass", Actor: "human:operator"})
	if err != nil {
		t.Fatal(err)
	}
	executor.calls = nil
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nodes after the first approval = %v, want %v", got, want)
	}
	if got := pendingHumanGates(t, store, status.RunID); status.Final || !reflect.DeepEqual(got, []string{"gate_two"}) {
		t.Fatalf("status = %+v, pending %v, want waiting for gate_two", status, got)
	}
	// The second verdict and resume run on a fresh engine, as after a restart.
	engine = NewRunEngine(store, personas, executor)
	status, err = engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_two", Verdict: "pass", Actor: "human:operator"})
	if err != nil {
		t.Fatal(err)
	}
	executor.calls = nil
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
	if err != nil {
		t.Fatal(err)
	}
	if got := executor.nodeIDs(); len(got) != 0 {
		t.Fatalf("nodes after the second approval = %v, want none", got)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, want succeeded", status)
	}
	requireSucceededAfterOutputs(t, store, status.RunID, "fmn_a", "fmn_b")
}
