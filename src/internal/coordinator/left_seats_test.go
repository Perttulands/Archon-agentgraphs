package coordinator

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// archon-itk: a daemon shutdown leaves a working seat running for the
// restarted daemon to reattach. The run still owns it, so cancelling the
// blocked run ends that seat, before run_canceled, and a resume that runs the
// step again ends it before the new attempt's seats start.

// leaveWorkSeatAtShutdown records what a shutdown mid-step leaves in the
// ledger: Work started, its seat created and left running, and the restart's
// block.
func leaveWorkSeatAtShutdown(t *testing.T, c *Coordinator) string {
	t.Helper()
	started, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", ExpectedBoardRev: 1, Personas: c.personas})
	if err != nil {
		t.Fatal(err)
	}
	id := started.RunID
	for _, event := range []formations.RunEvent{
		{Type: formations.RunEventNodeStarted, NodeID: "fmn_work", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: formations.RunEventSeatCreated, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"sessionName": "archon-slot_work", "sessionId": "$left", "paneId": "%left", "harness": "openai-codex"}},
		{Type: formations.RunEventSeatCleanup, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"sessionName": "archon-slot_work", "outcome": formations.SeatOutcomeLeftShutdown}},
	} {
		if err := c.store.AppendRunEvent(id, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.engine.BlockInterruptedRun(id); err != nil {
		t.Fatal(err)
	}
	return id
}

func seatCleanups(t *testing.T, c *Coordinator, id string) []string {
	t.Helper()
	var trail []string
	for _, event := range eventsOf(t, c, id) {
		switch event.Type {
		case formations.RunEventSeatCleanup:
			entry := event.Data["outcome"].(string)
			if cause, _ := event.Data["cause"].(string); cause != "" {
				entry += " " + cause
			}
			trail = append(trail, entry)
		case formations.RunEventCanceled:
			trail = append(trail, event.Type)
		}
	}
	return trail
}

func TestCancellingABlockedRunEndsTheSeatAShutdownLeft(t *testing.T) {
	keeper := &keeperExecutor{}
	c, _ := onCallFixture(t, testBoard, keeper, nil)
	id := leaveWorkSeatAtShutdown(t, c)
	if w := post(t, c, "/api/runs/"+id+"/abort", `{"reason":"no longer needed"}`); w.Code != 200 {
		t.Fatalf("abort %d %s", w.Code, w.Body.String())
	}
	if _, ended := keeper.snapshot(); !reflect.DeepEqual(ended, []string{"$left"}) {
		t.Fatalf("ended = %v, want the seat left at shutdown", ended)
	}
	if got := seatCleanups(t, c, id); !reflect.DeepEqual(got, []string{"left_shutdown", "ended run_final", "run_canceled"}) {
		t.Fatalf("seat trail = %v", got)
	}
}

func TestRunningTheStepAgainEndsTheSeatAShutdownLeft(t *testing.T) {
	keeper := &keeperExecutor{}
	c, _ := onCallFixture(t, testBoard, keeper, nil)
	id := leaveWorkSeatAtShutdown(t, c)
	if w := post(t, c, "/api/runs/"+id+"/resume", `{"mode":"redispatch","reason":"run Work again"}`); w.Code != 202 {
		t.Fatalf("resume %d %s", w.Code, w.Body.String())
	}
	awaitState(t, c, id, "waiting_human")
	if _, ended := keeper.snapshot(); !reflect.DeepEqual(ended, []string{"$left"}) {
		t.Fatalf("ended = %v, want the seat left at shutdown", ended)
	}
	got := strings.Join(seatCleanups(t, c, id), ", ")
	if !strings.HasPrefix(got, "left_shutdown, ended new_attempt") {
		t.Fatalf("seat trail = %s", got)
	}
}
