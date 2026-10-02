package formations

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Usage names every slot, in every mission, that staffs a role.
func TestRoleUsageNamesEverySlotInEveryMission(t *testing.T) {
	store, _ := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	other := strings.NewReplacer(`id = "brd_01J9_sesssearch"`, `id = "brd_other"`, `slug = "session-search"`, `slug = "other"`, `title = "Improve session search"`, `title = "Other"`).Replace(s4RunBoardFixture())
	writeFixture(t, store.BoardPath("other"), other)

	uses, err := store.RoleUsage("scout")
	if err != nil {
		t.Fatal(err)
	}
	want := []RoleUse{
		{MissionID: "brd_other", MissionSlug: "other", MissionTitle: "Other", FormationID: "fmn_research", FormationTitle: "Research", SlotID: "slot_research", SlotLabel: "Researcher"},
		{MissionID: "brd_01J9_sesssearch", MissionSlug: "session-search", MissionTitle: "Improve session search", FormationID: "fmn_research", FormationTitle: "Research", SlotID: "slot_research", SlotLabel: "Researcher"},
	}
	if !reflect.DeepEqual(uses, want) {
		t.Fatalf("usage = %+v, want %+v", uses, want)
	}
	if got := RoleInUseError("scout", uses).Error(); got != `role_in_use: role "scout" staffs 2 slots: Other › Research › Researcher; Improve session search › Research › Researcher; restaff them, or retire the role instead` {
		t.Fatalf("in-use error = %s", got)
	}
	if uses, err := store.RoleUsage("judge"); err != nil || len(uses) != 0 {
		t.Fatalf("judge usage = %+v, %v", uses, err)
	}
}

// A card goes with its lock; a built-in role has no card to delete, and
// deleting a card that overrides one brings the built-in back.
func TestDeletePersonaRemovesTheCardAndKeepsBuiltinRoles(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	card, err := store.CreatePersona(CreatePersonaRequest{ID: "writer", Kind: "builder"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeletePersona("writer", ""); !errors.Is(err, ErrPreconditionRequired) {
		t.Fatalf("delete without an ETag = %v", err)
	}
	if _, err := store.DeletePersona("writer", "stale"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete with a stale ETag = %v", err)
	}
	if builtin, err := store.DeletePersona("writer", card.ETag); err != nil || builtin {
		t.Fatalf("delete = %v, %v", builtin, err)
	}
	for _, name := range []string{"writer.toml", "writer.toml.lock"} {
		if _, err := os.Stat(store.AgentsDir + "/" + name); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", name, err)
		}
	}
	if _, err := store.ReadPersona("writer"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read after delete = %v", err)
	}
	if _, err := store.DeletePersona("writer", card.ETag); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}

	if _, err := store.DeletePersona("judge", "x"); !errors.Is(err, ErrBuiltinRole) || !strings.Contains(err.Error(), `"judge" is a built-in role with no card to delete; retire it instead`) {
		t.Fatalf("delete a built-in role = %v", err)
	}
	summary := "Judges plans only."
	builtin, _ := store.ReadPersona("judge")
	override, err := store.EditPersona("judge", EditPersonaRequest{SetSummary: &summary, ExpectedETag: builtin.ETag})
	if err != nil {
		t.Fatal(err)
	}
	if remains, err := store.DeletePersona("judge", override.ETag); err != nil || !remains {
		t.Fatalf("delete the override = %v, %v", remains, err)
	}
	if card, err := store.ReadPersona("judge"); err != nil || card.Customized || card.Summary == summary {
		t.Fatalf("judge after deleting its override = %+v, %v", card, err)
	}
}

// Retiring a built-in role writes a card for it; bringing it back leaves the
// role as it ships, with no card.
func TestBringingBackABuiltinRoleLeavesNoCard(t *testing.T) {
	store := NewPersonaStore(t.TempDir())
	for _, retired := range []bool{true, false} {
		card, err := store.ReadPersona("builder")
		if err != nil {
			t.Fatal(err)
		}
		if card, err = store.EditPersona("builder", EditPersonaRequest{SetRetired: &retired, ExpectedETag: card.ETag}); err != nil || (card.Status == "retired") != retired || card.Customized != retired {
			t.Fatalf("retired %v: %+v, %v", retired, card, err)
		}
		if _, err := os.Stat(store.PersonaPath("builder")); os.IsNotExist(err) == retired {
			t.Fatalf("retired %v: card file stat = %v", retired, err)
		}
	}
	builtin, _ := builtinPresetPersona("builder")
	if card, err := store.ReadPersona("builder"); err != nil || card.ETag != builtin.ETag || card.Customized {
		t.Fatalf("builder after bringing it back = %+v, %v", card, err)
	}
}

// Retiring a role keeps it readable and stops it staffing a run until it is
// brought back; validation and admission name the slot and the role.
func TestARetiredRoleStopsItsSlotsUntilBroughtBack(t *testing.T) {
	store, personas := s4RunFixture(t)
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	retire := func(retired bool) {
		t.Helper()
		card, err := personas.ReadPersona("scout")
		if err != nil {
			t.Fatal(err)
		}
		if card, err = personas.EditPersona("scout", EditPersonaRequest{SetRetired: &retired, ExpectedETag: card.ETag}); err != nil || (card.Status == "retired") != retired {
			t.Fatalf("retire %v = %+v, %v", retired, card, err)
		}
	}
	retire(true)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	report := ValidateRunAdmission(board, personas, RunAdmissionScope{})
	want := BoardFinding{Code: FindingRetiredPersona, NodeID: "fmn_research", Message: `formation "fmn_research" slot "Researcher" (slot_research) uses retired role "scout"; staff it with another role or none, or bring the role back`}
	if len(report.Errors) != 1 || !reflect.DeepEqual(report.Errors[0], want) {
		t.Fatalf("validation = %+v", report.Errors)
	}
	if err := CheckRunAdmission(board, personas, RunAdmissionScope{MissionID: "mis_showcase"}); !errors.Is(err, ErrRunAdmission) || !strings.Contains(err.Error(), `uses retired role "scout"`) {
		t.Fatalf("admitting a run with a retired role = %v", err)
	}
	retire(false)
	if report := ValidateRunAdmission(board, personas, RunAdmissionScope{}); len(report.Errors) != 0 {
		t.Fatalf("validation after bringing the role back = %+v", report.Errors)
	}
}
