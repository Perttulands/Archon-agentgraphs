package main

import (
	"encoding/json"
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
effort = "medium"
`

// formation assign states a slot's harness, model and effort, with an optional
// role; formation inspect prints them.
func TestArchonFormationAssignSetsTheSlotsSettings(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("ARCHON_AGENTS_DIR", t.TempDir())
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

// Slots own their settings; there is no slot migration command.
func TestArchonMissionMigrateSlotsIsUnknown(t *testing.T) {
	runner := &fakeTmux{live: map[string]bool{}}
	_, stderr, code := runArchon(t, runner, "--workspace", t.TempDir(), "mission", "migrate-slots")
	if code != 2 || !strings.HasPrefix(stderr, "unknown mission command \"migrate-slots\"") {
		t.Fatalf("migrate-slots code=%d stderr=%s", code, stderr)
	}
}

// A model outside its harness's catalog is staffed with a warning, and the
// usage names the known models (archon-v53).
func TestArchonFormationAssignWarnsOfAModelOutsideTheCatalog(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("ARCHON_AGENTS_DIR", t.TempDir())
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("staff"), slotStaffingBoard)
	runner := &fakeTmux{live: map[string]bool{}}
	archon := func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", workspace}, args...)...)
	}
	stdout, stderr, code := archon("formation", "assign", "staff", "Work", "--slot", "slot_a", "--harness", "claude-code", "--model", "claude-opus-5", "--effort", "xhigh")
	if code != 0 || stdout != "slot_a is vanilla · claude-code · claude-opus-5 · xhigh\n" || stderr != "warning: slot \"A\" (slot_a) model \"claude-opus-5\" is not in the claude-code catalog; the harness decides\n" {
		t.Fatalf("off-catalog model: code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, stderr, code := archon("formation", "assign", "staff", "Work", "--slot", "slot_a", "--harness", "claude-code", "--model", "sonnet", "--effort", "xhigh"); code != 0 || stderr != "" {
		t.Fatalf("known model: code=%d stderr=%q", code, stderr)
	}
	if _, stderr, _ := archon("formation", "assign", "staff", "Work", "--slot", "slot_a"); !strings.Contains(stderr, "Known models: claude-code runs opus, sonnet, haiku, fable. Another model is accepted with a warning.") {
		t.Fatalf("usage does not name the known models:\n%s", stderr)
	}
}
