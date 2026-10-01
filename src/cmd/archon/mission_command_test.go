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
	t.Setenv("ARCHON_AGENTS_DIR", filepath.Join(t.TempDir(), "agents"))
	runner := &fakeTmux{live: map[string]bool{}}
	return workspace, func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", workspace}, args...)...)
	}
}

func TestArchonMissionHelpNamesEveryCommand(t *testing.T) {
	_, archon := newMissionCommandWorkspace(t)
	for _, args := range [][]string{{"mission"}, {"mission", "help"}, {"mission", "--help"}} {
		stdout, stderr, code := archon(args...)
		if code != 2 || stdout != "" {
			t.Fatalf("%v code=%d stdout=%q, want help on stderr", args, code, stdout)
		}
		for _, verb := range []string{"new <slug>", "list", "inspect <mission>", "notes", "note", "validate", "arrange", "create", "update", "wire", "run"} {
			if !strings.Contains(stderr, verb) {
				t.Fatalf("%v help lacks %q:\n%s", args, verb, stderr)
			}
		}
		if strings.Contains(stderr, "board") {
			t.Fatalf("%v help mentions board:\n%s", args, stderr)
		}
	}
	if _, stderr, code := archon("mission", "frobnicate"); code != 2 || !strings.Contains(stderr, `unknown mission command "frobnicate"`) || !strings.Contains(stderr, "new <slug>") {
		t.Fatalf("unknown mission verb code=%d stderr=%s, want the help", code, stderr)
	}
	if _, stderr, code := archon("list"); code != 2 || !strings.Contains(stderr, "<mission|formation|gate|tool|agent|run|peer>") || strings.Contains(stderr, "board") {
		t.Fatalf("top-level usage code=%d stderr=%s", code, stderr)
	}
}

// There is no archon board command: the noun is mission.
func TestArchonBoardIsAnUnknownNoun(t *testing.T) {
	_, archon := newMissionCommandWorkspace(t)
	for _, args := range [][]string{{"board", "new", "poems"}, {"board", "list"}, {"board", "help"}} {
		stdout, stderr, code := archon(args...)
		if code != 2 || stdout != "" || stderr != "unknown archon noun \"board\"\n" {
			t.Fatalf("%v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}

// With one Input card, wire and update act on it without naming it, or name it.
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
		Mission formations.MissionNode `json:"inputCard"`
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

func TestArchonRunListFiltersByMission(t *testing.T) {
	_, archon := newMissionCommandWorkspace(t)
	archon("mission", "new", "poems")
	if stdout, stderr, code := archon("run", "list", "--mission", "poems", "--json"); code != 0 || !strings.Contains(stdout, `"runs"`) {
		t.Fatalf("run list --mission code=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if _, stderr, code := archon("run", "list", "--board", "poems"); code != 2 || !strings.Contains(stderr, "flag provided but not defined: -board") {
		t.Fatalf("run list --board code=%d stderr=%q", code, stderr)
	}
	if _, stderr, code := archon("run", "list", "--mission", "nowhere"); code == 0 || stderr == "" {
		t.Fatalf("run list for a missing mission code=%d stderr=%q", code, stderr)
	}
}
