package formations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

var (
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
	// watchers wake each run's worker when a verdict or pick is recorded
	// while it works (watchRun).
	watchMu  sync.Mutex
	watchers map[string]chan struct{}
	// keptSeats serializes reconsidering a run's kept seats, which its
	// concurrent steps do as each starts dispatching.
	keptSeats runLocks
	// tokenPools hold each working run's live token counts (tokenPool).
	tokenPools runTokenPools
}

// FormationRunRequest starts one step on its own. It takes the run fields a
// mission run takes: cwd (empty allocates a workspace), context paths, Bead
// and the mission's inputs.
type FormationRunRequest struct {
	ExpectedBoardRev  int
	ExpectedBoardETag string
	Actor             string
	Personas          *PersonaStore
	Cwd               string
	ContextPaths      []string
	BeadID            string
	// Inputs, when not nil, must pass the mission's input checks (archon-o7p.3).
	Inputs map[string]string
}

type FormationExecution struct {
	// JudgeGateID adds the verdict contract; FinalJudge validates the chain result.
	JudgeGateID string
	FinalJudge  bool
	// PickAttempts, on a judge, asks it to pick the best of these attempts
	// instead of judging: the pick contract replaces the verdict contract
	// (archon-gihe).
	PickAttempts []int
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
	// ArtifactDir is the attempt's own artifact directory, absolute and
	// created before dispatch: .archon/artifacts/<run>/<node>/<attempt>.
	ArtifactDir string
	// KeepSeatsOnCall keeps the formation's seats running when it finishes,
	// because its work reaches a human gate on a session-channel run.
	KeepSeatsOnCall bool
	// PeerMessages is a peer step's Limit card rounds, its journal messages:
	// used in earlier attempts of Max. Nil when no card caps the step.
	PeerMessages *RunLimitReached
	// Warnings are the time cards' warnings for this attempt's seats.
	Warnings []LimitWarning
	// TokenBudget is how many tokens the attempt may spend before its seats
	// stop, from the Limit card covering it with the fewest left; 0 when no
	// card sets tokens. An executor that has a budget records each dispatch's
	// token_usage and reports ErrTokenBudgetSpent when it stops the seats.
	TokenBudget int
	// tokens is the attempt's live count, kept by the executor.
	tokens *tokenMeter
	// tokenPool is the run's live token counts, which every attempt working
	// at once adds to, when the mission's card sets tokens.
	tokenPool *tokenPool
	// ctx, when set, is the context the attempt runs in: the run's worker
	// gives each job its own, within the run's.
	ctx context.Context
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
	// Kind is file or folder for a file or folder port, whose Ref is then the
	// accepted snapshot's path; blank for text.
	Kind string `json:"kind,omitempty"`
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

// slotIDPattern is what a slot ID may be, wherever it is written or named: a
// letter or digit, then up to 63 letters, digits, underscores or hyphens. Its
// seat's session is named after it and a relayed verdict names it
// (archon-1ds).
var slotIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// slotIDRule says what a slot ID may be, for messages.
const slotIDRule = "a letter or digit, then up to 63 letters, digits, underscores or hyphens"

// ValidSlotID reports whether id is a slot ID Archon can use.
func ValidSlotID(id string) bool {
	return slotIDPattern.MatchString(id)
}

// ValidateRelayedBy accepts no relay, or a slot ID.
func ValidateRelayedBy(slotID string) error {
	if slotID == "" || ValidSlotID(slotID) {
		return nil
	}
	return fmt.Errorf("%w: relayedBy %q must be a slot ID: %s", ErrInvalidRelayedBy, slotID, slotIDRule)
}

type RunInputRef struct {
	Feedback    *GateFeedback `json:"feedback,omitempty"`
	Response    *GateResponse `json:"response,omitempty"`
	EdgeID      string        `json:"edgeId,omitempty"`
	FromNodeID  string        `json:"fromNodeId,omitempty"`
	FromPortID  string        `json:"fromPortId,omitempty"`
	ToPortID    string        `json:"toPortId,omitempty"`
	Ref         string        `json:"ref,omitempty"`
	Text        string        `json:"text,omitempty"`
	ReportRef   string        `json:"reportRef,omitempty"`
	ArtifactRef string        `json:"artifactRef,omitempty"`
	// Kind is file or folder when Ref is the path of a file or folder output.
	Kind string `json:"kind,omitempty"`
	// Origin is the step output this delivery carries, which travels
	// unchanged through gates: a pass routes it as it came, and a fail
	// route's feedback names the output the gate rejected.
	Origin *OutputOrigin `json:"origin,omitempty"`
	// unread marks a delivery in a formation's port that the formation has
	// not yet run on (deliverToPort); it never leaves the engine.
	unread bool
}

// OutputOrigin names one output: the node and port that produced it and the
// attempt it came from, as node_output records it. The Input card's output
// is attempt 0.
type OutputOrigin struct {
	NodeID  string `json:"nodeId"`
	PortID  string `json:"portId"`
	Attempt int    `json:"attempt"`
}

func outputOriginFromAny(value any) *OutputOrigin {
	fields, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return &OutputOrigin{NodeID: stringFromAny(fields["nodeId"]), PortID: stringFromAny(fields["portId"]), Attempt: intFromRunEventData(fields["attempt"])}
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
	if err := ValidateRunContextPaths(req.ContextPaths); err != nil {
		return nil, nil, err
	}
	if req.Inputs != nil {
		if findings := runInputFindings(board, req.Inputs); len(findings) > 0 {
			return nil, nil, &RunAdmissionError{Findings: findings}
		}
	}
	personas := req.Personas
	if personas == nil {
		personas = e.personas
	}
	started, err := e.startFormationRun(slug, board, formation, personas, req)
	if err != nil {
		return nil, nil, err
	}
	return started, func() (*RunStatusProjection, error) {
		events, err := e.store.ReadRunEvents(started.RunID)
		if err != nil {
			return nil, err
		}
		if err := e.runSingleFormation(started.RunID, board, events, "single-formation"); err != nil {
			return nil, err
		}
		return e.projectAndNotify(started.RunID)
	}, nil
}

// runSingleFormation goes on with a single step's run: once the step's latest
// attempt has its output the run succeeds; otherwise the step runs as its next
// attempt on the run's inputs, as the run's first attempt did. Resume, a Limit
// card grant included, continues a blocked single-step run here
// (archon-y8br). reason is recorded on node_started.
func (e *RunEngine) runSingleFormation(runID string, board *BoardDocument, events []RunEvent, reason string) error {
	started := events[0]
	formationID := stringFromEventData(started, "formationId")
	formation, ok := findFormation(board.Formations, formationID)
	if !ok {
		return fmt.Errorf("%w: formation %q", ErrNotFound, formationID)
	}
	attempt, output := 0, false
	for _, event := range events {
		if event.NodeID != formation.ID {
			continue
		}
		switch event.Type {
		case RunEventNodeStarted:
			attempt, output = max(attempt, event.Attempt), false
		case RunEventNodeOutput:
			output = true
		}
	}
	if !output {
		attempt++
		seedInput := formationRunSeed(board, formation, RunInputsFromEventData(started.Data["inputs"]), stringFromEventData(started, "objective"))
		spent, err := e.startFormationUnlessSpent(runID, board, formation, RunEvent{
			Type:    RunEventNodeStarted,
			NodeID:  formation.ID,
			Attempt: attempt,
			Data: map[string]any{
				"nodeKind":  "formation",
				"inputRefs": []RunInputRef{seedInput},
				"reason":    reason,
				"brief":     formationBriefEventData(formationBriefValue(formation)),
			},
		})
		if err != nil {
			return err
		}
		if spent != nil {
			// A spent limit blocks the run, resumable only with a grant.
			return e.appendLimitBlock(runID, board, formation.ID, *spent)
		}
		result, err := e.executeFormation(FormationExecution{
			RunID:     runID,
			NodeID:    formation.ID,
			Title:     formation.Title,
			Formation: formation,
			Brief:     formationBriefValue(formation),
			Inputs:    []RunInputRef{seedInput},
			Attempt:   attempt,
		})
		if err != nil {
			return e.appendExecutionFailureAndBlock(runID, formation.ID, err)
		}
		if result.Status == "" {
			result.Status = "done"
		}
		if err := checkFormationOutputPayloads(formation, result); err != nil {
			return e.appendExecutionFailureAndBlock(runID, formation.ID, err)
		}
		if err := e.store.AppendRunEvent(runID, RunEvent{
			Type:    RunEventNodeOutput,
			NodeID:  formation.ID,
			Attempt: attempt,
			Data:    formationOutputEventData(result),
		}); err != nil {
			return err
		}
	}
	return e.store.AppendRunEvent(runID, RunEvent{
		Type: RunEventSucceeded,
		Data: map[string]any{
			"summaryRef":   "",
			"outputRefs":   []string{},
			"artifactRefs": []string{},
			"final":        true,
			"mode":         "formation",
			"formationId":  formation.ID,
			"inputCardId":  started.MissionID,
		},
	})
}

// formationRunSeed is what a single step's run hands its step: the run's
// inputs, as a mission's first step receives them, or the step's brief when
// the run supplied none.
func formationRunSeed(board *BoardDocument, formation FormationNode, inputs []RunInput, goal string) RunInputRef {
	seed := RenderRunInputs(board, inputs)
	if seed == "" {
		seed = goal
	}
	return RunInputRef{Ref: "brief://" + formation.ID, Text: seed}
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
	// This command appended the latest run_resumed; another writer, such as a
	// verdict, may have appended since, so the event is found by its type.
	var resumeEvent RunEvent
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == RunEventResumed {
			resumeEvent = events[i]
			break
		}
	}
	if resumeEvent.Seq == 0 {
		return nil, ErrRunLedgerInvalid
	}
	if recovered != nil {
		if err := NewSlotDispatcher(e.store, nil).CompleteFromCapture(runID, recoveredRef.DispatchID, recovered.Text); err != nil {
			if blockErr := e.appendExecutionFailureAndBlock(runID, recoveredRef.NodeID, err); blockErr != nil {
				return nil, blockErr
			}
			return nil, err
		}
		if err := e.store.AppendRunEvent(runID, RunEvent{Type: "seat_result_recovered", NodeID: recoveredRef.NodeID, SlotID: recoveredRef.SlotID, Data: map[string]any{"dispatchId": recoveredRef.DispatchID}}); err != nil {
			return nil, err
		}
		attempt := latestNodeAttempt(events, recoveredRef.NodeID)
		if formation, ok := findFormation(board.Formations, recoveredRef.NodeID); ok {
			kept, err := e.keepAttemptOutputs(runID, formation, attempt, isJudgeFormation(board, formation.ID), *recovered)
			if err != nil {
				if blockErr := e.appendExecutionFailureAndBlock(runID, formation.ID, err); blockErr != nil {
					return nil, blockErr
				}
				return e.projectAndNotify(runID)
			}
			recovered = &kept
		}
		if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventNodeOutput, NodeID: recoveredRef.NodeID, Attempt: attempt, Data: formationOutputEventData(*recovered)}); err != nil {
			return nil, err
		}
		events, err = e.store.ReadRunEvents(runID)
		if err != nil {
			return nil, err
		}
	} else if pending := pendingOutputFromEvent(resumeEvent); pending != nil && req.Mode != "redispatch" {
		// The attempt finished but its outputs were not kept: keep them now,
		// rather than run the step again.
		if stopped, err := e.keepPendingOutput(runID, board, *pending); err != nil || stopped {
			if err != nil {
				return nil, err
			}
			return e.projectAndNotify(runID)
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
			stopped, err := e.reattachOpenDispatches(runID, board, openDispatches)
			if err != nil {
				return nil, err
			}
			if stopped {
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
	if err := e.continueSnapshot(runID, board, events, "resume"); err != nil {
		return nil, err
	}
	return e.projectAndNotify(runID)
}

// continueSnapshot runs what a run still owes from its root: a mission's
// Input card, or a single step's run's step.
func (e *RunEngine) continueSnapshot(runID string, board *BoardDocument, events []RunEvent, reason string) error {
	started := events[0]
	if stringFromEventData(started, "mode") == "formation" {
		return e.runSingleFormation(runID, board, events, reason)
	}
	mission, ok := findMission(board, started.MissionID)
	if !ok {
		return fmt.Errorf("%w: mission %q", ErrNotFound, started.MissionID)
	}
	return e.resumeSnapshot(runID, board, mission, events, reason)
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
	if err := e.continueSnapshot(runID, board, events, "continue"); err != nil {
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
// (runScheduler), so a verdict is accepted while other seats work, and on a
// blocked run it waits for the resume (archon-o7p.11). The request
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
	if isPickEvent(requestEvent) {
		return nil, ErrHumanRequestIsAPick
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
	// A run working now routes the verdict at once, while its other steps
	// work (archon-1uyi).
	e.wakeRun(runID)
	return e.store.ProjectRun(runID)
}

func (e *RunEngine) validateHumanRequestKindResults(runID string, gate GateNode, request RunEvent, events []RunEvent) error {
	if isPickEvent(request) {
		// A pick asks after the gate judged every candidate (archon-gihe).
		return nil
	}
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

// reattachOpenDispatches takes up the completed turn of each open dispatch a
// block named, in the order they were sent, and records its step's output as
// the step would have when it finished. Steps that worked at once leave one
// each (archon-1uyi). A dispatch that cannot be taken up costs no other its
// output: the run blocks naming only those, for another resume or a
// redispatch. An output that cannot be kept stops the run as it would have
// when its step finished. It reports whether the run blocked.
func (e *RunEngine) reattachOpenDispatches(runID string, board *BoardDocument, refs []openDispatchRef) (bool, error) {
	reattacher, _ := e.executor.(FormationReattachExecutor)
	var failed []openDispatchRef
	var reasons []string
	var stop *runStop
	for _, ref := range refs {
		reason, kept, err := e.reattachOpenDispatch(runID, board, reattacher, ref)
		if err != nil {
			return false, err
		}
		if reason != "" {
			failed = append(failed, ref)
			reasons = append(reasons, reason)
		}
		if kept != nil && stop == nil {
			stop = kept
		}
	}
	if len(failed) > 0 {
		// A failed reattach keeps the run resumable; the reason is durable.
		reason := reasons[0]
		if len(failed) > 1 {
			for i := range reasons {
				reasons[i] = failed[i].NodeID + ": " + reasons[i]
			}
			reason = strings.Join(reasons, "; ")
		}
		return true, e.appendOpenDispatchReattachFailure(runID, failed, reason)
	}
	if stop != nil {
		return true, stop.leave(e, runID)
	}
	return false, nil
}

// reattachOpenDispatch takes up one open dispatch's completed turn. It returns
// why the dispatch could not be taken up, or the stop its step's output left
// when it could not be kept; err is a fault of the ledger.
func (e *RunEngine) reattachOpenDispatch(runID string, board *BoardDocument, reattacher FormationReattachExecutor, ref openDispatchRef) (string, *runStop, error) {
	if reattacher == nil {
		return "could not reattach open dispatch without live capture", nil, nil
	}
	formation, ok := findFormation(board.Formations, ref.NodeID)
	if !ok {
		return "the open dispatch's step is not in the run's mission", nil, nil
	}
	result, err := reattacher.ReattachFormationDispatch(FormationReattachRequest{
		RunID:      runID,
		DispatchID: ref.DispatchID,
		NodeID:     ref.NodeID,
		SlotID:     ref.SlotID,
		Formation:  formation,
	})
	if err != nil {
		return err.Error(), nil, nil
	}
	if result.Status == "" {
		result.Status = "done"
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return "", nil, err
	}
	attempt := latestNodeAttempt(events, formation.ID)
	if result, err = e.keepAttemptOutputs(runID, formation, attempt, isJudgeFormation(board, formation.ID), result); err == nil {
		err = checkFormationOutputPayloads(formation, result)
	}
	if err != nil {
		stop, err := e.stopFor(runID, formation.ID, err)
		if err != nil {
			return "", nil, err
		}
		return "", &stop, nil
	}
	return "", nil, e.store.AppendRunEvent(runID, RunEvent{
		Type:    RunEventNodeOutput,
		NodeID:  ref.NodeID,
		Attempt: attempt,
		Data:    formationOutputEventData(result),
	})
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

func (e *RunEngine) startFormationRun(slug string, board *BoardDocument, formation FormationNode, personas *PersonaStore, req FormationRunRequest) (result *RunStartResult, err error) {
	err = e.store.withRunStorageLock(func() error {
		var x error
		result, x = e.startFormationRunStorageLocked(slug, board, formation, personas, req)
		return x
	})
	return
}

func (e *RunEngine) startFormationRunStorageLocked(slug string, board *BoardDocument, formation FormationNode, personas *PersonaStore, req FormationRunRequest) (*RunStartResult, error) {
	bindings, err := resolveRunBindings(board, personas)
	if err != nil {
		return nil, err
	}
	goal := ""
	if formation.Brief != nil {
		goal = formation.Brief.Goal
	}
	inputs := ResolveRunInputs(board, req.Inputs)
	// The step's brief is the run's objective, with its {name} references
	// resolved as the brief itself is at dispatch.
	goal = SubstituteRunInputs(goal, MissionRunInputs(board), inputs, req.BeadID)
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
		return nil, fmt.Errorf("%w: run snapshot exceeds byte limit", ErrRunLedgerInvalid)
	}
	bindingsRaw := []byte(renderRunBindings(runID, board, mission, bindings, nil))
	if int64(len(bindingsRaw)) > runRecordMaxBytes {
		return nil, fmt.Errorf("%w: run persona snapshot exceeds byte limit", ErrRunLedgerInvalid)
	}
	runDirectory, err := e.store.openRunArtifactDirectory(slug, true)
	if err != nil {
		return nil, err
	}
	defer runDirectory.close()
	if err := writeRunArtifactExclusiveAt(runDirectory, runID+".snapshot.toml", boardRaw); err != nil {
		return nil, err
	}
	if err := writeRunArtifactExclusiveAt(runDirectory, runID+".bindings.toml", bindingsRaw); err != nil {
		return nil, err
	}
	cwd := req.Cwd
	if cwd == "" {
		if cwd, err = e.store.allocateRunWorkspace(runID); err != nil {
			return nil, err
		}
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
		Actor:     defaultRunActor(req.Actor),
		BoardID:   board.ID,
		BoardRev:  board.Rev,
		MissionID: mission.ID,
		BeadID:    req.BeadID,
		Epoch:     0,
		Attempt:   0,
		Data: map[string]any{
			"storagePolicy":    frozenRunStoragePolicy(),
			"missionSlug":      slug,
			"missionPath":      filepath.ToSlash(e.store.BoardPath(slug)),
			"missionRev":       board.Rev,
			"snapshot":         snapshotPath,
			"bindingsSnapshot": bindingsPath,
			"inputCardId":      mission.ID,
			"beadId":           req.BeadID,
			"objective":        mission.Goal,
			"cwd":              cwd,
			"contextPaths":     req.ContextPaths,
			"inputs":           inputs,
			"mode":             "formation",
			"formationId":      formation.ID,
		},
	}
	if err := writeInitialRunEventAt(runDirectory, runID, event); err != nil {
		if req.Cwd == "" {
			// Remove only our newly allocated empty folder, never existing contents.
			err = errors.Join(err, os.Remove(cwd))
		}
		return nil, err
	}
	return started, nil
}

// resumeSnapshot goes on with a mission run from its ledger (runScheduler).
// reason names why the run continues: resume after a block, or continue
// after a verdict; it is recorded on each node_started and on the run's end.
func (e *RunEngine) resumeSnapshot(runID string, board *BoardDocument, mission MissionNode, events []RunEvent, reason string) error {
	s := e.newRunScheduler(runID, board, reason, reason)
	return s.run(func(events []RunEvent) error {
		// A run interrupted before its Input card delivered the brief, its
		// first output, starts there.
		if !slices.ContainsFunc(events, func(event RunEvent) bool { return event.Type == RunEventNodeOutput }) {
			return e.deliverInputCard(runID, board, mission)
		}
		// A resume with --pick-best picks the stopped step's best attempt first.
		return s.pickOnResume(events)
	})
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
// otherwise the run succeeds, or is picked when its work reached the done End
// through a pick (ADR-0022). reason, when set, says how the run got here,
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
	// A run that reached a done End delivers the mission's declared outputs; a
	// required one it did not produce fails it (archon-5tg1).
	outputs, picks, missing := resolveMissionOutputs(board, events)
	if missing != "" {
		return e.store.AppendRunEvent(runID, RunEvent{
			Type:  RunEventFailed,
			Actor: RunFailureActor,
			Data: map[string]any{
				"code":   RunFailureMissingOutput,
				"reason": missing,
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
	if len(outputs) > 0 {
		data["outputs"] = outputs
	}
	if reason != "" {
		data["reason"] = reason
	}
	// A pick carries on failed work, so a run whose work reached its done End
	// through one is picked, not succeeded (ADR-0022).
	if len(picks) > 0 {
		data["picks"] = picks
		return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventPicked, Data: data})
	}
	return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventSucceeded, Data: data})
}

// eventGateID is the gate an event concerns.
func eventGateID(event RunEvent) string {
	if event.GateID != "" {
		return event.GateID
	}
	return event.NodeID
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
	events, err := s.readLedger(ledger, runID, false)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrRunLedgerInvalid
	}
	return s.readRunSnapshot(events[0], runID, ledger)
}

func (e *RunEngine) executeSnapshot(runID string, board *BoardDocument, mission MissionNode) error {
	return e.newRunScheduler(runID, board, "initial", "").run(func([]RunEvent) error {
		return e.deliverInputCard(runID, board, mission)
	})
}

// deliverInputCard records the Input card's output, the run's brief, which
// replay delivers to the steps it is wired to.
func (e *RunEngine) deliverInputCard(runID string, board *BoardDocument, mission MissionNode) error {
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
	if text := RenderRunInputs(board, RunInputsFromEventData(events[0].Data["inputs"])); text != "" {
		missionText = text
	}
	missionOutputs := map[string]FormationOutputPayload{
		"out": {Text: missionText},
	}
	return e.store.AppendRunEvent(runID, RunEvent{
		Type:      RunEventNodeOutput,
		NodeID:    mission.ID,
		MissionID: mission.ID,
		Data: formationOutputEventData(FormationExecutionResult{
			Status:  "done",
			Text:    missionText,
			Outputs: missionOutputs,
		}),
	})
}

// startFormationUnlessSpent records the step's start, or returns the limit
// starting it would pass and records nothing; its caller picks or blocks.
func (e *RunEngine) startFormationUnlessSpent(runID string, board *BoardDocument, formation FormationNode, event RunEvent) (*RunLimitReached, error) {
	started := e.store.now()
	event.Timestamp = started.Format(time.RFC3339Nano)
	// Judges start from their gates' jobs while the run's worker starts steps
	// (archon-1uyi), so the limits are read and the start recorded under the
	// ledger's lock: two starts never share a card's last round.
	var spent *RunLimitReached
	err := e.store.appendRunEventIf(runID, event, func(events []RunEvent) error {
		spent = limitSpentBefore(board, events, formation, started)
		if formation.Type == FormationTypePeer && spent == nil {
			// A peer step's rounds are its journal messages, posted in earlier attempts.
			if use := e.peerMessagesUse(board, events, FormationExecution{RunID: runID, NodeID: formation.ID, Attempt: event.Attempt}); use != nil && use.Used >= use.Max {
				spent = use
			}
		}
		if spent != nil {
			return errNothingToRecord
		}
		return nil
	})
	if errors.Is(err, errNothingToRecord) {
		return spent, nil
	}
	return nil, err
}

// executeFormation runs one attempt in its own artifact directory and accepts
// its output: file and folder outputs are checked and kept there.
func (e *RunEngine) executeFormation(req FormationExecution) (FormationExecutionResult, error) {
	if e.executor == nil {
		return FormationExecutionResult{}, ErrRunExecutorUnavailable
	}
	dir, err := e.store.createAttemptArtifactDir(req.RunID, req.NodeID, req.Attempt)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	req.ArtifactDir = dir
	result, err := e.dispatchFormation(req)
	if err != nil {
		return result, err
	}
	return e.keepAttemptOutputs(req.RunID, req.Formation, req.Attempt, req.JudgeGateID != "", result)
}

// keepAttemptOutputs accepts a finished attempt's result: each file and
// folder output is checked and kept as a read-only snapshot in the attempt's
// directory, which the result then names. A copy that fails, or that a cancel
// or shutdown interrupts, leaves the result pending (pendingOutputError) so a
// resume keeps it without running the step again. The copy runs after the
// step's seats, outside its time limit.
func (e *RunEngine) keepAttemptOutputs(runID string, formation FormationNode, attempt int, judge bool, result FormationExecutionResult) (FormationExecutionResult, error) {
	outputs, err := e.store.acceptAttemptOutputs(e.runContext(runID), runID, formation, attempt, result.Outputs)
	if err != nil {
		var notKept *outputNotKeptError
		if !errors.As(err, &notKept) {
			return FormationExecutionResult{}, err
		}
		data := formationOutputEventData(result)
		if judge {
			data["reason"] = "judge"
		}
		return FormationExecutionResult{}, &pendingOutputError{NodeID: formation.ID, Attempt: attempt, Output: data, Err: notKept}
	}
	result.Outputs = outputs
	return result, nil
}

// runContext is the run's execution context, which a cancel or the
// coordinator's shutdown ends.
func (e *RunEngine) runContext(runID string) context.Context {
	if e.executionContext != nil {
		return e.executionContext(runID)
	}
	return context.Background()
}

func (e *RunEngine) dispatchFormation(req FormationExecution) (FormationExecutionResult, error) {
	ctx := req.ctx
	if ctx == nil {
		ctx = e.runContext(req.RunID)
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
	// A step brief's {name} references take the run's input values.
	if strings.Contains(req.Brief.Goal, "{") {
		req.Brief.Goal = SubstituteRunInputs(req.Brief.Goal, MissionRunInputs(board), RunInputsFromEventData(events[0].Data["inputs"]), events[0].BeadID)
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
	// The tokens cards give the attempt what they have left (archon-o7p.9),
	// and the mission's card what every attempt working at once has left
	// together (tokenPool).
	tokenLimit := tokenBudget(board, events, req.NodeID)
	if tokenLimit != nil {
		req.TokenBudget = tokenLimit.Max - tokenLimit.Used
	}
	req.tokenPool = e.tokenPools.get(req.RunID)
	tokensSpent := func(err error) error {
		if tokenLimit == nil || !errors.Is(err, ErrTokenBudgetSpent) {
			return err
		}
		use := *tokenLimit
		if pool := req.tokenPool; pool.spent() {
			// The attempts working at once together spent the mission's card.
			if limit, ok := findLimit(board, pool.limitID); ok {
				if mission := tokensUse(board, events, limit); mission != nil {
					use = *mission
				}
			}
		}
		if limit, ok := findLimit(board, use.LimitID); ok {
			if events, readErr := e.store.ReadRunEvents(req.RunID); readErr == nil {
				if current := tokensUse(board, events, limit); current != nil {
					use = *current
				}
			}
		}
		use.Used = max(use.Used, use.Max)
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
		return result, tokensSpent(err)
	}
	// Coordinator-owned executors without context support run synchronously so a timeout cannot
	// leave a hidden writer behind after the coordinator releases its lock.
	if e.executionContext != nil || req.Deadline.IsZero() {
		result, err := e.executor.ExecuteFormation(req)
		if !req.Deadline.IsZero() && !e.store.now().Before(req.Deadline) {
			return FormationExecutionResult{}, timeSpent()
		}
		return result, tokensSpent(err)
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
		return result.result, tokensSpent(result.err)
	case <-ctx.Done():
		if timeLimit != nil && errors.Is(context.Cause(ctx), ErrFormationTimeoutExceeded) {
			return FormationExecutionResult{}, timeSpent()
		}
		return FormationExecutionResult{}, context.Cause(ctx)
	}
}

// outputPayloadError is a step's output that does not match its ports.
type outputPayloadError struct {
	Code    string
	Message string
	Reason  string
}

func (e *outputPayloadError) Error() string { return e.Message }

// checkFormationOutputPayloads reports a result that misses one of the step's
// outputs or names a port the step does not have.
func checkFormationOutputPayloads(formation FormationNode, result FormationExecutionResult) error {
	expected := make(map[string]bool, len(formation.Outputs))
	for _, output := range formation.Outputs {
		expected[output.ID] = true
		if _, ok := result.Outputs[output.ID]; !ok {
			return &outputPayloadError{Code: "missing_output_payload", Message: fmt.Sprintf("formation %s did not produce output for port %s", formation.ID, output.ID), Reason: "missing output payload"}
		}
	}
	for portID := range result.Outputs {
		if !expected[portID] {
			return &outputPayloadError{Code: "invalid_output_payload", Message: fmt.Sprintf("formation %s produced unknown output port %s", formation.ID, portID), Reason: "invalid output payload"}
		}
	}
	return nil
}

// startGateEvaluation records that a gate takes its oldest queued input.
func (e *RunEngine) startGateEvaluation(runID string, board *BoardDocument, gate GateNode, input RunInputRef) error {
	return e.store.AppendRunEvent(runID, RunEvent{
		Type:   RunEventGateEvaluating,
		GateID: gate.ID,
		NodeID: gate.ID,
		Data: map[string]any{
			"kinds":      gate.Kinds,
			"criterion":  gate.Criterion,
			"inputRef":   input,
			"judgeChain": formationIDs(judgeChainForGate(board, gate.ID)),
		},
	})
}

// runInputRefForConnection is the delivery of one output, which attempt
// produced, along a connection.
func runInputRefForConnection(runID string, connection BoardConnection, payload FormationOutputPayload, attempt int) RunInputRef {
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
		Kind:        payload.Kind,
		Origin:      &OutputOrigin{NodeID: fromNode, PortID: fromPort, Attempt: attempt},
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
			Kind:        stringFromAny(raw["kind"]),
		}
	default:
		return FormationOutputPayload{}
	}
}

type durableGateKindResult struct {
	Seq    int
	Result GateEvaluationResult
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

// routeGateEvaluation records a gate's verdict on its evaluation; replay
// delivers its routes.
func (e *RunEngine) routeGateEvaluation(runID string, board *BoardDocument, gate GateNode, input RunInputRef, verdict string, result GateEvaluationResult) error {
	attempt, err := e.gateAttempt(runID, gate.ID)
	if err != nil {
		return err
	}
	routePort := verdict
	routes := outgoingConnectionsFromPort(board.Connections, gate.ID, routePort)
	return e.store.AppendRunEvent(runID, RunEvent{
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
	})
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

// routeHumanVerdict records the gate verdict for one recorded human verdict,
// whose routes replay delivers: a pass carries the request's input with the
// operator's response, a fail sends that input back as feedback.
func (e *RunEngine) routeHumanVerdict(runID string, board *BoardDocument, gates map[string]GateNode, events []RunEvent, recorded RunEvent) error {
	gate, ok := gates[recorded.GateID]
	if !ok {
		return fmt.Errorf("%w: gate %q", ErrNotFound, recorded.GateID)
	}
	requestedSeq := intFromRunEventData(recorded.Data["requestedSeq"])
	if requestedSeq <= 0 || requestedSeq >= recorded.Seq || events[requestedSeq-1].Type != RunEventHumanInputRequested || events[requestedSeq-1].GateID != gate.ID {
		return fmt.Errorf("%w: Gate %q verdict names invalid human request %d", ErrRunLedgerInvalid, gate.ID, requestedSeq)
	}
	request := events[requestedSeq-1]
	if isPickEvent(request) {
		return e.routeHumanPick(runID, board, gates, events, request, recorded)
	}
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
	return e.store.AppendRunEvent(runID, RunEvent{
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
}

// gateEvaluatorError is a code check that could not judge: the run's worker
// records it on the gate and blocks the run there, and a resume evaluates the
// input again.
type gateEvaluatorError struct {
	Code    string
	Message string
	Reason  string
}

func (e *gateEvaluatorError) Error() string { return e.Message }

func (e *RunEngine) evaluateCodeGateResult(req GateEvaluation) (GateEvaluationResult, error) {
	if e.gateEvaluator == nil {
		return GateEvaluationResult{}, &gateEvaluatorError{Code: "missing_gate_evaluator", Message: "gate evaluator unavailable", Reason: "gate evaluator unavailable"}
	}
	evaluatorError := func(err error) error {
		return &gateEvaluatorError{Code: "gate_evaluator_error", Message: err.Error(), Reason: "gate evaluator error"}
	}
	if strings.TrimSpace(req.Check) != "" || strings.TrimSpace(req.CheckVersion) != "" {
		binding, err := e.store.readRunGateBinding(req.RunID, req.GateID)
		if err != nil {
			return GateEvaluationResult{}, evaluatorError(err)
		}
		req.Binding = binding
	}
	result, err := callGateEvaluator(e.gateEvaluator, req)
	if err != nil {
		return GateEvaluationResult{}, evaluatorError(err)
	}
	if err := validateCodeGateEvaluationResult(req, result); err != nil {
		return GateEvaluationResult{}, evaluatorError(err)
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
// continues from the first judge without output (archon-n7u.57). With pick,
// the chain picks the best attempt instead, anchored at its pick_started:
// every judge reads the candidates and the recommendations of the judges
// before it, and the last judge's pick decides (archon-gihe). The chain runs
// as a job of the run's worker, in ctx, and records nothing but its judges'
// starts and outputs: a judge it cannot run returns a stepError naming that
// judge, which the worker records.
func (e *RunEngine) runJudgeChain(ctx context.Context, board *BoardDocument, req GateEvaluation, chain []FormationNode, pick *judgePickMode) (string, error) {
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
	invalidCode, reason := "invalid_judge_result", "judge"
	var pickAttempts []int
	if pick != nil {
		evaluating, invalidCode, reason, pickAttempts = pick.anchorSeq, RunBlockInvalidJudgePick, "pick", pick.attempts
	}
	// A gate's evaluation and a pick by the same gate can follow each other
	// after either's anchor (archon-1uyi): a judge's output answers this chain
	// only when its attempt started for the same work, judging or picking.
	answered := map[string]RunEvent{}
	startedFor := map[string]string{}
	attemptKey := func(event RunEvent) string { return fmt.Sprintf("%s/%d", event.NodeID, event.Attempt) }
	for _, event := range events[evaluating:] {
		if event.Type == RunEventNodeStarted {
			startedFor[attemptKey(event)] = stringFromEventData(event, "reason")
		}
		if event.Type == RunEventNodeOutput && startedFor[attemptKey(event)] == reason {
			answered[event.NodeID] = event
		}
		// Preserve earlier reviews but discard the rejected final answer on resume.
		if event.Type == RunEventBlocked && event.GateID == req.GateID && stringFromEventData(event, "code") == invalidCode {
			delete(answered, chain[len(chain)-1].ID)
		}
	}
	if text, ok := takenJudgeFail(events[evaluating:], req.GateID); ok && pick == nil {
		return text, nil
	}
	input := req.Input
	var finalText string
	var recommendations strings.Builder
	replaying := true
	for index, formation := range chain {
		if len(formation.Outputs) == 0 {
			return "", fmt.Errorf("%w: judge formation %q has no output port", ErrConflict, formation.ID)
		}
		fromPortID := formation.Outputs[0].ID
		var payload FormationOutputPayload
		var outputAttempt int
		if output, ok := answered[formation.ID]; ok && replaying {
			finalText = stringFromEventData(output, "text")
			payload, _ = outputPayloadForPortFromEvent(output, fromPortID)
			outputAttempt = output.Attempt
		} else {
			replaying = false
			nodeAttempt := attempt
			for _, event := range events {
				if event.Type == RunEventNodeStarted && event.NodeID == formation.ID {
					if event.Seq > evaluating && stringFromEventData(event, "reason") == reason {
						// A restart before dispatch continues this same attempt.
						nodeAttempt = event.Attempt
					} else if event.Attempt >= nodeAttempt {
						nodeAttempt = event.Attempt + 1
					}
				}
				if event.Seq > evaluating && index == len(chain)-1 && event.Type == RunEventBlocked && event.GateID == req.GateID && stringFromEventData(event, "code") == invalidCode {
					nodeAttempt++
				}
			}
			spent, err := e.startFormationUnlessSpent(req.RunID, board, formation, RunEvent{
				Type:    RunEventNodeStarted,
				NodeID:  formation.ID,
				Attempt: nodeAttempt,
				Data: map[string]any{
					"nodeKind":  "formation",
					"inputRefs": []RunInputRef{input},
					"reason":    reason,
					"brief":     formationBriefEventData(formationBriefValue(formation)),
				},
			})
			if err != nil {
				return "", err
			}
			if spent != nil {
				return "", &stepError{NodeID: formation.ID, Err: &LimitReachedError{Use: *spent}}
			}
			result, err := e.executeFormation(FormationExecution{
				ctx:          ctx,
				JudgeGateID:  req.GateID,
				FinalJudge:   index == len(chain)-1,
				PickAttempts: pickAttempts,
				RunID:        req.RunID,
				NodeID:       formation.ID,
				Title:        formation.Title,
				Formation:    formation,
				Brief:        formationBriefValue(formation),
				Inputs:       []RunInputRef{input},
				Attempt:      nodeAttempt,
			})
			if err != nil {
				return "", &stepError{NodeID: formation.ID, Err: err}
			}
			if result.Status == "" {
				result.Status = "done"
			}
			if err := checkFormationOutputPayloads(formation, result); err != nil {
				return "", &stepError{NodeID: formation.ID, Err: err}
			}
			data := formationOutputEventData(result)
			data["reason"] = reason
			if err := e.store.AppendRunEvent(req.RunID, RunEvent{
				Type:    RunEventNodeOutput,
				NodeID:  formation.ID,
				Attempt: nodeAttempt,
				Data:    data,
			}); err != nil {
				return "", err
			}
			finalText = result.Text
			payload = result.Outputs[fromPortID]
			outputAttempt = nodeAttempt
		}
		if pick != nil {
			// Every judge of a pick compares the candidates themselves; a
			// judge before the last recommends one, and the judges after it
			// read that recommendation below the candidates.
			fmt.Fprintf(&recommendations, "\nrecommendation from %s:\n%s\n", nodeName(board, formation.ID), judgeRecommendation(payload, finalText))
			input = req.Input
			input.Text = req.Input.Text + recommendations.String()
			continue
		}
		input = RunInputRef{
			FromNodeID:  formation.ID,
			FromPortID:  fromPortID,
			Ref:         payload.Ref,
			Text:        payload.Text,
			ReportRef:   payload.ReportRef,
			ArtifactRef: payload.ArtifactRef,
			Kind:        payload.Kind,
			Origin:      &OutputOrigin{NodeID: formation.ID, PortID: fromPortID, Attempt: outputAttempt},
		}
		if input.Ref == "" {
			input.Ref = fmt.Sprintf("ledger://%s/%s", req.RunID, formation.ID)
		}
	}
	return finalText, nil
}

// takenJudgeFail answers for the judge when the gate's latest block on a
// rejected verdict was resumed with the verdict taken as a fail: a verdict in
// the judge's own form, naming who took it and what was rejected.
func takenJudgeFail(events []RunEvent, gateID string) (string, bool) {
	rejected, text := "", ""
	for _, event := range events {
		switch {
		case event.Type == RunEventBlocked:
			rejected, text = "", ""
			if event.GateID == gateID && stringFromEventData(event, "code") == "invalid_judge_result" {
				rejected = stringFromEventData(event, "reason")
			}
		case event.Type == RunEventResumed && rejected != "" && stringFromEventData(event, "judgeVerdict") == "fail":
			reason := "taken as a fail by " + stringFromEventData(event, "resumedBy") + ": the judge's verdict was rejected (" + rejected + ")"
			if why := stringFromEventData(event, "reason"); why != "" {
				reason += "; " + why
			}
			raw, _ := json.Marshal(map[string]any{"verdict": "fail", "reason": reason, "evidence": []string{}})
			text = "```archon-verdict\n" + string(raw) + "\n```\n"
		}
	}
	return text, text != ""
}

type executionFailureDetails struct {
	Code       string
	Message    string
	Boundary   string
	NodeID     string
	SlotID     string
	DispatchID string
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
	return RunInputRef{
		Feedback:    gateFeedbackFromAny(raw["feedback"]),
		Response:    gateResponseFromAny(raw["response"]),
		EdgeID:      stringFromAny(raw["edgeId"]),
		FromNodeID:  stringFromAny(raw["fromNodeId"]),
		FromPortID:  stringFromAny(raw["fromPortId"]),
		ToPortID:    stringFromAny(raw["toPortId"]),
		Ref:         stringFromAny(raw["ref"]),
		Text:        stringFromAny(raw["text"]),
		ReportRef:   stringFromAny(raw["reportRef"]),
		ArtifactRef: stringFromAny(raw["artifactRef"]),
		Kind:        stringFromAny(raw["kind"]),
		Origin:      outputOriginFromAny(raw["origin"]),
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
			ref.unread = false
			inputs = append(inputs, ref)
		}
	}
	return inputs
}
