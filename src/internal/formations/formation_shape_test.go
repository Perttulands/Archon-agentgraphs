package formations

import (
	"errors"
	"reflect"
	"testing"
)

func invalidFormationShapes() []FormationNode {
	a := FormationSlot{ID: "slot_a"}
	b := FormationSlot{ID: "slot_b"}
	c := a
	c.Controller = true
	d := b
	d.Controller = true
	return []FormationNode{
		{ID: "empty_solo", Type: FormationTypeSolo, Slots: []FormationSlot{}},
		{ID: "large_solo", Type: FormationTypeSolo, Slots: []FormationSlot{a, b}},
		{ID: "empty_peer", Type: FormationTypePeer, Slots: []FormationSlot{}},
		{ID: "small_peer", Type: FormationTypePeer, Slots: []FormationSlot{a}},
		{ID: "controlled_peer", Type: FormationTypePeer, Slots: []FormationSlot{c, b}},
		{ID: "empty_orchestrated", Type: FormationTypeOrchestrated, Slots: []FormationSlot{}},
		{ID: "small_orchestrated", Type: FormationTypeOrchestrated, Slots: []FormationSlot{c}},
		{ID: "no_controller", Type: FormationTypeOrchestrated, Slots: []FormationSlot{a, b}},
		{ID: "two_controllers", Type: FormationTypeOrchestrated, Slots: []FormationSlot{c, d}},
	}
}

func TestRestorationRefusesInvalidFormationShapesWithoutWriting(t *testing.T) {
	for _, formation := range invalidFormationShapes() {
		t.Run(formation.ID, func(t *testing.T) {
			store, current := formationTypeFixture(t)
			before := readFile(t, store.BoardPath("types"))
			_, err := store.SetFormationType("types", FormationTypeRequest{FormationID: "fmn_work", Type: formation.Type, Slots: formation.Slots}, current())
			if !errors.Is(err, ErrInvalidTypeChange) {
				t.Errorf("set type error = %v, want invalid type change", err)
			}
			if after := readFile(t, store.BoardPath("types")); after != before {
				t.Error("refused type restoration changed mission")
			}
			store = nodeRestoreFixture(t)
			before = readFile(t, store.BoardPath("restore"))
			layout := readFile(t, store.LayoutPath("restore"))
			_, err = store.RestoreNode("restore", NodeRestoreRequest{Formation: &formation, X: 400, Y: 200}, restoreOptions(t, store))
			if !errors.Is(err, ErrInvalidNodeRestore) {
				t.Errorf("restore node error = %v, want invalid node restore", err)
			}
			if readFile(t, store.BoardPath("restore")) != before || readFile(t, store.LayoutPath("restore")) != layout {
				t.Error("refused node restoration changed mission or layout")
			}
		})
	}
}

func TestImportedInvalidFormationShapesBlockValidationAndAdmission(t *testing.T) {
	for _, formation := range invalidFormationShapes() {
		t.Run(formation.ID, func(t *testing.T) {
			// The malformed formation is off-path: every formation is bound in a run snapshot.
			board := &BoardDocument{Formations: []FormationNode{formation, {ID: "valid", Type: FormationTypeSolo, Slots: []FormationSlot{{ID: "slot_valid", Harness: "openai-codex", Effort: "medium", Controller: true}}}}}
			for name, report := range map[string]BoardValidationReport{
				"validation": ValidateBoard(board),
				"admission":  ValidateRunAdmission(board, nil, RunAdmissionScope{FormationID: "valid"}),
			} {
				found := false
				for _, finding := range report.Errors {
					if finding.NodeID == formation.ID && (finding.Code == "invalid_formation_shape" || finding.Code == FindingFormationWithoutSlots || finding.Code == FindingOrchestratedController) {
						found = true
					}
				}
				if !found {
					t.Errorf("%s missed imported shape: %+v", name, report.Errors)
				}
			}
		})
	}
}

func TestSuppliedSlotsPreserveLegalInversesAndRequireStaffedRemovalChoice(t *testing.T) {
	store, current := formationTypeFixture(t)
	solo := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo, KeepSlotID: "slot_reviewer"})
	peer := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer})
	// Undo may remove the newly added empty peer seat and restore a legal solo controller flag exactly.
	solo.Slots[0].Controller = true
	restored := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo, Slots: solo.Slots})
	if !reflect.DeepEqual(restored.Slots, solo.Slots) {
		t.Fatalf("solo inverse changed slots: %+v", restored.Slots)
	}
	// Bring back the peer snapshot, then staff the added seat as a user might before undo.
	setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer, Slots: peer.Slots})
	if _, err := store.AssignFormationSlot("types", FormationSlotAssignmentRequest{FormationID: "fmn_work", SlotID: peer.Slots[1].ID, Harness: "openai-codex", Effort: "medium"}, current()); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, store.BoardPath("types"))
	if _, err := store.SetFormationType("types", FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo, Slots: solo.Slots}, current()); !errors.Is(err, ErrSlotChoiceRequired) {
		t.Errorf("silent staffed removal error = %v", err)
	}
	if _, err := store.SetFormationType("types", FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo, Slots: solo.Slots, KeepSlotID: peer.Slots[1].ID}, current()); !errors.Is(err, ErrInvalidTypeChange) {
		t.Errorf("mismatched choice error = %v", err)
	}
	if readFile(t, store.BoardPath("types")) != before {
		t.Fatal("refused removal wrote mission")
	}
	chosen := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo, Slots: solo.Slots, KeepSlotID: solo.Slots[0].ID})
	if !reflect.DeepEqual(chosen.Slots, solo.Slots) {
		t.Fatal("explicit choice changed exact solo snapshot")
	}
	// RestoreNode likewise preserves a legal staffed solo controller with its identity and fields.
	store = nodeRestoreFixture(t)
	node := FormationNode{ID: "legal_solo", Type: FormationTypeSolo, Slots: solo.Slots}
	result, err := store.RestoreNode("restore", NodeRestoreRequest{Formation: &node}, restoreOptions(t, store))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := findFormation(result.Board.Formations, node.ID)
	if !reflect.DeepEqual(got.Slots, node.Slots) {
		t.Fatalf("restored solo slots = %+v, want %+v", got.Slots, node.Slots)
	}
}

func TestPeerSnapshotCanRemoveOnlyEmptySeats(t *testing.T) {
	store, current := formationTypeFixture(t)
	peer := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer})
	slots := peer.Slots[1:]
	before := readFile(t, store.BoardPath("types"))
	for _, keep := range []string{"", slots[0].ID} {
		_, err := store.SetFormationType("types", FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer, Slots: slots, KeepSlotID: keep}, current())
		want := ErrSlotChoiceRequired
		if keep != "" {
			want = ErrInvalidTypeChange
		}
		if !errors.Is(err, want) || readFile(t, store.BoardPath("types")) != before {
			t.Fatalf("peer staffed removal choice %q = %v, want unchanged mission and %v", keep, err, want)
		}
	}
	if _, err := store.AssignFormationSlot("types", FormationSlotAssignmentRequest{FormationID: "fmn_work", SlotID: peer.Slots[0].ID}, current()); err != nil {
		t.Fatal(err)
	}
	got := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer, Slots: slots})
	if !reflect.DeepEqual(got.Slots, slots) {
		t.Fatal("empty-seat removal changed retained snapshot")
	}
}
