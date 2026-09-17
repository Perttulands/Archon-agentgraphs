package coordinator

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestAHumanGateWaitPastTheWallClockSurvivesARestartAndTheRunGoesOn(t *testing.T) {
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	var mu sync.Mutex
	clock := time.Now().UTC()
	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return clock
	}
	open := func() *Coordinator {
		t.Helper()
		c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
			store.Now = now
			return formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: root, Roots: []string{root}})
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := open()
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(testBoard), 0o600); err != nil {
		t.Fatal(err)
	}
	id := startProof(t, c) // a 600 s wall clock
	request := awaitState(t, c, id, "waiting_human").WaitingGates[0]

	// The operator is away for three hours, and the daemon restarts meanwhile.
	mu.Lock()
	clock = clock.Add(3 * time.Hour)
	mu.Unlock()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	next := open()
	t.Cleanup(func() { next.Close() })
	if err := next.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	if p, err := next.Project(id); err != nil || p.Status != "waiting_human" || len(p.WaitingGates) != 1 {
		t.Fatalf("a gate waiting past the wall clock expired the run: %+v, %v", p, err)
	}

	verdict(t, next, id, "gate_review", request.RequestedSeq, true, "")
	awaitState(t, next, id, "succeeded")
	for _, event := range eventsOf(t, next, id) {
		if event.Type == formations.RunEventError {
			t.Fatalf("the run recorded an error after the operator answered: %+v", event.Data)
		}
	}
}
