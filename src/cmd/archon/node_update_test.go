package main

import (
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestArchonFormationRenameAndMissionUpdate(t *testing.T) {
	workspace := t.TempDir()
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("rename"), `schema = 1
id = "brd_rename"
slug = "rename"
title = "Rename"
rev = 2

[[mission]]
id = "mis_frame"
title = "New mission"
goal = "Old goal"
beadId = "form-3yd.10"

[[formation]]
id = "fmn_map"
type = "solo"
title = "New formation"
[[formation.input]]
id = "port_map_in"
label = "Input"
`)
	runner := &fakeTmux{live: map[string]bool{}}
	archon := func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", workspace}, args...)...)
	}

	if _, stderr, code := archon("formation", "rename", "rename", "New formation", "Map the territory"); code != 0 {
		t.Fatalf("formation rename by title: %d %s", code, stderr)
	}
	if _, stderr, code := archon("mission", "update", "rename", "mis_frame", "--title", "Frame the goal", "--goal", ""); code != 0 {
		t.Fatalf("mission update: %d %s", code, stderr)
	}
	board, err := store.ReadBoard("rename")
	if err != nil {
		t.Fatal(err)
	}
	if board.Formations[0].Title != "Map the territory" || board.Missions[0].Title != "Frame the goal" || board.Missions[0].Goal != "" || board.Missions[0].BeadID != "form-3yd.10" {
		t.Fatalf("after updates: formation %+v mission %+v", board.Formations[0], board.Missions[0])
	}
	if _, stderr, code := archon("mission", "update", "rename", "Frame the goal", "--bead", ""); code != 0 {
		t.Fatalf("clear bead by mission title: %d %s", code, stderr)
	}
	if board, _ := store.ReadBoard("rename"); board.Missions[0].BeadID != "" {
		t.Fatalf("bead not cleared: %+v", board.Missions[0])
	}
	if _, stderr, code := archon("mission", "update", "rename", "mis_frame", "--input-hint", "Paste the operator's sketch"); code != 0 {
		t.Fatalf("set input hint: %d %s", code, stderr)
	}
	if board, _ := store.ReadBoard("rename"); board.Missions[0].InputHint != "Paste the operator's sketch" || board.Missions[0].Title != "Frame the goal" {
		t.Fatalf("input hint not set: %+v", board.Missions[0])
	}

	if _, stderr, code := archon("mission", "update", "rename", "mis_frame", "--human-channel", "session"); code != 0 {
		t.Fatalf("set human channel: %d %s", code, stderr)
	}
	if stdout, stderr, code := archon("board", "inspect", "rename", "--json"); code != 0 || !strings.Contains(stdout, `"humanChannel": "session"`) {
		t.Fatalf("board inspect after session: %d %s %s", code, stdout, stderr)
	}
	for _, clear := range []string{"notify", ""} {
		if _, stderr, code := archon("mission", "update", "rename", "mis_frame", "--human-channel", clear); code != 0 {
			t.Fatalf("human channel %q: %d %s", clear, code, stderr)
		}
		if raw := readArchonFile(t, store.BoardPath("rename")); strings.Contains(raw, "humanChannel") {
			t.Fatalf("human channel %q leaves the key:\n%s", clear, raw)
		}
		if _, _, code := archon("mission", "update", "rename", "mis_frame", "--human-channel", "session"); code != 0 {
			t.Fatal("set session again")
		}
	}
	before := readArchonFile(t, store.BoardPath("rename"))
	if _, stderr, code := archon("mission", "update", "rename", "mis_frame", "--human-channel", "email"); code == 0 || !strings.Contains(stderr, "must be notify or session") {
		t.Fatalf("unknown human channel: %d %s", code, stderr)
	}
	if after := readArchonFile(t, store.BoardPath("rename")); after != before {
		t.Fatalf("a rejected human channel saved the board:\n%s", after)
	}

	if _, stderr, code := archon("mission", "update", "rename", "mis_frame"); code != 2 || !strings.Contains(stderr, "Only the flags you give change the mission") {
		t.Fatalf("update without fields: %d %s", code, stderr)
	}
	if _, _, code := archon("mission", "update", "rename", "mis_frame", "--bead", "Home-123"); code == 0 {
		t.Fatal("unsafe Bead ID accepted")
	}
	if _, stderr, code := archon("formation", "rename", "rename", "fmn_map"); code != 2 || !strings.Contains(stderr, "usage: archon formation rename") {
		t.Fatalf("rename without title: %d %s", code, stderr)
	}
}
