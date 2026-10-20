package formations

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// This is the admitted fanout from the audit reconciliation: every producer
// emits all its declared outputs. The control sends the other join input on
// the untaken verdict, so that input really has not arrived.
func gateFanoutBoard(both bool) string {
	right := "gate:fail"
	if both {
		right = "gate:pass"
	}
	return s4MissionOnlyBoardFixture() + branchingBoardFormation("draft", "Draft") + `
[[formation]]
id = "join"
type = "solo"
title = "Join"
[[formation.input]]
id = "left"
label = "Left"
[[formation.input]]
id = "right"
label = "Right"
[[formation.output]]
id = "out"
label = "Output"
[[formation.slot]]
id = "slot_join"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
effort = "medium"
[[limit]]
id = "mission_limit"
title = "Mission rounds"
target = "mis_showcase"
rounds = 2
` + branchingBoardHumanGate("gate") + branchingBoardEnds() +
		branchingBoardConnection("start", "mis_showcase:out", "draft:port_draft_in") +
		branchingBoardConnection("review", "draft:port_draft_out", "gate:in") +
		branchingBoardConnection("left", "gate:pass", "join:left") +
		branchingBoardConnection("right", right, "join:right") +
		branchingBoardConnection("reject", "gate:fail", "end_rejected:in") +
		branchingBoardConnection("done", "join:out", "end_done:in")
}

func TestHumanGateFanoutPreviewAgreesWithAdmittedExecution(t *testing.T) {
	for _, both := range []bool{true, false} {
		name := "one input absent"
		if both {
			name = "both inputs delivered"
		}
		t.Run(name, func(t *testing.T) {
			executor := &fakeRunExecutor{}
			store, personas, engine, status := startBranchingRun(t, gateFanoutBoard(both), executor)
			frozen, err := store.ReadRunBoard(status.RunID)
			if err != nil {
				t.Fatal(err)
			}
			scope := RunAdmissionScope{MissionID: "mis_showcase", Inputs: map[string]string{"brief": "test"}}
			report := ValidateRunAdmission(frozen, personas, scope)
			if len(report.Errors) != 0 || len(report.Warnings) != 0 {
				t.Fatalf("fixture not cleanly admitted: %+v", report)
			}
			if err := CheckRunAdmission(frozen, personas, scope); err != nil {
				t.Fatal(err)
			}
			events := mustEvents(t, store, status.RunID)
			approve := HumanGateRoutes(frozen, events, "gate", time.Time{})[0]
			if len(approve.Targets) != 1 || approve.Targets[0].NodeID != "join" || approve.Targets[0].WaitsForInputs == both {
				t.Fatalf("preview = %+v, want one join waiting=%v", approve, !both)
			}
			if both {
				if approve.RoundsNeeded != 1 || approve.MissionRounds == nil || approve.MissionRounds.Used != 1 || approve.Limit != nil {
					t.Fatalf("ready join should use one remaining round: %+v", approve)
				}
			} else if approve.RoundsNeeded != 0 || approve.Limit != nil {
				t.Fatalf("waiting join should spend no round/limit: %+v", approve)
			}
			executor.calls = nil
			status = approveAndContinue(t, engine, status.RunID, "gate")
			if both {
				if !reflect.DeepEqual(executor.nodeIDs(), []string{"join"}) || status.Status != RunStatusSucceeded {
					t.Fatalf("after fanout nodes=%v status=%+v", executor.nodeIDs(), status)
				}
				requireSucceededAfterOutputs(t, store, status.RunID, "draft", "join")
			} else if len(executor.calls) != 0 || status.Status != RunStatusBlocked {
				t.Fatalf("missing input should keep join waiting: nodes=%v status=%+v", executor.nodeIDs(), status)
			}
		})
	}
}

func TestHumanGatePreviewKeepsInputsDeliveredByEarlierGate(t *testing.T) {
	fixture := gateFanoutBoard(true)
	fixture = strings.Replace(fixture, `to = "gate:in"`, `to = "first_gate:in"`, 1)
	fixture = strings.Replace(fixture, `from = "gate:pass"`, `from = "first_gate:pass"`, 1)
	fixture += branchingBoardHumanGate("first_gate") +
		branchingBoardConnection("first_pass", "first_gate:pass", "gate:in") +
		branchingBoardConnection("first_fail", "first_gate:fail", "end_rejected:in")
	executor := &fakeRunExecutor{}
	store, _, engine, status := startBranchingRun(t, fixture, executor)
	status = approveAndContinue(t, engine, status.RunID, "first_gate")
	frozen, err := store.ReadRunBoard(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	approve := HumanGateRoutes(frozen, mustEvents(t, store, status.RunID), "gate", time.Time{})[0]
	if len(approve.Targets) != 1 || approve.Targets[0].WaitsForInputs || approve.RoundsNeeded != 1 {
		t.Fatalf("earlier gate fed left; this verdict feeds right: %+v", approve)
	}
	executor.calls = nil
	status = approveAndContinue(t, engine, status.RunID, "gate")
	if !reflect.DeepEqual(executor.nodeIDs(), []string{"join"}) || status.Status != RunStatusSucceeded {
		t.Fatalf("after second verdict nodes=%v status=%+v", executor.nodeIDs(), status)
	}
}
