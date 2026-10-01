package formations

import (
	"fmt"
	"strings"
)

// Spent limits (archon-o7p.8). A run has no limits unless its frozen mission
// holds a Limit card. Each knob a card sets is counted from the ledger, so
// replay and every reader agree: rounds count a step's starts, or every step
// start of the run for a card on the Input card. A grant, recorded on the
// run_resumed that gives it, adds one more allowance. At the limit the run
// blocks with the limit on the block itself, resumable only with a grant.

// RunLimitReached is a Limit card's knob as the run used it: Used of Max, Max
// counting the card's own value and every grant.
type RunLimitReached struct {
	Kind    string `json:"kind"`
	LimitID string `json:"limitId"`
	// NodeID is the step the card covers, or the Input card for the mission.
	NodeID  string `json:"nodeId"`
	Used    int    `json:"used"`
	Max     int    `json:"max"`
	Granted int    `json:"granted,omitempty"`
}

// RunLimitGrant is the allowance a grant adds, recorded on its run_resumed.
type RunLimitGrant struct {
	LimitID string `json:"limitId"`
	Kind    string `json:"kind"`
	Amount  int    `json:"amount"`
}

// limitGranted sums what grants added to a Limit card's knob.
func limitGranted(events []RunEvent, limitID, kind string) int {
	granted := 0
	for _, event := range events {
		if event.Type != RunEventResumed {
			continue
		}
		grant, ok := event.Data["grant"].(map[string]any)
		if !ok {
			if typed, isTyped := event.Data["grant"].(RunLimitGrant); isTyped {
				grant = map[string]any{"limitId": typed.LimitID, "kind": typed.Kind, "amount": typed.Amount}
			} else {
				continue
			}
		}
		if stringFromAny(grant["limitId"]) == limitID && stringFromAny(grant["kind"]) == kind {
			granted += intFromRunEventData(grant["amount"])
		}
	}
	return granted
}

// isMissionLimit reports whether a card covers the whole mission.
func isMissionLimit(board *BoardDocument, limit LimitNode) bool {
	_, ok := findMission(board, limit.Target)
	return ok
}

// roundsUsed is what a card's rounds have counted so far: its step's starts,
// or every step start of the run for the mission. A start a restart cut short
// is not a round; the step's re-run is. A peer step's rounds are its journal
// messages and count in its conversation instead.
func roundsUsed(board *BoardDocument, events []RunEvent, limit LimitNode) int {
	used := 0
	mission := isMissionLimit(board, limit)
	for i, event := range events {
		if event.Type != RunEventNodeStarted || stringFromEventData(event, "nodeKind") != "formation" {
			continue
		}
		if (mission || event.NodeID == limit.Target) && !cutShortByRestart(events, i) {
			used++
		}
	}
	return used
}

// cutShortByRestart reports whether the step started at index never produced
// its output because the coordinator restarted, so the run started it again.
// A run reaches the same limits wherever a restart falls.
func cutShortByRestart(events []RunEvent, index int) bool {
	nodeID, interrupted := events[index].NodeID, false
	for _, event := range events[index+1:] {
		switch {
		case event.NodeID == nodeID && event.Type == RunEventNodeStarted:
			return interrupted
		case event.NodeID == nodeID && event.Type == RunEventNodeOutput:
			return false
		case event.Type == RunEventError && stringFromEventData(event, "code") == RunBlockCoordinatorInterrupted:
			interrupted = true
		}
	}
	return interrupted
}

// roundsUse is a rounds card's use, or nil when the card sets no rounds.
func roundsUse(board *BoardDocument, events []RunEvent, limit LimitNode) *RunLimitReached {
	if limit.Rounds == nil || *limit.Rounds <= 0 {
		return nil
	}
	granted := limitGranted(events, limit.ID, LimitKindRounds)
	return &RunLimitReached{Kind: LimitKindRounds, LimitID: limit.ID, NodeID: limit.Target, Used: roundsUsed(board, events, limit), Max: *limit.Rounds + granted, Granted: granted}
}

// roundsSpentBefore is the rounds limit that starting the step now would pass:
// the step's own card first, then the mission's. Nil means the step may start.
// A peer step's own rounds are its journal messages, not its starts.
func roundsSpentBefore(board *BoardDocument, events []RunEvent, formation FormationNode) *RunLimitReached {
	if formation.Type != FormationTypePeer {
		if limit, ok := limitCovering(board, formation.ID); ok {
			if use := roundsUse(board, events, limit); use != nil && use.Used >= use.Max {
				return use
			}
		}
	}
	for _, mission := range board.Missions {
		if limit, ok := limitCovering(board, mission.ID); ok {
			if use := roundsUse(board, events, limit); use != nil && use.Used >= use.Max {
				return use
			}
		}
	}
	return nil
}

// peerMessagesUse is a peer step's rounds, its journal messages: those its
// earlier attempts posted, of the card's rounds and grants. Nil when no card
// caps the step's rounds.
func (e *RunEngine) peerMessagesUse(board *BoardDocument, events []RunEvent, req FormationExecution) *RunLimitReached {
	limit, ok := limitCovering(board, req.NodeID)
	if !ok || limit.Rounds == nil || *limit.Rounds <= 0 {
		return nil
	}
	used := 0
	for attempt := 1; attempt < req.Attempt; attempt++ {
		if conversation, err := e.store.ReadPeerConversation(PeerConversationID{RunID: req.RunID, NodeID: req.NodeID, Attempt: attempt}); err == nil {
			used += conversation.Messages
		}
	}
	granted := limitGranted(events, limit.ID, LimitKindRounds)
	return &RunLimitReached{Kind: LimitKindRounds, LimitID: limit.ID, NodeID: limit.Target, Used: used, Max: *limit.Rounds + granted, Granted: granted}
}

// limitReason says what a spent limit used, plainly: "Review used 3 of 3
// rounds", or "The mission used 20 of 20 rounds, 1 of them granted".
func limitReason(board *BoardDocument, use RunLimitReached) string {
	who := nodeName(board, use.NodeID)
	if _, ok := findMission(board, use.NodeID); ok {
		who = "The mission"
	}
	reason := fmt.Sprintf("%s used %d of %s", who, use.Used, plural(use.Max, strings.TrimSuffix(use.Kind, "s")))
	if use.Granted > 0 {
		reason += fmt.Sprintf(", %d of them granted", use.Granted)
	}
	return reason
}

// appendLimitBlock stops the run at a spent limit before nodeID starts. The
// block names the limit and resumes only with a grant.
func (e *RunEngine) appendLimitBlock(runID string, board *BoardDocument, nodeID string, use RunLimitReached) error {
	reason := limitReason(board, use)
	if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventError, NodeID: nodeID, Data: map[string]any{
		"code": RunBlockLimitReached, "message": reason, "reason": reason, "boundary": "limits", "nodeId": nodeID, "recoverable": true, "limit": use,
	}}); err != nil {
		return err
	}
	block := runBlockedEvent(reason, nodeID, "", "", nil)
	block.Data["code"] = RunBlockLimitReached
	block.Data["limit"] = use
	block.Data["resumePolicy"] = ResumePolicyGrant
	return e.store.AppendRunEvent(runID, block)
}

// runLimitReached is the limit the block at index records, or nil.
func runLimitReached(events []RunEvent, index int) *RunLimitReached {
	if index < 0 || index >= len(events) || events[index].Type != RunEventBlocked {
		return nil
	}
	switch raw := events[index].Data["limit"].(type) {
	case RunLimitReached:
		return &raw
	case *RunLimitReached:
		return raw
	case map[string]any:
		return &RunLimitReached{
			Kind: stringFromAny(raw["kind"]), LimitID: stringFromAny(raw["limitId"]), NodeID: stringFromAny(raw["nodeId"]),
			Used: intFromRunEventData(raw["used"]), Max: intFromRunEventData(raw["max"]), Granted: intFromRunEventData(raw["granted"]),
		}
	}
	return nil
}

// runBlockCode is the code a block records, or the code of the error recorded
// just before it for the same node.
func runBlockCode(events []RunEvent, index int) string {
	block := events[index]
	if code := stringFromEventData(block, "code"); code != "" {
		return code
	}
	if index == 0 {
		return ""
	}
	previous := events[index-1]
	if previous.Type != RunEventError || previous.NodeID != block.NodeID || previous.GateID != block.GateID {
		return ""
	}
	return stringFromEventData(previous, "code")
}

// runBlockResumeAllowed reports whether the block at index may be resumed.
func runBlockResumeAllowed(events []RunEvent, index int) bool {
	return boolFromEventData(events[index], "resumeAllowed")
}

// runBlockNeedsGrant reports whether the block at index resumes only with a
// grant: a spent limit.
func runBlockNeedsGrant(events []RunEvent, index int) bool {
	return stringFromEventData(events[index], "resumePolicy") == ResumePolicyGrant
}
