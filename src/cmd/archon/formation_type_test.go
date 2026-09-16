package main

import (
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestArchonFormationSetType(t *testing.T) {
	workspace := t.TempDir()
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("types"), `schema = 1
id = "brd_types"
slug = "types"
title = "Types"
rev = 2

[[formation]]
id = "fmn_work"
type = "orchestrated"
title = "Desk"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.slot]]
id = "slot_lead"
label = "Lead"
controller = true
agentId = "codex-builder"
[[formation.slot]]
id = "slot_worker"
label = "Worker"
controller = false
agentId = "codex-reviewer"
`)
	runner := &fakeTmux{live: map[string]bool{}}
	archon := func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", workspace, "formation", "set-type", "types", "Desk"}, args...)...)
	}

	if _, stderr, code := archon("solo"); code != 1 || !strings.Contains(stderr, "name the slot to keep") {
		t.Fatalf("solo without --keep-slot: %d %s", code, stderr)
	}
	if _, stderr, code := archon("flow"); code != 1 || !strings.Contains(stderr, "use solo, peer or orchestrated") {
		t.Fatalf("flow target: %d %s", code, stderr)
	}
	if _, stderr, code := archon("solo", "--keep-slot", "Worker"); code != 0 {
		t.Fatalf("solo keeping Worker: %d %s", code, stderr)
	}
	board, err := store.ReadBoard("types")
	if err != nil || board.Formations[0].Type != "solo" || len(board.Formations[0].Slots) != 1 || board.Formations[0].Slots[0].ID != "slot_worker" {
		t.Fatalf("after solo: %+v %v", board.Formations, err)
	}
	if _, stderr, code := archon("peer", "--json"); code != 0 {
		t.Fatalf("solo->peer: %d %s", code, stderr)
	}
	board, _ = store.ReadBoard("types")
	if board.Formations[0].Type != "peer" || len(board.Formations[0].Slots) != 2 {
		t.Fatalf("after peer: %+v", board.Formations[0])
	}
	if _, stderr, code := archon(); code != 2 || !strings.Contains(stderr, "usage: archon formation set-type") {
		t.Fatalf("missing type: %d %s", code, stderr)
	}
	if _, stderr, code := runArchon(t, runner, "--workspace", workspace, "formation", "create", "types", "flow"); code == 0 || !strings.Contains(stderr, "use solo, peer or orchestrated") {
		t.Fatalf("formation create flow: %d %s", code, stderr)
	}
}
