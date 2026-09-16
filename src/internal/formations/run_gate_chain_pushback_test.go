package formations

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// chainScriptExecutor returns scripted outputs per node call. It keeps its
// counts across fresh engines, as a real host keeps its agents.
type chainScriptExecutor struct {
	calls  []FormationExecution
	script map[string][]string
	counts map[string]int
}

const seatLost = "SEAT_LOST"

func (e *chainScriptExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	e.calls = append(e.calls, req)
	if e.counts == nil {
		e.counts = map[string]int{}
	}
	call := e.counts[req.NodeID]
	e.counts[req.NodeID]++
	text := fmt.Sprintf("%s output %d", req.NodeID, call+1)
	if script := e.script[req.NodeID]; call < len(script) {
		text = script[call]
	}
	if text == seatLost {
		return FormationExecutionResult{}, &RunExecutionError{Code: "native_turn_failed", Message: "seat died", Boundary: "executor", NodeID: req.NodeID}
	}
	return FormationExecutionResult{Status: "done", Text: text, Outputs: payloadsForFormationOutputs(req.Formation, text, "refs/"+req.NodeID+".md")}, nil
}

func (e *chainScriptExecutor) nodeIDs() []string {
	ids := make([]string, 0, len(e.calls))
	for _, call := range e.calls {
		ids = append(ids, call.NodeID)
	}
	return ids
}

// gateChainBoardFixture is Draft -> Review (formation gate) -> Sign-off, with
// sign-off:fail sending the work back to Draft. signoffKinds selects a human
// sign-off or an automatic one judged by fmn_signoff_judge.
func gateChainBoardFixture(signoffKinds string) string {
	formationBlock := func(id, title string) string {
		return fmt.Sprintf(`
[[formation]]
id = "%[1]s"
type = "solo"
title = "%[2]s"

[[formation.input]]
id = "%[1]s_in"
label = "Input"

[[formation.output]]
id = "%[1]s_out"
label = "Output"

[[formation.slot]]
id = "%[1]s_slot"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
controller = true
`, id, title)
	}
	board := s4MissionOnlyBoardFixture() + formationBlock("fmn_draft", "Draft") + formationBlock("fmn_review_judge", "Adversarial reviewer") + `
[[gate]]
id = "gate_review"
title = "Adversarial review"
kinds = ["formation"]
criterion = "The draft survives review"

[[gate]]
id = "gate_signoff"
title = "Brief sign-off"
kinds = ` + signoffKinds + `
criterion = "The operator signs off the brief"

[[connection]]
id = "edge_mission_draft"
from = "mis_showcase:out"
to = "fmn_draft:fmn_draft_in"

[[connection]]
id = "edge_draft_review"
from = "fmn_draft:fmn_draft_out"
to = "gate_review:in"

[[connection]]
id = "edge_review_judge"
from = "gate_review:judge"
to = "fmn_review_judge:fmn_review_judge_in"

[[connection]]
id = "edge_review_judge_back"
from = "fmn_review_judge:fmn_review_judge_out"
to = "gate_review:judge"

[[connection]]
id = "edge_review_pass_signoff"
from = "gate_review:pass"
to = "gate_signoff:in"

[[connection]]
id = "edge_signoff_fail_draft"
from = "gate_signoff:fail"
to = "fmn_draft:fmn_draft_in"
`
	if strings.Contains(signoffKinds, "formation") {
		board += formationBlock("fmn_signoff_judge", "Sign-off judge") + `
[[connection]]
id = "edge_signoff_judge"
from = "gate_signoff:judge"
to = "fmn_signoff_judge:fmn_signoff_judge_in"

[[connection]]
id = "edge_signoff_judge_back"
from = "fmn_signoff_judge:fmn_signoff_judge_out"
to = "gate_signoff:judge"
`
	}
	return board
}

func startGateChainRun(t *testing.T, signoffKinds string, executor *chainScriptExecutor) (*Store, *PersonaStore, string) {
	t.Helper()
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), gateChainBoardFixture(signoffKinds))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	status, err := NewRunEngine(store, personas, executor).RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
		Limits: RunLimits{MaxDispatch: 20, MaxAttempts: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	return store, personas, status.RunID
}

// eventTrail lists node dispatches and gate evaluations after a sequence, in order.
func eventTrail(t *testing.T, store *Store, runID string, afterSeq int) []string {
	t.Helper()
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	var trail []string
	for _, event := range events {
		if event.Seq <= afterSeq {
			continue
		}
		switch event.Type {
		case RunEventNodeStarted:
			trail = append(trail, "start "+event.NodeID)
		case RunEventGateEvaluating:
			trail = append(trail, "evaluate "+event.GateID)
		case RunEventHumanInputRequested:
			trail = append(trail, "ask "+event.GateID)
		case RunEventSucceeded, RunEventFailed, RunEventBlocked:
			trail = append(trail, event.Type)
		}
	}
	return trail
}

func lastSeq(t *testing.T, store *Store, runID string) int {
	t.Helper()
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	return len(events)
}

func TestHumanSendBackOnGateFedByGateRedispatchesTheWork(t *testing.T) {
	executor := &chainScriptExecutor{script: map[string][]string{
		"fmn_draft":        {"draft v1", "draft v2"},
		"fmn_review_judge": {judgeBlock("pass", "survives"), judgeBlock("pass", "survives again")},
	}}
	store, personas, runID := startGateChainRun(t, `["human"]`, executor)
	if got, want := eventTrail(t, store, runID, 0), []string{"start mis_showcase", "start fmn_draft", "evaluate gate_review", "start fmn_review_judge", "evaluate gate_signoff", "ask gate_signoff"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first round = %v, want %v", got, want)
	}

	if _, err := NewRunEngine(store, personas, executor).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: "gate_signoff", Verdict: "fail", Reason: "Add a success criterion about reading time.", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	sentBack := lastSeq(t, store, runID)
	status, err := NewRunEngine(store, personas, executor).ResumeRun(runID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := eventTrail(t, store, runID, sentBack), []string{"start fmn_draft", "evaluate gate_review", "start fmn_review_judge", "evaluate gate_signoff", "ask gate_signoff"}; !reflect.DeepEqual(got, want) || status.Final {
		t.Fatalf("after send-back = %v (%+v), want %v", got, status, want)
	}
	redraft := executor.calls[2]
	if redraft.NodeID != "fmn_draft" || len(redraft.Inputs) != 1 || redraft.Inputs[0].Feedback == nil || redraft.Inputs[0].Feedback.Reason != "Add a success criterion about reading time." {
		t.Fatalf("redraft input = %+v, want the send-back feedback", redraft)
	}
	events, _ := store.ReadRunEvents(runID)
	request := lastEventOfType(t, events, RunEventHumanInputRequested)
	if input := runInputRefFromAny(request.Data["inputRef"]); input.Text != "draft v2" {
		t.Fatalf("sign-off asked about %q, want the revised draft", input.Text)
	}

	if _, err := NewRunEngine(store, personas, executor).RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: "gate_signoff", Verdict: "pass", Reason: "Signed off.", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	status, err = NewRunEngine(store, personas, executor).ResumeRun(runID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusSucceeded || len(executor.calls) != 4 {
		t.Fatalf("after approval status %+v calls %v, want success with no further dispatch", status, executor.nodeIDs())
	}
}

func TestAutomaticSendBackAfterGateChainSurvivesResume(t *testing.T) {
	executor := &chainScriptExecutor{script: map[string][]string{
		"fmn_draft":         {"draft v1", seatLost, "draft v2"},
		"fmn_review_judge":  {judgeBlock("pass", "survives"), judgeBlock("pass", "survives again")},
		"fmn_signoff_judge": {judgeBlock("fail", "missing reading time", "criteria list"), judgeBlock("pass", "complete")},
	}}
	store, personas, runID := startGateChainRun(t, `["formation"]`, executor)
	blocked := lastSeq(t, store, runID)
	if got, want := executor.nodeIDs(), []string{"fmn_draft", "fmn_review_judge", "fmn_signoff_judge", "fmn_draft"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first round calls = %v, want %v", got, want)
	}

	status, err := NewRunEngine(store, personas, executor).ResumeRun(runID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "seat replaced"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := eventTrail(t, store, runID, blocked), []string{"start fmn_draft", "evaluate gate_review", "start fmn_review_judge", "evaluate gate_signoff", "start fmn_signoff_judge", "run_succeeded"}; !reflect.DeepEqual(got, want) || status.Status != RunStatusSucceeded {
		t.Fatalf("after resume = %v (%+v), want %v", got, status, want)
	}
	redraft := executor.calls[4]
	if redraft.NodeID != "fmn_draft" || redraft.Inputs[0].Feedback == nil || redraft.Inputs[0].Feedback.Reason != "missing reading time" {
		t.Fatalf("redraft input = %+v, want the sign-off judge feedback", redraft)
	}
}

func TestTerminalPassWaitsForAPendingPushback(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), gateChainBoardFixture(`["human"]`))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	verdict := func(seq int, gateID, port string, routes ...string) RunEvent {
		return RunEvent{Seq: seq, Type: RunEventGateVerdict, GateID: gateID, NodeID: gateID, Data: map[string]any{"routePort": port, "routedEdges": routes}}
	}
	sentBack := []RunEvent{
		{Seq: 1, Type: RunEventStarted},
		{Seq: 2, Type: RunEventNodeOutput, NodeID: "fmn_draft"},
		verdict(3, "gate_review", "pass", "edge_review_pass_signoff"),
		verdict(4, "gate_signoff", "fail", "edge_signoff_fail_draft"),
		verdict(5, "gate_signoff", "pass"),
	}
	if !pendingPushback(board, sentBack) || terminalPassReachedOnBoard(board, sentBack) {
		t.Fatal("a terminal pass finished the run past an unserviced send-back")
	}
	revised := append(append([]RunEvent{}, sentBack[:4]...), RunEvent{Seq: 5, Type: RunEventNodeOutput, NodeID: "fmn_draft"}, verdict(6, "gate_review", "pass", "edge_review_pass_signoff"), verdict(7, "gate_signoff", "pass"))
	if pendingPushback(board, revised) || !terminalPassReachedOnBoard(board, revised) {
		t.Fatal("a terminal pass after the revised work did not finish the run")
	}
}

func TestGateDeliveryIsConsumedOnlyByALaterEvaluationOfTheSameInput(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), gateChainBoardFixture(`["human"]`))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	draftOutput := RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_draft", Data: map[string]any{"outputs": map[string]any{"fmn_draft_out": map[string]any{"text": "draft"}}}}
	evaluate := func(gateID string) RunEvent {
		return RunEvent{Type: RunEventGateEvaluating, GateID: gateID, NodeID: gateID, Data: map[string]any{"inputRef": map[string]any{"edgeId": "edge_draft_review", "text": "draft"}}}
	}
	events := []RunEvent{draftOutput, evaluate("gate_review"), evaluate("gate_signoff"), draftOutput, evaluate("gate_review")}
	keys := gateEvaluationKeys(board, events)
	first, second := gateInputReplayKey("edge_draft_review", 1), gateInputReplayKey("edge_draft_review", 2)
	for _, check := range []struct {
		index int
		input string
		want  bool
	}{
		{1, first, true},   // the review's first pass was taken by sign-off
		{2, first, false},  // nothing after sign-off's own evaluation
		{4, second, false}, // the revised draft's pass has not reached sign-off yet
	} {
		if got := keys.consumedAfter(check.index, "gate_signoff", check.input); got != check.want {
			t.Errorf("consumedAfter(%d, %s) = %v, want %v", check.index, check.input, got, check.want)
		}
	}
}
