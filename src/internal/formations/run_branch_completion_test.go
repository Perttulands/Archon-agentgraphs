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

// branchingBoardEnds are the Done and Rejected End nodes every branching
// board ends its paths at (archon-o7p.10).
func branchingBoardEnds() string {
	return `
[[end]]
id = "end_done"
title = "Done"
outcome = "done"

[[end]]
id = "end_rejected"
title = "Rejected"
outcome = "rejected"
`
}

// branchingBoardGateEnds wires a gate whose pass ends the path done and whose
// fail ends it rejected.
func branchingBoardGateEnds(gateID string) string {
	return branchingBoardConnection("edge_"+gateID+"_pass", gateID+":pass", "end_done:in") +
		branchingBoardConnection("edge_"+gateID+"_fail", gateID+":fail", "end_rejected:in")
}

// mission -> A -> human gate (pass Done, fail Rejected), and mission -> B -> C -> Done.
func branchingGateBoardFixture() string {
	return s4MissionOnlyBoardFixture() +
		branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_b", "B") + branchingBoardFormation("fmn_c", "C") +
		branchingBoardHumanGate("gate_review") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_review:in") +
		branchingBoardGateEnds("gate_review") +
		branchingBoardConnection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in") +
		branchingBoardConnection("edge_b_c", "fmn_b:port_fmn_b_out", "fmn_c:port_fmn_c_in") +
		branchingBoardConnection("edge_c_done", "fmn_c:port_fmn_c_out", "end_done:in")
}

// mission -> A -> human gate 1, and mission -> B -> human gate 2; each gate's
// pass ends its path Done and its fail ends it Rejected.
func twoTerminalGatesBoardFixture() string {
	return s4MissionOnlyBoardFixture() +
		branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_b", "B") +
		branchingBoardHumanGate("gate_one") + branchingBoardHumanGate("gate_two") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_one:in") +
		branchingBoardGateEnds("gate_one") +
		branchingBoardConnection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in") +
		branchingBoardConnection("edge_b_gate", "fmn_b:port_fmn_b_out", "gate_two:in") +
		branchingBoardGateEnds("gate_two")
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

func gateVerdictRecorded(events []RunEvent, gateID, routePort string) bool {
	for _, event := range events {
		if event.Type == RunEventGateVerdict && event.GateID == gateID && stringFromEventData(event, "routePort") == routePort {
			return true
		}
	}
	return false
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

// archon-o7p.11: the other branches run while a terminal human gate waits,
// and approving it ends the run, which succeeds only after every branch. A
// second engine on the same store continues from the ledger to the same
// outcome.
func TestOtherBranchesRunWhileATerminalHumanGateWaits(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "same engine", true: "restarted engine"}[restart], func(t *testing.T) {
			executor := &fakeRunExecutor{}
			store, personas, engine, status := startBranchingRun(t, branchingGateBoardFixture(), executor)
			if got := pendingHumanGates(t, store, status.RunID); status.Final || !reflect.DeepEqual(got, []string{"gate_review"}) {
				t.Fatalf("status = %+v, pending %v, want waiting for the gate", status, got)
			}
			if got, want := executor.nodeIDs(), []string{"fmn_a", "fmn_b", "fmn_c"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("nodes before the verdict = %v, want %v", got, want)
			}
			if status.Status != RunStatusRunning {
				t.Fatalf("status = %+v, want running with the gate waiting", status)
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
			status, err = engine.ContinueRun(status.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if got := executor.nodeIDs(); len(got) != 0 {
				t.Fatalf("nodes after approval = %v, want none", got)
			}
			if status.Status != RunStatusSucceeded || !status.Final {
				t.Fatalf("status = %+v, want succeeded", status)
			}
			requireSucceededAfterOutputs(t, store, status.RunID, "fmn_a", "fmn_b", "fmn_c")
		})
	}
}

// A branch that fails while the gate waits blocks the run, and the gate's
// request stays open. The operator may answer it on the blocked run, which
// routes the verdict once resumed, or resume first, which runs the branch
// while the gate keeps waiting. Either way the run succeeds after both.
func TestABranchBlockedWhileAGateWaitsResumesAroundTheGate(t *testing.T) {
	for _, answerFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "answer, then resume", false: "resume, then answer"}[answerFirst], func(t *testing.T) {
			executor := &seatLossOnceExecutor{failNodeID: "fmn_b"}
			store, _, engine, status := startBranchingRun(t, branchingGateBoardFixture(), executor)
			if status.Status != RunStatusBlocked || !status.ResumeAllowed {
				t.Fatalf("status = %+v, want a resumable block for B's lost seat", status)
			}
			if got := pendingHumanGates(t, store, status.RunID); !reflect.DeepEqual(got, []string{"gate_review"}) {
				t.Fatalf("pending gates = %v, want gate_review", got)
			}
			approve := func() {
				t.Helper()
				if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"}); err != nil {
					t.Fatal(err)
				}
			}
			executor.calls = nil
			var err error
			if answerFirst {
				approve()
				if status, err = store.ProjectRun(status.RunID); err != nil || status.Status != RunStatusBlocked || !status.ResumeAllowed {
					t.Fatalf("after the verdict = %+v, %v; want still blocked and resumable", status, err)
				}
				status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "redispatch", Reason: "seat lost; run B again"})
			} else {
				if status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "redispatch", Reason: "seat lost; run B again"}); err != nil {
					t.Fatal(err)
				}
				if got := pendingHumanGates(t, store, status.RunID); status.Final || !reflect.DeepEqual(got, []string{"gate_review"}) {
					t.Fatalf("after the resume = %+v, pending %v; want waiting for the gate", status, got)
				}
				approve()
				status, err = engine.ContinueRun(status.RunID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got, want := executor.nodeIDs(), []string{"fmn_b", "fmn_c"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("nodes after the block = %v, want %v", got, want)
			}
			if status.Status != RunStatusSucceeded {
				t.Fatalf("status = %+v, want succeeded", status)
			}
			requireSucceededAfterOutputs(t, store, status.RunID, "fmn_a", "fmn_b", "fmn_c")
		})
	}
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
	if !gateVerdictRecorded(events, "gate_review", "pass") {
		t.Fatalf("setup: the judge gate did not pass: %s", eventTypeTrail(events))
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

// archon-o7p.11: two terminal human gates on separate branches are both open
// at once and can be answered in either order, on the same engine or after a
// restart; only the last answer ends the run.
func TestTwoTerminalHumanGatesOnSeparateBranchesAreOpenAtOnce(t *testing.T) {
	for _, order := range [][]string{{"gate_one", "gate_two"}, {"gate_two", "gate_one"}} {
		t.Run(strings.Join(order, " then "), func(t *testing.T) {
			executor := &fakeRunExecutor{}
			store, personas, engine, status := startBranchingRun(t, twoTerminalGatesBoardFixture(), executor)
			if got := pendingHumanGates(t, store, status.RunID); !reflect.DeepEqual(got, []string{"gate_one", "gate_two"}) {
				t.Fatalf("waiting gates = %+v, want both", got)
			}
			if got, want := executor.nodeIDs(), []string{"fmn_a", "fmn_b"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("nodes before any verdict = %v, want %v", got, want)
			}
			executor.calls = nil
			for i, gateID := range order {
				// The second verdict runs on a fresh engine, as after a restart.
				if i == 1 {
					engine = NewRunEngine(store, personas, executor)
				}
				if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: gateID, Verdict: "pass", Actor: "human:operator"}); err != nil {
					t.Fatal(err)
				}
				var err error
				if status, err = engine.ContinueRun(status.RunID); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					if got := pendingHumanGates(t, store, status.RunID); status.Final || !reflect.DeepEqual(got, []string{order[1]}) {
						t.Fatalf("after %s: status %+v, pending %v; want waiting for %s", gateID, status, got, order[1])
					}
				}
			}
			if got := executor.nodeIDs(); len(got) != 0 {
				t.Fatalf("nodes after the approvals = %v, want none", got)
			}
			if status.Status != RunStatusSucceeded {
				t.Fatalf("status = %+v, want succeeded", status)
			}
			requireSucceededAfterOutputs(t, store, status.RunID, "fmn_a", "fmn_b")
		})
	}
}
