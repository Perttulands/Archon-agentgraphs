package coordinator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Pause at the real lab executor boundary so both admissions are demonstrably
// live together, and cancellation cannot be confused with fast completion.
type pausedLab struct {
	lab     *formations.LabFormationExecutor
	store   *formations.Store
	entered chan formations.FormationExecution
	finish  chan struct{}
}

func (e *pausedLab) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	return e.ExecuteFormationContext(context.Background(), req)
}
func (e *pausedLab) ExecuteFormationContext(ctx context.Context, req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	e.entered <- req
	select {
	case <-ctx.Done():
		_ = e.store.AppendRunEvent(req.RunID, formations.RunEvent{Type: "seat_cleanup", NodeID: req.NodeID, SlotID: req.Formation.Slots[0].ID, Data: map[string]any{"outcome": "ended"}})
		return formations.FormationExecutionResult{}, ctx.Err()
	case <-e.finish:
		return e.lab.ExecuteFormation(req)
	}
}
func TestConcurrentLabAdmissionInputsAndPerRunAbort(t *testing.T) {
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	e := &pausedLab{entered: make(chan formations.FormationExecution, 8), finish: make(chan struct{})}
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
		e.store = store
		e.lab = formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Cwd: root, Roots: []string{root}, Harnesses: []string{"openai-codex"}})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(e.finish); c.Close() })
	board := strings.Replace(testBoard, `kinds = ["human"]`, `kinds = ["code"]`+"\ncheck = \"output_absent\"\ncheckVersion = \"1\"\ncheckValue = \"FORBIDDEN\"", 1)
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(board), 0600); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, bead := range []string{"form-first", "form-second"} {
		cwd := t.TempDir()
		raw, _ := json.Marshal(map[string]any{"board": "proof", "missionId": "mis_proof", "expectedRev": 1, "cwd": cwd, "brief": "brief for " + bead, "beadId": bead, "limits": formations.RunLimits{MaxDispatch: 4, MaxAttempts: 2, WallClockSeconds: 60}})
		w := post(t, c, "/api/formations/runs", string(raw))
		if w.Code != 202 {
			t.Fatalf("admission %s", w.Body.String())
		}
		var receipt struct {
			Data struct {
				RunID string `json:"runId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, receipt.Data.RunID)
		select {
		case req := <-e.entered:
			if req.Cwd != cwd || req.MissionBeadID != bead || req.MissionGoal != "PRIVATE-OBJECTIVE" || req.Inputs[0].Text != "brief for "+bead {
				t.Fatalf("seat inputs: %+v", req)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("run did not enter lab")
		}
		p, err := c.Project(receipt.Data.RunID)
		if err != nil || p.Cwd != cwd || p.BeadID != bead {
			t.Fatalf("projection %+v %v", p, err)
		}
	}
	signal := c.nextChange(ids[1])
	w := post(t, c, "/api/formations/runs/"+ids[0]+"/abort", `{"reason":"cancel first"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	p := awaitState(t, c, ids[0], "canceled")
	if !p.Final || p.Events[len(p.Events)-1].Type != formations.RunEventCanceled {
		t.Fatal(p)
	}
	cleanup := false
	for _, event := range p.Events {
		if event.Type == "seat_cleanup" && event.Outcome == "ended" {
			cleanup = true
		}
	}
	if !cleanup {
		t.Fatal("missing cleanup before final cancellation")
	}
	select {
	case <-signal:
		t.Fatal("other run woke for first run abort")
	default:
	}
	c.mu.Lock()
	other := c.state(ids[1])
	err = other.ctx.Err()
	c.mu.Unlock()
	if err != nil {
		t.Fatal("other run canceled", err)
	}
	// Let the second run finish its full two-seat graph through the lab executor.
	e.finish <- struct{}{}
	select {
	case <-e.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("second seat not dispatched")
	}
	e.finish <- struct{}{}
	awaitState(t, c, ids[1], "succeeded")
}

func TestRestartLabNamesUnresolvedDispatchAndKeepsHistory(t *testing.T) {
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	open := func() *Coordinator {
		c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
			return formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Cwd: root, Roots: []string{root}})
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := open()
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(testBoard), 0600); err != nil {
		t.Fatal(err)
	}
	started, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", ExpectedBoardRev: 1, Personas: personas, Limits: formations.RunLimits{MaxDispatch: 4, MaxAttempts: 2, WallClockSeconds: 60}})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := formations.NewSlotDispatcher(c.store, nil).DispatchSlot(started.RunID, formations.SlotDispatchRequest{NodeID: "fmn_work", SlotID: "slot_work", Attempt: 1, Prompt: "unfinished"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c = open()
	defer c.Close()
	if err := c.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	p, err := c.Project(started.RunID)
	if err != nil || p.Status != "blocked" || !p.ResumeAllowed || p.Final {
		t.Fatalf("restart %+v %v", p, err)
	}
	events, err := c.store.ReadRunEvents(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.RecoverInterruptedRuns(); err != nil {
		t.Fatalf("second restart: %v", err)
	}
	raw, _ := json.Marshal(events[len(events)-1])
	if !strings.Contains(string(raw), lease.DispatchID) || len(events) != 4 {
		t.Fatalf("lost open dispatch or history: %s", raw)
	}
	response := post(t, c, "/api/formations/runs/"+started.RunID+"/resume", `{"mode":"reattach"}`)
	if response.Code != 202 {
		t.Fatal(response.Body.String())
	}
	// Lab cannot recover a native seat. Explicit resume must retain the open
	// dispatch and block, rather than forgetting it and reporting success.
	awaitState(t, c, started.RunID, "blocked")
	events, err = c.store.ReadRunEvents(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	refs, ok := events[len(events)-1].Data["openDispatches"].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("resume lost interrupted dispatch: %+v", events[len(events)-1])
	}
	ref := refs[0].(map[string]any)
	if ref["dispatchId"] != lease.DispatchID {
		t.Fatalf("unreadable interrupted dispatch: %+v", ref)
	}
}

func TestAbortRejectsReservedCommandWithoutAcknowledgingCancellation(t *testing.T) {
	c, e, _ := fixture(t)
	id := startRun(t, c)
	<-e.entered
	e.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	if !c.acquire(id) {
		t.Fatal("reserve command")
	}
	response := post(t, c, "/api/formations/runs/"+id+"/abort", `{"reason":"during another command"}`)
	c.release(id)
	if response.Code != 409 {
		t.Fatalf("acknowledged cancellation without worker: %d %s", response.Code, response.Body.String())
	}
	response = post(t, c, "/api/formations/runs/"+id+"/abort", `{"reason":"after command"}`)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	awaitState(t, c, id, "canceled")
}
