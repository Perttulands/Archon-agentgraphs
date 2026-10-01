package formations

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

var (
	errRunStopped               = errors.New("archon run stopped")
	ErrRunExecutorUnavailable   = errors.New("archon run executor unavailable")
	ErrGateEvaluatorUnavailable = errors.New("archon gate evaluator unavailable")
)

type FormationExecutor interface {
	ExecuteFormation(FormationExecution) (FormationExecutionResult, error)
}

type ContextFormationExecutor interface {
	ExecuteFormationContext(context.Context, FormationExecution) (FormationExecutionResult, error)
}

type FormationReattachExecutor interface {
	ReattachFormationDispatch(FormationReattachRequest) (FormationExecutionResult, error)
}

type FormationReattachRequest struct {
	RunID      string
	DispatchID string
	NodeID     string
	SlotID     string
	Formation  FormationNode
}

type unavailableFormationExecutor struct {
	boundary string
}

type GateEvaluator interface {
	EvaluateGate(GateEvaluation) (GateEvaluationResult, error)
}

// SetExecutionContext supplies the owner's cancellation context for each seat.
// Configure it before executing runs; the executor retains its recovery methods.
func (e *RunEngine) SetExecutionContext(provider func(string) context.Context) {
	e.executionContext = provider
}

type RunEngine struct {
	executionContext func(string) context.Context
	store            *Store
	personas         *PersonaStore
	executor         FormationExecutor
	gateEvaluator    GateEvaluator
	needsYouNotifier NeedsYouNotifier
	needsYouBoardURL string
}

type FormationRunRequest struct {
	ExpectedBoardRev  int
	ExpectedBoardETag string
	Actor             string
	Personas          *PersonaStore
}

type FormationExecution struct {
	// Deadline uses the ledger clock and covers the entire formation attempt.
	Deadline      time.Time
	Cwd           string
	ContextPaths  []string
	MissionGoal   string
	MissionBeadID string
	RunID         string
	NodeID        string
	Title         string
	Formation     FormationNode
	Brief         FormationBrief
	Inputs        []RunInputRef
	Attempt       int
	// KeepSeatsOnCall keeps the formation's seats running when it finishes,
	// because its work reaches a human gate on a session-channel run.
	KeepSeatsOnCall bool
	// PeerMessages is a peer step's Limit card rounds, its journal messages:
	// used in earlier attempts of Max. Nil when no card caps the step.
	PeerMessages *RunLimitReached
	// Warnings are the time cards' warnings for this attempt's seats.
	Warnings []LimitWarning
}

// LimitReachedError is how an executor stops a step at its Limit card; the
// engine blocks the run there, resumable with a grant.
type LimitReachedError struct {
	Use RunLimitReached
}

func (e *LimitReachedError) Error() string {
	return fmt.Sprintf("limit reached: %s used %d of %d %s", e.Use.NodeID, e.Use.Used, e.Use.Max, e.Use.Kind)
}

type FormationExecutionResult struct {
	Status    string
	ReportRef string
	Text      string
	Outputs   map[string]FormationOutputPayload
}

type FormationOutputPayload struct {
	Ref         string `json:"ref,omitempty"`
	Text        string `json:"text,omitempty"`
	ReportRef   string `json:"reportRef,omitempty"`
	ArtifactRef string `json:"artifactRef,omitempty"`
}

type GateEvaluation struct {
	RunID        string
	GateID       string
	Title        string
	Kinds        []string
	Criterion    string
	Check        string
	CheckVersion string
	CheckValue   string
	Input        RunInputRef
	Binding      *RunGateBinding
}

type GateEvaluationResult struct {
	Verdict         string
	Reason          string
	Evidence        []GateEvidenceRef
	PerKind         map[string]string
	KindResultSeqs  map[string]int
	CodeVerdict     string
	CodeReason      string
	ResultEncoding  string
	ResultSHA256    string
	CanonicalResult string
	GateBindingID   string
}

type GateEvidenceRef struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

type HumanGateVerdictRequest struct {
	GateID string
	// RequestedSeq, when set, names the exact pending request decided.
	RequestedSeq int
	Verdict      string
	Reason       string
	Actor        string
	// RelayedBy is the slot ID of the seat that typed a decision the actor made.
	RelayedBy string
}

var relayedByPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// ValidateRelayedBy accepts no relay, or a slot ID: a letter or digit, then up
// to 63 letters, digits, underscores or hyphens.
func ValidateRelayedBy(slotID string) error {
	if slotID == "" || relayedByPattern.MatchString(slotID) {
		return nil
	}
	return fmt.Errorf("%w: relayedBy %q must be a slot ID: a letter or digit, then up to 63 letters, digits, underscores or hyphens", ErrInvalidRelayedBy, slotID)
}

type RunInputRef struct {
	Feedback    *GateFeedback `json:"feedback,omitempty"`
	Response    *GateResponse `json:"response,omitempty"`
	EdgeID      string        `json:"edgeId,omitempty"`
	FromNodeID  string        `json:"fromNodeId,omitempty"`
	FromPortID  string        `json:"fromPortId,omitempty"`
	ToPortID    string        `json:"toPortId,omitempty"`
	OutputSeq   int           `json:"outputSeq,omitempty"`
	Ref         string        `json:"ref,omitempty"`
	Text        string        `json:"text,omitempty"`
	ReportRef   string        `json:"reportRef,omitempty"`
	ArtifactRef string        `json:"artifactRef,omitempty"`
}

func NewRunEngine(store *Store, personas *PersonaStore, executor FormationExecutor) *RunEngine {
	return &RunEngine{
		store:    store,
		personas: personas,
		executor: executor,
	}
}

func NewUnavailableFormationExecutor(boundary string) FormationExecutor {
	return unavailableFormationExecutor{boundary: strings.TrimSpace(boundary)}
}

func (e unavailableFormationExecutor) ExecuteFormation(FormationExecution) (FormationExecutionResult, error) {
	if e.boundary == "" {
		return FormationExecutionResult{}, ErrRunExecutorUnavailable
	}
	return FormationExecutionResult{}, fmt.Errorf("%w: %s executor is not configured", ErrRunExecutorUnavailable, e.boundary)
}

func (e *RunEngine) SetGateEvaluator(evaluator GateEvaluator) {
	e.gateEvaluator = evaluator
}

// SetNeedsYouNotifier wires the outbound needs-you channel. A nil notifier
// leaves the feature off (reconcileNeedsYou becomes a no-op). boardBaseURL is an
// optional origin used to build a pointer back to the board.
func (e *RunEngine) SetNeedsYouNotifier(notifier NeedsYouNotifier, boardBaseURL string) {
	e.needsYouNotifier = notifier
	e.needsYouBoardURL = strings.TrimSpace(boardBaseURL)
}

// projectAndNotify pushes any newly-opened needs-you asks and then returns the
// run projection. Notification is best-effort and never affects the run result.
func (e *RunEngine) projectAndNotify(runID string) (*RunStatusProjection, error) {
	e.reconcileNeedsYou(runID)
	return e.store.ProjectRun(runID)
}

// reconcileNeedsYou delivers one notification per open ask that has not been
// delivered yet, sourced from the durable run ledger. It is idempotent: dedup is
// persisted, resolved asks are never projected as open, and send failures leave
// the ask undelivered so the next genuine state transition retries it.
func (e *RunEngine) reconcileNeedsYou(runID string) {
	if e == nil || e.needsYouNotifier == nil || e.store == nil {
		return
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil || len(events) == 0 {
		return
	}
	asks := projectOpenNeedsYouAsks(events)
	if len(asks) == 0 {
		return
	}
	notified, err := e.store.NeedsYouNotifiedSeqs(runID)
	if err != nil {
		return
	}
	boardSlug := stringFromEventData(events[0], "missionSlug")
	for _, ask := range asks {
		if notified[ask.Seq] {
			continue
		}
		notification := buildNeedsYouNotification(ask, boardSlug, e.needsYouBoardURL)
		if err := e.needsYouNotifier.NotifyNeedsYou(context.Background(), notification); err != nil {
			continue // best-effort; retry on the next state transition
		}
		_ = e.store.MarkNeedsYouNotified(runID, ask.Seq)
	}
}

func (e *RunEngine) RunMission(slug string, req RunStartRequest) (*RunStatusProjection, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("%w: run engine store required", ErrNotFound)
	}
	board, err := e.store.ReadBoard(slug)
	if err != nil {
		return nil, err
	}
	mission, ok := findMission(board, req.MissionID)
	if !ok {
		return nil, fmt.Errorf("%w: Input card %q", ErrNotFound, req.MissionID)
	}
	if len(outgoingConnections(board.Connections, mission.ID)) == 0 {
		return nil, fmt.Errorf("%w: wire the Input card to a step", ErrConflict)
	}
	if req.Personas == nil {
		req.Personas = e.personas
	}
	started, err := e.store.StartRun(slug, req)
	if err != nil {
		return nil, err
	}
	return e.ExecuteStartedMission(started.RunID)
}

// ExecuteStartedMission continues a newly admitted mission in its coordinator.
// The caller owns exclusive execution; a prior execution is never replayed here.
func (e *RunEngine) ExecuteStartedMission(runID string) (*RunStatusProjection, error) {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return nil, err
	}
	if len(events) != 1 || events[0].Type != RunEventStarted {
		return nil, ErrConflict
	}
	runBoard, err := e.readRunBoard(runID)
	if err != nil {
		return nil, err
	}
	mission, ok := findMission(runBoard, events[0].MissionID)
	if !ok {
		return nil, fmt.Errorf("%w: mission %q", ErrNotFound, events[0].MissionID)
	}
	if err := e.executeSnapshot(runID, runBoard, mission); err != nil {
		return nil, err
	}
	return e.projectAndNotify(runID)
}

func (e *RunEngine) RunFormation(slug, formationID string, req FormationRunRequest) (*RunStatusProjection, error) {
	_, execute, err := e.PrepareFormationRun(slug, formationID, req)
	if err != nil {
		return nil, err
	}
	return execute()
}

// PrepareFormationRun durably admits an isolated formation without dispatching.
// The owner must invoke the returned continuation once under its worker guard.
func (e *RunEngine) PrepareFormationRun(slug, formationID string, req FormationRunRequest) (*RunStartResult, func() (*RunStatusProjection, error), error) {
	if e == nil || e.store == nil {
		return nil, nil, fmt.Errorf("%w: run engine store required", ErrNotFound)
	}
	board, err := e.store.ReadBoard(slug)
	if err != nil {
		return nil, nil, err
	}
	if (req.ExpectedBoardRev != 0 && board.Rev != req.ExpectedBoardRev) || (req.ExpectedBoardETag != "" && board.ETag != req.ExpectedBoardETag) {
		return nil, nil, ErrConflict
	}

	formation, ok := findFormation(board.Formations, formationID)
	if !ok {
		return nil, nil, fmt.Errorf("%w: formation %q", ErrNotFound, formationID)
	}
	if err := preflightIsolatedFormationDefinition(board, formation.ID); err != nil {
		return nil, nil, err
	}
	personas := req.Personas
	if personas == nil {
		personas = e.personas
	}
	started, mission, seedInput, err := e.startFormationRun(slug, board, formation, req.Actor, personas)
	if err != nil {
		return nil, nil, err
	}
	return started, func() (*RunStatusProjection, error) {
		if err := e.startFormationExecution(started.RunID, board, formation, RunEvent{
			Type:    RunEventNodeStarted,
			NodeID:  formation.ID,
			Attempt: 1,
			Data: map[string]any{
				"nodeKind":  "formation",
				"inputRefs": []RunInputRef{seedInput},
				"reason":    "single-formation",
				"brief":     formationBriefEventData(formationBriefValue(formation)),
			},
		}); err != nil {
			if errors.Is(err, errRunStopped) {
				return e.projectAndNotify(started.RunID)
			}
			return nil, err
		}
		result, err := e.executeFormation(FormationExecution{
			RunID:     started.RunID,
			NodeID:    formation.ID,
			Title:     formation.Title,
			Formation: formation,
			Brief:     formationBriefValue(formation),
			Inputs:    []RunInputRef{seedInput},
			Attempt:   1,
		})
		if err != nil {
			if blockErr := e.appendExecutionFailureAndBlock(started.RunID, formation.ID, err); blockErr != nil {
				return nil, blockErr
			}
			return e.projectAndNotify(started.RunID)
		}
		if result.Status == "" {
			result.Status = "done"
		}
		if err := e.ensureFormationOutputPayloads(started.RunID, formation, result); err != nil {
			if errors.Is(err, errRunStopped) {
				return e.projectAndNotify(started.RunID)
			}
			return nil, err
		}
		if err := e.store.AppendRunEvent(started.RunID, RunEvent{
			Type:   RunEventNodeOutput,
			NodeID: formation.ID,
			Data:   formationOutputEventData(result),
		}); err != nil {
			return nil, err
		}
		if err := e.store.AppendRunEvent(started.RunID, RunEvent{
			Type: RunEventSucceeded,
			Data: map[string]any{
				"summaryRef":   "",
				"outputRefs":   []string{},
				"artifactRefs": []string{},
				"final":        true,
				"mode":         "formation",
				"formationId":  formation.ID,
				"inputCardId":  mission.ID,
			},
		}); err != nil {
			return nil, err
		}
		return e.projectAndNotify(started.RunID)
	}, nil
}

func (e *RunEngine) ResumeRun(runID string, req RunResumeRequest) (*RunStatusProjection, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("%w: run engine store required", ErrNotFound)
	}
	beforeEvents, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return nil, err
	}
	beforeBoard, err := e.readRunBoard(runID)
	if err != nil {
		return nil, err
	}
	if err := e.validateDurableCodeGateState(runID, beforeBoard, beforeEvents); err != nil {
		return nil, err
	}
	var recovered *FormationExecutionResult
	var recoveredRef openDispatchRef
	if req.Mode == "completed-native-turn" {
		ref, result, err := e.prepareCompletedRecovery(runID, beforeBoard, beforeEvents)
		if err != nil {
			return nil, err
		}
		recoveredRef, recovered = ref, &result
		req.CompletedDispatchID = ref.DispatchID
	} else if req.CompletedDispatchID != "" {
		return nil, errors.New("completed dispatch identity requires explicit completed-native-turn mode")
	}
	_, board, err := e.store.resumeRunWithSnapshot(runID, req)
	if err != nil {
		return nil, err
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrRunLedgerInvalid
	}
	lifecycle := lifecycleLedger(events)
	if len(lifecycle) == 0 {
		return nil, ErrRunLedgerInvalid
	}
	resumeEvent := lifecycle[len(lifecycle)-1]
	if recovered != nil {
		if err := NewSlotDispatcher(e.store, nil).CompleteFromCapture(runID, recoveredRef.DispatchID, recovered.Text); err != nil {
			return nil, err
		}
		if err := e.store.AppendRunEvent(runID, RunEvent{Type: "seat_result_recovered", NodeID: recoveredRef.NodeID, SlotID: recoveredRef.SlotID, Data: map[string]any{"dispatchId": recoveredRef.DispatchID}}); err != nil {
			return nil, err
		}
		if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventNodeOutput, NodeID: recoveredRef.NodeID, Data: formationOutputEventData(*recovered)}); err != nil {
			return nil, err
		}
		events, err = e.store.ReadRunEvents(runID)
		if err != nil {
			return nil, err
		}
	} else if openDispatches := openDispatchRefsFromEvent(resumeEvent); len(openDispatches) > 0 {
		openDispatches = enrichOpenDispatchRefs(events, openDispatches)
		if req.Mode == "redispatch" {
			// The operator gave up on the open dispatches; record each as
			// abandoned so the node runs again as a fresh bounded attempt.
			for _, ref := range openDispatches {
				if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventSlotResult, NodeID: ref.NodeID, SlotID: ref.SlotID, Data: map[string]any{
					"dispatchId": ref.DispatchID, "nodeId": ref.NodeID, "slotId": ref.SlotID, "status": "abandoned", "reason": redactLedgerText(req.Reason),
				}}); err != nil {
					return nil, err
				}
			}
		} else {
			handled, err := e.reattachOpenDispatches(runID, board, openDispatches)
			if err != nil {
				// A failed reattach keeps the run resumable; the reason is durable.
				if blockErr := e.appendOpenDispatchReattachFailure(runID, openDispatches, err.Error()); blockErr != nil {
					return nil, blockErr
				}
				return e.projectAndNotify(runID)
			}
			if !handled {
				if err := e.appendOpenDispatchReattachFailure(runID, openDispatches, "could not reattach open dispatch without live capture"); err != nil {
					return nil, err
				}
				return e.projectAndNotify(runID)
			}
		}
		events, err = e.store.ReadRunEvents(runID)
		if err != nil {
			return nil, err
		}
	}
	// Seats found gone while the run was blocked are recorded now it resumed.
	if err := e.recordGoneKeptSeats(runID); err != nil {
		return nil, err
	}
	if events, err = e.store.ReadRunEvents(runID); err != nil {
		return nil, err
	}
	started := events[0]
	mission, ok := findMission(board, started.MissionID)
	if !ok {
		return nil, fmt.Errorf("%w: mission %q", ErrNotFound, started.MissionID)
	}
	if err := e.resumeSnapshot(runID, board, mission, events, "resume"); err != nil {
		return nil, err
	}
	return e.projectAndNotify(runID)
}

// ContinueRun goes on with a run that is neither blocked nor final: it routes
// the human verdicts recorded since the run last worked and runs whatever they,
// or anything else, still owe (archon-o7p.11). The coordinator calls it after a
// verdict on a run with no worker. A blocked run continues on resume instead.
func (e *RunEngine) ContinueRun(runID string) (*RunStatusProjection, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("%w: run engine store required", ErrNotFound)
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return nil, err
	}
	lifecycle := lifecycleLedger(events)
	if len(lifecycle) == 0 {
		return nil, ErrRunLedgerInvalid
	}
	if last := lifecycle[len(lifecycle)-1]; isFinalRunEvent(last.Type) || last.Type == RunEventBlocked {
		return e.store.ProjectRun(runID)
	}
	board, err := e.readRunBoard(runID)
	if err != nil {
		return nil, err
	}
	mission, ok := findMission(board, events[0].MissionID)
	if !ok {
		return nil, fmt.Errorf("%w: mission %q", ErrNotFound, events[0].MissionID)
	}
	if err := e.resumeSnapshot(runID, board, mission, events, "continue"); err != nil {
		return nil, err
	}
	return e.projectAndNotify(runID)
}

func (e *RunEngine) validateDurableCodeGateState(runID string, board *BoardDocument, events []RunEvent) error {
	gates := map[string]GateNode{}
	for _, gate := range board.Gates {
		gates[gate.ID] = gate
	}
	for _, event := range events {
		gateID := event.GateID
		if gateID == "" {
			gateID = event.NodeID
		}
		gate, ok := gates[gateID]
		if !ok {
			continue
		}
		switch event.Type {
		case RunEventGateKindResult:
			if stringFromAny(event.Data["kind"]) != "code" {
				continue
			}
			input := runInputRefFromAny(event.Data["inputRef"])
			if _, err := e.gateKindResultFromEvent(runID, gate, input, event); err != nil {
				return err
			}
		case RunEventHumanInputRequested:
			if err := e.validateHumanRequestKindResults(runID, gate, event, events); err != nil {
				return err
			}
		case RunEventGateVerdict:
			if err := e.validateGateVerdictKindResults(runID, gate, event, events); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *RunEngine) validateGateVerdictKindResults(runID string, gate GateNode, verdictEvent RunEvent, events []RunEvent) error {
	rawSeqs, present := verdictEvent.Data["kindResultSeqs"]
	if !present {
		return nil
	}
	seqs := intMapFromRunEventData(rawSeqs)
	perKind := stringMapFromRunEventData(verdictEvent.Data["perKind"])
	input := runInputRefFromAny(verdictEvent.Data["inputRef"])
	evidenceByKind := map[string][]GateEvidenceRef{}
	for kind, seq := range seqs {
		if kind != "code" && kind != "formation" {
			return fmt.Errorf("%w: Gate %q aggregate result names unknown kind %q", ErrRunLedgerInvalid, gate.ID, kind)
		}
		if seq <= 0 || seq >= verdictEvent.Seq || seq > len(events) {
			return fmt.Errorf("%w: Gate %q aggregate %s result sequence is invalid", ErrRunLedgerInvalid, gate.ID, kind)
		}
		event := events[seq-1]
		eventGateID := event.GateID
		if eventGateID == "" {
			eventGateID = event.NodeID
		}
		if event.Seq != seq ||
			event.Type != RunEventGateKindResult ||
			eventGateID != gate.ID ||
			stringFromAny(event.Data["kind"]) != kind ||
			!reflect.DeepEqual(runInputRefFromAny(event.Data["inputRef"]), input) {
			return fmt.Errorf("%w: Gate %q aggregate %s result identity mismatch", ErrRunLedgerInvalid, gate.ID, kind)
		}
		result, err := e.gateKindResultFromEvent(runID, gate, input, event)
		if err != nil {
			return err
		}
		evidenceByKind[kind] = result.Evidence
		if perKind[kind] != result.Verdict {
			return fmt.Errorf("%w: Gate %q aggregate %s verdict mismatch", ErrRunLedgerInvalid, gate.ID, kind)
		}
		if kind == "code" &&
			(stringFromAny(verdictEvent.Data["codeVerdict"]) != result.Verdict ||
				stringFromAny(verdictEvent.Data["codeReason"]) != result.Reason ||
				stringFromAny(verdictEvent.Data["resultEncoding"]) != result.ResultEncoding ||
				stringFromAny(verdictEvent.Data["resultSha256"]) != result.ResultSHA256 ||
				stringFromAny(verdictEvent.Data["gateBindingId"]) != result.GateBindingID) {
			return fmt.Errorf("%w: Gate %q aggregate code result mismatch", ErrRunLedgerInvalid, gate.ID)
		}
	}
	for _, kind := range []string{"code", "formation"} {
		state := perKind[kind]
		if hasGateKind(gate.Kinds, kind) && state != "" && state != "not_run" && seqs[kind] == 0 {
			return fmt.Errorf("%w: Gate %q aggregate %s result sequence is missing", ErrRunLedgerInvalid, gate.ID, kind)
		}
	}
	wantEvidence := append(evidenceByKind["code"], evidenceByKind["formation"]...)
	if !gateEvidenceRefsEqual(gateEvidenceRefsFromRunEventData(verdictEvent.Data["evidence"]), wantEvidence) {
		return fmt.Errorf("%w: Gate %q aggregate evidence mismatch", ErrRunLedgerInvalid, gate.ID)
	}
	return nil
}

// RecordHumanGateVerdict records the operator's verdict on a gate's pending
// request. It routes nothing: the run's worker routes recorded verdicts
// (routeRecordedVerdicts), so a verdict is accepted while other seats work,
// and on a blocked run it waits for the resume (archon-o7p.11). The request
// is checked under the ledger's append lock, so two verdicts on one request
// cannot both land.
func (e *RunEngine) RecordHumanGateVerdict(runID string, req HumanGateVerdictRequest) (*RunStatusProjection, error) {
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
	requestEvent, ok := latestHumanRequest(events, req.GateID)
	if !ok {
		return nil, fmt.Errorf("%w: human gate request %q", ErrNotFound, req.GateID)
	}
	if req.RequestedSeq != 0 && req.RequestedSeq != requestEvent.Seq {
		return nil, ErrHumanRequestNotPending
	}
	board, err := e.readRunBoard(runID)
	if err != nil {
		return nil, err
	}
	gate, ok := findGate(board.Gates, req.GateID)
	if !ok {
		return nil, fmt.Errorf("%w: gate %q", ErrNotFound, req.GateID)
	}
	if err := validateRunRoot(board, events[0]); err != nil {
		return nil, err
	}
	if err := e.validateHumanRequestKindResults(runID, gate, requestEvent, events); err != nil {
		return nil, err
	}
	actor := defaultRunActor(req.Actor)
	data := map[string]any{
		"gateId":       req.GateID,
		"nodeId":       req.GateID,
		"verdict":      normalizeGateVerdict(req.Verdict),
		"reason":       req.Reason,
		"requestedSeq": requestEvent.Seq,
		"decidedBy":    actor,
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
		if pending, ok := latestHumanRequest(current, req.GateID); !ok || pending.Seq != requestEvent.Seq {
			return ErrHumanRequestNotPending
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return e.store.ProjectRun(runID)
}

func (e *RunEngine) validateHumanRequestKindResults(runID string, gate GateNode, request RunEvent, events []RunEvent) error {
	seqs := intMapFromRunEventData(request.Data["kindResultSeqs"])
	perKind := stringMapFromRunEventData(request.Data["codePerKind"])
	input := runInputRefFromAny(request.Data["inputRef"])
	requiredKinds := make([]string, 0, 2)
	for _, kind := range []string{"code", "formation"} {
		if hasGateKind(gate.Kinds, kind) {
			requiredKinds = append(requiredKinds, kind)
		}
	}
	if len(seqs) != len(requiredKinds) {
		return fmt.Errorf("%w: Gate %q human request kind result set mismatch", ErrRunLedgerInvalid, gate.ID)
	}
	var wantEvidence []GateEvidenceRef
	for _, kind := range requiredKinds {
		seq := seqs[kind]
		if seq <= 0 || seq >= request.Seq || seq > len(events) {
			return fmt.Errorf("%w: Gate %q human request %s result sequence is invalid", ErrRunLedgerInvalid, gate.ID, kind)
		}
		event := events[seq-1]
		eventGateID := event.GateID
		if eventGateID == "" {
			eventGateID = event.NodeID
		}
		if event.Seq != seq ||
			event.Type != RunEventGateKindResult ||
			eventGateID != gate.ID ||
			stringFromAny(event.Data["kind"]) != kind ||
			!reflect.DeepEqual(runInputRefFromAny(event.Data["inputRef"]), input) {
			return fmt.Errorf("%w: Gate %q human request %s result identity mismatch", ErrRunLedgerInvalid, gate.ID, kind)
		}
		result, err := e.gateKindResultFromEvent(runID, gate, input, event)
		if err != nil {
			return err
		}
		wantEvidence = append(wantEvidence, result.Evidence...)
		if perKind[kind] != result.Verdict {
			return fmt.Errorf("%w: Gate %q human request %s verdict mismatch", ErrRunLedgerInvalid, gate.ID, kind)
		}
		if kind == "code" &&
			(stringFromAny(request.Data["codeVerdict"]) != result.Verdict ||
				stringFromAny(request.Data["codeReason"]) != result.Reason ||
				stringFromAny(request.Data["resultEncoding"]) != result.ResultEncoding ||
				stringFromAny(request.Data["resultSha256"]) != result.ResultSHA256 ||
				stringFromAny(request.Data["gateBindingId"]) != result.GateBindingID) {
			return fmt.Errorf("%w: Gate %q human request code result mismatch", ErrRunLedgerInvalid, gate.ID)
		}
	}
	if !gateEvidenceRefsEqual(gateEvidenceRefsFromRunEventData(request.Data["evidence"]), wantEvidence) {
		return fmt.Errorf("%w: Gate %q human request evidence mismatch", ErrRunLedgerInvalid, gate.ID)
	}
	return nil
}

func gateEvidenceRefsEqual(left, right []GateEvidenceRef) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type openDispatchRef struct {
	DispatchID string `json:"dispatchId"`
	NodeID     string `json:"nodeId"`
	SlotID     string `json:"slotId"`
}

func openDispatchRefsFromEvent(event RunEvent) []openDispatchRef {
	if event.Data == nil {
		return nil
	}
	raw, ok := event.Data["openDispatches"].([]any)
	if !ok {
		return nil
	}
	refs := make([]openDispatchRef, 0, len(raw))
	for _, item := range raw {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ref := openDispatchRef{
			DispatchID: stringFromAny(fields["dispatchId"]),
			NodeID:     stringFromAny(fields["nodeId"]),
			SlotID:     stringFromAny(fields["slotId"]),
		}
		if ref.DispatchID != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func enrichOpenDispatchRefs(events []RunEvent, refs []openDispatchRef) []openDispatchRef {
	if len(refs) == 0 {
		return refs
	}
	dispatches := map[string]RunEvent{}
	for _, event := range events {
		if event.Type != RunEventSlotDispatch || event.Data == nil {
			continue
		}
		dispatchID := stringFromEventData(event, "dispatchId")
		if dispatchID != "" {
			dispatches[dispatchID] = event
		}
	}
	enriched := append([]openDispatchRef(nil), refs...)
	for i, ref := range enriched {
		dispatch := dispatches[ref.DispatchID]
		if ref.NodeID == "" {
			enriched[i].NodeID = dispatch.NodeID
			if enriched[i].NodeID == "" {
				enriched[i].NodeID = stringFromEventData(dispatch, "nodeId")
			}
		}
		if ref.SlotID == "" {
			enriched[i].SlotID = dispatch.SlotID
			if enriched[i].SlotID == "" {
				enriched[i].SlotID = stringFromEventData(dispatch, "slotId")
			}
		}
	}
	return enriched
}

func (e *RunEngine) reattachOpenDispatches(runID string, board *BoardDocument, refs []openDispatchRef) (bool, error) {
	reattacher, ok := e.executor.(FormationReattachExecutor)
	if !ok || reattacher == nil || len(refs) == 0 {
		return false, nil
	}
	if len(refs) != 1 {
		return false, nil
	}
	ref := refs[0]
	formation, ok := findFormation(board.Formations, ref.NodeID)
	if !ok {
		return false, nil
	}
	result, err := reattacher.ReattachFormationDispatch(FormationReattachRequest{
		RunID:      runID,
		DispatchID: ref.DispatchID,
		NodeID:     ref.NodeID,
		SlotID:     ref.SlotID,
		Formation:  formation,
	})
	if err != nil {
		return true, err
	}
	if result.Status == "" {
		result.Status = "done"
	}
	if err := e.ensureFormationOutputPayloads(runID, formation, result); err != nil {
		return true, err
	}
	if err := e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventNodeOutput,
		NodeID: ref.NodeID,
		Data:   formationOutputEventData(result),
	}); err != nil {
		return true, err
	}
	return true, nil
}

func (e *RunEngine) appendOpenDispatchReattachFailure(runID string, refs []openDispatchRef, reason string) error {
	reason = redactLedgerText(reason)
	openDispatches := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		openDispatches = append(openDispatches, map[string]any{
			"dispatchId": ref.DispatchID,
			"nodeId":     ref.NodeID,
			"slotId":     ref.SlotID,
		})
		if err := e.store.AppendRunEvent(runID, RunEvent{
			Type:   RunEventError,
			NodeID: ref.NodeID,
			SlotID: ref.SlotID,
			Data: map[string]any{
				"code":        "dispatch_reattach_failed",
				"message":     reason,
				"reason":      reason,
				"boundary":    "recovery",
				"nodeId":      ref.NodeID,
				"slotId":      ref.SlotID,
				"recoverable": true,
				"dispatchId":  ref.DispatchID,
			},
		}); err != nil {
			return err
		}
	}
	blockedNodeID := ""
	blockedSlotID := ""
	if len(refs) > 0 {
		blockedNodeID = refs[0].NodeID
		blockedSlotID = refs[0].SlotID
	}
	return e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventBlocked,
		NodeID: blockedNodeID,
		SlotID: blockedSlotID,
		Data: map[string]any{
			"code":           "dispatch_reattach_failed",
			"reason":         "dispatch reattach failed: " + reason,
			"blockedNodeId":  blockedNodeID,
			"resumeAllowed":  true,
			"resumePolicy":   "explicit",
			"openDispatches": openDispatches,
			"nextEpoch":      1,
		},
	})
}

func (e *RunEngine) startFormationRun(slug string, board *BoardDocument, formation FormationNode, actor string, personas *PersonaStore) (*RunStartResult, MissionNode, RunInputRef, error) {
	bindings, err := resolveRunBindings(board, personas)
	if err != nil {
		return nil, MissionNode{}, RunInputRef{}, err
	}
	goal := ""
	beadID := ""
	if formation.Brief != nil {
		goal = formation.Brief.Goal
		beadID = formation.Brief.BeadID
	}
	mission := MissionNode{
		ID:    "single_" + formation.ID,
		Title: "Single formation: " + formation.Title,
		Goal:  goal,
	}
	runID := newPrefixedID("run")
	ledgerPath := runArtifactPath(slug, runID, ".ndjson")
	snapshotPath := runArtifactPath(slug, runID, ".snapshot.toml")
	bindingsPath := runArtifactPath(slug, runID, ".bindings.toml")
	boardRaw := []byte(board.TOML)
	if int64(len(boardRaw)) > runRecordMaxBytes {
		return nil, MissionNode{}, RunInputRef{}, fmt.Errorf("%w: run snapshot exceeds byte limit", ErrRunLedgerInvalid)
	}
	bindingsRaw := []byte(renderRunBindings(runID, board, mission, bindings, nil))
	if int64(len(bindingsRaw)) > runRecordMaxBytes {
		return nil, MissionNode{}, RunInputRef{}, fmt.Errorf("%w: run persona snapshot exceeds byte limit", ErrRunLedgerInvalid)
	}
	runDirectory, err := e.store.openRunArtifactDirectory(slug, true)
	if err != nil {
		return nil, MissionNode{}, RunInputRef{}, err
	}
	defer runDirectory.close()
	if err := writeRunArtifactExclusiveAt(runDirectory, runID+".snapshot.toml", boardRaw); err != nil {
		return nil, MissionNode{}, RunInputRef{}, err
	}
	if err := writeRunArtifactExclusiveAt(runDirectory, runID+".bindings.toml", bindingsRaw); err != nil {
		return nil, MissionNode{}, RunInputRef{}, err
	}
	started := &RunStartResult{
		RunID:                runID,
		BoardSlug:            slug,
		LedgerPath:           ledgerPath,
		SnapshotPath:         snapshotPath,
		BindingsSnapshotPath: bindingsPath,
	}
	event := RunEvent{
		Timestamp: e.store.now().Format(time.RFC3339Nano),
		RunID:     runID,
		Seq:       1,
		Type:      RunEventStarted,
		Actor:     defaultRunActor(actor),
		BoardID:   board.ID,
		BoardRev:  board.Rev,
		MissionID: mission.ID,
		BeadID:    beadID,
		Epoch:     0,
		Attempt:   0,
		Data: map[string]any{
			"missionSlug":      slug,
			"missionPath":      filepath.ToSlash(e.store.BoardPath(slug)),
			"missionRev":       board.Rev,
			"snapshot":         snapshotPath,
			"bindingsSnapshot": bindingsPath,
			"inputCardId":      mission.ID,
			"beadId":           beadID,
			"objective":        mission.Goal,
			"mode":             "formation",
			"formationId":      formation.ID,
		},
	}
	if err := writeInitialRunEventAt(runDirectory, runID, event); err != nil {
		return nil, MissionNode{}, RunInputRef{}, err
	}
	seedInput := RunInputRef{
		Ref:  "brief://" + formation.ID,
		Text: goal,
	}
	return started, mission, seedInput, nil
}

// resumeSnapshot rebuilds the run's deliveries from its ledger and runs
// whatever is still owed. reason names why the run continues: resume after a
// block, or continue after a verdict.
func (e *RunEngine) resumeSnapshot(runID string, board *BoardDocument, mission MissionNode, events []RunEvent, reason string) error {
	gateByID := map[string]GateNode{}
	for _, gate := range board.Gates {
		gateByID[gate.ID] = gate
	}
	// A terminal pass in the ledger is not the end of the run by itself:
	// replay first, so every branch still to run continues (archon-n7u.53).

	ready := map[string]map[string]RunInputRef{}
	queued := map[string]bool{}
	attempts := map[string]int{}
	processedGateInputs := processedGateInputRefs(board, events)
	replayOutputOrdinals := map[string]int{}
	var queue []string

	for _, event := range events {
		if event.Type == RunEventNodeStarted && event.Attempt > attempts[event.NodeID] {
			attempts[event.NodeID] = event.Attempt
		}
		if event.Type != RunEventNodeOutput {
			continue
		}
		if err := e.replayNodeOutputToReady(runID, board, gateByID, event, processedGateInputs, replayOutputOrdinals, ready, queued, &queue); err != nil {
			if errors.Is(err, errRunStopped) {
				return nil
			}
			return err
		}
	}
	if err := e.resumeIncompleteGateEvaluations(runID, board, gateByID, events, ready, queued, &queue); err != nil {
		if errors.Is(err, errRunStopped) {
			return nil
		}
		return err
	}
	if err := e.replayGateVerdictsToReady(runID, board, gateByID, events, ready, queued, &queue); err != nil {
		if errors.Is(err, errRunStopped) {
			return nil
		}
		return err
	}
	// A run interrupted before its Input card delivered the brief, its first
	// output, starts there.
	if !slices.ContainsFunc(events, func(event RunEvent) bool { return event.Type == RunEventNodeOutput }) {
		if err := e.deliverInputCard(runID, board, gateByID, mission, ready, queued, &queue); err != nil {
			if errors.Is(err, errRunStopped) {
				return nil
			}
			return err
		}
	}
	return e.drainRun(runID, board, gateByID, attempts, ready, queued, &queue, reason, reason)
}

// drainRun runs the queued steps one at a time, then ends the run by the one
// completion rule. Before each step, and after each step records its output
// but before that output is delivered, it routes the human verdicts recorded
// meanwhile (archon-o7p.11). Replay queues every formation the ledger ever
// fed; a step runs only while the ledger, read as it stands now, still owes
// it a delivery, so a send-back routed during this drain runs again
// (archon-n7u.53). startReason is recorded on each node_started, and
// finishReason, when set, on run_succeeded.
func (e *RunEngine) drainRun(runID string, board *BoardDocument, gates map[string]GateNode, attempts map[string]int, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string, startReason, finishReason string) error {
	formationByID := map[string]FormationNode{}
	for _, formation := range board.Formations {
		formationByID[formation.ID] = formation
	}
	stopped := func(err error) error {
		if errors.Is(err, errRunStopped) {
			return nil
		}
		return err
	}
	for {
		if err := e.routeRecordedVerdicts(runID, board, gates, ready, queued, queue); err != nil {
			return stopped(err)
		}
		if len(*queue) == 0 {
			break
		}
		nodeID := (*queue)[0]
		*queue = (*queue)[1:]
		queued[nodeID] = false
		current, err := e.store.ReadRunEvents(runID)
		if err != nil {
			return err
		}
		if !runWorkOwedTo(board, current, nodeID) {
			continue
		}
		formation, ok := formationByID[nodeID]
		if !ok {
			continue
		}
		inputs := orderedInputs(formation, ready[nodeID])
		if !formationReady(formation, ready[nodeID]) {
			if err := e.appendWaiting(runID, formation, ready[nodeID]); err != nil {
				return err
			}
			continue
		}
		nextAttempt := attempts[nodeID] + 1
		attempts[nodeID] = nextAttempt
		if err := e.startFormationExecution(runID, board, formation, RunEvent{
			Type:    RunEventNodeStarted,
			NodeID:  nodeID,
			Attempt: nextAttempt,
			Data: map[string]any{
				"nodeKind":  "formation",
				"inputRefs": inputs,
				"reason":    startReason,
				"brief":     formationBriefEventData(formationBriefValue(formation)),
			},
		}); err != nil {
			return stopped(err)
		}
		result, err := e.executeFormation(FormationExecution{
			RunID:           runID,
			NodeID:          nodeID,
			Title:           formation.Title,
			Formation:       formation,
			Brief:           formationBriefValue(formation),
			Inputs:          inputs,
			Attempt:         nextAttempt,
			KeepSeatsOnCall: RunHumanChannel(board, current) == HumanChannelSession && FormationKeepsSeatsOnCall(board, nodeID),
		})
		if err != nil {
			return e.appendExecutionFailureAndBlock(runID, nodeID, err)
		}
		if result.Status == "" {
			result.Status = "done"
		}
		if err := e.ensureFormationOutputPayloads(runID, formation, result); err != nil {
			return stopped(err)
		}
		if err := e.store.AppendRunEvent(runID, RunEvent{
			Type:   RunEventNodeOutput,
			NodeID: nodeID,
			Data:   formationOutputEventData(result),
		}); err != nil {
			return err
		}
		if err := e.routeRecordedVerdicts(runID, board, gates, ready, queued, queue); err != nil {
			return stopped(err)
		}
		if err := e.deliverFormationOutput(runID, board, gates, nodeID, result, ready, queued, queue); err != nil {
			return stopped(err)
		}
	}
	completionEvents, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	return e.endWhenNothingCanRun(runID, board, completionEvents, finishReason)
}

func (e *RunEngine) resumeIncompleteGateEvaluations(runID string, board *BoardDocument, gates map[string]GateNode, events []RunEvent, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	pending := map[string]RunEvent{}
	for _, event := range events {
		gateID := event.GateID
		if gateID == "" {
			gateID = event.NodeID
		}
		if gateID == "" {
			continue
		}
		switch event.Type {
		case RunEventGateEvaluating:
			pending[gateID] = event
		case RunEventGateVerdict, RunEventHumanInputRequested, RunEventError:
			delete(pending, gateID)
		}
	}
	evaluations := make([]RunEvent, 0, len(pending))
	for _, event := range pending {
		evaluations = append(evaluations, event)
	}
	sort.Slice(evaluations, func(i, j int) bool { return evaluations[i].Seq < evaluations[j].Seq })
	for _, evaluation := range evaluations {
		gateID := evaluation.GateID
		if gateID == "" {
			gateID = evaluation.NodeID
		}
		gate, ok := gates[gateID]
		if !ok {
			return fmt.Errorf("%w: gate %q", ErrNotFound, gateID)
		}
		input := runInputRefFromAny(evaluation.Data["inputRef"])
		prior, err := e.durableGateKindResultsForEvaluation(runID, gate, input, events, evaluation.Seq)
		if err != nil {
			return err
		}
		if err := e.evaluateGateKinds(runID, board, gates, gate, input, ready, queued, queue, prior); err != nil {
			return err
		}
	}
	return nil
}

func (e *RunEngine) durableGateKindResultsForEvaluation(runID string, gate GateNode, input RunInputRef, events []RunEvent, evaluatingSeq int) (map[string]durableGateKindResult, error) {
	results := map[string]durableGateKindResult{}
	lastKindIndex := -1
	canonicalOrder := map[string]int{"code": 0, "formation": 1}
	for _, event := range events {
		if event.Seq <= evaluatingSeq {
			continue
		}
		gateID := event.GateID
		if gateID == "" {
			gateID = event.NodeID
		}
		if gateID != gate.ID {
			continue
		}
		if event.Type == RunEventGateEvaluating || event.Type == RunEventGateVerdict || event.Type == RunEventHumanInputRequested {
			break
		}
		if event.Type != RunEventGateKindResult {
			continue
		}
		kind := stringFromAny(event.Data["kind"])
		kindIndex, ok := canonicalOrder[kind]
		if !ok || kindIndex <= lastKindIndex {
			return nil, fmt.Errorf("%w: Gate %q has duplicate or out-of-order kind result %q", ErrRunLedgerInvalid, gate.ID, kind)
		}
		if !reflect.DeepEqual(runInputRefFromAny(event.Data["inputRef"]), input) {
			return nil, fmt.Errorf("%w: Gate %q kind result input mismatch", ErrRunLedgerInvalid, gate.ID)
		}
		result, err := e.gateKindResultFromEvent(runID, gate, input, event)
		if err != nil {
			return nil, err
		}
		results[kind] = durableGateKindResult{Seq: event.Seq, Result: result}
		lastKindIndex = kindIndex
	}
	return results, nil
}

func (e *RunEngine) gateKindResultFromEvent(runID string, gate GateNode, input RunInputRef, event RunEvent) (GateEvaluationResult, error) {
	kind := stringFromAny(event.Data["kind"])
	verdict := stringFromAny(event.Data["verdict"])
	if verdict != "pass" && verdict != "fail" {
		return GateEvaluationResult{}, fmt.Errorf("%w: Gate %q kind %q has invalid verdict", ErrRunLedgerInvalid, gate.ID, kind)
	}
	result := GateEvaluationResult{
		Verdict:        verdict,
		Reason:         stringFromAny(event.Data["reason"]),
		Evidence:       gateEvidenceRefsFromRunEventData(event.Data["evidence"]),
		PerKind:        map[string]string{kind: verdict},
		KindResultSeqs: map[string]int{kind: event.Seq},
		ResultEncoding: stringFromAny(event.Data["resultEncoding"]),
		ResultSHA256:   stringFromAny(event.Data["resultSha256"]),
		GateBindingID:  stringFromAny(event.Data["gateBindingId"]),
	}
	if kind != "code" {
		return result, nil
	}
	binding, err := e.store.readRunGateBinding(runID, gate.ID)
	if err != nil {
		return GateEvaluationResult{}, err
	}
	if stringFromAny(event.Data["inputSha256"]) != codeGateSHA256(input.Text) ||
		stringFromAny(event.Data["profileId"]) != binding.ProfileID ||
		stringFromAny(event.Data["profileVersion"]) != binding.ProfileVersion ||
		stringFromAny(event.Data["profileSha256"]) != binding.ProfileSHA256 ||
		stringFromAny(event.Data["evaluatorBundleSha256"]) != binding.EvaluatorBundleSHA256 ||
		stringFromAny(event.Data["parametersSha256"]) != binding.ParametersSHA256 ||
		stringFromAny(event.Data["policySha256"]) != binding.PolicySHA256 ||
		stringFromAny(event.Data["determinismPolicySha256"]) != binding.DeterminismPolicySHA256 ||
		intFromRunEventData(event.Data["maxInputBytes"]) != binding.MaxInputBytes ||
		intFromRunEventData(event.Data["maxResultBytes"]) != binding.MaxResultBytes ||
		intFromRunEventData(event.Data["maxOperations"]) != binding.MaxOperations {
		return GateEvaluationResult{}, fmt.Errorf("%w: Gate %q code result frozen binding mismatch", ErrRunLedgerInvalid, gate.ID)
	}
	canonical, err := canonicalCodeGateResult(result.Verdict, result.Reason, result.Evidence)
	if err != nil {
		return GateEvaluationResult{}, fmt.Errorf("%w: Gate %q canonical code result: %v", ErrRunLedgerInvalid, gate.ID, err)
	}
	result.CanonicalResult = canonical
	result.CodeVerdict = result.Verdict
	result.CodeReason = result.Reason
	if err := validateCodeGateEvaluationResult(GateEvaluation{
		RunID:        runID,
		GateID:       gate.ID,
		Check:        gate.Check,
		CheckVersion: gate.CheckVersion,
		CheckValue:   gate.CheckValue,
		Input:        input,
		Binding:      binding,
	}, result); err != nil {
		return GateEvaluationResult{}, fmt.Errorf("%w: %v", ErrRunLedgerInvalid, err)
	}
	return result, nil
}

// endWhenNothingCanRun applies the one completion rule (runFinishState) once
// the engine has run everything queued: work that can still run blocks the
// run resumably; gates waiting on the operator leave the run waiting, with no
// event, until a verdict continues it (archon-o7p.11); a rejected path fails
// it, even when its rejection starved a join; formations starved with no
// rejection block it as a wiring gap; and otherwise it succeeds.
func (e *RunEngine) endWhenNothingCanRun(runID string, board *BoardDocument, events []RunEvent, reason string) error {
	finish := runFinishState(board, events, "")
	deciding := openHumanDecisions(events)
	runnable := slices.DeleteFunc(finish.runnable, func(nodeID string) bool { return slices.Contains(deciding, nodeID) })
	switch {
	case len(runnable) > 0:
		return e.appendUnfinishedWorkBlock(runID, runnable)
	case len(deciding) > 0:
		return nil
	case finish.rejected == nil && len(finish.starved) > 0:
		return e.appendStarvedBlock(runID, finish.starved)
	}
	return e.finishRun(runID, board, events, reason)
}

// RunFailurePathRejected is the run_failed code of a run whose path ended at
// a rejected End node; the failure's reason is the gate verdict's reason.
const RunFailurePathRejected = "path_rejected"

// finishRun ends a run in which every path has ended and nothing else can run
// (unfinishedRunWork is empty). A path that ended at a rejected End node
// fails the run with the reason of the gate verdict that routed there;
// otherwise the run succeeds. reason, when set, says how the run got here,
// such as resume.
func (e *RunEngine) finishRun(runID string, board *BoardDocument, events []RunEvent, reason string) error {
	if err := e.EndKeptSeats(runID); err != nil {
		return err
	}
	// endIds lists every End node a path ended at, so a run view can show where.
	endIDs := []string{}
	for _, path := range runPaths(board, events).ended {
		if !slices.Contains(endIDs, path.EndID) {
			endIDs = append(endIDs, path.EndID)
		}
	}
	if rejected := rejectedRunPath(board, events); rejected != nil {
		failure := rejected.Reason
		if failure == "" {
			failure = fmt.Sprintf("the path ended at %s (rejected)", nodeName(board, rejected.EndID))
		}
		return e.store.AppendRunEvent(runID, RunEvent{
			Type:   RunEventFailed,
			Actor:  rejected.Actor,
			NodeID: rejected.EndID,
			GateID: rejected.GateID,
			Data: map[string]any{
				"code":   RunFailurePathRejected,
				"reason": failure,
				"endId":  rejected.EndID,
				"gateId": rejected.GateID,
				"endIds": endIDs,
				"final":  true,
			},
		})
	}
	data := map[string]any{
		"summaryRef":   "",
		"outputRefs":   []string{},
		"artifactRefs": []string{},
		"endIds":       endIDs,
		"final":        true,
	}
	if reason != "" {
		data["reason"] = reason
	}
	return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventSucceeded, Data: data})
}

func processedGateInputRefs(board *BoardDocument, events []RunEvent) map[string]bool {
	processed := map[string]bool{}
	outputOrdinals := map[string]int{}
	for _, event := range events {
		if event.Type == RunEventNodeOutput {
			advanceOutputOrdinals(board, outputOrdinals, event)
			continue
		}
		switch event.Type {
		case RunEventGateEvaluating, RunEventGateVerdict, RunEventHumanInputRequested:
		default:
			continue
		}
		if event.Data == nil {
			continue
		}
		input := runInputRefFromAny(event.Data["inputRef"])
		if input.EdgeID != "" {
			processed[gateInputReplayKey(input.EdgeID, gateInputOutputSeq(input, outputOrdinals))] = true
		}
	}
	return processed
}

func advanceOutputOrdinals(board *BoardDocument, outputOrdinals map[string]int, event RunEvent) {
	if board == nil || event.Type != RunEventNodeOutput {
		return
	}
	for _, connection := range outgoingConnections(board.Connections, event.NodeID) {
		_, fromPort := endpointParts(connection.From)
		if _, ok := outputPayloadForPortFromEvent(event, fromPort); ok {
			outputOrdinals[connection.ID]++
		}
	}
}

func gateInputOutputSeq(input RunInputRef, outputOrdinals map[string]int) int {
	if input.OutputSeq > 0 {
		return input.OutputSeq
	}
	return outputOrdinals[input.EdgeID]
}

func gateInputReplayKey(edgeID string, outputSeq int) string {
	return fmt.Sprintf("%s#%d", edgeID, outputSeq)
}

func (e *RunEngine) replayNodeOutputToReady(runID string, board *BoardDocument, gates map[string]GateNode, event RunEvent, processedGateInputs map[string]bool, outputOrdinals map[string]int, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	// Judge chains are evaluated by their owning gate. Their outputs are
	// durable evidence, never workflow inputs, including links between judges.
	for _, gate := range board.Gates {
		for _, judge := range judgeChainForGate(board, gate.ID) {
			if judge.ID == event.NodeID {
				return nil
			}
		}
	}
	for _, connection := range outgoingConnections(board.Connections, event.NodeID) {
		_, fromPort := endpointParts(connection.From)
		payload, ok := outputPayloadForPortFromEvent(event, fromPort)
		if !ok {
			if err := e.appendErrorAndBlock(runID, "missing_output_payload", fmt.Sprintf("node %s did not produce output for port %s", event.NodeID, fromPort), "engine", event.NodeID, "missing output payload"); err != nil {
				return err
			}
			return errRunStopped
		}
		toNode, toPort := endpointParts(connection.To)
		if toNode == "" || toPort == "" {
			continue
		}
		outputOrdinals[connection.ID]++
		outputSeq := outputOrdinals[connection.ID]
		input := runInputRefForConnection(runID, connection, payload)
		input.OutputSeq = outputSeq
		if _, ok := gates[toNode]; ok {
			key := gateInputReplayKey(connection.ID, outputSeq)
			if processedGateInputs[key] {
				continue
			}
			processedGateInputs[key] = true
			if err := e.deliverConnection(runID, board, gates, connection, input, ready, queued, queue); err != nil {
				return err
			}
			continue
		}
		formation, ok := findFormation(board.Formations, toNode)
		if !ok {
			continue
		}
		if ready[toNode] == nil {
			ready[toNode] = map[string]RunInputRef{}
		}
		ready[toNode][toPort] = input
		if formationReady(formation, ready[toNode]) && !queued[toNode] {
			queued[toNode] = true
			*queue = append(*queue, toNode)
		}
	}
	return nil
}

func (e *RunEngine) replayGateVerdictsToReady(runID string, board *BoardDocument, gates map[string]GateNode, events []RunEvent, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	evaluations := gateEvaluationKeys(board, events)
	outputOrdinals := map[string]int{}
	for i, event := range events {
		if event.Type == RunEventNodeOutput {
			advanceOutputOrdinals(board, outputOrdinals, event)
			continue
		}
		if event.Type != RunEventGateVerdict {
			continue
		}
		routePort := stringFromEventData(event, "routePort")
		if routePort != "pass" && routePort != "fail" {
			continue
		}
		gateID := event.GateID
		if gateID == "" {
			gateID = event.NodeID
		}
		input := runInputRefFromAny(event.Data["inputRef"])
		for _, route := range gateVerdictRoutes(board, event, gateID, routePort) {
			// Every route is replayed; the resume loop runs only targets still
			// owed this delivery (runWorkOwedTo), so a serviced pushback is not rerun.
			nextInput := input
			if routePort == "fail" {
				nextInput = gateFailInput(runID, route, gateID, event.Attempt, input, stringFromEventData(event, "reason"), gateEvidenceRefsFromRunEventData(event.Data["evidence"]))
			} else if routePort == "pass" {
				var err error
				if nextInput, err = gatePassInput(events, event, gateID, input); err != nil {
					return err
				}
			}
			// A gate that already began evaluating this delivery has consumed it.
			// Replaying it would re-run that gate on stale input ahead of any
			// pushback that followed its verdict.
			toNode, _ := endpointParts(route.To)
			if _, isGate := gates[toNode]; isGate && evaluations.consumedAfter(i, toNode, gateInputReplayKey(nextInput.EdgeID, gateInputOutputSeq(nextInput, outputOrdinals))) {
				continue
			}
			if err := e.deliverConnection(runID, board, gates, route, nextInput, ready, queued, queue); err != nil {
				return err
			}
		}
	}
	return nil
}

type gateEvaluationKey struct {
	index  int
	gateID string
	input  string
}

type gateEvaluationIndex []gateEvaluationKey

// gateEvaluationKeys lists each gate evaluation with the input it took, keyed
// the way node outputs number their deliveries at that point in the ledger.
func gateEvaluationKeys(board *BoardDocument, events []RunEvent) gateEvaluationIndex {
	outputOrdinals := map[string]int{}
	var keys gateEvaluationIndex
	for i, event := range events {
		switch event.Type {
		case RunEventNodeOutput:
			advanceOutputOrdinals(board, outputOrdinals, event)
		case RunEventGateEvaluating:
			gateID := event.GateID
			if gateID == "" {
				gateID = event.NodeID
			}
			input := runInputRefFromAny(event.Data["inputRef"])
			keys = append(keys, gateEvaluationKey{index: i, gateID: gateID, input: gateInputReplayKey(input.EdgeID, gateInputOutputSeq(input, outputOrdinals))})
		}
	}
	return keys
}

func (keys gateEvaluationIndex) consumedAfter(index int, gateID, input string) bool {
	for _, key := range keys {
		if key.index > index && key.gateID == gateID && key.input == input {
			return true
		}
	}
	return false
}

func gateVerdictRoutes(board *BoardDocument, event RunEvent, gateID, routePort string) []BoardConnection {
	routed := map[string]bool{}
	for _, id := range stringSliceFromAny(event.Data["routedEdges"]) {
		routed[id] = true
	}
	var routes []BoardConnection
	for _, connection := range outgoingConnectionsFromPort(board.Connections, gateID, routePort) {
		if len(routed) > 0 && !routed[connection.ID] {
			continue
		}
		routes = append(routes, connection)
	}
	return routes
}

func (e *RunEngine) readRunBoard(runID string) (*BoardDocument, error) {
	return e.store.ReadRunBoard(runID)
}

// ReadRunBoard returns the validated frozen definition, not the current draft.
func (s *Store) ReadRunBoard(runID string) (*BoardDocument, error) {
	ledger, err := s.openRunLedger(runID, false)
	if err != nil {
		return nil, fmt.Errorf("%w: open run ledger: %v", ErrRunLedgerInvalid, err)
	}
	defer ledger.close()
	events, err := readRunEventsFrom(ledger.file, runID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrRunLedgerInvalid
	}
	return s.readRunSnapshot(events[0], runID, ledger)
}

func (e *RunEngine) executeSnapshot(runID string, board *BoardDocument, mission MissionNode) error {
	gateByID := map[string]GateNode{}
	for _, gate := range board.Gates {
		gateByID[gate.ID] = gate
	}
	ready := map[string]map[string]RunInputRef{}
	queued := map[string]bool{}
	attempts := map[string]int{}
	var queue []string
	if err := e.deliverInputCard(runID, board, gateByID, mission, ready, queued, &queue); err != nil {
		if errors.Is(err, errRunStopped) {
			return nil
		}
		return err
	}
	return e.drainRun(runID, board, gateByID, attempts, ready, queued, &queue, "initial", "")
}

// deliverInputCard records the Input card's output, the run's brief, and
// delivers it to the steps it is wired to.
func (e *RunEngine) deliverInputCard(runID string, board *BoardDocument, gates map[string]GateNode, mission MissionNode, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	if err := e.store.AppendRunEvent(runID, RunEvent{
		Type:      RunEventNodeStarted,
		NodeID:    mission.ID,
		MissionID: mission.ID,
		Data: map[string]any{
			"nodeKind":  "inputCard",
			"inputRefs": []RunInputRef{},
			"reason":    "initial",
		},
	}); err != nil {
		return err
	}
	missionText := mission.Goal
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	if brief := stringFromEventData(events[0], "brief"); brief != "" {
		missionText = brief
	}
	missionOutputs := map[string]FormationOutputPayload{
		"out": {Text: missionText},
	}
	if err := e.store.AppendRunEvent(runID, RunEvent{
		Type:      RunEventNodeOutput,
		NodeID:    mission.ID,
		MissionID: mission.ID,
		Data: formationOutputEventData(FormationExecutionResult{
			Status:  "done",
			Text:    missionText,
			Outputs: missionOutputs,
		}),
	}); err != nil {
		return err
	}
	return e.deliverOutputPayloads(runID, board, gates, mission.ID, missionOutputs, ready, queued, queue)
}

// startFormationExecution records a step's start before the executor can
// launch seats, once the run's Limit cards allow it (archon-o7p.8): a spent
// rounds limit blocks the run instead, resumable only with a grant. Every
// start counts, failed or interrupted work included, so neither resume nor a
// new engine replenishes an allowance; only a grant does. The run's worker
// serializes this read and append with other executions.
func (e *RunEngine) startFormationExecution(runID string, board *BoardDocument, formation FormationNode, event RunEvent) error {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	started := e.store.now()
	spent := limitSpentBefore(board, events, formation, started)
	if formation.Type == FormationTypePeer && spent == nil {
		// A peer step's rounds are its journal messages, posted in earlier attempts.
		if use := e.peerMessagesUse(board, events, FormationExecution{RunID: runID, NodeID: formation.ID, Attempt: event.Attempt}); use != nil && use.Used >= use.Max {
			spent = use
		}
	}
	if spent != nil {
		if err := e.appendLimitBlock(runID, board, formation.ID, *spent); err != nil {
			return err
		}
		return errRunStopped
	}
	event.Timestamp = started.Format(time.RFC3339Nano)
	return e.store.AppendRunEvent(runID, event)
}

func (e *RunEngine) executeFormation(req FormationExecution) (FormationExecutionResult, error) {
	if e.executor == nil {
		return FormationExecutionResult{}, ErrRunExecutorUnavailable
	}
	ctx := context.Background()
	if e.executionContext != nil {
		ctx = e.executionContext(req.RunID)
	}
	if err := dispatchContextError(ctx); err != nil {
		return FormationExecutionResult{}, err
	}
	events, err := e.store.ReadRunEvents(req.RunID)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if len(events) == 0 {
		return FormationExecutionResult{}, ErrRunLedgerInvalid
	}
	req.MissionBeadID = events[0].BeadID
	req.Cwd = stringFromEventData(events[0], "cwd")
	req.ContextPaths = stringSliceFromAny(events[0].Data["contextPaths"])
	req.MissionGoal = stringFromEventData(events[0], "objective")
	board, err := e.readRunBoard(req.RunID)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if req.Formation.Type == FormationTypePeer {
		req.PeerMessages = e.peerMessagesUse(board, events, req)
	}
	// Kept seats are reconsidered as a formation starts dispatching (ADR-0019).
	if err := e.reconsiderKeptSeats(req.RunID, req.NodeID); err != nil {
		return FormationExecutionResult{}, err
	}
	now := e.store.now()
	// The time cards covering the step bound it from the ledger, so a restart or
	// a resumed attempt gets only the time it has left (archon-o7p.8).
	timeLimit, warnings := timeBudget(board, events, req.NodeID, now)
	req.Warnings = warnings
	timeSpent := func() error {
		use := *timeLimit
		if limit, ok := findLimit(board, use.LimitID); ok {
			if events, err := e.store.ReadRunEvents(req.RunID); err == nil {
				if current := timeUse(board, events, limit, e.store.now()); current != nil {
					use = *current
				}
			}
		}
		// The step stops at the limit: it used all of the card's time.
		use.Used = use.Max
		return &LimitReachedError{Use: use}
	}
	if timeLimit != nil {
		req.Deadline = now.Add(time.Duration(timeLimit.Max-timeLimit.Used) * time.Second)
		if !now.Before(req.Deadline) {
			return FormationExecutionResult{}, timeSpent()
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeoutCause(ctx, req.Deadline.Sub(now), ErrFormationTimeoutExceeded)
		defer cancel()
	}
	if executor, ok := e.executor.(ContextFormationExecutor); ok {
		result, err := executor.ExecuteFormationContext(ctx, req)
		if cause := context.Cause(ctx); cause != nil {
			if timeLimit != nil && errors.Is(cause, ErrFormationTimeoutExceeded) {
				return FormationExecutionResult{}, timeSpent()
			}
			return FormationExecutionResult{}, cause
		}
		if !req.Deadline.IsZero() && (!e.store.now().Before(req.Deadline) || errors.Is(err, ErrFormationTimeoutExceeded)) {
			return FormationExecutionResult{}, timeSpent()
		}
		return result, err
	}
	// Coordinator-owned executors without context support run synchronously so a timeout cannot
	// leave a hidden writer behind after the coordinator releases its lock.
	if e.executionContext != nil || req.Deadline.IsZero() {
		result, err := e.executor.ExecuteFormation(req)
		if !req.Deadline.IsZero() && !e.store.now().Before(req.Deadline) {
			return FormationExecutionResult{}, timeSpent()
		}
		return result, err
	}
	type executionResult struct {
		result FormationExecutionResult
		err    error
	}
	done := make(chan executionResult, 1)
	go func() {
		result, err := e.executor.ExecuteFormation(req)
		done <- executionResult{result: result, err: err}
	}()
	select {
	case result := <-done:
		if !e.store.now().Before(req.Deadline) {
			return FormationExecutionResult{}, timeSpent()
		}
		return result.result, result.err
	case <-ctx.Done():
		if timeLimit != nil && errors.Is(context.Cause(ctx), ErrFormationTimeoutExceeded) {
			return FormationExecutionResult{}, timeSpent()
		}
		return FormationExecutionResult{}, context.Cause(ctx)
	}
}

func (e *RunEngine) ensureFormationOutputPayloads(runID string, formation FormationNode, result FormationExecutionResult) error {
	expected := make(map[string]bool, len(formation.Outputs))
	for _, output := range formation.Outputs {
		expected[output.ID] = true
		if _, ok := result.Outputs[output.ID]; !ok {
			if err := e.appendErrorAndBlock(runID, "missing_output_payload", fmt.Sprintf("formation %s did not produce output for port %s", formation.ID, output.ID), "engine", formation.ID, "missing output payload"); err != nil {
				return err
			}
			return errRunStopped
		}
	}
	for portID := range result.Outputs {
		if !expected[portID] {
			if err := e.appendErrorAndBlock(runID, "invalid_output_payload", fmt.Sprintf("formation %s produced unknown output port %s", formation.ID, portID), "engine", formation.ID, "invalid output payload"); err != nil {
				return err
			}
			return errRunStopped
		}
	}
	return nil
}

func (e *RunEngine) deliverOutputPayloads(runID string, board *BoardDocument, gates map[string]GateNode, fromNodeID string, outputs map[string]FormationOutputPayload, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	for _, connection := range outgoingConnections(board.Connections, fromNodeID) {
		_, fromPort := endpointParts(connection.From)
		payload, ok := outputs[fromPort]
		if !ok {
			if err := e.appendErrorAndBlock(runID, "missing_output_payload", fmt.Sprintf("node %s did not produce output for port %s", fromNodeID, fromPort), "engine", fromNodeID, "missing output payload"); err != nil {
				return err
			}
			return errRunStopped
		}
		input := runInputRefForConnection(runID, connection, payload)
		if err := e.deliverConnection(runID, board, gates, connection, input, ready, queued, queue); err != nil {
			return err
		}
	}
	return nil
}

func (e *RunEngine) deliverFormationOutput(runID string, board *BoardDocument, gates map[string]GateNode, fromNodeID string, result FormationExecutionResult, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	return e.deliverOutputPayloads(runID, board, gates, fromNodeID, result.Outputs, ready, queued, queue)
}

func runInputRefForConnection(runID string, connection BoardConnection, payload FormationOutputPayload) RunInputRef {
	fromNode, fromPort := endpointParts(connection.From)
	_, toPort := endpointParts(connection.To)
	ref := payload.Ref
	if ref == "" {
		ref = fmt.Sprintf("ledger://%s/%s", runID, connection.ID)
	}
	return RunInputRef{
		EdgeID:      connection.ID,
		FromNodeID:  fromNode,
		FromPortID:  fromPort,
		ToPortID:    toPort,
		Ref:         ref,
		Text:        payload.Text,
		ReportRef:   payload.ReportRef,
		ArtifactRef: payload.ArtifactRef,
	}
}

func formationOutputEventData(result FormationExecutionResult) map[string]any {
	outputs := make(map[string]FormationOutputPayload, len(result.Outputs))
	for portID, payload := range result.Outputs {
		outputs[portID] = payload
	}
	return map[string]any{
		"status":    result.Status,
		"reportRef": result.ReportRef,
		"text":      result.Text,
		"outputs":   outputs,
	}
}

func outputPayloadForPortFromEvent(event RunEvent, portID string) (FormationOutputPayload, bool) {
	outputs := outputPayloadsFromAny(event.Data["outputs"])
	payload, ok := outputs[portID]
	return payload, ok
}

func outputPayloadsFromAny(value any) map[string]FormationOutputPayload {
	switch raw := value.(type) {
	case map[string]FormationOutputPayload:
		outputs := make(map[string]FormationOutputPayload, len(raw))
		for portID, payload := range raw {
			outputs[portID] = payload
		}
		return outputs
	case map[string]any:
		outputs := make(map[string]FormationOutputPayload, len(raw))
		for portID, payload := range raw {
			outputs[portID] = outputPayloadFromAny(payload)
		}
		return outputs
	default:
		return nil
	}
}

func outputPayloadFromAny(value any) FormationOutputPayload {
	switch raw := value.(type) {
	case FormationOutputPayload:
		return raw
	case map[string]any:
		return FormationOutputPayload{
			Ref:         stringFromAny(raw["ref"]),
			Text:        stringFromAny(raw["text"]),
			ReportRef:   stringFromAny(raw["reportRef"]),
			ArtifactRef: stringFromAny(raw["artifactRef"]),
		}
	default:
		return FormationOutputPayload{}
	}
}

func (e *RunEngine) deliverConnection(runID string, board *BoardDocument, gates map[string]GateNode, connection BoardConnection, input RunInputRef, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	toNode, toPort := endpointParts(connection.To)
	if toNode == "" || toPort == "" {
		return nil
	}
	input.ToPortID = toPort
	if gate, ok := gates[toNode]; ok {
		return e.evaluateGate(runID, board, gates, gate, input, ready, queued, queue)
	}
	if _, ok := findEnd(board, toNode); ok {
		// The path ends here; runPaths reads it from the ledger.
		return nil
	}
	if ready[toNode] == nil {
		ready[toNode] = map[string]RunInputRef{}
	}
	ready[toNode][toPort] = input
	formation, ok := findFormation(board.Formations, toNode)
	if !ok {
		return nil
	}
	if formationReady(formation, ready[toNode]) {
		if !queued[toNode] {
			queued[toNode] = true
			*queue = append(*queue, toNode)
		}
		return nil
	}
	return e.appendWaiting(runID, formation, ready[toNode])
}

func (e *RunEngine) evaluateGate(runID string, board *BoardDocument, gates map[string]GateNode, gate GateNode, input RunInputRef, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	judgeChain := judgeChainForGate(board, gate.ID)
	if err := e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventGateEvaluating,
		GateID: gate.ID,
		NodeID: gate.ID,
		Data: map[string]any{
			"kinds":      gate.Kinds,
			"criterion":  gate.Criterion,
			"inputRef":   input,
			"judgeChain": formationIDs(judgeChain),
		},
	}); err != nil {
		return err
	}
	return e.evaluateGateKinds(runID, board, gates, gate, input, ready, queued, queue, nil)
}

type durableGateKindResult struct {
	Seq    int
	Result GateEvaluationResult
}

func (e *RunEngine) evaluateGateKinds(runID string, board *BoardDocument, gates map[string]GateNode, gate GateNode, input RunInputRef, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string, prior map[string]durableGateKindResult) error {
	result := GateEvaluationResult{
		Verdict:        "pass",
		PerKind:        map[string]string{},
		KindResultSeqs: map[string]int{},
	}
	if hasGateKind(gate.Kinds, "code") {
		codeResult := GateEvaluationResult{}
		codeResultSeq := 0
		if durable, ok := prior["code"]; ok {
			codeResult = durable.Result
			codeResultSeq = durable.Seq
		} else {
			var err error
			codeResult, err = e.evaluateCodeGateResult(GateEvaluation{
				RunID:        runID,
				GateID:       gate.ID,
				Title:        gate.Title,
				Kinds:        []string{"code"},
				Criterion:    gate.Criterion,
				Check:        gate.Check,
				CheckVersion: gate.CheckVersion,
				CheckValue:   gate.CheckValue,
				Input:        input,
			})
			if err != nil {
				return err
			}
			codeResult.Verdict = normalizeGateVerdict(codeResult.Verdict)
			codeResult.PerKind = map[string]string{"code": codeResult.Verdict}
			codeResultSeq, err = e.appendGateKindResult(runID, gate, "code", input, codeResult)
			if err != nil {
				return err
			}
		}
		codeVerdict := normalizeGateVerdict(codeResult.Verdict)
		codeResult.Verdict = codeVerdict
		codeResult.PerKind = map[string]string{"code": codeVerdict}
		codeResult.KindResultSeqs = map[string]int{"code": codeResultSeq}
		codeResult.CodeVerdict = codeVerdict
		codeResult.CodeReason = codeResult.Reason
		result = codeResult
		result.PerKind = map[string]string{"code": codeVerdict}
		result.KindResultSeqs = map[string]int{"code": codeResultSeq}
		if codeVerdict == "fail" {
			markLaterGateKindsNotRun(gate.Kinds, result.PerKind, "code")
			return e.routeGateEvaluation(runID, board, gates, gate, input, "fail", result, ready, queued, queue)
		}
	}
	if hasGateKind(gate.Kinds, "formation") {
		formationResult := GateEvaluationResult{}
		formationResultSeq := 0
		if durable, ok := prior["formation"]; ok {
			formationResult = durable.Result
			formationResultSeq = durable.Seq
		} else {
			text, err := e.runJudgeChain(board, GateEvaluation{
				RunID:     runID,
				GateID:    gate.ID,
				Title:     gate.Title,
				Kinds:     []string{"formation"},
				Criterion: gate.Criterion,
				Input:     input,
			}, judgeChainForGate(board, gate.ID))
			if err != nil {
				return err
			}
			formationResult, err = parseJudgeVerdict(text)
			if err != nil {
				return e.blockInvalidJudge(runID, gate.ID, err)
			}
			formationResultSeq, err = e.appendGateKindResult(runID, gate, "formation", input, formationResult)
			if err != nil {
				return err
			}
		}
		formationResult.PerKind["formation"] = formationResult.Verdict
		result.Verdict = formationResult.Verdict
		result.Reason = formationResult.Reason
		result.Evidence = append(result.Evidence, formationResult.Evidence...)
		result.PerKind["formation"] = formationResult.Verdict
		result.KindResultSeqs["formation"] = formationResultSeq
		if formationResult.Verdict == "fail" {
			markLaterGateKindsNotRun(gate.Kinds, result.PerKind, "formation")
			return e.routeGateEvaluation(runID, board, gates, gate, input, "fail", result, ready, queued, queue)
		}
	}
	if hasGateKind(gate.Kinds, "human") {
		if err := e.store.AppendRunEvent(runID, RunEvent{
			Type:   RunEventHumanInputRequested,
			GateID: gate.ID,
			NodeID: gate.ID,
			Data: map[string]any{
				"gateId":         gate.ID,
				"nodeId":         gate.ID,
				"prompt":         gate.Criterion,
				"choices":        []string{"pass", "fail"},
				"requestedBy":    gate.ID,
				"inputRef":       input,
				"codeVerdict":    result.CodeVerdict,
				"codeReason":     result.CodeReason,
				"codePerKind":    result.PerKind,
				"codeResultSeq":  result.KindResultSeqs["code"],
				"kindResultSeqs": result.KindResultSeqs,
				"evidence":       result.Evidence,
				"resultEncoding": result.ResultEncoding,
				"resultSha256":   result.ResultSHA256,
				"gateBindingId":  result.GateBindingID,
			},
		}); err != nil {
			return err
		}
		// Only this path waits for the verdict; the run goes on with every
		// other branch (archon-o7p.11).
		return nil
	}
	return e.routeGateEvaluation(runID, board, gates, gate, input, "pass", result, ready, queued, queue)
}

func (e *RunEngine) appendGateKindResult(runID string, gate GateNode, kind string, input RunInputRef, result GateEvaluationResult) (int, error) {
	data := map[string]any{
		"kind":           kind,
		"verdict":        normalizeGateVerdict(result.Verdict),
		"reason":         result.Reason,
		"evidence":       result.Evidence,
		"resultEncoding": result.ResultEncoding,
		"resultSha256":   result.ResultSHA256,
		"gateBindingId":  result.GateBindingID,
		"inputRef":       input,
	}
	if kind == "code" {
		binding, err := e.store.readRunGateBinding(runID, gate.ID)
		if err != nil {
			return 0, err
		}
		data["inputSha256"] = codeGateSHA256(input.Text)
		data["profileId"] = binding.ProfileID
		data["profileVersion"] = binding.ProfileVersion
		data["profileSha256"] = binding.ProfileSHA256
		data["evaluatorBundleSha256"] = binding.EvaluatorBundleSHA256
		data["parametersSha256"] = binding.ParametersSHA256
		data["policySha256"] = binding.PolicySHA256
		data["determinismPolicySha256"] = binding.DeterminismPolicySHA256
		data["maxInputBytes"] = binding.MaxInputBytes
		data["maxResultBytes"] = binding.MaxResultBytes
		data["maxOperations"] = binding.MaxOperations
	}
	return e.store.appendRunEventSeq(runID, RunEvent{
		Type:   RunEventGateKindResult,
		GateID: gate.ID,
		NodeID: gate.ID,
		Data:   data,
	})
}

func markLaterGateKindsNotRun(kinds []string, perKind map[string]string, failedKind string) {
	afterFailure := false
	for _, kind := range []string{"code", "formation", "human"} {
		if kind == failedKind {
			afterFailure = true
			continue
		}
		if afterFailure && hasGateKind(kinds, kind) {
			perKind[kind] = "not_run"
		}
	}
}

func (e *RunEngine) routeGateEvaluation(runID string, board *BoardDocument, gates map[string]GateNode, gate GateNode, input RunInputRef, verdict string, result GateEvaluationResult, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	attempt, err := e.gateAttempt(runID, gate.ID)
	if err != nil {
		return err
	}
	routePort := verdict
	routes := outgoingConnectionsFromPort(board.Connections, gate.ID, routePort)
	if err := e.store.AppendRunEvent(runID, RunEvent{
		Type:    RunEventGateVerdict,
		Attempt: attempt,
		GateID:  gate.ID,
		NodeID:  gate.ID,
		Data: map[string]any{
			"verdict":        verdict,
			"perKind":        result.PerKind,
			"kindResultSeqs": result.KindResultSeqs,
			"codeResultSeq":  result.KindResultSeqs["code"],
			"codeVerdict":    result.CodeVerdict,
			"codeReason":     result.CodeReason,
			"routePort":      routePort,
			"routedEdges":    connectionIDs(routes),
			"reason":         result.Reason,
			"evidence":       result.Evidence,
			"resultEncoding": result.ResultEncoding,
			"resultSha256":   result.ResultSHA256,
			"gateBindingId":  result.GateBindingID,
			"inputRef":       input,
		},
	}); err != nil {
		return err
	}
	for _, route := range routes {
		nextInput := input
		if verdict == "fail" {
			nextInput = gateFailInput(runID, route, gate.ID, attempt, input, result.Reason, result.Evidence)
		}
		if err := e.deliverConnection(runID, board, gates, route, nextInput, ready, queued, queue); err != nil {
			return err
		}
	}
	return nil
}

// unroutedHumanVerdicts lists, oldest first, the recorded human verdicts the
// run has not routed yet: each human_verdict_recorded whose request no
// gate_verdict names.
func unroutedHumanVerdicts(events []RunEvent) []RunEvent {
	routed := map[int]bool{}
	for _, event := range events {
		if event.Type == RunEventGateVerdict {
			if seq := intFromRunEventData(event.Data["requestedSeq"]); seq > 0 {
				routed[seq] = true
			}
		}
	}
	var verdicts []RunEvent
	for _, event := range events {
		if event.Type == RunEventHumanVerdictRecorded && !routed[intFromRunEventData(event.Data["requestedSeq"])] {
			verdicts = append(verdicts, event)
		}
	}
	return verdicts
}

// routeRecordedVerdicts routes every recorded human verdict the run has not
// routed yet, oldest first, as a code or judge verdict routes. The run's
// worker calls it between steps, so a verdict recorded while a seat worked is
// routed once that step has recorded its output (archon-o7p.11).
func (e *RunEngine) routeRecordedVerdicts(runID string, board *BoardDocument, gates map[string]GateNode, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	for {
		events, err := e.store.ReadRunEvents(runID)
		if err != nil {
			return err
		}
		pending := unroutedHumanVerdicts(events)
		if len(pending) == 0 {
			return nil
		}
		if err := e.routeHumanVerdict(runID, board, gates, events, pending[0], ready, queued, queue); err != nil {
			return err
		}
	}
}

// routeHumanVerdict records the gate verdict for one recorded human verdict
// and delivers its routes: a pass carries the request's input with the
// operator's response, a fail sends that input back as feedback.
func (e *RunEngine) routeHumanVerdict(runID string, board *BoardDocument, gates map[string]GateNode, events []RunEvent, recorded RunEvent, ready map[string]map[string]RunInputRef, queued map[string]bool, queue *[]string) error {
	gate, ok := gates[recorded.GateID]
	if !ok {
		return fmt.Errorf("%w: gate %q", ErrNotFound, recorded.GateID)
	}
	requestedSeq := intFromRunEventData(recorded.Data["requestedSeq"])
	if requestedSeq <= 0 || requestedSeq >= recorded.Seq || events[requestedSeq-1].Type != RunEventHumanInputRequested || events[requestedSeq-1].GateID != gate.ID {
		return fmt.Errorf("%w: Gate %q verdict names invalid human request %d", ErrRunLedgerInvalid, gate.ID, requestedSeq)
	}
	request := events[requestedSeq-1]
	// The verdict answers the evaluation that asked, even when the gate has
	// evaluated again since.
	attempt := 0
	for _, event := range events[:requestedSeq] {
		if event.Type == RunEventGateEvaluating && event.GateID == gate.ID {
			attempt++
		}
	}
	verdict := normalizeGateVerdict(stringFromEventData(recorded, "verdict"))
	reason := stringFromEventData(recorded, "reason")
	input := runInputRefFromAny(request.Data["inputRef"])
	routes := outgoingConnectionsFromPort(board.Connections, gate.ID, verdict)
	result := gateResultFromHumanRequest(request, verdict, reason)
	verdictSeq, err := e.store.appendRunEventSeq(runID, RunEvent{
		Type:    RunEventGateVerdict,
		Attempt: attempt,
		GateID:  gate.ID,
		NodeID:  gate.ID,
		Data: map[string]any{
			"verdict":        verdict,
			"perKind":        result.PerKind,
			"kindResultSeqs": result.KindResultSeqs,
			"codeResultSeq":  result.KindResultSeqs["code"],
			"codeVerdict":    result.CodeVerdict,
			"codeReason":     result.CodeReason,
			"routePort":      verdict,
			"routedEdges":    connectionIDs(routes),
			"reason":         reason,
			"evidence":       result.Evidence,
			"resultEncoding": result.ResultEncoding,
			"resultSha256":   result.ResultSHA256,
			"gateBindingId":  result.GateBindingID,
			"inputRef":       input,
			"requestedSeq":   requestedSeq,
		},
	})
	if err != nil {
		return err
	}
	if events, err = e.store.ReadRunEvents(runID); err != nil {
		return err
	}
	verdictEvent := events[verdictSeq-1]
	for _, route := range routes {
		next := input
		if verdict == "fail" {
			next = gateFailInput(runID, route, gate.ID, attempt, input, reason, result.Evidence)
		} else if next, err = gatePassInput(events, verdictEvent, gate.ID, input); err != nil {
			return err
		}
		if err := e.deliverConnection(runID, board, gates, route, next, ready, queued, queue); err != nil {
			return err
		}
	}
	return nil
}

func (e *RunEngine) evaluateCodeGateResult(req GateEvaluation) (GateEvaluationResult, error) {
	if e.gateEvaluator == nil {
		if err := e.appendGateErrorAndBlock(req.RunID, req.GateID, "missing_gate_evaluator", "gate evaluator unavailable", "gate", "gate evaluator unavailable"); err != nil {
			return GateEvaluationResult{}, err
		}
		return GateEvaluationResult{}, errRunStopped
	}
	if strings.TrimSpace(req.Check) != "" || strings.TrimSpace(req.CheckVersion) != "" {
		binding, err := e.store.readRunGateBinding(req.RunID, req.GateID)
		if err != nil {
			if blockErr := e.appendGateErrorAndBlock(req.RunID, req.GateID, "gate_evaluator_error", err.Error(), "gate", "gate evaluator error"); blockErr != nil {
				return GateEvaluationResult{}, blockErr
			}
			return GateEvaluationResult{}, errRunStopped
		}
		req.Binding = binding
	}
	result, err := callGateEvaluator(e.gateEvaluator, req)
	if err != nil {
		if blockErr := e.appendGateErrorAndBlock(req.RunID, req.GateID, "gate_evaluator_error", err.Error(), "gate", "gate evaluator error"); blockErr != nil {
			return GateEvaluationResult{}, blockErr
		}
		return GateEvaluationResult{}, errRunStopped
	}
	if err := validateCodeGateEvaluationResult(req, result); err != nil {
		if blockErr := e.appendGateErrorAndBlock(req.RunID, req.GateID, "gate_evaluator_error", err.Error(), "gate", "gate evaluator error"); blockErr != nil {
			return GateEvaluationResult{}, blockErr
		}
		return GateEvaluationResult{}, errRunStopped
	}
	return result, nil
}

func validateCodeGateEvaluationResult(req GateEvaluation, result GateEvaluationResult) error {
	if result.Verdict != "pass" && result.Verdict != "fail" {
		return fmt.Errorf("gate %q evaluator returned invalid verdict %q", req.GateID, result.Verdict)
	}
	if req.Binding == nil || result.GateBindingID != req.Binding.GateBindingID {
		return fmt.Errorf("gate %q evaluator result binding mismatch", req.GateID)
	}
	if result.ResultEncoding != CodeGateResultEncoding {
		return fmt.Errorf("gate %q evaluator result encoding mismatch", req.GateID)
	}
	canonical, err := canonicalCodeGateResult(result.Verdict, result.Reason, result.Evidence)
	if err != nil {
		return fmt.Errorf("gate %q canonical result: %w", req.GateID, err)
	}
	if result.CanonicalResult != canonical {
		return fmt.Errorf("gate %q evaluator canonical result mismatch", req.GateID)
	}
	if result.ResultSHA256 != codeGateSHA256(canonical) {
		return fmt.Errorf("gate %q evaluator result hash mismatch", req.GateID)
	}
	return nil
}

func gateResultFromHumanRequest(request RunEvent, verdict, reason string) GateEvaluationResult {
	perKind := stringMapFromRunEventData(request.Data["codePerKind"])
	perKind["human"] = verdict
	return GateEvaluationResult{
		Verdict:        verdict,
		Reason:         reason,
		Evidence:       gateEvidenceRefsFromRunEventData(request.Data["evidence"]),
		PerKind:        perKind,
		KindResultSeqs: intMapFromRunEventData(request.Data["kindResultSeqs"]),
		CodeVerdict:    stringFromAny(request.Data["codeVerdict"]),
		CodeReason:     stringFromAny(request.Data["codeReason"]),
		ResultEncoding: stringFromAny(request.Data["resultEncoding"]),
		ResultSHA256:   stringFromAny(request.Data["resultSha256"]),
		GateBindingID:  stringFromAny(request.Data["gateBindingId"]),
	}
}

func intMapFromRunEventData(value any) map[string]int {
	result := map[string]int{}
	switch raw := value.(type) {
	case map[string]int:
		for key, item := range raw {
			result[key] = item
		}
	case map[string]any:
		for key, item := range raw {
			result[key] = intFromRunEventData(item)
		}
	}
	return result
}

func stringMapFromRunEventData(value any) map[string]string {
	result := map[string]string{}
	switch raw := value.(type) {
	case map[string]string:
		for key, item := range raw {
			result[key] = item
		}
	case map[string]any:
		for key, item := range raw {
			if text, ok := item.(string); ok {
				result[key] = text
			}
		}
	}
	return result
}

func gateEvidenceRefsFromRunEventData(value any) []GateEvidenceRef {
	switch raw := value.(type) {
	case []GateEvidenceRef:
		return append([]GateEvidenceRef(nil), raw...)
	case []any:
		result := make([]GateEvidenceRef, 0, len(raw))
		for _, item := range raw {
			fields, ok := item.(map[string]any)
			if !ok {
				continue
			}
			result = append(result, GateEvidenceRef{
				Kind: stringFromAny(fields["kind"]),
				Text: stringFromAny(fields["text"]),
			})
		}
		return result
	default:
		return nil
	}
}

func callGateEvaluator(evaluator GateEvaluator, req GateEvaluation) (result GateEvaluationResult, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = GateEvaluationResult{}
			err = fmt.Errorf("gate evaluator panic: %v", recovered)
		}
	}()
	return evaluator.EvaluateGate(req)
}

// runJudgeChain runs the gate's judges in order, each judging the previous
// judge's output, and returns the last judge's text. A judge that already
// produced output for this evaluation, after the gate's latest
// gate_evaluating, is not run again: resuming after a crash inside the chain
// continues from the first judge without output (archon-n7u.57).
func (e *RunEngine) runJudgeChain(board *BoardDocument, req GateEvaluation, chain []FormationNode) (string, error) {
	events, err := e.store.ReadRunEvents(req.RunID)
	if err != nil {
		return "", err
	}
	attempt, evaluating := 0, 0
	for _, event := range events {
		if event.Type == RunEventGateEvaluating && event.GateID == req.GateID {
			attempt, evaluating = attempt+1, event.Seq
		}
	}
	answered := map[string]RunEvent{}
	for _, event := range events[evaluating:] {
		if _, seen := answered[event.NodeID]; event.Type == RunEventNodeOutput && !seen {
			answered[event.NodeID] = event
		}
	}
	input := req.Input
	var finalText string
	replaying := true
	for _, formation := range chain {
		if len(formation.Outputs) == 0 {
			return "", fmt.Errorf("%w: judge formation %q has no output port", ErrConflict, formation.ID)
		}
		fromPortID := formation.Outputs[0].ID
		var payload FormationOutputPayload
		if output, ok := answered[formation.ID]; ok && replaying {
			finalText = stringFromEventData(output, "text")
			payload, _ = outputPayloadForPortFromEvent(output, fromPortID)
		} else {
			replaying = false
			if err := e.startFormationExecution(req.RunID, board, formation, RunEvent{
				Type:    RunEventNodeStarted,
				NodeID:  formation.ID,
				Attempt: attempt,
				Data: map[string]any{
					"nodeKind":  "formation",
					"inputRefs": []RunInputRef{input},
					"reason":    "judge",
					"brief":     formationBriefEventData(formationBriefValue(formation)),
				},
			}); err != nil {
				return "", err
			}
			result, err := e.executeFormation(FormationExecution{
				RunID:     req.RunID,
				NodeID:    formation.ID,
				Title:     formation.Title,
				Formation: formation,
				Brief:     formationBriefValue(formation),
				Inputs:    []RunInputRef{input},
				Attempt:   attempt,
			})
			if err != nil {
				if blockErr := e.appendExecutionFailureAndBlock(req.RunID, formation.ID, err); blockErr != nil {
					return "", blockErr
				}
				return "", errRunStopped
			}
			if result.Status == "" {
				result.Status = "done"
			}
			if err := e.ensureFormationOutputPayloads(req.RunID, formation, result); err != nil {
				return "", err
			}
			data := formationOutputEventData(result)
			data["reason"] = "judge"
			if err := e.store.AppendRunEvent(req.RunID, RunEvent{
				Type:   RunEventNodeOutput,
				NodeID: formation.ID,
				Data:   data,
			}); err != nil {
				return "", err
			}
			finalText = result.Text
			payload = result.Outputs[fromPortID]
		}
		input = RunInputRef{
			FromNodeID:  formation.ID,
			FromPortID:  fromPortID,
			Ref:         payload.Ref,
			Text:        payload.Text,
			ReportRef:   payload.ReportRef,
			ArtifactRef: payload.ArtifactRef,
		}
		if input.Ref == "" {
			input.Ref = fmt.Sprintf("ledger://%s/%s", req.RunID, formation.ID)
		}
	}
	return finalText, nil
}

func (e *RunEngine) appendErrorAndBlock(runID, code, message, boundary, nodeID, blockReason string) error {
	return e.appendErrorAndBlockWithDetails(runID, code, message, boundary, nodeID, blockReason, "", "")
}

func (e *RunEngine) appendErrorAndBlockWithDetails(runID, code, message, boundary, nodeID, blockReason, slotID, dispatchID string) error {
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
	if err := e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventError,
		NodeID: nodeID,
		SlotID: slotID,
		Data:   data,
	}); err != nil {
		return err
	}
	// The block carries its own code: other writers may append between the
	// error and the block (archon-o7p.11).
	block := runBlockedEvent(blockReason, nodeID, "", slotID, openDispatchesForBlock(nodeID, slotID, dispatchID))
	block.Data["code"] = code
	return e.store.AppendRunEvent(runID, block)
}

func (e *RunEngine) appendGateErrorAndBlock(runID, gateID, code, message, boundary, blockReason string) error {
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
		return err
	}
	block := runBlockedEvent(blockReason, "", gateID, "", nil)
	block.Data["code"] = code
	return e.store.AppendRunEvent(runID, block)
}

type executionFailureDetails struct {
	Code       string
	Message    string
	Boundary   string
	NodeID     string
	SlotID     string
	DispatchID string
}

func (e *RunEngine) appendExecutionFailureAndBlock(runID, nodeID string, err error) error {
	var limit *LimitReachedError
	if errors.As(err, &limit) {
		board, readErr := e.readRunBoard(runID)
		if readErr != nil {
			return readErr
		}
		return e.appendLimitBlock(runID, board, nodeID, limit.Use)
	}
	if errors.Is(err, ErrCoordinatorShutdown) {
		events, readErr := e.store.ReadRunEvents(runID)
		if readErr != nil {
			return readErr
		}
		return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventBlocked, NodeID: nodeID, Data: map[string]any{
			"reason": "coordinator shutdown; inspect dispatch evidence before resume", "resumeAllowed": true, "openDispatches": unresolvedDispatches(events),
		}})
	}
	failure := executionFailureEvent(err)
	if failure.NodeID != "" {
		nodeID = failure.NodeID
	}
	return e.appendErrorAndBlockWithDetails(runID, failure.Code, failure.Message, failure.Boundary, nodeID, failure.Message, failure.SlotID, failure.DispatchID)
}

func executionFailureEvent(err error) executionFailureDetails {
	var executionErr *RunExecutionError
	if errors.As(err, &executionErr) {
		boundary := executionErr.Boundary
		if boundary == "" {
			boundary = "executor"
		}
		return executionFailureDetails{
			Code:       executionErr.Code,
			Message:    executionErr.Message,
			Boundary:   boundary,
			NodeID:     executionErr.NodeID,
			SlotID:     executionErr.SlotID,
			DispatchID: executionErr.DispatchID,
		}
	}
	switch {
	case errors.Is(err, ErrRunExecutorUnavailable):
		return executionFailureDetails{Code: "missing_executor", Message: "formation executor unavailable", Boundary: "executor"}
	default:
		if err == nil {
			return executionFailureDetails{Code: "executor_failed", Message: "formation executor failed", Boundary: "executor"}
		}
		return executionFailureDetails{Code: "executor_failed", Message: redactLedgerText(err.Error()), Boundary: "executor"}
	}
}

func runBlockedEvent(reason, nodeID, gateID, slotID string, openDispatches []map[string]any) RunEvent {
	if openDispatches == nil {
		openDispatches = []map[string]any{}
	}
	return RunEvent{
		Type:   RunEventBlocked,
		NodeID: nodeID,
		SlotID: slotID,
		GateID: gateID,
		Data: map[string]any{
			"reason":         redactLedgerText(reason),
			"blockedNodeId":  nodeID,
			"blockedGateId":  gateID,
			"resumeAllowed":  true,
			"resumePolicy":   "explicit",
			"openDispatches": openDispatches,
			"nextEpoch":      1,
		},
	}
}

func openDispatchesForBlock(nodeID, slotID, dispatchID string) []map[string]any {
	if dispatchID == "" {
		return nil
	}
	return []map[string]any{{
		"dispatchId": dispatchID,
		"nodeId":     nodeID,
		"slotId":     slotID,
	}}
}

type starvedFormation struct {
	ID      string
	Title   string
	Missing []string
}

// appendUnfinishedWorkBlock refuses success while unfinishedRunWork names
// nodes: the run blocks, resumable, naming them, rather than claim success
// with work still owed (archon-n7u.53).
func (e *RunEngine) appendUnfinishedWorkBlock(runID string, unfinished []string) error {
	message := fmt.Sprintf("run has unfinished work at %v; resume to continue it", unfinished)
	return e.appendErrorAndBlock(runID, "run_work_unfinished", message, "engine", unfinished[0], message)
}

// appendStarvedBlock records a fail-loud run_blocked instead of run_succeeded
// when reachable required formations can never run. Resume cannot conjure a
// missing producer, so the run is blocked non-resumably with recovery guidance:
// wire a producer to the starved ports and start a new run.
func (e *RunEngine) appendStarvedBlock(runID string, starved []starvedFormation) error {
	waitingNodes := make([]map[string]any, 0, len(starved))
	for _, s := range starved {
		waitingNodes = append(waitingNodes, map[string]any{
			"nodeId":        s.ID,
			"title":         s.Title,
			"missingInputs": s.Missing,
		})
	}
	primary := starved[0]
	reason := fmt.Sprintf("formation %q is waiting on inputs %v that no upstream node produces; wire a producer to those ports and start a new run", primary.ID, primary.Missing)
	return e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventBlocked,
		NodeID: primary.ID,
		Data: map[string]any{
			"reason":         reason,
			"code":           "reachable_node_starved",
			"blockedNodeId":  primary.ID,
			"blockedGateId":  "",
			"boundary":       "wiring",
			"waitingNodes":   waitingNodes,
			"recoverable":    false,
			"resumeAllowed":  false,
			"resumePolicy":   "authoring",
			"openDispatches": []map[string]any{},
			"nextEpoch":      1,
		},
	})
}

func intFromRunEventData(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// latestHumanRequest is the gate's request still waiting for a verdict, by
// the one rule (OpenHumanRequests).
func latestHumanRequest(events []RunEvent, gateID string) (RunEvent, bool) {
	for _, request := range OpenHumanRequests(events) {
		if request.GateID == gateID {
			return request, true
		}
	}
	return RunEvent{}, false
}

func runInputRefFromAny(value any) RunInputRef {
	raw, ok := value.(map[string]any)
	if !ok {
		return RunInputRef{}
	}
	var feedback *GateFeedback
	if fields, ok := raw["feedback"].(map[string]any); ok {
		feedback = &GateFeedback{
			GateID: stringFromAny(fields["gateId"]), GateAttempt: intFromRunEventData(fields["gateAttempt"]),
			Verdict: stringFromAny(fields["verdict"]), Reason: stringFromAny(fields["reason"]),
			Evidence:    gateEvidenceRefsFromRunEventData(fields["evidence"]),
			OriginalRef: stringFromAny(fields["originalRef"]), OriginalText: stringFromAny(fields["originalText"]),
		}
	}
	var response *GateResponse
	if fields, ok := raw["response"].(map[string]any); ok {
		response = &GateResponse{
			GateID: stringFromAny(fields["gateId"]), GateAttempt: intFromRunEventData(fields["gateAttempt"]),
			RequestedSeq: intFromRunEventData(fields["requestedSeq"]),
			DecidedBy:    stringFromAny(fields["decidedBy"]), Text: stringFromAny(fields["text"]),
		}
	}
	return RunInputRef{
		Feedback:    feedback,
		Response:    response,
		EdgeID:      stringFromAny(raw["edgeId"]),
		FromNodeID:  stringFromAny(raw["fromNodeId"]),
		FromPortID:  stringFromAny(raw["fromPortId"]),
		ToPortID:    stringFromAny(raw["toPortId"]),
		OutputSeq:   intFromRunEventData(raw["outputSeq"]),
		Ref:         stringFromAny(raw["ref"]),
		Text:        stringFromAny(raw["text"]),
		ReportRef:   stringFromAny(raw["reportRef"]),
		ArtifactRef: stringFromAny(raw["artifactRef"]),
	}
}

func hasGateKind(kinds []string, want string) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}

func withoutGateKind(kinds []string, without string) []string {
	filtered := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		if kind != without {
			filtered = append(filtered, kind)
		}
	}
	return filtered
}

func findGate(gates []GateNode, id string) (GateNode, bool) {
	for _, gate := range gates {
		if gate.ID == id {
			return gate, true
		}
	}
	return GateNode{}, false
}

func formationBriefValue(formation FormationNode) FormationBrief {
	if formation.Brief == nil {
		return FormationBrief{}
	}
	return FormationBrief{
		Goal:   formation.Brief.Goal,
		BeadID: formation.Brief.BeadID,
		Files:  append([]string(nil), formation.Brief.Files...),
		Links:  append([]string(nil), formation.Brief.Links...),
	}
}

func formationBriefEventData(brief FormationBrief) map[string]any {
	return map[string]any{
		"goal":   brief.Goal,
		"beadId": brief.BeadID,
		"files":  append([]string(nil), brief.Files...),
		"links":  append([]string(nil), brief.Links...),
	}
}

func normalizeGateVerdict(verdict string) string {
	if verdict == "fail" {
		return "fail"
	}
	return "pass"
}

func connectionIDs(connections []BoardConnection) []string {
	ids := make([]string, 0, len(connections))
	for _, connection := range connections {
		ids = append(ids, connection.ID)
	}
	return ids
}

func formationIDs(formations []FormationNode) []string {
	ids := make([]string, 0, len(formations))
	for _, formation := range formations {
		ids = append(ids, formation.ID)
	}
	return ids
}

// judgeChainForGate is the gate's judge chain: the steps wired from its judge
// port, each to the next, and back to that port (archon-n7u.51). From the step
// the judge port feeds, each step either returns to the port, which completes
// the chain, or hands on to the first step it feeds. A chain that leaves the
// steps, loops or never returns is no chain, so validation names the gate
// incomplete; the cockpit's judgeChain follows the same rule
// (testdata/judge_chains.json).
func judgeChainForGate(board *BoardDocument, gateID string) []FormationNode {
	entries := outgoingConnectionsFromPort(board.Connections, gateID, "judge")
	if len(entries) == 0 {
		return nil
	}
	formationByID := map[string]FormationNode{}
	for _, formation := range board.Formations {
		formationByID[formation.ID] = formation
	}
	currentNode, _ := endpointParts(entries[0].To)
	visited := map[string]bool{}
	var chain []FormationNode
	for currentNode != "" && !visited[currentNode] {
		visited[currentNode] = true
		formation, ok := formationByID[currentNode]
		if !ok {
			return nil
		}
		chain = append(chain, formation)
		var nextNode string
		for _, connection := range outgoingConnections(board.Connections, currentNode) {
			toNode, toPort := endpointParts(connection.To)
			if toNode == gateID && toPort == "judge" {
				return chain
			}
			if _, ok := formationByID[toNode]; ok && nextNode == "" {
				nextNode = toNode
			}
		}
		currentNode = nextNode
	}
	return nil
}

func outgoingConnectionsFromPort(connections []BoardConnection, nodeID, portID string) []BoardConnection {
	var out []BoardConnection
	for _, connection := range connections {
		fromNode, fromPort := endpointParts(connection.From)
		if fromNode == nodeID && fromPort == portID {
			out = append(out, connection)
		}
	}
	return out
}

func (e *RunEngine) appendWaiting(runID string, formation FormationNode, ready map[string]RunInputRef) error {
	waitingFor := make([]string, 0, len(formation.Inputs))
	for _, input := range formation.Inputs {
		if _, ok := ready[input.ID]; !ok {
			waitingFor = append(waitingFor, input.ID)
		}
	}
	return e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventNodeWaiting,
		NodeID: formation.ID,
		Data: map[string]any{
			"neededInputs": len(waitingFor),
			"readyInputs":  len(ready),
			"totalInputs":  len(formation.Inputs),
			"waitingFor":   waitingFor,
		},
	})
}

func outgoingConnections(connections []BoardConnection, nodeID string) []BoardConnection {
	var out []BoardConnection
	for _, connection := range connections {
		fromNode, _ := endpointParts(connection.From)
		if fromNode == nodeID {
			out = append(out, connection)
		}
	}
	return out
}

func endpointParts(endpoint string) (string, string) {
	parts := strings.SplitN(endpoint, ":", 2)
	if len(parts) != 2 {
		return endpoint, ""
	}
	return parts[0], parts[1]
}

func findFormation(formations []FormationNode, id string) (FormationNode, bool) {
	for _, formation := range formations {
		if formation.ID == id {
			return formation, true
		}
	}
	return FormationNode{}, false
}

func formationReady(formation FormationNode, ready map[string]RunInputRef) bool {
	if len(formation.Inputs) == 0 {
		return true
	}
	if len(ready) < len(formation.Inputs) {
		return false
	}
	for _, input := range formation.Inputs {
		if _, ok := ready[input.ID]; !ok {
			return false
		}
	}
	return true
}

func orderedInputs(formation FormationNode, ready map[string]RunInputRef) []RunInputRef {
	inputs := make([]RunInputRef, 0, len(formation.Inputs))
	for _, input := range formation.Inputs {
		if ref, ok := ready[input.ID]; ok {
			inputs = append(inputs, ref)
		}
	}
	return inputs
}
