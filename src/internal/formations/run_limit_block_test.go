package formations

import (
	"errors"
	"testing"
)

func limitLedger(limits map[string]any, tail ...RunEvent) []RunEvent {
	events := []RunEvent{{Seq: 1, Type: RunEventStarted, Data: map[string]any{"limits": limits}}}
	for i, event := range tail {
		event.Seq = i + 2
		events = append(events, event)
	}
	return events
}

// The engine now records the limit block as not resumable, and the ledger
// refuses a resume of it.
func TestEngineRecordsALimitBlockThatCannotResume(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4CascadeBoardFixture())
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Limits: RunLimits{MaxDispatch: 1, MaxAttempts: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusBlocked || status.ResumeAllowed {
		t.Fatalf("status = %+v, want a block that cannot resume", status)
	}
	if _, err := store.ResumeRun(status.RunID, RunResumeRequest{Mode: "reattach"}); !errors.Is(err, ErrRunResumeNotAllowed) {
		t.Fatalf("store resume error = %v", err)
	}
	if err := store.AppendRunEvent(status.RunID, RunEvent{Type: RunEventResumed}); !errors.Is(err, ErrRunResumeNotAllowed) {
		t.Fatalf("append resume error = %v", err)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	problems := projectRunProblems(events)
	last := problems[len(problems)-1]
	if last.Type != RunEventBlocked || last.Limit == nil || last.Limit.Kind != RunLimitDispatches || last.Limit.Used != 1 || last.Limit.Max != 1 {
		t.Fatalf("limit block problem = %+v", last)
	}
}
