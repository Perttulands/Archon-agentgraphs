package formations

import (
	"strings"
	"testing"
)

const migrationBoard = `schema = 1
id = "brd_migrate"
slug = "migrate"
title = "Migrate"
rev = 4

[[formation]]
id = "fmn_team"
type = "peer"
title = "Team"

[[formation.slot]]
id = "slot_preset"
label = "Preset"
agentId = "delivery-final-reviewer"
harness = "openai-codex"
controller = false

[[formation.slot]]
id = "slot_card"
label = "Card"
agentId = "pinned"
harness = "claude-code"
controller = false

[[formation.slot]]
id = "slot_no_harness"
label = "No harness"
agentId = "delivery-lead"
controller = false

[[formation.slot]]
id = "slot_ambiguous"
label = "Ambiguous"
agentId = "pinned"
controller = false

[[formation.slot]]
id = "slot_second_variant"
label = "Second variant"
agentId = "pinned"
harness = "openai-codex"
controller = false

[[formation.slot]]
id = "slot_owned"
label = "Owned"
harness = "claude-code"
model = "sonnet"
effort = "low"
controller = false

[[formation.slot]]
id = "slot_missing"
label = "Missing"
agentId = "gone"
harness = "claude-code"
controller = false

[[formation.slot]]
id = "slot_empty"
label = "Empty"
controller = false
`

// Migration writes each legacy slot's role settings onto the slot, so every
// seat launches exactly as before, and leaves everything else alone.
func TestSlotMigrationKeepsEveryLaunchIdentical(t *testing.T) {
	stubHarnessCLIs(t)
	store, personas := s4RunFixture(t)
	if _, err := personas.CreatePersona(CreatePersonaRequest{ID: "pinned", Kind: "specialist", Harness: "claude-code", Model: "claude-opus-5", Effort: "xhigh"}); err != nil {
		t.Fatal(err)
	}
	card, err := personas.ReadPersona("pinned")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := personas.EditPersona("pinned", EditPersonaRequest{ExpectedETag: card.ETag, AddHarness: "openai-codex", Model: "gpt-6-sol", Effort: "high", SessionStem: "codex-pinned"}); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, store.BoardPath("migrate"), migrationBoard)

	dry, err := store.MigrateSlotSettings(personas, true)
	if err != nil {
		t.Fatal(err)
	}
	if raw := readFile(t, store.BoardPath("migrate")); raw != migrationBoard {
		t.Fatalf("dry run wrote the board:\n%s", raw)
	}
	outcomes := func(report SlotMigrationReport) map[string]SlotMigrationEntry {
		byID := map[string]SlotMigrationEntry{}
		for _, entry := range report.Slots {
			byID[entry.SlotID] = entry
		}
		return byID
	}
	got := outcomes(dry)
	if len(got) != 7 {
		t.Fatalf("dry run slots = %+v, want every staffed slot", dry.Slots)
	}
	for id, outcome := range map[string]string{"slot_preset": SlotMigrationWouldMove, "slot_card": SlotMigrationWouldMove, "slot_second_variant": SlotMigrationWouldMove, "slot_owned": SlotMigrationOwned, "slot_missing": SlotMigrationSkipped, "slot_no_harness": SlotMigrationWouldMove, "slot_ambiguous": SlotMigrationSkipped} {
		if got[id].Outcome != outcome {
			t.Errorf("dry %s = %+v, want %s", id, got[id], outcome)
		}
	}

	report, err := store.MigrateSlotSettings(personas, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed() != nil || len(report.Boards) != 1 || report.Boards[0].RevFrom != 4 || report.Boards[0].RevTo != 5 {
		t.Fatalf("boards = %+v (%v), want one write from rev 4 to 5", report.Boards, report.Failed())
	}
	got = outcomes(report)
	for id, want := range map[string][3]string{
		"slot_preset":         {"openai-codex", "gpt-6-astra", "medium"},
		"slot_card":           {"claude-code", "claude-opus-5", "xhigh"},
		"slot_second_variant": {"openai-codex", "gpt-6-sol", "high"},
		"slot_owned":          {"claude-code", "sonnet", "low"},
		"slot_no_harness":     {"claude-code", "", "medium"},
	} {
		entry := got[id]
		if [3]string{entry.Harness, entry.Model, entry.Effort} != want || !entry.Identical || entry.LaunchBefore == "" || entry.LaunchBefore != entry.LaunchAfter {
			t.Errorf("%s = %+v, want %v with an identical launch", id, entry, want)
		}
	}
	if !strings.Contains(got["slot_card"].LaunchAfter, "--model 'claude-opus-5' --effort 'xhigh'") {
		t.Errorf("slot_card launch = %q", got["slot_card"].LaunchAfter)
	}
	if got["slot_missing"].Outcome != SlotMigrationSkipped || !strings.Contains(got["slot_missing"].Reason, "gone") {
		t.Errorf("missing role = %+v, want skipped with its reason", got["slot_missing"])
	}

	board, err := store.ReadBoard("migrate")
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]FormationSlot{}
	for _, slot := range board.Formations[0].Slots {
		slots[slot.ID] = slot
	}
	if slot := slots["slot_card"]; slot.AgentID != "pinned" || slot.Harness != "claude-code" || slot.Model != "claude-opus-5" || slot.Effort != "xhigh" {
		t.Fatalf("migrated slot = %+v", slot)
	}
	if slot := slots["slot_no_harness"]; slot.Harness != "claude-code" || slot.Model != "" || slot.Effort != "medium" {
		t.Fatalf("migrated slot without a harness = %+v, want the role's default harness written", slot)
	}
	if slot := slots["slot_missing"]; slot.Model != "" || slot.Effort != "" {
		t.Fatalf("unmigratable slot changed: %+v", slot)
	}
	if board.UpdatedBy != SlotMigrationActor {
		t.Fatalf("updatedBy = %q", board.UpdatedBy)
	}

	// Migrated slots no longer follow the role card.
	card, err = personas.ReadPersona("pinned")
	if err != nil {
		t.Fatal(err)
	}
	low := "low"
	if _, err := personas.EditPersona("pinned", EditPersonaRequest{ExpectedETag: card.ETag, SetEffort: &low}); err != nil {
		t.Fatal(err)
	}
	settings, _, err := ResolveSlotSettings(slots["slot_card"], personas)
	if err != nil || settings.Effort != "xhigh" || settings.FromRole {
		t.Fatalf("migrated slot settings = %+v (%v), want the slot's own xhigh", settings, err)
	}

	// A second pass writes nothing.
	again, err := store.MigrateSlotSettings(personas, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Boards[0].RevTo != 0 {
		t.Fatalf("second pass wrote rev %d", again.Boards[0].RevTo)
	}
}
