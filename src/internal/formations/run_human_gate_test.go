package formations

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestS5HumanGateRequestsInputAndWaits(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGateBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"pass"}})

	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	if status.Status != RunStatusRunning || status.Final {
		t.Fatalf("status = %+v, want running non-final while waiting for human verdict", status)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_work"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes = %v, want work only before human verdict", got)
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	request := eventOfType(t, events, RunEventHumanInputRequested)
	if request.GateID != "gate_review" || request.NodeID != "gate_review" {
		t.Fatalf("human request envelope = %+v, want gate_review", request)
	}
	if request.Data["prompt"] != "Good enough to ship" || request.Data["requestedBy"] != "gate_review" {
		t.Fatalf("human request data = %#v, want gate criterion prompt", request.Data)
	}
	for _, event := range events {
		if event.Type == RunEventGateVerdict {
			t.Fatalf("gate verdict recorded before human decision: %+v", event)
		}
	}
}

func TestS5HumanGateVerdictIsRoutedWhenTheRunContinues(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGateBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"pass"}})

	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	executor.calls = nil
	status, err = engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{
		GateID:  "gate_review",
		Verdict: "pass",
		Reason:  "direction is right",
		Actor:   "human:operator",
	})
	if err != nil {
		t.Fatalf("record human verdict: %v", err)
	}
	// Recording the verdict routes nothing and blocks nothing; the run's
	// worker routes it when the run continues (archon-o7p.11).
	if status.Status != RunStatusRunning || status.Final {
		t.Fatalf("status = %+v, want running after the pass verdict", status)
	}
	if got := executor.nodeIDs(); len(got) != 0 {
		t.Fatalf("executor nodes after verdict = %v, want no downstream dispatch yet", got)
	}
	for _, event := range readRunEvents(t, findOnlyRunLedger(t, store, "session-search")) {
		if event.Type == RunEventGateVerdict || event.Type == RunEventBlocked {
			t.Fatalf("recording the verdict appended %s", event.Type)
		}
	}
	status, err = engine.ContinueRun(status.RunID)
	if err != nil {
		t.Fatalf("continue after human verdict: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded after the pass wire runs", status)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_ship"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes after the verdict = %v, want only downstream ship", got)
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	verdict := eventOfType(t, events, RunEventHumanVerdictRecorded)
	if verdict.GateID != "gate_review" || verdict.Data["verdict"] != "pass" || verdict.Data["reason"] != "direction is right" || verdict.Data["decidedBy"] != "human:operator" {
		t.Fatalf("human verdict event = %+v, want pass reason/actor", verdict)
	}
	gateVerdict := eventOfType(t, events, RunEventGateVerdict)
	if gateVerdict.Data["verdict"] != "pass" || gateVerdict.Data["routePort"] != "pass" {
		t.Fatalf("gate verdict = %+v, want human pass routed through pass wire", gateVerdict)
	}
	if eventsContainType(events, RunEventResumed) || eventsContainType(events, RunEventBlocked) {
		t.Fatalf("events = %v, want no block or resume around the verdict", eventTypes(events))
	}
}

func TestS5HumanGatePassToUnderfedJoinBlocksWithoutFinalSuccess(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGateUnderfedJoinBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	engine := NewRunEngine(store, personas, &fakeRunExecutor{})

	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	status, err = engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{
		GateID:  "gate_review",
		Verdict: "pass",
		Reason:  "draft is usable",
		Actor:   "human:operator",
	})
	if err != nil {
		t.Fatalf("record human verdict: %v", err)
	}
	status, err = engine.ContinueRun(status.RunID)
	if err != nil {
		t.Fatalf("continue after human verdict: %v", err)
	}
	if status.Status != RunStatusBlocked || status.Final {
		t.Fatalf("status = %+v, want blocked non-final for underfed join", status)
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	if events[len(events)-1].Type != RunEventBlocked {
		t.Fatalf("last event = %s, want run_blocked instead of final success over underfed join", events[len(events)-1].Type)
	}
	if lastEventOfType(t, events, RunEventBlocked).Data["code"] != "reachable_node_starved" {
		t.Fatalf("last block = %+v, want reachable_node_starved", lastEventOfType(t, events, RunEventBlocked))
	}
}

func s5HumanGateBoardFixture() string {
	return s4MissionOnlyBoardFixture() + `
[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.input]]
id = "port_work_in"
label = "Input"

[[formation.output]]
id = "port_work_out"
label = "Output"

[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "Good enough to ship"

[[formation]]
id = "fmn_ship"
type = "solo"
title = "Ship"

[[formation.input]]
id = "port_ship_in"
label = "Input"

[[formation.output]]
id = "port_ship_out"
label = "Output"

[[formation.slot]]
id = "slot_ship"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[connection]]
id = "edge_mission_work"
from = "mis_showcase:out"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_work_gate"
from = "fmn_work:port_work_out"
to = "gate_review:in"

[[connection]]
id = "edge_gate_pass_ship"
from = "gate_review:pass"
to = "fmn_ship:port_ship_in"

[[connection]]
id = "edge_ship_done"
from = "fmn_ship:port_ship_out"
to = "end_done:in"

[[connection]]
id = "edge_gate_fail_rejected"
from = "gate_review:fail"
to = "end_rejected:in"
` + branchingBoardEnds()
}

func s5HumanGateUnderfedJoinBoardFixture() string {
	return s4MissionOnlyBoardFixture() + `
[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.input]]
id = "port_work_in"
label = "Input"

[[formation.output]]
id = "port_work_out"
label = "Output"

[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "Good enough to ship"

[[formation]]
id = "fmn_join"
type = "solo"
title = "Join"

[[formation.input]]
id = "port_join_left"
label = "Left"

[[formation.input]]
id = "port_join_right"
label = "Right"

[[formation.output]]
id = "port_join_out"
label = "Output"

[[formation.slot]]
id = "slot_join"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[connection]]
id = "edge_mission_work"
from = "mis_showcase:out"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_work_gate"
from = "fmn_work:port_work_out"
to = "gate_review:in"

[[connection]]
id = "edge_gate_pass_join"
from = "gate_review:pass"
to = "fmn_join:port_join_left"

[[connection]]
id = "edge_join_done"
from = "fmn_join:port_join_out"
to = "end_done:in"

[[connection]]
id = "edge_gate_fail_rejected"
from = "gate_review:fail"
to = "end_rejected:in"
` + branchingBoardEnds()
}

func s5HumanGatePushbackBoardFixture() string {
	return s4MissionOnlyBoardFixture() + `
[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.input]]
id = "port_work_in"
label = "Input"

[[formation.output]]
id = "port_work_out"
label = "Output"

[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "Good enough to ship"

[[formation]]
id = "fmn_ship"
type = "solo"
title = "Ship"

[[formation.input]]
id = "port_ship_in"
label = "Input"

[[formation.output]]
id = "port_ship_out"
label = "Output"

[[formation.slot]]
id = "slot_ship"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
effort = "medium"

[[connection]]
id = "edge_mission_work"
from = "mis_showcase:out"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_work_gate"
from = "fmn_work:port_work_out"
to = "gate_review:in"

[[connection]]
id = "edge_gate_fail_work"
from = "gate_review:fail"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_gate_pass_ship"
from = "gate_review:pass"
to = "fmn_ship:port_ship_in"

[[connection]]
id = "edge_ship_done"
from = "fmn_ship:port_ship_out"
to = "end_done:in"
` + branchingBoardEnds()
}

func TestS5HumanGateFailPushbackReDispatchesWork(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGatePushbackBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)

	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	if status.Status != RunStatusRunning || status.Final {
		t.Fatalf("status = %+v, want running while waiting for first human verdict", status)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_work"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes before verdict = %v, want work only", got)
	}

	status, err = engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{
		GateID:  "gate_review",
		Verdict: "fail",
		Reason:  "revise the draft",
		Actor:   "human:operator",
	})
	if err != nil {
		t.Fatalf("record fail verdict: %v", err)
	}
	if status.Status != RunStatusRunning || status.Final {
		t.Fatalf("status = %+v, want running after the fail verdict", status)
	}

	executor.calls = nil
	status, err = engine.ContinueRun(status.RunID)
	if err != nil {
		t.Fatalf("continue after fail pushback: %v", err)
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	for _, ev := range events {
		if ev.Type == RunEventError && ev.Data["code"] == "resume_no_work" {
			t.Fatalf("resume recorded resume_no_work; fail-routed work was not re-queued")
		}
	}
	if got, want := executor.nodeIDs(), []string{"fmn_work"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes after fail-pushback resume = %v, want work re-dispatched", got)
	}

	feedback := executor.calls[0].Inputs[0].Feedback
	if feedback == nil || feedback.GateID != "gate_review" || feedback.GateAttempt != 1 || feedback.Verdict != "fail" || feedback.Reason != "revise the draft" || feedback.OriginalText != "output from fmn_work" || feedback.OriginalRef == "" {
		t.Fatalf("human pushback feedback = %+v", feedback)
	}

	status, err = engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{
		GateID:  "gate_review",
		Verdict: "pass",
		Reason:  "looks good now",
		Actor:   "human:operator",
	})
	if err != nil {
		t.Fatalf("record pass verdict: %v", err)
	}
	executor.calls = nil
	status, err = engine.ContinueRun(status.RunID)
	if err != nil {
		t.Fatalf("continue after pass: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded after revise->approve->ship", status)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_ship"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes after the approval = %v, want ship", got)
	}
}

type seatLossOnceExecutor struct {
	fakeRunExecutor
	failNodeID string
	failed     bool
}

func (e *seatLossOnceExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	if req.NodeID == e.failNodeID && !e.failed {
		e.failed = true
		e.calls = append(e.calls, req)
		return FormationExecutionResult{}, &RunExecutionError{Code: "native_turn_failed", Message: "seat died", Boundary: "executor", NodeID: req.NodeID}
	}
	return e.fakeRunExecutor.ExecuteFormation(req)
}

func startHumanGateRun(t *testing.T) (*Store, *PersonaStore, string) {
	t.Helper()
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGatePushbackBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	status, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	return store, personas, status.RunID
}

func TestHumanGatePassResponseReachesDownstreamPromptAcrossRestarts(t *testing.T) {
	store, personas, runID := startHumanGateRun(t)
	answer := "  " + strings.Repeat("Päätös: inspect the repository before drafting.\r\n", 120) + "\n"
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "pass", Reason: answer, Actor: "human:operator",
	}); err != nil {
		t.Fatalf("record pass verdict: %v", err)
	}
	// Each step uses a fresh engine, as a daemon restart would. The first
	// dispatch of Ship loses its seat; the resume replays the ledger again.
	first := &seatLossOnceExecutor{failNodeID: "fmn_ship"}
	status, err := NewRunEngine(store, personas, first).ContinueRun(runID)
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if status.Status != RunStatusBlocked || !status.ResumeAllowed {
		t.Fatalf("status after seat loss = %+v, want resumable block", status)
	}
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	request := eventOfType(t, events, RunEventHumanInputRequested)
	if got := intFromRunEventData(lastEventOfType(t, events, RunEventGateVerdict).Data["requestedSeq"]); got != request.Seq {
		t.Fatalf("gate verdict requestedSeq = %d, want %d", got, request.Seq)
	}
	second := &fakeRunExecutor{}
	status, err = NewRunEngine(store, personas, second).ResumeRun(runID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "seat replaced"})
	if err != nil {
		t.Fatalf("second resume: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status after second resume = %+v, want succeeded", status)
	}

	want := GateResponse{GateID: "gate_review", GateAttempt: 1, RequestedSeq: request.Seq, DecidedBy: "human:operator", Text: answer}
	for name, calls := range map[string][]FormationExecution{"first": first.calls, "replayed": second.calls} {
		if len(calls) != 1 || calls[0].NodeID != "fmn_ship" || len(calls[0].Inputs) != 1 {
			t.Fatalf("%s calls = %+v, want one Ship dispatch", name, calls)
		}
		input := calls[0].Inputs[0]
		if input.Response == nil || *input.Response != want {
			t.Fatalf("%s response = %+v, want %+v", name, input.Response, want)
		}
		if input.Text != "output from fmn_work" || input.FromNodeID != "fmn_work" || input.ToPortID != "port_ship_in" || input.Feedback != nil {
			t.Fatalf("%s input = %+v, want Work output with the response", name, input)
		}
		card := PersonaCard{ID: "scout"}
		variant := HarnessVariant{ID: "openai-codex"}
		for executor, prompt := range map[string]string{
			"lab":  (&LabFormationExecutor{config: LabExecutorConfig{Cwd: t.TempDir()}}).renderPrompt(calls[0], FormationSlot{ID: "slot_ship"}, card, variant),
			"tmux": (&TmuxFormationExecutor{config: TmuxExecutorConfig{Cwd: t.TempDir()}}).renderPromptWithContext(calls[0], FormationSlot{ID: "slot_ship"}, card, variant, "", nil),
		} {
			section := "human response from gate_review, attempt 1:\nverdict: pass\ndecided by: human:operator\nresponse:\n" + answer + "\n"
			if !strings.Contains(prompt, section) || strings.Index(prompt, "input: output from fmn_work") > strings.Index(prompt, section) {
				t.Fatalf("%s %s prompt lacks the response after the original input:\n%s", name, executor, prompt)
			}
		}
	}
	events, err = store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	started := 0
	for _, event := range events {
		if event.Type != RunEventNodeStarted || event.NodeID != "fmn_ship" {
			continue
		}
		started++
		refs, _ := event.Data["inputRefs"].([]any)
		if len(refs) != 1 || runInputRefFromAny(refs[0]).Response == nil || *runInputRefFromAny(refs[0]).Response != want {
			t.Fatalf("durable Ship inputRefs = %#v, want the response", event.Data["inputRefs"])
		}
	}
	if started != 2 {
		t.Fatalf("Ship node_started count = %d, want 2", started)
	}
}

func TestHumanGateEmptyPassResponseRoutesInputUnchanged(t *testing.T) {
	store, personas, runID := startHumanGateRun(t)
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "pass", Reason: "", Actor: "human:operator",
	}); err != nil {
		t.Fatalf("record pass verdict: %v", err)
	}
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	want := runInputRefFromAny(eventOfType(t, events, RunEventHumanInputRequested).Data["inputRef"])
	want.ToPortID = "port_ship_in"
	executor := &fakeRunExecutor{}
	if _, err := NewRunEngine(store, personas, executor).ContinueRun(runID); err != nil {
		t.Fatalf("continue: %v", err)
	}
	if len(executor.calls) != 1 || !reflect.DeepEqual(executor.calls[0].Inputs, []RunInputRef{want}) {
		t.Fatalf("Ship inputs = %+v, want unchanged gate input %+v", executor.calls, want)
	}
	prompt := (&LabFormationExecutor{config: LabExecutorConfig{Cwd: t.TempDir()}}).renderPrompt(executor.calls[0], FormationSlot{ID: "slot_ship"}, PersonaCard{ID: "scout"}, HarnessVariant{ID: "openai-codex"})
	if strings.Contains(prompt, "human response") {
		t.Fatalf("empty response rendered a response section:\n%s", prompt)
	}
}

func TestHumanGateFailResponseStaysFeedback(t *testing.T) {
	answer := "\n  " + strings.Repeat("Korjaus: answer question 3 first.\r\n", 150) + "\n\n"
	store, personas, runID := startHumanGateRun(t)
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "fail", Reason: answer, Actor: "human:operator",
	}); err != nil {
		t.Fatalf("record fail verdict: %v", err)
	}
	executor := &fakeRunExecutor{}
	if _, err := NewRunEngine(store, personas, executor).ContinueRun(runID); err != nil {
		t.Fatalf("continue: %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].NodeID != "fmn_work" {
		t.Fatalf("calls = %+v, want Work pushback", executor.calls)
	}
	input := executor.calls[0].Inputs[0]
	if input.Response != nil || input.Feedback == nil || input.Feedback.Reason != answer || input.Feedback.Verdict != "fail" {
		t.Fatalf("pushback input = %+v, want feedback only", input)
	}
}

func TestHumanGatePassResponseRequiresMatchingLedgerVerdict(t *testing.T) {
	store, personas, runID := startHumanGateRun(t)
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "pass", Reason: "ship it", Actor: "human:operator",
	}); err != nil {
		t.Fatalf("record pass verdict: %v", err)
	}
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).ContinueRun(runID); err != nil {
		t.Fatalf("route pass verdict: %v", err)
	}
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	verdict := lastEventOfType(t, events, RunEventGateVerdict)
	request := eventOfType(t, events, RunEventHumanInputRequested)
	input := runInputRefFromAny(verdict.Data["inputRef"])
	if next, err := gatePassInput(events, verdict, "gate_review", input); err != nil || next.Response == nil || next.Response.Text != "ship it" {
		t.Fatalf("matching ledger = %+v, %v", next, err)
	}
	cases := map[string]func([]RunEvent, RunEvent) ([]RunEvent, RunEvent){
		"request is not a human request": func(events []RunEvent, verdict RunEvent) ([]RunEvent, RunEvent) {
			verdict.Data = cloneEventData(verdict.Data)
			verdict.Data["requestedSeq"] = request.Seq - 1
			return events, verdict
		},
		"request is later than the verdict": func(events []RunEvent, verdict RunEvent) ([]RunEvent, RunEvent) {
			verdict.Data = cloneEventData(verdict.Data)
			verdict.Data["requestedSeq"] = verdict.Seq
			return events, verdict
		},
		"recorded verdict contradicts pass": func(events []RunEvent, verdict RunEvent) ([]RunEvent, RunEvent) {
			events = append([]RunEvent(nil), events...)
			for i, event := range events {
				if event.Type == RunEventHumanVerdictRecorded {
					events[i].Data = cloneEventData(event.Data)
					events[i].Data["verdict"] = "fail"
				}
			}
			return events, verdict
		},
		"no recorded verdict": func(events []RunEvent, verdict RunEvent) ([]RunEvent, RunEvent) {
			events = append([]RunEvent(nil), events...)
			for i, event := range events {
				if event.Type == RunEventHumanVerdictRecorded {
					events[i].Type = RunEventBlocked
				}
			}
			return events, verdict
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			mutatedEvents, mutatedVerdict := mutate(events, verdict)
			if _, err := gatePassInput(mutatedEvents, mutatedVerdict, "gate_review", input); !errors.Is(err, ErrRunLedgerInvalid) {
				t.Fatalf("err = %v, want ErrRunLedgerInvalid", err)
			}
		})
	}
}

func cloneEventData(data map[string]any) map[string]any {
	clone := make(map[string]any, len(data))
	for key, value := range data {
		clone[key] = value
	}
	return clone
}

func TestLabBriefCarriesHumanGateResponse(t *testing.T) {
	store, personas, runID := startHumanGateRun(t)
	answer := "Q1: keep the CLI.\nQ2: email on every block."
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "pass", Reason: answer, Actor: "human:operator",
	}); err != nil {
		t.Fatalf("record pass verdict: %v", err)
	}
	lab := NewLabFormationExecutor(store, personas, LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: store.Workspace})
	status, err := NewRunEngine(store, personas, lab).ContinueRun(runID)
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, want succeeded", status)
	}
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := lastEventOfType(t, events, RunEventSlotDispatch)
	path := stringFromEventData(dispatch, "briefPath")
	if dispatch.NodeID != "fmn_ship" || filepath.Dir(path) != filepath.Join(store.Workspace, "briefs") {
		t.Fatalf("Ship dispatch = %+v, want a brief under the workspace", dispatch)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("brief mode = %v, %v; want 0600 like seat briefs", info, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if brief := string(raw); !strings.Contains(brief, "input: output from fmn_work\n") || !strings.Contains(brief, "response:\n"+answer+"\n") || stringFromEventData(dispatch, "promptSha256") != etag(raw) {
		t.Fatalf("Ship brief lacks the routed response:\n%s", brief)
	}
}

func TestHumanGateVerdictRecordsWhoRelayedIt(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGateBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	engine := NewRunEngine(store, personas, &fakeRunExecutor{})
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	ledger := findOnlyRunLedger(t, store, "session-search")
	before := len(readRunEvents(t, ledger))
	for _, invalid := range []string{"-slot", "_slot", "slot work", "slot/work", "slot.work", strings.Repeat("s", 65)} {
		_, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator", RelayedBy: invalid})
		if !errors.Is(err, ErrInvalidRelayedBy) {
			t.Fatalf("relayedBy %q: %v, want ErrInvalidRelayedBy", invalid, err)
		}
	}
	if after := len(readRunEvents(t, ledger)); after != before {
		t.Fatalf("rejected relayedBy appended %d events", after-before)
	}
	if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "pass", Reason: "Ship it", Actor: "human:operator", RelayedBy: "slot_01M2QBT0QAHHN0T8KFWC9VVNRS",
	}); err != nil {
		t.Fatalf("record relayed verdict: %v", err)
	}
	verdict := eventOfType(t, readRunEvents(t, ledger), RunEventHumanVerdictRecorded)
	if verdict.Data["decidedBy"] != "human:operator" || verdict.Data["relayedBy"] != "slot_01M2QBT0QAHHN0T8KFWC9VVNRS" || verdict.Actor != "human:operator" {
		t.Fatalf("relayed verdict event = %+v", verdict)
	}
	evidence, err := store.ProjectNodeEvidence(status.RunID, "gate_review")
	if err != nil {
		t.Fatal(err)
	}
	decision := evidence.Evaluations[0].HumanRequests[0].Decision
	if decision == nil || decision.DecidedBy != "human:operator" || decision.RelayedBy != "slot_01M2QBT0QAHHN0T8KFWC9VVNRS" || decision.Response.Text != "Ship it" {
		t.Fatalf("evidence decision = %+v", decision)
	}
}

// One slot ID rule holds where a slot is authored, admitted and named by a
// relayed verdict, so a slot a run admits can always relay its own gate
// decision (archon-1ds).
func TestSlotIDRuleIsTheSameForAuthoringAdmissionAndRelays(t *testing.T) {
	longest := "w" + strings.Repeat("0", 63)
	for id, want := range map[string]bool{longest: true, longest + "0": false, "slot.work": false, "_slot": false, "slot-work_1": true} {
		if ValidSlotID(id) != want {
			t.Fatalf("ValidSlotID(%q) = %t, want %t", id, !want, want)
		}
		if err := ValidateRelayedBy(id); (err == nil) != want {
			t.Fatalf("ValidateRelayedBy(%q) = %v, want accepted %t", id, err, want)
		}
		if _, bad := firstBadSlotID([]FormationSlot{{ID: id}}); bad == want {
			t.Fatalf("authoring a slot %q: refused %t, want %t", id, bad, !want)
		}
	}

	start := func(t *testing.T, slotID string) (*RunEngine, *RunStatusProjection, error) {
		t.Helper()
		store, personas := s4RunFixture(t)
		store.Now = fixedClock()
		personas.Now = fixedClock()
		createS4Persona(t, personas, "scout")
		writeFixture(t, store.BoardPath("session-search"), strings.Replace(s5HumanGateBoardFixture(), `id = "slot_work"`, `id = "`+slotID+`"`, 1))
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatalf("read board: %v", err)
		}
		if err := CheckRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_showcase"}); err != nil {
			return nil, nil, err
		}
		engine := NewRunEngine(store, personas, &fakeRunExecutor{})
		status, err := engine.RunMission("session-search", RunStartRequest{
			MissionID:         "mis_showcase",
			Actor:             "agent:test",
			ExpectedBoardETag: board.ETag,
			ExpectedBoardRev:  board.Rev,
		})
		return engine, status, err
	}

	_, _, err := start(t, strings.Repeat("s", 88))
	var admission *RunAdmissionError
	if !errors.As(err, &admission) || !hasFindingCode(admission.Findings, FindingInvalidSlotID) {
		t.Fatalf("admitting an 88-character slot id = %v, want an invalid_slot_id finding", err)
	}

	engine, status, err := start(t, longest)
	if err != nil {
		t.Fatalf("admit the longest slot id: %v", err)
	}
	if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "pass", Reason: "Ship it", Actor: "human:operator", RelayedBy: longest,
	}); err != nil {
		t.Fatalf("the admitted slot relays its own gate decision: %v", err)
	}
}

func hasFindingCode(findings []BoardFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
