package formations

import (
	"context"
	"errors"
	"testing"
	"time"
)

type cleanupExecutor struct{ cleanupDone bool }

func (e *cleanupExecutor) ExecuteFormation(FormationExecution) (FormationExecutionResult, error) {
	panic("context-aware path required")
}
func (e *cleanupExecutor) ExecuteFormationContext(ctx context.Context, _ FormationExecution) (FormationExecutionResult, error) {
	<-ctx.Done()
	e.cleanupDone = true
	return FormationExecutionResult{}, ctx.Err()
}

func TestContextExecutorDeadlineJoinsCleanup(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4CascadeBoardFixture())
	store.Now = func() time.Time { return time.Now().Add(-2 * time.Second) }
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas, Limits: RunLimits{MaxDispatch: 3, WallClockSeconds: 1}})
	if err != nil {
		t.Fatal(err)
	}
	store.Now = nil // the run started two seconds ago
	executor := &cleanupExecutor{}
	engine := NewRunEngine(store, personas, executor)
	_, err = engine.executeFormation(FormationExecution{RunID: started.RunID}, RunLimits{WallClockSeconds: 1})
	if !errors.Is(err, ErrRunWallClockExceeded) || !executor.cleanupDone {
		t.Fatalf("deadline returned before cleanup: err %v done %t", err, executor.cleanupDone)
	}
}
