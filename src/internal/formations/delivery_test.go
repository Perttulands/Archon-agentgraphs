package formations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeliveryMissionLabPushback(t *testing.T) {
	store, personas := s4RunFixture(t)
	for name, dest := range map[string]string{
		"delivery.formation.toml": store.BoardPath("delivery"),
		"delivery.notes.toml":     store.NotesPath("delivery"),
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "examples", name))
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, dest, string(raw))
	}
	board, err := store.ReadBoard("delivery")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := store.ReadBoardNotes("delivery")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes.Board) != 1 || len(notes.Elements) != 7 {
		t.Fatalf("imported notes: %+v", notes)
	}
	for _, note := range notes.Elements {
		if !boardHasNoteTarget(board, note.NodeID) || len(note.Entries) != 1 || note.Entries[0].Text == "" || note.Entries[0].Author != "agent:archon" {
			t.Fatalf("unusable template note: %+v", note)
		}
	}
	cwd := t.TempDir()
	executor := &scriptedLabExecutor{
		lab: NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: cwd, Roots: []string{cwd}, Harnesses: []string{"claude-code", "openai-codex"}}),
		responses: map[string]map[int]string{
			"fmn_plan":           {1: "plan.md: implement the target brief"},
			"fmn_beads":          {1: "task-one: draft graph; plan.md", 2: "task-one: linked graph with verification; plan.md"},
			"fmn_beads_reviewer": {1: judgeBlock("fail", "add the parent link and verification", "task-one has no parent"), 2: judgeBlock("pass", "graph ready", "bd lint passed; parent and dependencies checked")},
			"fmn_execution":      {1: "commit abc123; closed task-one; tests passed; plan.md"},
			"fmn_final_review":   {1: "final-review.md: pass; diff, closure and plan checked"},
		},
	}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("delivery", RunStartRequest{MissionID: "mis_delivery", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev, Limits: RunLimits{MaxDispatch: 20, MaxAttempts: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusSucceeded || !status.Final {
		t.Fatalf("delivery status: %+v", status)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	skills := map[string][]string{
		"fmn_plan": {"brainstorm"}, "fmn_beads": {"beads-drafting"}, "fmn_beads_reviewer": {"review-beads", "bd lint"},
		"fmn_execution": {"beads-driven-development", "beads skill"}, "fmn_final_review": {"code-review"},
	}
	for _, call := range executor.calls {
		order = append(order, call.NodeID)
		for _, slot := range call.Formation.Slots {
			card, err := personas.ReadPersona(slot.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			variant, err := card.SelectHarnessVariant(slot.Harness)
			if err != nil {
				t.Fatal(err)
			}
			prompt := executor.lab.renderPrompt(call, slot, *card, variant)
			required := append([]string{card.Summary, "target repository", "mission bead", "Git", "tmux", "chrote-outputs", "CHROTE-DONE"}, skills[call.NodeID]...)
			for _, port := range call.Formation.Inputs {
				required = append(required, port.ID)
			}
			for _, port := range call.Formation.Outputs {
				required = append(required, port.ID)
			}
			if call.NodeID == "fmn_beads" && call.Attempt == 2 {
				required = append(required, "gate feedback from gate_beads_review, attempt 1:", "reason: add the parent link and verification", "evidence: task-one has no parent", "original input: task-one: draft graph; plan.md")
			}
			if call.NodeID == "fmn_execution" {
				required = append(required, "task-one: linked graph with verification; plan.md")
			}
			if call.NodeID == "fmn_final_review" {
				required = append(required, "commit abc123; closed task-one; tests passed; plan.md")
			}
			for _, want := range required {
				if !strings.Contains(prompt, want) {
					t.Errorf("%s/%s attempt %d prompt missing %q", call.NodeID, slot.ID, call.Attempt, want)
				}
			}
			matched := false
			for _, event := range events {
				if event.Type == RunEventSlotDispatch && event.NodeID == call.NodeID && event.SlotID == slot.ID && event.Attempt == call.Attempt {
					if stringFromEventData(event, "promptSha256") != etag([]byte(prompt)) {
						t.Fatal("prompt differs from dispatched lab prompt")
					}
					matched = true
				}
			}
			if !matched {
				t.Fatalf("missing dispatch for %s/%s attempt %d", call.NodeID, slot.ID, call.Attempt)
			}
		}
	}
	wantOrder := []string{"fmn_plan", "fmn_beads", "fmn_beads_reviewer", "fmn_beads", "fmn_beads_reviewer", "fmn_execution", "fmn_final_review"}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("node order = %v, want %v", order, wantOrder)
	}
	var verdicts []string
	for _, event := range events {
		if event.Type == RunEventGateVerdict {
			verdicts = append(verdicts, stringFromEventData(event, "verdict"))
		}
		if event.Type == RunEventGateVerdict || event.Type == RunEventSucceeded || event.Type == RunEventSlotDispatch || (event.Type == RunEventNodeStarted && event.NodeID == "fmn_beads" && event.Attempt == 2) {
			raw, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			t.Log(string(raw))
		}
	}
	if !reflect.DeepEqual(verdicts, []string{"fail", "pass"}) {
		t.Fatalf("gate verdicts: %v", verdicts)
	}
	execution := callsByNode(executor.calls)["fmn_execution"][0].Formation
	if execution.Type != "orchestrated" || len(execution.Slots) != 4 {
		t.Fatalf("execution team: %+v", execution)
	}
	for i, slot := range execution.Slots {
		if i == 0 {
			if !slot.Controller || slot.Harness != "claude-code" {
				t.Fatalf("controller: %+v", slot)
			}
		} else if slot.Controller || slot.Harness != "openai-codex" || slot.AgentID != "delivery-worker" {
			t.Fatalf("worker: %+v", slot)
		}
	}
}
