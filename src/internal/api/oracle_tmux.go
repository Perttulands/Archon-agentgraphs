package api

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/Perttulands/chrote-agent-formations/internal/core"
)

type oracleTmuxRunner interface {
	Run(args ...string) (string, error)
}

type realOracleTmuxRunner struct{}

type oracleTmuxError struct {
	cause      error
	diagnostic string
}

func (e *oracleTmuxError) Error() string {
	return "tmux command failed: " + e.cause.Error()
}

func (e *oracleTmuxError) Unwrap() error {
	return e.cause
}

func (realOracleTmuxRunner) Run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, core.TmuxBin(), args...)
	cmd.Env = core.GetTmuxEnv()
	output, err := cmd.Output()
	if err == nil {
		return string(output), nil
	}

	cause := err
	if ctx.Err() != nil {
		cause = ctx.Err()
	}
	diagnostic := ""
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		diagnostic = strings.TrimSpace(string(exitErr.Stderr))
	}
	return "", &oracleTmuxError{cause: cause, diagnostic: diagnostic}
}

func tmuxErrorDiagnostic(err error) string {
	var commandErr *oracleTmuxError
	if errors.As(err, &commandErr) {
		return commandErr.diagnostic
	}
	if err == nil {
		return ""
	}
	return err.Error()
}
