package formations

// Limit blocks (archon-n7u.6). A run that exhausts its attempts or dispatches
// blocks, and resuming it cannot progress: the engine counts every recorded
// start, and neither resume nor a new engine replenishes the allowance. Such a
// block records resumeAllowed false, and every reader derives the limit it hit
// from the block's code and the ledger's counts.

// Codes of the blocks that exhaust a run limit.
const (
	RunBlockResumeAttemptsExhausted = "resume_attempts_exhausted"
	RunBlockReviseLoopExhausted     = "revise_loop_exhausted"
	RunBlockMaxDispatchExceeded     = "max_dispatch_exceeded"
)

// Kinds of run limit.
const (
	RunLimitAttempts   = "attempts"
	RunLimitDispatches = "dispatches"
)

// RunLimitReached is the limit a block exhausted: how much of it the run used.
// Attempts count one node's starts; dispatches count the run's formation starts.
type RunLimitReached struct {
	Kind   string `json:"kind"`
	NodeID string `json:"nodeId,omitempty"`
	Used   int    `json:"used"`
	Max    int    `json:"max"`
}

func isRunLimitCode(code string) bool {
	return code == RunBlockResumeAttemptsExhausted || code == RunBlockReviseLoopExhausted || code == RunBlockMaxDispatchExceeded
}

// runBlockCode is the code a block records, or the code of the error recorded
// just before it for the same node, as appendErrorAndBlock writes them.
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

// runLimitReached reports the limit the block at index exhausted, or nil when
// the event is not a limit block.
func runLimitReached(events []RunEvent, index int) *RunLimitReached {
	if index < 0 || index >= len(events) || events[index].Type != RunEventBlocked {
		return nil
	}
	code := runBlockCode(events, index)
	if !isRunLimitCode(code) {
		return nil
	}
	limits := RunLimits{}
	if len(events) > 0 && events[0].Type == RunEventStarted {
		limits = runLimitsFromEvent(events[0])
	}
	nodeID := events[index].NodeID
	if nodeID == "" {
		nodeID = stringFromEventData(events[index], "blockedNodeId")
	}
	if code == RunBlockMaxDispatchExceeded {
		return &RunLimitReached{Kind: RunLimitDispatches, NodeID: nodeID, Used: formationStartsBefore(events, index), Max: limits.MaxDispatch}
	}
	return &RunLimitReached{Kind: RunLimitAttempts, NodeID: nodeID, Used: nodeAttemptsBefore(events, index, nodeID), Max: limits.MaxAttempts}
}

// formationStartsBefore counts the dispatches the run consumed before index,
// as startFormationExecution counts them.
func formationStartsBefore(events []RunEvent, index int) int {
	consumed := 0
	for _, event := range events[:index] {
		if event.Type == RunEventNodeStarted && stringFromEventData(event, "nodeKind") == "formation" {
			consumed++
		}
	}
	return consumed
}

// nodeAttemptsBefore is the node's latest recorded attempt before index.
func nodeAttemptsBefore(events []RunEvent, index int, nodeID string) int {
	attempts := 0
	for _, event := range events[:index] {
		if event.Type == RunEventNodeStarted && event.NodeID == nodeID && event.Attempt > attempts {
			attempts = event.Attempt
		}
	}
	return attempts
}

// runBlockResumeAllowed reports whether the block at index may be resumed.
func runBlockResumeAllowed(events []RunEvent, index int) bool {
	return boolFromEventData(events[index], "resumeAllowed")
}
