package formations

import (
	"reflect"
	"slices"
	"testing"
	"time"
)

func branchingBoardJudgeGate(id, judgeID string) string {
	return `
[[gate]]
id = "` + id + `"
title = "Judge review"
kinds = ["formation"]
criterion = "Judge the work"
` + branchingBoardConnection("edge_"+id+"_judge", id+":judge", judgeID+":port_"+judgeID+"_in") +
		branchingBoardConnection("edge_"+judgeID+"_verdict", judgeID+":port_"+judgeID+"_out", id+":judge")
}

// mission -> A -> human gate ending Done or Rejected, and mission -> work ->
// judge gate whose fail sends back to work and whose pass goes to ship, which
// ends Done (archon-n7u.53 review).
func pushbackAfterApproveBoardFixture() string {
	return s4MissionOnlyBoardFixture() +
		branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_work", "Work") +
		branchingBoardFormation("fmn_judge", "Judge") + branchingBoardFormation("fmn_ship", "Ship") +
		branchingBoardHumanGate("gate_review") + branchingBoardJudgeGate("gate_judge", "fmn_judge") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_review:in") +
		branchingBoardGateEnds("gate_review") +
		branchingBoardConnection("edge_ship_done", "fmn_ship:port_fmn_ship_out", "end_done:in") +
		branchingBoardConnection("edge_m_work", "mis_showcase:out", "fmn_work:port_fmn_work_in") +
		branchingBoardConnection("edge_work_judge", "fmn_work:port_fmn_work_out", "gate_judge:in") +
		branchingBoardConnection("edge_judge_fail", "gate_judge:fail", "fmn_work:port_fmn_work_in") +
		branchingBoardConnection("edge_judge_pass", "gate_judge:pass", "fmn_ship:port_fmn_ship_in")
}

// mission -> W -> human gate whose pass ends Done and whose fail goes to R,
// which ends Rejected.
func failOnlyRouteBoardFixture() string {
	return s4MissionOnlyBoardFixture() +
		branchingBoardFormation("fmn_w", "W") + branchingBoardFormation("fmn_r", "R") +
		branchingBoardHumanGate("gate_review") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_w", "mis_showcase:out", "fmn_w:port_fmn_w_in") +
		branchingBoardConnection("edge_w_gate", "fmn_w:port_fmn_w_out", "gate_review:in") +
		branchingBoardConnection("edge_gate_pass", "gate_review:pass", "end_done:in") +
		branchingBoardConnection("edge_gate_fail", "gate_review:fail", "fmn_r:port_fmn_r_in") +
		branchingBoardConnection("edge_r_rejected", "fmn_r:port_fmn_r_out", "end_rejected:in")
}

// mission -> W -> human gate ending Done or Rejected.
func linearGateBoardFixture() string {
	return s4MissionOnlyBoardFixture() + branchingBoardFormation("fmn_w", "W") + branchingBoardHumanGate("gate_review") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_w", "mis_showcase:out", "fmn_w:port_fmn_w_in") +
		branchingBoardConnection("edge_w_gate", "fmn_w:port_fmn_w_out", "gate_review:in") +
		branchingBoardGateEnds("gate_review")
}

// mission -> W -> human gate whose pass ends Done and whose fail sends W back.
func sendBackLoopBoardFixture() string {
	return s4MissionOnlyBoardFixture() + branchingBoardFormation("fmn_w", "W") + branchingBoardHumanGate("gate_review") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_w", "mis_showcase:out", "fmn_w:port_fmn_w_in") +
		branchingBoardConnection("edge_w_gate", "fmn_w:port_fmn_w_out", "gate_review:in") +
		branchingBoardConnection("edge_gate_pass", "gate_review:pass", "end_done:in") +
		branchingBoardConnection("edge_back", "gate_review:fail", "fmn_w:port_fmn_w_in")
}

// scriptedBranchExecutor answers each node's nth call with texts[node][n],
// or the fake executor's default text past the script.
type scriptedBranchExecutor struct {
	fakeRunExecutor
	texts map[string][]string
	seen  map[string]int
}

func (e *scriptedBranchExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	if e.seen == nil {
		e.seen = map[string]int{}
	}
	n := e.seen[req.NodeID]
	e.seen[req.NodeID]++
	script := e.texts[req.NodeID]
	if n >= len(script) {
		return e.fakeRunExecutor.ExecuteFormation(req)
	}
	e.calls = append(e.calls, req)
	return FormationExecutionResult{Status: "done", Text: script[n], Outputs: payloadsForFormationOutputs(req.Formation, script[n], "refs/"+req.NodeID+".md")}, nil
}

func judgeFailsThenPasses() *scriptedBranchExecutor {
	return &scriptedBranchExecutor{texts: map[string][]string{
		"fmn_judge": {judgeBlock("fail", "add the missing test"), judgeBlock("pass", "tests added")},
	}}
}

func approveAndContinue(t *testing.T, engine *RunEngine, runID, gateID string) *RunStatusProjection {
	t.Helper()
	if _, err := engine.RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: gateID, Verdict: "pass", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	status, err := engine.ContinueRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	return status
}

// A send-back a judge gate routes on another branch runs while the human gate
// waits (archon-o7p.11): the work runs again with the judge's feedback and the
// pass after it reaches ship, and approving then ends the run.
func TestAJudgeSendBackOnAnotherBranchRunsWhileAGateWaits(t *testing.T) {
	for _, restart := range []bool{false, true} {
		t.Run(map[bool]string{false: "same engine", true: "restarted engine"}[restart], func(t *testing.T) {
			executor := judgeFailsThenPasses()
			store, personas, engine, status := startBranchingRun(t, pushbackAfterApproveBoardFixture(), executor)
			if got, want := executor.nodeIDs(), []string{"fmn_a", "fmn_work", "fmn_judge", "fmn_work", "fmn_judge", "fmn_ship"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("nodes before the verdict = %v, want %v", got, want)
			}
			if feedback := executor.calls[3].Inputs[0].Feedback; feedback == nil || feedback.Verdict != "fail" || feedback.Reason != "add the missing test" {
				t.Fatalf("second work attempt feedback = %+v", feedback)
			}
			if restart {
				engine = NewRunEngine(store, personas, executor)
			}
			executor.calls = nil
			status = approveAndContinue(t, engine, status.RunID, "gate_review")
			if got := executor.nodeIDs(); len(got) != 0 {
				t.Fatalf("nodes after approval = %v, want none", got)
			}
			if status.Status != RunStatusSucceeded {
				t.Fatalf("status = %+v, want succeeded", status)
			}
			requireSucceededAfterOutputs(t, store, status.RunID, "fmn_a", "fmn_work", "fmn_ship")
		})
	}
}

// The same rule answers the gate's routes ("approving ends the run") and the
// engine (it succeeds straight after the approval, running nothing more).
func TestApprovalRoutesAndTheEngineAgreeWhetherTheRunEnds(t *testing.T) {
	linear, loop := linearGateBoardFixture(), sendBackLoopBoardFixture()
	for _, tc := range []struct {
		name     string
		board    string
		gate     string
		endsRun  bool
		executor func() FormationExecutor
	}{
		{name: "linear", board: linear, gate: "gate_review", endsRun: true},
		{name: "send-back loop, pass ends done", board: loop, gate: "gate_review", endsRun: true},
		{name: "fail-only route to R", board: failOnlyRouteBoardFixture(), gate: "gate_review", endsRun: true},
		// The other branches ran while the gate waited (archon-o7p.11).
		{name: "other branch B then C", board: branchingGateBoardFixture(), gate: "gate_review", endsRun: true},
		{name: "other branch with a judge send-back", board: pushbackAfterApproveBoardFixture(), gate: "gate_review", endsRun: true,
			executor: func() FormationExecutor { return judgeFailsThenPasses() }},
		{name: "another terminal gate also waiting", board: twoTerminalGatesBoardFixture(), gate: "gate_one", endsRun: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var executor FormationExecutor = &fakeRunExecutor{}
			if tc.executor != nil {
				executor = tc.executor()
			}
			store, _, engine, status := startBranchingRun(t, tc.board, executor)
			if got := pendingHumanGates(t, store, status.RunID); !slices.Contains(got, tc.gate) {
				t.Fatalf("pending gates = %v, want %s among them", got, tc.gate)
			}
			board, err := store.ReadRunBoard(status.RunID)
			if err != nil {
				t.Fatal(err)
			}
			events, err := store.ReadRunEvents(status.RunID)
			if err != nil {
				t.Fatal(err)
			}
			approve := HumanGateRoutes(board, events, tc.gate, time.Time{})[0]
			if approve.EndsRun != tc.endsRun || approve.RunFails {
				t.Fatalf("routes say approve = %+v, want endsRun %v", approve, tc.endsRun)
			}
			before := len(events)
			status = approveAndContinue(t, engine, status.RunID, tc.gate)
			after, err := store.ReadRunEvents(status.RunID)
			if err != nil {
				t.Fatal(err)
			}
			ran := 0
			for _, event := range after[before:] {
				if event.Type == RunEventNodeStarted {
					ran++
				}
			}
			engineEnded := status.Status == RunStatusSucceeded && ran == 0
			if engineEnded != tc.endsRun {
				t.Fatalf("engine after approval: %s, %d formations ran; routes said endsRun %v: %s", status.Status, ran, tc.endsRun, eventTypeTrail(after[before:]))
			}
		})
	}
}

// unfinishedRunWork is the shared rule. Read straight from a ledger: a
// delivery not yet acted on, a send-back not yet serviced, and an open node
// are each unfinished; except leaves out the gate being answered, and a
// judge's output is gate evidence rather than a delivery.
func TestUnfinishedRunWorkReadsDeliveriesAndOpenNodesFromTheLedger(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), pushbackAfterApproveBoardFixture())
	source, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: source.ETag, ExpectedBoardRev: source.Rev, Personas: personas})
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.ReadRunBoard(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	verdict := func(gate, port string, routes ...string) RunEvent {
		return RunEvent{Type: RunEventGateVerdict, GateID: gate, NodeID: gate, Data: map[string]any{"verdict": port, "routePort": port, "routedEdges": routes}}
	}
	var events []RunEvent
	for _, step := range []struct {
		event  RunEvent
		except string
		want   []string
	}{
		{RunEvent{Type: RunEventStarted, MissionID: "mis_showcase"}, "", []string{}},
		{RunEvent{Type: RunEventNodeStarted, NodeID: "mis_showcase"}, "", []string{"mis_showcase"}},
		{RunEvent{Type: RunEventNodeOutput, NodeID: "mis_showcase", Data: branchOutputData("out")}, "", []string{"fmn_a", "fmn_work"}},
		{RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_a", Attempt: 1}, "", []string{"fmn_a", "fmn_work"}},
		{RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_a", Data: branchOutputData("port_fmn_a_out")}, "", []string{"fmn_work", "gate_review"}},
		{RunEvent{Type: RunEventGateEvaluating, GateID: "gate_review", NodeID: "gate_review"}, "", []string{"fmn_work", "gate_review"}},
		{RunEvent{Type: RunEventHumanInputRequested, GateID: "gate_review", NodeID: "gate_review"}, "gate_review", []string{"fmn_work"}},
		// A recorded verdict holds its gate open until the run routes it.
		{RunEvent{Type: RunEventHumanVerdictRecorded, GateID: "gate_review", NodeID: "gate_review"}, "", []string{"fmn_work", "gate_review"}},
		{verdict("gate_review", "pass"), "", []string{"fmn_work"}},
		{RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: 1}, "", []string{"fmn_work"}},
		{RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_work", Data: branchOutputData("port_fmn_work_out")}, "", []string{"gate_judge"}},
		{RunEvent{Type: RunEventGateEvaluating, GateID: "gate_judge", NodeID: "gate_judge"}, "", []string{"gate_judge"}},
		{RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_judge", Attempt: 1}, "", []string{"gate_judge", "fmn_judge"}},
		{RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_judge", Data: branchOutputData("port_fmn_judge_out")}, "", []string{"gate_judge"}},
		{verdict("gate_judge", "fail", "edge_judge_fail"), "", []string{"fmn_work"}},
		{RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: 2}, "", []string{"fmn_work"}},
		{RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_work", Data: branchOutputData("port_fmn_work_out")}, "", []string{"gate_judge"}},
		{RunEvent{Type: RunEventGateEvaluating, GateID: "gate_judge", NodeID: "gate_judge"}, "", []string{"gate_judge"}},
		{verdict("gate_judge", "pass", "edge_judge_pass"), "", []string{"fmn_ship"}},
		{RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_ship", Attempt: 1}, "", []string{"fmn_ship"}},
		{RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_ship", Data: branchOutputData("port_fmn_ship_out")}, "", []string{}},
	} {
		step.event.Seq = len(events) + 1
		events = append(events, step.event)
		if got := unfinishedRunWork(board, events, step.except); !reflect.DeepEqual(got, step.want) {
			t.Fatalf("after %s (except %q): unfinished = %v, want %v", eventTypeTrail(events[len(events)-1:]), step.except, got, step.want)
		}
	}
}

// A resume reached while a human request is still open (here an unrelated
// resumable block after the request, resumed at engine level) never records
// success, and blocks nothing either: the open gate holds up only its own
// path, so the run waits for the verdict (archon-o7p.11).
func TestAResumeWithAHumanRequestStillOpenWaitsForTheVerdict(t *testing.T) {
	linear := linearGateBoardFixture()
	executor := &fakeRunExecutor{}
	store, _, engine, status := startBranchingRun(t, linear, executor)
	if err := store.AppendRunEvent(status.RunID, RunEvent{Type: RunEventBlocked, Data: map[string]any{"reason": "other failure", "resumeAllowed": true}}); err != nil {
		t.Fatal(err)
	}
	executor.calls = nil
	status, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "resume the unrelated block"})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusRunning || len(executor.calls) != 0 {
		t.Fatalf("status = %s after %v: %s", status.Status, executor.nodeIDs(), eventTypeTrail(events))
	}
	if last := events[len(events)-1]; last.Type != RunEventResumed {
		t.Fatalf("want the resume last, waiting for the verdict: %s", eventTypeTrail(events))
	}
	if got := pendingHumanGates(t, store, status.RunID); !reflect.DeepEqual(got, []string{"gate_review"}) {
		t.Fatalf("pending gates = %v, want gate_review", got)
	}
}
