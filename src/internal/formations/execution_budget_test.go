package formations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// unlimited is what budgetRecordingExecutor records for a step with no deadline.
const unlimited = time.Duration(-1)

type budgetRecordingExecutor struct {
	fakeRunExecutor
	clock *time.Time
	left  map[string]time.Duration
	work  time.Duration
}

func (e *budgetRecordingExecutor) ExecuteFormationContext(ctx context.Context, req FormationExecution) (FormationExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return FormationExecutionResult{}, err
	}
	if e.left == nil {
		e.left = map[string]time.Duration{}
	}
	deadline, ok := ctx.Deadline()
	if req.Deadline.IsZero() {
		if ok {
			return FormationExecutionResult{}, fmt.Errorf("a step without a duration got a context deadline")
		}
		e.left[req.NodeID] = unlimited
	} else {
		e.left[req.NodeID] = req.Deadline.Sub(*e.clock)
		if !ok || time.Until(deadline) > e.left[req.NodeID] || time.Until(deadline) < e.left[req.NodeID]-time.Second {
			return FormationExecutionResult{}, fmt.Errorf("context deadline does not follow admitted budget")
		}
	}
	*e.clock = e.clock.Add(e.work)
	return e.fakeRunExecutor.ExecuteFormation(req)
}

// A step runs as long as it takes unless its mission authored a duration. The
// clock moves 31 minutes per step, past the 30-minute default archond used to
// impose, and the steps without a duration still succeed.
func TestExecutionBudgetFreezesAuthoredDurationsAndLeavesOthersUnlimited(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4CascadeBoardFixture())
	if _, err := setTestExecutionPolicy(t, store, "fmn_frame", 37*60); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	executor := &budgetRecordingExecutor{clock: &clock, work: 31 * time.Minute}
	engine := NewRunEngine(store, personas, executor)
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
	if err != nil {
		t.Fatal(err)
	}
	// The mission changes after admission; the run keeps its snapshot.
	if _, err := setTestExecutionPolicy(t, store, "fmn_frame", 211); err != nil {
		t.Fatal(err)
	}
	if _, err := setTestExecutionPolicy(t, store, "fmn_research", 83); err != nil {
		t.Fatal(err)
	}
	status, err := NewRunEngine(store, personas, executor).ExecuteStartedMission(started.RunID)
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("old run: %+v %v", status, err)
	}
	for id, want := range map[string]time.Duration{"fmn_frame": 37 * time.Minute, "fmn_research": unlimited, "fmn_ship": unlimited} {
		if executor.left[id] != want {
			t.Fatalf("%s got %s, want %s", id, executor.left[id], want)
		}
	}
	events := mustEvents(t, store, started.RunID)
	limits, _ := events[0].Data["limits"].(map[string]any)
	if _, ok := limits["formationTimeoutSeconds"]; ok {
		t.Fatalf("run_started froze a default step duration: %v", limits)
	}
	for _, event := range events {
		if event.Type != RunEventNodeStarted || stringFromEventData(event, "nodeKind") != "formation" {
			continue
		}
		recorded := stringFromEventData(event, "executionDeadline")
		if executor.left[event.NodeID] == unlimited {
			if recorded != "" || event.Data["executionTimeoutSeconds"] != nil {
				t.Fatalf("a step without a duration recorded a deadline: %+v", event)
			}
			continue
		}
		start, _ := time.Parse(time.RFC3339Nano, event.Timestamp)
		deadline, err := time.Parse(time.RFC3339Nano, recorded)
		if err != nil || deadline.Sub(start) != executor.left[event.NodeID] {
			t.Fatalf("durable deadline = %+v %v", event, err)
		}
	}
	executor.work = time.Second
	status, err = engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase"})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("fresh run: %+v %v", status, err)
	}
	if executor.left["fmn_frame"] != 211*time.Second || executor.left["fmn_research"] != 83*time.Second || executor.left["fmn_ship"] != unlimited {
		t.Fatalf("fresh durations = %v", executor.left)
	}
}

func TestIsolatedAdmissionFreezesFormationBudget(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	executor := &budgetRecordingExecutor{clock: &clock}
	engine := NewRunEngine(store, personas, executor)
	if _, err := setTestExecutionPolicy(t, store, "fmn_research", 73); err != nil {
		t.Fatal(err)
	}
	_, execute, err := engine.PrepareFormationRun("session-search", "fmn_research", FormationRunRequest{})
	if err != nil {
		t.Fatal(err)
	}
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
	if _, err := setTestExecutionPolicy(t, store, "fmn_research", 37); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	store.Now = func() time.Time { return clock }
	executor := &budgetRecordingExecutor{clock: &clock}
	engine := NewRunEngine(store, personas, executor)
	limits := RunLimits{MaxDispatch: 5}
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	formation, _ := findFormation(board.Formations, "fmn_research")
	if err := engine.startFormationExecution(started.RunID, formation, limits, RunEvent{Type: RunEventNodeStarted, NodeID: formation.ID, Attempt: 1, Data: map[string]any{"nodeKind": "formation"}}); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(15 * time.Second)
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
	direct := &fakeRunExecutor{}
	engine = NewRunEngine(store, personas, direct)
	engine.SetExecutionContext(func(string) context.Context { return context.Background() })
	if _, err := engine.executeFormation(req, limits); !errors.Is(err, ErrFormationTimeoutExceeded) {
		t.Fatalf("expired direct execution: %v", err)
	}
	if len(direct.calls) != 0 {
		t.Fatal("expired allocation invoked a coordinator-owned executor")
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
	}{{0, time.Time{}, 0}, {89, time.Time{}, 89}, {89, now.Add(11 * time.Second), 11}} {
		req := FormationExecution{Deadline: tc.deadline}
		if tc.override > 0 {
			req.Formation.Execution = &FormationExecutionPolicy{TimeoutSeconds: tc.override}
		}
		ctx, cancel, err := withFormationDeadline(context.Background(), &req, now)
		if err != nil {
			t.Fatal(err)
		}
		deadline, ok := ctx.Deadline()
		if tc.want == 0 {
			// No authored duration: the step has no deadline at all.
			if !req.Deadline.IsZero() || ok {
				t.Fatalf("a step without a duration got deadline %v", req.Deadline)
			}
			cancel()
			continue
		}
		if req.Deadline.Sub(now) != time.Duration(tc.want)*time.Second {
			t.Fatalf("deadline %v", req.Deadline)
		}
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
