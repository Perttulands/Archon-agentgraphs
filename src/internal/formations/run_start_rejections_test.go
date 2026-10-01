package formations

import (
	"errors"
	"path/filepath"
	"testing"
)

// A start that names a missing or invalid definition fails with that reason
// and has no effects.
func TestRunStartsRejectInvalidDefinitionsWithoutEffects(t *testing.T) {
	tests := []struct {
		name      string
		slug      string
		board     string
		malformed bool
		wantErr   error
		start     func(*Store, *RunEngine) error
	}{
		{
			name:    "store missing definition",
			slug:    "missing",
			wantErr: ErrNotFound,
			start: func(store *Store, _ *RunEngine) error {
				_, err := store.StartRun("missing", RunStartRequest{MissionID: "mis_showcase"})
				return err
			},
		},
		{
			name:      "store malformed definition",
			slug:      "malformed",
			board:     malformedRuntimeStartBoardFixture(),
			malformed: true,
			start: func(store *Store, _ *RunEngine) error {
				_, err := store.StartRun("malformed", RunStartRequest{MissionID: "mis_showcase"})
				return err
			},
		},
		{
			name:    "store missing mission",
			slug:    "session-search",
			board:   s4RunBoardFixture(),
			wantErr: ErrNotFound,
			start: func(store *Store, _ *RunEngine) error {
				_, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_missing"})
				return err
			},
		},
		{
			name:    "engine mission missing definition",
			slug:    "missing",
			wantErr: ErrNotFound,
			start: func(_ *Store, engine *RunEngine) error {
				_, err := engine.RunMission("missing", RunStartRequest{MissionID: "mis_showcase"})
				return err
			},
		},
		{
			name:      "engine mission malformed definition",
			slug:      "malformed",
			board:     malformedRuntimeStartBoardFixture(),
			malformed: true,
			start: func(_ *Store, engine *RunEngine) error {
				_, err := engine.RunMission("malformed", RunStartRequest{MissionID: "mis_showcase"})
				return err
			},
		},
		{
			name:    "engine mission missing root",
			slug:    "session-search",
			board:   s4RunBoardFixture(),
			wantErr: ErrNotFound,
			start: func(_ *Store, engine *RunEngine) error {
				_, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_missing"})
				return err
			},
		},
		{
			name:      "engine formation malformed definition",
			slug:      "malformed",
			board:     malformedRuntimeStartBoardFixture(),
			malformed: true,
			start: func(_ *Store, engine *RunEngine) error {
				_, err := engine.RunFormation("malformed", "fmn_work", FormationRunRequest{})
				return err
			},
		},
		{
			name:    "engine formation missing root",
			slug:    "session-search",
			board:   s4RunBoardFixture(),
			wantErr: ErrNotFound,
			start: func(_ *Store, engine *RunEngine) error {
				_, err := engine.RunFormation("session-search", "fmn_missing", FormationRunRequest{})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			store := NewStore(workspace)
			if test.board != "" {
				writeFixture(t, store.BoardPath(test.slug), test.board)
			}
			if test.malformed {
				if _, err := store.ReadBoard(test.slug); err == nil {
					t.Fatalf("malformed fixture read error = %v, want definition parse rejection", err)
				}
			}
			executor := &countingFormationExecutor{}
			evaluator := &countingGateEvaluator{}
			engine := NewRunEngine(store, nil, executor)
			engine.SetGateEvaluator(evaluator)

			err := test.start(store, engine)
			if err == nil {
				t.Fatal("start accepted an invalid definition")
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("start error = %v, want %v", err, test.wantErr)
			}
			if executor.calls != 0 || evaluator.calls != 0 {
				t.Fatalf("start effects = executor:%d evaluator:%d, want zero", executor.calls, evaluator.calls)
			}
			if matches := mustGlob(t, filepath.Join(workspace, ".archon", "runs", "*")); len(matches) != 0 {
				t.Fatalf("rejected start created run artifacts: %v", matches)
			}
		})
	}
}

func malformedRuntimeStartBoardFixture() string {
	return `schema = 2
id = "brd_malformed"
slug = "malformed"
title = "Malformed Tool definition"
rev = 1

[tool]
id = "tool_malformed"
`
}

type countingFormationExecutor struct {
	calls int
}

type countingGateEvaluator struct {
	calls int
}

func (e *countingGateEvaluator) EvaluateGate(GateEvaluation) (GateEvaluationResult, error) {
	e.calls++
	return GateEvaluationResult{}, nil
}

func (e *countingFormationExecutor) ExecuteFormation(FormationExecution) (FormationExecutionResult, error) {
	e.calls++
	return FormationExecutionResult{}, nil
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return matches
}
