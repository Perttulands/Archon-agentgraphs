package formations

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Limit cards (archon-o7p.8.1): a run has no limits unless its mission holds a
// Limit card; a card caps its step's or the whole mission's rounds, and the
// driver grants one more round at a time.

func addLimit(t *testing.T, store *Store, target string, rounds int) LimitNode {
	t.Helper()
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CreateLimit("session-search", LimitCreateRequest{Title: "Cap", Target: target, Rounds: rounds}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	return result.Limit
}

// loopingGateRun starts Work -> code gate (fail back to Work, pass to Ship)
// whose gate fails six times before it passes.
func loopingGateRun(t *testing.T, limits func(*Store)) (*Store, *RunEngine, *RunStatusProjection) {
	t.Helper()
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
	if limits != nil {
		limits(store)
	}
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewRunEngine(store, personas, &fakeRunExecutor{})
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail", "fail", "fail", "fail", "fail", "fail", "pass"}})
	status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	return store, engine, status
}

// Without a Limit card a gate that keeps sending work back re-runs it as often
// as it takes, and the run finishes when the gate passes; hours between steps
// stop nothing.
func TestARunWithoutALimitCardLoopsUntilTheGatePasses(t *testing.T) {
	store, personas := s4RunFixture(t)
	clock := time.Date(2026, 6, 3, 17, 0, 0, 0, time.UTC)
	store.Now = func() time.Time {
		clock = clock.Add(time.Hour)
		return clock
	}
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
	engine := NewRunEngine(store, personas, &fakeRunExecutor{})
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail", "fail", "fail", "fail", "fail", "fail", "pass"}})
	status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test"})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, %v; want succeeded once the gate passes", status, err)
	}
	events := mustEvents(t, store, status.RunID)
	if got := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("work attempts = %v", got)
	}
	if _, recorded := events[0].Data["limits"]; recorded {
		t.Fatalf("run_started recorded limits: %+v", events[0].Data)
	}
	for _, event := range events {
		if event.Type == RunEventError || event.Type == RunEventBlocked {
			t.Fatalf("unlimited run recorded %s: %+v", event.Type, event.Data)
		}
	}
}

// A card on a step stops it at its rounds, send-backs included, with the exact
// reason; a resume needs a grant, and each grant gives exactly one more round,
// recorded with who gave it.
func TestAStepLimitStopsAtItsRoundsAndAGrantGivesOneMore(t *testing.T) {
	var limit LimitNode
	store, engine, status := loopingGateRun(t, func(store *Store) { limit = addLimit(t, store, "fmn_work", 3) })
	if status.Status != RunStatusBlocked || !status.ResumeAllowed || status.ResumePolicy != ResumePolicyGrant {
		t.Fatalf("status = %+v, want a block that resumes with a grant", status)
	}
	events := mustEvents(t, store, status.RunID)
	if got := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("work attempts = %v, want three", got)
	}
	block := events[len(events)-1]
	want := RunLimitReached{Kind: LimitKindRounds, LimitID: limit.ID, NodeID: "fmn_work", Used: 3, Max: 3}
	if block.Type != RunEventBlocked || block.Data["reason"] != "Work used 3 of 3 rounds" || block.Data["code"] != RunBlockLimitReached {
		t.Fatalf("block = %+v", block)
	}
	problems := projectRunProblems(events)
	if last := problems[len(problems)-1]; last.Limit == nil || *last.Limit != want || last.Code != RunBlockLimitReached {
		t.Fatalf("problem = %+v limit %+v, want %+v", last, last.Limit, want)
	}

	if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach"}); !errors.Is(err, ErrRunGrantRequired) {
		t.Fatalf("resume without a grant: %v", err)
	}
	// A fresh engine replays the ledger, as after a restart.
	engine = NewRunEngine(store, nil, &fakeRunExecutor{})
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail", "pass"}})
	status, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "human:perttu", Mode: "reattach", Reason: "one more try", Grant: true})
	if err != nil || status.Status != RunStatusBlocked {
		t.Fatalf("after the grant = %+v, %v", status, err)
	}
	events = mustEvents(t, store, status.RunID)
	if got := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("work attempts after one grant = %v, want exactly one more", got)
	}
	resumed := lastEventOfType(t, events, RunEventResumed)
	if resumed.Actor != "human:perttu" || !reflect.DeepEqual(resumed.Data["grant"], map[string]any{"limitId": limit.ID, "kind": "rounds", "amount": float64(1)}) {
		t.Fatalf("grant = actor %q %+v", resumed.Actor, resumed.Data["grant"])
	}
	if reason := events[len(events)-1].Data["reason"]; reason != "Work used 4 of 4 rounds, 1 of them granted" {
		t.Fatalf("second block reason = %v", reason)
	}
	// The second grant takes it past the passing verdict.
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "human:perttu", Mode: "reattach", Grant: true})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("after the second grant = %+v, %v", status, err)
	}
}

// A card on the Input card caps every step start of the run.
func TestAMissionLimitStopsTheWholeRunAtItsRounds(t *testing.T) {
	var limit LimitNode
	store, engine, status := loopingGateRun(t, func(store *Store) { limit = addLimit(t, store, "mis_showcase", 4) })
	if status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant {
		t.Fatalf("status = %+v", status)
	}
	events := mustEvents(t, store, status.RunID)
	if got := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatalf("work attempts = %v", got)
	}
	block := events[len(events)-1]
	if block.Data["reason"] != "The mission used 4 of 4 rounds" || block.NodeID != "fmn_work" {
		t.Fatalf("block = %+v", block)
	}
	if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Mode: "reattach", Grant: true}); err != nil {
		t.Fatal(err)
	}
	events = mustEvents(t, store, status.RunID)
	if got := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("work attempts after the grant = %v", got)
	}
	if use := roundsUse(mustReadRunBoard(t, store, status.RunID), events, limit); use.Used != 5 || use.Max != 5 || use.Granted != 1 {
		t.Fatalf("mission use = %+v", use)
	}
}

// A judge's start counts against the mission's rounds as any step's does.
func TestJudgesCountTowardTheMissionsRounds(t *testing.T) {
	fixture := strings.Replace(s4JudgeChainRunBoardFixture(), `kinds = ["code", "formation"]`, `kinds = ["formation"]`, 1)
	executor := &fakeRunExecutor{outputs: map[string]string{"fmn_j2": judgeBlock("pass", "fine")}}
	store, _, _, status := startBranchingRun(t, fixture+"\n[[limit]]\nid = \"lim_mission\"\ntitle = \"Cap\"\ntarget = \"mis_showcase\"\nrounds = 2\n", executor)
	if status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant {
		t.Fatalf("status = %+v", status)
	}
	if got := executor.nodeIDs(); !reflect.DeepEqual(got, []string{"fmn_work", "fmn_j1"}) {
		t.Fatalf("started = %v, want work and the first judge", got)
	}
	if block := mustEvents(t, store, status.RunID); block[len(block)-1].Data["reason"] != "The mission used 2 of 2 rounds" || block[len(block)-1].NodeID != "fmn_j2" {
		t.Fatalf("block = %+v", block[len(block)-1])
	}
}

// --grant only resumes a spent limit.
func TestAGrantResumesOnlyASpentLimit(t *testing.T) {
	executor := &seatLossOnceExecutor{failNodeID: "fmn_b"}
	_, _, engine, status := startBranchingRun(t, branchingGateBoardFixture(), executor)
	if status.Status != RunStatusBlocked || status.ResumePolicy != "" {
		t.Fatalf("status = %+v, want an ordinary block", status)
	}
	if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Mode: "redispatch", Grant: true}); !errors.Is(err, ErrRunNothingToGrant) {
		t.Fatalf("grant on an ordinary block: %v", err)
	}
}

func mustReadRunBoard(t *testing.T, store *Store, runID string) *BoardDocument {
	t.Helper()
	board, err := store.ReadRunBoard(runID)
	if err != nil {
		t.Fatal(err)
	}
	return board
}

// Cards are created, rewired, cleared, deleted and restored through one store
// path, and a write refuses a target that is not a step or the Input card, or
// a negative value.
func TestLimitCardsAuthorThroughTheStore(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
	limit := addLimit(t, store, "fmn_work", 3)
	board, _ := store.ReadBoard("session-search")
	if len(board.Limits) != 1 || board.Limits[0].Target != "fmn_work" || *board.Limits[0].Rounds != 3 || board.Limits[0].Title != "Cap" {
		t.Fatalf("limits = %+v", board.Limits)
	}
	if !strings.Contains(board.TOML, "[[limit]]\nid = \""+limit.ID+"\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\nrounds = 3\n") {
		t.Fatalf("toml:\n%s", board.TOML)
	}
	opts := func() WriteOptions {
		current, _ := store.ReadBoard("session-search")
		return WriteOptions{ExpectedETag: current.ETag, ExpectedRev: current.Rev}
	}
	for _, bad := range []LimitUpdateRequest{
		{LimitID: limit.ID, Target: ptr("gate_review")},
		{LimitID: limit.ID, Target: ptr("fmn_missing")},
		{LimitID: limit.ID, Rounds: ptr(-2)},
	} {
		if _, err := store.UpdateLimit("session-search", bad, opts()); !errors.Is(err, ErrInvalidLimit) {
			t.Fatalf("update %+v: %v", bad, err)
		}
	}
	if _, err := store.UpdateLimit("session-search", LimitUpdateRequest{LimitID: limit.ID, Target: ptr("mis_showcase"), Rounds: ptr(9), Title: ptr("")}, opts()); err != nil {
		t.Fatal(err)
	}
	board, _ = store.ReadBoard("session-search")
	if got := board.Limits[0]; got.Target != "mis_showcase" || *got.Rounds != 9 || got.Title != "Limit" {
		t.Fatalf("updated = %+v", got)
	}
	if _, err := store.UpdateLimit("session-search", LimitUpdateRequest{LimitID: limit.ID, Rounds: ptr(0), Target: ptr("")}, opts()); err != nil {
		t.Fatal(err)
	}
	board, _ = store.ReadBoard("session-search")
	if got := board.Limits[0]; got.Target != "" || got.Rounds != nil {
		t.Fatalf("cleared = %+v", got)
	}
	result, err := store.DeleteLimit("session-search", LimitDeleteRequest{ID: limit.ID}, opts())
	if err != nil || len(result.Board.Limits) != 0 {
		t.Fatalf("delete = %+v, %v", result, err)
	}
	restored, err := store.RestoreNode("session-search", NodeRestoreRequest{Limit: &LimitNode{ID: limit.ID, Title: "Cap", Target: "fmn_work", Rounds: ptr(3)}, X: 40, Y: 60}, opts())
	if err != nil || restored.NodeID != limit.ID || len(restored.Board.Limits) != 1 || *restored.Board.Limits[0].Rounds != 3 {
		t.Fatalf("restore = %+v, %v", restored, err)
	}
	if _, err := store.CreateLimit("session-search", LimitCreateRequest{Target: "end_rejected", Rounds: 1}, opts()); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("a card on an End node: %v", err)
	}
}

func ptr[T any](value T) *T { return &value }

// Validation rejects a card wired to nothing, a card on a node it cannot
// cover, a non-positive value and two cards on one target, in plain words; a
// card with no knob is a warning. Admission refuses every run while a card is
// invalid.
func TestValidationRejectsALimitCardWiredToNothingOrHoldingABadValue(t *testing.T) {
	base := s4GateBoardFixture(true)
	for _, tc := range []struct {
		name, toml, code, message string
	}{
		{"unwired", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"\"\nrounds = 3\n", FindingInvalidLimit, "Limit Cap is wired to nothing: wire it to a step, or to the Input card for the whole mission"},
		{"wired to a gate", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"gate_review\"\nrounds = 3\n", FindingInvalidLimit, "Limit Cap covers gate_review, which is not a step or the Input card: wire it to a step, or to the Input card for the whole mission"},
		{"zero rounds", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\nrounds = 0\n", FindingInvalidLimit, "Limit Cap holds rounds = 0: rounds must be a positive whole number"},
		{"two cards on one step", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\nrounds = 3\n\n[[limit]]\nid = \"lim_b\"\ntitle = \"Second cap\"\ntarget = \"fmn_work\"\nrounds = 5\n", FindingInvalidLimit, "Work has two Limit cards, Cap and Second cap: keep one"},
		{"no knob", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\n", FindingEmptyLimit, "Limit Cap sets no limit: give it rounds or time, or delete it"},
		{"zero time", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\nseconds = 0\n", FindingInvalidLimit, "Limit Cap holds seconds = 0: time must be a positive whole number of seconds"},
		{"a warning without time", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\nrounds = 3\nwarnSeconds = 300\n", FindingInvalidLimit, "Limit Cap warns with 5 min left but sets no time: give it time, or clear the warning"},
		{"a warning before any work", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\nseconds = 300\nwarnSeconds = 300\n", FindingInvalidLimit, "Limit Cap warns with 5 min left of 5 min, before any work: warn with less time left"},
		{"a negative warning", "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\nseconds = 300\nwarnSeconds = -1\n", FindingInvalidLimit, "Limit Cap holds warnSeconds = -1: the warning must be a positive whole number of seconds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			board, err := parseBoard([]byte(base + "\n" + tc.toml))
			if err != nil {
				t.Fatal(err)
			}
			report := ValidateBoard(board)
			findings := report.Errors
			if tc.code == FindingEmptyLimit {
				findings = report.Warnings
			}
			found := false
			for _, finding := range findings {
				found = found || finding.Code == tc.code && finding.Message == tc.message
			}
			if !found {
				t.Fatalf("findings = %+v, want %q", report, tc.message)
			}
			err = CheckRunAdmission(board, nil, RunAdmissionScope{MissionID: "mis_showcase"})
			var admission *RunAdmissionError
			refused := errors.As(err, &admission) && strings.Contains(admission.Error(), tc.message)
			if refused != (tc.code == FindingInvalidLimit) {
				t.Fatalf("admission = %v", err)
			}
		})
	}
}

// A start a restart cut short is not a round: the step's re-run is, so a run
// reaches the same limits wherever a restart falls.
func TestARestartCutShortStartIsNotARound(t *testing.T) {
	board := &BoardDocument{Missions: []MissionNode{{ID: "mis"}}}
	rounds := 9
	step := LimitNode{ID: "lim_step", Target: "fmn_work", Rounds: &rounds}
	mission := LimitNode{ID: "lim_mission", Target: "mis", Rounds: &rounds}
	start := func(node string) RunEvent {
		return RunEvent{Type: RunEventNodeStarted, NodeID: node, Data: map[string]any{"nodeKind": "formation"}}
	}
	restart := RunEvent{Type: RunEventError, Data: map[string]any{"code": RunBlockCoordinatorInterrupted}}
	output := func(node string) RunEvent { return RunEvent{Type: RunEventNodeOutput, NodeID: node} }
	for _, tc := range []struct {
		name          string
		events        []RunEvent
		step, mission int
	}{
		{"a finished start", []RunEvent{start("fmn_work"), output("fmn_work")}, 1, 1},
		{"a running start", []RunEvent{start("fmn_work")}, 1, 1},
		{"a start cut short and not yet re-run", []RunEvent{start("fmn_work"), restart}, 0, 0},
		{"a start cut short and re-run", []RunEvent{start("fmn_work"), restart, start("fmn_work"), output("fmn_work")}, 1, 1},
		{"a start whose seat was reattached", []RunEvent{start("fmn_work"), restart, output("fmn_work")}, 1, 1},
		{"a finished start before a restart", []RunEvent{start("fmn_work"), output("fmn_work"), restart, start("fmn_other")}, 1, 2},
	} {
		if got := roundsUsed(board, tc.events, step); got != tc.step {
			t.Errorf("%s: step rounds = %d, want %d", tc.name, got, tc.step)
		}
		if got := roundsUsed(board, tc.events, mission); got != tc.mission {
			t.Errorf("%s: mission rounds = %d, want %d", tc.name, got, tc.mission)
		}
	}
}

// failingRunExecutor fails its first failures dispatches, then works as
// fakeRunExecutor does.
type failingRunExecutor struct {
	fakeRunExecutor
	failures int
}

func (f *failingRunExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	if f.failures > 0 {
		f.failures--
		f.calls = append(f.calls, req)
		return FormationExecutionResult{}, runExecutionError("seat_exited", "the seat exited before its output", "adapter", nil)
	}
	return f.fakeRunExecutor.ExecuteFormation(req)
}

// A single step's run resumes as a mission run does (archon-y8br): its step
// runs again as the next attempt on the same inputs, a spent Limit card stops
// it until a grant, and its output ends the run.
func TestABlockedSingleStepRunResumes(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(map[bool]string{false: "after a failure", true: "with a grant"}[limited], func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
			if limited {
				addLimit(t, store, "fmn_work", 1)
			}
			executor := &failingRunExecutor{failures: 1}
			status, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_work", FormationRunRequest{Actor: "agent:test", Inputs: map[string]string{"brief": "Find the sessions"}})
			if err != nil || status.Status != RunStatusBlocked || !status.ResumeAllowed {
				t.Fatalf("first attempt = %+v, %v; want a resumable block", status, err)
			}
			// A fresh engine replays the ledger, as after a restart.
			engine := NewRunEngine(store, personas, executor)
			status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "try again"})
			if err != nil {
				t.Fatal(err)
			}
			if limited {
				if status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant {
					t.Fatalf("resume at a spent limit = %+v, want a block that takes a grant", status)
				}
				if status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "human:perttu", Mode: "reattach", Grant: true}); err != nil {
					t.Fatal(err)
				}
			}
			if status.Status != RunStatusSucceeded {
				t.Fatalf("resumed = %+v, want succeeded", status)
			}
			events := mustEvents(t, store, status.RunID)
			if got := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(got, []int{1, 2}) {
				t.Fatalf("work attempts = %v, want the failed one and one more", got)
			}
			if len(executor.calls) != 2 || !reflect.DeepEqual(executor.calls[0].Inputs, executor.calls[1].Inputs) || executor.calls[1].Inputs[0].Text != "Find the sessions" {
				t.Fatalf("calls = %+v, want the same inputs twice", executor.calls)
			}
			last := events[len(events)-1]
			if last.Type != RunEventSucceeded || last.Data["mode"] != "formation" || last.Data["inputCardId"] != "single_fmn_work" {
				t.Fatalf("last event = %+v", last)
			}
		})
	}
}
