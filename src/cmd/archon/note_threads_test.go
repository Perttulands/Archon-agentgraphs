package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestArchonBoardNoteRepliesInsteadOfOverwriting(t *testing.T) {
	workspace := t.TempDir()
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("threads"), `schema = 1
id = "brd_threads"
slug = "threads"
title = "Threads"
rev = 2

[[formation]]
id = "fmn_map"
type = "solo"
title = "Map"
`)
	writeArchonFile(t, store.NotesPath("threads"), `schema = 1
boardId = "brd_threads"
rev = 1
updatedAt = "2026-09-16T12:00:00Z"
updatedBy = "human:ui"
board = ""

[[element]]
nodeId = "fmn_map"
text = "Start by mapping the territory"
`)
	runner := &fakeTmux{live: map[string]bool{}}
	archon := func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", workspace, "board"}, args...)...)
	}
	thread := func() []formations.NoteEntry {
		t.Helper()
		stdout, stderr, code := archon("notes", "threads", "--json")
		var notes formations.BoardNotesDocument
		if code != 0 || json.Unmarshal([]byte(stdout), &notes) != nil || len(notes.Elements) != 1 {
			t.Fatalf("board notes: %d %s %s", code, stdout, stderr)
		}
		return notes.Elements[0].Entries
	}

	stdout, stderr, code := archon("note", "threads", "--node", "fmn_map", "--text", "Scout staffed; mapping now")
	if code != 0 || !strings.Contains(stdout, "added note nte_") {
		t.Fatalf("agent reply: %d %s %s", code, stdout, stderr)
	}
	entries := thread()
	if len(entries) != 2 || entries[0].ID != "migrated-fmn_map" || entries[0].Author != "human:ui" || entries[1].Author != "agent:archon" {
		t.Fatalf("thread after reply = %+v, want the operator's note kept first", entries)
	}

	if _, stderr, code := archon("note", "threads", "--node", "fmn_map", "--entry", entries[0].ID, "--text", "overwrite", "--json"); code != 1 || !strings.Contains(stderr, `"code": "note_author_mismatch"`) {
		t.Fatalf("editing the operator's entry: %d %s", code, stderr)
	}
	if _, stderr, code := archon("note", "threads", "--node", "fmn_map", "--entry", entries[1].ID, "--text", "Mapped three areas"); code != 0 {
		t.Fatalf("editing own entry: %d %s", code, stderr)
	}
	if stdout, stderr, code := archon("note", "threads", "--node", "fmn_map", "--text", "Operator follow-up", "--updated-by", "human:ui"); code != 0 || !strings.Contains(stdout, "added note") {
		t.Fatalf("--updated-by as author: %d %s %s", code, stdout, stderr)
	}
	stdout, _, _ = archon("notes", "threads")
	for _, want := range []string{"[fmn_map]", "migrated-fmn_map\thuman:ui", "\tagent:archon\t", "\tedited\nMapped three areas", "Operator follow-up"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("board notes text missing %q:\n%s", want, stdout)
		}
	}

	if _, stderr, code := archon("note", "threads", "--node", "fmn_map", "--clear"); code != 2 || !strings.Contains(stderr, "--clear --entry <id>") {
		t.Fatalf("--clear without --entry: %d %s", code, stderr)
	}
	if _, stderr, code := archon("note", "threads", "--node", "fmn_map", "--clear", "--entry", entries[1].ID); code != 0 {
		t.Fatalf("deleting own entry: %d %s", code, stderr)
	}
	if got := thread(); len(got) != 2 || got[0].Text != "Start by mapping the territory" || got[1].Text != "Operator follow-up" {
		t.Fatalf("thread after delete = %+v", got)
	}
}
