package formations

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Steps whose inputs are ready work at the same time (archon-1uyi). These
// tests hold each step in the executor until they release it, so they see
// which steps work at once and decide the order their work ends in.

// heldExecutor holds every call until the test releases it.
type heldExecutor struct {
	fakeRunExecutor
	entered chan *heldCall
	// clock, when set, is the run's clock: each call records the time its
	// deadline leaves it.
	clock *testClock
	// tokens is what each node's call spends when released, metered as a
	// seat's transcript is (tokenMeter).
	tokens map[string]int
	store  *Store
}

// heldCall is one call the executor holds.
type heldCall struct {
	req      FormationExecution
	left     time.Duration
	release  chan error
	returned chan struct{}
}

func newHeldExecutor() *heldExecutor {
	return &heldExecutor{entered: make(chan *heldCall, 16)}
}

func (e *heldExecutor) ExecuteFormationContext(ctx context.Context, req FormationExecution) (FormationExecutionResult, error) {
	call := &heldCall{req: req, left: unlimited, release: make(chan error, 1), returned: make(chan struct{})}
	defer close(call.returned)
	if e.clock != nil && !req.Deadline.IsZero() {
		call.left = req.Deadline.Sub(e.clock.now())
	}
	e.entered <- call
	select {
	case <-ctx.Done():
		return FormationExecutionResult{}, ctx.Err()
	case err := <-call.release:
		if err != nil {
			e.record(req)
			return FormationExecutionResult{}, err
		}
	}
	if tokens := e.tokens[req.NodeID]; tokens > 0 {
		if err := e.store.AppendRunEvent(req.RunID, RunEvent{Type: RunEventTokenUsage, NodeID: req.NodeID, Attempt: req.Attempt, Data: map[string]any{"tokens": tokens}}); err != nil {
			return FormationExecutionResult{}, err
		}
		if newTokenMeter(req.TokenBudget, req.tokenPool, func() {}).set(fmt.Sprintf("%s/%d", req.NodeID, req.Attempt), tokens) {
			e.record(req)
			return FormationExecutionResult{}, ErrTokenBudgetSpent
		}
	}
	return e.fakeRunExecutor.ExecuteFormation(req)
}

// take waits for n calls to be held at once and returns them by node.
func (e *heldExecutor) take(t *testing.T, n int) map[string]*heldCall {
	t.Helper()
	calls := map[string]*heldCall{}
	for len(calls) < n {
		select {
		case call := <-e.entered:
			if calls[call.req.NodeID] != nil {
				t.Fatalf("%s entered twice", call.req.NodeID)
			}
			calls[call.req.NodeID] = call
		case <-time.After(10 * time.Second):
			t.Fatalf("held %d calls, want %d at once", len(calls), n)
		}
	}
	return calls
}

// none checks that no further call is held.
func (e *heldExecutor) none(t *testing.T) {
	t.Helper()
	select {
	case call := <-e.entered:
		t.Fatalf("%s#%d started, want nothing", call.req.NodeID, call.req.Attempt)
	case <-time.After(50 * time.Millisecond):
	}
}

func (c *heldCall) finish() { c.release <- nil }

func (c *heldCall) working() bool {
	select {
	case <-c.returned:
		return false
	default:
		return true
	}
}

// testClock is a run's clock, which the test moves on.
type testClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *testClock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

type heldRun struct {
	status *RunStatusProjection
	err    error
}

// startHeldRun starts the board's run, set up first by setup, and works it in
// the background; its result arrives on the returned channel.
func startHeldRun(t *testing.T, fixture string, executor *heldExecutor, setup func(*Store, *RunEngine)) (*Store, *RunEngine, string, <-chan heldRun) {
	t.Helper()
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), fixture)
	executor.store = store
	engine := NewRunEngine(store, personas, executor)
	if setup != nil {
		setup(store, engine)
	}
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test", Personas: personas})
	if err != nil {
		t.Fatal(err)
	}
	return store, engine, started.RunID, inBackground(func() (*RunStatusProjection, error) { return engine.ExecuteStartedMission(started.RunID) })
}

func inBackground(work func() (*RunStatusProjection, error)) <-chan heldRun {
	done := make(chan heldRun, 1)
	go func() {
		status, err := work()
		done <- heldRun{status, err}
	}()
	return done
}

func awaitRun(t *testing.T, done <-chan heldRun) *RunStatusProjection {
	t.Helper()
	select {
	case run := <-done:
		if run.err != nil {
			t.Fatal(run.err)
		}
		return run.status
	case <-time.After(10 * time.Second):
		t.Fatal("the run's worker did not settle")
		return nil
	}
}

// waitForLedger polls the run's ledger until done holds.
func waitForLedger(t *testing.T, store *Store, runID, what string, done func([]RunEvent) bool) []RunEvent {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		events := mustEvents(t, store, runID)
		if done(events) {
			return events
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited for %s:\n%s", what, eventTypeTrail(events))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func hasEvent(eventType, nodeID string) func([]RunEvent) bool {
	return func(events []RunEvent) bool { return indexEvent(events, eventType, nodeID) >= 0 }
}

// mission -> A and mission -> B, which a two-input join J takes together,
// then Done.
func fanOutJoinBoardFixture() string {
	return s4MissionOnlyBoardFixture() + branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_b", "B") + `
[[formation]]
id = "fmn_join"
type = "solo"
title = "Join"

[[formation.input]]
id = "port_join_a"
label = "From A"

[[formation.input]]
id = "port_join_b"
label = "From B"

[[formation.output]]
id = "port_join_out"
label = "Output"

[[formation.slot]]
id = "slot_join"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
effort = "medium"
controller = true
` + branchingBoardEnds() +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in") +
		branchingBoardConnection("edge_a_join", "fmn_a:port_fmn_a_out", "fmn_join:port_join_a") +
		branchingBoardConnection("edge_b_join", "fmn_b:port_fmn_b_out", "fmn_join:port_join_b") +
		branchingBoardConnection("edge_join_done", "fmn_join:port_join_out", "end_done:in")
}

// Fan-out branches work at the same time, and the join starts only once both
// have delivered, whichever finishes first.
func TestFanOutBranchesWorkAtTheSameTime(t *testing.T) {
	executor := newHeldExecutor()
	store, _, runID, done := startHeldRun(t, fanOutJoinBoardFixture(), executor, nil)
	// Both branches are held in the executor at once: they overlap.
	branches := executor.take(t, 2)
	if branches["fmn_a"] == nil || branches["fmn_b"] == nil {
		t.Fatalf("working at once: %v, want A and B", branches)
	}
	branches["fmn_b"].finish()
	events := waitForLedger(t, store, runID, "the join to wait for A", hasEvent(RunEventNodeWaiting, "fmn_join"))
	executor.none(t)
	if indexEvent(events, RunEventNodeStarted, "fmn_join") >= 0 || !branches["fmn_a"].working() {
		t.Fatalf("the join started before A delivered:\n%s", eventTypeTrail(events))
	}
	branches["fmn_a"].finish()
	join := executor.take(t, 1)["fmn_join"]
	if join == nil || len(join.req.Inputs) != 2 || join.req.Inputs[0].FromNodeID != "fmn_a" || join.req.Inputs[1].FromNodeID != "fmn_b" {
		t.Fatalf("join inputs = %+v, want A's and B's in port order", join)
	}
	join.finish()
	status := awaitRun(t, done)
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v", status)
	}
	events = mustEvents(t, store, runID)
	startedA, startedB := indexEvent(events, RunEventNodeStarted, "fmn_a"), indexEvent(events, RunEventNodeStarted, "fmn_b")
	outputA, outputB := indexEvent(events, RunEventNodeOutput, "fmn_a"), indexEvent(events, RunEventNodeOutput, "fmn_b")
	if max(startedA, startedB) > min(outputA, outputB) || indexEvent(events, RunEventNodeStarted, "fmn_join") < max(outputA, outputB) {
		t.Fatalf("ledger order:\n%s", eventTypeTrail(events))
	}
}

// A verdict recorded while another branch works routes at once, and the
// path it opens runs beside that branch.
func TestAVerdictRoutesWhileAnotherBranchWorks(t *testing.T) {
	executor := newHeldExecutor()
	store, engine, runID, done := startHeldRun(t, branchingGateBoardFixture(), executor, nil)
	branches := executor.take(t, 2)
	branches["fmn_a"].finish()
	events := waitForLedger(t, store, runID, "A's gate to ask", hasEvent(RunEventHumanInputRequested, "gate_review"))
	if _, err := engine.RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	events = waitForLedger(t, store, runID, "the verdict to route", hasEvent(RunEventGateVerdict, "gate_review"))
	if !branches["fmn_b"].working() || indexEvent(events, RunEventNodeOutput, "fmn_b") >= 0 {
		t.Fatalf("the verdict waited for B:\n%s", eventTypeTrail(events))
	}
	branches["fmn_b"].finish()
	executor.take(t, 1)["fmn_c"].finish()
	if status := awaitRun(t, done); status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v", status)
	}
}

// A step's failure is recorded when it happens; the run starts nothing more,
// lets the branch still working finish and record its output, and only then
// blocks. The resume runs the failed step again, not the finished one.
func TestAFailureBlocksTheRunOnceTheOtherBranchesSettle(t *testing.T) {
	executor := newHeldExecutor()
	store, engine, runID, done := startHeldRun(t, branchingGateBoardFixture(), executor, nil)
	branches := executor.take(t, 2)
	branches["fmn_a"].release <- &RunExecutionError{Code: "native_turn_failed", Message: "seat died", Boundary: "executor", NodeID: "fmn_a"}
	events := waitForLedger(t, store, runID, "A's failure", hasEvent(RunEventError, "fmn_a"))
	if indexEventType(events, RunEventBlocked) >= 0 {
		t.Fatalf("blocked while B works:\n%s", eventTypeTrail(events))
	}
	branches["fmn_b"].finish()
	status := awaitRun(t, done)
	executor.none(t)
	events = mustEvents(t, store, runID)
	if status.Status != RunStatusBlocked || !status.ResumeAllowed || events[len(events)-1].NodeID != "fmn_a" {
		t.Fatalf("status = %+v:\n%s", status, eventTypeTrail(events))
	}
	if output, blocked := indexEvent(events, RunEventNodeOutput, "fmn_b"), indexEventType(events, RunEventBlocked); output < 0 || output > blocked || indexEvent(events, RunEventNodeStarted, "fmn_c") >= 0 {
		t.Fatalf("B's output must be recorded before the block, and C not started:\n%s", eventTypeTrail(events))
	}
	resumed := inBackground(func() (*RunStatusProjection, error) {
		return engine.ResumeRun(runID, RunResumeRequest{Actor: "agent:test", Mode: "redispatch", Reason: "seat lost; run A again"})
	})
	again := executor.take(t, 2)
	if again["fmn_a"] == nil || again["fmn_a"].req.Attempt != 2 || again["fmn_c"] == nil {
		t.Fatalf("after the resume: %v, want A's second attempt beside C", again)
	}
	again["fmn_a"].finish()
	again["fmn_c"].finish()
	waitForLedger(t, store, runID, "A's gate to ask again", func(events []RunEvent) bool {
		return len(eventsOfType(events, RunEventHumanInputRequested)) > 0
	})
	if status := awaitRun(t, resumed); status.Status != RunStatusRunning {
		t.Fatalf("after the resume: %+v", status)
	}
}

// The operator's cancel ends every working step's seat at once.
func TestACancelEndsEveryWorkingStep(t *testing.T) {
	executor := newHeldExecutor()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	store, _, runID, done := startHeldRun(t, fanOutJoinBoardFixture(), executor, func(_ *Store, engine *RunEngine) {
		engine.SetExecutionContext(func(string) context.Context { return ctx })
	})
	branches := executor.take(t, 2)
	cancel(context.Canceled)
	awaitRun(t, done)
	for node, call := range branches {
		if call.working() {
			t.Fatalf("%s still works after the cancel", node)
		}
	}
	events := mustEvents(t, store, runID)
	if len(eventsOfType(events, RunEventNodeOutput)) != 1 || indexEventType(events, RunEventBlocked) >= 0 || indexEventType(events, RunEventError) >= 0 {
		t.Fatalf("a cancel records nothing more; the coordinator ends the run:\n%s", eventTypeTrail(events))
	}
}

// mission -> W and mission -> A -> W, both into W's one input, then W ->
// Done: A's delivery reaches W while W works.
func deliveryWhileWorkingBoardFixture() string {
	return s4MissionOnlyBoardFixture() + branchingBoardFormation("fmn_w", "W") + branchingBoardFormation("fmn_a", "A") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_w", "mis_showcase:out", "fmn_w:port_fmn_w_in") +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_a_w", "fmn_a:port_fmn_a_out", "fmn_w:port_fmn_w_in") +
		branchingBoardConnection("edge_w_done", "fmn_w:port_fmn_w_out", "end_done:in")
}

// A delivery that reaches a step while it works waits: the step finishes
// on the inputs it started with, then runs again on the new one.
func TestADeliveryToAWorkingStepWaitsForItsOutput(t *testing.T) {
	executor := newHeldExecutor()
	store, _, runID, done := startHeldRun(t, deliveryWhileWorkingBoardFixture(), executor, nil)
	first := executor.take(t, 2)
	first["fmn_a"].finish()
	waitForLedger(t, store, runID, "A's output", hasEvent(RunEventNodeOutput, "fmn_a"))
	executor.none(t)
	if got := first["fmn_w"].req.Inputs; len(got) != 1 || got[0].FromNodeID != "mis_showcase" {
		t.Fatalf("W's first inputs = %+v", got)
	}
	first["fmn_w"].finish()
	again := executor.take(t, 1)["fmn_w"]
	if again == nil || again.req.Attempt != 2 || len(again.req.Inputs) != 1 || again.req.Inputs[0].FromNodeID != "fmn_a" {
		t.Fatalf("W's second call = %+v, want attempt 2 on A's delivery", again)
	}
	again.finish()
	if status := awaitRun(t, done); status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v", status)
	}
}

// mission -> A and mission -> B, both into one judge gate whose pass ends
// Done.
func twoIntoOneJudgeGateBoardFixture() string {
	return s4MissionOnlyBoardFixture() + branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_b", "B") +
		branchingBoardFormation("fmn_judge", "Judge") + branchingBoardJudgeGate("gate_review", "fmn_judge") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in") +
		branchingBoardConnection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_review:in") +
		branchingBoardConnection("edge_b_gate", "fmn_b:port_fmn_b_out", "gate_review:in") +
		branchingBoardGateEnds("gate_review")
}

// A gate judges one input at a time, oldest first: B delivered first, so the
// judge takes B's output, and A's waits until B's verdict.
func TestAGateTakesItsInputsOldestFirst(t *testing.T) {
	executor := newHeldExecutor()
	executor.outputs = map[string]string{"fmn_judge": judgeBlock("pass", "fine")}
	store, _, runID, done := startHeldRun(t, twoIntoOneJudgeGateBoardFixture(), executor, nil)
	branches := executor.take(t, 2)
	branches["fmn_b"].finish()
	first := executor.take(t, 1)["fmn_judge"]
	branches["fmn_a"].finish()
	waitForLedger(t, store, runID, "A's output", hasEvent(RunEventNodeOutput, "fmn_a"))
	executor.none(t)
	first.finish()
	second := executor.take(t, 1)["fmn_judge"]
	second.finish()
	if status := awaitRun(t, done); status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v", status)
	}
	for i, judged := range []*heldCall{first, second} {
		want := []string{"fmn_b", "fmn_a"}[i]
		if inputs := judged.req.Inputs; len(inputs) == 0 || inputs[0].FromNodeID != want {
			t.Fatalf("judgement %d inputs = %+v, want %s's output", i+1, inputs, want)
		}
	}
}

// The mission's tokens card covers every step working at once: a step
// stops when the steps together spend the card, though its own budget, read
// when it started, is not spent.
func TestTheMissionTokensCardCountsStepsWorkingAtOnce(t *testing.T) {
	executor := newHeldExecutor()
	executor.tokens = map[string]int{"fmn_a": 6000, "fmn_b": 6000}
	var limit LimitNode
	store, _, runID, done := startHeldRun(t, fanOutJoinBoardFixture(), executor, func(store *Store, _ *RunEngine) {
		limit = addTokensLimit(t, store, "mis_showcase", 10000)
	})
	branches := executor.take(t, 2)
	for node, call := range branches {
		if call.req.TokenBudget != 10000 {
			t.Fatalf("%s budget = %d, want the mission's 10,000", node, call.req.TokenBudget)
		}
	}
	branches["fmn_a"].finish()
	waitForLedger(t, store, runID, "A's output", hasEvent(RunEventNodeOutput, "fmn_a"))
	branches["fmn_b"].finish()
	status := awaitRun(t, done)
	events := mustEvents(t, store, runID)
	block := events[len(events)-1]
	if status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant || block.NodeID != "fmn_b" || block.Data["reason"] != "The mission used 12,000 of 10,000 tokens" {
		t.Fatalf("status = %+v, block %+v:\n%s", status, block, eventTypeTrail(events))
	}
	if use := tokensUse(mustReadRunBoard(t, store, runID), events, limit); use == nil || use.Used != 12000 {
		t.Fatalf("use = %+v", use)
	}
}

// Two steps working at once each get the mission's warning; a step that
// starts after it went out does not.
func TestTheMissionWarningGoesToEveryStepWorkingWhenItFires(t *testing.T) {
	executor := newHeldExecutor()
	store, _, _, done := startHeldRun(t, fanOutJoinBoardFixture(), executor, nil)
	branches := executor.take(t, 2)
	mission := LimitWarning{LimitID: "lim_mission", Mission: true, Text: "mission"}
	claim := func(call *heldCall) bool { return store.claimLimitWarning(call.req, "slot_"+call.req.NodeID, mission) }
	if !claim(branches["fmn_a"]) || !claim(branches["fmn_b"]) || claim(branches["fmn_a"]) {
		t.Fatal("each working step's seat gets the mission's warning once")
	}
	branches["fmn_a"].finish()
	branches["fmn_b"].finish()
	join := executor.take(t, 1)["fmn_join"]
	if claim(join) {
		t.Fatal("the join started after the mission's warning went out")
	}
	join.finish()
	awaitRun(t, done)
}
