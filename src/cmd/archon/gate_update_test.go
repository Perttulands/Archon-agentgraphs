package main

import (
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestArchonGateUpdateChangesOnlyGivenFlags(t *testing.T) {
	workspace := t.TempDir()
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7

[[gate]]
id = "gate_review"
title = "Review gate"
kinds = ["code"]
criterion = "Human framing review"
check = "output_absent"
checkVersion = "1"
checkValue = "complaint text"
`)
	runner := &fakeTmux{live: map[string]bool{}}
	gate := func(args ...string) formations.GateNode {
		t.Helper()
		_, stderr, code := runArchon(t, runner, append([]string{"--workspace", workspace, "gate", "update", "session-search", "gate_review"}, args...)...)
		if code != 0 {
			t.Fatalf("gate update %v: %d %s", args, code, stderr)
		}
		board, err := store.ReadBoard("session-search")
		if err != nil || len(board.Gates) != 1 {
			t.Fatalf("reload: %+v %v", board, err)
		}
		return board.Gates[0]
	}

	if got := gate("--title", "Framing review"); got.Title != "Framing review" || got.CheckValue != "complaint text" || got.Criterion != "Human framing review" {
		t.Fatalf("title update = %+v", got)
	}
	if got := gate("--clear-check"); got.Check != "" || got.CheckVersion != "" || got.CheckValue != "" || got.Kinds[0] != "code" {
		t.Fatalf("clear-check = %+v", got)
	}
	if got := gate("--check", "output_contains", "--check-version", "1"); got.Check != "output_contains" || got.CheckValue != "" {
		t.Fatalf("partial check = %+v", got)
	}
	if got := gate("--kinds", "human", "--criterion", ""); strings.Join(got.Kinds, ",") != "human" || got.Check != "" || got.Criterion != "" {
		t.Fatalf("convert to human with cleared criterion = %+v", got)
	}

	_, stderr, code := runArchon(t, runner, "--workspace", workspace, "gate", "update", "session-search", "gate_review", "--clear-check", "--check", "output_absent")
	if code != 2 || !strings.Contains(stderr, "Only the flags you give change the gate") {
		t.Fatalf("conflicting clear flags: %d %s", code, stderr)
	}
	_, stderr, code = runArchon(t, runner, "--workspace", workspace, "gate", "update", "session-search", "gate_review", "--kinds", "")
	if code == 0 || !strings.Contains(stderr, "at least one kind") {
		t.Fatalf("empty kinds: %d %s", code, stderr)
	}
}
