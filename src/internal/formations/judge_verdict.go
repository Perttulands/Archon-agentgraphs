package formations

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Perttulands/chrote-agent-formations/internal/jsonstrict"
)

// parseJudgeVerdict accepts one explicitly fenced result, never prose as a verdict.
func parseJudgeVerdict(text string) (GateEvaluationResult, error) {
	var body strings.Builder
	blocks, inside, closed := 0, false, false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```chrote-verdict") {
			blocks++
			if line != "```chrote-verdict" || inside || blocks != 1 {
				return GateEvaluationResult{}, fmt.Errorf("expected exactly one chrote-verdict block")
			}
			inside = true
			continue
		}
		if inside && line == "```" {
			inside, closed = false, true
			continue
		}
		if inside {
			body.WriteString(line + "\n")
		}
	}
	if blocks != 1 || inside || !closed {
		return GateEvaluationResult{}, fmt.Errorf("missing or unterminated chrote-verdict block")
	}
	if err := jsonstrict.ValidateUnicode([]byte(body.String())); err != nil {
		return GateEvaluationResult{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(body.String()))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return GateEvaluationResult{}, fmt.Errorf("judge verdict must be a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return GateEvaluationResult{}, fmt.Errorf("invalid judge verdict key: %w", err)
		}
		name, ok := key.(string)
		if !ok || (name != "verdict" && name != "reason" && name != "evidence") || fields[name] != nil {
			return GateEvaluationResult{}, fmt.Errorf("unknown or duplicate judge verdict key %q", key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return GateEvaluationResult{}, fmt.Errorf("invalid judge verdict value: %w", err)
		}
		fields[name] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') || len(fields) != 3 {
		return GateEvaluationResult{}, fmt.Errorf("judge verdict requires exactly verdict, reason and evidence")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return GateEvaluationResult{}, fmt.Errorf("trailing data in judge verdict")
	}
	var verdict, reason *string
	var evidence []*string
	if json.Unmarshal(fields["verdict"], &verdict) != nil || verdict == nil || (*verdict != "pass" && *verdict != "fail") {
		return GateEvaluationResult{}, fmt.Errorf("judge verdict must be exactly pass or fail")
	}
	if json.Unmarshal(fields["reason"], &reason) != nil || reason == nil {
		return GateEvaluationResult{}, fmt.Errorf("judge reason must be a string")
	}
	if json.Unmarshal(fields["evidence"], &evidence) != nil || evidence == nil {
		return GateEvaluationResult{}, fmt.Errorf("judge evidence must be an array of strings")
	}
	result := GateEvaluationResult{Verdict: *verdict, Reason: *reason, PerKind: map[string]string{}}
	for _, item := range evidence {
		if item == nil {
			return GateEvaluationResult{}, fmt.Errorf("judge evidence must contain only strings")
		}
		result.Evidence = append(result.Evidence, GateEvidenceRef{Kind: "formation", Text: *item})
	}
	return result, nil
}

type GateFeedback struct {
	GateID       string            `json:"gateId"`
	GateAttempt  int               `json:"gateAttempt"`
	Verdict      string            `json:"verdict"`
	Reason       string            `json:"reason"`
	Evidence     []GateEvidenceRef `json:"evidence"`
	OriginalRef  string            `json:"originalRef"`
	OriginalText string            `json:"originalText"`
}

func gateFailInput(runID string, route BoardConnection, gateID string, attempt int, input RunInputRef, reason string, evidence []GateEvidenceRef) RunInputRef {
	next := input
	next.EdgeID = route.ID
	next.FromNodeID = gateID
	next.FromPortID = "fail"
	next.Ref = fmt.Sprintf("ledger://%s/%s", runID, route.ID)
	next.Feedback = &GateFeedback{
		GateID: gateID, GateAttempt: attempt, Verdict: "fail", Reason: reason,
		Evidence: append([]GateEvidenceRef(nil), evidence...), OriginalRef: input.Ref, OriginalText: input.Text,
	}
	return next
}

func (e *RunEngine) gateAttempt(runID, gateID string) (int, error) {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return 0, err
	}
	attempt := 0
	for _, event := range events {
		if event.Type == RunEventGateEvaluating && event.GateID == gateID {
			attempt++
		}
	}
	return attempt, nil
}

func (e *RunEngine) blockInvalidJudge(runID, gateID string, err error) error {
	attempt, readErr := e.gateAttempt(runID, gateID)
	if readErr != nil {
		return readErr
	}
	if appendErr := e.store.AppendRunEvent(runID, RunEvent{
		Type: RunEventJudgeAttemptFailed, GateID: gateID, NodeID: gateID, Attempt: attempt,
		Data: map[string]any{"code": "invalid_judge_result", "reason": err.Error()},
	}); appendErr != nil {
		return appendErr
	}
	if appendErr := e.store.AppendRunEvent(runID, RunEvent{
		Type: RunEventBlocked, GateID: gateID, NodeID: gateID,
		Data: map[string]any{"reason": "invalid judge result: " + err.Error(), "blockedGateId": gateID, "resumeAllowed": false},
	}); appendErr != nil {
		return appendErr
	}
	return errRunStopped
}
