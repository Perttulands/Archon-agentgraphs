package formations

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Run evidence serves what a run recorded to the trusted operator: node
// outputs, gate results and judge evidence, human responses, briefs and
// artifacts. Projections stay sanitized; these reads are separate, capped and
// confined to the run's own ledger, artifact directory and dispatched briefs
// (ADR-0017).

const (
	EvidenceTextMaxBytes            = 64 << 10
	EvidenceNodeBudgetBytes         = 2 << 20
	EvidenceItemsMax                = 100
	EvidenceBriefMaxBytes           = 256 << 10
	EvidenceArtifactPreviewMaxBytes = 256 << 10
	EvidenceArtifactRawMaxBytes     = 16 << 20
	EvidenceArtifactListMax         = 500
	EvidenceArtifactListDepth       = 8
)

// ErrEvidenceTooLarge marks an artifact beyond the raw read cap.
var ErrEvidenceTooLarge = errors.New("run evidence exceeds byte limit")

// EvidenceText is one served text: Bytes is its full size and Truncated marks
// a cut made by a cap.
type EvidenceText struct {
	Text      string `json:"text"`
	Bytes     int    `json:"bytes"`
	Truncated bool   `json:"truncated,omitempty"`
}

// EvidenceRef names where an output or input also lives. Artifact is relative
// to the run's artifact directory and readable through the evidence routes;
// External is only the base name of a file elsewhere.
type EvidenceRef struct {
	Artifact string `json:"artifact,omitempty"`
	External string `json:"external,omitempty"`
}

type EvidenceInput struct {
	EdgeID     string       `json:"edgeId,omitempty"`
	FromNodeID string       `json:"fromNodeId,omitempty"`
	FromPortID string       `json:"fromPortId,omitempty"`
	ToPortID   string       `json:"toPortId,omitempty"`
	Text       EvidenceText `json:"text"`
	Ref        *EvidenceRef `json:"ref,omitempty"`
}

type EvidencePort struct {
	PortID string       `json:"portId"`
	Text   EvidenceText `json:"text"`
	Ref    *EvidenceRef `json:"ref,omitempty"`
}

type EvidenceOutput struct {
	Seq    int            `json:"seq"`
	Status string         `json:"status,omitempty"`
	Reason *EvidenceText  `json:"reason,omitempty"`
	Text   EvidenceText   `json:"text"`
	Ports  []EvidencePort `json:"ports"`
}

// EvidenceDispatch is one seat dispatch. Its Seq identifies the brief route.
type EvidenceDispatch struct {
	Seq       int    `json:"seq"`
	SlotID    string `json:"slotId,omitempty"`
	AgentID   string `json:"agentId,omitempty"`
	Harness   string `json:"harness,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Brief     bool   `json:"brief"`
	ResultSeq int    `json:"resultSeq,omitempty"`
	Status    string `json:"status,omitempty"`
}

type EvidenceAttempt struct {
	Attempt    int                `json:"attempt"`
	StartedSeq int                `json:"startedSeq,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	Inputs     []EvidenceInput    `json:"inputs"`
	Dispatches []EvidenceDispatch `json:"dispatches"`
	Output     *EvidenceOutput    `json:"output,omitempty"`
}

type EvidenceItem struct {
	Kind string       `json:"kind,omitempty"`
	Text EvidenceText `json:"text"`
}

type EvidenceKindResult struct {
	Seq             int            `json:"seq"`
	Kind            string         `json:"kind"`
	Verdict         string         `json:"verdict"`
	Reason          EvidenceText   `json:"reason"`
	Evidence        []EvidenceItem `json:"evidence"`
	EvidenceOmitted int            `json:"evidenceOmitted,omitempty"`
}

type EvidenceJudgeFailure struct {
	Seq    int          `json:"seq"`
	Code   string       `json:"code,omitempty"`
	Reason EvidenceText `json:"reason"`
}

type EvidenceHumanDecision struct {
	Seq       int          `json:"seq"`
	Verdict   string       `json:"verdict"`
	Response  EvidenceText `json:"response"`
	DecidedBy string       `json:"decidedBy,omitempty"`
}

type EvidenceHumanRequest struct {
	Seq      int                    `json:"seq"`
	Pending  bool                   `json:"pending"`
	Decision *EvidenceHumanDecision `json:"decision,omitempty"`
}

type EvidenceGateVerdict struct {
	Seq             int               `json:"seq"`
	Verdict         string            `json:"verdict"`
	Reason          EvidenceText      `json:"reason"`
	PerKind         map[string]string `json:"perKind,omitempty"`
	RoutePort       string            `json:"routePort,omitempty"`
	Evidence        []EvidenceItem    `json:"evidence"`
	EvidenceOmitted int               `json:"evidenceOmitted,omitempty"`
}

type EvidenceEvaluation struct {
	Seq           int                    `json:"seq"`
	Attempt       int                    `json:"attempt,omitempty"`
	Kinds         []string               `json:"kinds"`
	Criterion     EvidenceText           `json:"criterion"`
	JudgeChain    []string               `json:"judgeChain,omitempty"`
	Input         *EvidenceInput         `json:"input,omitempty"`
	KindResults   []EvidenceKindResult   `json:"kindResults"`
	JudgeFailures []EvidenceJudgeFailure `json:"judgeFailures,omitempty"`
	HumanRequests []EvidenceHumanRequest `json:"humanRequests,omitempty"`
	Verdict       *EvidenceGateVerdict   `json:"verdict,omitempty"`
}

// EvidenceProblem is a block or error the ledger recorded against the node.
type EvidenceProblem struct {
	Seq           int          `json:"seq"`
	Type          string       `json:"type"`
	Code          string       `json:"code,omitempty"`
	Reason        EvidenceText `json:"reason"`
	ResumeAllowed *bool        `json:"resumeAllowed,omitempty"`
}

type NodeEvidence struct {
	RunID       string               `json:"runId"`
	NodeID      string               `json:"nodeId"`
	Kind        string               `json:"kind"`
	Attempts    []EvidenceAttempt    `json:"attempts,omitempty"`
	Evaluations []EvidenceEvaluation `json:"evaluations,omitempty"`
	Problems    []EvidenceProblem    `json:"problems,omitempty"`
}

// CapEvidenceText cuts text to at most maxBytes on a rune boundary, so a
// truncated text stays valid UTF-8.
func CapEvidenceText(text string, maxBytes int) (string, bool) {
	if maxBytes < 0 {
		maxBytes = 0
	}
	if len(text) <= maxBytes {
		return text, false
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}

// redactEvidenceText applies the ledger's secret patterns without trimming,
// so served Markdown and code keep their layout.
func redactEvidenceText(text string) string {
	redacted := secretTokenPattern.ReplaceAllString(text, "[REDACTED_SECRET]")
	return credentialAssignmentPattern.ReplaceAllString(redacted, "$1=[REDACTED]")
}

// evidenceCapper caps each text and spends a shared response budget.
type evidenceCapper struct {
	remaining int
}

func (c *evidenceCapper) text(raw string) EvidenceText {
	redacted := redactEvidenceText(raw)
	limit := min(EvidenceTextMaxBytes, max(c.remaining, 0))
	text, truncated := CapEvidenceText(redacted, limit)
	c.remaining -= len(text)
	return EvidenceText{Text: text, Bytes: len(redacted), Truncated: truncated}
}

// ProjectNodeEvidence reads one node's recorded content from the run ledger.
// An unknown run, or a node absent from the run's frozen board, is ErrNotFound.
func (s *Store) ProjectNodeEvidence(runID, nodeID string) (*NodeEvidence, error) {
	events, err := s.ReadRunEvents(runID)
	if err != nil {
		return nil, evidenceNotFound(err)
	}
	board, err := s.ReadRunBoard(runID)
	if err != nil {
		return nil, err
	}
	kind := evidenceNodeKind(board, nodeID)
	if kind == "" {
		return nil, fmt.Errorf("%w: node %q", ErrNotFound, nodeID)
	}
	status, err := ProjectRunEvents(runID, events)
	if err != nil {
		return nil, err
	}
	return projectNodeEvidence(runID, nodeID, kind, events, status.Final, s.runArtifactRoots(runID)), nil
}

func evidenceNotFound(err error) error {
	if errors.Is(err, ErrInvalidSlug) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}

func evidenceNodeKind(board *BoardDocument, nodeID string) string {
	for _, mission := range board.Missions {
		if mission.ID == nodeID {
			return "mission"
		}
	}
	for _, formation := range board.Formations {
		if formation.ID == nodeID {
			return "formation"
		}
	}
	for _, gate := range board.Gates {
		if gate.ID == nodeID {
			return "gate"
		}
	}
	for _, tool := range board.Tools {
		if tool.ID == nodeID {
			return "tool"
		}
	}
	return ""
}

// runArtifactRoots are the textual forms a recorded ref can use for the run's
// artifact directory: the configured workspace and its absolute form.
func (s *Store) runArtifactRoots(runID string) []string {
	roots := []string{filepath.Clean(filepath.Join(s.workspaceRoot(), ".formations", "artifacts", runID))}
	if workspace, err := s.workspaceAbsolutePath(); err == nil {
		roots = append(roots, filepath.Join(workspace, ".formations", "artifacts", runID))
	}
	return roots
}

func projectNodeEvidence(runID, nodeID, kind string, events []RunEvent, final bool, artifactRoots []string) *NodeEvidence {
	evidence := &NodeEvidence{RunID: runID, NodeID: nodeID, Kind: kind}
	capper := &evidenceCapper{remaining: EvidenceNodeBudgetBytes}
	dispatchByID := map[string][2]int{}
	for _, event := range events {
		switch event.Type {
		case RunEventNodeStarted:
			if event.NodeID != nodeID {
				continue
			}
			attempt := EvidenceAttempt{Attempt: max(event.Attempt, 1), StartedSeq: event.Seq, Inputs: []EvidenceInput{}, Dispatches: []EvidenceDispatch{}}
			attempt.Reason, _ = event.Data["reason"].(string)
			if refs, ok := event.Data["inputRefs"].([]any); ok {
				for _, raw := range refs {
					if input := evidenceInput(raw, capper, artifactRoots); input != nil {
						attempt.Inputs = append(attempt.Inputs, *input)
					}
				}
			}
			evidence.Attempts = append(evidence.Attempts, attempt)
		case RunEventSlotDispatch:
			if event.NodeID != nodeID {
				continue
			}
			attempt := currentEvidenceAttempt(evidence, event)
			dispatch := EvidenceDispatch{Seq: event.Seq, SlotID: event.SlotID, Brief: stringFromEventData(event, "briefPath") != ""}
			dispatch.AgentID = stringFromEventData(event, "agentId")
			dispatch.Harness = stringFromEventData(event, "harness")
			dispatch.Phase = stringFromEventData(event, "phase")
			attempt.Dispatches = append(attempt.Dispatches, dispatch)
			if id := stringFromEventData(event, "dispatchId"); id != "" {
				dispatchByID[id] = [2]int{len(evidence.Attempts) - 1, len(attempt.Dispatches) - 1}
			}
		case RunEventSlotResult:
			if event.NodeID != nodeID {
				continue
			}
			if at, ok := dispatchByID[stringFromEventData(event, "dispatchId")]; ok {
				dispatch := &evidence.Attempts[at[0]].Dispatches[at[1]]
				dispatch.ResultSeq = event.Seq
				dispatch.Status = stringFromEventData(event, "status")
			}
		case RunEventNodeOutput:
			if event.NodeID != nodeID {
				continue
			}
			attempt := currentEvidenceAttempt(evidence, event)
			output := &EvidenceOutput{Seq: event.Seq, Status: stringFromEventData(event, "status"), Ports: []EvidencePort{}}
			if reason := stringFromEventData(event, "reason"); reason != "" {
				text := capper.text(reason)
				output.Reason = &text
			}
			output.Text = capper.text(stringFromEventData(event, "text"))
			payloads := outputPayloadsFromAny(event.Data["outputs"])
			ports := make([]string, 0, len(payloads))
			for portID := range payloads {
				ports = append(ports, portID)
			}
			sort.Strings(ports)
			for _, portID := range ports {
				payload := payloads[portID]
				ref := payload.Ref
				if ref == "" {
					ref = payload.ArtifactRef
				}
				output.Ports = append(output.Ports, EvidencePort{PortID: portID, Text: capper.text(payload.Text), Ref: evidenceRef(ref, artifactRoots)})
			}
			attempt.Output = output
		case RunEventGateEvaluating:
			if event.GateID != nodeID {
				continue
			}
			evaluation := EvidenceEvaluation{Seq: event.Seq, Attempt: event.Attempt, Kinds: stringsFromEventData(event.Data["kinds"]), KindResults: []EvidenceKindResult{}}
			evaluation.Criterion = capper.text(stringFromEventData(event, "criterion"))
			evaluation.JudgeChain = stringsFromEventData(event.Data["judgeChain"])
			evaluation.Input = evidenceInput(event.Data["inputRef"], capper, artifactRoots)
			evidence.Evaluations = append(evidence.Evaluations, evaluation)
		case RunEventGateKindResult:
			if event.GateID != nodeID {
				continue
			}
			evaluation := currentEvidenceEvaluation(evidence, event)
			result := EvidenceKindResult{Seq: event.Seq, Kind: stringFromEventData(event, "kind"), Verdict: stringFromEventData(event, "verdict")}
			result.Reason = capper.text(stringFromEventData(event, "reason"))
			result.Evidence, result.EvidenceOmitted = evidenceItems(event.Data["evidence"], capper)
			evaluation.KindResults = append(evaluation.KindResults, result)
		case RunEventJudgeAttemptFailed:
			if event.GateID != nodeID {
				continue
			}
			evaluation := currentEvidenceEvaluation(evidence, event)
			evaluation.JudgeFailures = append(evaluation.JudgeFailures, EvidenceJudgeFailure{Seq: event.Seq, Code: stringFromEventData(event, "code"), Reason: capper.text(stringFromEventData(event, "reason"))})
		case RunEventHumanInputRequested:
			if event.GateID != nodeID {
				continue
			}
			// Only the gate's latest unanswered request can still be pending.
			for i := range evidence.Evaluations {
				for j := range evidence.Evaluations[i].HumanRequests {
					evidence.Evaluations[i].HumanRequests[j].Pending = false
				}
			}
			evaluation := currentEvidenceEvaluation(evidence, event)
			evaluation.HumanRequests = append(evaluation.HumanRequests, EvidenceHumanRequest{Seq: event.Seq, Pending: !final})
		case RunEventHumanVerdictRecorded:
			if event.GateID != nodeID {
				continue
			}
			requestedSeq := intFromRunEventData(event.Data["requestedSeq"])
			for i := range evidence.Evaluations {
				for j := range evidence.Evaluations[i].HumanRequests {
					request := &evidence.Evaluations[i].HumanRequests[j]
					if request.Seq != requestedSeq {
						continue
					}
					request.Pending = false
					request.Decision = &EvidenceHumanDecision{Seq: event.Seq, Verdict: stringFromEventData(event, "verdict"), DecidedBy: stringFromEventData(event, "decidedBy")}
					request.Decision.Response = capper.text(stringFromEventData(event, "reason"))
				}
			}
		case RunEventGateVerdict:
			if event.GateID != nodeID {
				continue
			}
			evaluation := currentEvidenceEvaluation(evidence, event)
			verdict := &EvidenceGateVerdict{Seq: event.Seq, Verdict: stringFromEventData(event, "verdict"), RoutePort: stringFromEventData(event, "routePort")}
			verdict.Reason = capper.text(stringFromEventData(event, "reason"))
			verdict.PerKind = stringMapFromRunEventData(event.Data["perKind"])
			verdict.Evidence, verdict.EvidenceOmitted = evidenceItems(event.Data["evidence"], capper)
			evaluation.Verdict = verdict
		case RunEventBlocked, RunEventError:
			if !evidenceProblemFor(event, nodeID) {
				continue
			}
			problem := EvidenceProblem{Seq: event.Seq, Type: event.Type, Code: stringFromEventData(event, "code")}
			reason := stringFromEventData(event, "reason")
			if reason == "" {
				reason = stringFromEventData(event, "message")
			}
			problem.Reason = capper.text(reason)
			if allowed, ok := event.Data["resumeAllowed"].(bool); ok {
				problem.ResumeAllowed = &allowed
			}
			evidence.Problems = append(evidence.Problems, problem)
		}
	}
	return evidence
}

// currentEvidenceAttempt returns the latest attempt, opening one when output
// or dispatch evidence precedes any recorded start.
func currentEvidenceAttempt(evidence *NodeEvidence, event RunEvent) *EvidenceAttempt {
	if len(evidence.Attempts) == 0 {
		evidence.Attempts = append(evidence.Attempts, EvidenceAttempt{Attempt: max(event.Attempt, 1), Inputs: []EvidenceInput{}, Dispatches: []EvidenceDispatch{}})
	}
	return &evidence.Attempts[len(evidence.Attempts)-1]
}

func currentEvidenceEvaluation(evidence *NodeEvidence, event RunEvent) *EvidenceEvaluation {
	if len(evidence.Evaluations) == 0 {
		evidence.Evaluations = append(evidence.Evaluations, EvidenceEvaluation{Attempt: event.Attempt, Kinds: []string{}, KindResults: []EvidenceKindResult{}})
	}
	return &evidence.Evaluations[len(evidence.Evaluations)-1]
}

func evidenceProblemFor(event RunEvent, nodeID string) bool {
	if event.NodeID == nodeID || event.GateID == nodeID {
		return true
	}
	for _, key := range []string{"blockedNodeId", "blockedGateId", "nodeId"} {
		if stringFromEventData(event, key) == nodeID {
			return true
		}
	}
	return false
}

func evidenceInput(raw any, capper *evidenceCapper, artifactRoots []string) *EvidenceInput {
	data, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	field := func(key string) string {
		value, _ := data[key].(string)
		return value
	}
	return &EvidenceInput{
		EdgeID:     field("edgeId"),
		FromNodeID: field("fromNodeId"),
		FromPortID: field("fromPortId"),
		ToPortID:   field("toPortId"),
		Text:       capper.text(field("text")),
		Ref:        evidenceRef(field("ref"), artifactRoots),
	}
}

func evidenceItems(raw any, capper *evidenceCapper) ([]EvidenceItem, int) {
	items := []EvidenceItem{}
	list, _ := raw.([]any)
	for index, entry := range list {
		if index >= EvidenceItemsMax {
			return items, len(list) - EvidenceItemsMax
		}
		switch value := entry.(type) {
		case string:
			items = append(items, EvidenceItem{Text: capper.text(value)})
		case map[string]any:
			kind, _ := value["kind"].(string)
			text, _ := value["text"].(string)
			items = append(items, EvidenceItem{Kind: kind, Text: capper.text(text)})
		}
	}
	return items, 0
}

// evidenceRef reports a recorded ref by artifact name when it lies in the
// run's artifact directory, otherwise by base name only.
func evidenceRef(ref string, artifactRoots []string) *EvidenceRef {
	if strings.TrimSpace(ref) == "" {
		return nil
	}
	clean := filepath.Clean(ref)
	if filepath.IsAbs(clean) {
		for _, root := range artifactRoots {
			relative, err := filepath.Rel(root, clean)
			if err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return &EvidenceRef{Artifact: filepath.ToSlash(relative)}
			}
		}
	}
	return &EvidenceRef{External: filepath.Base(clean)}
}

func stringsFromEventData(raw any) []string {
	values := []string{}
	switch list := raw.(type) {
	case []string:
		values = append(values, list...)
	case []any:
		for _, entry := range list {
			if value, ok := entry.(string); ok {
				values = append(values, value)
			}
		}
	}
	return values
}
