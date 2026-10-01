package formations

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrFormationTimeoutExceeded = errors.New("formation execution time limit exceeded")

// formationExecutionSeconds is the step's authored duration, frozen in the
// run's mission snapshot. A step without one has no time limit.
func formationExecutionSeconds(formation FormationNode) (int, error) {
	if formation.Execution == nil {
		return 0, nil
	}
	seconds := formation.Execution.TimeoutSeconds
	if !validExecutionSeconds(seconds) {
		return 0, fmt.Errorf("%w: formation duration must be positive whole seconds", ErrInvalidExecutionPolicy)
	}
	return seconds, nil
}

type executionBudget struct {
	deadline time.Time
	cause    error
}

func formationExecutionBudget(req FormationExecution, events []RunEvent, now time.Time) (executionBudget, error) {
	var budget executionBudget
	seconds, err := formationExecutionSeconds(req.Formation)
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
	return budget, nil
}

// A direct executor call has no engine-provided deadline; it gets one
// allocation of the step's authored duration, if it has one. Admitted runs
// arrive with their ledger-derived deadline and retain it.
func withFormationDeadline(parent context.Context, req *FormationExecution, now time.Time) (context.Context, context.CancelFunc, error) {
	if req.Deadline.IsZero() {
		seconds, err := formationExecutionSeconds(req.Formation)
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
