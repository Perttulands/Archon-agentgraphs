package formations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type budgetRecordingExecutor struct {
	fakeRunExecutor
	fallback int
	clock    *time.Time
	left     map[string]time.Duration
	work     time.Duration
}

func (e *budgetRecordingExecutor) DefaultFormationTimeoutSeconds() int { return e.fallback }
func (e *budgetRecordingExecutor) ExecuteFormationContext(ctx context.Context, req FormationExecution) (FormationExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return FormationExecutionResult{}, err
	}
	if e.left == nil {
		e.left = map[string]time.Duration{}
	}
	e.left[req.NodeID] = req.Deadline.Sub(*e.clock)
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > e.left[req.NodeID] || time.Until(deadline) < e.left[req.NodeID]-time.Second {
		return FormationExecutionResult{}, fmt.Errorf("context deadline does not follow admitted budget")
	}
	*e.clock = e.clock.Add(e.work)
	return e.fakeRunExecutor.ExecuteFormation(req)
}

func TestExecutionBudgetFreezesOverridesAndInheritedDefault(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4CascadeBoardFixture())
	for id, seconds := range map[string]int{"fmn_frame": 37, "fmn_research": 83} {
		if _, err := setTestExecutionPolicy(t, store, id, seconds); err != nil {
			t.Fatal(err)
		}
	}
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	executor := &budgetRecordingExecutor{fallback: 127, clock: &clock, work: time.Second}
	engine := NewRunEngine(store, personas, executor)
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas, Limits: engine.AdmissionLimits(RunLimits{MaxDispatch: 5})})
	if err != nil {
		t.Fatal(err)
	}
	// Both the board and the daemon configuration change after admission.
	if _, err := setTestExecutionPolicy(t, store, "fmn_frame", 211); err != nil {
		t.Fatal(err)
	}
	executor.fallback = 419
	status, err := NewRunEngine(store, personas, executor).ExecuteStartedMission(started.RunID)
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("old run: %+v %v", status, err)
	}
	for id, seconds := range map[string]int{"fmn_frame": 37, "fmn_research": 83, "fmn_ship": 127} {
		if executor.left[id] != time.Duration(seconds)*time.Second {
			t.Fatalf("%s got %s", id, executor.left[id])
		}
	}
	events := mustEvents(t, store, started.RunID)
	if got := runLimitsFromEvent(events[0]).FormationTimeoutSeconds; got != 127 {
		t.Fatalf("frozen default = %d", got)
	}
	for _, event := range events {
		if event.Type != RunEventNodeStarted || stringFromEventData(event, "nodeKind") != "formation" {
			continue
		}
		start, _ := time.Parse(time.RFC3339Nano, event.Timestamp)
		deadline, err := time.Parse(time.RFC3339Nano, stringFromEventData(event, "executionDeadline"))
		if err != nil || deadline.Sub(start) != executor.left[event.NodeID] {
			t.Fatalf("durable deadline = %+v %v", event, err)
		}
	}
	status, err = engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Limits: RunLimits{MaxDispatch: 5}})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("fresh run: %+v %v", status, err)
	}
	if executor.left["fmn_frame"] != 211*time.Second || executor.left["fmn_ship"] != 419*time.Second {
		t.Fatalf("fresh defaults = %v", executor.left)
	}
}

func TestIsolatedAdmissionFreezesFormationBudget(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	executor := &budgetRecordingExecutor{fallback: 73, clock: &clock}
	engine := NewRunEngine(store, personas, executor)
	_, execute, err := engine.PrepareFormationRun("session-search", "fmn_research", FormationRunRequest{})
	if err != nil {
		t.Fatal(err)
	}
	executor.fallback = 2
	if _, err := setTestExecutionPolicy(t, store, "fmn_research", 3); err != nil {
		t.Fatal(err)
	}
	status, err := execute()
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("status %+v %v", status, err)
	}
	if executor.left["fmn_research"] != 73*time.Second {
		t.Fatalf("allocation = %v", executor.left)
	}
}

func TestFormationBudgetUsesOriginalStartAfterRestart(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	executor := &budgetRecordingExecutor{fallback: 37, clock: &clock}
	engine := NewRunEngine(store, personas, executor)
	limits := engine.AdmissionLimits(RunLimits{MaxDispatch: 5})
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	formation := board.Formations[0]
	if err := engine.startFormationExecution(started.RunID, formation, limits, RunEvent{Type: RunEventNodeStarted, NodeID: formation.ID, Attempt: 1, Data: map[string]any{"nodeKind": "formation"}}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(15 * time.Second)
	executor.fallback = 999
	engine = NewRunEngine(store, personas, executor)
	req := FormationExecution{RunID: started.RunID, NodeID: formation.ID, Formation: formation, Attempt: 1}
	if _, err := engine.executeFormation(req, runLimitsFromEvent(mustEvents(t, store, started.RunID)[0])); err != nil {
		t.Fatal(err)
	}
	if executor.left[formation.ID] != 22*time.Second {
		t.Fatalf("remaining %v", executor.left)
	}
	clock = clock.Add(30 * time.Second)
	if _, err := engine.executeFormation(req, limits); !errors.Is(err, ErrFormationTimeoutExceeded) {
		t.Fatalf("expired result=%v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatal("expired allocation executed again")
	}
	legacy := &fakeRunExecutor{}
	engine = NewRunEngine(store, personas, legacy)
	engine.SetExecutionContext(func(string) context.Context { return context.Background() })
	if _, err := engine.executeFormation(req, limits); !errors.Is(err, ErrFormationTimeoutExceeded) {
		t.Fatalf("expired legacy execution: %v", err)
	}
	if len(legacy.calls) != 0 {
		t.Fatal("expired allocation invoked a coordinator-owned legacy executor")
	}
}

func TestFormationBudgetComposesWithRunDeadline(t *testing.T) {
	start := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	events := []RunEvent{{Type: RunEventStarted, Timestamp: start.Format(time.RFC3339Nano)}, {Type: RunEventNodeStarted, NodeID: "work", Attempt: 1, Timestamp: start.Add(10 * time.Second).Format(time.RFC3339Nano)}}
	for _, tc := range []struct {
		formation, run, left int
		cause                error
	}{{37, 20, 5, ErrRunWallClockExceeded}, {7, 100, 2, ErrFormationTimeoutExceeded}} {
		req := FormationExecution{NodeID: "work", Attempt: 1, Formation: FormationNode{Execution: &FormationExecutionPolicy{TimeoutSeconds: tc.formation}}}
		now := start.Add(15 * time.Second)
		budget, err := formationExecutionBudget(req, events, RunLimits{WallClockSeconds: tc.run}, now)
		if err != nil || budget.deadline.Sub(now) != time.Duration(tc.left)*time.Second || !errors.Is(budget.cause, tc.cause) {
			t.Fatalf("budget=%+v error=%v", budget, err)
		}
	}
}

func TestDirectExecutorBudgetHonorsOverrideAndExistingDeadline(t *testing.T) {
	now := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		override int
		deadline time.Time
		want     int
	}{{0, time.Time{}, 43}, {89, time.Time{}, 89}, {89, now.Add(11 * time.Second), 11}} {
		req := FormationExecution{Deadline: tc.deadline}
		if tc.override > 0 {
			req.Formation.Execution = &FormationExecutionPolicy{TimeoutSeconds: tc.override}
		}
		ctx, cancel, err := withFormationDeadline(context.Background(), &req, now, 43)
		if err != nil {
			t.Fatal(err)
		}
		if req.Deadline.Sub(now) != time.Duration(tc.want)*time.Second {
			t.Fatalf("deadline %v", req.Deadline)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > time.Duration(tc.want)*time.Second {
			t.Fatal("wrong context deadline")
		}
		cancel()
	}
}

func TestMalformedExecutionPoliciesRejectAdmission(t *testing.T) {
	for _, policy := range []string{`execution = "wrong"`, `execution = [{timeoutSeconds = 37}]`, `execution = {timeoutSeconds = "37"}`, `execution = {timeoutSeconds = 0}`, `execution = {timeoutSeconds = 1.5}`, "[formation.execution]\ntimeoutSeconds = \"37\"", "[formation.execution]\ntimeoutSeconds = -1", "[[formation.execution]]\ntimeoutSeconds = 37"} {
		t.Run(policy, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), strings.Replace(s4RunBoardFixture(), "title = \"Research\"", "title = \"Research\"\n"+policy, 1))
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatal(err)
			}
			if len(executionPolicyFindings(board)) == 0 {
				t.Fatal("malformed policy lost in compatibility parser")
			}
			if _, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas}); !errors.Is(err, ErrInvalidExecutionPolicy) {
				t.Fatalf("admission error = %v", err)
			}
		})
	}
}

func setTestExecutionPolicy(t *testing.T, store *Store, node string, seconds int) (*BoardDocument, error) {
	t.Helper()
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	return store.SetFormationExecutionPolicy("session-search", FormationExecutionPolicyRequest{FormationID: node, TimeoutSeconds: seconds}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
}

type budgetTmuxClient struct {
	*fakeTmuxHarnessClient
	left time.Duration
}

func (c *budgetTmuxClient) Create(ctx context.Context, socket, name, cwd, root string, variant HarnessVariant) (*nativeSeat, error) {
	if deadline, ok := ctx.Deadline(); ok {
		c.left = time.Until(deadline)
	}
	return c.fakeTmuxHarnessClient.Create(ctx, socket, name, cwd, root, variant)
}
func TestTmuxReceivesAuthoredBudgetWithoutDefaultCap(t *testing.T) {
	for _, runSeconds := range []int{0, 17} {
		t.Run(fmt.Sprint(runSeconds), func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
			if _, err := setTestExecutionPolicy(t, store, "fmn_research", 83); err != nil {
				t.Fatal(err)
			}
			cfg := tmuxTestConfig(t)
			cfg.TimeoutSeconds = 1
			client := &budgetTmuxClient{fakeTmuxHarnessClient: &fakeTmuxHarnessClient{pane: tmuxPaneState{CurrentPath: cfg.Cwd}}}
			executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
			status, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_research", FormationRunRequest{Limits: RunLimits{WallClockSeconds: runSeconds}})
			if err != nil || status.Status != RunStatusSucceeded {
				t.Fatalf("status %+v %v", status, err)
			}
			want := 83 * time.Second
			if runSeconds > 0 {
				want = time.Duration(runSeconds) * time.Second
			}
			if client.left > want || client.left < want-time.Second {
				t.Fatalf("seat received %v, want %v", client.left, want)
			}
		})
	}
}
