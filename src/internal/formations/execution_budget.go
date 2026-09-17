package formations

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrFormationTimeoutExceeded = errors.New("formation execution time limit exceeded")

type formationTimeoutProvider interface {
	DefaultFormationTimeoutSeconds() int
}

// AdmissionLimits freezes the executor default before the run is recorded.
// A formation's authored override is already frozen in the board snapshot.
func (e *RunEngine) AdmissionLimits(limits RunLimits) RunLimits {
	limits.FormationTimeoutSeconds = 0
	if provider, ok := e.executor.(formationTimeoutProvider); ok {
		limits.FormationTimeoutSeconds = provider.DefaultFormationTimeoutSeconds()
	}
	return limits
}

func (e *TmuxFormationExecutor) DefaultFormationTimeoutSeconds() int {
	return e.config.TimeoutSeconds
}

func formationExecutionSeconds(formation FormationNode, fallback int) (int, error) {
	seconds := fallback
	if formation.Execution != nil {
		seconds = formation.Execution.TimeoutSeconds
	}
	if seconds == 0 && formation.Execution == nil {
		return 0, nil
	}
	if !validExecutionSeconds(seconds) {
		return 0, fmt.Errorf("%w: formation duration must be positive whole seconds", ErrInvalidExecutionPolicy)
	}
	return seconds, nil
}

type executionBudget struct {
	deadline time.Time
	cause    error
}

func formationExecutionBudget(req FormationExecution, events []RunEvent, limits RunLimits, now time.Time) (executionBudget, error) {
	var budget executionBudget
	seconds, err := formationExecutionSeconds(req.Formation, limits.FormationTimeoutSeconds)
	if err != nil {
		return budget, err
	}
	if seconds > 0 {
		var started *RunEvent
		for i := len(events) - 1; i >= 0; i-- {
			event := &events[i]
			if event.Type == RunEventNodeStarted && event.NodeID == req.NodeID && event.Attempt == req.Attempt {
				started = event
				break
			}
		}
		if started == nil {
			return budget, fmt.Errorf("%w: formation execution start is missing", ErrRunLedgerInvalid)
		}
		start, err := time.Parse(time.RFC3339Nano, started.Timestamp)
		if err != nil {
			return budget, fmt.Errorf("%w: formation execution start time is invalid", ErrRunLedgerInvalid)
		}
		budget.deadline = start.Add(time.Duration(seconds) * time.Second)
		if recorded := stringFromEventData(*started, "executionDeadline"); recorded != "" {
			deadline, err := time.Parse(time.RFC3339Nano, recorded)
			if err != nil || !deadline.Equal(budget.deadline) {
				return budget, fmt.Errorf("%w: formation execution deadline does not match its admitted budget", ErrRunLedgerInvalid)
			}
		}
		budget.cause = ErrFormationTimeoutExceeded
	}
	if limits.WallClockSeconds > 0 {
		deadline, err := wallClockDeadline(events, limits.WallClockSeconds, now)
		if err != nil {
			return budget, err
		}
		if budget.deadline.IsZero() || !budget.deadline.Before(deadline) {
			budget.deadline, budget.cause = deadline, ErrRunWallClockExceeded
		}
	}
	return budget, nil
}

// Direct legacy executor calls have no engine-provided deadline. Give those
// calls one allocation from the configured default or authored policy. Normal
// admitted runs arrive with their ledger-derived deadline and retain it.
func withFormationDeadline(parent context.Context, req *FormationExecution, now time.Time, fallback int) (context.Context, context.CancelFunc, error) {
	if req.Deadline.IsZero() {
		seconds, err := formationExecutionSeconds(req.Formation, fallback)
		if err != nil {
			return nil, nil, err
		}
		if seconds > 0 {
			req.Deadline = now.Add(time.Duration(seconds) * time.Second)
		}
	}
	if req.Deadline.IsZero() {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, nil
	}
	ctx, cancel := context.WithTimeoutCause(parent, req.Deadline.Sub(now), ErrFormationTimeoutExceeded)
	return ctx, cancel, nil
}
