package formations

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Time cards (archon-o7p.8.2): a Limit card's time counts wall time while the
// covered work runs, pauses while only waiting work remains, stops the step at
// the limit and warns its seats once.

// unlimited is what timedExecutor records for a step with no deadline.
const unlimited = time.Duration(-1)

// timedExecutor works on a test clock: each node's nth call takes work[node][n]
// and stops at its deadline as a real seat is stopped. It records the time each
// call was given and the warnings it carried.
type timedExecutor struct {
	fakeRunExecutor
	clock    *time.Time
	work     map[string][]time.Duration
	left     map[string][]time.Duration
	warnings map[string][]LimitWarning
}

func (e *timedExecutor) ExecuteFormationContext(ctx context.Context, req FormationExecution) (FormationExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return FormationExecutionResult{}, err
	}
	if e.left == nil {
		e.left, e.warnings = map[string][]time.Duration{}, map[string][]LimitWarning{}
	}
	n := len(e.left[req.NodeID])
	left := unlimited
	if !req.Deadline.IsZero() {
		left = req.Deadline.Sub(*e.clock)
	}
	e.left[req.NodeID] = append(e.left[req.NodeID], left)
	e.warnings[req.NodeID] = append(e.warnings[req.NodeID], req.Warnings...)
	var work time.Duration
	if script := e.work[req.NodeID]; n < len(script) {
		work = script[n]
	}
	if left != unlimited && work >= left {
		*e.clock = req.Deadline
		return FormationExecutionResult{}, ErrFormationTimeoutExceeded
	}
	*e.clock = e.clock.Add(work)
	return e.fakeRunExecutor.ExecuteFormation(req)
}

func timedRun(t *testing.T, fixture string, work map[string][]time.Duration, cards func(*Store)) (*Store, *RunEngine, *timedExecutor, *time.Time, *RunStatusProjection) {
	t.Helper()
	store, personas := s4RunFixture(t)
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), fixture)
	cards(store)
	executor := &timedExecutor{clock: &clock, work: work}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test"})
	if err != nil {
		t.Fatal(err)
	}
	return store, engine, executor, &clock, status
}

func addTimeLimit(t *testing.T, store *Store, target string, seconds, warnSeconds int) LimitNode {
	t.Helper()
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.CreateLimit("session-search", LimitCreateRequest{Title: "Clock", Target: target, Seconds: seconds, WarnSeconds: warnSeconds}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	return result.Limit
}

// Only the step a card covers is timed; the others run as long as they take.
func TestAStepTimeCardBoundsOnlyTheStepItCovers(t *testing.T) {
	store, _, executor, _, status := timedRun(t, s4CascadeBoardFixture(), map[string][]time.Duration{
		"fmn_frame": {31 * time.Minute}, "fmn_research": {31 * time.Minute}, "fmn_ship": {31 * time.Minute},
	}, func(store *Store) { addTimeLimit(t, store, "fmn_frame", 37*60, 0) })
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v: %s", status, eventTypeTrail(mustEvents(t, store, status.RunID)))
	}
	want := map[string][]time.Duration{"fmn_frame": {37 * time.Minute}, "fmn_research": {unlimited}, "fmn_ship": {unlimited}}
	if !reflect.DeepEqual(executor.left, want) {
		t.Fatalf("time given = %v, want %v", executor.left, want)
	}
}

// A step that runs out of time stops there; the run blocks naming the card and
// resumes only with a grant, which gives the card's time again.
func TestAStepTimeCardStopsTheStepAndAGrantGivesItsTimeAgain(t *testing.T) {
	store, engine, executor, _, status := timedRun(t, s4CascadeBoardFixture(), map[string][]time.Duration{
		"fmn_research": {90 * time.Second, 30 * time.Second},
	}, func(store *Store) { addTimeLimit(t, store, "fmn_research", 60, 0) })
	events := mustEvents(t, store, status.RunID)
	if status.Status != RunStatusBlocked || status.ResumePolicy != ResumePolicyGrant {
		t.Fatalf("status = %+v: %s", status, eventTypeTrail(events))
	}
	block := events[len(events)-1]
	if block.Type != RunEventBlocked || stringFromEventData(block, "code") != RunBlockLimitReached || stringFromEventData(block, "reason") != "Research used 1 min of 1 min" {
		t.Fatalf("block = %+v", block)
	}
	if limit := runLimitReached(events, len(events)-1); limit == nil || limit.Kind != LimitKindTime || limit.Used != 60 || limit.Max != 60 {
		t.Fatalf("block limit = %+v", limit)
	}
	if _, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "reattach"}); !errors.Is(err, ErrRunGrantRequired) {
		t.Fatalf("plain resume = %v, want the grant required", err)
	}
	status, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "human:perttu", Mode: "reattach", Reason: "Research needs another minute", Grant: true})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("granted = %+v, %v", status, err)
	}
	if got := executor.left["fmn_research"]; !reflect.DeepEqual(got, []time.Duration{time.Minute, time.Minute}) {
		t.Fatalf("research was given %v, want a minute, then the granted minute", got)
	}
	for _, event := range mustEvents(t, store, status.RunID) {
		if event.Type == RunEventResumed {
			grant, _ := event.Data["grant"].(map[string]any)
			if grant["kind"] != LimitKindTime || intFromRunEventData(grant["amount"]) != 60 || event.Actor != "human:perttu" {
				t.Fatalf("grant = %+v", event)
			}
		}
	}
}

// pausingBoardFixture is the lab check's mission: Input -> A -> human gate
// whose fail sends A back and whose pass ends Done, beside Input -> B -> Done.
func pausingBoardFixture() string {
	return s4MissionOnlyBoardFixture() + branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_b", "B") +
		branchingBoardHumanGate("gate_review") + branchingBoardEnds() +
		branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_review:in") +
		branchingBoardConnection("edge_gate_pass", "gate_review:pass", "end_done:in") +
		branchingBoardConnection("edge_back", "gate_review:fail", "fmn_a:port_fmn_a_in") +
		branchingBoardConnection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in") +
		branchingBoardConnection("edge_b_done", "fmn_b:port_fmn_b_out", "end_done:in")
}

// Mission time counts while any step runs, B included while A's gate waits,
// and pauses once only the gate waits. A's own time does not count the wait,
// so the send-back resumes A with the time it has left.
func TestTimePausesOnlyForWaitingWork(t *testing.T) {
	var stepCard, missionCard LimitNode
	store, engine, executor, clock, status := timedRun(t, pausingBoardFixture(), map[string][]time.Duration{
		"fmn_a": {30 * time.Second, 30 * time.Second}, "fmn_b": {40 * time.Second},
	}, func(store *Store) {
		stepCard = addTimeLimit(t, store, "fmn_a", 100, 0)
		missionCard = addTimeLimit(t, store, "mis_showcase", 3600, 0)
	})
	board := mustReadRunBoard(t, store, status.RunID)
	used := func(limit LimitNode) int {
		return timeUsed(board, mustEvents(t, store, status.RunID), limit, *clock)
	}
	if status.Status != RunStatusRunning || used(missionCard) != 70 || used(stepCard) != 30 {
		t.Fatalf("after A and B: %+v, mission %d s, A %d s", status, used(missionCard), used(stepCard))
	}
	// Only the gate waits for ten minutes: nothing counts.
	*clock = clock.Add(10 * time.Minute)
	if used(missionCard) != 70 || used(stepCard) != 30 {
		t.Fatalf("the wait counted: mission %d s, A %d s", used(missionCard), used(stepCard))
	}
	request, _ := latestHumanRequest(mustEvents(t, store, status.RunID), "gate_review")
	if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", RequestedSeq: request.Seq, Verdict: "fail", Reason: "again", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ContinueRun(status.RunID); err != nil {
		t.Fatal(err)
	}
	if got := executor.left["fmn_a"]; !reflect.DeepEqual(got, []time.Duration{100 * time.Second, 70 * time.Second}) {
		t.Fatalf("A was given %v, want 100 s, then the 70 s it had left", got)
	}
	if got := executor.left["fmn_b"]; !reflect.DeepEqual(got, []time.Duration{3570 * time.Second}) {
		t.Fatalf("B was given %v, want the mission's 3570 s left", got)
	}
	if used(missionCard) != 100 || used(stepCard) != 60 {
		t.Fatalf("after the send-back: mission %d s, A %d s", used(missionCard), used(stepCard))
	}
	// The counted times are the ledger's own: A's two runs and B's.
	events := mustEvents(t, store, status.RunID)
	var spans []time.Duration
	for i, event := range events {
		if event.Type != RunEventNodeStarted || stringFromEventData(event, "nodeKind") != "formation" {
			continue
		}
		start, _ := time.Parse(time.RFC3339Nano, event.Timestamp)
		for _, end := range events[i+1:] {
			if end.Type == RunEventNodeOutput && end.NodeID == event.NodeID {
				at, _ := time.Parse(time.RFC3339Nano, end.Timestamp)
				spans = append(spans, at.Sub(start))
				break
			}
		}
	}
	if !reflect.DeepEqual(spans, []time.Duration{30 * time.Second, 40 * time.Second, 30 * time.Second}) {
		t.Fatalf("ledger spans = %v", spans)
	}
}

// A blocked run counts no time, and a restart gives a started step only the
// time the ledger says it has left.
func TestTimeCountsFromTheLedgerAcrossBlocksAndRestarts(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	addTimeLimit(t, store, "fmn_research", 37, 0)
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	executor := &timedExecutor{clock: &clock}
	engine := NewRunEngine(store, personas, executor)
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.ReadRunBoard(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	formation, _ := findFormation(board.Formations, "fmn_research")
	if err := engine.startFormationExecution(started.RunID, board, formation, RunEvent{Type: RunEventNodeStarted, NodeID: formation.ID, Attempt: 1, Data: map[string]any{"nodeKind": "formation"}}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(15 * time.Second)
	// A restart blocks the run; the minutes it stays blocked count nothing.
	if err := engine.BlockInterruptedRun(started.RunID); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(20 * time.Minute)
	if err := store.AppendRunEvent(started.RunID, RunEvent{Type: RunEventResumed, Data: map[string]any{"resumeMode": "reattach"}}); err != nil {
		t.Fatal(err)
	}
	engine = NewRunEngine(store, personas, executor)
	req := FormationExecution{RunID: started.RunID, NodeID: formation.ID, Formation: formation, Attempt: 1}
	if _, err := engine.executeFormation(req); err != nil {
		t.Fatal(err)
	}
	if got := executor.left[formation.ID]; !reflect.DeepEqual(got, []time.Duration{22 * time.Second}) {
		t.Fatalf("time given after the restart = %v, want the 22 s left", got)
	}
	clock = clock.Add(30 * time.Second)
	var spent *LimitReachedError
	if _, err := engine.executeFormation(req); !errors.As(err, &spent) || spent.Use.Kind != LimitKindTime || spent.Use.Used != 37 {
		t.Fatalf("an attempt past its time = %v", err)
	}
	if len(executor.left[formation.ID]) != 1 {
		t.Fatal("a spent card ran the step again")
	}
	direct := &fakeRunExecutor{}
	engine = NewRunEngine(store, personas, direct)
	engine.SetExecutionContext(func(string) context.Context { return context.Background() })
	if _, err := engine.executeFormation(req); !errors.As(err, &spent) {
		t.Fatalf("a spent card on a coordinator-owned executor = %v", err)
	}
	if len(direct.calls) != 0 {
		t.Fatal("a spent card invoked a coordinator-owned executor")
	}
}

// A time card warns the seats of the step about to run: a step's card each
// attempt, the mission's card once in the whole run.
func TestATimeCardWarnsItsSeatsOnce(t *testing.T) {
	ten, two := 600, 120
	board := &BoardDocument{
		Missions:   []MissionNode{{ID: "mis"}},
		Formations: []FormationNode{{ID: "fmn_work", Title: "Work"}},
		Limits: []LimitNode{
			{ID: "lim_step", Title: "Work clock", Target: "fmn_work", Seconds: &ten, WarnSeconds: &two},
			{ID: "lim_mission", Title: "Mission clock", Target: "mis", Seconds: &ten, WarnSeconds: &two},
		},
	}
	now := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	at := func(seconds int) string {
		return now.Add(time.Duration(seconds) * time.Second).Format(time.RFC3339Nano)
	}
	fresh := []RunEvent{{Seq: 1, Type: RunEventStarted, Timestamp: at(0)}}
	first, warnings := timeBudget(board, fresh, "fmn_work", now)
	if first == nil || first.Max-first.Used != 600 || len(warnings) != 2 {
		t.Fatalf("budget = %+v, warnings %+v", first, warnings)
	}
	if warnings[0].At != now.Add(480*time.Second) || warnings[0].Mission ||
		warnings[0].Text != "Archon: 2 min left of this step's working time (Limit card Work clock). When it runs out the step stops and the run waits for the operator. Finish your output now." {
		t.Fatalf("step warning = %+v", warnings[0])
	}
	if !warnings[1].Mission || !strings.Contains(warnings[1].Text, "the mission's working time (Limit card Mission clock)") {
		t.Fatalf("mission warning = %+v", warnings[1])
	}
	// 550 s used: the warning is due at once and says what is left.
	worked := append(append([]RunEvent{}, fresh...),
		RunEvent{Seq: 2, Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: 1, Timestamp: at(0), Data: map[string]any{"nodeKind": "formation"}},
		RunEvent{Seq: 3, Type: RunEventNodeOutput, NodeID: "fmn_work", Timestamp: at(550)},
		RunEvent{Seq: 4, Type: RunEventLimitWarning, NodeID: "fmn_work", Attempt: 1, SlotID: "slot_a", Timestamp: at(500), Data: map[string]any{"limitId": "lim_mission"}})
	_, warnings = timeBudget(board, worked, "fmn_work", now.Add(600*time.Second))
	if len(warnings) != 1 || warnings[0].LimitID != "lim_step" || warnings[0].At != now.Add(600*time.Second) || !strings.HasPrefix(warnings[0].Text, "Archon: 50 s left") {
		t.Fatalf("warnings after 550 s with the mission's already sent = %+v", warnings)
	}
}

// The ledger keeps each warning to once per seat: a step card's per attempt,
// the mission card's only for the attempt that first sent it.
func TestALimitWarningIsClaimedOncePerSeat(t *testing.T) {
	store, _, _, status := startBranchingRun(t, linearGateBoardFixture(), &fakeRunExecutor{})
	claim := func(node string, attempt int, slot string, warning LimitWarning) bool {
		return store.claimLimitWarning(FormationExecution{RunID: status.RunID, NodeID: node, Attempt: attempt}, slot, warning)
	}
	step := LimitWarning{LimitID: "lim_step", Text: "step"}
	mission := LimitWarning{LimitID: "lim_mission", Mission: true, Text: "mission"}
	for _, tc := range []struct {
		name    string
		node    string
		attempt int
		slot    string
		warning LimitWarning
		want    bool
	}{
		{"a step card's first seat", "fmn_w", 1, "slot_a", step, true},
		{"the same seat again", "fmn_w", 1, "slot_a", step, false},
		{"another seat of the attempt", "fmn_w", 1, "slot_b", step, true},
		{"the step's next attempt", "fmn_w", 2, "slot_a", step, true},
		{"the mission card's first seat", "fmn_w", 1, "slot_a", mission, true},
		{"another seat working then", "fmn_w", 1, "slot_b", mission, true},
		{"a later attempt", "fmn_w", 2, "slot_a", mission, false},
	} {
		if got := claim(tc.node, tc.attempt, tc.slot, tc.warning); got != tc.want {
			t.Errorf("%s: claimed %v, want %v", tc.name, got, tc.want)
		}
	}
}

// warnedSeats records what the executor pastes into seats besides their
// briefs, and holds each turn until its seat was warned.
type warnedSeats struct {
	*fakeTmuxHarnessClient
	mu     sync.Mutex
	pasted map[string][]string
}

func (s *warnedSeats) Stage(ctx context.Context, socket string, seat *nativeSeat, dispatch, text string) error {
	if text == seatPointer(seat.brief) {
		return s.fakeTmuxHarnessClient.Stage(ctx, socket, seat, dispatch, text)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pasted[seat.name] = append(s.pasted[seat.name], text)
	return nil
}

func (s *warnedSeats) WaitTurn(ctx context.Context, seat *nativeSeat, cwd, pointer string, consumed func(codexTranscriptTurn) error) (codexTranscriptTurn, error) {
	turn, err := s.fakeTmuxHarnessClient.WaitTurn(ctx, seat, cwd, pointer, consumed)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		s.mu.Lock()
		warned := len(s.pasted[seat.name]) > 0
		s.mu.Unlock()
		if warned {
			break
		}
	}
	return turn, err
}

// A real seat gets the warning pasted while it works, once, and the ledger
// records it.
func TestATmuxSeatGetsItsTimeWarningPastedOnce(t *testing.T) {
	board := s4RunBoardFixture() + "\n[[limit]]\nid = \"lim_research\"\ntitle = \"Research clock\"\ntarget = \"fmn_research\"\nseconds = 600\nwarnSeconds = 599\n"
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), board)
	cfg := tmuxTestConfig(t)
	seats := &warnedSeats{fakeTmuxHarnessClient: &fakeTmuxHarnessClient{pane: tmuxPaneState{CurrentPath: cfg.Cwd}}, pasted: map[string][]string{}}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, seats)
	status, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_research", FormationRunRequest{Actor: "agent:test"})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v, %v", status, err)
	}
	if len(seats.pasted) != 1 {
		t.Fatalf("pasted = %v, want one seat warned", seats.pasted)
	}
	for _, texts := range seats.pasted {
		if len(texts) != 1 || !strings.HasPrefix(texts[0], "Archon: 9 min 59 s left of this step's working time (Limit card Research clock).") {
			t.Fatalf("pasted = %q", texts)
		}
	}
	warned := 0
	for _, event := range mustEvents(t, store, status.RunID) {
		if event.Type == RunEventLimitWarning && stringFromEventData(event, "limitId") == "lim_research" {
			warned++
		}
	}
	if warned != 1 {
		t.Fatalf("limit_warning events = %d, want one", warned)
	}
}

// A lab step with "archon-lab-work: 1500ms" in its brief takes that long, so a
// lab run rehearses time cards: its seats' warnings are recorded when due, and
// a card shorter than the work stops the step.
func TestALabStepTakesItsWorkTimeAndRecordsItsWarnings(t *testing.T) {
	for _, tc := range []struct {
		name          string
		seconds, warn int
		want          string
	}{
		{"warned and finished", 3, 2, RunStatusSucceeded},
		{"stopped at its time", 1, 0, RunStatusBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), strings.Replace(s4RunBoardFixture(), "title = \"Research\"\n", "title = \"Research\"\n\n[formation.brief]\ngoal = \"Research. archon-lab-work: 1500ms\"\n", 1))
			addTimeLimit(t, store, "fmn_research", tc.seconds, tc.warn)
			lab := NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: t.TempDir(), Harnesses: []string{"openai-codex"}})
			status, err := NewRunEngine(store, personas, lab).RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Actor: "agent:test"})
			if err != nil || status.Status != tc.want {
				t.Fatalf("status = %+v, %v", status, err)
			}
			events := mustEvents(t, store, status.RunID)
			warned := 0
			for _, event := range events {
				if event.Type == RunEventLimitWarning {
					warned++
				}
			}
			if (tc.warn > 0) != (warned == 1) {
				t.Fatalf("warnings = %d: %s", warned, eventTypeTrail(events))
			}
			if tc.want == RunStatusBlocked && stringFromEventData(events[len(events)-1], "reason") != "Research used 1 s of 1 s" {
				t.Fatalf("block = %+v", events[len(events)-1])
			}
		})
	}
}
