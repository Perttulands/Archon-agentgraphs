package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Runs list by ?mission=; an unknown ?board= filters nothing.
func TestRunListFiltersByMission(t *testing.T) {
	c, executor, _ := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	for path, want := range map[string]int{"/api/runs": 1, "/api/runs?mission=proof": 1, "/api/runs?mission=other": 0, "/api/runs?board=other": 1} {
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		var body struct {
			Data []Projection `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || body.Data == nil || len(body.Data) != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if want == 1 && (body.Data[0].RunID != id || body.Data[0].Status != "waiting_human" || len(body.Data[0].WaitingGates) != 1) {
			t.Fatalf("%s: %+v", path, body.Data[0])
		}
	}
}

// A mission lists only its own runs: a mission created again under a deleted
// one's slug starts with none, and the deleted mission's runs stay readable
// by ID and in the full list (archon-n7u.15).
func TestARecreatedMissionDoesNotInheritArchivedRuns(t *testing.T) {
	c, executor, _ := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	if w := post(t, c, "/api/runs/"+id+"/abort", `{"reason":"test","requestedBy":"operator:test"}`); w.Code != 200 {
		t.Fatalf("abort %d %s", w.Code, w.Body.String())
	}
	board, err := c.store.ReadBoard("proof")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.store.DeleteBoard("proof", formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); err != nil {
		t.Fatal(err)
	}
	list := func(path string) []Projection {
		t.Helper()
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		var body struct {
			Data []Projection `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return body.Data
	}
	if runs := list("/api/runs?mission=proof"); len(runs) != 0 {
		t.Fatalf("deleted mission still lists %d runs", len(runs))
	}
	if _, err := c.store.CreateBoard(formations.BoardCreateRequest{Slug: "proof", Title: "Proof again"}); err != nil {
		t.Fatal(err)
	}
	if runs := list("/api/runs?mission=proof"); len(runs) != 0 {
		t.Fatalf("recreated mission inherited %d archived runs", len(runs))
	}
	all := list("/api/runs")
	if len(all) != 1 || all[0].RunID != id || all[0].BoardID != board.ID {
		t.Fatalf("all runs = %+v, want the archived run under its own mission ID", all)
	}
}

// A run is identifiable without its ID: the projection names who started it,
// when, and when it last changed, and a waiting gate says since when. Its
// artifacts carry absolute paths, and the mission as it ran stays readable
// after edits (archon-o7p.2).
func TestRunsSayWhoStartedThemWhenAndWhatTheyRan(t *testing.T) {
	c, executor, _ := fixture(t)
	w := post(t, c, "/api/runs", `{"inputs":{"brief":"run the proof"},"mission":"proof","inputCardId":"mis_proof","expectedRev":1,"actor":"agent:driver"}`)
	if w.Code != 202 {
		t.Fatalf("start %d %s", w.Code, w.Body.String())
	}
	var receipt struct {
		Data struct {
			RunID string `json:"runId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	id := receipt.Data.RunID
	<-executor.entered
	executor.proceed <- struct{}{}
	p := awaitState(t, c, id, "waiting_human")
	started, err := time.Parse(time.RFC3339Nano, p.StartedAt)
	if err != nil || p.StartedBy != "agent:driver" || p.UpdatedAt < p.StartedAt {
		t.Fatalf("run times and driver = %q %q %q (%v)", p.StartedBy, p.StartedAt, p.UpdatedAt, err)
	}
	if asked, err := time.Parse(time.RFC3339Nano, p.WaitingGates[0].RequestedAt); err != nil || asked.Before(started) {
		t.Fatalf("waiting gate asked at %q (%v)", p.WaitingGates[0].RequestedAt, err)
	}
	if startRun(t, c) == "" {
		t.Fatal("second start")
	}
	for _, run := range listRuns(t, c, "/api/runs?mission=proof") {
		if run.RunID != id && run.StartedBy != "operator:standalone" {
			t.Fatalf("a start naming no actor is driven by %q", run.StartedBy)
		}
	}

	artifacts := filepath.Join(c.store.Workspace, ".archon", "artifacts", id)
	if err := os.MkdirAll(filepath.Join(artifacts, "notes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(artifacts, "notes", "plan.md"), []byte("# Plan\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview := decodeEvidence[formations.RunArtifactPreview](t, getEvidence(c, "/api/runs/"+id+"/evidence/artifacts/notes/plan.md"), "artifact")
	if want, _ := filepath.Abs(filepath.Join(artifacts, "notes", "plan.md")); preview.Path != want {
		t.Fatalf("artifact path %q, want %q", preview.Path, want)
	}

	original, err := os.ReadFile(c.store.BoardPath("proof"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(strings.Replace(string(original), "rev = 1", "rev = 2", 1)+"\n# edited later\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ran := decodeEvidence[formations.RunMissionEvidence](t, getEvidence(c, "/api/runs/"+id+"/evidence/mission"), "mission")
	if ran.MissionRev != 1 || ran.Text.Text != string(original) || ran.Text.Truncated {
		t.Fatalf("mission as it ran = rev %d %q", ran.MissionRev, ran.Text.Text)
	}
	if w := getEvidence(c, "/api/runs/run_01NOSUCHRUN0000000000000000/evidence/mission"); w.Code != 404 {
		t.Fatalf("unknown run mission = %d %s", w.Code, w.Body.String())
	}
}

func listRuns(t *testing.T, c *Coordinator, path string) []Projection {
	t.Helper()
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	var body struct {
		Data []Projection `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
		t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
	}
	return body.Data
}

// ?needs=you lists the open runs that need the operator, across missions or
// for one, so the cockpit can count them (archon-n7u.29).
func TestRunListNeedsYouKeepsWaitingAndBlockedRuns(t *testing.T) {
	c, executor, _ := fixture(t)
	waiting := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, waiting, "waiting_human")
	ids := func(path string) []string {
		var out []string
		for _, run := range listRuns(t, c, path) {
			out = append(out, run.RunID+":"+run.Status)
		}
		return out
	}
	if got := ids("/api/runs?needs=you"); len(got) != 1 || got[0] != waiting+":waiting_human" {
		t.Fatalf("needs you = %v", got)
	}
	if got := ids("/api/runs?mission=proof&needs=you"); len(got) != 1 {
		t.Fatalf("mission needs you = %v", got)
	}
	if got := ids("/api/runs?mission=other&needs=you"); len(got) != 0 {
		t.Fatalf("other mission needs you = %v", got)
	}
	if w := post(t, c, "/api/runs/"+waiting+"/abort", `{"reason":"done","requestedBy":"operator:test"}`); w.Code != 200 {
		t.Fatalf("abort %d %s", w.Code, w.Body.String())
	}
	if got := ids("/api/runs?needs=you"); len(got) != 0 {
		t.Fatalf("a canceled run still needs you: %v", got)
	}
	// A run blocked on a spent limit needs the operator too: the mission may
	// run one step, so approving finds its rounds spent.
	limited := testBoard + "[[limit]]\nid = \"lim_mission\"\ntitle = \"Cap\"\ntarget = \"mis_proof\"\nrounds = 1\n"
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(limited), 0600); err != nil {
		t.Fatal(err)
	}
	w := post(t, c, "/api/runs", `{"inputs":{"brief":"one step only"},"mission":"proof","inputCardId":"mis_proof","expectedRev":1}`)
	var receipt struct {
		Data struct {
			RunID string `json:"runId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); w.Code != 202 || err != nil {
		t.Fatalf("start %d %s", w.Code, w.Body.String())
	}
	<-executor.entered
	executor.proceed <- struct{}{}
	seq := awaitState(t, c, receipt.Data.RunID, "waiting_human").WaitingGates[0].RequestedSeq
	if w := post(t, c, "/api/runs/"+receipt.Data.RunID+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"pass"}`); w.Code != 202 {
		t.Fatalf("verdict %d %s", w.Code, w.Body.String())
	}
	awaitState(t, c, receipt.Data.RunID, "blocked")
	if got := ids("/api/runs?needs=you"); len(got) != 1 || got[0] != receipt.Data.RunID+":blocked" {
		t.Fatalf("needs you after a limit block = %v", got)
	}
}
