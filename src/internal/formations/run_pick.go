package formations

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/jsonstrict"
)

// Best of N (archon-gihe). A step whose Limit card is spent has attempts its
// gate already judged. A card whose whenSpent is best, or a resume with
// --pick-best, picks the best of those attempts instead of stopping: the
// gate's judge chain compares them side by side, or the operator picks, and
// the picked attempt goes on through the gate's pass route with its output, as
// if the gate had passed it. Nothing downstream marks it as picked.
//
// The ledger records a pick in two events. pick_started names the step, its
// selecting gate, the spent limit and the candidate attempts; it settles the
// step's pending send-back, as the step's output would. The pick ends in a
// gate_verdict pass whose pick names the attempt, the candidates, why and who
// picked. Between the two a judge chain runs, anchored at pick_started, or a
// human_input_requested with a pick waits for the operator. Replay finishes a
// pick the ledger started and did not end, from the judge outputs it holds.

// RunEventPickStarted records that a step's spent limit picks its best attempt.
const RunEventPickStarted = "pick_started"

// What started a pick: the card's whenSpent, or a resume with --pick-best.
const (
	PickTriggerWhenSpent = "whenSpent"
	PickTriggerResume    = "resume"
)

// RunBlockInvalidJudgePick is the block a judge's malformed pick records; a
// plain resume asks the final judge again.
const RunBlockInvalidJudgePick = "invalid_judge_pick"

// ResumePolicyGrantOrPick marks a block at a spent limit that resumes with a
// grant or with a pick of the step's best attempt.
const ResumePolicyGrantOrPick = "grant_or_pick"

// PickDecidedByArchon decides a pick with only one candidate.
const PickDecidedByArchon = "archon"

var (
	// ErrRunNothingToPick refuses --pick-best where no pick is possible.
	ErrRunNothingToPick = errors.New("this block is not a spent limit of a step whose gate judged its attempts, so there is nothing to pick: resume without --pick-best")
	// ErrRunGrantOrPick refuses a resume that both grants and picks.
	ErrRunGrantOrPick = errors.New("give one more allowance with --grant or pick the best attempt with --pick-best, not both")
	// ErrHumanRequestIsAPick refuses a verdict on a pending pick.
	ErrHumanRequestIsAPick = errors.New("this gate waits for the operator to pick the best attempt, not for a verdict: answer it with gate pick")
	// ErrHumanRequestNotAPick refuses a pick on a pending verdict.
	ErrHumanRequestNotAPick = errors.New("this gate waits for a verdict, not a pick: answer it with gate approve or gate reject")
	// ErrPickNotACandidate refuses an attempt the pick does not offer.
	ErrPickNotACandidate = errors.New("not a candidate attempt")
)

// selectingGate is the gate that picks a step's best attempt: the one gate
// that judges the step's output, its in port fed by the step and nothing
// else, and whose fail route sends the work back to the step. A judge or a
// human must decide it, since a code check cannot compare attempts. problem
// says plainly why the step has none.
func selectingGate(board *BoardDocument, stepID string) (GateNode, string) {
	if board == nil {
		return GateNode{}, "the mission has no gates"
	}
	var found []GateNode
	for _, gate := range board.Gates {
		fed, other := false, false
		for _, connection := range board.Connections {
			toNode, toPort := endpointParts(connection.To)
			if toNode != gate.ID || toPort != "in" {
				continue
			}
			if fromNode, _ := endpointParts(connection.From); fromNode == stepID {
				fed = true
			} else {
				other = true
			}
		}
		if !fed || other {
			continue
		}
		for _, connection := range outgoingConnectionsFromPort(board.Connections, gate.ID, "fail") {
			if toNode, _ := endpointParts(connection.To); toNode == stepID {
				found = append(found, gate)
				break
			}
		}
	}
	switch len(found) {
	case 0:
		return GateNode{}, "no gate judges it and sends it back: wire a judge or human gate that only it feeds and whose fail returns to it, or set it to stop"
	case 1:
	default:
		return GateNode{}, fmt.Sprintf("%s and %s both judge it and send it back, so neither can pick: keep one, or set it to stop", nodeName(board, found[0].ID), nodeName(board, found[1].ID))
	}
	gate := found[0]
	if !hasGateKind(gate.Kinds, "formation") && !hasGateKind(gate.Kinds, "human") {
		return GateNode{}, fmt.Sprintf("its gate %s checks with code only, which cannot compare attempts: give it a judge or a human, or set it to stop", nodeName(board, gate.ID))
	}
	return gate, ""
}

// pickCandidate is an attempt a pick may take: the step's output was
// accepted (OutputSeq) and its selecting gate judged it (VerdictSeq). An
// attempt a limit cut off, or a crash ended, has no output and is never one.
type pickCandidate struct {
	Attempt    int `json:"attempt"`
	OutputSeq  int `json:"outputSeq"`
	VerdictSeq int `json:"verdictSeq"`
}

// pickCandidateDetail is a candidate as the ledger shows it: the verdict the
// gate gave it and the gate input that verdict judged, which a pick carries on.
type pickCandidateDetail struct {
	pickCandidate
	Verdict string
	Reason  string
	Input   RunInputRef
}

// pickCandidates lists, by attempt, the step's attempts whose output the gate
// judged with a verdict of its own; a pick's verdict judges nothing. The
// gate's input names the attempt it judges (RunInputRef.Origin).
func pickCandidates(events []RunEvent, stepID, gateID string) []pickCandidate {
	var candidates []pickCandidate
	outputSeq := map[int]int{}
	var judging *pickCandidate
	for _, event := range events {
		switch event.Type {
		case RunEventNodeOutput:
			if event.NodeID == stepID {
				outputSeq[event.Attempt] = event.Seq
			}
		case RunEventGateEvaluating:
			if eventGateID(event) != gateID {
				continue
			}
			judging = nil
			if origin := runInputRefFromAny(event.Data["inputRef"]).Origin; origin != nil && origin.NodeID == stepID && outputSeq[origin.Attempt] > 0 {
				judging = &pickCandidate{Attempt: origin.Attempt, OutputSeq: outputSeq[origin.Attempt]}
			}
		case RunEventGateVerdict:
			if eventGateID(event) != gateID || judging == nil || isPickEvent(event) {
				continue
			}
			if port := stringFromEventData(event, "routePort"); port != "pass" && port != "fail" {
				continue
			}
			judging.VerdictSeq = event.Seq
			attempt := judging.Attempt
			candidates = slices.DeleteFunc(candidates, func(candidate pickCandidate) bool { return candidate.Attempt == attempt })
			candidates = append(candidates, *judging)
			judging = nil
		}
	}
	slices.SortFunc(candidates, func(a, b pickCandidate) int { return a.Attempt - b.Attempt })
	return candidates
}

// loadPickCandidates reads each candidate's verdict from the ledger.
func loadPickCandidates(events []RunEvent, candidates []pickCandidate) ([]pickCandidateDetail, error) {
	details := make([]pickCandidateDetail, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.VerdictSeq <= 0 || candidate.VerdictSeq > len(events) || events[candidate.VerdictSeq-1].Type != RunEventGateVerdict {
			return nil, fmt.Errorf("%w: pick candidate attempt %d names verdict %d", ErrRunLedgerInvalid, candidate.Attempt, candidate.VerdictSeq)
		}
		verdict := events[candidate.VerdictSeq-1]
		details = append(details, pickCandidateDetail{
			pickCandidate: candidate,
			Verdict:       stringFromEventData(verdict, "verdict"),
			Reason:        stringFromEventData(verdict, "reason"),
			Input:         runInputRefFromAny(verdict.Data["inputRef"]),
		})
	}
	return details, nil
}

// pickCandidatesFromEvent reads the candidates a pick_started recorded.
func pickCandidatesFromEvent(event RunEvent) []pickCandidate {
	switch raw := event.Data["candidates"].(type) {
	case []pickCandidate:
		return append([]pickCandidate(nil), raw...)
	case []any:
		candidates := make([]pickCandidate, 0, len(raw))
		for _, item := range raw {
			fields, ok := item.(map[string]any)
			if !ok {
				continue
			}
			candidates = append(candidates, pickCandidate{
				Attempt:    intFromRunEventData(fields["attempt"]),
				OutputSeq:  intFromRunEventData(fields["outputSeq"]),
				VerdictSeq: intFromRunEventData(fields["verdictSeq"]),
			})
		}
		return candidates
	}
	return nil
}

// isPickEvent reports whether an event belongs to a pick: a pick's gate
// verdict, its human request, or the operator's pick.
func isPickEvent(event RunEvent) bool {
	_, ok := event.Data["pick"]
	return ok
}

// pickData reads an event's pick fields.
func pickData(event RunEvent) map[string]any {
	data, _ := event.Data["pick"].(map[string]any)
	return data
}

// intsFromAny reads a list of whole numbers.
func intsFromAny(value any) []int {
	switch raw := value.(type) {
	case []int:
		return append([]int(nil), raw...)
	case []any:
		values := make([]int, 0, len(raw))
		for _, item := range raw {
			values = append(values, intFromRunEventData(item))
		}
		return values
	}
	return nil
}

// stepPick is what picking a step's best attempt takes: its selecting gate
// and the attempts that gate judged.
type stepPick struct {
	gate       GateNode
	candidates []pickCandidate
}

// pickFor reports whether the spent limit use, met by the step, can pick: it
// is the step's own card, the step has a selecting gate, and that gate judged
// at least one of its attempts. A mission card never picks.
func pickFor(board *BoardDocument, events []RunEvent, stepID string, use RunLimitReached) (stepPick, bool) {
	card, ok := findLimit(board, use.LimitID)
	if !ok || card.Target != stepID || use.NodeID != stepID {
		return stepPick{}, false
	}
	gate, problem := selectingGate(board, stepID)
	if problem != "" {
		return stepPick{}, false
	}
	candidates := pickCandidates(events, stepID, gate.ID)
	if len(candidates) == 0 {
		return stepPick{}, false
	}
	return stepPick{gate: gate, candidates: candidates}, true
}

// judgedSinceLastPick reports whether the gate judged one of the candidates
// after the step's latest pick. A card picks by itself only then, so a pass
// route that leads back to the step cannot pick the same attempts forever.
func judgedSinceLastPick(events []RunEvent, stepID string, candidates []pickCandidate) bool {
	last := 0
	for _, event := range events {
		if event.Type == RunEventPickStarted && event.NodeID == stepID {
			last = event.Seq
		}
	}
	for _, candidate := range candidates {
		if candidate.VerdictSeq > last {
			return true
		}
	}
	return false
}

// pickOrBlock is what a step does at its spent limit. A card whose whenSpent
// is best, with an attempt its gate judged since the step's last pick, starts
// a pick; anything else blocks the run at the limit. cutOff marks an attempt
// the limit stopped while it worked: the ledger records why it stopped, and it
// is never a candidate.
func (e *RunEngine) pickOrBlock(runID string, board *BoardDocument, gates map[string]GateNode, stepID string, use RunLimitReached, cutOff bool, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	card, _ := findLimit(board, use.LimitID)
	pick, ok := pickFor(board, events, stepID, use)
	if !ok || card.WhenSpent != LimitWhenSpentBest || !judgedSinceLastPick(events, stepID, pick.candidates) {
		if err := e.appendLimitBlock(runID, board, stepID, use); err != nil {
			return err
		}
		return errRunStopped
	}
	if cutOff {
		if err := e.appendLimitError(runID, board, stepID, use); err != nil {
			return err
		}
	}
	return e.startPick(runID, board, gates, stepID, pick, use, PickTriggerWhenSpent, ready, queued, queue)
}

// pendingResumePick is the spent limit a resume with --pick-best asked to
// pick at, while that resume is the run's latest and no pick started since.
func pendingResumePick(events []RunEvent) (RunLimitReached, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case RunEventPickStarted:
			return RunLimitReached{}, false
		case RunEventResumed:
			if !boolFromEventData(events[i], "pickBest") {
				return RunLimitReached{}, false
			}
			limit := runLimitReached(events, intFromRunEventData(events[i].Data["resumedFromSeq"])-1)
			if limit == nil {
				return RunLimitReached{}, false
			}
			return *limit, true
		}
	}
	return RunLimitReached{}, false
}

// pickOnResume starts the pick a resume with --pick-best asked for, once
// replay rebuilt the run, whatever the card's whenSpent.
func (e *RunEngine) pickOnResume(runID string, board *BoardDocument, gates map[string]GateNode, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	use, ok := pendingResumePick(events)
	if !ok {
		return nil
	}
	pick, ok := pickFor(board, events, use.NodeID, use)
	if !ok {
		if err := e.appendLimitBlock(runID, board, use.NodeID, use); err != nil {
			return err
		}
		return errRunStopped
	}
	return e.startPick(runID, board, gates, use.NodeID, pick, use, PickTriggerResume, ready, queued, queue)
}

// startPick records pick_started, which settles the step's pending send-back
// as its output would, and decides the pick.
func (e *RunEngine) startPick(runID string, board *BoardDocument, gates map[string]GateNode, stepID string, pick stepPick, use RunLimitReached, trigger string, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	markPortsRead(ready, stepID)
	if queued[stepID] {
		queued[stepID] = false
		*queue = slices.DeleteFunc(*queue, func(nodeID string) bool { return nodeID == stepID })
	}
	data := map[string]any{
		"nodeId":     stepID,
		"gateId":     pick.gate.ID,
		"trigger":    trigger,
		"limit":      use,
		"candidates": pick.candidates,
	}
	if hasGateKind(pick.gate.Kinds, "formation") {
		data["judgeChain"] = formationIDs(judgeChainForGate(board, pick.gate.ID))
	}
	seq, err := e.store.appendRunEventSeq(runID, RunEvent{Type: RunEventPickStarted, NodeID: stepID, GateID: pick.gate.ID, Data: data})
	if err != nil {
		return err
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	return e.runPick(runID, board, gates, events[seq-1], ready, queued, queue)
}

// pickChoice is a decided pick.
type pickChoice struct {
	Attempt   int
	Reason    string
	Evidence  []GateEvidenceRef
	DecidedBy string
}

func (c pickChoice) data() map[string]any {
	evidence := c.Evidence
	if evidence == nil {
		evidence = []GateEvidenceRef{}
	}
	return map[string]any{"attempt": c.Attempt, "reason": c.Reason, "evidence": evidence, "decidedBy": c.DecidedBy}
}

func pickChoiceFromAny(value any) *pickChoice {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return &pickChoice{
		Attempt:   intFromRunEventData(fields["attempt"]),
		Reason:    stringFromAny(fields["reason"]),
		Evidence:  gateEvidenceRefsFromRunEventData(fields["evidence"]),
		DecidedBy: stringFromAny(fields["decidedBy"]),
	}
}

// runPick decides the pick a pick_started began. The only candidate is taken
// without a picker. Otherwise the gate's judge chain picks, and a human gate
// asks the operator, with the judge's pick as a recommendation when the gate
// has a judge. Replay finishes an unfinished pick through it too.
func (e *RunEngine) runPick(runID string, board *BoardDocument, gates map[string]GateNode, started RunEvent, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	gate, ok := gates[started.GateID]
	if !ok {
		return fmt.Errorf("%w: gate %q", ErrNotFound, started.GateID)
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	candidates, err := loadPickCandidates(events, pickCandidatesFromEvent(started))
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return fmt.Errorf("%w: pick %d has no candidates", ErrRunLedgerInvalid, started.Seq)
	}
	if len(candidates) == 1 {
		only := candidates[0]
		return e.resolvePick(runID, board, gates, gate, started, pickChoice{
			Attempt: only.Attempt, Reason: fmt.Sprintf("the only attempt %s judged", nodeName(board, gate.ID)), DecidedBy: PickDecidedByArchon,
		}, 0, nil, ready, queued, queue)
	}
	input := pickInput(runID, board, gate, started, candidates)
	attempts := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		attempts = append(attempts, candidate.Attempt)
	}
	var recommendation *pickChoice
	if hasGateKind(gate.Kinds, "formation") {
		chain := judgeChainForGate(board, gate.ID)
		if len(chain) == 0 {
			return fmt.Errorf("%w: gate %q has no judge chain", ErrConflict, gate.ID)
		}
		text, err := e.runJudgeChain(board, GateEvaluation{
			RunID: runID, GateID: gate.ID, Title: gate.Title, Kinds: []string{"formation"}, Criterion: gate.Criterion, Input: input,
		}, chain, &judgePickMode{anchorSeq: started.Seq, attempts: attempts})
		if err != nil {
			return err
		}
		choice, err := parseJudgePick(text, attempts)
		if err != nil {
			return e.blockInvalidPick(runID, gate.ID, err)
		}
		choice.DecidedBy = chain[len(chain)-1].ID
		if !hasGateKind(gate.Kinds, "human") {
			return e.resolvePick(runID, board, gates, gate, started, choice, 0, nil, ready, queued, queue)
		}
		recommendation = &choice
	}
	if !hasGateKind(gate.Kinds, "human") {
		return fmt.Errorf("%w: gate %q has no judge or human to pick", ErrRunLedgerInvalid, gate.ID)
	}
	choices := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		choices = append(choices, strconv.Itoa(attempt))
	}
	pick := map[string]any{"nodeId": started.NodeID, "pickSeq": started.Seq, "candidates": attempts}
	if recommendation != nil {
		pick["recommendation"] = recommendation.data()
	}
	// Only this path waits for the pick; the run goes on with every other
	// branch, as with a verdict (archon-o7p.11).
	return e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventHumanInputRequested,
		GateID: gate.ID,
		NodeID: gate.ID,
		Data: map[string]any{
			"gateId":      gate.ID,
			"nodeId":      gate.ID,
			"prompt":      gate.Criterion,
			"choices":     choices,
			"requestedBy": gate.ID,
			"inputRef":    input,
			"pick":        pick,
		},
	})
}

// pickInput is what a picker reads: every candidate side by side, with the
// verdict the gate gave it and the output it judged, as text or as the path
// of the accepted file or folder.
func pickInput(runID string, board *BoardDocument, gate GateNode, started RunEvent, candidates []pickCandidateDetail) RunInputRef {
	var b strings.Builder
	fmt.Fprintf(&b, "Pick the best attempt of %s. Its Limit card is spent, so it runs no more; the attempt picked goes on through %s as if it had passed.\n",
		nodeName(board, started.NodeID), nodeName(board, gate.ID))
	if criterion := strings.TrimSpace(gate.Criterion); criterion != "" {
		fmt.Fprintf(&b, "criterion: %s\n", criterion)
	}
	for _, candidate := range candidates {
		fmt.Fprintf(&b, "\nattempt %d\nverdict: %s\nreason: %s\n", candidate.Attempt, candidate.Verdict, candidate.Reason)
		if (candidate.Input.Kind == PortKindFile || candidate.Input.Kind == PortKindFolder) && candidate.Input.Ref != "" {
			fmt.Fprintf(&b, "output %s: %s\n", candidate.Input.Kind, candidate.Input.Ref)
		}
		if candidate.Input.Text != "" {
			fmt.Fprintf(&b, "output:\n%s\n", candidate.Input.Text)
		}
	}
	return RunInputRef{FromNodeID: started.NodeID, Ref: fmt.Sprintf("ledger://%s/pick/%d", runID, started.Seq), Text: b.String()}
}

// resolvePick ends a pick: a gate_verdict pass that carries the picked
// attempt's gate input down the pass route, as the gate's pass would have.
// requestedSeq names the operator's request it answers, if any.
func (e *RunEngine) resolvePick(runID string, board *BoardDocument, gates map[string]GateNode, gate GateNode, started RunEvent, choice pickChoice, requestedSeq int, recommendation *pickChoice, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	candidates, err := loadPickCandidates(events, pickCandidatesFromEvent(started))
	if err != nil {
		return err
	}
	index := slices.IndexFunc(candidates, func(candidate pickCandidateDetail) bool { return candidate.Attempt == choice.Attempt })
	if index < 0 {
		return fmt.Errorf("%w: pick %d names attempt %d, which is not a candidate", ErrRunLedgerInvalid, started.Seq, choice.Attempt)
	}
	picked := candidates[index]
	attempts := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		attempts = append(attempts, candidate.Attempt)
	}
	attempt, err := e.gateAttempt(runID, gate.ID)
	if err != nil {
		return err
	}
	routes := outgoingConnectionsFromPort(board.Connections, gate.ID, "pass")
	pick := map[string]any{
		"nodeId":     started.NodeID,
		"attempt":    choice.Attempt,
		"pickSeq":    started.Seq,
		"candidates": attempts,
		"decidedBy":  choice.DecidedBy,
		"trigger":    stringFromEventData(started, "trigger"),
	}
	if recommendation != nil {
		pick["recommendation"] = recommendation.data()
	}
	evidence := choice.Evidence
	if evidence == nil {
		evidence = []GateEvidenceRef{}
	}
	data := map[string]any{
		"verdict":     "pass",
		"routePort":   "pass",
		"routedEdges": connectionIDs(routes),
		"reason":      choice.Reason,
		"evidence":    evidence,
		"inputRef":    picked.Input,
		"pick":        pick,
	}
	if requestedSeq > 0 {
		data["requestedSeq"] = requestedSeq
	}
	if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventGateVerdict, Attempt: attempt, GateID: gate.ID, NodeID: gate.ID, Data: data}); err != nil {
		return err
	}
	for _, route := range routes {
		if err := e.deliverConnection(runID, board, gates, route, picked.Input, ready, queued, queue); err != nil {
			return err
		}
	}
	return nil
}

// routeHumanPick routes the operator's recorded pick of a pending pick.
func (e *RunEngine) routeHumanPick(runID string, board *BoardDocument, gates map[string]GateNode, events []RunEvent, request, recorded RunEvent, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	gate, ok := gates[request.GateID]
	if !ok {
		return fmt.Errorf("%w: gate %q", ErrNotFound, request.GateID)
	}
	pickSeq := intFromRunEventData(pickData(request)["pickSeq"])
	if pickSeq <= 0 || pickSeq >= request.Seq || events[pickSeq-1].Type != RunEventPickStarted || events[pickSeq-1].GateID != gate.ID {
		return fmt.Errorf("%w: Gate %q pick request names invalid pick %d", ErrRunLedgerInvalid, gate.ID, pickSeq)
	}
	choice := pickChoice{
		Attempt:   intFromRunEventData(pickData(recorded)["attempt"]),
		Reason:    stringFromEventData(recorded, "reason"),
		DecidedBy: stringFromEventData(recorded, "decidedBy"),
	}
	return e.resolvePick(runID, board, gates, gate, events[pickSeq-1], choice, request.Seq, pickChoiceFromAny(pickData(request)["recommendation"]), ready, queued, queue)
}

// pickSettled reports whether the pick that pick_started at seq began has
// ended in a verdict, or asked the operator, whose pick the run routes.
func pickSettled(events []RunEvent, seq int) bool {
	for _, event := range events[seq:] {
		if (event.Type == RunEventGateVerdict || event.Type == RunEventHumanInputRequested) && intFromRunEventData(pickData(event)["pickSeq"]) == seq {
			return true
		}
	}
	return false
}

// blockInvalidPick stops the run at a judge's malformed pick, as a malformed
// verdict stops it; a plain resume asks the final judge again.
func (e *RunEngine) blockInvalidPick(runID, gateID string, err error) error {
	attempt, readErr := e.gateAttempt(runID, gateID)
	if readErr != nil {
		return readErr
	}
	if appendErr := e.store.AppendRunEvent(runID, RunEvent{
		Type: RunEventJudgeAttemptFailed, GateID: gateID, NodeID: gateID, Attempt: attempt,
		Data: map[string]any{"code": RunBlockInvalidJudgePick, "reason": err.Error()},
	}); appendErr != nil {
		return appendErr
	}
	if appendErr := e.store.AppendRunEvent(runID, RunEvent{
		Type: RunEventBlocked, GateID: gateID, NodeID: gateID,
		Data: map[string]any{"reason": "invalid judge pick: " + err.Error(), "blockedGateId": gateID, "resumeAllowed": true, "code": RunBlockInvalidJudgePick},
	}); appendErr != nil {
		return appendErr
	}
	return errRunStopped
}

// PendingPick is what a gate's pending pick asks the operator: the step and
// the attempts the gate judged, with the judge's pick as a recommendation when
// the gate has a judge (archon-gihe).
type PendingPick struct {
	NodeID         string              `json:"nodeId"`
	Candidates     []int               `json:"candidates"`
	Recommendation *PickRecommendation `json:"recommendation,omitempty"`
}

// PickRecommendation is the judge's pick, which the operator may take.
type PickRecommendation struct {
	Attempt   int    `json:"attempt"`
	Reason    string `json:"reason"`
	DecidedBy string `json:"decidedBy"`
}

// PendingPickOf reads the pick a human request asks for, or nil when the
// request asks for a verdict.
func PendingPickOf(request RunEvent) *PendingPick {
	if request.Type != RunEventHumanInputRequested || !isPickEvent(request) {
		return nil
	}
	data := pickData(request)
	pick := &PendingPick{NodeID: stringFromAny(data["nodeId"]), Candidates: intsFromAny(data["candidates"])}
	if pick.Candidates == nil {
		pick.Candidates = []int{}
	}
	if choice := pickChoiceFromAny(data["recommendation"]); choice != nil {
		pick.Recommendation = &PickRecommendation{Attempt: choice.Attempt, Reason: choice.Reason, DecidedBy: choice.DecidedBy}
	}
	return pick
}

// HumanGatePickRequest is the operator's pick on a gate's pending pick.
type HumanGatePickRequest struct {
	GateID string
	// RequestedSeq, when set, names the exact pending request decided.
	RequestedSeq int
	Attempt      int
	Reason       string
	Actor        string
	// RelayedBy is the slot ID of the seat that typed a pick the actor made.
	RelayedBy string
}

// RecordHumanGatePick records the operator's pick on a gate's pending pick.
// Like a verdict it routes nothing: the run's worker routes it, so a pick is
// accepted while other seats work, and on a blocked run it waits for the
// resume. The request is checked under the ledger's append lock.
func (e *RunEngine) RecordHumanGatePick(runID string, req HumanGatePickRequest) (*RunStatusProjection, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("%w: run engine store required", ErrNotFound)
	}
	if err := ValidateRelayedBy(req.RelayedBy); err != nil {
		return nil, err
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrRunLedgerInvalid
	}
	request, ok := latestHumanRequest(events, req.GateID)
	if !ok {
		return nil, fmt.Errorf("%w: human gate request %q", ErrNotFound, req.GateID)
	}
	if req.RequestedSeq != 0 && req.RequestedSeq != request.Seq {
		return nil, ErrHumanRequestNotPending
	}
	if !isPickEvent(request) {
		return nil, ErrHumanRequestNotAPick
	}
	candidates := intsFromAny(pickData(request)["candidates"])
	if !slices.Contains(candidates, req.Attempt) {
		return nil, fmt.Errorf("%w: attempt %d; pick one of %s", ErrPickNotACandidate, req.Attempt, AttemptListWords(candidates))
	}
	board, err := e.readRunBoard(runID)
	if err != nil {
		return nil, err
	}
	if err := validateRunRoot(board, events[0]); err != nil {
		return nil, err
	}
	actor := defaultRunActor(req.Actor)
	data := map[string]any{
		"gateId":       req.GateID,
		"nodeId":       req.GateID,
		"verdict":      "pass",
		"reason":       req.Reason,
		"requestedSeq": request.Seq,
		"decidedBy":    actor,
		"pick":         map[string]any{"attempt": req.Attempt},
	}
	if req.RelayedBy != "" {
		data["relayedBy"] = req.RelayedBy
	}
	if err := e.store.appendRunEventIf(runID, RunEvent{
		Type:   RunEventHumanVerdictRecorded,
		Actor:  actor,
		GateID: req.GateID,
		NodeID: req.GateID,
		Data:   data,
	}, func(current []RunEvent) error {
		if pending, ok := latestHumanRequest(current, req.GateID); !ok || pending.Seq != request.Seq {
			return ErrHumanRequestNotPending
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return e.store.ProjectRun(runID)
}

// AttemptListWords says attempt numbers plainly: "1, 2 or 3".
func AttemptListWords(attempts []int) string {
	words := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		words = append(words, strconv.Itoa(attempt))
	}
	switch len(words) {
	case 0:
		return "none"
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}

// judgePickMode runs a judge chain to pick rather than to judge: anchored at
// its pick_started, with the pick contract and the candidate attempts.
type judgePickMode struct {
	anchorSeq int
	attempts  []int
}

const judgePickKeys = "requires exactly attempt, reason and evidence"

// pickContract is what a picking judge must return.
func pickContract(attempts []int) string {
	example := 1
	if len(attempts) > 0 {
		example = attempts[0]
	}
	return fmt.Sprintf("judge pick contract:\nThis is a pick, not a verdict: compare the attempts below side by side and pick the best one. Before the completion sentinel, emit exactly one fenced archon-pick JSON block, separate from archon-outputs, with exactly these three keys:\n```archon-pick\n{\"attempt\":%d,\"reason\":\"Why this attempt is the best\",\"evidence\":[\"what decided it\"]}\n```\nattempt must be one of attempts %s, reason a string, and evidence an array of strings (use [] when there are no references). Do not omit evidence or add keys. The final judge in the chain decides the pick.\n",
		example, AttemptListWords(attempts))
}

// parseJudgePick accepts one explicitly fenced pick of a candidate attempt,
// never prose, as parseJudgeVerdict accepts a verdict.
func parseJudgePick(text string, attempts []int) (pickChoice, error) {
	fields, err := strictFencedObject(text, "archon-pick", []string{"attempt", "reason", "evidence"}, "judge pick", judgePickKeys)
	if err != nil {
		return pickChoice{}, err
	}
	var attempt *int
	var reason *string
	var evidence []*string
	if json.Unmarshal(fields["attempt"], &attempt) != nil || attempt == nil || !slices.Contains(attempts, *attempt) {
		return pickChoice{}, fmt.Errorf("judge pick attempt must be one of attempts %s", AttemptListWords(attempts))
	}
	if json.Unmarshal(fields["reason"], &reason) != nil || reason == nil {
		return pickChoice{}, fmt.Errorf("judge pick reason must be a string")
	}
	if json.Unmarshal(fields["evidence"], &evidence) != nil || evidence == nil {
		return pickChoice{}, fmt.Errorf("judge pick evidence must be an array of strings")
	}
	choice := pickChoice{Attempt: *attempt, Reason: *reason, Evidence: []GateEvidenceRef{}}
	for _, item := range evidence {
		if item == nil {
			return pickChoice{}, fmt.Errorf("judge pick evidence must contain only strings")
		}
		choice.Evidence = append(choice.Evidence, GateEvidenceRef{Kind: "formation", Text: *item})
	}
	return choice, nil
}

// strictFencedObject reads the one fenced lang block in text as a JSON object
// holding exactly keys, each once. what names the result in errors.
func strictFencedObject(text, lang string, keys []string, what, keysRule string) (map[string]json.RawMessage, error) {
	var body strings.Builder
	blocks, inside, closed := 0, false, false
	fence := "```" + lang
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, fence) {
			blocks++
			if line != fence || inside || blocks != 1 {
				return nil, fmt.Errorf("expected exactly one %s block", lang)
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
		return nil, fmt.Errorf("missing or unterminated %s block", lang)
	}
	if err := jsonstrict.ValidateUnicode([]byte(body.String())); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(strings.NewReader(body.String()))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, fmt.Errorf("%s must be a JSON object", what)
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid %s key: %w", what, err)
		}
		name, ok := key.(string)
		if !ok || !slices.Contains(keys, name) {
			return nil, fmt.Errorf("%s has extra key %q; it %s", what, key, keysRule)
		}
		if fields[name] != nil {
			return nil, fmt.Errorf("%s repeats key %q; it %s", what, name, keysRule)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("invalid %s value: %w", what, err)
		}
		fields[name] = value
	}
	if end, err := decoder.Token(); err != nil || end != json.Delim('}') {
		return nil, fmt.Errorf("%s must be a JSON object", what)
	}
	var missing []string
	for _, name := range keys {
		if fields[name] == nil {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s is missing %s; it %s", what, strings.Join(missing, " and "), keysRule)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing data in %s", what)
	}
	return fields, nil
}
