package formations

import (
	"reflect"
	"testing"
	"time"
)

// End nodes end paths on purpose (archon-o7p.10). A run finishes when every path
// has ended and nothing else can run (unfinishedRunWork), and fails when a
// path ended at a rejected End node, with the reason of the gate verdict that
// routed there. These tests drive the engine on linear and branching boards,
// with a fresh engine standing in for a coordinator restart.

func requireRunFailedWithReason(t *testing.T, store *Store, runID, endID, gateID, reason string) {
	t.Helper()
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Type != RunEventFailed || last.Data["code"] != RunFailurePathRejected || last.Data["reason"] != reason ||
		last.Data["endId"] != endID || last.Data["gateId"] != gateID || last.Data["final"] != true {
		t.Fatalf("ledger ends in %+v, want run_failed at %s from %s with reason %q: %s", last, endID, gateID, reason, eventTypeTrail(events))
	}
	for _, event := range events {
		if event.Type == RunEventSucceeded {
			t.Fatalf("a run whose path was rejected recorded success: %s", eventTypeTrail(events))
		}
	}
	if problem := RunEndProblem(events); problem == nil || problem.Code != RunFailurePathRejected || problem.Reason.Text != reason {
		t.Fatalf("run end problem = %+v, want the gate's reason", problem)
	}
}

func rejectAndContinue(t *testing.T, engine *RunEngine, runID, gateID, reason string) *RunStatusProjection {
	t.Helper()
	if _, err := engine.RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: gateID, Verdict: "fail", Reason: reason, Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	status, err := engine.ContinueRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

// A linear path ending at a Done End node succeeds as soon as it ends.
func TestAPathEndingDoneSucceedsTheRun(t *testing.T) {
	board := s4MissionOnlyBoardFixture() + branchingBoardFormation("fmn_w", "W") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_w", "mis_showcase:out", "fmn_w:port_fmn_w_in") +
		branchingBoardConnection("edge_w_done", "fmn_w:port_fmn_w_out", "end_done:in")
	executor := &fakeRunExecutor{}
	store, _, _, status := startBranchingRun(t, board, executor)
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded", status)
	}
	requireSucceededAfterOutputs(t, store, status.RunID, "fmn_w")
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if ends := events[len(events)-1].Data["endIds"]; !reflect.DeepEqual(ends, []any{"end_done"}) {
		t.Fatalf("run_succeeded endIds = %#v, want the End node the path reached", ends)
	}
}

// A linear path whose step's output ends at a Rejected End node fails the
// run; with no gate verdict, the reason names the End node.
func TestAStepOutputEndingRejectedFailsTheRunNamingTheEnd(t *testing.T) {
	board := s4MissionOnlyBoardFixture() + branchingBoardFormation("fmn_w", "W") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_w", "mis_showcase:out", "fmn_w:port_fmn_w_in") +
		branchingBoardConnection("edge_w_rejected", "fmn_w:port_fmn_w_out", "end_rejected:in")
	store, _, _, status := startBranchingRun(t, board, &fakeRunExecutor{})
	if status.Status != RunStatusFailed || !status.Final {
		t.Fatalf("status = %+v, want failed", status)
	}
	requireRunFailedWithReason(t, store, status.RunID, "end_rejected", "", "the path ended at Rejected (rejected)")
}

// Rejecting a human gate whose fail ends rejected fails the run with the
// operator's reason, the same on a linear board and on a branching one,
// where the other branch runs to its own end first (archon-n7u.54).
func TestRejectingAGateWhoseFailEndsRejectedFailsTheRunWithItsReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		board  string
		before []string
		after  []string
	}{
		{name: "linear", board: linearGateBoardFixture(), before: []string{"fmn_w"}, after: []string{}},
		// The other branch ran while the gate waited (archon-o7p.11).
		{name: "branching", board: branchingGateBoardFixture(), before: []string{"fmn_a", "fmn_b", "fmn_c"}, after: []string{}},
	} {
		for _, restart := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "", true: " after a restart"}[restart], func(t *testing.T) {
				executor := &fakeRunExecutor{}
				store, personas, engine, status := startBranchingRun(t, tc.board, executor)
				if got := executor.nodeIDs(); !reflect.DeepEqual(got, tc.before) {
					t.Fatalf("nodes before the verdict = %v, want %v", got, tc.before)
				}
				frozen, err := store.ReadRunBoard(status.RunID)
				if err != nil {
					t.Fatal(err)
				}
				events, err := store.ReadRunEvents(status.RunID)
				if err != nil {
					t.Fatal(err)
				}
				gate := pendingHumanGates(t, store, status.RunID)[0]
				reject := HumanGateRoutes(frozen, events, gate, time.Time{})[1]
				if !reject.EndsRun || len(reject.Targets) != 1 || reject.Targets[0].Outcome != EndOutcomeRejected {
					t.Fatalf("reject route = %+v", reject)
				}
				if restart {
					engine = NewRunEngine(store, personas, executor)
				}
				executor.calls = nil
				status = rejectAndContinue(t, engine, status.RunID, gate, "the brief misses the audience")
				if got := executor.nodeIDs(); !reflect.DeepEqual(append([]string{}, got...), tc.after) {
					t.Fatalf("nodes after the rejection = %v, want %v", got, tc.after)
				}
				if status.Status != RunStatusFailed || !status.Final || status.EndedBy != "human:operator" {
					t.Fatalf("status = %+v, want failed, ended by the operator who rejected", status)
				}
				requireRunFailedWithReason(t, store, status.RunID, "end_rejected", gate, "the brief misses the audience")
			})
		}
	}
}

// On a branching board a rejected path does not stop the other branch: its
// own gate still waits for an answer, and the run fails only once that path
// has ended too, whatever its outcome.
func TestARejectedPathFailsTheRunOnlyAfterEveryOtherPathHasEnded(t *testing.T) {
	executor := &fakeRunExecutor{}
	store, personas, engine, status := startBranchingRun(t, twoTerminalGatesBoardFixture(), executor)
	status = rejectAndContinue(t, engine, status.RunID, "gate_one", "A is wrong")
	if got := pendingHumanGates(t, store, status.RunID); status.Final || !reflect.DeepEqual(got, []string{"gate_two"}) {
		t.Fatalf("status = %+v, pending %v, want the run waiting at gate_two", status, got)
	}
	frozen, err := store.ReadRunBoard(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	// Approving gate_two ends the last path; the run fails for gate_one.
	if approve := HumanGateRoutes(frozen, events, "gate_two", time.Time{})[0]; !approve.EndsRun || !approve.RunFails {
		t.Fatalf("approve gate_two = %+v, want it to end the run, which fails", approve)
	}
	engine = NewRunEngine(store, personas, executor)
	status = approveAndContinue(t, engine, status.RunID, "gate_two")
	if status.Status != RunStatusFailed {
		t.Fatalf("status = %+v, want failed", status)
	}
	requireRunFailedWithReason(t, store, status.RunID, "end_rejected", "gate_one", "A is wrong")
}

// A restart after the last path ended Done but before the run recorded its
// end: the resume runs nothing and succeeds.
func TestAResumeAfterEveryPathEndedDoneSucceedsWithoutRunningAnything(t *testing.T) {
	executor := &fakeRunExecutor{}
	store, personas, engine, status := startBranchingRun(t, branchingGateBoardFixture(), executor)
	// B and C ran and ended Done while the gate waited; the operator approves
	// and the coordinator dies before routing the approval.
	if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	if err := engine.BlockInterruptedRun(status.RunID); err != nil {
		t.Fatal(err)
	}
	executor.calls = nil
	status, err := NewRunEngine(store, personas, executor).ResumeRun(status.RunID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "restart"})
	if err != nil {
		t.Fatal(err)
	}
	if got := executor.nodeIDs(); len(got) != 0 {
		t.Fatalf("resume ran %v, want nothing", got)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, want succeeded", status)
	}
}

// runPaths reads every delivery to an End node from the ledger, with the
// verdict's reason and actor when a gate routed there.
func TestRunPathsReadsEndedPathsFromTheLedger(t *testing.T) {
	board, err := parseBoard([]byte(branchingGateBoardFixture()))
	if err != nil {
		t.Fatal(err)
	}
	events := []RunEvent{
		{Seq: 1, Type: RunEventNodeOutput, NodeID: "mis_showcase", Data: branchOutputData("out")},
		{Seq: 2, Type: RunEventNodeOutput, NodeID: "fmn_b", Data: branchOutputData("port_fmn_b_out")},
		{Seq: 3, Type: RunEventNodeOutput, NodeID: "fmn_c", Data: branchOutputData("port_fmn_c_out")},
		{Seq: 4, Type: RunEventNodeOutput, NodeID: "fmn_a", Data: branchOutputData("port_fmn_a_out")},
		{Seq: 5, Type: RunEventGateEvaluating, NodeID: "gate_review", GateID: "gate_review"},
		{Seq: 6, Type: RunEventGateVerdict, NodeID: "gate_review", GateID: "gate_review", Actor: "human:operator",
			Data: map[string]any{"verdict": "fail", "routePort": "fail", "routedEdges": []string{"edge_gate_review_fail"}, "reason": "no"}},
	}
	state := runPaths(board, events)
	want := []endedPath{
		{EndID: "end_done", Outcome: EndOutcomeDone, Title: "Done", Seq: 3},
		{EndID: "end_rejected", Outcome: EndOutcomeRejected, Title: "Rejected", GateID: "gate_review", Reason: "no", Actor: "human:operator", Seq: 6},
	}
	if len(state.owed) != 0 || !reflect.DeepEqual(state.ended, want) {
		t.Fatalf("paths = %+v, want nothing owed and ended %+v", state, want)
	}
	if rejected := rejectedRunPath(board, events); rejected == nil || rejected.GateID != "gate_review" {
		t.Fatalf("rejected path = %+v", rejected)
	}
	if rejected := rejectedRunPath(board, events[:5]); rejected != nil {
		t.Fatalf("rejected path before the verdict = %+v", rejected)
	}
}
