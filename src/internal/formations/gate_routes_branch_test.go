package formations

import (
	"reflect"
	"testing"
)

// branchOutputData is a node_output payload on one port, as the engine
// records it; deliveries follow the payload's ports.
func branchOutputData(port string) map[string]any {
	return formationOutputEventData(FormationExecutionResult{Status: "done", Text: "output", Outputs: map[string]FormationOutputPayload{port: {Text: "output"}}})
}

// Approving a gate whose pass ends its path at an End node ends the run only
// when nothing else can still run; with another branch still to run, the path
// ends and the run goes on (form-n7u.7 review, form-o7p.10).
func TestHumanGateRoutesOnABranchingBoardEndOnlyWhenNothingElseCanRun(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), branchingGateBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.StartRun("session-search", RunStartRequest{
		MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev, Personas: personas,
		Limits: RunLimits{MaxDispatch: 10, MaxAttempts: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := store.ReadRunBoard(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []RunEvent{
		{Type: RunEventNodeStarted, NodeID: "mis_showcase", Data: map[string]any{"nodeKind": "mission"}},
		{Type: RunEventNodeOutput, NodeID: "mis_showcase", Data: branchOutputData("out")},
		{Type: RunEventNodeStarted, NodeID: "fmn_a", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeOutput, NodeID: "fmn_a", Data: branchOutputData("port_fmn_a_out")},
		{Type: RunEventGateEvaluating, NodeID: "gate_review", GateID: "gate_review"},
		{Type: RunEventHumanInputRequested, NodeID: "gate_review", GateID: "gate_review"},
	} {
		if err := store.AppendRunEvent(started.RunID, event); err != nil {
			t.Fatalf("append %s: %v", event.Type, err)
		}
	}
	events, err := store.ReadRunEvents(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	doneTarget := []GateRouteTarget{{NodeID: "end_done", Title: "Done", Kind: "end", Outcome: EndOutcomeDone}}
	approve := HumanGateRoutes(frozen, events, "gate_review")[0]
	if approve.EndsRun || approve.RunFails || !reflect.DeepEqual(approve.Targets, doneTarget) {
		t.Fatalf("approve with B and C still to run = %+v, want the path to end done while the run goes on", approve)
	}
	if reject := HumanGateRoutes(frozen, events, "gate_review")[1]; reject.EndsRun || reject.Targets[0].Outcome != EndOutcomeRejected {
		t.Fatalf("reject with B and C still to run = %+v", reject)
	}

	// With B and C done, approving is the end of the run.
	done := append([]RunEvent{}, events...)
	for _, event := range []RunEvent{
		{Type: RunEventNodeStarted, NodeID: "fmn_b", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeOutput, NodeID: "fmn_b", Data: branchOutputData("port_fmn_b_out")},
		{Type: RunEventNodeStarted, NodeID: "fmn_c", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeOutput, NodeID: "fmn_c", Data: branchOutputData("port_fmn_c_out")},
	} {
		event.Seq = len(done) + 1
		done = append(done, event)
	}
	approve = HumanGateRoutes(frozen, done, "gate_review")[0]
	if !approve.EndsRun || approve.RunFails {
		t.Fatalf("approve with every branch done = %+v, want it to end the run", approve)
	}
	if reject := HumanGateRoutes(frozen, done, "gate_review")[1]; !reject.EndsRun || !reject.RunFails {
		t.Fatalf("reject with every branch done = %+v, want it to end and fail the run", reject)
	}

	// Another gate still waiting for the operator keeps the run open.
	open := append([]RunEvent{}, done...)
	open = append(open, RunEvent{Seq: len(open) + 1, Type: RunEventHumanInputRequested, NodeID: "gate_other", GateID: "gate_other"})
	if approve := HumanGateRoutes(frozen, open, "gate_review")[0]; approve.EndsRun {
		t.Fatalf("approve with another gate open = %+v", approve)
	}
}
