// Package core provides business logic and utility functions
package core

import (
	"fmt"
	"os"
	"strings"
)

// GetTmuxTmpdir returns the TMUX_TMPDIR environment variable or a portable default.
// Prefers XDG_RUNTIME_DIR/tmux, falls back to /tmp/tmux-<uid>.
func GetTmuxTmpdir() string {
	tmpdir := strings.TrimSpace(os.Getenv("TMUX_TMPDIR"))
	if tmpdir != "" {
		return tmpdir
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return xdg + "/tmux"
	}
	return fmt.Sprintf("/tmp/tmux-%d", os.Getuid())
}

// TmuxBin returns the tmux client binary every tmux call must invoke.
// ARCHON_TMUX_BIN pins one client version: a tmux 3.4 client cannot talk to a
// 3.6a server at all, so resolving "tmux" from PATH per code path silently
// breaks terminals. Falls back to PATH lookup when unset.
func TmuxBin() string {
	if bin := strings.TrimSpace(os.Getenv("ARCHON_TMUX_BIN")); bin != "" {
		return bin
	}
	return "tmux"
}

// GetTmuxEnv returns the environment for tmux commands
func GetTmuxEnv() []string {
	env := os.Environ()
	tmpdir := GetTmuxTmpdir()
	// Ensure TMUX_TMPDIR is set
	found := false
	for i, e := range env {
		if strings.HasPrefix(e, "TMUX_TMPDIR=") {
			env[i] = "TMUX_TMPDIR=" + tmpdir
			found = true
			break
		}
	}
	if !found {
		env = append(env, "TMUX_TMPDIR="+tmpdir)
	}
	return env
}
