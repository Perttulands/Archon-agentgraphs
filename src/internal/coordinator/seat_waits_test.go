package coordinator

import (
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// run seats shows a seat's recorded wait until the seat records anything else,
// and run wait --until any-change reports each seat_state as a change.
func TestSeatWaitsLastUntilTheSeatMovesOn(t *testing.T) {
	state := func(seq int, slot, value string) formations.RunEvent {
		return formations.RunEvent{RunID: "run_x", Seq: seq, Type: formations.RunEventSeatState, NodeID: "fmn_work", SlotID: slot, Data: map[string]any{"state": value, "detail": "why", "since": "2026-10-01T09:00:00Z"}}
	}
	events := []formations.RunEvent{
		{RunID: "run_x", Seq: 1, Type: formations.RunEventStarted, Data: map[string]any{"boardSlug": "proof"}},
		state(2, "slot_a", formations.SeatStateNotReady),
		state(3, "slot_b", formations.SeatStateWaitingForIdleInput),
		{RunID: "run_x", Seq: 4, Type: formations.RunEventSlotDispatch, NodeID: "fmn_work", SlotID: "slot_a"},
		state(5, "slot_c", formations.SeatStateTurnEndedWithoutSentinel),
		state(6, "slot_c", formations.SeatStateWorking),
	}
	waits := seatWaits(events)
	if waits[[2]string{"fmn_work", "slot_a"}] != nil || waits[[2]string{"fmn_work", "slot_c"}] != nil {
		t.Fatalf("superseded waits remain: %+v", waits)
	}
	if got := waits[[2]string{"fmn_work", "slot_b"}]; got == nil || *got != (SeatWaiting{State: formations.SeatStateWaitingForIdleInput, Detail: "why", Since: "2026-10-01T09:00:00Z", Seq: 3}) {
		t.Fatalf("slot_b wait = %+v", got)
	}

	got, err := projectWait("run_x", events, nil, WaitUntilAnyChange, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != WaitOutcomeChanged || len(got.Changes) != 2 || got.Changes[0].State != formations.SeatStateTurnEndedWithoutSentinel || got.Changes[0].SlotID != "slot_c" || got.Changes[0].Detail != "why" {
		t.Fatalf("any-change wait = %+v", got)
	}
}
