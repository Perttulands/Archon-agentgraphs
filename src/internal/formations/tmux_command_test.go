package formations

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// One hung tmux command fails with a reason instead of stalling a dispatch
// forever; the bound is per command, not per step.
func TestRunTmuxCommandBoundsOneHungCommand(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "tmux")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ARCHON_TMUX_BIN", bin)
	previous := tmuxCommandTimeout
	tmuxCommandTimeout = 200 * time.Millisecond
	defer func() { tmuxCommandTimeout = previous }()
	started := time.Now()
	_, err := runTmuxCommand(context.Background(), "/tmp/socket", nil, "capture-pane", "-p")
	if err == nil || !strings.Contains(err.Error(), "tmux capture-pane did not finish within 200ms") {
		t.Fatalf("hung command error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("hung command took %s to fail", elapsed)
	}
}
