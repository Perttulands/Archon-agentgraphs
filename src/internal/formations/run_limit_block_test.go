package formations

import (
	"errors"
	"reflect"
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

// A ledger written before limit blocks were recorded as such still says
// resumeAllowed. The projection derives the limit from the error's code and the
// ledger's counts, so the block names it and is not offered for resume.
func TestLegacyLimitBlocksNameTheLimitAndCannotResume(t *testing.T) {
	legacyBlock := func(nodeID, code string) []RunEvent {
		return []RunEvent{
			{Type: RunEventError, NodeID: nodeID, Data: map[string]any{"code": code, "recoverable": true}},
			{Type: RunEventBlocked, NodeID: nodeID, Data: map[string]any{"reason": "limit", "blockedNodeId": nodeID, "resumeAllowed": true}},
		}
	}
	attempts := limitLedger(map[string]any{"maxAttempts": float64(3), "maxDispatch": float64(20)}, append([]RunEvent{
		{Type: RunEventNodeStarted, NodeID: "fmn_draft", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeStarted, NodeID: "fmn_draft", Attempt: 2, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeStarted, NodeID: "fmn_draft", Attempt: 3, Data: map[string]any{"nodeKind": "formation"}},
	}, legacyBlock("fmn_draft", RunBlockResumeAttemptsExhausted)...)...)
	block := len(attempts) - 1
	if got, want := runLimitReached(attempts, block), (&RunLimitReached{Kind: RunLimitAttempts, NodeID: "fmn_draft", Used: 3, Max: 3}); !reflect.DeepEqual(got, want) {
		t.Fatalf("attempt limit = %+v, want %+v", got, want)
	}
	if runBlockResumeAllowed(attempts, block) {
		t.Fatal("an exhausted attempt limit must not be resumable")
	}
	status, err := ProjectRunEvents("run_1", attempts)
	if err != nil || status.Status != RunStatusBlocked || status.ResumeAllowed {
		t.Fatalf("projection = %+v, %v", status, err)
	}
	problem := projectRunProblems(attempts)[1]
	if problem.Limit == nil || problem.Limit.Used != 3 || problem.Code != RunBlockResumeAttemptsExhausted || problem.ResumeAllowed == nil || *problem.ResumeAllowed {
		t.Fatalf("problem = %+v", problem)
	}

	dispatches := limitLedger(map[string]any{"maxAttempts": float64(3), "maxDispatch": float64(2)}, append([]RunEvent{
		{Type: RunEventNodeStarted, NodeID: "mis_start", Data: map[string]any{"nodeKind": "mission"}},
		{Type: RunEventNodeStarted, NodeID: "fmn_a", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
		{Type: RunEventNodeStarted, NodeID: "fmn_b", Attempt: 1, Data: map[string]any{"nodeKind": "formation"}},
	}, legacyBlock("fmn_c", RunBlockMaxDispatchExceeded)...)...)
	if got, want := runLimitReached(dispatches, len(dispatches)-1), (&RunLimitReached{Kind: RunLimitDispatches, NodeID: "fmn_c", Used: 2, Max: 2}); !reflect.DeepEqual(got, want) {
		t.Fatalf("dispatch limit = %+v, want %+v", got, want)
	}

	// Any other block keeps its recorded resumeAllowed.
	other := limitLedger(map[string]any{"maxAttempts": float64(3)}, legacyBlock("fmn_a", "executor_failed")...)
	if runLimitReached(other, len(other)-1) != nil || !runBlockResumeAllowed(other, len(other)-1) {
		t.Fatal("an executor failure stays resumable")
	}
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
