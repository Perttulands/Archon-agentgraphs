package formations

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateBoardReportsASlotIDUsedTwice(t *testing.T) {
	// The ship formation reuses the work formation's slot ID, as a hand-edited
	// or imported board can.
	raw := strings.Replace(cleanValidateBoardFixture(), `id = "slot_ship"`, `id = "slot_work"`, 1)
	board := mustParseValidateBoardFixture(t, raw)
	findings := findBoardFindings(ValidateBoard(board).Errors, FindingDuplicateSlotID)
	if len(findings) != 2 {
		t.Fatalf("duplicate slot findings = %+v, want one for each formation holding the ID", findings)
	}
	for _, node := range []string{"fmn_work", "fmn_ship"} {
		if !hasBoardFinding(findings, node, `slot id "slot_work" is used by formations "fmn_work" and "fmn_ship"`) {
			t.Errorf("no finding on %s naming both formations: %+v", node, findings)
		}
	}

	personas := NewPersonaStore(filepath.Join(t.TempDir(), "agents"))
	for _, scope := range []RunAdmissionScope{{MissionID: "mis_main"}, {FormationID: "fmn_ship"}} {
		err := CheckRunAdmission(board, personas, scope)
		var admission *RunAdmissionError
		if !errors.As(err, &admission) || len(findBoardFindings(admission.Findings, FindingDuplicateSlotID)) == 0 {
			t.Errorf("admission for %+v = %v, want the duplicate slot ID refused", scope, err)
		}
	}

	// Two slots of one formation with one ID are reported as well.
	within := strings.Replace(cleanValidateBoardFixture(), `[[formation]]
id = "fmn_ship"`, `[[formation.slot]]
id = "slot_work"
label = "Second"

[[formation]]
id = "fmn_ship"`, 1)
	findings = findBoardFindings(ValidateBoard(mustParseValidateBoardFixture(t, within)).Errors, FindingDuplicateSlotID)
	if len(findings) != 1 || !hasBoardFinding(findings, "fmn_work", `used by 2 slots of formation "fmn_work"`) {
		t.Fatalf("repeated slot within a formation: %+v", findings)
	}

	if clean := findBoardFindings(ValidateBoard(mustParseValidateBoardFixture(t, cleanValidateBoardFixture())).Errors, FindingDuplicateSlotID); len(clean) != 0 {
		t.Fatalf("unique slot IDs reported: %+v", clean)
	}
}

func TestAuthoringNeverRepeatsASlotID(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	if _, err := store.CreateBoard(BoardCreateRequest{Slug: "slots", Title: "Slots"}); err != nil {
		t.Fatal(err)
	}
	current := func() WriteOptions {
		t.Helper()
		board, err := store.ReadBoard("slots")
		if err != nil {
			t.Fatal(err)
		}
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
	var ids []string
	for _, kind := range []string{FormationTypeSolo, FormationTypePeer, FormationTypeOrchestrated, FormationTypePeer} {
		created, err := store.CreateFormation("slots", FormationCreateRequest{Type: kind, Title: kind}, current())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.Formation.ID)
	}
	// Type changes add slots.
	for _, change := range []struct{ id, kind string }{{ids[0], FormationTypeOrchestrated}, {ids[1], FormationTypeSolo}, {ids[3], FormationTypeOrchestrated}} {
		if _, err := store.SetFormationType("slots", FormationTypeRequest{FormationID: change.id, Type: change.kind}, current()); err != nil {
			t.Fatalf("set %s to %s: %v", change.id, change.kind, err)
		}
	}
	board, err := store.ReadBoard("slots")
	if err != nil {
		t.Fatal(err)
	}
	slots := 0
	for _, formation := range board.Formations {
		slots += len(formation.Slots)
	}
	if duplicates := duplicateSlotFindings(board.Formations); slots < 8 || len(duplicates) != 0 {
		t.Fatalf("%d slots across %d formations, duplicates %+v", slots, len(board.Formations), duplicates)
	}

	// Restoring slots, as undo does, cannot take another formation's slot ID.
	other := board.Formations[2].Slots[0]
	restored := []FormationSlot{{ID: other.ID, Label: "Taken"}}
	before := current()
	if _, err := store.SetFormationType("slots", FormationTypeRequest{FormationID: ids[1], Type: FormationTypeSolo, Slots: restored}, before); !errors.Is(err, ErrInvalidTypeChange) || !strings.Contains(err.Error(), other.ID) {
		t.Fatalf("restoring another formation's slot ID: %v", err)
	}
	if after := current(); after.ExpectedRev != before.ExpectedRev {
		t.Fatal("a refused restore saved the board")
	}
	// A formation may restore its own slot IDs.
	own := []FormationSlot{board.Formations[1].Slots[0]}
	if _, err := store.SetFormationType("slots", FormationTypeRequest{FormationID: ids[1], Type: FormationTypeSolo, Slots: own}, current()); err != nil {
		t.Fatalf("restoring the formation's own slot: %v", err)
	}
}
