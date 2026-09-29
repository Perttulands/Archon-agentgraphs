package formations

import (
	"reflect"
	"testing"
)

// The runs-gate board of the UX pass: Draft feeds a human gate whose pass goes
// to Publish and whose fail goes back to Draft.
func gateRoutesBoard() *BoardDocument {
	return &BoardDocument{
		Missions:   []MissionNode{{ID: "mis_note", Title: "Ship a note"}},
		Formations: []FormationNode{{ID: "fmn_draft", Title: "Draft"}, {ID: "fmn_publish", Title: "Publish"}},
		Gates:      []GateNode{{ID: "gate_review", Title: "Operator review", Kinds: []string{"human"}}},
		Connections: []BoardConnection{
			{ID: "edge_start", From: "mis_note:out", To: "fmn_draft:in"},
			{ID: "edge_gate", From: "fmn_draft:out", To: "gate_review:in"},
			{ID: "edge_pass", From: "gate_review:pass", To: "fmn_publish:in"},
			{ID: "edge_back", From: "gate_review:fail", To: "fmn_draft:in"},
		},
	}
}

func draftAttempts(maxDispatch float64, attempts int) []RunEvent {
	events := []RunEvent{{Seq: 1, Type: RunEventStarted, MissionID: "mis_note", Data: map[string]any{"limits": map[string]any{"maxAttempts": float64(3), "maxDispatch": maxDispatch}}}}
	for attempt := 1; attempt <= attempts; attempt++ {
		events = append(events,
			RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_draft", Attempt: attempt, Data: map[string]any{"nodeKind": "formation"}},
			RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_draft"},
			RunEvent{Type: RunEventHumanInputRequested, GateID: "gate_review", NodeID: "gate_review"},
		)
		if attempt < attempts {
			events = append(events, RunEvent{Type: RunEventGateVerdict, GateID: "gate_review", NodeID: "gate_review", Data: map[string]any{"verdict": "fail", "routePort": "fail"}})
		}
	}
	for i := range events {
		events[i].Seq = i + 1
	}
	return events
}

func TestHumanGateRoutesNameDestinationsAndTheLastAttempt(t *testing.T) {
	board := gateRoutesBoard()
	cases := []struct {
		name        string
		events      []RunEvent
		sendBack    GateRouteTarget
		limit       *RunLimitReached
		dispatches  int
		maxDispatch int
	}{
		{name: "first answer", events: draftAttempts(20, 1), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 2, MaxAttempts: 3}, dispatches: 1, maxDispatch: 20},
		{name: "last attempt", events: draftAttempts(20, 2), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 3, MaxAttempts: 3}, dispatches: 2, maxDispatch: 20},
		{name: "attempts spent", events: draftAttempts(20, 3), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 4, MaxAttempts: 3},
			limit: &RunLimitReached{Kind: RunLimitAttempts, NodeID: "fmn_draft", Used: 3, Max: 3}, dispatches: 3, maxDispatch: 20},
		{name: "dispatches spent", events: draftAttempts(2, 2), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 3, MaxAttempts: 3},
			limit: &RunLimitReached{Kind: RunLimitDispatches, Used: 2, Max: 2}, dispatches: 2, maxDispatch: 2},
	}
	for _, tc := range cases {
		routes := HumanGateRoutes(board, tc.events, "gate_review")
		if len(routes) != 2 || routes[0].Verdict != "pass" || routes[1].Verdict != "fail" {
			t.Fatalf("%s: routes = %+v", tc.name, routes)
		}
		approve, sendBack := routes[0], routes[1]
		if approve.EndsRun || approve.Unwired || !reflect.DeepEqual(approve.Targets, []GateRouteTarget{{NodeID: "fmn_publish", Title: "Publish", Kind: "formation", Attempt: 1, MaxAttempts: 3}}) {
			t.Fatalf("%s: approve = %+v", tc.name, approve)
		}
		if !reflect.DeepEqual(sendBack.Targets, []GateRouteTarget{tc.sendBack}) || !reflect.DeepEqual(sendBack.Limit, tc.limit) {
			t.Fatalf("%s: send back = %+v limit %+v", tc.name, sendBack, sendBack.Limit)
		}
		if sendBack.Dispatches == nil || sendBack.Dispatches.Used != tc.dispatches || sendBack.Dispatches.Max != tc.maxDispatch {
			t.Fatalf("%s: dispatches = %+v", tc.name, sendBack.Dispatches)
		}
	}
}

// The panel uses the engine's effective attempt limit: a run that set none
// gets one attempt per node, so a send-back after the first blocks, as the
// engine will. A run without a dispatch limit reports no dispatch use.
func TestHumanGateRoutesUseTheEnginesAttemptLimit(t *testing.T) {
	events := draftAttempts(0, 1)
	events[0].Data = map[string]any{"limits": map[string]any{}}
	sendBack := HumanGateRoutes(gateRoutesBoard(), events, "gate_review")[1]
	if sendBack.Dispatches != nil || !reflect.DeepEqual(sendBack.Targets, []GateRouteTarget{{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 2, MaxAttempts: maxAttempts(RunLimits{})}}) ||
		!reflect.DeepEqual(sendBack.Limit, &RunLimitReached{Kind: RunLimitAttempts, NodeID: "fmn_draft", Used: 1, Max: 1}) {
		t.Fatalf("send back without limits = %+v limit %+v", sendBack, sendBack.Limit)
	}
}

// A join receives the approval and waits for its other input; a judge gate
// needs a dispatch for each judge.
func TestHumanGateRoutesNameAJoinThatWaitsAndCountJudges(t *testing.T) {
	board := gateRoutesBoard()
	board.Formations = append(board.Formations, FormationNode{ID: "fmn_facts", Title: "Facts"}, FormationNode{ID: "fmn_judge", Title: "Judge"})
	for i := range board.Formations {
		if board.Formations[i].ID == "fmn_publish" {
			board.Formations[i].Inputs = []FormationPort{{ID: "in", Label: "Draft"}, {ID: "facts", Label: "Facts"}}
		}
	}
	board.Gates = append(board.Gates, GateNode{ID: "gate_judged", Title: "Judged", Kinds: []string{"judge"}})
	board.Connections = append(board.Connections,
		BoardConnection{ID: "edge_facts", From: "fmn_facts:out", To: "fmn_publish:facts"},
		BoardConnection{ID: "edge_to_judged", From: "gate_review:pass", To: "gate_judged:in"},
		BoardConnection{ID: "edge_judge_send", From: "gate_judged:judge", To: "fmn_judge:in"},
		BoardConnection{ID: "edge_judge_back", From: "fmn_judge:out", To: "gate_judged:judge"},
	)
	approve := HumanGateRoutes(board, draftAttempts(20, 1), "gate_review")[0]
	if len(approve.Targets) != 2 || !approve.Targets[0].WaitsForInputs || approve.DispatchesNeeded != 2 {
		t.Fatalf("approve = %+v", approve)
	}
	facts := draftAttempts(20, 1)
	facts = append(facts, RunEvent{Seq: len(facts) + 1, Type: RunEventNodeOutput, NodeID: "fmn_facts"})
	if approve := HumanGateRoutes(board, facts, "gate_review")[0]; approve.Targets[0].WaitsForInputs {
		t.Fatalf("approve once Facts delivered = %+v", approve)
	}
}

func TestHumanGateRoutesSayWhenApprovingEndsTheRun(t *testing.T) {
	board := gateRoutesBoard()
	board.Connections = board.Connections[:2]
	routes := HumanGateRoutes(board, draftAttempts(20, 1), "gate_review")
	if !routes[0].EndsRun || len(routes[0].Targets) != 0 || routes[0].Dispatches != nil {
		t.Fatalf("approve = %+v, want it to end the run", routes[0])
	}
	if !routes[1].Unwired || routes[1].EndsRun {
		t.Fatalf("send back = %+v, want unwired", routes[1])
	}
}
