package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// The local mission run and formation run refuse a negative limit with
// admission's message (archon-o7p.7) and write no run.
func TestArchonLocalRunsRefuseNegativeLimits(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := t.TempDir()
	t.Setenv("ARCHON_AGENTS_DIR", agentsDir)
	personas := formations.NewPersonaStore(agentsDir)
	if _, err := personas.CreatePersona(formations.CreatePersonaRequest{ID: "scout", Kind: "specialist", Harness: "openai-codex"}); err != nil {
		t.Fatalf("create persona: %v", err)
	}
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("session-search"), archonS5CascadeBoardFixture())
	runner := &fakeTmux{live: map[string]bool{}}
	for _, args := range [][]string{
		{"mission", "run", "session-search", "--max-attempts", "-1"},
		{"mission", "run", "session-search", "--wall-clock-seconds", "-1", "--json"},
		{"formation", "run", "session-search", "fmn_work", "--max-dispatch", "-1"},
	} {
		stdout, stderr, code := runArchon(t, runner, append([]string{"--workspace", workspace}, args...)...)
		if code != 1 || !strings.Contains(stderr, formations.ErrInvalidRunLimits.Error()) {
			t.Fatalf("%v: code=%d stderr=%s stdout=%s, want the limits message", args, code, stderr, stdout)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(workspace, ".archon", "runs", "session-search")); err == nil && len(entries) != 0 {
		t.Fatalf("refused starts wrote run artifacts: %v", entries)
	}
}
