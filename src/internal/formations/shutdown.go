package formations

import (
	"context"
	"errors"
)

var ErrCoordinatorShutdown = errors.New("coordinator shutting down; dispatch preserved for restart")

type shutdownKey struct{}

// WithShutdownFence allows an active turn to finish during the owner's grace
// period, while preventing subsequent dispatches and destructive seat cleanup.
func WithShutdownFence(ctx context.Context, stopping <-chan struct{}) context.Context {
	return context.WithValue(ctx, shutdownKey{}, stopping)
}

func shutdownRequested(ctx context.Context) bool {
	stopping, _ := ctx.Value(shutdownKey{}).(<-chan struct{})
	select {
	case <-stopping:
		return true
	default:
		return false
	}
}

func dispatchContextError(ctx context.Context) error {
	if shutdownRequested(ctx) {
		return ErrCoordinatorShutdown
	}
	return ctx.Err()
}
