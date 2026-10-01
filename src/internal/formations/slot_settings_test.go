package formations

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// vanillaSlotBoard is s4RunBoardFixture with a slot that has no role.
func vanillaSlotBoard(settings string) string {
	return strings.Replace(s4RunBoardFixture(), "agentId = \"scout\"\nharness = \"openai-codex\"\ncontroller = true\neffort = \"medium\"\n", settings+"controller = true\n", 1)
}

func stubHarnessCLIs(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return bin
}

func TestSlotSettingsRoundTripThroughTOML(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), vanillaSlotBoard("harness = \"claude-code\"\nmodel = \"opus\"\neffort = \"low\"\n"))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	slot := board.Formations[0].Slots[0]
	if slot.AgentID != "" || slot.Harness != "claude-code" || slot.Model != "opus" || slot.Effort != "low" || !slot.Staffed() {
		t.Fatalf("read slot = %+v, want a vanilla claude-code opus low slot", slot)
	}
	if got := slot.StaffingSummary(); got != "vanilla · claude-code · opus · low" {
		t.Fatalf("summary = %q", got)
	}

	// A write re-renders the slot through the same keys, and a new board's
	// slots render them too.
	assigned, err := store.AssignFormationSlot("session-search", FormationSlotAssignmentRequest{FormationID: "fmn_research", SlotID: "slot_research", AgentID: "scout", Harness: "openai-codex", Model: "gpt-6-astra", Effort: "xhigh"}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		t.Fatal(err)
	}
	slot = assigned.Formations[0].Slots[0]
	if slot.AgentID != "scout" || slot.Harness != "openai-codex" || slot.Model != "gpt-6-astra" || slot.Effort != "xhigh" {
		t.Fatalf("assigned slot = %+v", slot)
	}
	raw := readFile(t, store.BoardPath("session-search"))
	for _, want := range []string{`agentId = "scout"`, `harness = "openai-codex"`, `model = "gpt-6-astra"`, `effort = "xhigh"`} {
		if strings.Count(raw, want) != 1 {
			t.Fatalf("board TOML has %q %d times:\n%s", want, strings.Count(raw, want), raw)
		}
	}
	reread, err := store.ReadBoard("session-search")
	if err != nil || reread.Formations[0].Slots[0] != slot {
		t.Fatalf("reread slot = %+v (%v), want %+v", reread.Formations[0].Slots[0], err, slot)
	}
	rendered := renderSlotBlock(slot)
	var lines []string
	for _, line := range rendered {
		lines = append(lines, line.body)
	}
	if got := strings.Join(lines, "\n"); !strings.Contains(got, `model = "gpt-6-astra"`) || !strings.Contains(got, `effort = "xhigh"`) {
		t.Fatalf("rendered slot block:\n%s", got)
	}
}

func TestAssigningASlotStatesItsSettings(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), vanillaSlotBoard(""))
	assign := func(req FormationSlotAssignmentRequest) (*BoardDocument, error) {
		t.Helper()
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatal(err)
		}
		req.FormationID, req.SlotID, req.Personas = "fmn_research", "slot_research", personas
		return store.AssignFormationSlot("session-search", req, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	}
	slotOf := func(board *BoardDocument) FormationSlot { return board.Formations[0].Slots[0] }

	for _, refused := range []struct {
		name string
		req  FormationSlotAssignmentRequest
		want string
	}{
		{"no effort", FormationSlotAssignmentRequest{Harness: "claude-code", Model: "opus"}, "needs an effort; the policy is low for errands, medium for making things, xhigh for architecture and review, max for consequential reviews"},
		{"no harness", FormationSlotAssignmentRequest{Effort: "low"}, "needs a harness: claude-code or openai-codex"},
		{"ultra on claude", FormationSlotAssignmentRequest{Harness: "claude-code", Effort: "ultra"}, `effort "ultra" is not one claude-code accepts; use low, medium, high, xhigh, max`},
		{"a harness that cannot start seats", FormationSlotAssignmentRequest{Harness: "hermes", Effort: "low"}, `harness "hermes" cannot start seats`},
		{"a model with spaces", FormationSlotAssignmentRequest{Harness: "claude-code", Model: "opus 5", Effort: "low"}, "one model name without spaces"},
		{"an unknown role alone", FormationSlotAssignmentRequest{AgentID: "nobody"}, `role "nobody" is not a known persona`},
		{"a role with a model and no effort", FormationSlotAssignmentRequest{AgentID: "scout", Model: "opus"}, "needs a harness"},
	} {
		if _, err := assign(refused.req); !errors.Is(err, ErrInvalidSlotSettings) || !strings.Contains(err.Error(), refused.want) {
			t.Errorf("%s: error = %v, want %q", refused.name, err, refused.want)
		}
	}
	if raw := readFile(t, store.BoardPath("session-search")); strings.Contains(raw, "effort") {
		t.Fatalf("a refused assignment was written:\n%s", raw)
	}

	// A vanilla agent: no role.
	board, err := assign(FormationSlotAssignmentRequest{Harness: "claude-code", Model: "opus", Effort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if slot := slotOf(board); slot != (FormationSlot{ID: "slot_research", Label: "Researcher", Harness: "claude-code", Model: "opus", Effort: "low", Controller: true}) {
		t.Fatalf("vanilla slot = %+v", slot)
	}
	// New keys join the slot's block, before the blank line that ends it.
	if raw := readFile(t, store.BoardPath("session-search")); !strings.Contains(raw, "controller = true\nharness = \"claude-code\"\nmodel = \"opus\"\neffort = \"low\"\n\n[[connection]]") {
		t.Fatalf("slot keys landed outside the slot block:\n%s", raw)
	}
	// A full statement replaces every setting: the model returns to the default.
	board, err = assign(FormationSlotAssignmentRequest{AgentID: "scout", Harness: "openai-codex", Effort: "xhigh"})
	if err != nil {
		t.Fatal(err)
	}
	if slot := slotOf(board); slot.AgentID != "scout" || slot.Harness != "openai-codex" || slot.Model != "" || slot.Effort != "xhigh" {
		t.Fatalf("role slot = %+v", slot)
	}
	// The cockpit's role drag names only a role and harness; the role's current
	// settings are written onto the slot.
	board, err = assign(FormationSlotAssignmentRequest{AgentID: "delivery-final-reviewer", Harness: "openai-codex"})
	if err != nil {
		t.Fatal(err)
	}
	if slot := slotOf(board); slot.AgentID != "delivery-final-reviewer" || slot.Harness != "openai-codex" || slot.Model != "gpt-6-astra" || slot.Effort != "medium" {
		t.Fatalf("role drag slot = %+v, want the preset's settings written down", slot)
	}
	// Nothing named empties the slot.
	board, err = assign(FormationSlotAssignmentRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if slot := slotOf(board); slot.Staffed() {
		t.Fatalf("emptied slot = %+v", slot)
	}
}

func TestAdmissionReadsTheSlotsOwnSettings(t *testing.T) {
	store, personas := s4RunFixture(t)
	for _, check := range []struct {
		name     string
		settings string
		code     string
		message  string
	}{
		{"vanilla needs no role", "harness = \"claude-code\"\neffort = \"low\"\n", "", ""},
		{"a role adds only role text", "agentId = \"delivery-worker\"\nharness = \"claude-code\"\nmodel = \"opus\"\neffort = \"xhigh\"\n", "", ""},
		{"an effort is required", "harness = \"claude-code\"\nmodel = \"opus\"\n", FindingInvalidSlotSettings, `slot "Researcher" (slot_research) needs an effort`},
		{"the harness must accept the effort", "harness = \"claude-code\"\neffort = \"ultra\"\n", FindingInvalidSlotSettings, `effort "ultra" is not one claude-code accepts`},
		{"a named role must exist", "agentId = \"nobody-here\"\nharness = \"claude-code\"\neffort = \"low\"\n", FindingUnavailablePersona, `names unknown role "nobody-here"`},
		{"an empty slot is unstaffed", "", FindingUnstaffedSlot, "needs a harness and effort, and optionally a role"},
	} {
		t.Run(check.name, func(t *testing.T) {
			writeFixture(t, store.BoardPath("session-search"), vanillaSlotBoard(check.settings))
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatal(err)
			}
			report := ValidateRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_showcase"})
			if check.code == "" {
				if len(report.Errors) != 0 {
					t.Fatalf("errors = %+v, want none", report.Errors)
				}
				return
			}
			if len(report.Errors) != 1 || report.Errors[0].Code != check.code || !strings.Contains(report.Errors[0].Message, check.message) {
				t.Fatalf("errors = %+v, want one %s containing %q", report.Errors, check.code, check.message)
			}
		})
	}
}

// A seat starts from its slot's settings and the run freezes them: a vanilla
// slot needs no persona store at all, and later edits reach only new runs.
func TestSeatsLaunchFromTheSlotsSettings(t *testing.T) {
	bin := stubHarnessCLIs(t)
	store, personas := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), vanillaSlotBoard("harness = \"claude-code\"\nmodel = \"opus\"\neffort = \"low\"\n"))
	cfg := tmuxTestConfig(t)
	cfg.Harnesses = []string{"claude-code"}
	client := &fakeTmuxHarnessClient{harness: "claude-code", pane: tmuxPaneState{CurrentPath: cfg.Cwd}}
	// No persona store: a vanilla slot needs none.
	status, err := NewRunEngine(store, nil, newTmuxFormationExecutorWithClient(store, nil, cfg, client)).RunFormation("session-search", "fmn_research", FormationRunRequest{Limits: RunLimits{MaxDispatch: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status = %+v", status)
	}
	if len(client.seatVariants) != 1 {
		t.Fatalf("seats = %+v, want one", client.seatVariants)
	}
	launch, err := client.seatVariants[0].LaunchCommand()
	if want := "exec '" + filepath.Join(bin, "claude") + "' --model 'opus' --effort 'low' --dangerously-skip-permissions"; err != nil || launch != want {
		t.Fatalf("seat launch = %q (%v), want %q", launch, err, want)
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	seat := lastEventOfType(t, events, "seat_created")
	if seat.Data["harness"] != "claude-code" || seat.Data["model"] != "opus" || seat.Data["effort"] != "low" {
		t.Fatalf("seat_created = %+v, want the slot's settings", seat.Data)
	}
	dispatch := lastEventOfType(t, events, RunEventSlotDispatch)
	if dispatch.Data["agentId"] != "" || !strings.Contains(client.lastPrompt, "agent: vanilla (no role)") {
		t.Fatalf("dispatch = %+v prompt=%q, want a vanilla seat", dispatch.Data, client.lastPrompt)
	}
	raw := readFile(t, filepath.Join(store.Workspace, ".archon", "runs", "session-search", status.RunID+".bindings.toml"))
	if !strings.Contains(raw, "schema = 3") || strings.Contains(raw, "cardToml") || !strings.Contains(raw, `effort = "low"`) || !strings.Contains(raw, `model = "opus"`) {
		t.Fatalf("bindings snapshot:\n%s", raw)
	}

	// A role slot freezes its role card for role text; the settings are still
	// the slot's, never the card's.
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), vanillaSlotBoard("agentId = \"scout\"\nharness = \"claude-code\"\neffort = \"max\"\n"))
	client = &fakeTmuxHarnessClient{harness: "claude-code", pane: tmuxPaneState{CurrentPath: cfg.Cwd}}
	status, err = NewRunEngine(store, personas, newTmuxFormationExecutorWithClient(store, personas, cfg, client)).RunFormation("session-search", "fmn_research", FormationRunRequest{Limits: RunLimits{MaxDispatch: 1}})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("role run = %+v (%v)", status, err)
	}
	if v := client.seatVariants[0]; v.ID != "claude-code" || v.Model != "" || v.Effort != "max" {
		t.Fatalf("role seat variant = %+v, want the slot's claude-code max, not the card's openai-codex", v)
	}
	if !strings.Contains(client.lastPrompt, "agent: scout") {
		t.Fatalf("role prompt = %q", client.lastPrompt)
	}
}

func TestSchemaThreeSnapshotRejectsSettingsThatDisagreeWithTheSlot(t *testing.T) {
	store, personas := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), vanillaSlotBoard("harness = \"claude-code\"\neffort = \"low\"\n"))
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
	if err != nil {
		t.Fatal(err)
	}
	slot := FormationSlot{ID: "slot_research", Harness: "claude-code", Effort: "low"}
	if _, variant, err := store.readRunPersonaBinding(started.RunID, "fmn_research", slot); err != nil || variant.Effort != "low" || variant.ID != "claude-code" {
		t.Fatalf("binding = %+v (%v)", variant, err)
	}
	path := filepath.Join(store.Workspace, started.BindingsSnapshotPath)
	raw := readFile(t, path)
	if err := os.WriteFile(path, []byte(strings.Replace(raw, `effort = "low"`, `effort = "max"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	var executionErr *RunExecutionError
	if _, _, err := store.readRunPersonaBinding(started.RunID, "fmn_research", slot); !errors.As(err, &executionErr) || executionErr.Code != "persona_snapshot_invalid" {
		t.Fatalf("altered settings error = %v", err)
	}
}
