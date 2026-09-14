package coordinator

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Use the production multi-slot reattach rejection, and lab seats only for
// the subsequently authorized new attempt. No transport can create a real seat.
type reattachThenLab struct {
	*formations.TmuxFormationExecutor
	lab *formations.LabFormationExecutor
}

func (e *reattachThenLab) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	return e.lab.ExecuteFormation(req)
}
func (e *reattachThenLab) ExecuteFormationContext(ctx context.Context, req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	return e.lab.ExecuteFormationContext(ctx, req)
}

func TestOrchestratedReattachFailureRemainsResumableAndRedispatchWorks(t *testing.T) {
	c, _, root := fixture(t)
	board := strings.Replace(testBoard, `type = "solo"`, `type = "orchestrated"`, 1)
	board = strings.Replace(board, "[[gate]]", `[[formation.slot]]
id = "slot_worker"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
controller = false
[[gate]]`, 1)
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(board), 0600); err != nil {
		t.Fatal(err)
	}
	started, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", ExpectedBoardRev: 1, Personas: c.personas, Cwd: root, Brief: "recover", Limits: formations.RunLimits{MaxDispatch: 8, MaxAttempts: 3, WallClockSeconds: 600}})
	if err != nil {
		t.Fatal(err)
	}
	id := started.RunID
	for _, event := range []formations.RunEvent{
		{Type: formations.RunEventNodeStarted, NodeID: "mis_proof", Data: map[string]any{"nodeKind": "mission", "reason": "initial", "inputRefs": []formations.RunInputRef{}}},
		{Type: formations.RunEventNodeOutput, NodeID: "mis_proof", Data: map[string]any{"status": "done", "outputs": map[string]any{"out": map[string]any{"text": "recover"}}}},
		{Type: formations.RunEventNodeStarted, NodeID: "fmn_work", Attempt: 1, Data: map[string]any{"nodeKind": "formation", "reason": "initial", "inputRefs": []formations.RunInputRef{{EdgeID: "edge_start", FromNodeID: "mis_proof", FromPortID: "out", ToPortID: "port_in", OutputSeq: 3, Text: "recover"}}}},
	} {
		if err := c.store.AppendRunEvent(id, event); err != nil {
			t.Fatal(err)
		}
	}
	lease, err := formations.NewSlotDispatcher(c.store, nil).DispatchSlot(id, formations.SlotDispatchRequest{NodeID: "fmn_work", SlotID: "slot_work", AgentID: "codex-builder", Harness: "openai-codex", SessionRef: "tmux:old-controller", Prompt: "old brief", Attempt: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = Open(root, c.personas, func(store *formations.Store) formations.FormationExecutor {
		return &reattachThenLab{TmuxFormationExecutor: formations.NewTmuxFormationExecutor(store, c.personas, formations.TmuxExecutorConfig{}), lab: formations.NewLabFormationExecutor(store, c.personas, formations.LabExecutorConfig{Cwd: root, Roots: []string{root}, Harnesses: []string{"openai-codex"}})}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	before := awaitState(t, c, id, "blocked")
	if !before.ResumeAllowed || before.Final {
		t.Fatal(before)
	}
	if w := post(t, c, "/api/formations/runs/"+id+"/resume", `{"mode":"reattach","reason":"inspect completed evidence"}`); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	p := awaitState(t, c, id, "blocked")
	if !p.ResumeAllowed || p.Final {
		t.Fatal(p)
	}
	events, _ := c.store.ReadRunEvents(id)
	failure := events[len(events)-2]
	if failure.Data["code"] != "dispatch_reattach_failed" || !strings.Contains(failure.Data["reason"].(string), "single-slot") {
		t.Fatalf("wrong rejection: %+v", failure)
	}
	raw, _ := json.Marshal(events[len(events)-1])
	if !strings.Contains(string(raw), lease.DispatchID) {
		t.Fatalf("lost dispatch: %s", raw)
	}
	for _, event := range events {
		if event.Type == formations.RunEventSlotResult || event.Type == formations.RunEventFailed {
			t.Fatalf("reattach changed old outcome: %+v", event)
		}
	}
	if w := post(t, c, "/api/formations/runs/"+id+"/resume", `{"mode":"redispatch","reason":"old controller lost; new bounded attempt"}`); w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	awaitState(t, c, id, "waiting_human")
	events, _ = c.store.ReadRunEvents(id)
	abandoned, newDispatches, secondAttempt := 0, 0, false
	for _, event := range events {
		if event.Type == formations.RunEventSlotResult && event.Data["dispatchId"] == lease.DispatchID && event.Data["status"] == "abandoned" {
			abandoned++
		}
		if event.Type == formations.RunEventSlotDispatch && event.Data["dispatchId"] != lease.DispatchID {
			newDispatches++
		}
		if event.Type == formations.RunEventNodeStarted && event.NodeID == "fmn_work" && event.Attempt == 2 {
			secondAttempt = true
		}
	}
	if abandoned != 1 || newDispatches != 2 || !secondAttempt {
		t.Fatalf("abandoned=%d newDispatches=%d secondAttempt=%v", abandoned, newDispatches, secondAttempt)
	}
}
