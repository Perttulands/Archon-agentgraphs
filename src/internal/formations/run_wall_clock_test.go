package formations

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestWallClockDeadlineLeavesOutTimeWaitingForTheOperator(t *testing.T) {
	start := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	at := func(seconds int) string {
		return start.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339Nano)
	}
	events := []RunEvent{{Seq: 1, Type: RunEventStarted, Timestamp: at(0)}}
	if deadline, err := wallClockDeadline(events, 120, start.Add(time.Hour)); err != nil || !deadline.Equal(start.Add(120*time.Second)) {
		t.Fatalf("a run that never waited: %v, %v", deadline, err)
	}
	events = append(events,
		RunEvent{Seq: 2, Type: RunEventHumanInputRequested, GateID: "gate_a", Timestamp: at(10)},
		RunEvent{Seq: 3, Type: RunEventHumanInputRequested, GateID: "gate_b", Timestamp: at(20)},
		RunEvent{Seq: 4, Type: RunEventHumanVerdictRecorded, GateID: "gate_a", Timestamp: at(100)},
		// A verdict for a gate that is not waiting changes nothing.
		RunEvent{Seq: 5, Type: RunEventHumanVerdictRecorded, GateID: "gate_c", Timestamp: at(110)},
		RunEvent{Seq: 6, Type: RunEventHumanVerdictRecorded, GateID: "gate_b", Timestamp: at(130)},
	)
	// Two requests waiting together, from 10 s to 130 s, count once: 120 s.
	if deadline, err := wallClockDeadline(events, 120, start.Add(time.Hour)); err != nil || !deadline.Equal(start.Add(240*time.Second)) {
		t.Fatalf("answered waits: deadline %v (%v), want start + 240 s", deadline.Sub(start), err)
	}
	// A request still waiting holds the clock until now: 80 s of agent time
	// were spent by 200 s, and 40 s stay left however long it waits.
	events = append(events, RunEvent{Seq: 7, Type: RunEventHumanInputRequested, GateID: "gate_a", Timestamp: at(200)})
	for _, now := range []int{260, 7200} {
		deadline, err := wallClockDeadline(events, 120, start.Add(time.Duration(now)*time.Second))
		if want := start.Add(time.Duration(240+now-200) * time.Second); err != nil || !deadline.Equal(want) {
			t.Fatalf("open wait at %d s: deadline %v (%v), want %v", now, deadline.Sub(start), err, want.Sub(start))
		}
		if remaining := deadline.Sub(start.Add(time.Duration(now) * time.Second)); remaining != 40*time.Second {
			t.Fatalf("open wait at %d s leaves %v, want the 40 s left when the request was made", now, remaining)
		}
	}
}

// clockedExecutor finishes each formation at once while the test clock moves
// on by that formation's work time. It records the time left on its deadline.
type clockedExecutor struct {
	clock *time.Time
	work  map[string]time.Duration
	calls []string
	left  map[string]time.Duration
}

func (e *clockedExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	e.calls = append(e.calls, req.NodeID)
	*e.clock = e.clock.Add(e.work[req.NodeID])
	text := req.NodeID + " done"
	return FormationExecutionResult{Status: "done", Text: text, Outputs: payloadsForFormationOutputs(req.Formation, text, "refs/"+req.NodeID+".md")}, nil
}

// contextClockedExecutor takes the context-aware path the tmux and lab
// executors take; clockedExecutor alone takes the legacy synchronous one.
type contextClockedExecutor struct{ *clockedExecutor }

func (e contextClockedExecutor) ExecuteFormationContext(ctx context.Context, req FormationExecution) (FormationExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return FormationExecutionResult{}, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if e.left == nil {
			e.left = map[string]time.Duration{}
		}
		e.left[req.NodeID] = time.Until(deadline)
	}
	return e.ExecuteFormation(req)
}

// runPastAHumanGate runs Work, which takes work, to the human gate Review. The
// operator answers after wait; Ship dispatches after beforeShip more. With
// restart the store and engine are reopened between request and verdict.
func runPastAHumanGate(t *testing.T, contextAware bool, work, wait, beforeShip time.Duration, restart bool) (*RunStatusProjection, *clockedExecutor, []RunEvent) {
	t.Helper()
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGateBoardFixture())
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	clocked := &clockedExecutor{clock: &clock, work: map[string]time.Duration{"fmn_work": work}}
	var executor FormationExecutor = clocked
	if contextAware {
		executor = contextClockedExecutor{clocked}
	}
	document, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: document.ETag, ExpectedBoardRev: document.Rev,
		Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 2, WallClockSeconds: 60},
	})
	if err != nil || status.Final || len(OpenHumanRequests(mustEvents(t, store, status.RunID))) != 1 {
		t.Fatalf("run to the gate = %+v, %v", status, err)
	}
	clock = clock.Add(wait)
	if restart {
		store = NewStore(store.Workspace)
		store.Now = func() time.Time { return clock }
		engine = NewRunEngine(store, personas, executor)
	}
	if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(beforeShip)
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
	if err != nil {
		t.Fatal(err)
	}
	return status, clocked, mustEvents(t, store, status.RunID)
}

func TestWaitingAtAHumanGateDoesNotSpendTheRunsWallClock(t *testing.T) {
	for _, path := range []struct {
		name         string
		contextAware bool
	}{{"context executor", true}, {"legacy synchronous executor", false}} {
		for _, restart := range []bool{false, true} {
			name := path.name
			if restart {
				name += " across a restart"
			}
			t.Run(name, func(t *testing.T) {
				// 20 s of work, two hours at the gate, 10 s to Ship: 30 s of a 60 s clock.
				status, executor, _ := runPastAHumanGate(t, path.contextAware, 20*time.Second, 2*time.Hour, 10*time.Second, restart)
				if status.Status != RunStatusSucceeded || !reflect.DeepEqual(executor.calls, []string{"fmn_work", "fmn_ship"}) {
					t.Fatalf("status %+v, calls %v", status, executor.calls)
				}
				if left := executor.left["fmn_ship"]; path.contextAware && (left <= 25*time.Second || left > 30*time.Second) {
					t.Fatalf("Ship had %v left, want the 30 s the agents did not use", left)
				}
			})
		}
	}
}

func TestAgentTimeAloneStillRunsTheWallClockOut(t *testing.T) {
	for _, contextAware := range []bool{true, false} {
		// 20 s of work and 45 s before Ship is 65 s of a 60 s clock, however
		// long the operator took.
		status, executor, events := runPastAHumanGate(t, contextAware, 20*time.Second, 2*time.Hour, 45*time.Second, false)
		if status.Status != RunStatusBlocked || !reflect.DeepEqual(executor.calls, []string{"fmn_work"}) {
			t.Fatalf("context-aware %t: status %+v, calls %v", contextAware, status, executor.calls)
		}
		if failure := eventOfType(t, events, RunEventError); failure.Data["code"] != "wall_clock_exceeded" {
			t.Fatalf("context-aware %t: error %#v", contextAware, failure.Data)
		}
	}
	_, err := wallClockDeadline(nil, 60, time.Now())
	if !errors.Is(err, ErrRunLedgerInvalid) {
		t.Fatalf("empty ledger: %v", err)
	}
}
