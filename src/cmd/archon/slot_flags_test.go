package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

const slotStaffingBoard = `schema = 1
id = "brd_staff"
slug = "staff"
title = "Staff"
rev = 1

[[formation]]
id = "fmn_work"
type = "peer"
title = "Work"

[[formation.slot]]
id = "slot_a"
label = "A"
controller = false

[[formation.slot]]
id = "slot_b"
label = "B"
agentId = "delivery-final-reviewer"
harness = "openai-codex"
controller = false
`

// formation assign states a slot's harness, model and effort, with an optional
// role; formation inspect prints them.
func TestArchonFormationAssignSetsTheSlotsSettings(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("CHROTE_AGENTS_DIR", t.TempDir())
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("staff"), slotStaffingBoard)
	runner := &fakeTmux{live: map[string]bool{}}
	archon := func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", workspace}, args...)...)
	}

	// The effort is required, and the usage teaches the policy.
	for _, args := range [][]string{
		{"formation", "assign", "staff", "Work", "--slot", "slot_a", "--harness", "claude-code"},
		{"formation", "assign", "staff", "Work", "--slot", "slot_a", "--effort", "low"},
		{"formation", "assign", "staff", "Work", "--slot", "slot_a", "--role", "delivery-worker"},
	} {
		_, stderr, code := archon(args...)
		if code != 2 || !strings.Contains(stderr, "--harness <claude-code|openai-codex> --effort <effort>") || !strings.Contains(stderr, "low for errands, medium for making things, xhigh for architecture and review, max for consequential reviews") {
			t.Fatalf("%v: code=%d stderr=%s, want usage with the effort policy", args, code, stderr)
		}
	}
	if _, stderr, code := archon("formation", "assign", "staff", "Work", "--slot", "slot_a", "--harness", "claude-code", "--effort", "ultra", "--json"); code == 0 || !strings.Contains(stderr, `"code": "invalid_slot_settings"`) || !strings.Contains(stderr, "low, medium, high, xhigh, max") {
		t.Fatalf("ultra on claude-code: code=%d stderr=%s", code, stderr)
	}
	if raw := readArchonFile(t, store.BoardPath("staff")); raw != slotStaffingBoard {
		t.Fatalf("refused assignments wrote the board:\n%s", raw)
	}

	stdout, stderr, code := archon("formation", "assign", "staff", "Work", "--slot", "slot_a", "--harness", "claude-code", "--model", "opus", "--effort", "low")
	if code != 0 || stdout != "slot_a is vanilla · claude-code · opus · low\n" {
		t.Fatalf("vanilla assign: code=%d stdout=%q stderr=%s", code, stdout, stderr)
	}
	stdout, stderr, code = archon("formation", "assign", "staff", "Work", "--slot", "slot_b", "--role", "delivery-final-reviewer", "--harness", "openai-codex", "--effort", "max", "--json")
	if code != 0 {
		t.Fatalf("role assign: code=%d stderr=%s", code, stderr)
	}
	var board formations.BoardDocument
	if err := json.Unmarshal([]byte(stdout), &board); err != nil {
		t.Fatal(err)
	}
	if slot := board.Formations[0].Slots[1]; slot.AgentID != "delivery-final-reviewer" || slot.Harness != "openai-codex" || slot.Model != "" || slot.Effort != "max" {
		t.Fatalf("role slot = %+v, want the stated settings, not the role's", slot)
	}

	stdout, stderr, code = archon("formation", "inspect", "staff", "Work")
	want := "fmn_work\tpeer\tWork\t2/2 staffed\t0 connections\n" +
		"slot slot_a\tA\tvanilla · claude-code · opus · low\n" +
		"slot slot_b\tB\tdelivery-final-reviewer · openai-codex · default model · max\n"
	if code != 0 || stdout != want {
		t.Fatalf("inspect code=%d stdout=%q stderr=%s, want %q", code, stdout, stderr, want)
	}
	stdout, _, _ = archon("formation", "inspect", "staff", "Work", "--json")
	for _, field := range []string{`"model": "opus"`, `"effort": "low"`, `"effort": "max"`} {
		if !strings.Contains(stdout, field) {
			t.Fatalf("inspect JSON missing %s:\n%s", field, stdout)
		}
	}

	if _, stderr, code := archon("formation", "unassign", "staff", "Work", "--slot", "slot_a"); code != 0 {
		t.Fatalf("unassign: %d %s", code, stderr)
	}
	if raw := readArchonFile(t, store.BoardPath("staff")); strings.Contains(raw, `model = "opus"`) || strings.Contains(raw, `effort = "low"`) {
		t.Fatalf("unassign left settings behind:\n%s", raw)
	}
}

// board migrate-slots reports every staffed slot's launch before and after,
// and --dry-run writes nothing.
func TestArchonBoardMigrateSlotsReportsIdenticalLaunches(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	workspace := t.TempDir()
	t.Setenv("CHROTE_AGENTS_DIR", t.TempDir())
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("staff"), slotStaffingBoard)
	runner := &fakeTmux{live: map[string]bool{}}

	stdout, stderr, code := runArchon(t, runner, "--workspace", workspace, "board", "migrate-slots", "--dry-run")
	wantLaunch := "exec '" + filepath.Join(bin, "codex") + "' --model 'gpt-6-astra' -c 'model_reasoning_effort=\"medium\"' -c check_for_update_on_startup=false --dangerously-bypass-approvals-and-sandbox"
	if code != 0 || !strings.Contains(stdout, "would-migrate\tstaff/fmn_work\tslot_b\tdelivery-final-reviewer · openai-codex · gpt-6-astra · medium\n  before: "+wantLaunch+"\n  after:  "+wantLaunch+"\n  identical: true\n") {
		t.Fatalf("dry run code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if raw := readArchonFile(t, store.BoardPath("staff")); raw != slotStaffingBoard {
		t.Fatalf("dry run wrote the board:\n%s", raw)
	}
	stdout, stderr, code = runArchon(t, runner, "--workspace", workspace, "board", "migrate-slots", "staff", "--json")
	var report formations.SlotMigrationReport
	if code != 0 || json.Unmarshal([]byte(stdout), &report) != nil || len(report.Slots) != 1 || report.Slots[0].Outcome != formations.SlotMigrationMigrated || !report.Slots[0].Identical || report.Boards[0].RevTo != 2 {
		t.Fatalf("migrate code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if raw := readArchonFile(t, store.BoardPath("staff")); !strings.Contains(raw, `model = "gpt-6-astra"`) || !strings.Contains(raw, `effort = "medium"`) {
		t.Fatalf("migrated board:\n%s", raw)
	}
}
