package formations

import (
	"context"
	"errors"
	"testing"
)

type cleanupExecutor struct {
	cleanupDone bool
	onEnter     func()
}

func (e *cleanupExecutor) ExecuteFormation(FormationExecution) (FormationExecutionResult, error) {
	panic("context-aware path required")
}
func (e *cleanupExecutor) ExecuteFormationContext(ctx context.Context, _ FormationExecution) (FormationExecutionResult, error) {
	if e.onEnter != nil {
		e.onEnter()
	}
	<-ctx.Done()
	e.cleanupDone = true
	return FormationExecutionResult{}, ctx.Err()
}

func TestContextExecutorDeadlineJoinsCleanup(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4CascadeBoardFixture())
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas, Limits: RunLimits{MaxDispatch: 3, WallClockSeconds: 60}})
	if err != nil {
		t.Fatal(err)
	}
	// Expiry before dispatch must not invoke the executor. Here it happens only
	// after entry, so the engine must join native cleanup before returning.
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	executor := &cleanupExecutor{onEnter: func() { cancel(ErrRunWallClockExceeded) }}
	engine := NewRunEngine(store, personas, executor)
	engine.SetExecutionContext(func(string) context.Context { return ctx })
	_, err = engine.executeFormation(FormationExecution{RunID: started.RunID}, RunLimits{WallClockSeconds: 60})
	if !errors.Is(err, ErrRunWallClockExceeded) || !executor.cleanupDone {
		t.Fatalf("deadline returned before cleanup: err %v done %t", err, executor.cleanupDone)
	}
}
