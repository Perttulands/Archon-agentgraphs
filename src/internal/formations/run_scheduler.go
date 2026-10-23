package formations

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
)

// The run's worker (archon-1uyi). Every step whose inputs are ready runs at
// once, each in its own job, as do the judge chains of gates evaluating
// different inputs. The worker alone decides what runs next: before each
// decision it reads the ledger through replayRun, the same reading a restart
// makes, so a decision never rests on anything the ledger does not hold. Jobs
// run the seats; the worker records what each finished: a step's output, a
// judge's verdict as its gate's kind result. It records every event that moves
// work (node_started, node_output, gate_evaluating, gate_verdict,
// human_input_requested, pick_started), in the order things happened.
//
// It routes a recorded human verdict as soon as it learns of it, starts the
// steps whose inputs arrived, takes each idle gate's oldest queued input, and
// goes on with open evaluations and picks. A join starts only when every one
// of its inputs has arrived. When nothing is in flight and nothing can start,
// the run ends by the one completion rule (endWhenNothingCanRun), or leaves
// the stop that halted it (runStop).
type runScheduler struct {
	e            *RunEngine
	runID        string
	board        *BoardDocument
	gates        map[string]GateNode
	startReason  string
	finishReason string
	// jobs is the context every job runs in; a broken ledger cancels it.
	jobs       context.Context
	cancelJobs context.CancelCauseFunc
	// running holds the steps and gates with a job in flight.
	running map[string]bool
	done    chan jobDone
	// noted is the last seq whose deliveries' waiting steps are recorded.
	noted    int
	stop     *runStop
	shutdown *runStop
	aborted  bool
	// err is a fault of the engine or its ledger: the run stops, and the
	// coordinator records it once every job has returned.
	err error
}

// RunWorkerObserver, when the executor implements it, is told each time a
// run's worker has decided all it can and waits on its jobs, naming the steps
// and gates whose jobs work, and each time it wakes, with no jobs. A test
// executor uses it to let jobs that work at once finish one at a time, in an
// order a restart can repeat.
type RunWorkerObserver interface {
	RunWorkerWaiting(runID string, jobs []string)
}

// jobDone is a job's end: finish records its result on the worker.
type jobDone struct {
	key    string
	finish func() error
}

func (e *RunEngine) newRunScheduler(runID string, board *BoardDocument, startReason, finishReason string) *runScheduler {
	gates := map[string]GateNode{}
	for _, gate := range board.Gates {
		gates[gate.ID] = gate
	}
	jobs, cancel := context.WithCancelCause(e.runContext(runID))
	return &runScheduler{e: e, runID: runID, board: board, gates: gates, startReason: startReason, finishReason: finishReason,
		jobs: jobs, cancelJobs: cancel, running: map[string]bool{}, done: make(chan jobDone)}
}

// run prepares the run, then works until nothing is in flight and nothing can
// start, and ends or stops the run.
func (s *runScheduler) run(prepare func([]RunEvent) error) error {
	wake, unwatch := s.e.watchRun(s.runID)
	defer unwatch()
	defer s.cancelJobs(nil)
	events, err := s.e.store.ReadRunEvents(s.runID)
	if err != nil {
		return err
	}
	s.noted = len(events)
	defer s.e.tokenPools.open(s.runID, s.board, events)()
	if err := prepare(events); err != nil {
		s.fail(err)
	}
	for {
		if s.err == nil {
			if err := s.advance(); err != nil {
				s.fail(err)
			}
		}
		if len(s.running) == 0 {
			break
		}
		s.waiting(slices.Sorted(maps.Keys(s.running)))
		select {
		case done := <-s.done:
			s.waiting(nil)
			delete(s.running, done.key)
			if err := done.finish(); err != nil {
				s.fail(err)
			}
		case <-wake:
			s.waiting(nil)
		}
	}
	return s.end()
}

func (s *runScheduler) waiting(jobs []string) {
	if observer, ok := s.e.executor.(RunWorkerObserver); ok {
		observer.RunWorkerWaiting(s.runID, jobs)
	}
}

func (s *runScheduler) fail(err error) {
	if s.err == nil {
		s.err = err
		s.cancelJobs(err)
	}
}

// end leaves what stopped the run, or applies the completion rule.
func (s *runScheduler) end() error {
	switch {
	case s.err != nil:
		return s.err
	case s.aborted:
		return nil
	case s.shutdown != nil:
		return s.shutdown.leave(s.e, s.runID)
	case s.stop != nil:
		return s.stop.leave(s.e, s.runID)
	}
	events, err := s.e.store.ReadRunEvents(s.runID)
	if err != nil {
		return err
	}
	return s.e.endWhenNothingCanRun(s.runID, s.board, events, s.finishReason)
}

// stopWith keeps the stop that ranks first (see runStop).
func (s *runScheduler) stopWith(stop runStop) {
	switch {
	case stop.abort:
		s.aborted = true
	case stop.shutdown:
		if s.shutdown == nil {
			s.shutdown = &stop
		}
	case s.stop == nil || stop.final && !s.stop.final:
		s.stop = &stop
	}
}

// halted reports whether the run starts nothing more: a stop, the operator's
// cancel or the coordinator's shutdown.
func (s *runScheduler) halted() bool {
	if err := dispatchContextError(s.e.runContext(s.runID)); err != nil {
		if errors.Is(err, ErrCoordinatorShutdown) {
			s.stopWith(runStop{shutdown: true})
		} else {
			s.aborted = true
		}
	}
	return s.err != nil || s.aborted || s.shutdown != nil || s.stop != nil
}

// failed records the failure of a job's attempt at nodeID as a stop.
func (s *runScheduler) failed(nodeID string, err error) error {
	stop, err := s.e.stopFor(s.runID, nodeID, err)
	if err != nil {
		return err
	}
	s.stopWith(stop)
	return nil
}

// launch runs work as a job holding key, a step or a gate; the function work
// returns records its result on the worker.
func (s *runScheduler) launch(key string, work func() func() error) {
	s.running[key] = true
	go func() {
		finish := work()
		s.done <- jobDone{key: key, finish: func() error {
			if s.err != nil {
				// The engine faulted: the run records nothing more.
				return nil
			}
			return finish()
		}}
	}()
}

// advance acts until nothing more can be decided now.
func (s *runScheduler) advance() error {
	for {
		events, err := s.e.store.ReadRunEvents(s.runID)
		if err != nil {
			return err
		}
		replay := replayRun(s.board, events)
		if replay.err != nil {
			return replay.err
		}
		acted, err := s.act(events, replay)
		if err != nil || !acted {
			return err
		}
	}
}

// act makes the next decision the ledger allows and reports whether it
// recorded anything, so the next is read from the ledger again.
func (s *runScheduler) act(events []RunEvent, replay *runReplay) (bool, error) {
	if s.noted < len(events) {
		// A delivery that leaves a step waiting for its other inputs says so.
		from := s.noted
		s.noted = len(events)
		waiting := false
		for seq := from + 1; seq <= len(events); seq++ {
			for _, nodeID := range replay.waiting[seq] {
				formation, ok := replay.formations[nodeID]
				if !ok || formationReady(formation, replay.ports[nodeID]) {
					continue
				}
				if err := s.e.appendWaiting(s.runID, formation, replay.ports[nodeID]); err != nil {
					return false, err
				}
				waiting = true
			}
		}
		if waiting {
			return true, nil
		}
	}
	// A stopped run starts no work: no step, no judge and no code check,
	// whose error stopped it perhaps. It still routes the verdicts the
	// ledger's results decide, asks the operator, and records what its
	// working seats finish; it leaves its stop once no seat works (runStop).
	// A cancel or a fault of the engine records nothing more.
	stopping := s.halted()
	if s.err != nil || s.aborted {
		return false, nil
	}
	// A recorded human verdict routes first, oldest first.
	if verdicts := unroutedHumanVerdicts(events); len(verdicts) > 0 {
		return true, s.e.routeHumanVerdict(s.runID, s.board, s.gates, events, verdicts[0])
	}
	// A gate goes on with its open evaluation, or takes its oldest input.
	for _, gate := range s.board.Gates {
		if s.running[gate.ID] {
			continue
		}
		if evaluating, ok := replay.evaluating[gate.ID]; ok {
			if acted, err := s.evaluate(gate, evaluating, events, stopping); acted || err != nil {
				return true, err
			}
			continue
		}
		if replay.pickingAt(gate.ID) {
			continue
		}
		if queued := replay.queue[gate.ID]; len(queued) > 0 {
			return true, s.e.startGateEvaluation(s.runID, s.board, gate, queued[0])
		}
	}
	// A pick goes on.
	for _, started := range replay.picks {
		if s.running[started.GateID] {
			continue
		}
		if acted, err := s.pick(started, events, stopping); acted || err != nil {
			return true, err
		}
	}
	if stopping {
		return false, nil
	}
	// A step cut short runs again on the inputs it started with; a step
	// with unread inputs runs once all its ports are fed.
	for _, formation := range s.board.Formations {
		if s.running[formation.ID] || replay.judges[formation.ID] || replay.picking(formation.ID) {
			continue
		}
		attempt := replay.attempts[formation.ID] + 1
		if open, ok := replay.open[formation.ID]; ok {
			if err := s.e.abandonCutShortDispatches(s.runID, formation.ID, open.Attempt, events); err != nil {
				return false, err
			}
			return true, s.start(formation, open.Inputs, attempt, events)
		}
		if replay.unread(formation.ID) && formationReady(formation, replay.ports[formation.ID]) {
			return true, s.start(formation, orderedInputs(formation, replay.ports[formation.ID]), attempt, events)
		}
	}
	return false, nil
}

// start records a step's start, once the run's Limit cards allow it, and
// runs it as a job. A spent card picks the step's best attempt or stops the
// run (pickOrStop).
func (s *runScheduler) start(formation FormationNode, inputs []RunInputRef, attempt int, events []RunEvent) error {
	spent, err := s.e.startFormationUnlessSpent(s.runID, s.board, formation, RunEvent{
		Type:    RunEventNodeStarted,
		NodeID:  formation.ID,
		Attempt: attempt,
		Data: map[string]any{
			"nodeKind":  "formation",
			"inputRefs": inputs,
			"reason":    s.startReason,
			"brief":     formationBriefEventData(formationBriefValue(formation)),
		},
	})
	if err != nil {
		return err
	}
	if spent != nil {
		return s.pickOrStop(formation.ID, *spent, false)
	}
	keep := RunHumanChannel(s.board, events) == HumanChannelSession && FormationKeepsSeatsOnCall(s.board, formation.ID)
	s.launch(formation.ID, func() func() error {
		result, err := s.e.executeFormation(FormationExecution{
			ctx:             s.jobs,
			RunID:           s.runID,
			NodeID:          formation.ID,
			Title:           formation.Title,
			Formation:       formation,
			Brief:           formationBriefValue(formation),
			Inputs:          inputs,
			Attempt:         attempt,
			KeepSeatsOnCall: keep,
		})
		return func() error {
			if limit := (*LimitReachedError)(nil); errors.As(err, &limit) {
				// The limit cut the attempt short; it is never a candidate.
				return s.pickOrStop(formation.ID, limit.Use, true)
			}
			if err != nil {
				return s.failed(formation.ID, err)
			}
			if result.Status == "" {
				result.Status = "done"
			}
			if err := checkFormationOutputPayloads(formation, result); err != nil {
				return s.failed(formation.ID, err)
			}
			return s.e.store.AppendRunEvent(s.runID, RunEvent{
				Type:    RunEventNodeOutput,
				NodeID:  formation.ID,
				Attempt: attempt,
				Data:    formationOutputEventData(result),
			})
		}
	})
	return nil
}

// abandonCutShortDispatches records as abandoned each dispatch a cut-short
// attempt left open, before the step runs again. Several steps can stop at
// once, and a block names only its own stop's dispatches; the others' would
// otherwise stay open, and a later block would name a dispatch of an attempt
// the run has moved past for a reattach (archon-1uyi).
func (e *RunEngine) abandonCutShortDispatches(runID, nodeID string, attempt int, events []RunEvent) error {
	for _, dispatch := range unresolvedDispatchEvents(events) {
		if dispatch.NodeID != nodeID || dispatch.Attempt != attempt {
			continue
		}
		if err := e.store.AppendRunEvent(runID, RunEvent{Type: RunEventSlotResult, NodeID: nodeID, SlotID: dispatch.SlotID, Data: map[string]any{
			"dispatchId": stringFromEventData(dispatch, "dispatchId"), "nodeId": nodeID, "slotId": dispatch.SlotID, "status": "abandoned",
			"reason": fmt.Sprintf("attempt %d was cut short and runs again", attempt),
		}}); err != nil {
			return err
		}
	}
	return nil
}

// pickOrStop is what a step does at its spent limit. A card whose whenSpent
// is best, with an attempt its gate judged since the step's last pick, starts
// a pick; anything else stops the run at the limit. cutOff marks an attempt
// the limit stopped while it worked: the ledger records why it stopped, and it
// is never a candidate.
func (s *runScheduler) pickOrStop(stepID string, use RunLimitReached, cutOff bool) error {
	events, err := s.e.store.ReadRunEvents(s.runID)
	if err != nil {
		return err
	}
	card, _ := findLimit(s.board, use.LimitID)
	pick, ok := pickFor(s.board, events, stepID, use)
	if !ok || card.WhenSpent != LimitWhenSpentBest || !judgedSinceLastPick(events, stepID, pick.candidates) {
		stop, err := s.e.stopAtLimit(s.runID, s.board, stepID, use)
		if err != nil {
			return err
		}
		s.stopWith(stop)
		return nil
	}
	if cutOff {
		if err := s.e.appendLimitError(s.runID, s.board, stepID, use); err != nil {
			return err
		}
	}
	return s.e.startPick(s.runID, s.board, stepID, pick, use, PickTriggerWhenSpent)
}

// pickOnResume starts the pick a resume with --pick-best asked for, whatever
// the card's whenSpent.
func (s *runScheduler) pickOnResume(events []RunEvent) error {
	use, ok := pendingResumePick(events)
	if !ok {
		return nil
	}
	pick, ok := pickFor(s.board, events, use.NodeID, use)
	if !ok {
		stop, err := s.e.stopAtLimit(s.runID, s.board, use.NodeID, use)
		if err != nil {
			return err
		}
		s.stopWith(stop)
		return nil
	}
	return s.e.startPick(s.runID, s.board, use.NodeID, pick, use, PickTriggerResume)
}

// evaluate goes on with a gate's open evaluation from the kind results the
// ledger holds: the code check runs here, the judge chain as a job, and a
// human gate asks; the verdict routes once the kinds have decided. A stopping
// run runs no check and starts no judge. It reports whether it recorded
// anything.
func (s *runScheduler) evaluate(gate GateNode, evaluating RunEvent, events []RunEvent, stopping bool) (bool, error) {
	input := runInputRefFromAny(evaluating.Data["inputRef"])
	prior, err := s.e.durableGateKindResultsForEvaluation(s.runID, gate, input, events, evaluating.Seq)
	if err != nil {
		return false, err
	}
	result := GateEvaluationResult{Verdict: "pass", PerKind: map[string]string{}, KindResultSeqs: map[string]int{}}
	if hasGateKind(gate.Kinds, "code") {
		codeResult, codeResultSeq := prior["code"].Result, prior["code"].Seq
		if _, ok := prior["code"]; !ok {
			if stopping {
				return false, nil
			}
			codeResult, err = s.e.evaluateCodeGateResult(GateEvaluation{
				RunID:        s.runID,
				GateID:       gate.ID,
				Title:        gate.Title,
				Kinds:        []string{"code"},
				Criterion:    gate.Criterion,
				Check:        gate.Check,
				CheckVersion: gate.CheckVersion,
				CheckValue:   gate.CheckValue,
				Input:        input,
			})
			if evaluatorErr := (*gateEvaluatorError)(nil); errors.As(err, &evaluatorErr) {
				stop, err := s.e.stopAtGateError(s.runID, gate.ID, evaluatorErr.Code, evaluatorErr.Message, "gate", evaluatorErr.Reason)
				if err != nil {
					return false, err
				}
				s.stopWith(stop)
				return true, nil
			}
			if err != nil {
				return false, err
			}
			codeResult.Verdict = normalizeGateVerdict(codeResult.Verdict)
			codeResult.PerKind = map[string]string{"code": codeResult.Verdict}
			if codeResultSeq, err = s.e.appendGateKindResult(s.runID, gate, "code", input, codeResult); err != nil {
				return false, err
			}
		}
		codeVerdict := normalizeGateVerdict(codeResult.Verdict)
		result = codeResult
		result.Verdict = codeVerdict
		result.CodeVerdict = codeVerdict
		result.CodeReason = codeResult.Reason
		result.PerKind = map[string]string{"code": codeVerdict}
		result.KindResultSeqs = map[string]int{"code": codeResultSeq}
		if codeVerdict == "fail" {
			markLaterGateKindsNotRun(gate.Kinds, result.PerKind, "code")
			return true, s.e.routeGateEvaluation(s.runID, s.board, gate, input, "fail", result)
		}
	}
	if hasGateKind(gate.Kinds, "formation") {
		durable, ok := prior["formation"]
		if !ok {
			if !stopping {
				s.judge(gate, input)
			}
			return false, nil
		}
		result.Verdict = durable.Result.Verdict
		result.Reason = durable.Result.Reason
		result.Evidence = append(result.Evidence, durable.Result.Evidence...)
		result.PerKind["formation"] = durable.Result.Verdict
		result.KindResultSeqs["formation"] = durable.Seq
		if durable.Result.Verdict == "fail" {
			markLaterGateKindsNotRun(gate.Kinds, result.PerKind, "formation")
			return true, s.e.routeGateEvaluation(s.runID, s.board, gate, input, "fail", result)
		}
	}
	if hasGateKind(gate.Kinds, "human") {
		// Only this path waits for the verdict; the run goes on with every
		// other branch (archon-o7p.11).
		return true, s.e.store.AppendRunEvent(s.runID, RunEvent{
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
		})
	}
	return true, s.e.routeGateEvaluation(s.runID, s.board, gate, input, "pass", result)
}

// judge runs the gate's judge chain on input as a job; its verdict becomes
// the gate's formation kind result, and the evaluation goes on from there.
func (s *runScheduler) judge(gate GateNode, input RunInputRef) {
	s.launch(gate.ID, func() func() error {
		text, err := s.e.runJudgeChain(s.jobs, s.board, GateEvaluation{
			RunID:     s.runID,
			GateID:    gate.ID,
			Title:     gate.Title,
			Kinds:     []string{"formation"},
			Criterion: gate.Criterion,
			Input:     input,
		}, judgeChainForGate(s.board, gate.ID), nil)
		return func() error {
			if err != nil {
				return s.failed(gate.ID, err)
			}
			verdict, err := parseJudgeVerdict(text)
			if err != nil {
				stop, err := s.e.stopAtInvalidJudge(s.runID, gate.ID, err)
				if err != nil {
					return err
				}
				s.stopWith(stop)
				return nil
			}
			_, err = s.e.appendGateKindResult(s.runID, gate, "formation", input, verdict)
			return err
		}
	})
}

// pick decides the pick a pick_started began. The only candidate is taken
// without a picker. Otherwise the gate's judge chain picks, as a job, and a
// human gate asks the operator, with the judge's pick as a recommendation
// when the gate has a judge. A stopping run starts no judge. It reports
// whether it recorded anything.
func (s *runScheduler) pick(started RunEvent, events []RunEvent, stopping bool) (bool, error) {
	gate, ok := s.gates[started.GateID]
	if !ok {
		return false, fmt.Errorf("%w: gate %q", ErrNotFound, started.GateID)
	}
	candidates, err := loadPickCandidates(events, pickCandidatesFromEvent(started))
	if err != nil {
		return false, err
	}
	if len(candidates) == 0 {
		return false, fmt.Errorf("%w: pick %d has no candidates", ErrRunLedgerInvalid, started.Seq)
	}
	if len(candidates) == 1 {
		only := candidates[0]
		return true, s.e.resolvePick(s.runID, s.board, gate, started, pickChoice{
			Attempt: only.Attempt, Reason: fmt.Sprintf("the only attempt %s judged", nodeName(s.board, gate.ID)), DecidedBy: PickDecidedByArchon,
		}, 0, nil)
	}
	input := pickInput(s.runID, s.board, gate, started, candidates)
	attempts := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		attempts = append(attempts, candidate.Attempt)
	}
	if hasGateKind(gate.Kinds, "formation") {
		chain := judgeChainForGate(s.board, gate.ID)
		if len(chain) == 0 {
			return false, fmt.Errorf("%w: gate %q has no judge chain", ErrConflict, gate.ID)
		}
		if stopping {
			return false, nil
		}
		s.launch(gate.ID, func() func() error {
			text, err := s.e.runJudgeChain(s.jobs, s.board, GateEvaluation{
				RunID: s.runID, GateID: gate.ID, Title: gate.Title, Kinds: []string{"formation"}, Criterion: gate.Criterion, Input: input,
			}, chain, &judgePickMode{anchorSeq: started.Seq, attempts: attempts})
			return func() error {
				if err != nil {
					return s.failed(gate.ID, err)
				}
				choice, err := parseJudgePick(text, attempts)
				if err != nil {
					stop, err := s.e.stopAtInvalidPick(s.runID, gate.ID, err)
					if err != nil {
						return err
					}
					s.stopWith(stop)
					return nil
				}
				choice.DecidedBy = chain[len(chain)-1].ID
				if !hasGateKind(gate.Kinds, "human") {
					return s.e.resolvePick(s.runID, s.board, gate, started, choice, 0, nil)
				}
				return s.e.askPick(s.runID, gate, started, input, attempts, &choice)
			}
		})
		return false, nil
	}
	if !hasGateKind(gate.Kinds, "human") {
		return false, fmt.Errorf("%w: gate %q has no judge or human to pick", ErrRunLedgerInvalid, gate.ID)
	}
	return true, s.e.askPick(s.runID, gate, started, input, attempts, nil)
}

// askPick asks the operator to pick the best attempt. Only this path waits
// for the pick; the run goes on with every other branch, as with a verdict
// (archon-o7p.11).
func (e *RunEngine) askPick(runID string, gate GateNode, started RunEvent, input RunInputRef, attempts []int, recommendation *pickChoice) error {
	choices := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		choices = append(choices, strconv.Itoa(attempt))
	}
	pick := map[string]any{"nodeId": started.NodeID, "pickSeq": started.Seq, "candidates": attempts}
	if recommendation != nil {
		pick["recommendation"] = recommendation.data()
	}
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

// watchRun lets a run's worker learn of a verdict recorded while it works, so
// the verdict routes at once rather than when the worker settles.
func (e *RunEngine) watchRun(runID string) (<-chan struct{}, func()) {
	wake := make(chan struct{}, 1)
	e.watchMu.Lock()
	if e.watchers == nil {
		e.watchers = map[string]chan struct{}{}
	}
	e.watchers[runID] = wake
	e.watchMu.Unlock()
	return wake, func() {
		e.watchMu.Lock()
		if e.watchers[runID] == wake {
			delete(e.watchers, runID)
		}
		e.watchMu.Unlock()
	}
}

// wakeRun tells the run's worker, if one works, that the ledger changed.
func (e *RunEngine) wakeRun(runID string) {
	e.watchMu.Lock()
	defer e.watchMu.Unlock()
	if wake := e.watchers[runID]; wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
}

// runLocks serializes work on one run that its concurrent steps share, such
// as reconsidering its kept seats.
type runLocks struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func (l *runLocks) lock(runID string) func() {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*sync.Mutex{}
	}
	lock := l.locks[runID]
	if lock == nil {
		lock = &sync.Mutex{}
		l.locks[runID] = lock
	}
	l.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}
