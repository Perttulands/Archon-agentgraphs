package formations

import (
	"strings"
	"testing"
)

// A final run says why it ended and who ended it, on the run's problems and on
// the node it stopped, and an earlier block the run resumed past is marked so
// it is not read as the cause (form-n7u.5).
func TestRunProblemsNameTheEndOfARunAndWhoEndedIt(t *testing.T) {
	failed := []RunEvent{
		{Seq: 1, Type: RunEventStarted},
		evidenceEvent(2, RunEventNodeStarted, "fmn_execution", nil),
		evidenceEvent(3, RunEventError, "fmn_execution", map[string]any{"code": "native_turn_failed", "reason": "another user message interrupted the dispatched Claude turn"}),
		evidenceEvent(4, RunEventBlocked, "fmn_execution", map[string]any{"reason": "another user message interrupted the dispatched Claude turn", "resumeAllowed": true}),
		{Seq: 5, Type: RunEventResumed, Actor: "operator:archon"},
		// The coordinator's failure names no node and inherited the run's actor.
		{Seq: 6, Type: RunEventFailed, Actor: "operator:standalone", Data: map[string]any{"reason": "coordinator_execution_failed", "detail": "completed recovery requires a single-slot formation", "final": true}},
	}
	problems := projectRunProblems(failed)
	if len(problems) != 3 {
		t.Fatalf("problems = %+v", problems)
	}
	if block := problems[1]; block.ResumedSeq != 5 {
		t.Fatalf("resumed block = %+v, want resumedSeq 5", block)
	}
	end := problems[2]
	if end.Type != RunEventFailed || end.Code != "coordinator_execution_failed" || end.Reason.Text != "completed recovery requires a single-slot formation" || end.Actor != RunFailureActor || strings.Join(end.NodeIDs, ",") != "fmn_execution" {
		t.Fatalf("failure = %+v", end)
	}
	execution := projectNodeEvidence("run_1", "fmn_execution", "formation", failed, true, nil, nil)
	if last := execution.Problems[len(execution.Problems)-1]; last.Seq != 6 || last.Actor != RunFailureActor {
		t.Fatalf("execution problems = %+v", execution.Problems)
	}
	status, err := ProjectRunEvents("run_1", failed)
	if err != nil || status.EndedBy != RunFailureActor || status.Status != RunStatusFailed {
		t.Fatalf("failed projection = %+v, %v", status, err)
	}

	canceled := []RunEvent{
		{Seq: 1, Type: RunEventStarted},
		evidenceEvent(2, RunEventNodeStarted, "fmn_draft", nil),
		evidenceEvent(3, RunEventNodeOutput, "fmn_draft", nil),
		evidenceEvent(4, RunEventGateEvaluating, "gate_review", nil),
		evidenceEvent(5, RunEventHumanInputRequested, "gate_review", nil),
		{Seq: 6, Type: RunEventCanceled, Actor: "agent:ui", Data: map[string]any{"reason": "the brief was wrong", "requestedBy": "agent:ui", "final": true}},
	}
	problems = projectRunProblems(canceled)
	if len(problems) != 1 {
		t.Fatalf("cancel problems = %+v", problems)
	}
	end = problems[0]
	gate := projectNodeEvidence("run_1", "gate_review", "gate", canceled, true, nil, nil)
	if end.Type != RunEventCanceled || end.Actor != "agent:ui" || end.Reason.Text != "the brief was wrong" || strings.Join(end.NodeIDs, ",") != "gate_review" || len(gate.Problems) != 1 || gate.Problems[0] != end.EvidenceProblem {
		t.Fatalf("cancel = %+v, gate problems %+v", end, gate.Problems)
	}
	if draft := projectNodeEvidence("run_1", "fmn_draft", "formation", canceled, true, nil, nil); len(draft.Problems) != 0 {
		t.Fatalf("a finished step does not carry the cancel: %+v", draft.Problems)
	}
	if status, err := ProjectRunEvents("run_1", canceled); err != nil || status.EndedBy != "agent:ui" {
		t.Fatalf("canceled projection = %+v, %v", status, err)
	}
}
