package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func newMissionCommandWorkspace(t *testing.T) (string, func(args ...string) (string, string, int)) {
	t.Helper()
	workspace := t.TempDir()
	t.Setenv("CHROTE_AGENTS_DIR", filepath.Join(t.TempDir(), "agents"))
	runner := &fakeTmux{live: map[string]bool{}}
	return workspace, func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", workspace}, args...)...)
	}
}

func TestArchonMissionHelpNamesEveryCommandAndTheBoardAlias(t *testing.T) {
	_, archon := newMissionCommandWorkspace(t)
	for _, args := range [][]string{{"mission"}, {"mission", "help"}, {"mission", "--help"}, {"board"}} {
		stdout, stderr, code := archon(args...)
		if code != 2 || stdout != "" {
			t.Fatalf("%v code=%d stdout=%q, want help on stderr", args, code, stdout)
		}
		for _, verb := range []string{"new <slug>", "list", "inspect <mission>", "notes", "note", "validate", "arrange", "create", "update", "wire", "run", `"archon board <command>" is a deprecated alias`} {
			if !strings.Contains(stderr, verb) {
				t.Fatalf("%v help lacks %q:\n%s", args, verb, stderr)
			}
		}
	}
	if _, stderr, code := archon("mission", "frobnicate"); code != 2 || !strings.Contains(stderr, `unknown mission command "frobnicate"`) || !strings.Contains(stderr, "new <slug>") {
		t.Fatalf("unknown mission verb code=%d stderr=%s, want the help", code, stderr)
	}
	if _, stderr, code := archon("list"); code != 2 || !strings.Contains(stderr, "<mission|formation|gate|end|tool|agent|run|peer>") || strings.Contains(stderr, "board") {
		t.Fatalf("top-level usage code=%d stderr=%s", code, stderr)
	}
}

// Each archon board command still works for one release, answers exactly as
// the archon mission command of the same name, and says which one to use.
func TestArchonBoardAliasRunsTheMissionCommandWithADeprecationNote(t *testing.T) {
	_, archon := newMissionCommandWorkspace(t)
	stdout, stderr, code := archon("board", "new", "poems", "--title", "Poems")
	if code != 0 || stdout != "created poems\n" || stderr != "archon board is deprecated and will be removed in a later release; use: archon mission new\n" {
		t.Fatalf("board new code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, stderr, code := archon("mission", "create", "poems", "--title", "Brief"); code != 0 {
		t.Fatalf("mission create: %d %s", code, stderr)
	}
	if _, stderr, code := archon("mission", "note", "poems", "--text", "Why this exists"); code != 0 {
		t.Fatalf("mission note: %d %s", code, stderr)
	}
	for _, command := range [][]string{
		{"list"}, {"list", "--json"},
		{"inspect", "poems"}, {"inspect", "poems", "--json"},
		{"notes", "poems"}, {"validate", "poems"}, {"validate", "poems", "--json"},
		{"arrange", "poems"},
	} {
		wantOut, wantErr, wantCode := archon(append([]string{"mission"}, command...)...)
		gotOut, gotErr, gotCode := archon(append([]string{"board"}, command...)...)
		note := "archon board is deprecated and will be removed in a later release; use: archon mission " + command[0] + "\n"
		if gotOut != wantOut || gotCode != wantCode || gotErr != note+wantErr {
			t.Fatalf("board %v = (%d, %q, %q), want mission's (%d, %q, %q) after the note", command, gotCode, gotOut, gotErr, wantCode, wantOut, wantErr)
		}
	}
	if _, stderr, code := archon("board", "note", "poems", "--text", "Still works"); code != 0 || !strings.HasPrefix(stderr, "archon board is deprecated") {
		t.Fatalf("board note code=%d stderr=%q", code, stderr)
	}
	if _, stderr, code := archon("board", "create", "poems"); code != 2 || !strings.Contains(stderr, `unknown board command "create"`) {
		t.Fatalf("board create code=%d stderr=%q, want unknown: board never had create", code, stderr)
	}
}

// With one Input card, wire and update act on it without naming it; the old
// forms that name it keep working.
func TestArchonMissionWireAndUpdateActOnTheInputCard(t *testing.T) {
	workspace, archon := newMissionCommandWorkspace(t)
	archon("mission", "new", "poems")
	if _, stderr, code := archon("mission", "wire", "poems", "fmn_x:in"); code == 0 || !strings.Contains(stderr, "has no Input card; add one with: archon mission create poems") {
		t.Fatalf("wire without an Input card code=%d stderr=%s", code, stderr)
	}
	stdout, stderr, code := archon("mission", "create", "poems", "--title", "Brief", "--json")
	if code != 0 {
		t.Fatalf("mission create: %d %s", code, stderr)
	}
	var created struct {
		Mission formations.MissionNode `json:"mission"`
	}
	if err := json.Unmarshal([]byte(stdout), &created); err != nil {
		t.Fatalf("decode create: %v %s", err, stdout)
	}
	stdout, stderr, code = archon("formation", "create", "poems", "solo", "--title", "Draft", "--json")
	if code != 0 {
		t.Fatalf("formation create: %d %s", code, stderr)
	}
	var formation struct {
		Formation formations.FormationNode `json:"formation"`
	}
	if err := json.Unmarshal([]byte(stdout), &formation); err != nil {
		t.Fatalf("decode formation: %v %s", err, stdout)
	}
	target := formation.Formation.ID + ":" + formation.Formation.Inputs[0].ID

	if stdout, stderr, code := archon("mission", "update", "poems", "--goal", "Write a poem"); code != 0 || stdout != "updated Input card "+created.Mission.ID+"\n" {
		t.Fatalf("update without <input> code=%d stdout=%q stderr=%s", code, stdout, stderr)
	}
	if _, stderr, code := archon("mission", "update", "poems", "Brief", "--input-hint", "A theme"); code != 0 {
		t.Fatalf("update naming the Input card: %d %s", code, stderr)
	}
	if stdout, stderr, code := archon("mission", "wire", "poems", target); code != 0 || stdout != "wired Input card "+created.Mission.ID+" -> "+target+"\n" {
		t.Fatalf("wire without <input> code=%d stdout=%q stderr=%s", code, stdout, stderr)
	}
	board, err := formations.NewStore(workspace).ReadBoard("poems")
	if err != nil {
		t.Fatal(err)
	}
	if mission := board.Missions[0]; mission.Goal != "Write a poem" || mission.InputHint != "A theme" || len(board.Connections) != 1 || board.Connections[0].From != created.Mission.ID+":out" || board.Connections[0].To != target {
		t.Fatalf("board after update and wire: missions %+v connections %+v", board.Missions, board.Connections)
	}
	stdout, stderr, code = archon("mission", "inspect", "poems", "Brief", "--json")
	if code != 0 || !strings.Contains(stdout, `"chain"`) || !strings.Contains(stdout, formation.Formation.ID) {
		t.Fatalf("inspect the Input card code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
}

// A file saved before one-mission-per-file can hold several Input cards; the
// commands that would have to guess ask which one instead.
func TestArchonMissionCommandsAskWhichInputCardWhenThereAreSeveral(t *testing.T) {
	workspace, archon := newMissionCommandWorkspace(t)
	archon("mission", "new", "legacy")
	store := formations.NewStore(workspace)
	for _, title := range []string{"First", "Second"} {
		board, err := store.ReadBoard("legacy")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateMission("legacy", formations.MissionCreateRequest{Title: title, UpdatedBy: "test"}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"mission", "update", "legacy", "--goal", "x"},
		{"mission", "wire", "legacy", "fmn_x:in"},
	} {
		if _, stderr, code := archon(args...); code == 0 || !strings.Contains(stderr, `mission "legacy" has 2 Input cards; name one of them`) {
			t.Fatalf("%v code=%d stderr=%s", args, code, stderr)
		}
	}
	if _, stderr, code := archon("mission", "run", "legacy"); code == 0 || !strings.Contains(stderr, `mission "legacy" holds 2 Input cards`) || !strings.Contains(stderr, "Split it") {
		t.Fatalf("run code=%d stderr=%s", code, stderr)
	}
	if _, stderr, code := archon("mission", "update", "legacy", "Second", "--goal", "Named"); code != 0 {
		t.Fatalf("update naming one card: %d %s", code, stderr)
	}
}

func TestArchonRunListFiltersByMissionAndKeepsTheBoardFlag(t *testing.T) {
	_, archon := newMissionCommandWorkspace(t)
	archon("mission", "new", "poems")
	for _, flag := range []string{"--mission", "--board"} {
		if stdout, stderr, code := archon("run", "list", flag, "poems", "--json"); code != 0 || !strings.Contains(stdout, `"runs"`) {
			t.Fatalf("run list %s code=%d stdout=%s stderr=%s", flag, code, stdout, stderr)
		}
	}
	if _, stderr, code := archon("run", "list", "--mission", "nowhere"); code == 0 || stderr == "" {
		t.Fatalf("run list for a missing mission code=%d stderr=%q", code, stderr)
	}
}
