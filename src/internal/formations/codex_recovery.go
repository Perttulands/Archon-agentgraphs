package formations

import (
	"errors"
	"os"
	"path/filepath"
)

// ReattachFormationDispatch implements the engine's recovery seam using an
// explicitly selected, already completed native turn. It never sends input,
// adopts a live session, or creates a replacement for the unresolved dispatch.
func (e *CodexSeatExecutor) ReattachFormationDispatch(req FormationReattachRequest) (FormationExecutionResult, error) {
	result, err := e.readCompletedFormationDispatch(req)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if err := NewSlotDispatcher(e.store, nil).CompleteFromCapture(req.RunID, req.DispatchID, result.Text); err != nil {
		return FormationExecutionResult{}, err
	}
	return result, nil
}

// Validate and copy the completed result before the run ledger is changed.
func (e *CodexSeatExecutor) readCompletedFormationDispatch(req FormationReattachRequest) (FormationExecutionResult, error) {
	c := e.config
	if !filepath.IsAbs(c.RecoveryTranscript) || !filepath.IsAbs(c.RecoveryBrief) {
		return FormationExecutionResult{}, errors.New("completed-turn recovery requires explicit absolute transcript and brief paths")
	}
	dispatcher := NewSlotDispatcher(e.store, nil)
	dispatch := dispatcher.dispatchEvent(req.RunID, req.DispatchID)
	if dispatch.Type == "" || dispatch.NodeID != req.NodeID || dispatch.SlotID != req.SlotID {
		return FormationExecutionResult{}, errors.New("recovery dispatch identity mismatch")
	}
	brief, err := os.ReadFile(c.RecoveryBrief)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if etag(brief) != stringFromEventData(dispatch, "promptSha256") {
		return FormationExecutionResult{}, errors.New("recovery brief does not match the dispatched prompt digest")
	}
	pointer := "Read the file " + c.RecoveryBrief + " and execute it exactly; it is your whole brief."
	turn, err := readCodexTurn(c.RecoveryTranscript, c.Cwd, pointer)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if !turn.Complete || turn.Model != c.Model || turn.Effort != c.Effort {
		return FormationExecutionResult{}, errors.New("recovery requires a completed exact turn with the configured model and effort")
	}
	events, err := e.store.ReadRunEvents(req.RunID)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	matched := false
	for _, event := range events {
		if event.Type == "seat_prompt_consumed" && event.NodeID == req.NodeID && event.SlotID == req.SlotID && stringFromEventData(event, "dispatchId") == req.DispatchID && stringFromEventData(event, "nativeSessionId") == turn.SessionID {
			matched = true
		}
	}
	if !matched {
		return FormationExecutionResult{}, errors.New("recovery transcript is not the native session recorded for this dispatch")
	}
	legacy := NewTmuxFormationExecutor(e.store, e.personas, TmuxExecutorConfig{Cwd: c.Cwd, Roots: []string{c.Cwd, c.StateDir}, OutputCapBytes: 1 << 20})
	result, err := legacy.formationResultFromText(FormationExecution{RunID: req.RunID, NodeID: req.NodeID, Formation: req.Formation}, "", turn.Text)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if _, ok := ParseCompletionSentinel(turn.Text, req.RunID); !ok {
		return FormationExecutionResult{}, errors.New("completed native turn has no matching run completion sentinel")
	}
	return result, nil
}

func unresolvedDispatches(events []RunEvent) []openDispatchRef {
	pending := map[string]openDispatchRef{}
	for _, event := range events {
		id := stringFromEventData(event, "dispatchId")
		if event.Type == RunEventSlotDispatch && id != "" {
			pending[id] = openDispatchRef{DispatchID: id, NodeID: event.NodeID, SlotID: event.SlotID}
		}
		if event.Type == RunEventSlotResult {
			delete(pending, id)
		}
	}
	refs := make([]openDispatchRef, 0, len(pending))
	for _, ref := range pending {
		refs = append(refs, ref)
	}
	return refs
}

func (e *RunEngine) prepareCompletedRecovery(runID string, board *BoardDocument, events []RunEvent) (openDispatchRef, FormationExecutionResult, error) {
	var ref openDispatchRef
	var result FormationExecutionResult
	reader, ok := e.executor.(interface {
		readCompletedFormationDispatch(FormationReattachRequest) (FormationExecutionResult, error)
	})
	if !ok {
		return ref, result, errors.New("executor does not support completed native turn recovery")
	}
	refs := unresolvedDispatches(events)
	if len(refs) != 1 {
		return ref, result, errors.New("completed-turn recovery requires exactly one unresolved dispatch")
	}
	ref = refs[0]
	formation, ok := findFormation(board.Formations, ref.NodeID)
	if !ok {
		return ref, result, errors.New("unresolved dispatch formation is absent from the frozen board")
	}
	result, err := reader.readCompletedFormationDispatch(FormationReattachRequest{RunID: runID, DispatchID: ref.DispatchID, NodeID: ref.NodeID, SlotID: ref.SlotID, Formation: formation})
	return ref, result, err
}

func (e *RunEngine) ValidateCompletedRecovery(runID string) error {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	board, err := e.readRunBoard(runID)
	if err != nil {
		return err
	}
	_, _, err = e.prepareCompletedRecovery(runID, board, events)
	return err
}
