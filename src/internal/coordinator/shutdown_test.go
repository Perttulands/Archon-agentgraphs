package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/chrote-agent-formations/internal/formations"
)

type pausedBody struct {
	*strings.Reader
	entered chan struct{}
	proceed chan struct{}
}

func (b *pausedBody) Read(p []byte) (int, error) {
	select {
	case <-b.entered:
	default:
		close(b.entered)
	}
	<-b.proceed
	return b.Reader.Read(p)
}

func TestShutdownRetainsWriterForAdmittedAuthoringRequest(t *testing.T) {
	c, e, root := fixture(t)
	c.shutdownGrace = 10 * time.Millisecond
	c.shutdownTimeout = 40 * time.Millisecond
	body := &pausedBody{Reader: strings.NewReader(`{"title":"During shutdown","slug":"late-board"}`), entered: make(chan struct{}), proceed: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/formations/boards", body))
	}()
	<-body.entered
	if err := c.Close(); err == nil {
		t.Fatal("active authoring request was not retained")
	}
	if next, err := Open(root, c.personas, func(*formations.Store) formations.FormationExecutor { return e }); err == nil {
		next.Close()
		t.Fatal("writer released while authoring active")
	}
	close(body.proceed)
	<-done
	select {
	case <-c.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("authoring did not release shutdown")
	}
}

type shutdownSeat struct {
	store   *formations.Store
	entered chan string
}

func (e *shutdownSeat) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	return e.ExecuteFormationContext(context.Background(), req)
}
func (e *shutdownSeat) ExecuteFormationContext(ctx context.Context, req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	lease, err := formations.NewSlotDispatcher(e.store, nil).DispatchSlot(req.RunID, formations.SlotDispatchRequest{NodeID: req.NodeID, SlotID: req.Formation.Slots[0].ID, Attempt: req.Attempt, Prompt: "unfinished"})
	if err != nil {
		return formations.FormationExecutionResult{}, err
	}
	e.entered <- lease.DispatchID
	<-ctx.Done()
	if !errors.Is(context.Cause(ctx), formations.ErrCoordinatorShutdown) {
		return formations.FormationExecutionResult{}, errors.New("shutdown used abort cancellation")
	}
	return formations.FormationExecutionResult{}, ctx.Err()
}

func TestShutdownDetachesWithinBudgetAndRestartNamesDispatch(t *testing.T) {
	c, _, root := fixture(t)
	seat := &shutdownSeat{store: c.store, entered: make(chan string, 1)}
	c.engine = formations.NewRunEngine(c.store, c.personas, seat)
	c.engine.SetExecutionContext(func(id string) context.Context { c.mu.Lock(); defer c.mu.Unlock(); return c.state(id).ctx })
	id := startRun(t, c)
	dispatch := <-seat.entered
	started := time.Now()
	c.BeginShutdown()
	if w := post(t, c, "/api/formations/runs", `{}`); w.Code != 503 {
		t.Fatalf("admission during shutdown: %d", w.Code)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 5*time.Second || elapsed >= 10*time.Second {
		t.Fatalf("shutdown duration %s", elapsed)
	}
	p := awaitState(t, c, id, "blocked")
	if p.Final || !p.ResumeAllowed {
		t.Fatal(p)
	}
	events, _ := c.store.ReadRunEvents(id)
	raw, _ := json.Marshal(events[len(events)-1])
	if !strings.Contains(string(raw), dispatch) {
		t.Fatalf("open dispatch lost: %s", raw)
	}
	next, err := Open(root, c.personas, func(store *formations.Store) formations.FormationExecutor {
		return formations.NewLabFormationExecutor(store, c.personas, formations.LabExecutorConfig{})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := next.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	p = awaitState(t, next, id, "blocked")
	if !p.ResumeAllowed || p.Final {
		t.Fatal(p)
	}
}

func TestShutdownFencesDownstreamDuringGrace(t *testing.T) {
	c, e, _ := fixture(t)
	// Replace the human pause with an automatic code gate to expose scheduling.
	board := strings.Replace(testBoard, `kinds = ["human"]`, `kinds = ["code"]`+"\ncheck = \"output_absent\"\ncheckVersion = \"1\"\ncheckValue = \"FORBIDDEN\"", 1)
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(board), 0600); err != nil {
		t.Fatal(err)
	}
	id := startRun(t, c)
	<-e.entered
	c.BeginShutdown()
	e.proceed <- struct{}{}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case node := <-e.entered:
		t.Fatalf("downstream dispatched during shutdown: %s", node)
	default:
	}
	p := awaitState(t, c, id, "blocked")
	if !p.ResumeAllowed {
		t.Fatal(p)
	}
}

func TestShutdownTimeoutRetainsWriterUntilWorkerReturns(t *testing.T) {
	c, e, root := fixture(t)
	c.shutdownGrace = 10 * time.Millisecond
	c.shutdownTimeout = 40 * time.Millisecond
	id := startRun(t, c)
	<-e.entered
	if err := c.Close(); err == nil {
		t.Fatal("uncooperative executor exceeded budget without error")
	}
	if next, err := Open(root, c.personas, func(*formations.Store) formations.FormationExecutor { return e }); err == nil {
		next.Close()
		t.Fatal("writer lock released while old executor still active")
	}
	e.proceed <- struct{}{}
	select {
	case <-c.shutdownDone:
	case <-time.After(time.Second):
		t.Fatal("worker never settled")
	}
	next, err := Open(root, c.personas, func(*formations.Store) formations.FormationExecutor { return e })
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	p, err := next.Project(id)
	if err != nil || p.Final {
		t.Fatalf("post-timeout ledger %+v %v", p, err)
	}
}
