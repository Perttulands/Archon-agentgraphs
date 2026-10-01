package formations

import (
	"reflect"
	"testing"
	"time"
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

func draftAttempts(attempts int) []RunEvent {
	events := []RunEvent{{Seq: 1, Type: RunEventStarted, MissionID: "mis_note"}}
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

// draftCardUse is Draft's card use as a route reports it.
func draftCardUse(used, max, granted int) *RunLimitReached {
	return &RunLimitReached{Kind: "rounds", LimitID: "lim_draft", NodeID: "fmn_draft", Used: used, Max: max, Granted: granted}
}

// limitedGateRoutesBoard caps Draft at draftRounds and the mission at
// missionRounds; a zero value leaves that card out.
func limitedGateRoutesBoard(draftRounds, missionRounds int) *BoardDocument {
	board := gateRoutesBoard()
	if draftRounds > 0 {
		board.Limits = append(board.Limits, LimitNode{ID: "lim_draft", Title: "Draft cap", Target: "fmn_draft", Rounds: &draftRounds})
	}
	if missionRounds > 0 {
		board.Limits = append(board.Limits, LimitNode{ID: "lim_mission", Title: "Mission cap", Target: "mis_note", Rounds: &missionRounds})
	}
	return board
}

func granted(events []RunEvent, limitID string) []RunEvent {
	events = append(append([]RunEvent{}, events...), RunEvent{Seq: len(events) + 1, Type: RunEventResumed, Data: map[string]any{"grant": map[string]any{"limitId": limitID, "kind": "rounds", "amount": float64(1)}}})
	return events
}

func TestHumanGateRoutesNameDestinationsAndTheLastRound(t *testing.T) {
	cases := []struct {
		name          string
		board         *BoardDocument
		events        []RunEvent
		sendBack      GateRouteTarget
		limit         *RunLimitReached
		missionRounds *RunLimitReached
	}{
		{name: "first answer", board: limitedGateRoutesBoard(3, 20), events: draftAttempts(1), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 2, Rounds: draftCardUse(1, 3, 0)},
			missionRounds: &RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "mis_note", Used: 1, Max: 20}},
		{name: "last round", board: limitedGateRoutesBoard(3, 20), events: draftAttempts(2), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 3, Rounds: draftCardUse(2, 3, 0)},
			missionRounds: &RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "mis_note", Used: 2, Max: 20}},
		{name: "step rounds spent", board: limitedGateRoutesBoard(3, 20), events: draftAttempts(3), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 4, Rounds: draftCardUse(3, 3, 0)},
			limit:         &RunLimitReached{Kind: "rounds", LimitID: "lim_draft", NodeID: "fmn_draft", Used: 3, Max: 3},
			missionRounds: &RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "mis_note", Used: 3, Max: 20}},
		{name: "a grant gives one more round", board: limitedGateRoutesBoard(3, 0), events: granted(draftAttempts(3), "lim_draft"), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 4, Rounds: draftCardUse(3, 4, 1)}},
		{name: "mission rounds spent", board: limitedGateRoutesBoard(0, 2), events: draftAttempts(2), sendBack: GateRouteTarget{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: 3},
			limit:         &RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "mis_note", Used: 2, Max: 2},
			missionRounds: &RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "mis_note", Used: 2, Max: 2}},
	}
	for _, tc := range cases {
		routes := HumanGateRoutes(tc.board, tc.events, "gate_review", time.Time{})
		if len(routes) != 2 || routes[0].Verdict != "pass" || routes[1].Verdict != "fail" {
			t.Fatalf("%s: routes = %+v", tc.name, routes)
		}
		approve, sendBack := routes[0], routes[1]
		if approve.EndsRun || !reflect.DeepEqual(approve.Targets, []GateRouteTarget{{NodeID: "fmn_publish", Title: "Publish", Kind: "formation", Attempt: 1}}) {
			t.Fatalf("%s: approve = %+v", tc.name, approve)
		}
		if !reflect.DeepEqual(sendBack.Targets, []GateRouteTarget{tc.sendBack}) || !reflect.DeepEqual(sendBack.Limit, tc.limit) {
			t.Fatalf("%s: send back = %+v limit %+v", tc.name, sendBack, sendBack.Limit)
		}
		if !reflect.DeepEqual(sendBack.MissionRounds, tc.missionRounds) || (tc.missionRounds != nil) != (sendBack.RoundsNeeded == 1) {
			t.Fatalf("%s: mission rounds = %+v, needed %d", tc.name, sendBack.MissionRounds, sendBack.RoundsNeeded)
		}
	}
}

// A mission without a Limit card has no limits (archon-o7p.8): however often
// the draft was sent back, the next send-back names no rounds and no limit,
// because the engine will start the draft again.
func TestHumanGateRoutesNameNoLimitWithoutACard(t *testing.T) {
	for _, attempts := range []int{1, 2, 5, 12} {
		sendBack := HumanGateRoutes(gateRoutesBoard(), draftAttempts(attempts), "gate_review", time.Time{})[1]
		if sendBack.MissionRounds != nil || sendBack.Limit != nil || !reflect.DeepEqual(sendBack.Targets, []GateRouteTarget{{NodeID: "fmn_draft", Title: "Draft", Kind: "formation", Attempt: attempts + 1}}) {
			t.Fatalf("send back after %d attempts without limits = %+v limit %+v", attempts, sendBack, sendBack.Limit)
		}
	}
}

// The panel and the engine share one rounds rule (roundsUse and
// roundsSpentBefore): the panel names a spent limit exactly when the engine
// would refuse the start.
func TestHumanGateRoutesAgreeWithTheEnginesRoundsRule(t *testing.T) {
	for _, rounds := range []int{1, 2, 3} {
		for attempts := 1; attempts <= 4; attempts++ {
			for _, grant := range []bool{false, true} {
				board := limitedGateRoutesBoard(rounds, 0)
				events := draftAttempts(attempts)
				if grant {
					events = granted(events, "lim_draft")
				}
				sendBack := HumanGateRoutes(board, events, "gate_review", time.Time{})[1]
				draft, _ := findFormation(board.Formations, "fmn_draft")
				engineRefuses := limitSpentBefore(board, events, draft, time.Time{}) != nil
				if (sendBack.Limit != nil) != engineRefuses {
					t.Fatalf("rounds %d after %d attempts, grant %v: panel limit %+v, engine refuses %v", rounds, attempts, grant, sendBack.Limit, engineRefuses)
				}
				allowance := rounds
				if grant {
					allowance++
				}
				if engineRefuses != (attempts >= allowance) {
					t.Fatalf("rounds %d after %d attempts, grant %v: engine refuses %v", rounds, attempts, grant, engineRefuses)
				}
			}
		}
	}
}

// A join receives the approval and waits for its other input; a judge gate
// starts each of its judges, a round each under a mission card.
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
	rounds := 20
	board.Limits = []LimitNode{{ID: "lim_mission", Target: "mis_note", Rounds: &rounds}}
	approve := HumanGateRoutes(board, draftAttempts(1), "gate_review", time.Time{})[0]
	if len(approve.Targets) != 2 || !approve.Targets[0].WaitsForInputs || approve.RoundsNeeded != 2 {
		t.Fatalf("approve = %+v", approve)
	}
	facts := draftAttempts(1)
	facts = append(facts, RunEvent{Seq: len(facts) + 1, Type: RunEventNodeOutput, NodeID: "fmn_facts"})
	if approve := HumanGateRoutes(board, facts, "gate_review", time.Time{})[0]; approve.Targets[0].WaitsForInputs {
		t.Fatalf("approve once Facts delivered = %+v", approve)
	}
}

// A verdict whose routes all lead to End nodes ends its path; with nothing
// else to run it ends the run, which a rejected End fails (archon-o7p.10).
func TestHumanGateRoutesSayWhenAVerdictEndsTheRun(t *testing.T) {
	board := gateRoutesBoard()
	board.Ends = []EndNode{{ID: "end_done", Title: "Shipped", Outcome: EndOutcomeDone}, {ID: "end_rejected", Title: "Rejected", Outcome: EndOutcomeRejected}}
	board.Connections = append(board.Connections[:2],
		BoardConnection{ID: "edge_pass", From: "gate_review:pass", To: "end_done:in"},
		BoardConnection{ID: "edge_fail", From: "gate_review:fail", To: "end_rejected:in"},
	)
	routes := HumanGateRoutes(board, draftAttempts(1), "gate_review", time.Time{})
	if !routes[0].EndsRun || routes[0].RunFails || routes[0].MissionRounds != nil ||
		!reflect.DeepEqual(routes[0].Targets, []GateRouteTarget{{NodeID: "end_done", Title: "Shipped", Kind: "end", Outcome: EndOutcomeDone}}) {
		t.Fatalf("approve = %+v, want this path to end done and the run with it", routes[0])
	}
	if !routes[1].EndsRun || !routes[1].RunFails ||
		!reflect.DeepEqual(routes[1].Targets, []GateRouteTarget{{NodeID: "end_rejected", Title: "Rejected", Kind: "end", Outcome: EndOutcomeRejected}}) {
		t.Fatalf("send back = %+v, want this path to end rejected and fail the run", routes[1])
	}
}
