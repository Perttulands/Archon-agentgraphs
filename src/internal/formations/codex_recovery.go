package formations

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReattachFormationDispatch implements the engine's recovery seam using an
// explicitly selected, already completed native turn. It never sends input,
// adopts a live session, or creates a replacement for the unresolved dispatch.
func (e *TmuxFormationExecutor) ReattachFormationDispatch(req FormationReattachRequest) (FormationExecutionResult, error) {
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
func (e *TmuxFormationExecutor) readCompletedFormationDispatch(req FormationReattachRequest) (FormationExecutionResult, error) {
	copy := *e
	e = &copy
	c := e.config
	events, err := e.store.ReadRunEvents(req.RunID)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if cwd := stringFromEventData(events[0], "cwd"); cwd != "" {
		c.Cwd = cwd
		c.Roots = append(append([]string{}, c.Roots...), cwd)
	}
	e.config = c
	dispatch := NewSlotDispatcher(e.store, nil).dispatchEvent(req.RunID, req.DispatchID)
	if dispatch.Type == "" || dispatch.NodeID != req.NodeID || dispatch.SlotID != req.SlotID {
		return FormationExecutionResult{}, errors.New("recovery dispatch identity mismatch")
	}
	if len(req.Formation.Slots) != 1 {
		return FormationExecutionResult{}, errors.New("completed recovery requires a single-slot formation")
	}
	if c.RecoveryBrief == "" {
		c.RecoveryBrief = stringFromEventData(dispatch, "briefPath")
		root := c.StateDir
		if root == "" {
			root = e.store.Workspace
		}
		resolved, err := filepath.EvalSymlinks(c.RecoveryBrief)
		if err != nil {
			return FormationExecutionResult{}, err
		}
		briefRoot, err := filepath.EvalSymlinks(filepath.Join(root, "briefs"))
		if err != nil {
			return FormationExecutionResult{}, err
		}
		rel, err := filepath.Rel(briefRoot, resolved)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return FormationExecutionResult{}, errors.New("recovery brief must remain under state briefs")
		}
	}
	if !filepath.IsAbs(c.RecoveryBrief) {
		return FormationExecutionResult{}, errors.New("completed recovery requires an absolute brief path")
	}
	brief, err := os.ReadFile(c.RecoveryBrief)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if etag(brief) != stringFromEventData(dispatch, "promptSha256") {
		return FormationExecutionResult{}, errors.New("recovery brief does not match the dispatched prompt digest")
	}
	pointer := "Read the file " + c.RecoveryBrief + " and execute it exactly; it is your whole brief."
	card, err := e.personas.ReadPersona(stringFromEventData(dispatch, "agentId"))
	if err != nil {
		return FormationExecutionResult{}, err
	}
	variant, err := card.SelectHarnessVariant(stringFromEventData(dispatch, "harness"))
	if err != nil {
		return FormationExecutionResult{}, err
	}
	root := c.CodexTranscriptRoot
	if variant.ID == "claude-code" {
		root = c.ClaudeTranscriptRoot
	}
	seat := &nativeSeat{root: root, variant: variant}
	var turn codexTranscriptTurn
	if c.RecoveryTranscript != "" {
		if !filepath.IsAbs(c.RecoveryTranscript) {
			return FormationExecutionResult{}, errors.New("absolute transcript path required")
		}
		turn, err = readSeatTurn(seat, c.RecoveryTranscript, c.Cwd, pointer)
	} else {
		if !filepath.IsAbs(root) {
			return FormationExecutionResult{}, errors.New("absolute transcript root required")
		}
		// The consumed event recorded the native session; read that transcript
		// directly instead of parsing every transcript under the root. The
		// dispatch time bounds any fallback walk.
		nativeID := ""
		for _, event := range events {
			if event.Type == "seat_prompt_consumed" && stringFromEventData(event, "dispatchId") == req.DispatchID {
				nativeID = stringFromEventData(event, "nativeSessionId")
			}
		}
		if created, parseErr := time.Parse(time.RFC3339Nano, dispatch.Timestamp); parseErr == nil {
			seat.created = created
		}
		if path := findTranscriptBySessionID(root, nativeID); path != "" {
			turn, err = readSeatTurn(seat, path, c.Cwd, pointer)
		} else {
			turn, _, err = findSeatTurn(seat, c.Cwd, pointer)
		}
	}
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if !turn.Complete || variant.verifyTurnSettings(turn) != nil {
		return FormationExecutionResult{}, errors.New("recovery requires a completed exact turn with the configured model and effort")
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
	result, err := e.formationResultFromText(FormationExecution{RunID: req.RunID, NodeID: req.NodeID, Formation: req.Formation}, "", turn.Text)
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

// BlockInterruptedRun records every unresolved dispatch before any restart
// recovery attempt. A failed recovery therefore remains visible and resumable.
func (e *RunEngine) BlockInterruptedRun(runID string) error {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	refs := unresolvedDispatches(events)
	if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventError, Data: map[string]any{
		"code": "coordinator_interrupted", "message": fmt.Sprintf("coordinator restarted with open dispatches: %v", refs), "openDispatches": refs,
	}}); err != nil {
		return err
	}
	return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventBlocked, Data: map[string]any{
		"reason": "coordinator restarted; completed-turn evidence required", "openDispatches": refs, "resumeAllowed": true,
	}})
}

// findTranscriptBySessionID returns the one native transcript whose file name
// carries the recorded session id, or "" when none or several exist.
func findTranscriptBySessionID(root, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	found := ""
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".jsonl" || !strings.Contains(filepath.Base(path), sessionID) {
			return nil
		}
		if found != "" {
			found = "\x00"
			return fs.SkipAll
		}
		found = path
		return nil
	})
	if found == "\x00" {
		return ""
	}
	return found
}
