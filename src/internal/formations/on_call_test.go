package formations

import (
	"fmt"
	"strings"
	"testing"
)

// onCallBoard builds a board from compact connection specs. Formations are
// solo unless named in peer or orchestrated; gates take their kinds from gates.
func onCallBoard(channel string, gates map[string][]string, typed map[string]string, connections ...string) *BoardDocument {
	board := &BoardDocument{Missions: []MissionNode{{ID: "mis", HumanChannel: channel}}}
	formations := map[string]bool{}
	addFormation := func(id string) {
		if formations[id] || gates[id] != nil || id == "mis" {
			return
		}
		formations[id] = true
		formation := FormationNode{ID: id, Type: FormationTypeSolo, Slots: []FormationSlot{{ID: id + "_a", Controller: true}}}
		switch typed[id] {
		case FormationTypePeer:
			formation.Type = FormationTypePeer
			formation.Slots = []FormationSlot{{ID: id + "_a"}, {ID: id + "_b"}}
		case FormationTypeOrchestrated:
			formation.Type = FormationTypeOrchestrated
			formation.Slots = []FormationSlot{{ID: id + "_lead", Controller: true}, {ID: id + "_worker"}}
		}
		board.Formations = append(board.Formations, formation)
	}
	for id, kinds := range gates {
		board.Gates = append(board.Gates, GateNode{ID: id, Kinds: kinds})
	}
	for i, spec := range connections {
		from, to, _ := strings.Cut(spec, "->")
		board.Connections = append(board.Connections, BoardConnection{ID: fmt.Sprintf("edge_%d", i), From: from, To: to})
		addFormation(strings.Split(from, ":")[0])
		addFormation(strings.Split(to, ":")[0])
	}
	return board
}

func TestFormationsKeepSeatsOnlyWhenTheirWorkReachesAHumanGateThroughGates(t *testing.T) {
	board := onCallBoard(HumanChannelSession,
		map[string][]string{"g_human": {"human"}, "g_judge": {"formation"}, "g_signoff": {"human"}, "g_code": {"code"}, "g_code2": {"code"}},
		nil,
		"mis:out->work:in", "work:out->g_human:in", "g_human:pass->after:in",
		"draft:out->g_judge:in", "g_judge:judge->critic:in", "critic:out->g_judge:judge", "g_judge:pass->g_signoff:in",
		"build:out->g_code:in", "g_code:fail->g_code2:in", "g_code2:fail->g_signoff:in",
		"after:out->next:in",
	)
	for id, want := range map[string]bool{"work": true, "draft": true, "build": true, "critic": false, "after": false, "next": false} {
		if got := FormationKeepsSeatsOnCall(board, id); got != want {
			t.Fatalf("%s keeps seats = %v, want %v", id, got, want)
		}
	}
	if RunHumanChannel(board, []RunEvent{{MissionID: "mis"}}) != HumanChannelSession || RunHumanChannel(onCallBoard("", nil, nil, "mis:out->w:in"), []RunEvent{{MissionID: "mis"}}) != HumanChannelNotify {
		t.Fatal("run human channel should follow the frozen mission")
	}
}

func inputRef(from string) map[string]any { return map[string]any{"fromNodeId": from} }

func TestAskingFormationFollowsPassesAndFailRoutesBackToWork(t *testing.T) {
	board := onCallBoard(HumanChannelSession, map[string][]string{"g_code": {"code"}, "g_human": {"human"}, "g_signoff": {"human"}}, nil,
		"mis:out->work:in", "work:out->g_code:in", "g_code:fail->g_human:in", "g_code:pass->g_signoff:in", "mis:out->g_mission:in")
	events := []RunEvent{
		{Seq: 1, Type: RunEventStarted, MissionID: "mis"},
		{Seq: 2, Type: RunEventGateEvaluating, GateID: "g_code", Data: map[string]any{"inputRef": inputRef("work")}},
		{Seq: 3, Type: RunEventGateEvaluating, GateID: "g_human", Data: map[string]any{"inputRef": inputRef("g_code")}},
		{Seq: 4, Type: RunEventHumanInputRequested, GateID: "g_human", Data: map[string]any{"inputRef": inputRef("g_code")}},
		{Seq: 5, Type: RunEventHumanInputRequested, GateID: "g_signoff", Data: map[string]any{"inputRef": inputRef("work")}},
		{Seq: 6, Type: RunEventHumanInputRequested, GateID: "g_mission", Data: map[string]any{"inputRef": inputRef("mis")}},
	}
	for _, c := range []struct {
		request int
		want    string
	}{{4, "work"}, {5, "work"}, {6, ""}} {
		if got := AskingFormation(board, events, events[c.request-1]); got != c.want {
			t.Fatalf("request %d asker = %q, want %q", c.request, got, c.want)
		}
	}
}

func seatEvents(seq int, node, slot, session string) RunEvent {
	return RunEvent{Seq: seq, Type: RunEventSeatCreated, NodeID: node, SlotID: slot, Data: map[string]any{"sessionName": session, "sessionId": fmt.Sprintf("$%d", seq), "paneId": fmt.Sprintf("%%%d", seq), "harness": "claude-code"}}
}

func cleanup(seq int, node, slot, session, outcome string) RunEvent {
	return RunEvent{Seq: seq, Type: RunEventSeatCleanup, NodeID: node, SlotID: slot, Data: map[string]any{"sessionName": session, "outcome": outcome}}
}

func TestKeptSeatsFollowCleanupsAcrossAttemptsThatReuseSessionNames(t *testing.T) {
	events := []RunEvent{
		{Seq: 1, Type: RunEventStarted},
		seatEvents(2, "work", "work_a", "form-run-work_a"),
		cleanup(3, "work", "work_a", "form-run-work_a", SeatOutcomeKeptOnCall),
		seatEvents(4, "after", "after_a", "form-run-after_a"),
		cleanup(5, "after", "after_a", "form-run-after_a", SeatOutcomeEnded),
		cleanup(6, "work", "work_a", "form-run-work_a", SeatOutcomeEnded),
		seatEvents(7, "work", "work_a", "form-run-work_a"),
		cleanup(8, "work", "work_a", "form-run-work_a", SeatOutcomeKeptOnCall),
	}
	seats := KeptSeats(events)
	if len(seats) != 1 || seats[0].CreatedSeq != 7 || seats[0].KeptSeq != 8 || seats[0].SessionID != "$7" || seats[0].PaneID != "%7" || seats[0].Harness != "claude-code" {
		t.Fatalf("kept seats = %+v", seats)
	}
	if got := KeptSeats(events[:6]); len(got) != 0 {
		t.Fatalf("ended kept seat still kept: %+v", got)
	}
}

func request(seq int, gate, from string) RunEvent {
	return RunEvent{Seq: seq, Type: RunEventHumanInputRequested, GateID: gate, NodeID: gate, Data: map[string]any{"inputRef": inputRef(from)}}
}

func delivered(seq, requested, createdSeq int, gate, node, slot string) RunEvent {
	return RunEvent{Seq: seq, Type: RunEventHumanAskDelivered, GateID: gate, NodeID: node, SlotID: slot, Data: map[string]any{"requestedSeq": requested, "seatCreatedSeq": createdSeq}}
}

func present(gone ...int) SeatProbe {
	return func(seat KeptSeat) (bool, string) {
		for _, seq := range gone {
			if seat.CreatedSeq == seq {
				return false, SeatOutcomeGone
			}
		}
		return true, ""
	}
}

func TestPlanOnCallDeliversToEveryPeerSeatAndOnlyTheOrchestratedController(t *testing.T) {
	board := onCallBoard(HumanChannelSession, map[string][]string{"g_peer": {"human"}, "g_lead": {"human"}},
		map[string]string{"peers": FormationTypePeer, "team": FormationTypeOrchestrated},
		"mis:out->peers:in", "peers:out->g_peer:in", "mis:out->team:in", "team:out->g_lead:in")
	events := []RunEvent{
		{Seq: 1, Type: RunEventStarted, MissionID: "mis", RunID: "run"},
		seatEvents(2, "peers", "peers_a", "s-a"), seatEvents(3, "peers", "peers_b", "s-b"),
		cleanup(4, "peers", "peers_a", "s-a", SeatOutcomeKeptOnCall), cleanup(5, "peers", "peers_b", "s-b", SeatOutcomeKeptOnCall),
		request(6, "g_peer", "peers"),
		seatEvents(7, "team", "team_lead", "s-lead"), cleanup(8, "team", "team_lead", "s-lead", SeatOutcomeKeptOnCall),
		request(9, "g_lead", "team"),
		delivered(10, 6, 2, "g_peer", "peers", "peers_a"),
	}
	plan := PlanOnCall(board, events, true, present())
	var got []string
	for _, delivery := range plan.Deliveries {
		got = append(got, fmt.Sprintf("%d:%s:%s", delivery.Request.Seq, delivery.AskingNodeID, delivery.Seat.SlotID))
	}
	if strings.Join(got, ",") != "6:peers:peers_b,9:team:team_lead" || len(plan.Fallbacks) != 0 || len(plan.Answered) != 0 || plan.OpenAsks != 2 {
		t.Fatalf("plan = %+v (deliveries %v)", plan, got)
	}
	// A notify channel, a blocked run and a final run plan nothing.
	for _, variant := range [][]RunEvent{append(events, RunEvent{Seq: 11, Type: RunEventBlocked}), append(events, RunEvent{Seq: 11, Type: RunEventCanceled})} {
		if plan := PlanOnCall(board, variant, true, present()); len(plan.Deliveries)+len(plan.Fallbacks)+len(plan.Gone)+len(plan.Answered) != 0 {
			t.Fatalf("plan for %s run = %+v", variant[len(variant)-1].Type, plan)
		}
	}
	notify := onCallBoard("", map[string][]string{"g_peer": {"human"}}, map[string]string{"peers": FormationTypePeer}, "mis:out->peers:in", "peers:out->g_peer:in")
	if plan := PlanOnCall(notify, events, true, present()); len(plan.Deliveries) != 0 {
		t.Fatalf("notify channel plan = %+v", plan)
	}
}

func TestPlanOnCallFallsBackOnceWithAReason(t *testing.T) {
	board := onCallBoard(HumanChannelSession, map[string][]string{"g": {"human"}, "g_mission": {"human"}}, nil,
		"mis:out->work:in", "work:out->g:in", "mis:out->g_mission:in")
	base := []RunEvent{{Seq: 1, Type: RunEventStarted, MissionID: "mis", RunID: "run"}, seatEvents(2, "work", "work_a", "s-a"), cleanup(3, "work", "work_a", "s-a", SeatOutcomeKeptOnCall), request(4, "g", "work")}
	codes := func(plan OnCallPlan) string {
		var codes []string
		for _, fallback := range plan.Fallbacks {
			codes = append(codes, fmt.Sprintf("%d:%s", fallback.Request.Seq, fallback.Code))
		}
		return strings.Join(codes, ",")
	}
	if got := codes(PlanOnCall(board, base, false, present())); got != "4:lab_executor" {
		t.Fatalf("lab fallback = %q", got)
	}
	if got := codes(PlanOnCall(board, append(base, request(5, "g_mission", "mis")), true, present())); got != "5:no_asking_formation" {
		t.Fatalf("mission fallback = %q", got)
	}
	gone := PlanOnCall(board, base, true, present(2))
	if got := codes(gone); got != "4:no_receivable_seat" || len(gone.Gone) != 1 || gone.Gone[0].Outcome != SeatOutcomeGone {
		t.Fatalf("no receivable seat plan = %+v", gone)
	}
	asked := append(append([]RunEvent{}, base...), delivered(5, 4, 2, "g", "work", "work_a"))
	if got := codes(PlanOnCall(board, asked, true, present(2))); got != "4:asked_seats_gone" {
		t.Fatalf("asked seats gone fallback = %q", got)
	}
	if plan := PlanOnCall(board, asked, true, present()); len(plan.Deliveries)+len(plan.Fallbacks) != 0 {
		t.Fatalf("a delivered ask with its seat present owes nothing: %+v", plan)
	}
	fellBack := append(append([]RunEvent{}, asked...), RunEvent{Seq: 6, Type: RunEventHumanAskFallback, GateID: "g", Data: map[string]any{"requestedSeq": 4, "code": AskFallbackAskedSeatsGone}})
	if got := codes(PlanOnCall(board, fellBack, true, present(2))); got != "" {
		t.Fatalf("a fallback repeats: %q", got)
	}
	if AskFallbackReason(AskFallbackAskedSeatsGone) != "every seat that received the ask is gone" {
		t.Fatal("fallback reason text")
	}
}

func TestPlanOnCallReleasesAnsweredSeatsButKeepsOnesAskedAgain(t *testing.T) {
	board := onCallBoard(HumanChannelSession, map[string][]string{"g1": {"human"}, "g2": {"human"}, "g3": {"human"}}, nil,
		"mis:out->work:in", "work:out->g1:in", "g1:pass->g2:in", "mis:out->other:in", "other:out->g3:in")
	events := []RunEvent{
		{Seq: 1, Type: RunEventStarted, MissionID: "mis", RunID: "run"},
		seatEvents(2, "work", "work_a", "s-work"), cleanup(3, "work", "work_a", "s-work", SeatOutcomeKeptOnCall),
		request(4, "g1", "work"), delivered(5, 4, 2, "g1", "work", "work_a"),
		{Seq: 6, Type: RunEventHumanVerdictRecorded, GateID: "g1", NodeID: "g1"},
	}
	if plan := PlanOnCall(board, events, true, present()); len(plan.Answered) != 1 || plan.Answered[0].CreatedSeq != 2 {
		t.Fatalf("answered after the verdict = %+v", plan)
	}
	again := append(append([]RunEvent{}, events...), RunEvent{Seq: 7, Type: RunEventResumed}, request(8, "g2", "work"))
	plan := PlanOnCall(board, again, true, present())
	if len(plan.Answered) != 0 || len(plan.Deliveries) != 1 || plan.Deliveries[0].Request.Seq != 8 {
		t.Fatalf("the seat asked again by g2 = %+v", plan)
	}
	// A seat never asked stays kept, even when another formation's gate asks.
	unasked := []RunEvent{events[0], events[1], events[2], seatEvents(4, "other", "other_a", "s-other"), cleanup(5, "other", "other_a", "s-other", SeatOutcomeKeptOnCall), request(6, "g3", "other")}
	if plan := PlanOnCall(board, unasked, true, present()); len(plan.Answered) != 0 || len(plan.Deliveries) != 1 || plan.Deliveries[0].Seat.NodeID != "other" {
		t.Fatalf("unasked seat plan = %+v", plan)
	}
}

func TestPlanOnCallUncertainInputBelongsToOneSeatCreation(t *testing.T) {
	board := onCallBoard(HumanChannelSession, map[string][]string{"g1": {"human"}, "g2": {"human"}}, map[string]string{"peers": FormationTypePeer},
		"mis:out->peers:in", "peers:out->g1:in", "g1:pass->g2:in")
	events := []RunEvent{
		{Seq: 1, Type: RunEventStarted, MissionID: "mis", RunID: "run"},
		seatEvents(2, "peers", "peers_a", "s-a"), seatEvents(3, "peers", "peers_b", "s-b"),
		cleanup(4, "peers", "peers_a", "s-a", SeatOutcomeKeptOnCall), cleanup(5, "peers", "peers_b", "s-b", SeatOutcomeKeptOnCall),
		request(6, "g1", "peers"),
		{Seq: 7, Type: RunEventHumanAskFallback, GateID: "g1", Data: map[string]any{"requestedSeq": 6, "code": AskFallbackDeliveryUncertain, "seatCreatedSeq": 2}},
		{Seq: 8, Type: RunEventHumanVerdictRecorded, GateID: "g1"},
		{Seq: 9, Type: RunEventResumed},
		request(10, "g2", "peers"),
	}
	plan := PlanOnCall(board, events, true, present())
	if len(plan.Fallbacks) != 0 || len(plan.Deliveries) != 1 || plan.Deliveries[0].Seat.CreatedSeq != 3 {
		t.Fatalf("the healthy peer must receive the next ask: %+v", plan)
	}
	replaced := append(append([]RunEvent{}, events...),
		cleanup(11, "peers", "peers_a", "s-a", SeatOutcomeEnded),
		seatEvents(12, "peers", "peers_a", "s-a"),
		cleanup(13, "peers", "peers_a", "s-a", SeatOutcomeKeptOnCall),
	)
	plan = PlanOnCall(board, replaced, true, present())
	if len(plan.Fallbacks) != 0 || len(plan.Deliveries) != 2 || plan.Deliveries[0].Seat.CreatedSeq != 3 || plan.Deliveries[1].Seat.CreatedSeq != 12 {
		t.Fatalf("reusing a slot and session name must not poison its new seat: %+v", plan)
	}
}
