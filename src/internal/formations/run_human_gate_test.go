package formations

import (
	"errors"
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
		Limits:            RunLimits{MaxDispatch: 5, MaxAttempts: 2},
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

func TestS5HumanGateVerdictRequiresResumeToDispatchPassWire(t *testing.T) {
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
		Limits:            RunLimits{MaxDispatch: 5, MaxAttempts: 2},
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
	if status.Status != RunStatusBlocked || status.Final || !status.ResumeAllowed {
		t.Fatalf("status = %+v, want resumable block after pass verdict", status)
	}
	if got := executor.nodeIDs(); len(got) != 0 {
		t.Fatalf("executor nodes after verdict = %v, want no downstream dispatch before resume", got)
	}
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{
		Actor:  "agent:test",
		Mode:   "reattach",
		Reason: "human gate approved",
	})
	if err != nil {
		t.Fatalf("resume after human verdict: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded after resume dispatches pass wire", status)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_ship"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes after resume = %v, want only downstream ship", got)
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
	if !eventsContainType(events, RunEventResumed) {
		t.Fatalf("events = %v, want run_resumed before downstream dispatch", eventTypes(events))
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
		Limits:            RunLimits{MaxDispatch: 5, MaxAttempts: 2},
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
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{
		Actor:  "agent:test",
		Mode:   "reattach",
		Reason: "human gate approved",
	})
	if err != nil {
		t.Fatalf("resume after human verdict: %v", err)
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
`
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
`
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
`
}

func TestS5HumanGateFailPushbackResumeReDispatchesWork(t *testing.T) {
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
		Limits:            RunLimits{MaxDispatch: 8, MaxAttempts: 3},
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
	if status.Status != RunStatusBlocked || !status.ResumeAllowed {
		t.Fatalf("status = %+v, want resumable block after fail verdict", status)
	}

	executor.calls = nil
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "revise"})
	if err != nil {
		t.Fatalf("resume after fail pushback: %v", err)
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
	status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "approved"})
	if err != nil {
		t.Fatalf("resume after pass: %v", err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("status = %+v, want succeeded after revise->approve->ship", status)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_ship"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes after approve resume = %v, want ship", got)
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
		Limits:            RunLimits{MaxDispatch: 8, MaxAttempts: 3},
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	return store, personas, status.RunID
}

func TestHumanGatePassResponseReachesDownstreamPromptAcrossRestarts(t *testing.T) {
	store, personas, runID := startHumanGateRun(t)
	answer := "1. Use Postgres.\n2. Ship on Friday."
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "pass", Reason: "  " + answer + "\n", Actor: "human:operator",
	}); err != nil {
		t.Fatalf("record pass verdict: %v", err)
	}
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	request := eventOfType(t, events, RunEventHumanInputRequested)
	if got := intFromRunEventData(lastEventOfType(t, events, RunEventGateVerdict).Data["requestedSeq"]); got != request.Seq {
		t.Fatalf("gate verdict requestedSeq = %d, want %d", got, request.Seq)
	}

	// Each resume uses a fresh engine, as a daemon restart would. The first
	// dispatch of Ship loses its seat; the next resume replays the ledger again.
	first := &seatLossOnceExecutor{failNodeID: "fmn_ship"}
	status, err := NewRunEngine(store, personas, first).ResumeRun(runID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "approved"})
	if err != nil {
		t.Fatalf("first resume: %v", err)
	}
	if status.Status != RunStatusBlocked || !status.ResumeAllowed {
		t.Fatalf("status after seat loss = %+v, want resumable block", status)
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
		GateID: "gate_review", Verdict: "pass", Reason: " \n", Actor: "human:operator",
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
	if _, err := NewRunEngine(store, personas, executor).ResumeRun(runID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "approved"}); err != nil {
		t.Fatalf("resume: %v", err)
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
	store, personas, runID := startHumanGateRun(t)
	if _, err := NewRunEngine(store, personas, &fakeRunExecutor{}).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{
		GateID: "gate_review", Verdict: "fail", Reason: "Answer question 3 first", Actor: "human:operator",
	}); err != nil {
		t.Fatalf("record fail verdict: %v", err)
	}
	executor := &fakeRunExecutor{}
	if _, err := NewRunEngine(store, personas, executor).ResumeRun(runID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "revise"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if len(executor.calls) != 1 || executor.calls[0].NodeID != "fmn_work" {
		t.Fatalf("calls = %+v, want Work pushback", executor.calls)
	}
	input := executor.calls[0].Inputs[0]
	if input.Response != nil || input.Feedback == nil || input.Feedback.Reason != "Answer question 3 first" || input.Feedback.Verdict != "fail" {
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
