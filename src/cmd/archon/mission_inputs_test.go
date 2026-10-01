package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// An agent learns a mission's inputs from archon mission input, declares them
// there, and supplies them with --input to a mission run or a single step
// (archon-o7p.3).
func TestArchonMissionInputsAreDeclaredListedAndSupplied(t *testing.T) {
	workspace := t.TempDir()
	agentsDir := t.TempDir()
	t.Setenv("ARCHON_AGENTS_DIR", agentsDir)
	t.Setenv("ARCHON_LAB_HARNESSES", "openai-codex")
	if _, err := formations.NewPersonaStore(agentsDir).CreatePersona(formations.CreatePersonaRequest{ID: "lab-poet", Kind: "specialist", Harness: "openai-codex"}); err != nil {
		t.Fatal(err)
	}
	store := formations.NewStore(workspace)
	writeArchonFile(t, store.BoardPath("poems"), archonS4PoemMissingRootBoardFixture())
	archon := func(args ...string) (string, string, int) {
		t.Helper()
		return runArchon(t, &fakeTmux{live: map[string]bool{}}, append([]string{"--workspace", workspace}, args...)...)
	}

	if out, stderr, code := archon("mission", "input", "poems"); code != 0 || out != "brief\ttext\trequired\n" {
		t.Fatalf("implicit inputs = %d %q %s", code, out, stderr)
	}
	if out, stderr, code := archon("mission", "input", "poems", "subject", "--required", "--description", "What the poem is about"); code != 0 || out != "set input subject on Input card mis_poem\n" {
		t.Fatalf("declare subject = %d %q %s", code, out, stderr)
	}
	if _, stderr, code := archon("mission", "input", "poems", "notes", "--kind", "file"); code != 0 {
		t.Fatalf("declare notes = %d %s", code, stderr)
	}
	if _, stderr, code := archon("formation", "set-brief", "poems", "fmn_draft", "--goal", "Write a poem about {subject}; read {notes} first."); code != 0 {
		t.Fatalf("set brief = %d %s", code, stderr)
	}
	out, _, _ := archon("mission", "input", "poems")
	if out != "subject\ttext\trequired\tWhat the poem is about\nnotes\tfile\toptional\n" {
		t.Fatalf("declared inputs listed as %q", out)
	}
	if out, _, _ := archon("mission", "inspect", "poems", "mis_poem"); !strings.Contains(out, "input\tsubject\ttext\trequired\tWhat the poem is about\ninput\tnotes\tfile\toptional\n") {
		t.Fatalf("mission inspect lacks the inputs:\n%s", out)
	}
	if _, stderr, code := archon("mission", "input", "poems", "subject", "--required", "--optional"); code != 2 || !strings.Contains(stderr, "not both") {
		t.Fatalf("contradictory flags = %d %s", code, stderr)
	}

	// A run without the required input is refused with the input named.
	_, stderr, code := archon("mission", "run", "poems", "--input", "brief=old habit")
	if code != 1 || !strings.Contains(stderr, "ERROR\tmissing_input\tmis_poem\tinput subject is required: What the poem is about") ||
		!strings.Contains(stderr, "ERROR\tunknown_input\tmis_poem\tmission poems has no input named \"brief\"; its inputs are subject, notes") {
		t.Fatalf("refused run = %d\n%s", code, stderr)
	}

	notes := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(notes, []byte("Waves."), 0600); err != nil {
		t.Fatal(err)
	}
	subject := filepath.Join(t.TempDir(), "subject.txt")
	if err := os.WriteFile(subject, []byte("the sea\nat dusk"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"mission", "run", "poems", "--input-file", "subject=" + subject, "--input", "notes=" + notes, "--bead", "archon-o7p.3", "--json"},
		{"formation", "run", "poems", "fmn_draft", "--input-file", "subject=" + subject, "--input", "notes=" + notes, "--bead", "archon-o7p.3", "--json"},
	} {
		stdout, stderr, code := archon(args...)
		if code != 0 {
			t.Fatalf("%v = %d %s", args[:2], code, stderr)
		}
		started := decodeArchonRunResponse(t, stdout)
		if started.Status.Status != formations.RunStatusSucceeded || started.Status.BeadID != "archon-o7p.3" || len(started.Status.Inputs) != 2 || started.Status.Inputs[0].Value != "the sea\nat dusk" {
			t.Fatalf("%v status = %+v", args[:2], started.Status)
		}
		var brief string
		matches, _ := filepath.Glob(filepath.Join(workspace, "briefs", "lab-*.md"))
		for _, match := range matches {
			if raw, _ := os.ReadFile(match); strings.Contains(string(raw), started.RunID) {
				brief += string(raw)
			}
		}
		if !strings.Contains(brief, "brief: Write a poem about the sea\nat dusk; read "+notes+" first.") || !strings.Contains(brief, "input: subject: the sea\nat dusk\nnotes: "+notes) {
			t.Fatalf("%v lab brief:\n%s", args[:2], brief)
		}
	}

	if _, stderr, code := archon("mission", "run", "poems", "--input", "subject"); code != 2 || !strings.Contains(stderr, `--input "subject" must be name=value`) {
		t.Fatalf("malformed --input = %d %s", code, stderr)
	}
	stdout, _, code := archon("mission", "input", "poems", "notes", "--delete", "--json")
	var board formations.BoardDocument
	if err := json.Unmarshal([]byte(stdout), &board); code != 0 || err != nil || len(board.Missions[0].Inputs) != 1 {
		t.Fatalf("delete notes = %d %s (%v)", code, stdout, err)
	}
	// The brief still references {notes}, so validation names it.
	if out, _, code := archon("mission", "validate", "poems"); code != 1 || !strings.Contains(out, "ERROR\tunknown_input_reference\tfmn_draft\tDraft poem's brief references {notes}, but the mission has no input named notes; its inputs are subject") {
		t.Fatalf("validate after delete = %d\n%s", code, out)
	}
}
