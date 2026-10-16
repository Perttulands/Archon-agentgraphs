package formations

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Best of N (archon-gihe): a step whose Limit card is spent picks the best
// attempt its gate judged, when the card says best or a resume asks, and goes
// on through the gate's pass route with that attempt's output.

// pickBoardFixture is Input -> Work -> Review (pass Ship -> Done, fail back to
// Work), Review judged by the chain Judge 1 -> Judge 2, with the given gate
// kinds and Limit card.
func pickBoardFixture(kinds, card string) string {
	fixture := strings.Replace(s4JudgeChainRunBoardFixture(), `kinds = ["code", "formation"]`, "kinds = "+kinds, 1)
	fixture = strings.Replace(fixture, "id = \"edge_gate_fail_rejected\"\nfrom = \"gate_review:fail\"\nto = \"end_rejected:in\"", "id = \"edge_gate_fail_work\"\nfrom = \"gate_review:fail\"\nto = \"fmn_work:port_work_in\"", 1)
	return fixture + "\n" + card
}

// workCard is a Limit card on Work with the given knobs.
func workCard(knobs string) string {
	return "[[limit]]\nid = \"lim_cap\"\ntitle = \"Cap\"\ntarget = \"fmn_work\"\n" + knobs + "\n"
}

func pickBlock(attempt int, reason string, evidence ...string) string {
	if evidence == nil {
		evidence = []string{}
	}
	raw, _ := json.Marshal(map[string]any{"attempt": attempt, "reason": reason, "evidence": evidence})
	return "```archon-pick\n" + string(raw) + "\n```"
}

// pickRun starts the fixture's mission on a scripted lab.
func pickRun(t *testing.T, fixture string, responses map[string]map[int]string) (*Store, *RunEngine, *scriptedLabExecutor, *RunStatusProjection) {
	t.Helper()
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), fixture)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	if report := ValidateRunAdmission(board, nil, RunAdmissionScope{}); len(report.Errors) != 0 {
		t.Fatalf("fixture findings: %+v", report.Errors)
	}
	cwd := t.TempDir()
	executor := &scriptedLabExecutor{lab: NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: cwd, Harnesses: []string{"openai-codex"}}), responses: responses}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{Cwd: cwd, MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	return store, engine, executor, status
}

// callTrail lists the calls as node#attempt.
func callTrail(calls []FormationExecution) []string {
	trail := make([]string, 0, len(calls))
	for _, call := range calls {
		trail = append(trail, fmt.Sprintf("%s#%d", call.NodeID, call.Attempt))
	}
	return trail
}

// eventsOfType lists the events of a type.
func eventsOfType(events []RunEvent, eventType string) []RunEvent {
	var found []RunEvent
	for _, event := range events {
		if event.Type == eventType {
			found = append(found, event)
		}
	}
	return found
}

// pickVerdicts lists the gate verdicts a pick gave.
func pickVerdicts(events []RunEvent) []RunEvent {
	var found []RunEvent
	for _, event := range eventsOfType(events, RunEventGateVerdict) {
		if isPickEvent(event) {
			found = append(found, event)
		}
	}
	return found
}

// threeFailedDrafts is Work's three drafts, each judged a fail.
func threeFailedDrafts(pick string) map[string]map[int]string {
	return map[string]map[int]string{
		"fmn_work": {1: "draft one", 2: "draft two", 3: "draft three"},
		"fmn_j1":   {1: "review one", 2: "review two", 3: "review three", 4: "compared the three drafts"},
		"fmn_j2": {
			1: judgeBlock("fail", "no tests"), 2: judgeBlock("fail", "retry untested"), 3: judgeBlock("fail", "tests break the build"),
			4: pick,
		},
		"fmn_ship": {1: "shipped"},
	}
}

// Rounds 3, best: three drafts judged a fail, the judges pick the second side
// by side, and it goes on through Review's pass route to Ship as if Review had
// passed it. Status, the attempts and Review's evidence say it was a pick.
func TestABestCardPicksTheBestJudgedAttemptAndGoesOn(t *testing.T) {
	store, _, executor, status := pickRun(t, pickBoardFixture(`["formation"]`, workCard("rounds = 3\nwhenSpent = \"best\"")),
		threeFailedDrafts(pickBlock(2, "the only draft whose tests pass", "draft two: retry covered")))
	events := mustEvents(t, store, status.RunID)
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v:\n%s", status, eventTypeTrail(events))
	}
	want := []string{"fmn_work#1", "fmn_j1#1", "fmn_j2#1", "fmn_work#2", "fmn_j1#2", "fmn_j2#2", "fmn_work#3", "fmn_j1#3", "fmn_j2#3", "fmn_j1#4", "fmn_j2#4", "fmn_ship#1"}
	if got := callTrail(executor.calls); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if len(eventsOfType(events, RunEventBlocked)) != 0 || len(eventsOfType(events, RunEventError)) != 0 {
		t.Fatalf("a best card blocked or erred:\n%s", eventTypeTrail(events))
	}

	// The judges compare every judged attempt side by side under the pick contract.
	j1, j2 := executor.calls[9], executor.calls[10]
	for _, text := range []string{"Pick the best attempt of Work", "criterion: Judge the work", "attempt 1\nverdict: fail\nreason: no tests", "draft one", "attempt 2\nverdict: fail\nreason: retry untested", "draft two", "attempt 3", "draft three"} {
		if !strings.Contains(j1.Inputs[0].Text, text) {
			t.Fatalf("pick input lacks %q:\n%s", text, j1.Inputs[0].Text)
		}
	}
	if !reflect.DeepEqual(j2.PickAttempts, []int{1, 2, 3}) || !j2.FinalJudge || j2.JudgeGateID != "gate_review" {
		t.Fatalf("final judge call = %+v", j2)
	}
	slot := j2.Formation.Slots[0]
	settings, card, err := ResolveSlotSettings(slot, executor.lab.personas)
	if err != nil {
		t.Fatal(err)
	}
	prompt := executor.lab.renderPrompt(j2, slot, *card, settings.Variant())
	if !strings.Contains(prompt, "judge pick contract:") || !strings.Contains(prompt, "attempt must be one of attempts 1, 2 or 3") || strings.Contains(prompt, "judge verdict contract") {
		t.Fatalf("final judge prompt:\n%s", prompt)
	}

	started := eventsOfType(events, RunEventPickStarted)
	if len(started) != 1 {
		t.Fatalf("pick_started = %+v", started)
	}
	candidates := pickCandidatesFromEvent(started[0])
	if started[0].NodeID != "fmn_work" || started[0].GateID != "gate_review" || stringFromEventData(started[0], "trigger") != PickTriggerWhenSpent || len(candidates) != 3 {
		t.Fatalf("pick_started = %+v", started[0])
	}
	if limit := limitFromAny(started[0].Data["limit"]); limit == nil || limit.LimitID != "lim_cap" || limit.Kind != LimitKindRounds || limit.Used != 3 || limit.Max != 3 {
		t.Fatalf("pick limit = %+v", limit)
	}
	verdicts := pickVerdicts(events)
	if len(verdicts) != 1 {
		t.Fatalf("pick verdicts = %+v", verdicts)
	}
	verdict, pick := verdicts[0], pickData(verdicts[0])
	if verdict.Data["verdict"] != "pass" || verdict.Data["routePort"] != "pass" || verdict.Data["reason"] != "the only draft whose tests pass" ||
		intFromRunEventData(pick["attempt"]) != 2 || pick["decidedBy"] != "fmn_j2" || !reflect.DeepEqual(intsFromAny(pick["candidates"]), []int{1, 2, 3}) ||
		intFromRunEventData(pick["pickSeq"]) != started[0].Seq || !reflect.DeepEqual(gateEvidenceRefsFromRunEventData(verdict.Data["evidence"]), []GateEvidenceRef{{Kind: "formation", Text: "draft two: retry covered"}}) {
		t.Fatalf("pick verdict = %+v", verdict)
	}

	// Ship runs on the picked draft, with nothing marking it as picked.
	ship := callsByNode(executor.calls)["fmn_ship"][0]
	if len(ship.Inputs) != 1 || ship.Inputs[0].Text != "draft two" || ship.Inputs[0].FromNodeID != "fmn_work" || ship.Inputs[0].Feedback != nil {
		t.Fatalf("Ship ran on %+v", ship.Inputs)
	}

	if want := []RunPick{{Seq: verdict.Seq, GateID: "gate_review", NodeID: "fmn_work", Attempt: 2, Candidates: []int{1, 2, 3}, DecidedBy: "fmn_j2", Trigger: PickTriggerWhenSpent}}; !reflect.DeepEqual(status.Picks, want) {
		t.Fatalf("status picks = %+v, want %+v", status.Picks, want)
	}
	attempts, err := store.ProjectRunAttempts(status.RunID, "fmn_work")
	if err != nil {
		t.Fatal(err)
	}
	for _, attempt := range attempts.Steps[0].Attempts {
		if picked := attempt.Picked != nil; picked != (attempt.Attempt == 2) {
			t.Fatalf("attempt %d picked = %+v", attempt.Attempt, attempt.Picked)
		}
	}
	picked := attempts.Steps[0].Attempts[1].Picked
	if picked.GateID != "gate_review" || picked.GateTitle != "Review" || picked.DecidedBy != "fmn_j2" || picked.Reason.Text != "the only draft whose tests pass" || picked.Trigger != PickTriggerWhenSpent || !reflect.DeepEqual(picked.Candidates, []int{1, 2, 3}) {
		t.Fatalf("picked = %+v", picked)
	}
	evidence, err := store.ProjectNodeEvidence(status.RunID, "gate_review")
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Picks) != 1 || len(evidence.Evaluations) != 3 {
		t.Fatalf("Review evidence: %d picks, %d evaluations", len(evidence.Picks), len(evidence.Evaluations))
	}
	if got := evidence.Picks[0]; got.NodeID != "fmn_work" || !reflect.DeepEqual(got.Candidates, []int{1, 2, 3}) || !reflect.DeepEqual(got.JudgeChain, []string{"fmn_j1", "fmn_j2"}) ||
		got.Limit == nil || got.Picked == nil || got.Picked.Attempt != 2 || got.Picked.DecidedBy != "fmn_j2" || len(got.Picked.Evidence) != 1 {
		t.Fatalf("Review's pick evidence = %+v", got)
	}
}

// A stop card blocks as before, offering a grant or a pick; --pick-best then
// picks, and Ship runs on the picked attempt. A plain resume, or both, is
// refused.
func TestPickBestResumesASpentLimitBlock(t *testing.T) {
	responses := threeFailedDrafts("")
	responses["fmn_j1"][3] = "compared the two drafts"
	responses["fmn_j2"][3] = pickBlock(1, "smaller diff")
	store, engine, executor, status := pickRun(t, pickBoardFixture(`["formation"]`, workCard("rounds = 2")), responses)
	if status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrantOrPick || status.BlockCode != RunBlockLimitReached {
		t.Fatalf("status = %+v:\n%s", status, eventTypeTrail(mustEvents(t, store, status.RunID)))
	}
	if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test"}); !errors.Is(err, ErrRunGrantOrPickRequired) {
		t.Fatalf("plain resume = %v", err)
	}
	if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Grant: true, PickBest: true}); !errors.Is(err, ErrRunGrantOrPick) {
		t.Fatalf("grant and pick = %v", err)
	}
	resumed, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", PickBest: true})
	if err != nil || resumed.Status != RunStatusSucceeded {
		t.Fatalf("resume --pick-best = %+v, %v:\n%s", resumed, err, eventTypeTrail(mustEvents(t, store, status.RunID)))
	}
	if got, want := callTrail(executor.calls), []string{"fmn_work#1", "fmn_j1#1", "fmn_j2#1", "fmn_work#2", "fmn_j1#2", "fmn_j2#2", "fmn_j1#3", "fmn_j2#3", "fmn_ship#1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if ship := callsByNode(executor.calls)["fmn_ship"][0]; ship.Inputs[0].Text != "draft one" {
		t.Fatalf("Ship ran on %+v", ship.Inputs)
	}
	if len(resumed.Picks) != 1 || resumed.Picks[0].Trigger != PickTriggerResume || resumed.Picks[0].Attempt != 1 || !reflect.DeepEqual(resumed.Picks[0].Candidates, []int{1, 2}) {
		t.Fatalf("picks = %+v", resumed.Picks)
	}
}

// --pick-best is refused where there is nothing to pick: a spent card whose
// step a code check judges, and a block that is not a spent limit.
func TestPickBestIsRefusedWhereNothingCanPick(t *testing.T) {
	t.Run("a code gate", func(t *testing.T) {
		store, engine, status := loopingGateRun(t, func(store *Store) { addLimit(t, store, "fmn_work", 2) })
		if status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant {
			t.Fatalf("status = %+v:\n%s", status, eventTypeTrail(mustEvents(t, store, status.RunID)))
		}
		if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", PickBest: true}); !errors.Is(err, ErrRunNothingToPick) {
			t.Fatalf("resume --pick-best = %v", err)
		}
	})
	t.Run("a rejected verdict", func(t *testing.T) {
		responses := threeFailedDrafts("")
		responses["fmn_j2"][1] = "```archon-verdict\n{invalid JSON}\n```"
		_, engine, _, status := pickRun(t, pickBoardFixture(`["formation"]`, workCard("rounds = 3\nwhenSpent = \"best\"")), responses)
		if status.Status != RunStatusBlocked || status.BlockCode != "invalid_judge_result" {
			t.Fatalf("status = %+v", status)
		}
		if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", PickBest: true}); !errors.Is(err, ErrRunNothingToPick) {
			t.Fatalf("resume --pick-best = %v", err)
		}
	})
}

// A step's card whose only judged attempt is one is taken without a picker.
func TestTheOnlyJudgedAttemptIsTakenWithoutAPicker(t *testing.T) {
	store, _, executor, status := pickRun(t, pickBoardFixture(`["formation"]`, workCard("rounds = 1\nwhenSpent = \"best\"")), threeFailedDrafts(""))
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v:\n%s", status, eventTypeTrail(mustEvents(t, store, status.RunID)))
	}
	if got, want := callTrail(executor.calls), []string{"fmn_work#1", "fmn_j1#1", "fmn_j2#1", "fmn_ship#1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if len(status.Picks) != 1 || status.Picks[0].DecidedBy != PickDecidedByArchon || status.Picks[0].Attempt != 1 {
		t.Fatalf("picks = %+v", status.Picks)
	}
	verdict := pickVerdicts(mustEvents(t, store, status.RunID))[0]
	if verdict.Data["reason"] != "the only attempt Review judged" {
		t.Fatalf("pick verdict = %+v", verdict)
	}
}

// clockedLabExecutor runs the scripted lab on a test clock: each node's nth
// call takes work[node][n-1], and stops at its deadline as a real seat is
// stopped.
type clockedLabExecutor struct {
	*scriptedLabExecutor
	clock *time.Time
	work  map[string][]time.Duration
}

func (e *clockedLabExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	var took time.Duration
	if script := e.work[req.NodeID]; req.Attempt-1 < len(script) {
		took = script[req.Attempt-1]
	}
	end := e.clock.Add(took)
	if !req.Deadline.IsZero() && !end.Before(req.Deadline) {
		*e.clock = req.Deadline
		return FormationExecutionResult{}, ErrFormationTimeoutExceeded
	}
	*e.clock = end
	return e.scriptedLabExecutor.ExecuteFormation(req)
}

// Work may work 150 s: two drafts take a minute each, and the third runs out
// of time. The cut draft is never a candidate: the judges pick between the
// two they judged, and the attempts show the third stopped.
func TestAnAttemptTheLimitCutIsNeverACandidate(t *testing.T) {
	store, personas := s4RunFixture(t)
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), pickBoardFixture(`["formation"]`, workCard("seconds = 150\nwhenSpent = \"best\"")))
	responses := threeFailedDrafts("")
	responses["fmn_j1"][3] = "compared the two drafts"
	responses["fmn_j2"][3] = pickBlock(2, "covers retry")
	cwd := t.TempDir()
	executor := &clockedLabExecutor{
		scriptedLabExecutor: &scriptedLabExecutor{lab: NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: cwd, Harnesses: []string{"openai-codex"}}), responses: responses},
		clock:               &clock,
		work:                map[string][]time.Duration{"fmn_work": {time.Minute, time.Minute, time.Minute}},
	}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{Cwd: cwd, MissionID: "mis_showcase", Actor: "agent:test"})
	if err != nil {
		t.Fatal(err)
	}
	events := mustEvents(t, store, status.RunID)
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v:\n%s", status, eventTypeTrail(events))
	}
	if got := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("work attempts = %v", got)
	}
	cut := lastEventOfType(t, events, RunEventError)
	if stringFromEventData(cut, "code") != RunBlockLimitReached || cut.NodeID != "fmn_work" {
		t.Fatalf("the cut recorded %+v", cut)
	}
	started := eventsOfType(events, RunEventPickStarted)
	if len(started) != 1 || started[0].Seq < cut.Seq {
		t.Fatalf("pick_started = %+v, cut at %d", started, cut.Seq)
	}
	if limit := limitFromAny(started[0].Data["limit"]); limit == nil || limit.Kind != LimitKindTime {
		t.Fatalf("pick limit = %+v", limit)
	}
	if got := callsByNode(executor.calls)["fmn_j2"]; got[len(got)-1].Attempt != 3 || !reflect.DeepEqual(got[len(got)-1].PickAttempts, []int{1, 2}) {
		t.Fatalf("final judge picked among %v", got[len(got)-1].PickAttempts)
	}
	if ship := callsByNode(executor.calls)["fmn_ship"][0]; ship.Inputs[0].Text != "draft two" {
		t.Fatalf("Ship ran on %+v", ship.Inputs)
	}
	attempts, err := store.ProjectRunAttempts(status.RunID, "fmn_work")
	if err != nil {
		t.Fatal(err)
	}
	third := attempts.Steps[0].Attempts[2]
	if third.Status != AttemptStopped || third.Picked != nil || attempts.Steps[0].Attempts[1].Picked == nil {
		t.Fatalf("attempts = %+v", attempts.Steps[0].Attempts)
	}
}

// humanPickFixture is Work reviewed by the operator, whose fail sends Work
// back, with a two-round best card.
func humanPickFixture() string {
	fixture := strings.Replace(s5HumanGateBoardFixture(), "id = \"edge_gate_fail_rejected\"\nfrom = \"gate_review:fail\"\nto = \"end_rejected:in\"", "id = \"edge_gate_fail_work\"\nfrom = \"gate_review:fail\"\nto = \"fmn_work:port_work_in\"", 1)
	return fixture + "\n" + workCard("rounds = 2\nwhenSpent = \"best\"")
}

// failHumanGate records the operator's fail of the gate's pending verdict and
// continues the run.
func failHumanGate(t *testing.T, engine *RunEngine, store *Store, runID string) {
	t.Helper()
	request, ok := latestHumanRequest(mustEvents(t, store, runID), "gate_review")
	if !ok {
		t.Fatalf("no request:\n%s", eventTypeTrail(mustEvents(t, store, runID)))
	}
	if _, err := engine.RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: "gate_review", RequestedSeq: request.Seq, Verdict: "fail", Reason: "not yet", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ContinueRun(runID); err != nil {
		t.Fatal(err)
	}
}

// At a human gate the operator picks: the pending request names the
// candidates, a verdict or an attempt not offered is refused, and the pick
// goes on through the pass route.
func TestAnOperatorPicksAtAHumanGate(t *testing.T) {
	store, engine, executor, status := pickRun(t, humanPickFixture(), map[string]map[int]string{
		"fmn_work": {1: "draft one", 2: "draft two"}, "fmn_ship": {1: "shipped"},
	})
	failHumanGate(t, engine, store, status.RunID)
	failHumanGate(t, engine, store, status.RunID)
	events := mustEvents(t, store, status.RunID)
	request, ok := latestHumanRequest(events, "gate_review")
	pending := PendingPickOf(request)
	if !ok || pending == nil || pending.NodeID != "fmn_work" || !reflect.DeepEqual(pending.Candidates, []int{1, 2}) || pending.Recommendation != nil {
		t.Fatalf("pending pick = %+v from %+v:\n%s", pending, request, eventTypeTrail(events))
	}
	if !strings.Contains(runInputRefFromAny(request.Data["inputRef"]).Text, "attempt 2\nverdict: fail\nreason: not yet") {
		t.Fatalf("pick request input = %+v", request.Data["inputRef"])
	}
	projected, err := store.ProjectRun(status.RunID)
	if err != nil || projected.Status != RunStatusRunning {
		t.Fatalf("status while the pick waits = %+v, %v", projected, err)
	}
	if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", RequestedSeq: request.Seq, Verdict: "pass", Actor: "human:operator"}); !errors.Is(err, ErrHumanRequestIsAPick) {
		t.Fatalf("verdict on a pick = %v", err)
	}
	if _, err := engine.RecordHumanGatePick(status.RunID, HumanGatePickRequest{GateID: "gate_review", RequestedSeq: request.Seq, Attempt: 3, Actor: "human:operator"}); !errors.Is(err, ErrPickNotACandidate) {
		t.Fatalf("pick of attempt 3 = %v", err)
	}
	if _, err := engine.RecordHumanGatePick(status.RunID, HumanGatePickRequest{GateID: "gate_review", RequestedSeq: request.Seq - 1, Attempt: 1, Actor: "human:operator"}); !errors.Is(err, ErrHumanRequestNotPending) {
		t.Fatalf("pick of a stale request = %v", err)
	}
	if _, err := engine.RecordHumanGatePick(status.RunID, HumanGatePickRequest{GateID: "gate_review", RequestedSeq: request.Seq, Attempt: 1, Reason: "cleaner", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RecordHumanGatePick(status.RunID, HumanGatePickRequest{GateID: "gate_review", RequestedSeq: request.Seq, Attempt: 1, Actor: "human:operator"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a second pick = %v", err)
	}
	final, err := engine.ContinueRun(status.RunID)
	if err != nil || final.Status != RunStatusSucceeded {
		t.Fatalf("after the pick = %+v, %v:\n%s", final, err, eventTypeTrail(mustEvents(t, store, status.RunID)))
	}
	if ship := callsByNode(executor.calls)["fmn_ship"][0]; ship.Inputs[0].Text != "draft one" {
		t.Fatalf("Ship ran on %+v", ship.Inputs)
	}
	if len(final.Picks) != 1 || final.Picks[0].DecidedBy != "human:operator" || final.Picks[0].Attempt != 1 {
		t.Fatalf("picks = %+v", final.Picks)
	}
	verdict := pickVerdicts(mustEvents(t, store, status.RunID))[0]
	if verdict.Data["reason"] != "cleaner" || intFromRunEventData(verdict.Data["requestedSeq"]) != request.Seq {
		t.Fatalf("pick verdict = %+v", verdict)
	}
	evidence, err := store.ProjectNodeEvidence(status.RunID, "gate_review")
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Picks) != 1 || len(evidence.Picks[0].HumanRequests) != 1 || evidence.Picks[0].HumanRequests[0].Pending || evidence.Picks[0].HumanRequests[0].Decision == nil ||
		evidence.Picks[0].HumanRequests[0].Decision.Verdict != "attempt 1" {
		t.Fatalf("Review's pick evidence = %+v", evidence.Picks)
	}
}

// A gate a judge and the operator decide: the judges pick first, which the
// operator sees as a recommendation and may overrule.
func TestTheJudgesRecommendAndTheOperatorPicks(t *testing.T) {
	responses := threeFailedDrafts("")
	responses["fmn_j1"][3] = "compared the two drafts"
	// The judges pass each draft, so the operator decides it.
	responses["fmn_j2"] = map[int]string{1: judgeBlock("pass", "fine"), 2: judgeBlock("pass", "fine too"), 3: pickBlock(2, "covers retry")}
	store, engine, executor, status := pickRun(t, pickBoardFixture(`["formation", "human"]`, workCard("rounds = 2\nwhenSpent = \"best\"")), responses)
	// The operator sends each draft back after the judges.
	for range 2 {
		failHumanGate(t, engine, store, status.RunID)
	}
	events := mustEvents(t, store, status.RunID)
	request, _ := latestHumanRequest(events, "gate_review")
	pending := PendingPickOf(request)
	if pending == nil || pending.Recommendation == nil || pending.Recommendation.Attempt != 2 || pending.Recommendation.DecidedBy != "fmn_j2" || pending.Recommendation.Reason != "covers retry" {
		t.Fatalf("pending pick = %+v:\n%s", pending, eventTypeTrail(events))
	}
	if _, err := engine.RecordHumanGatePick(status.RunID, HumanGatePickRequest{GateID: "gate_review", Attempt: 1, Reason: "smaller", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	final, err := engine.ContinueRun(status.RunID)
	if err != nil || final.Status != RunStatusSucceeded {
		t.Fatalf("after the pick = %+v, %v", final, err)
	}
	if ship := callsByNode(executor.calls)["fmn_ship"][0]; ship.Inputs[0].Text != "draft one" {
		t.Fatalf("Ship ran on %+v", ship.Inputs)
	}
	pick := pickData(pickVerdicts(mustEvents(t, store, status.RunID))[0])
	if recommendation := pickChoiceFromAny(pick["recommendation"]); recommendation == nil || recommendation.Attempt != 2 || pick["decidedBy"] != "human:operator" {
		t.Fatalf("pick = %+v", pick)
	}
}

// A judge's malformed pick blocks the run resumably; a plain resume asks the
// final judge again, and its pick goes on.
func TestAMalformedJudgePickBlocksAndAResumeAsksAgain(t *testing.T) {
	responses := threeFailedDrafts("I would pick the second draft.")
	responses["fmn_j2"][5] = pickBlock(3, "the last draft compiles")
	store, engine, executor, status := pickRun(t, pickBoardFixture(`["formation"]`, workCard("rounds = 3\nwhenSpent = \"best\"")), responses)
	if status.Status != RunStatusBlocked || !status.ResumeAllowed || status.BlockCode != RunBlockInvalidJudgePick || status.ResumePolicy != "" {
		t.Fatalf("status = %+v", status)
	}
	events := mustEvents(t, store, status.RunID)
	failed := lastEventOfType(t, events, RunEventJudgeAttemptFailed)
	if failed.Data["code"] != RunBlockInvalidJudgePick || !strings.Contains(stringFromEventData(failed, "reason"), "archon-pick") {
		t.Fatalf("judge failure = %+v", failed)
	}
	evidence, err := store.ProjectNodeEvidence(status.RunID, "gate_review")
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Picks) != 1 || len(evidence.Picks[0].JudgeFailures) != 1 || evidence.Picks[0].Picked != nil {
		t.Fatalf("pick evidence = %+v", evidence.Picks)
	}
	if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", PickBest: true}); !errors.Is(err, ErrRunNothingToPick) {
		t.Fatalf("resume --pick-best at a malformed pick = %v", err)
	}
	resumed, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test"})
	if err != nil || resumed.Status != RunStatusSucceeded {
		t.Fatalf("resume = %+v, %v:\n%s", resumed, err, eventTypeTrail(mustEvents(t, store, status.RunID)))
	}
	// Only the final judge answers again; Judge 1's comparison stands.
	if got, want := callTrail(executor.calls[9:]), []string{"fmn_j1#4", "fmn_j2#4", "fmn_j2#5", "fmn_ship#1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("calls from the pick = %v, want %v", got, want)
	}
	if ship := callsByNode(executor.calls)["fmn_ship"][0]; ship.Inputs[0].Text != "draft three" {
		t.Fatalf("Ship ran on %+v", ship.Inputs)
	}
}

// A run stopped anywhere inside a pick finishes it from what the ledger
// holds: replay after pick_started, after the first judge, or at the
// operator's request reaches the same pick, and no judge answers twice.
func TestReplayFinishesAPickTheLedgerStarted(t *testing.T) {
	for _, stop := range []string{RunEventPickStarted, "fmn_j1"} {
		t.Run("after "+stop, func(t *testing.T) {
			responses := threeFailedDrafts(pickBlock(2, "covers retry"))
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), pickBoardFixture(`["formation"]`, workCard("rounds = 3\nwhenSpent = \"best\"")))
			cwd := t.TempDir()
			executor := &scriptedLabExecutor{lab: NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: cwd, Harnesses: []string{"openai-codex"}}), responses: responses}
			// The run stops as its pick reaches the stop: the judge's next
			// call fails as a crash would leave it.
			crash := &crashingExecutor{inner: executor, stopAt: stop, store: store}
			engine := NewRunEngine(store, personas, crash)
			status, err := engine.RunMission("session-search", RunStartRequest{Cwd: cwd, MissionID: "mis_showcase", Actor: "agent:test"})
			if err != nil {
				t.Fatal(err)
			}
			if status.Status != RunStatusBlocked {
				t.Fatalf("status = %+v:\n%s", status, eventTypeTrail(mustEvents(t, store, status.RunID)))
			}
			crash.stopAt = ""
			resumed, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach"})
			if err != nil || resumed.Status != RunStatusSucceeded {
				t.Fatalf("resume = %+v, %v:\n%s", resumed, err, eventTypeTrail(mustEvents(t, store, status.RunID)))
			}
			events := mustEvents(t, store, status.RunID)
			if len(eventsOfType(events, RunEventPickStarted)) != 1 || len(pickVerdicts(events)) != 1 || resumed.Picks[0].Attempt != 2 {
				t.Fatalf("picks = %+v:\n%s", resumed.Picks, eventTypeTrail(events))
			}
			if ship := callsByNode(executor.calls)["fmn_ship"][0]; ship.Inputs[0].Text != "draft two" {
				t.Fatalf("Ship ran on %+v", ship.Inputs)
			}
			answered := map[string]int{}
			for _, event := range events {
				if event.Type == RunEventNodeOutput {
					answered[event.NodeID]++
				}
			}
			if answered["fmn_j1"] != 4 || answered["fmn_j2"] != 4 {
				t.Fatalf("judge outputs = %v", answered)
			}
		})
	}
}

// crashingExecutor fails the first call that would run past stopAt: the
// first judge of the pick once pick_started is recorded, or the final judge
// once the first judge of the pick answered.
type crashingExecutor struct {
	inner  FormationExecutor
	store  *Store
	stopAt string
}

func (e *crashingExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	if len(req.PickAttempts) > 0 && ((e.stopAt == RunEventPickStarted && req.NodeID == "fmn_j1") || (e.stopAt == "fmn_j1" && req.NodeID == "fmn_j2")) {
		return FormationExecutionResult{}, errors.New("the seat died")
	}
	return e.inner.ExecuteFormation(req)
}

// Validation names a best card that cannot pick, and admission refuses it: a
// mission's card, a step no gate judges and sends back, a gate only code
// checks, two such gates, and a value other than stop or best.
func TestValidationNamesABestCardThatCannotPick(t *testing.T) {
	judged := pickBoardFixture(`["formation"]`, "")
	secondGate := `
[[gate]]
id = "gate_two"
title = "Second review"
kinds = ["human"]
criterion = "Also good"

[[connection]]
id = "edge_work_two"
from = "fmn_work:port_work_out"
to = "gate_two:in"

[[connection]]
id = "edge_two_back"
from = "gate_two:fail"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_two_done"
from = "gate_two:pass"
to = "end_done:in"
`
	card := func(target, whenSpent string) string {
		return "[[limit]]\nid = \"lim_a\"\ntitle = \"Cap\"\ntarget = \"" + target + "\"\nrounds = 3\nwhenSpent = \"" + whenSpent + "\"\n"
	}
	for _, tc := range []struct{ name, toml, message string }{
		{"a mission card", judged + card("mis_showcase", "best"), "Limit Cap picks the best attempt when spent, but it covers the whole mission: only a step's card can pick; set it to stop"},
		{"no selecting gate", judged + card("fmn_ship", "best"), "Limit Cap picks the best attempt of Ship when spent, but no gate judges it and sends it back: wire a judge or human gate that only it feeds and whose fail returns to it, or set it to stop"},
		{"a code gate", strings.Replace(judged, `kinds = ["formation"]`, `kinds = ["code"]`, 1) + card("fmn_work", "best"), "Limit Cap picks the best attempt of Work when spent, but its gate Review checks with code only, which cannot compare attempts: give it a judge or a human, or set it to stop"},
		{"two selecting gates", judged + secondGate + card("fmn_work", "best"), "Limit Cap picks the best attempt of Work when spent, but Review and Second review both judge it and send it back, so neither can pick: keep one, or set it to stop"},
		{"another value", judged + card("fmn_work", "never"), `Limit Cap holds whenSpent = "never": set it to stop or best`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			board, err := parseBoard([]byte(tc.toml))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, finding := range ValidateBoard(board).Errors {
				found = found || finding.Code == FindingInvalidLimit && finding.Message == tc.message
			}
			if !found {
				t.Fatalf("findings = %+v, want %q", ValidateBoard(board).Errors, tc.message)
			}
			var admission *RunAdmissionError
			if err := CheckRunAdmission(board, nil, RunAdmissionScope{MissionID: "mis_showcase"}); !errors.As(err, &admission) || !strings.Contains(admission.Error(), tc.message) {
				t.Fatalf("admission = %v", err)
			}
		})
	}
	for _, whenSpent := range []string{"best", "stop"} {
		board, err := parseBoard([]byte(judged + card("fmn_work", whenSpent)))
		if err != nil {
			t.Fatal(err)
		}
		for _, finding := range ValidateBoard(board).Errors {
			if finding.Code == FindingInvalidLimit {
				t.Fatalf("whenSpent %s: %+v", whenSpent, finding)
			}
		}
		if want := map[string]string{"best": "best", "stop": ""}[whenSpent]; board.Limits[0].WhenSpent != want {
			t.Fatalf("whenSpent %s decoded as %q", whenSpent, board.Limits[0].WhenSpent)
		}
	}
}

// whenSpent is written as best and cleared by stop; any other value is
// refused.
func TestWhenSpentAuthoring(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), pickBoardFixture(`["formation"]`, ""))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateLimit("session-search", LimitCreateRequest{Target: "fmn_work", Rounds: 3, WhenSpent: "always"}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("create with whenSpent always = %v", err)
	}
	created, err := store.CreateLimit("session-search", LimitCreateRequest{Target: "fmn_work", Rounds: 3, WhenSpent: "best"}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	board = created.Board
	if created.Limit.WhenSpent != LimitWhenSpentBest || !strings.Contains(board.TOML, "whenSpent = \"best\"\n") || len(ValidateBoard(board).Errors) != 0 {
		t.Fatalf("created %+v:\n%s", created.Limit, board.TOML)
	}
	stop := "stop"
	updated, err := store.UpdateLimit("session-search", LimitUpdateRequest{LimitID: created.Limit.ID, WhenSpent: &stop}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(updated.TOML, "whenSpent") || updated.Limits[0].WhenSpent != "" {
		t.Fatalf("stop kept whenSpent:\n%s", updated.TOML)
	}
	bad := "later"
	if _, err := store.UpdateLimit("session-search", LimitUpdateRequest{LimitID: created.Limit.ID, WhenSpent: &bad}, WriteOptions{ExpectedETag: updated.ETag, ExpectedRev: updated.Rev}); !errors.Is(err, ErrInvalidLimit) {
		t.Fatalf("update with whenSpent later = %v", err)
	}
}

// The picked attempt is the one the run carried to its done End, so it is
// the mission's output, not the latest attempt.
func TestAPickedAttemptIsTheMissionsOutput(t *testing.T) {
	fixture := strings.Replace(pickBoardFixture(`["formation"]`, workCard("rounds = 3\nwhenSpent = \"best\"")), "beadId = \"home-7kc4.7\"\n",
		"beadId = \"home-7kc4.7\"\noutputs = [{ name = \"draft\", from = \"fmn_work:port_work_out\", required = true }]\n", 1)
	store, _, _, status := pickRun(t, fixture, threeFailedDrafts(pickBlock(2, "covers retry")))
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v:\n%s", status, eventTypeTrail(mustEvents(t, store, status.RunID)))
	}
	if want := []RunOutput{{Name: "draft", Kind: MissionInputText, Value: "draft two", NodeID: "fmn_work", PortID: "port_work_out", Attempt: 2}}; !reflect.DeepEqual(status.Outputs, want) {
		t.Fatalf("outputs = %+v, want %+v", status.Outputs, want)
	}
}
