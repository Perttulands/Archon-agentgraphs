package formations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLabPersonaSnapshotSurvivesEdit(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	card, err := personas.ReadPersona("scout")
	if err != nil {
		t.Fatal(err)
	}
	original := "Original run persona"
	card, err = personas.EditPersona("scout", EditPersonaRequest{ExpectedETag: card.ETag, SetSummary: &original})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, store.BoardPath("session-search"), s5HumanGateBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewRunEngine(store, personas, &fakeRunExecutor{})
	status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev, Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 3}})
	if err != nil {
		t.Fatal(err)
	}
	changed := "Changed during human gate"
	_, err = personas.EditPersona("scout", EditPersonaRequest{ExpectedETag: card.ETag, SetSummary: &changed})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Reason: "approved", Actor: "human:operator"})
	if err != nil {
		t.Fatal(err)
	}
	lab := NewLabFormationExecutor(store, personas, LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: store.Workspace, Roots: []string{store.Workspace}})
	status, err = NewRunEngine(store, personas, lab).ResumeRun(status.RunID, RunResumeRequest{Mode: "reattach", Reason: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := lastEventOfType(t, events, RunEventSlotDispatch)
	raw, err := os.ReadFile(stringFromEventData(dispatch, "briefPath"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("downstream=%s; original summary present=%t; changed summary present=%t; status=%s", dispatch.NodeID, strings.Contains(string(raw), original), strings.Contains(string(raw), changed), status.Status)
	if !strings.Contains(string(raw), original) || strings.Contains(string(raw), changed) {
		t.Fatal("running mission used edited persona instead of admission snapshot")
	}
}

func TestTmuxSeatsKeepAdmittedPersonaSettingsAcrossRestart(t *testing.T) {
	for _, harness := range []string{"openai-codex", "claude-code"} {
		t.Run(harness, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			card, err := personas.CreatePersona(CreatePersonaRequest{ID: "scout", Kind: "specialist", Summary: "admitted summary", Harness: harness, Model: "model-before", Effort: "low"})
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, store.BoardPath("session-search"), strings.ReplaceAll(s5HumanGateBoardFixture()+`
[[connection]]
id = "edge_gate_rework"
from = "gate_review:fail"
to = "fmn_work:port_work_in"
`, "openai-codex", harness))
			cfg := tmuxTestConfig(t)
			cfg.Harnesses = []string{harness}
			client := &fakeTmuxHarnessClient{harness: harness, pane: tmuxPaneState{CurrentPath: cfg.Cwd}}
			executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
			engine := NewRunEngine(store, personas, executor)
			status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 3}})
			if err != nil {
				t.Fatal(err)
			}
			if status.Status != RunStatusRunning {
				t.Fatalf("waiting status = %+v", status)
			}
			newSummary, newModel, newEffort := "edited summary", "model-after", "high"
			_, err = personas.EditPersona("scout", EditPersonaRequest{ExpectedETag: card.ETag, SetSummary: &newSummary, SetModel: &newModel, SetEffort: &newEffort})
			if err != nil {
				t.Fatal(err)
			}
			_, err = engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "fail", Reason: "revise", Actor: "human:operator"})
			if err != nil {
				t.Fatal(err)
			}
			// The replacement executor cannot access the persona store at all.
			executor = newTmuxFormationExecutorWithClient(store, nil, cfg, client)
			status, err = NewRunEngine(store, nil, executor).ResumeRun(status.RunID, RunResumeRequest{Mode: "reattach", Reason: "approved after restart"})
			if err != nil {
				t.Fatal(err)
			}
			if status.Status != RunStatusRunning {
				t.Fatalf("retry status = %+v", status)
			}
			if !strings.Contains(client.lastPrompt, "admitted summary") || strings.Contains(client.lastPrompt, "edited summary") {
				t.Fatal("retry used edited persona")
			}
			engine = NewRunEngine(store, nil, executor)
			if _, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Reason: "approved", Actor: "human:operator"}); err != nil {
				t.Fatal(err)
			}
			status, err = engine.ResumeRun(status.RunID, RunResumeRequest{Mode: "reattach", Reason: "approved rework"})
			if err != nil {
				t.Fatal(err)
			}
			if status.Status != RunStatusSucceeded {
				t.Fatalf("resumed status = %+v", status)
			}
			events, err := store.ReadRunEvents(status.RunID)
			if err != nil {
				t.Fatal(err)
			}
			seat := lastEventOfType(t, events, "seat_created")
			if seat.NodeID != "fmn_ship" || seat.Data["model"] != "model-before" || seat.Data["effort"] != "low" {
				t.Fatalf("resumed seat settings = %+v", seat)
			}
			if !strings.Contains(client.lastPrompt, "admitted summary") || strings.Contains(client.lastPrompt, "edited summary") {
				t.Fatalf("resumed prompt used edited persona: %s", client.lastPrompt)
			}

			// An independently admitted run gets the edit.
			executor = newTmuxFormationExecutorWithClient(store, personas, cfg, client)
			fresh, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_ship", FormationRunRequest{Limits: RunLimits{MaxDispatch: 1}})
			if err != nil {
				t.Fatal(err)
			}
			if fresh.Status != RunStatusSucceeded {
				t.Fatalf("fresh status = %+v", fresh)
			}
			events, err = store.ReadRunEvents(fresh.RunID)
			if err != nil {
				t.Fatal(err)
			}
			seat = lastEventOfType(t, events, "seat_created")
			if seat.Data["model"] != newModel || seat.Data["effort"] != newEffort || !strings.Contains(client.lastPrompt, newSummary) {
				t.Fatalf("new run did not use new persona: %+v", seat)
			}
		})
	}
}

func TestIncompletePersonaSnapshotsBlockNewSeats(t *testing.T) {
	for _, harness := range []string{"lab", "tmux"} {
		t.Run(harness, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
			client := &fakeTmuxHarnessClient{}
			var executor FormationExecutor = NewLabFormationExecutor(store, personas, LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: store.Workspace, Roots: []string{store.Workspace}})
			if harness == "tmux" {
				executor = newTmuxFormationExecutorWithClient(store, personas, tmuxTestConfig(t), client)
			}
			started, execute, err := NewRunEngine(store, personas, executor).PrepareFormationRun("session-search", "fmn_research", FormationRunRequest{})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.Workspace, started.BindingsSnapshotPath)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var legacy []string
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "cardToml =") || strings.HasPrefix(line, "model =") || strings.HasPrefix(line, "effort =") {
					continue
				}
				legacy = append(legacy, strings.Replace(line, "schema = 2", "schema = 1", 1))
			}
			if err := os.WriteFile(path, []byte(strings.Join(legacy, "\n")), 0600); err != nil {
				t.Fatal(err)
			}
			status, err := execute()
			if err != nil {
				t.Fatal(err)
			}
			if status.Status != RunStatusBlocked || status.Final {
				t.Fatalf("status = %+v", status)
			}
			events, err := store.ReadRunEvents(started.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if got := lastEventOfType(t, events, RunEventError).Data["code"]; got != "persona_snapshot_incomplete" {
				t.Fatalf("error = %v", got)
			}
			if len(client.created) != 0 || len(eventNodeOrder(events, RunEventSlotDispatch)) != 0 {
				t.Fatal("incomplete snapshot launched a seat")
			}
		})
	}
}

func TestPersonaSnapshotRejectsAlteredOrMismatchedBindings(t *testing.T) {
	for _, mutation := range []string{"card hash", "settings", "identity", "missing", "duplicate"} {
		t.Run(mutation, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
			started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.Workspace, started.BindingsSnapshotPath)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			switch mutation {
			case "card hash":
				text = strings.Replace(text, `cardSha256 = "`, `cardSha256 = "corrupt-`, 1)
			case "settings":
				text = strings.Replace(text, `effort = "medium"`, `effort = "high"`, 1)
			case "identity":
				text = strings.Replace(text, `boardSlug = "session-search"`, `boardSlug = "other"`, 1)
			case "missing":
				text = strings.Replace(text, `slotId = "slot_research"`, `slotId = "other"`, 1)
			case "duplicate":
				text += text[strings.Index(text, "[[binding]]"):]
			}
			if err := os.WriteFile(path, []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err = store.readRunPersonaBinding(started.RunID, "fmn_research", FormationSlot{ID: "slot_research", AgentID: "scout", Harness: "openai-codex"})
			var executionErr *RunExecutionError
			if !errors.As(err, &executionErr) || executionErr.Code != "persona_snapshot_invalid" {
				t.Fatalf("snapshot mutation error = %v", err)
			}
		})
	}
}

func TestPersonaSnapshotPreservesUnpinnedModelAndResolvesEffort(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(personas.PersonaPath("scout")); err != nil {
		t.Fatal(err)
	}
	card, variant, err := store.readRunPersonaBinding(started.RunID, "fmn_research", FormationSlot{ID: "slot_research", AgentID: "scout", Harness: "openai-codex"})
	if err != nil {
		t.Fatal(err)
	}
	if card.ID != "scout" || variant.Model != "" || variant.Effort != "medium" {
		t.Fatalf("default binding = %+v %+v", card, variant)
	}
}

func TestPersonaSnapshotSizeRejectedBeforeRecordingRun(t *testing.T) {
	for _, mode := range []string{"mission", "formation"} {
		t.Run(mode, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			_, err := personas.CreatePersona(CreatePersonaRequest{ID: "scout", Kind: "specialist", Summary: strings.Repeat("x", int(runtimeAuthorityMaxRecordBytes)), Harness: "openai-codex"})
			if err != nil {
				t.Fatal(err)
			}
			writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
			if mode == "mission" {
				_, err = store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
			} else {
				_, _, err = NewRunEngine(store, personas, &fakeRunExecutor{}).PrepareFormationRun("session-search", "fmn_research", FormationRunRequest{})
			}
			if err == nil || !strings.Contains(err.Error(), "persona snapshot exceeds byte limit") {
				t.Fatalf("admission = %v", err)
			}
			if _, err := os.Stat(filepath.Join(store.Workspace, ".formations", "runs", "session-search")); !os.IsNotExist(err) {
				t.Fatalf("run directory after rejected admission: %v", err)
			}
		})
	}
}
