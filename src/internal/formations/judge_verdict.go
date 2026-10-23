package formations

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/jsonstrict"
)

const judgeVerdictKeys = "requires exactly verdict, reason and evidence"

// judgeBlockReturned is what a judge wrote in its first lang block,
// archon-verdict or archon-pick, read leniently so a rejected verdict or pick
// can be shown beside why it was rejected. A reply without a block returns
// nothing.
func judgeBlockReturned(text, lang string) string {
	var body []string
	inside := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inside {
			inside = strings.HasPrefix(trimmed, "```"+lang)
			continue
		}
		if trimmed == "```" {
			break
		}
		body = append(body, line)
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}

// parseJudgeVerdict accepts one explicitly fenced result, never prose as a verdict.
func parseJudgeVerdict(text string) (GateEvaluationResult, error) {
	var body strings.Builder
	blocks, inside, closed := 0, false, false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```archon-verdict") {
			blocks++
			if line != "```archon-verdict" || inside || blocks != 1 {
				return GateEvaluationResult{}, fmt.Errorf("expected exactly one archon-verdict block")
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
		return GateEvaluationResult{}, fmt.Errorf("missing or unterminated archon-verdict block")
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
		if !ok || (name != "verdict" && name != "reason" && name != "evidence") {
			return GateEvaluationResult{}, fmt.Errorf("judge verdict has extra key %q; it %s", key, judgeVerdictKeys)
		}
		if fields[name] != nil {
			return GateEvaluationResult{}, fmt.Errorf("judge verdict repeats key %q; it %s", name, judgeVerdictKeys)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return GateEvaluationResult{}, fmt.Errorf("invalid judge verdict value: %w", err)
		}
		fields[name] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return GateEvaluationResult{}, fmt.Errorf("judge verdict must be a JSON object")
	}
	var missing []string
	for _, name := range []string{"verdict", "reason", "evidence"} {
		if fields[name] == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return GateEvaluationResult{}, fmt.Errorf("judge verdict is missing %s; it %s", strings.Join(missing, " and "), judgeVerdictKeys)
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
	// OriginalKind is file or folder when OriginalRef is a kept output's path.
	OriginalKind string `json:"originalKind,omitempty"`
	// Earlier is feedback that reached the same port before this, which the
	// formation had not run on yet (archon-o7p.11).
	Earlier *GateFeedback `json:"earlier,omitempty"`
}

// GateResponse is the operator's text on a passed human gate. It travels with
// the gate's original input and names the exact request it answered.
type GateResponse struct {
	GateID       string `json:"gateId"`
	GateAttempt  int    `json:"gateAttempt"`
	RequestedSeq int    `json:"requestedSeq"`
	DecidedBy    string `json:"decidedBy"`
	Text         string `json:"text"`
	// The port may hold newer unread work when this response is delivered.
	// Preserve what the operator actually approved (archon-qtq1).
	OriginalRef  string `json:"originalRef"`
	OriginalText string `json:"originalText"`
	// OriginalKind is file or folder when OriginalRef is a kept output's path.
	OriginalKind string `json:"originalKind,omitempty"`
	// Earlier is a response that reached the same port before this, which the
	// formation had not run on yet (archon-o7p.11).
	Earlier *GateResponse `json:"earlier,omitempty"`
}

// gatePassInput rebuilds a human pass route from the ledger. The gate verdict
// names its request sequence; the recorded verdict for that exact request
// supplies the response. Verdicts without a request, or with an empty
// response, route the gate input unchanged.
func gatePassInput(events []RunEvent, verdictEvent RunEvent, gateID string, input RunInputRef) (RunInputRef, error) {
	requestedSeq := intFromRunEventData(verdictEvent.Data["requestedSeq"])
	// A pick carries the picked attempt on as it was, without the picker's
	// reason (archon-gihe).
	if requestedSeq == 0 || isPickEvent(verdictEvent) {
		return input, nil
	}
	if requestedSeq < 0 || requestedSeq >= verdictEvent.Seq || verdictEvent.Seq > len(events) {
		return RunInputRef{}, fmt.Errorf("%w: Gate %q pass names invalid human request %d", ErrRunLedgerInvalid, gateID, requestedSeq)
	}
	request := events[requestedSeq-1]
	if request.Seq != requestedSeq || request.Type != RunEventHumanInputRequested || request.GateID != gateID ||
		!reflect.DeepEqual(runInputRefFromAny(request.Data["inputRef"]), input) {
		return RunInputRef{}, fmt.Errorf("%w: Gate %q pass request %d identity mismatch", ErrRunLedgerInvalid, gateID, requestedSeq)
	}
	for _, event := range events[requestedSeq:verdictEvent.Seq] {
		if event.Type != RunEventHumanVerdictRecorded || event.GateID != gateID || intFromRunEventData(event.Data["requestedSeq"]) != requestedSeq {
			continue
		}
		if stringFromEventData(event, "verdict") != "pass" {
			return RunInputRef{}, fmt.Errorf("%w: Gate %q pass contradicts recorded human verdict", ErrRunLedgerInvalid, gateID)
		}
		text := stringFromEventData(event, "reason")
		if text == "" {
			return input, nil
		}
		next := input
		next.Response = &GateResponse{
			GateID: gateID, GateAttempt: verdictEvent.Attempt, RequestedSeq: requestedSeq,
			DecidedBy: stringFromEventData(event, "decidedBy"), Text: text,
			OriginalRef: input.Ref, OriginalText: input.Text, OriginalKind: input.Kind,
		}
		return next, nil
	}
	return RunInputRef{}, fmt.Errorf("%w: Gate %q pass has no recorded human verdict for request %d", ErrRunLedgerInvalid, gateID, requestedSeq)
}

func gateFailInput(runID string, route BoardConnection, gateID string, attempt int, input RunInputRef, reason string, evidence []GateEvidenceRef) RunInputRef {
	next := input
	next.EdgeID = route.ID
	next.FromNodeID = gateID
	next.FromPortID = "fail"
	next.Ref = fmt.Sprintf("ledger://%s/%s", runID, route.ID)
	// The judged file or folder travels as the feedback's original ref.
	next.Kind = ""
	next.Feedback = &GateFeedback{
		GateID: gateID, GateAttempt: attempt, Verdict: "fail", Reason: reason,
		Evidence: append([]GateEvidenceRef(nil), evidence...), OriginalRef: input.Ref, OriginalText: input.Text, OriginalKind: input.Kind,
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

const judgeContract = "judge verdict contract:\nBefore the completion sentinel, emit exactly one fenced archon-verdict JSON block, separate from archon-outputs, with exactly these three keys:\n```archon-verdict\n{\"verdict\":\"fail\",\"reason\":\"Required check failed\",\"evidence\":[\"check report reference\"]}\n```\nverdict must be pass or fail, reason a string, and evidence an array of strings (use [] when there are no references). Do not omit evidence or add keys. The final judge in the chain decides the gate.\n"
