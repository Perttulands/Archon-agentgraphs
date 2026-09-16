package coordinator

import (
	"net/http"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// The pending-gate read route shows the operator what a human gate is waiting
// on. It is the run evidence API's view of an unanswered request (ADR-0017);
// after the verdict, the gate's node evidence carries the same input.

// pendingGateInputMaxBytes bounds the served input text; the cockpit reads it
// in a panel, and the full output stays in the private run evidence.
const pendingGateInputMaxBytes = formations.EvidenceTextMaxBytes

type PendingGateInput struct {
	FromNodeID string `json:"fromNodeId,omitempty"`
	FromPortID string `json:"fromPortId,omitempty"`
	Text       string `json:"text"`
	Truncated  bool   `json:"truncated"`
}

type PendingGateRequest struct {
	GateID       string           `json:"gateId"`
	RequestedSeq int              `json:"requestedSeq"`
	Criterion    string           `json:"criterion"`
	Input        PendingGateInput `json:"input"`
}

// pendingGateRequest serves the latest human request for a gate while it is
// pending: its frozen criterion and routed input text, never refs, paths,
// prompts or session identities. An unknown run or gate is 404; a request that
// is no longer pending is 409.
func (c *Coordinator) pendingGateRequest(w http.ResponseWriter, r *http.Request) {
	runID, gateID := r.PathValue("runId"), r.PathValue("gateId")
	events, err := c.store.ReadRunEvents(runID)
	if err != nil {
		failure(w, err)
		return
	}
	status, err := formations.ProjectRunEvents(runID, events)
	if err != nil {
		failure(w, err)
		return
	}
	var request *formations.RunEvent
	for i := range events {
		if events[i].Type == formations.RunEventHumanInputRequested && events[i].GateID == gateID {
			request = &events[i]
		}
	}
	if request == nil {
		reply(w, http.StatusNotFound, map[string]string{"error": "human gate request not found"})
		return
	}
	pending := false
	for _, gate := range project(status, events).WaitingGates {
		pending = pending || (gate.GateID == gateID && gate.RequestedSeq == request.Seq)
	}
	if !pending {
		reply(w, http.StatusConflict, map[string]string{"error": "human gate request is no longer pending"})
		return
	}
	body := PendingGateRequest{GateID: gateID, RequestedSeq: request.Seq}
	body.Criterion, _ = request.Data["prompt"].(string)
	input, _ := request.Data["inputRef"].(map[string]any)
	body.Input.FromNodeID, _ = input["fromNodeId"].(string)
	body.Input.FromPortID, _ = input["fromPortId"].(string)
	text, _ := input["text"].(string)
	body.Input.Text, body.Input.Truncated = capPendingGateText(text)
	reply(w, http.StatusOK, map[string]any{"request": body})
}

// capPendingGateText cuts at a rune boundary so truncated text stays valid UTF-8.
func capPendingGateText(text string) (string, bool) {
	return formations.CapEvidenceText(text, pendingGateInputMaxBytes)
}
