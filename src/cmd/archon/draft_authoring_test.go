package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestArchonDraftAuthoringSavesAndAdmissionListsEveryProblem(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("CHROTE_AGENTS_DIR", filepath.Join(t.TempDir(), "agents"))
	runner := &fakeTmux{live: map[string]bool{}}
	archon := func(args ...string) (string, string, int) {
		return runArchon(t, runner, append([]string{"--workspace", workspace}, args...)...)
	}

	if _, stderr, code := archon("board", "new", "sketch"); code != 0 {
		t.Fatalf("board new without title: %d %s", code, stderr)
	}
	if _, stderr, code := archon("mission", "create", "sketch"); code != 0 {
		t.Fatalf("mission create without --bead: %d %s", code, stderr)
	}
	stdout, stderr, code := archon("formation", "create", "sketch", "--json")
	if code != 0 {
		t.Fatalf("formation create without type: %d %s", code, stderr)
	}
	var created struct {
		Formation formations.FormationNode `json:"formation"`
	}
	if err := json.Unmarshal([]byte(stdout), &created); err != nil || created.Formation.Type != formations.FormationTypeSolo {
		t.Fatalf("created formation %s (%v), want solo", stdout, err)
	}
	if _, stderr, code := archon("gate", "create", "sketch", "--kinds", "code", "--check", "output_absent", "--check-version", "1"); code != 0 {
		t.Fatalf("gate create without a check value: %d %s", code, stderr)
	}
	if _, _, code := archon("gate", "create", "sketch", "--check", "no_such_profile", "--check-version", "1"); code == 0 {
		t.Fatal("unknown code check profile saved")
	}
	if _, _, code := archon("mission", "create", "sketch", "--bead", "Home-123"); code == 0 {
		t.Fatal("unsafe Bead ID saved")
	}

	store := formations.NewStore(workspace)
	board, err := store.ReadBoard("sketch")
	if err != nil || len(board.Missions) != 1 || len(board.Formations) != 1 || len(board.Gates) != 1 {
		t.Fatalf("reloaded sketch = %+v (%v), want one mission, formation and gate", board, err)
	}
	formation := board.Formations[0]
	gate := board.Gates[0]
	if _, stderr, code := archon("mission", "wire", "sketch", board.Missions[0].ID, formation.ID+":"+formation.Inputs[0].ID); code != 0 {
		t.Fatalf("mission wire: %d %s", code, stderr)
	}
	if _, stderr, code := archon("formation", "wire", "sketch", formation.ID+":"+formation.Outputs[0].ID, gate.ID+":in"); code != 0 {
		t.Fatalf("formation wire: %d %s", code, stderr)
	}

	stdout, _, code = archon("board", "validate", "sketch")
	if code != 1 || !strings.Contains(stdout, "ERROR\tunstaffed_slot\t"+formation.ID) || !strings.Contains(stdout, "ERROR\tgate_not_routable\t"+gate.ID+"\tgate \""+gate.ID+"\" needs forbidden text") {
		t.Fatalf("board validate %d:\n%s", code, stdout)
	}

	_, stderr, code = archon("mission", "run", "sketch", "--max-dispatch", "3", "--max-attempts", "1", "--wall-clock-seconds", "60", "--json")
	var failure archonErrorResponse
	if err := json.Unmarshal([]byte(stderr), &failure); code != 1 || err != nil {
		t.Fatalf("mission run %d %s (%v)", code, stderr, err)
	}
	if failure.Code != "run_admission_failed" || len(failure.Findings) != 2 {
		t.Fatalf("mission run failure = %+v, want both findings", failure)
	}
	_, stderr, _ = archon("mission", "run", "sketch")
	if !strings.Contains(stderr, "run admission found 2 problem(s)") || strings.Count(stderr, "\nERROR\t") != 2 {
		t.Fatalf("mission run text stderr:\n%s", stderr)
	}
	if runs, err := store.ListRuns(formations.RunListFilter{}); err != nil || len(runs) != 0 {
		t.Fatalf("rejected admission recorded runs %+v (%v)", runs, err)
	}
}

func TestArchonGateCreateWithoutKindsIsARoutableHumanGate(t *testing.T) {
	workspace := t.TempDir()
	t.Setenv("CHROTE_AGENTS_DIR", filepath.Join(t.TempDir(), "agents"))
	runner := &fakeTmux{live: map[string]bool{}}
	archon := func(args ...string) string {
		t.Helper()
		stdout, stderr, code := runArchon(t, runner, append([]string{"--workspace", workspace}, args...)...)
		if code != 0 {
			t.Fatalf("%v: %d %s", args, code, stderr)
		}
		return stdout
	}
	archon("board", "new", "review")
	archon("mission", "create", "review", "--title", "Work", "--goal", "Do it", "--bead", "form-demo")
	var created struct {
		Formation formations.FormationNode `json:"formation"`
		Gate      formations.GateNode      `json:"gate"`
	}
	if err := json.Unmarshal([]byte(archon("formation", "create", "review", "solo", "--title", "Worker", "--json")), &created); err != nil {
		t.Fatal(err)
	}
	worker := created.Formation
	if err := json.Unmarshal([]byte(archon("gate", "create", "review", "--title", "Signoff", "--json")), &created); err != nil {
		t.Fatal(err)
	}
	if gate := created.Gate; strings.Join(gate.Kinds, ",") != "human" {
		t.Fatalf("gate create without --kinds = %+v, want kinds [human]", gate)
	}
	if err := json.Unmarshal([]byte(archon("gate", "create", "review", "--title", "Lint", "--kinds", "code", "--json")), &created); err != nil || strings.Join(created.Gate.Kinds, ",") != "code" {
		t.Fatalf("gate create --kinds code = %+v (%v), want kinds unchanged", created.Gate, err)
	}
	_, stderr, code := runArchon(t, runner, "--workspace", workspace, "gate", "create", "review", "--check", "output_contains", "--check-version", "1", "--check-value", "OK", "--json")
	if code == 0 || !strings.Contains(stderr, "invalid_code_gate_profile") || !strings.Contains(stderr, "without the code kind") {
		t.Fatalf("gate create with a check and no --kinds = %d %s, want the code kind required", code, stderr)
	}
	archon("gate", "update", "review", "Lint", "--kinds", "human")

	store := formations.NewStore(workspace)
	board, err := store.ReadBoard("review")
	if err != nil {
		t.Fatal(err)
	}
	gate := board.Gates[0]
	archon("formation", "assign", "review", worker.ID, "--slot", worker.Slots[0].ID, "--agent", "codex-builder", "--harness", "openai-codex")
	archon("formation", "set-brief", "review", worker.ID, "--goal", "Produce the result")
	archon("mission", "wire", "review", "Work", worker.ID+":"+worker.Inputs[0].ID)
	archon("formation", "wire", "review", worker.ID+":"+worker.Outputs[0].ID, gate.ID+":in")
	if stdout := archon("board", "validate", "review"); stdout != "review\t0 errors\t0 warnings\n" {
		t.Fatalf("board validate with a wired human gate:\n%s", stdout)
	}
}

func TestRemoteAdmissionFindingsAndBoardValidation(t *testing.T) {
	findings := `[{"code":"unstaffed_slot","nodeId":"fmn_plan","message":"formation \"fmn_plan\" slot \"Planner\" (slot_plan) needs an agent"},{"code":"gate_not_routable","nodeId":"gate_lint","message":"gate \"gate_lint\" needs forbidden text for code check output_absent@1"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/formations/boards/draft":
			w.Write([]byte(`{"success":true,"data":{"board":{"rev":4}}}`))
		case "/api/formations/boards/draft/validation":
			w.Write([]byte(`{"success":true,"data":{"boardRev":4,"errors":` + findings + `,"warnings":[]}}`))
		case "/api/formations/runs":
			w.WriteHeader(422)
			w.Write([]byte(`{"success":false,"error":{"code":"RUN_ADMISSION_FAILED","message":"The run needs 2 fixes before it can start","findings":` + findings + `}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	var out, stderr bytes.Buffer
	if code := runRemote(server.URL, []string{"mission", "run", "draft", "--mission", "mis_draft", "--cwd", t.TempDir(), "--brief", "sketch"}, &out, &stderr); code != 1 {
		t.Fatalf("remote mission run code %d stderr %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "ERROR\tunstaffed_slot\tfmn_plan\t") || !strings.Contains(stderr.String(), "ERROR\tgate_not_routable\tgate_lint\t") {
		t.Fatalf("remote mission run stderr:\n%s", stderr.String())
	}

	out.Reset()
	stderr.Reset()
	if code := runRemote(server.URL, []string{"board", "validate", "draft"}, &out, &stderr); code != 1 {
		t.Fatalf("remote board validate code %d stderr %s", code, stderr.String())
	}
	if !strings.HasPrefix(out.String(), "draft\t2 errors\t0 warnings\n") || strings.Count(out.String(), "ERROR\t") != 2 {
		t.Fatalf("remote board validate stdout:\n%s", out.String())
	}
}
