package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func storageRun(t *testing.T, c *Coordinator) string {
	t.Helper()
	started, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", Personas: c.personas, Inputs: map[string]string{"brief": "storage lifecycle"}})
	if err != nil {
		t.Fatal(err)
	}
	return started.RunID
}

func storageScratch(t *testing.T, c *Coordinator, id string) string {
	t.Helper()
	path := filepath.Join(c.store.Workspace, ".archon", "artifacts", id, "work", "1", "scratch.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("scratch awaiting settlement"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func finalStorageRun(t *testing.T, c *Coordinator, id string) {
	t.Helper()
	if err := c.store.AppendRunEvent(id, formations.RunEvent{Type: formations.RunEventCanceled, Data: map[string]any{"reason": "storage test", "final": true}}); err != nil {
		t.Fatal(err)
	}
}

func TestRunStorageArchiveDisposeAndExpiredEvidence(t *testing.T) {
	c, _, _ := fixture(t)
	id := storageRun(t, c)
	scratch := storageScratch(t, c, id)
	base := "/api/runs/" + id
	report := decodeEvidence[formations.RunStorageReport](t, getEvidence(c, base+"/storage"), "storage")
	if report.Policy != "bounded" || report.Bytes == 0 || report.Archived || report.BudgetBytes != 32<<20 || report.PoolBudgetBytes != 256<<20 {
		t.Fatalf("report: %+v", report)
	}
	if w := post(t, c, base+"/storage", `{"action":"dispose"}`); w.Code != 409 {
		t.Fatalf("nonfinal dispose: %d %s", w.Code, w.Body.String())
	}
	if w := post(t, c, base+"/storage", `{"action":"typo"}`); w.Code != 400 {
		t.Fatalf("invalid action: %d", w.Code)
	}
	archived := decodeEvidence[formations.RunStorageReport](t, post(t, c, base+"/storage", `{"action":"archive"}`), "storage")
	if !archived.Archived {
		t.Fatal("archive choice not persisted")
	}
	finalStorageRun(t, c, id)
	if err := c.sweepStorage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("archive removed: %v", err)
	}
	disposed := decodeEvidence[formations.RunStorageReport](t, post(t, c, base+"/storage", `{"action":"dispose"}`), "storage")
	if disposed.State != "expired" || disposed.Bytes != 0 {
		t.Fatalf("disposed: %+v", disposed)
	}
	for _, path := range []string{"/artifacts/work/1/scratch.bin", "/evidence/artifacts/work/1/scratch.bin"} {
		if w := getEvidence(c, base+path); w.Code != 410 {
			t.Fatalf("expired %s: %d %s", path, w.Code, w.Body.String())
		}
	}
	if w := post(t, c, base+"/storage", `{"action":"archive"}`); w.Code != 409 {
		t.Fatalf("archive expired: %d %s", w.Code, w.Body.String())
	}
	if p, err := c.Project(id); err != nil || p.Status != "canceled" {
		t.Fatalf("history lost: %+v %v", p, err)
	}
}

func TestStorageWaitsForFinalWriterSettlement(t *testing.T) {
	c, _, _ := fixture(t)
	id := storageRun(t, c)
	if !c.acquire(id) {
		t.Fatal("reserve writer")
	}
	released := false
	defer func() {
		if !released {
			c.release(id)
		}
	}()
	scratch := storageScratch(t, c, id)
	finalStorageRun(t, c, id)
	if err := c.sweepStorage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scratch); err != nil {
		t.Fatalf("final event disposed a live writer's files: %v", err)
	}
	if w := post(t, c, "/api/runs/"+id+"/storage", `{"action":"dispose"}`); w.Code != 409 {
		t.Fatalf("live final dispose: %d %s", w.Code, w.Body.String())
	}
	// A late write is legal until the worker returns, even after its final event.
	if err := os.WriteFile(scratch, []byte("last executor cleanup"), 0600); err != nil {
		t.Fatal(err)
	}
	c.release(id)
	released = true
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		report, err := c.store.RunStorage(id)
		if err != nil {
			t.Fatal(err)
		}
		if report.Trimmed {
			if _, err := os.Stat(scratch); !os.IsNotExist(err) {
				t.Fatalf("scratch remains after settled sweep: %v", err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("settled worker did not trigger storage maintenance")
}

type storageLargeOutputExecutor struct{ source string }

func (e storageLargeOutputExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	return formations.FormationExecutionResult{Status: "done", Outputs: map[string]formations.FormationOutputPayload{"port_out": {Ref: e.source}}}, nil
}

func TestStorageArchiveThenSeparateResumeKeepsOverBudgetOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "large.bin")
	file, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((32 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	c, err := Open(root, personas, func(*formations.Store) formations.FormationExecutor {
		return storageLargeOutputExecutor{source: source}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	board := strings.Replace(testBoard, "label = \"Output\"", "label = \"Output\"\nkind = \"file\"", 1)
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(board), 0600); err != nil {
		t.Fatal(err)
	}
	id := startRun(t, c)
	defer func() {
		post(t, c, "/api/runs/"+id+"/abort", `{"reason":"test cleanup","requestedBy":"human:test"}`)
		post(t, c, "/api/runs/"+id+"/storage", `{"action":"dispose"}`)
	}()
	awaitState(t, c, id, "blocked")
	c.mu.Lock()
	done := c.state(id).done
	c.mu.Unlock()
	<-done
	before, err := c.store.ReadRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	problems, err := c.store.ProjectRunProblems(id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, problem := range problems {
		if problem.Code == "output_not_kept" {
			found = true
		}
	}
	if !found {
		t.Fatalf("over-budget output was not explicitly blocked: %+v", problems)
	}
	report := decodeEvidence[formations.RunStorageReport](t, post(t, c, "/api/runs/"+id+"/storage", `{"action":"archive"}`), "storage")
	if !report.Archived || report.Final {
		t.Fatalf("pending archive: %+v", report)
	}
	after, err := c.store.ReadRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatal("archive implicitly resumed the run")
	}
	if w := post(t, c, "/api/runs/"+id+"/resume", `{"actor":"human:test","mode":"redispatch","reason":"explicit archive selected"}`); w.Code != 202 {
		t.Fatalf("resume: %d %s", w.Code, w.Body.String())
	}
	awaitState(t, c, id, "waiting_human")
	artifacts, _, _, err := c.store.ListRunArtifacts(id)
	if err != nil {
		t.Fatal(err)
	}
	kept := ""
	for _, entry := range artifacts {
		if strings.Contains(entry.Name, "/outputs/") {
			kept = filepath.Join(root, ".archon", "artifacts", id, filepath.FromSlash(entry.Name))
			break
		}
	}
	if kept == "" {
		t.Fatalf("archive did not retain independent output: %+v", artifacts)
	}
	sourceFile, err := os.OpenFile(source, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceFile.WriteAt([]byte("changed"), 0); err != nil {
		t.Fatal(err)
	}
	sourceFile.Close()
	snapshot, err := os.Open(kept)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	first := make([]byte, 7)
	if _, err := snapshot.Read(first); err != nil {
		t.Fatal(err)
	}
	if string(first) == "changed" {
		t.Fatal("retained output followed source mutation")
	}
}

func TestStorageProtectsFinalConsumerUntilItsWorkerSettles(t *testing.T) {
	c, _, _ := fixture(t)
	source := storageRun(t, c)
	sourcePath := storageScratch(t, c, source)
	if _, err := c.store.SetRunArchive(source, true); err != nil {
		t.Fatal(err)
	}
	finalStorageRun(t, c, source)
	consumer, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", Personas: c.personas, ContextPaths: []string{sourcePath}, Inputs: map[string]string{"brief": "consume source"}})
	if err != nil {
		t.Fatal(err)
	}
	if !c.acquire(consumer.RunID) {
		t.Fatal("reserve consumer")
	}
	released := false
	defer func() {
		if !released {
			c.release(consumer.RunID)
		}
	}()
	finalStorageRun(t, c, consumer.RunID)
	if w := post(t, c, "/api/runs/"+source+"/storage", `{"action":"dispose"}`); w.Code != 409 {
		t.Fatalf("busy final consumer did not protect its source: %d %s", w.Code, w.Body.String())
	}
	c.release(consumer.RunID)
	released = true
	if w := post(t, c, "/api/runs/"+source+"/storage", `{"action":"dispose"}`); w.Code != 200 {
		t.Fatalf("settled final consumer kept source forever: %d %s", w.Code, w.Body.String())
	}
}

func TestStorageProtectsUnconfirmedFinalSeatsUntilProbeConfirmsGone(t *testing.T) {
	keeper := &keeperExecutor{gone: map[string]bool{}}
	c, _ := onCallFixture(t, testBoard, keeper, nil)
	id := storageRun(t, c)
	storageScratch(t, c, id)
	if _, err := c.store.SetRunArchive(id, true); err != nil {
		t.Fatal(err)
	}
	for _, event := range []formations.RunEvent{
		{Type: formations.RunEventSeatCreated, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"sessionName": "left-writer", "sessionId": "$unconfirmed", "paneId": "%unconfirmed"}},
		{Type: formations.RunEventSeatCleanup, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"sessionName": "left-writer", "outcome": formations.SeatOutcomeLeftCleanupFailed}},
	} {
		if err := c.store.AppendRunEvent(id, event); err != nil {
			t.Fatal(err)
		}
	}
	finalStorageRun(t, c, id)
	report := decodeEvidence[formations.RunStorageReport](t, getEvidence(c, "/api/runs/"+id+"/storage"), "storage")
	if len(report.ActiveSeats) != 1 {
		t.Fatalf("unconfirmed writer hidden: %+v", report)
	}
	if w := post(t, c, "/api/runs/"+id+"/storage", `{"action":"dispose"}`); w.Code != 409 {
		t.Fatalf("unconfirmed writer disposed: %d %s", w.Code, w.Body.String())
	}
	keeper.mu.Lock()
	keeper.gone["$unconfirmed"] = true
	keeper.mu.Unlock()
	if w := post(t, c, "/api/runs/"+id+"/storage", `{"action":"dispose"}`); w.Code != 200 {
		t.Fatalf("verified gone writer blocked forever: %d %s", w.Code, w.Body.String())
	}
}

func TestStorageStartupSweepAndShutdownFence(t *testing.T) {
	c, executor, root := fixture(t)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	// An offline settled run is found by the next coordinator without a final
	// event hook or another command arriving after startup.
	id := storageRun(t, c)
	scratch := storageScratch(t, c, id)
	finalStorageRun(t, c, id)
	next, err := Open(root, c.personas, func(*formations.Store) formations.FormationExecutor { return executor })
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		report, err := next.store.RunStorage(id)
		if err != nil {
			t.Fatal(err)
		}
		if report.Trimmed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup did not sweep settled evidence")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatalf("startup left disposable scratch: %v", err)
	}
	next.BeginShutdown()
	if w := post(t, next, "/api/runs/"+id+"/storage", `{"action":"unarchive"}`); w.Code != 503 {
		t.Fatalf("shutdown admitted storage mutation: %d %s", w.Code, w.Body.String())
	}
}
