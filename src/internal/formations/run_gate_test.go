package formations

import (
	"reflect"
	"testing"
)

func TestS4GateRoutesPassAndAFailEndingRejectedFailsTheRun(t *testing.T) {
	t.Run("pass routes through pass wire", func(t *testing.T) {
		store, personas := s4RunFixture(t)
		store.Now = fixedClock()
		personas.Now = fixedClock()
		createS4Persona(t, personas, "scout")
		writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(false))
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
		if status.Status != RunStatusSucceeded {
			t.Fatalf("status = %+v, want succeeded", status)
		}
		if got, want := executor.nodeIDs(), []string{"fmn_work", "fmn_ship"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("executor nodes = %v, want %v", got, want)
		}
		events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
		verdict := eventOfType(t, events, RunEventGateVerdict)
		if verdict.GateID != "gate_review" || verdict.Data["verdict"] != "pass" || verdict.Data["routePort"] != "pass" {
			t.Fatalf("gate verdict = %+v, want pass through pass route", verdict)
		}
	})

	t.Run("a fail routed to a rejected End fails the run with the gate's reason", func(t *testing.T) {
		store, personas := s4RunFixture(t)
		store.Now = fixedClock()
		personas.Now = fixedClock()
		createS4Persona(t, personas, "scout")
		writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(false))
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatalf("read board: %v", err)
		}
		executor := &fakeRunExecutor{}
		engine := NewRunEngine(store, personas, executor)
		engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail"}})

		status, err := engine.RunMission("session-search", RunStartRequest{
			MissionID:         "mis_showcase",
			Actor:             "agent:test",
			ExpectedBoardETag: board.ETag,
			ExpectedBoardRev:  board.Rev,
		})
		if err != nil {
			t.Fatalf("run mission: %v", err)
		}
		if status.Status != RunStatusFailed || !status.Final {
			t.Fatalf("status = %+v, want failed final", status)
		}
		if got, want := executor.nodeIDs(), []string{"fmn_work"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("executor nodes = %v, want only pre-gate work", got)
		}
		events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
		last := events[len(events)-1]
		if last.Type != RunEventFailed || last.Data["code"] != RunFailurePathRejected || last.Data["reason"] != "fake fail" || last.Data["endId"] != "end_rejected" || last.Data["gateId"] != "gate_review" {
			t.Fatalf("last event = %+v, want run_failed with the gate's reason", last)
		}
		verdict := eventOfType(t, events, RunEventGateVerdict)
		if verdict.Data["verdict"] != "fail" || verdict.Data["routePort"] != "fail" {
			t.Fatalf("gate verdict = %+v, want the fail route", verdict)
		}
	})
}

func TestS4GateFailWirePushesBackUntilItsStepsRoundsAreSpent(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4GateBoardFixture(true))
	addLimit(t, store, "fmn_work", 2)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail", "fail"}})

	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID:         "mis_showcase",
		Actor:             "agent:test",
		ExpectedBoardETag: board.ETag,
		ExpectedBoardRev:  board.Rev,
	})
	if err != nil {
		t.Fatalf("run mission: %v", err)
	}
	if status.Status != RunStatusBlocked {
		t.Fatalf("status = %+v, want blocked once Work's rounds are spent", status)
	}
	if got, want := executor.nodeIDs(), []string{"fmn_work", "fmn_work"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("executor nodes = %v, want two work attempts", got)
	}
	events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
	if attempts := nodeStartedAttempts(events, "fmn_work"); !reflect.DeepEqual(attempts, []int{1, 2}) {
		t.Fatalf("work attempts = %v, want [1 2]", attempts)
	}
	if errEvent := eventOfType(t, events, RunEventError); errEvent.Data["reason"] != "Work used 2 of 2 rounds" {
		t.Fatalf("error data = %#v, want Work used 2 of 2 rounds", errEvent.Data)
	}
}

type fakeGateEvaluator struct {
	verdicts []string
	calls    []GateEvaluation
}

func (f *fakeGateEvaluator) EvaluateGate(req GateEvaluation) (GateEvaluationResult, error) {
	f.calls = append(f.calls, req)
	verdict := "pass"
	if len(f.verdicts) > 0 {
		verdict = f.verdicts[0]
		f.verdicts = f.verdicts[1:]
	}
	reason := "fake " + verdict
	canonical, err := canonicalCodeGateResult(verdict, reason, nil)
	if err != nil {
		return GateEvaluationResult{}, err
	}
	bindingID := ""
	if req.Binding != nil {
		bindingID = req.Binding.GateBindingID
	}
	return GateEvaluationResult{
		Verdict:         verdict,
		Reason:          reason,
		ResultEncoding:  CodeGateResultEncoding,
		ResultSHA256:    codeGateSHA256(canonical),
		CanonicalResult: canonical,
		GateBindingID:   bindingID,
	}, nil
}

func eventOfType(t *testing.T, events []RunEvent, eventType string) RunEvent {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType {
			return event
		}
	}
	t.Fatalf("missing event type %s in %#v", eventType, events)
	return RunEvent{}
}

func nodeStartedAttempts(events []RunEvent, nodeID string) []int {
	var attempts []int
	for _, event := range events {
		if event.Type == RunEventNodeStarted && event.NodeID == nodeID {
			attempts = append(attempts, event.Attempt)
		}
	}
	return attempts
}

// s4GateBoardFixture is mission -> work -> code gate whose pass goes to ship,
// which ends Done. The gate's fail sends work back when pushback is set and
// otherwise ends the path Rejected.
func s4GateBoardFixture(pushback bool) string {
	failWire := `
[[connection]]
id = "edge_gate_fail_rejected"
from = "gate_review:fail"
to = "end_rejected:in"
`
	if pushback {
		failWire = `
[[connection]]
id = "edge_gate_fail_work"
from = "gate_review:fail"
to = "fmn_work:port_work_in"
`
	}
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
kinds = ["code"]
criterion = "Good enough to ship"
check = "output_contains"
checkVersion = "1"
checkValue = "output from"

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
` + branchingBoardEnds() + failWire
}
