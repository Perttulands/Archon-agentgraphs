package formations

import (
	"context"
	"errors"
	"time"
)

// ErrFormationTimeoutExceeded is the cause that ends a step whose Limit card
// time ran out; the engine blocks the run at that card (archon-o7p.8).
var ErrFormationTimeoutExceeded = errors.New("formation execution time limit exceeded")

// withFormationDeadline bounds an executor's work by the engine's deadline, the
// time the step's Limit cards leave it. A step no card times has none.
func withFormationDeadline(parent context.Context, req *FormationExecution, now time.Time) (context.Context, context.CancelFunc, error) {
	if req.Deadline.IsZero() {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, nil
	}
	ctx, cancel := context.WithTimeoutCause(parent, req.Deadline.Sub(now), ErrFormationTimeoutExceeded)
	return ctx, cancel, nil
}
