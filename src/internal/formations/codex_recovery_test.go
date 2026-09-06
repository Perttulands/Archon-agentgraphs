package formations

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletedNativeRecoveryValidatesBeforeResumeAndNeverRedispatches(t *testing.T) {
	for _, kind := range []string{"valid file output", "wrong native session", "changed brief", "incomplete turn", "no unresolved dispatch", "two unresolved dispatches"} {
		t.Run(kind, func(t *testing.T) {
			store, started := startS4DispatchRun(t)
			brief := filepath.Join(store.Workspace, "brief.md")
			writeFixture(t, brief, "Do the work")
			dispatcher := NewSlotDispatcher(store, nil)
			lease, err := dispatcher.DispatchSlot(started.RunID, SlotDispatchRequest{NodeID: "fmn_research", SlotID: "slot_research", AgentID: "scout", Harness: "openai-codex", Prompt: "Do the work", Attempt: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err := store.AppendRunEvent(started.RunID, RunEvent{Type: "seat_prompt_consumed", NodeID: "fmn_research", SlotID: "slot_research", Data: map[string]any{"dispatchId": lease.DispatchID, "nativeSessionId": "native-one"}}); err != nil {
				t.Fatal(err)
			}
			artifact := filepath.Join(store.Workspace, "guide.md")
			writeFixture(t, artifact, strings.Repeat("useful guide\n", 1000))
			outputs, err := json.Marshal(map[string]FormationOutputPayload{"port_research_out": {Ref: artifact}})
			if err != nil {
				t.Fatal(err)
			}
			final := "```chrote-outputs\n" + string(outputs) + "\n```\n<<<CHROTE-DONE run-id=" + started.RunID + " status=ok artifact=guide.md>>>"
			pointer := "Read the file " + brief + " and execute it exactly; it is your whole brief."
			nativeID := "native-one"
			if kind == "wrong native session" {
				nativeID = "native-other"
			}
			transcript := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":%q}}
{"type":"turn_context","payload":{"turn_id":"turn-one","model":"gpt-6-astra","effort":"xhigh"}}
{"type":"response_item","payload":{"type":"message","role":"user","content":[{"text":%q}]}}
{"type":"response_item","payload":{"type":"message","role":"assistant","phase":"final_answer","content":[{"text":%q}]}}
`, nativeID, store.Workspace, pointer, final)
			if kind != "incomplete turn" {
				transcript += "{\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"turn-one\"}}\n"
			}
			transcriptPath := filepath.Join(store.Workspace, "native.jsonl")
			writeFixture(t, transcriptPath, transcript)
			if kind == "changed brief" {
				writeFixture(t, brief, "Different work")
			}
			if kind == "no unresolved dispatch" {
				if err := dispatcher.CompleteFromCapture(started.RunID, lease.DispatchID, final); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "two unresolved dispatches" {
				if _, err := dispatcher.DispatchSlot(started.RunID, SlotDispatchRequest{NodeID: "fmn_research", SlotID: "slot_other", Prompt: "Different work", Attempt: 1}); err != nil {
					t.Fatal(err)
				}
			}
			// This reproduces the real adapter timeout, whose legacy blocked event
			// omitted the unresolved dispatch even though the ledger retained it.
			if err := store.AppendRunEvent(started.RunID, RunEvent{Type: RunEventBlocked, NodeID: "fmn_research", Data: map[string]any{"reason": "context deadline exceeded", "resumeAllowed": true, "openDispatches": []map[string]any{}}}); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(findOnlyRunLedger(t, store, "session-search"))
			if err != nil {
				t.Fatal(err)
			}
			executor := NewCodexSeatExecutor(store, nil, CodexSeatConfig{Cwd: store.Workspace, StateDir: store.Workspace, Model: "gpt-6-astra", Effort: "xhigh", RecoveryBrief: brief, RecoveryTranscript: transcriptPath})
			engine := NewRunEngine(store, nil, executor)
			status, err := engine.ResumeRun(started.RunID, RunResumeRequest{Mode: "completed-native-turn"})
			if kind != "valid file output" {
				if err == nil {
					t.Fatal("invalid recovery accepted")
				}
				after, readErr := os.ReadFile(findOnlyRunLedger(t, store, "session-search"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(before) != string(after) {
					t.Fatal("rejected recovery changed the blocked ledger")
				}
				return
			}
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
			dispatches, recovered := 0, 0
			for _, event := range events {
				if event.Type == RunEventSlotDispatch {
					dispatches++
				}
				if event.Type == "seat_result_recovered" {
					recovered++
				}
			}
			if dispatches != 1 || recovered != 1 {
				t.Fatalf("dispatches %d recovered %d", dispatches, recovered)
			}
		})
	}
}
