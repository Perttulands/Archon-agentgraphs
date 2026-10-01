package formations

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// A run started without limits has none (archon-o7p.7): a gate that keeps
// sending the work back re-runs it as often as it takes, and the run finishes
// when the gate passes. Hours pass between steps, and no wall clock stops it.
func TestRunWithoutLimitsLoopsUntilTheGatePasses(t *testing.T) {
	store, personas := s4RunFixture(t)
	clock := time.Date(2026, 6, 3, 17, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		clock = clock.Add(time.Hour)
		return clock
	}
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	engine := NewRunEngine(store, personas, &fakeRunExecutor{})
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail", "fail", "fail", "fail", "fail", "fail", "pass"}})

	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, want succeeded once the gate passes", status)
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	if got, want := nodeStartedAttempts(events, "fmn_work"), []int{1, 2, 3, 4, 5, 6, 7}; !reflect.DeepEqual(got, want) {
		t.Fatalf("work attempts = %v, want %v", got, want)
	}
	if got := nodeStartedAttempts(events, "fmn_ship"); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("ship attempts = %v, want [1]", got)
	}
	for _, event := range events {
		if event.Type == RunEventError || event.Type == RunEventBlocked {
			t.Fatalf("unlimited run recorded %s: %+v", event.Type, event.Data)
		}
	}
	if limits := runLimitsFromEvent(events[0]); limits != (RunLimits{}) {
		t.Fatalf("recorded limits = %+v, want none", limits)
	}
}

// Explicit limits still hold: the same loop with maxAttempts 3 blocks on the
// third send-back and names the attempt limit; with maxDispatch 4 it blocks
// before the fifth start and names the dispatch limit.
func TestRunWithExplicitLimitsStillBlocksAndNamesTheLimit(t *testing.T) {
	cases := []struct {
		name     string
		limits   RunLimits
		attempts []int
		code     string
		limit    RunLimitReached
	}{
		{name: "attempts", limits: RunLimits{MaxAttempts: 3}, attempts: []int{1, 2, 3}, code: RunBlockReviseLoopExhausted, limit: RunLimitReached{Kind: RunLimitAttempts, NodeID: "fmn_work", Used: 3, Max: 3}},
		{name: "dispatches", limits: RunLimits{MaxDispatch: 4}, attempts: []int{1, 2, 3, 4}, code: RunBlockMaxDispatchExceeded, limit: RunLimitReached{Kind: RunLimitDispatches, NodeID: "fmn_work", Used: 4, Max: 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			store.Now = fixedClock()
			personas.Now = fixedClock()
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatalf("read board: %v", err)
			}
			engine := NewRunEngine(store, personas, &fakeRunExecutor{})
			engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail", "fail", "fail", "fail", "fail", "fail", "pass"}})
			status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev, Limits: tc.limits})
			if err != nil {
				t.Fatalf("run mission: %v", err)
			}
			if status.Status != RunStatusBlocked || status.ResumeAllowed {
				t.Fatalf("status = %+v, want a limit block", status)
			}
			events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
			if got := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(got, tc.attempts) {
				t.Fatalf("work attempts = %v, want %v", got, tc.attempts)
			}
			if code := eventOfType(t, events, RunEventError).Data["code"]; code != tc.code {
				t.Fatalf("error code = %v, want %s", code, tc.code)
			}
			problems := projectRunProblems(events)
			last := problems[len(problems)-1]
			if last.Limit == nil || *last.Limit != tc.limit {
				t.Fatalf("limit block = %+v limit %+v, want %+v", last, last.Limit, tc.limit)
			}
		})
	}
}

// A negative limit is refused before anything is written, by the engine's own
// mission and formation starts, so every caller refuses it the same way.
func TestRunStartsRefuseNegativeLimits(t *testing.T) {
	for _, limits := range []RunLimits{{MaxDispatch: -1}, {MaxAttempts: -1}, {WallClockSeconds: -1}} {
		store, personas := s4RunFixture(t)
		createS4Persona(t, personas, "scout")
		writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
		engine := NewRunEngine(store, personas, &fakeRunExecutor{})
		if _, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Limits: limits}); !errors.Is(err, ErrInvalidRunLimits) {
			t.Fatalf("mission start with %+v: err = %v", limits, err)
		}
		if _, err := engine.RunFormation("session-search", "fmn_work", FormationRunRequest{Limits: limits}); !errors.Is(err, ErrInvalidRunLimits) {
			t.Fatalf("formation start with %+v: err = %v", limits, err)
		}
		assertNoRunArtifacts(t, store, "session-search")
	}
	if err := ValidateRunLimits(RunLimits{}); err != nil {
		t.Fatalf("no limits: %v", err)
	}
}

func assertNoRunArtifacts(t *testing.T, store *Store, slug string) {
	t.Helper()
	runDir := filepath.Join(store.Workspace, ".archon", "runs", slug)
	entries, err := os.ReadDir(runDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read run artifacts: %v", err)
	}
	if err == nil && len(entries) != 0 {
		t.Fatalf("run artifacts = %v, want none", entries)
	}
}
