package formations

import (
	"context"
	"errors"
)

// Stopping a run (archon-1uyi). Several steps can work at once, and a block
// accepts no event but a resume, a cancel or a failure, so a run never blocks
// while any of its work is in flight. What stops the run records its error at
// once, where the operator sees it. The run then starts no work, neither a
// step, a judge nor a code check; it still routes the verdicts its recorded
// results decide, opens gate evaluations and asks the operator, records what
// its other work in flight finishes, and then leaves the stop's block or final
// event. The first stop's block is the one left, except that a stop that ends
// the run outranks one that blocks it, a shutdown outranks both, and the
// operator's cancel leaves nothing, since the coordinator ends the run.

// runStop is why a run stops, and what it leaves once nothing runs.
type runStop struct {
	// abort is the operator's cancel: the coordinator records the end.
	abort bool
	// shutdown is the coordinator stopping: the run blocks with every open
	// dispatch, for the restart to settle.
	shutdown bool
	// final marks a stop that ends the run rather than blocking it.
	final  bool
	nodeID string
	// pendingOutput is an attempt that finished and whose outputs a shutdown
	// stopped keeping, carried on the shutdown's block.
	pendingOutput map[string]any
	// block appends what any other stop leaves: its block or final event.
	block func() error
}

// leave appends what the stop leaves.
func (stop runStop) leave(e *RunEngine, runID string) error {
	switch {
	case stop.abort:
		return nil
	case stop.shutdown:
		return e.appendShutdownBlock(runID, stop.nodeID, stop.pendingOutput)
	}
	return stop.block()
}

// stepError names the step whose attempt an error stopped, such as a judge
// in a gate's chain.
type stepError struct {
	NodeID string
	Err    error
}

func (e *stepError) Error() string { return e.Err.Error() }
func (e *stepError) Unwrap() error { return e.Err }

// stopFor records why an attempt of nodeID failed and returns the stop it
// leaves. Only the run owner's cancellation is an abort; executor-local
// cancellation still needs failure evidence, and a shutdown keeps its
// recovery block.
func (e *RunEngine) stopFor(runID, nodeID string, err error) (runStop, error) {
	var step *stepError
	if errors.As(err, &step) {
		nodeID = step.NodeID
	}
	if errors.Is(err, context.Canceled) && errors.Is(context.Cause(e.runContext(runID)), context.Canceled) {
		return runStop{abort: true}, nil
	}
	var pending *pendingOutputError
	if errors.As(err, &pending) {
		return e.stopForPendingOutput(runID, *pending)
	}
	var limit *LimitReachedError
	if errors.As(err, &limit) {
		board, readErr := e.readRunBoard(runID)
		if readErr != nil {
			return runStop{}, readErr
		}
		return e.stopAtLimit(runID, board, nodeID, limit.Use)
	}
	if errors.Is(err, ErrCoordinatorShutdown) {
		return runStop{shutdown: true, nodeID: nodeID}, nil
	}
	var payload *outputPayloadError
	if errors.As(err, &payload) {
		return e.stopWithError(runID, payload.Code, payload.Message, "engine", nodeID, payload.Reason, "", "")
	}
	failure := executionFailureEvent(err)
	if failure.NodeID != "" {
		nodeID = failure.NodeID
	}
	return e.stopWithError(runID, failure.Code, failure.Message, failure.Boundary, nodeID, failure.Message, failure.SlotID, failure.DispatchID)
}

// appendExecutionFailureAndBlock records an attempt's failure and leaves its
// stop at once, for a caller that runs nothing else meanwhile.
func (e *RunEngine) appendExecutionFailureAndBlock(runID, nodeID string, err error) error {
	stop, err := e.stopFor(runID, nodeID, err)
	if err != nil {
		return err
	}
	return stop.leave(e, runID)
}

// stopWithError records an error on nodeID; the stop blocks the run, naming
// the dispatch the error left open, if any. The block carries its own code,
// since other work records events between the error and the block.
func (e *RunEngine) stopWithError(runID, code, message, boundary, nodeID, blockReason, slotID, dispatchID string) (runStop, error) {
	message = redactLedgerText(message)
	blockReason = redactLedgerText(blockReason)
	data := map[string]any{
		"code":        code,
		"message":     message,
		"reason":      blockReason,
		"boundary":    boundary,
		"nodeId":      nodeID,
		"recoverable": true,
	}
	if slotID != "" {
		data["slotId"] = slotID
	}
	if dispatchID != "" {
		data["dispatchId"] = dispatchID
	}
	if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventError, NodeID: nodeID, SlotID: slotID, Data: data}); err != nil {
		return runStop{}, err
	}
	return runStop{nodeID: nodeID, block: func() error {
		block := runBlockedEvent(blockReason, nodeID, "", slotID, openDispatchesForBlock(nodeID, slotID, dispatchID))
		block.Data["code"] = code
		return e.store.AppendRunEvent(runID, block)
	}}, nil
}

// appendErrorAndBlock records an error on nodeID and blocks the run at once.
func (e *RunEngine) appendErrorAndBlock(runID, code, message, boundary, nodeID, blockReason string) error {
	stop, err := e.stopWithError(runID, code, message, boundary, nodeID, blockReason, "", "")
	if err != nil {
		return err
	}
	return stop.leave(e, runID)
}

// stopAtGateError records a gate's evaluator error; the stop blocks the run
// at the gate, whose evaluation a resume finishes.
func (e *RunEngine) stopAtGateError(runID, gateID, code, message, boundary, blockReason string) (runStop, error) {
	message = redactLedgerText(message)
	blockReason = redactLedgerText(blockReason)
	if err := e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventError,
		GateID: gateID,
		NodeID: gateID,
		Data: map[string]any{
			"code":        code,
			"message":     message,
			"reason":      blockReason,
			"boundary":    boundary,
			"gateId":      gateID,
			"recoverable": true,
		},
	}); err != nil {
		return runStop{}, err
	}
	return runStop{nodeID: gateID, block: func() error {
		block := runBlockedEvent(blockReason, "", gateID, "", nil)
		block.Data["code"] = code
		return e.store.AppendRunEvent(runID, block)
	}}, nil
}

// stopAtInvalidJudge records a judge's malformed verdict; the stop blocks the
// run at its gate, and a plain resume asks the final judge again.
func (e *RunEngine) stopAtInvalidJudge(runID, gateID string, err error) (runStop, error) {
	return e.stopAtJudgeAttempt(runID, gateID, "invalid_judge_result", "invalid judge result: ", err)
}

// stopAtInvalidPick records a judge's malformed pick, as a malformed verdict.
func (e *RunEngine) stopAtInvalidPick(runID, gateID string, err error) (runStop, error) {
	return e.stopAtJudgeAttempt(runID, gateID, RunBlockInvalidJudgePick, "invalid judge pick: ", err)
}

func (e *RunEngine) stopAtJudgeAttempt(runID, gateID, code, prefix string, err error) (runStop, error) {
	attempt, readErr := e.gateAttempt(runID, gateID)
	if readErr != nil {
		return runStop{}, readErr
	}
	if appendErr := e.store.AppendRunEvent(runID, RunEvent{
		Type: RunEventJudgeAttemptFailed, GateID: gateID, NodeID: gateID, Attempt: attempt,
		Data: map[string]any{"code": code, "reason": err.Error()},
	}); appendErr != nil {
		return runStop{}, appendErr
	}
	return runStop{nodeID: gateID, block: func() error {
		return e.store.AppendRunEvent(runID, RunEvent{
			Type: RunEventBlocked, GateID: gateID, NodeID: gateID,
			Data: map[string]any{"reason": prefix + err.Error(), "blockedGateId": gateID, "resumeAllowed": true, "code": code},
		})
	}}, nil
}

// stopAtLimit is what a spent limit does to nodeID: a card whose whenSpent is
// fail ends the run, keeping the attempts as evidence; any other records the
// limit and blocks the run there, resumable with a grant, or, when the step's
// own card is spent and its selecting gate judged an attempt, with a grant or
// a pick of the best attempt (archon-gihe).
func (e *RunEngine) stopAtLimit(runID string, board *BoardDocument, nodeID string, use RunLimitReached) (runStop, error) {
	if card, ok := findLimit(board, use.LimitID); ok && card.WhenSpent == LimitWhenSpentFail {
		return runStop{final: true, nodeID: nodeID, block: func() error {
			if err := e.EndKeptSeats(runID); err != nil {
				return err
			}
			return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventFailed, Actor: RunFailureActor, NodeID: nodeID, Data: map[string]any{
				"code": RunBlockLimitReached, "reason": limitReason(board, use), "limit": use, "final": true,
			}})
		}}, nil
	}
	if err := e.appendLimitError(runID, board, nodeID, use); err != nil {
		return runStop{}, err
	}
	return runStop{nodeID: nodeID, block: func() error {
		events, err := e.store.ReadRunEvents(runID)
		if err != nil {
			return err
		}
		policy := ResumePolicyGrant
		if _, ok := pickFor(board, events, nodeID, use); ok {
			policy = ResumePolicyGrantOrPick
		}
		block := runBlockedEvent(limitReason(board, use), nodeID, "", "", nil)
		block.Data["code"] = RunBlockLimitReached
		block.Data["limit"] = use
		block.Data["resumePolicy"] = policy
		return e.store.AppendRunEvent(runID, block)
	}}, nil
}

// appendLimitBlock leaves a spent limit's stop at once.
func (e *RunEngine) appendLimitBlock(runID string, board *BoardDocument, nodeID string, use RunLimitReached) error {
	stop, err := e.stopAtLimit(runID, board, nodeID, use)
	if err != nil {
		return err
	}
	return stop.leave(e, runID)
}

// stopForPendingOutput is the stop of an attempt that finished without its
// outputs kept: resume keeps them (keepPendingOutput), and resume in
// redispatch mode runs the step again instead.
func (e *RunEngine) stopForPendingOutput(runID string, pending pendingOutputError) (runStop, error) {
	carried := map[string]any{"nodeId": pending.NodeID, "attempt": pending.Attempt, "output": pending.Output}
	if errors.Is(pending.Err, ErrCoordinatorShutdown) {
		return runStop{shutdown: true, nodeID: pending.NodeID, pendingOutput: carried}, nil
	}
	message := redactLedgerText(pending.Err.Error())
	reason := message + "; resume keeps the outputs without running the step again"
	if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventError, NodeID: pending.NodeID, Data: map[string]any{
		"code": RunBlockOutputNotKept, "message": message, "reason": reason, "boundary": "engine", "nodeId": pending.NodeID, "recoverable": true,
	}}); err != nil {
		return runStop{}, err
	}
	return runStop{nodeID: pending.NodeID, block: func() error {
		block := runBlockedEvent(reason, pending.NodeID, "", "", nil)
		block.Data["code"] = RunBlockOutputNotKept
		block.Data["pendingOutput"] = carried
		return e.store.AppendRunEvent(runID, block)
	}}, nil
}

// appendShutdownBlock blocks a run the coordinator's shutdown stopped, naming
// every dispatch it left open, or carrying the outputs of an attempt that
// finished while they were being kept, which resume keeps without running the
// step again.
func (e *RunEngine) appendShutdownBlock(runID, nodeID string, pendingOutput map[string]any) error {
	if pendingOutput != nil {
		block := runBlockedEvent("coordinator shutdown while keeping "+nodeID+"'s outputs; resume keeps them without running the step again", nodeID, "", "", nil)
		block.Data["code"] = RunBlockCoordinatorShutdown
		block.Data["pendingOutput"] = pendingOutput
		return e.store.AppendRunEvent(runID, block)
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventBlocked, NodeID: nodeID, Data: map[string]any{
		"code": RunBlockCoordinatorShutdown, "reason": "coordinator shutdown; inspect dispatch evidence before resume", "resumeAllowed": true, "openDispatches": unresolvedDispatches(events),
	}})
}
