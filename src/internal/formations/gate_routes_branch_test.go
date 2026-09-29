package formations

import (
	"testing"
)

// mission -> A -> terminal human gate, and mission -> B -> C.
func branchingGateBoardFixture() string {
	formation := func(id, title string) string {
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
controller = true
`
	}
	connection := func(id, from, to string) string {
		return `
[[connection]]
id = "` + id + `"
from = "` + from + `"
to = "` + to + `"
`
	}
	return s4MissionOnlyBoardFixture() + formation("fmn_a", "A") + formation("fmn_b", "B") + formation("fmn_c", "C") + `
[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "Good enough"
` + connection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		connection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_review:in") +
		connection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in") +
		connection("edge_b_c", "fmn_b:port_fmn_b_out", "fmn_c:port_fmn_c_in")
}

// Approving a gate with nothing downstream ends the run only when nothing
// else can still run; with another branch still to run, nothing follows the
// gate and the run goes on (form-n7u.7 review).
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
		{Type: RunEventNodeOutput, NodeID: "mis_showcase"},
		{Type: RunEventNodeStarted, NodeID: "fmn_a", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeOutput, NodeID: "fmn_a"},
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
	approve := HumanGateRoutes(frozen, events, "gate_review")[0]
	if approve.EndsRun || !approve.NothingFollows || len(approve.Targets) != 0 {
		t.Fatalf("approve with B and C still to run = %+v, want nothing follows", approve)
	}

	// With B and C done, approving is the end of the run.
	done := append([]RunEvent{}, events...)
	for _, event := range []RunEvent{
		{Type: RunEventNodeStarted, NodeID: "fmn_b", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeOutput, NodeID: "fmn_b"},
		{Type: RunEventNodeStarted, NodeID: "fmn_c", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeOutput, NodeID: "fmn_c"},
	} {
		event.Seq = len(done) + 1
		done = append(done, event)
	}
	approve = HumanGateRoutes(frozen, done, "gate_review")[0]
	if !approve.EndsRun || approve.NothingFollows {
		t.Fatalf("approve with every branch done = %+v, want it to end the run", approve)
	}

	// Another gate still waiting for the operator keeps the run open.
	open := append([]RunEvent{}, done...)
	open = append(open, RunEvent{Seq: len(open) + 1, Type: RunEventHumanInputRequested, NodeID: "gate_other", GateID: "gate_other"})
	if approve := HumanGateRoutes(frozen, open, "gate_review")[0]; approve.EndsRun {
		t.Fatalf("approve with another gate open = %+v", approve)
	}
}
