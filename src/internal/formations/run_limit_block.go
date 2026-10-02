package formations

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Spent limits (archon-o7p.8). A run has no limits unless its frozen mission
// holds a Limit card. Each knob a card sets is counted from the ledger, so
// replay and every reader agree: rounds count a step's starts, or every step
// start of the run for a card on the Input card; time counts the wall time
// while the step, or any step for the mission, is running and the run is not
// blocked; tokens count the token_usage the executor recorded for the step,
// or for every step. A grant, recorded on the run_resumed that gives it, adds
// one more allowance: a round, or the card's time or tokens again. At the limit the run blocks
// with the limit on the block itself, resumable only with a grant.

// RunLimitReached is a Limit card's knob as the run used it: Used of Max, Max
// counting the card's own value and every grant. Time is in whole seconds.
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
// its output because the coordinator stopped, by a crash or a shutdown such
// as a deploy, so the run started it again. A run reaches the same limits
// wherever a restart falls.
func cutShortByRestart(events []RunEvent, index int) bool {
	nodeID, interrupted := events[index].NodeID, false
	for _, event := range events[index+1:] {
		switch {
		case event.NodeID == nodeID && event.Type == RunEventNodeStarted:
			return interrupted
		case event.NodeID == nodeID && event.Type == RunEventNodeOutput:
			return false
		case coordinatorStopped(event):
			interrupted = true
		}
	}
	return interrupted
}

// coordinatorStopped reports the event a stopped coordinator leaves: the
// error a restart records after a crash, or the block a shutdown records.
func coordinatorStopped(event RunEvent) bool {
	code := stringFromEventData(event, "code")
	return event.Type == RunEventError && code == RunBlockCoordinatorInterrupted || event.Type == RunEventBlocked && code == RunBlockCoordinatorShutdown
}

// roundsUse is a rounds card's use, or nil when the card sets no rounds.
func roundsUse(board *BoardDocument, events []RunEvent, limit LimitNode) *RunLimitReached {
	if limit.Rounds == nil || *limit.Rounds <= 0 {
		return nil
	}
	granted := limitGranted(events, limit.ID, LimitKindRounds)
	return &RunLimitReached{Kind: LimitKindRounds, LimitID: limit.ID, NodeID: limit.Target, Used: roundsUsed(board, events, limit), Max: *limit.Rounds + granted, Granted: granted}
}

// limitSpentBefore is the limit that starting the step now would pass: the
// step's own card first, then the mission's, rounds before time before
// tokens. Nil means the step may start. A peer step's own rounds are its
// journal messages, not its starts.
func limitSpentBefore(board *BoardDocument, events []RunEvent, formation FormationNode, now time.Time) *RunLimitReached {
	spent := func(limit LimitNode, rounds bool) *RunLimitReached {
		uses := []*RunLimitReached{timeUse(board, events, limit, now), tokensUse(board, events, limit)}
		if rounds {
			uses = append([]*RunLimitReached{roundsUse(board, events, limit)}, uses...)
		}
		for _, use := range uses {
			if use != nil && use.Used >= use.Max {
				return use
			}
		}
		return nil
	}
	if limit, ok := limitCovering(board, formation.ID); ok {
		if use := spent(limit, formation.Type != FormationTypePeer); use != nil {
			return use
		}
	}
	for _, mission := range board.Missions {
		if limit, ok := limitCovering(board, mission.ID); ok {
			if use := spent(limit, true); use != nil {
				return use
			}
		}
	}
	return nil
}

// timeUsed is the whole seconds a card's time has counted by now: while the
// covered step has an attempt running, or for the mission while any step has,
// and the run is not blocked. An attempt runs from its node_started to its
// output, its own error, its abandonment or the run's end. Waiting on a human
// gate, between steps or blocked counts nothing; a send-back resumes a step
// with the time it has left.
func timeUsed(board *BoardDocument, events []RunEvent, limit LimitNode, now time.Time) int {
	mission := isMissionLimit(board, limit)
	open := map[string]bool{}
	blocked := false
	var used time.Duration
	var last time.Time
	count := func(at time.Time) {
		if !last.IsZero() && len(open) > 0 && !blocked && at.After(last) {
			used += at.Sub(last)
		}
		if !at.IsZero() {
			last = at
		}
	}
	for _, event := range events {
		at, _ := time.Parse(time.RFC3339Nano, event.Timestamp)
		if event.Type == RunEventError && stringFromEventData(event, "code") == RunBlockCoordinatorInterrupted {
			// A restart after a crash records this when it comes up: the time
			// since the last event before it was mostly downtime, and counts
			// nothing.
			last = time.Time{}
		}
		count(at)
		covered := mission || event.NodeID == limit.Target
		switch event.Type {
		case RunEventNodeStarted:
			if covered && stringFromEventData(event, "nodeKind") == "formation" {
				open[event.NodeID] = true
			}
		case RunEventNodeOutput, RunEventError:
			delete(open, event.NodeID)
		case RunEventSlotResult:
			if stringFromEventData(event, "status") == "abandoned" {
				delete(open, event.NodeID)
			}
		case RunEventBlocked:
			blocked = true
		case RunEventResumed:
			blocked = false
		default:
			if isFinalRunEvent(event.Type) {
				clear(open)
			}
		}
	}
	count(now)
	return int(used / time.Second)
}

// timeUse is a time card's use by now, or nil when the card sets no time.
func timeUse(board *BoardDocument, events []RunEvent, limit LimitNode, now time.Time) *RunLimitReached {
	if limit.Seconds == nil || *limit.Seconds <= 0 {
		return nil
	}
	granted := limitGranted(events, limit.ID, LimitKindTime)
	return &RunLimitReached{Kind: LimitKindTime, LimitID: limit.ID, NodeID: limit.Target, Used: timeUsed(board, events, limit, now), Max: *limit.Seconds + granted, Granted: granted}
}

// LimitWarning is a time card's warning for the seats of the step about to
// run: pasted once into each seat at At.
type LimitWarning struct {
	LimitID string
	// Mission marks the mission's card, whose warning goes only to the seats
	// working when it fires, once in the whole run.
	Mission bool
	At      time.Time
	Text    string
}

// timeBudget is what the time cards covering a step allow it from now: the
// use of the card that runs out first, nil when no card sets time, and the
// warnings its seats get. The step stops when that card's time is spent.
func timeBudget(board *BoardDocument, events []RunEvent, nodeID string, now time.Time) (*RunLimitReached, []LimitWarning) {
	var first *RunLimitReached
	var warnings []LimitWarning
	for _, limit := range coveringLimits(board, nodeID) {
		use := timeUse(board, events, limit, now)
		if use == nil {
			continue
		}
		left := use.Max - use.Used
		if first == nil || left < first.Max-first.Used {
			first = use
		}
		mission := isMissionLimit(board, limit)
		if limit.WarnSeconds == nil || *limit.WarnSeconds <= 0 || (mission && limitWarned(events, limit.ID)) {
			continue
		}
		warnLeft := min(left, *limit.WarnSeconds)
		warnings = append(warnings, LimitWarning{LimitID: limit.ID, Mission: mission, At: now.Add(time.Duration(left-warnLeft) * time.Second),
			Text: limitWarningText(board, limit, warnLeft)})
	}
	return first, warnings
}

// limitWarned reports whether a card's warning already went out.
func limitWarned(events []RunEvent, limitID string) bool {
	for _, event := range events {
		if event.Type == RunEventLimitWarning && stringFromEventData(event, "limitId") == limitID {
			return true
		}
	}
	return false
}

// limitWarningText is what a warned seat reads.
func limitWarningText(board *BoardDocument, limit LimitNode, left int) string {
	whose := "this step's"
	if isMissionLimit(board, limit) {
		whose = "the mission's"
	}
	return fmt.Sprintf("Archon: %s left of %s working time (Limit card %s). When it runs out the step stops and the run waits for the operator. Finish your output now.",
		durationWords(left), whose, nodeName(board, limit.ID))
}

// peerGrantRound is how many rounds a grant gives the limit a run spent: one,
// or for a peer step's own card, whose rounds are journal messages, one round
// of the conversation, room for a proposal and every peer's acknowledgement,
// so the step can agree in it (archon-o7p.8.1).
func peerGrantRound(board *BoardDocument, limit RunLimitReached) int {
	if limit.Kind != LimitKindRounds {
		return 1
	}
	if formation, ok := findFormation(board.Formations, limit.NodeID); ok && formation.Type == FormationTypePeer {
		return len(formation.Slots) + 1
	}
	return 1
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
// rounds", "The mission used 20 of 20 rounds, 1 of them granted", "Review
// used 30 min of 30 min", "Review used 51,230 of 50,000 tokens".
func limitReason(board *BoardDocument, use RunLimitReached) string {
	who := nodeName(board, use.NodeID)
	if _, ok := findMission(board, use.NodeID); ok {
		who = "The mission"
	}
	if use.Kind == LimitKindTime {
		reason := fmt.Sprintf("%s used %s of %s", who, durationWords(use.Used), durationWords(use.Max))
		if use.Granted > 0 {
			reason += fmt.Sprintf(", %s of it granted", durationWords(use.Granted))
		}
		return reason
	}
	if use.Kind == LimitKindTokens {
		reason := fmt.Sprintf("%s used %s of %s", who, strings.TrimSuffix(tokenWords(use.Used), " tokens"), tokenWords(use.Max))
		if use.Granted > 0 {
			reason += fmt.Sprintf(", %s of them granted", strings.TrimSuffix(tokenWords(use.Granted), " tokens"))
		}
		return reason
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

// GrantWords says what a grant gives a spent limit: "one more round", or the
// card's time or tokens again, "5 min more", "50,000 tokens more".
func GrantWords(limit RunLimitReached) string {
	switch limit.Kind {
	case LimitKindTime:
		return durationWords(limit.Max-limit.Granted) + " more"
	case LimitKindTokens:
		return tokenWords(limit.Max-limit.Granted) + " more"
	}
	return "one more round"
}

// SpentWords says what a spent limit used up: "its only round", "all 3 of its
// rounds", "all 30 min of its time".
func SpentWords(limit RunLimitReached) string {
	switch {
	case limit.Kind == LimitKindTime:
		return "all " + durationWords(limit.Max) + " of its time"
	case limit.Kind == LimitKindTokens:
		return "all " + tokenWords(limit.Max) + " it may spend"
	case limit.Max == 1:
		return "its only " + strings.TrimSuffix(limit.Kind, "s")
	default:
		return fmt.Sprintf("all %d of its %s", limit.Max, limit.Kind)
	}
}

// LeftWords says what a time card has left: "25 min of 30 min left".
func LeftWords(limit RunLimitReached) string {
	return durationWords(max(limit.Max-limit.Used, 0)) + " of " + durationWords(limit.Max) + " left"
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

// claimLimitWarning records that a seat gets a time card's warning, once: a
// step's card warns each seat of an attempt once, the mission's card only the
// seats of the attempt that first claimed it. It reports false when the
// ledger already holds that warning.
func (s *Store) claimLimitWarning(req FormationExecution, slotID string, warning LimitWarning) bool {
	event := RunEvent{Type: RunEventLimitWarning, NodeID: req.NodeID, SlotID: slotID, Attempt: req.Attempt, Data: map[string]any{
		"limitId": warning.LimitID, "nodeId": req.NodeID, "slotId": slotID, "attempt": req.Attempt, "text": warning.Text,
	}}
	err := s.appendRunEventIf(req.RunID, event, func(events []RunEvent) error {
		for _, event := range events {
			if event.Type != RunEventLimitWarning || stringFromEventData(event, "limitId") != warning.LimitID {
				continue
			}
			sameAttempt := event.NodeID == req.NodeID && event.Attempt == req.Attempt
			if (sameAttempt && event.SlotID == slotID) || (warning.Mission && !sameAttempt) {
				return errNothingToRecord
			}
		}
		return nil
	})
	return err == nil
}

// awaitLimitWarnings calls warn with each warning at its time, once started
// closes, until ctx ends.
func awaitLimitWarnings(ctx context.Context, warnings []LimitWarning, started <-chan struct{}, warn func(LimitWarning)) {
	select {
	case <-ctx.Done():
		return
	case <-started:
	}
	pending := append([]LimitWarning(nil), warnings...)
	sort.Slice(pending, func(i, j int) bool { return pending[i].At.Before(pending[j].At) })
	for _, warning := range pending {
		timer := time.NewTimer(time.Until(warning.At))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		warn(warning)
	}
}
