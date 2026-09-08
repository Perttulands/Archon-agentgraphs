package formations

import (
	"path/filepath"
	"testing"
)

// A seat can die mid-turn (or its transcript can be unreadable) after the
// dispatch was recorded. The operator must be able to abandon that dispatch and
// run the node again, and a failed reattach must leave the run resumable
// rather than finishing it.
func TestResumeRedispatchAbandonsOpenDispatchAndReattachFailureBlocks(t *testing.T) {
	for _, kind := range []string{"redispatch", "reattach failure"} {
		t.Run(kind, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			store.Now = fixedClock()
			personas.Now = fixedClock()
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatal(err)
			}
			started, err := store.StartRun("session-search", RunStartRequest{
				MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
				Personas: personas, Limits: RunLimits{MaxDispatch: 10, MaxAttempts: 3, WallClockSeconds: 600},
			})
			if err != nil {
				t.Fatal(err)
			}
			// The events a real run records before a seat is dispatched.
			for _, event := range []RunEvent{
				{Type: RunEventNodeStarted, NodeID: "mis_showcase", Data: map[string]any{"nodeKind": "mission", "reason": "initial", "inputRefs": []RunInputRef{}}},
				{Type: RunEventNodeOutput, NodeID: "mis_showcase", Data: map[string]any{"status": "done", "outputs": map[string]any{"out": map[string]any{"text": "Ship a showcase"}}}},
				{Type: RunEventNodeStarted, NodeID: "fmn_research", Attempt: 1, Data: map[string]any{"nodeKind": "formation", "reason": "initial", "inputRefs": []RunInputRef{{EdgeID: "edge_mission_research", FromNodeID: "mis_showcase", FromPortID: "out", ToPortID: "port_research_in", OutputSeq: 3, Text: "Ship a showcase"}}}},
			} {
				if err := store.AppendRunEvent(started.RunID, event); err != nil {
					t.Fatal(err)
				}
			}
			brief := filepath.Join(store.Workspace, "briefs", "brief.md")
			writeFixture(t, brief, "Do the work")
			lease, err := NewSlotDispatcher(store, nil).DispatchSlot(started.RunID, SlotDispatchRequest{NodeID: "fmn_research", SlotID: "slot_research", AgentID: "scout", Harness: "openai-codex", Prompt: "Do the work", BriefPath: brief, Attempt: 1})
			if err != nil {
				t.Fatal(err)
			}
			open := []map[string]any{{"dispatchId": lease.DispatchID, "nodeId": "fmn_research", "slotId": "slot_research"}}
			if err := store.AppendRunEvent(started.RunID, RunEvent{Type: RunEventError, NodeID: "fmn_research", SlotID: "slot_research", Data: map[string]any{"code": "native_turn_failed", "message": "seat died", "recoverable": true, "dispatchId": lease.DispatchID}}); err != nil {
				t.Fatal(err)
			}
			if err := store.AppendRunEvent(started.RunID, RunEvent{Type: RunEventBlocked, NodeID: "fmn_research", Data: map[string]any{"reason": "seat died", "blockedNodeId": "fmn_research", "resumeAllowed": true, "resumePolicy": "explicit", "openDispatches": open, "nextEpoch": 1}}); err != nil {
				t.Fatal(err)
			}
			if kind == "reattach failure" {
				executor := NewTmuxFormationExecutor(store, personas, TmuxExecutorConfig{Cwd: store.Workspace, StateDir: store.Workspace, Roots: []string{store.Workspace}, OutputCapBytes: 1 << 20, RecoveryBrief: filepath.Join(store.Workspace, "missing.md"), RecoveryTranscript: filepath.Join(store.Workspace, "missing.jsonl")})
				status, err := NewRunEngine(store, personas, executor).ResumeRun(started.RunID, RunResumeRequest{Mode: "reattach", Actor: "agent:test"})
				if err != nil {
					t.Fatalf("reattach failure must not be an error: %v", err)
				}
				if status.Status != RunStatusBlocked || status.Final || !status.ResumeAllowed {
					t.Fatalf("status = %+v", status)
				}
				events, err := store.ReadRunEvents(started.RunID)
				if err != nil {
					t.Fatal(err)
				}
				last := events[len(events)-2]
				if last.Type != RunEventError || stringFromEventData(last, "code") != "dispatch_reattach_failed" || stringFromEventData(last, "reason") == "" {
					t.Fatalf("last error = %+v", last)
				}
				if len(unresolvedDispatches(events)) != 1 {
					t.Fatal("open dispatch must remain open after a failed reattach")
				}
				return
			}
			lab := NewLabFormationExecutor(store, personas, LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: store.Workspace, Roots: []string{store.Workspace}})
			status, err := NewRunEngine(store, personas, lab).ResumeRun(started.RunID, RunResumeRequest{Mode: "redispatch", Actor: "agent:test", Reason: "seat died; run the node again"})
			if err != nil {
				t.Fatal(err)
			}
			if status.Status != RunStatusSucceeded || !status.Final {
				t.Fatalf("status = %+v", status)
			}
			events, err := store.ReadRunEvents(started.RunID)
			if err != nil {
				t.Fatal(err)
			}
			abandoned, dispatches, secondAttempt := 0, 0, false
			for _, event := range events {
				if event.Type == RunEventSlotResult && stringFromEventData(event, "status") == "abandoned" && stringFromEventData(event, "dispatchId") == lease.DispatchID {
					abandoned++
				}
				if event.Type == RunEventSlotDispatch {
					dispatches++
				}
				if event.Type == RunEventNodeStarted && event.NodeID == "fmn_research" && event.Attempt == 2 {
					secondAttempt = true
				}
			}
			if abandoned != 1 || dispatches != 2 || !secondAttempt {
				t.Fatalf("abandoned %d dispatches %d secondAttempt %v", abandoned, dispatches, secondAttempt)
			}
			if len(unresolvedDispatches(events)) != 0 {
				t.Fatal("abandoned dispatch still counts as open")
			}
		})
	}
}
