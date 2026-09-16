package formations

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const formationTypeBoardFixture = `schema = 1
id = "brd_types"
slug = "types"
title = "Types"
rev = 3

[[mission]]
id = "mis_start"
title = "Start"
goal = "Go"
beadId = ""

[[formation]]
id = "fmn_work"
type = "orchestrated"
title = "Desk"
[formation.brief]
goal = "Do the work"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_lead"
label = "Lead"
controller = false
agentId = "codex-builder"
harness = "openai-codex"
[[formation.slot]]
id = "slot_reviewer"
label = "Reviewer"
controller = true
agentId = "codex-reviewer"
harness = "openai-codex"
reviewNote = "keep this unknown key"
[[formation.slot]]
id = "slot_spare"
label = "Spare"
controller = false

[[formation]]
id = "fmn_sketch"
type = "flow"
title = "Legacy flow"
[[formation.input]]
id = "port_sketch_in"
label = "Input"
[[formation.slot]]
id = "slot_plan"
label = "Plan"
[[formation.slot]]
id = "slot_push"
label = "Push"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = ""

[[connection]]
id = "edge_start"
from = "mis_start:out"
to = "fmn_work:port_in"

[[connection]]
id = "edge_review"
from = "fmn_work:port_out"
to = "gate_review:in"
`

func formationTypeFixture(t *testing.T) (*Store, func() WriteOptions) {
	t.Helper()
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("types"), formationTypeBoardFixture)
	writeFixture(t, store.LayoutPath("types"), "schema = 1\nboardId = \"brd_types\"\nboardRev = 3\n\n[[node]]\nid = \"fmn_work\"\nx = 300\ny = 100\n")
	if _, err := store.UpdateBoardNote("types", BoardNotePatch{Target: "fmn_work", Text: "Desk note", UpdatedBy: "human:operator"}, NoteWriteOptions{ExpectedETag: "*"}); err != nil {
		t.Fatal(err)
	}
	return store, func() WriteOptions {
		t.Helper()
		board := mustReadBoard(t, store, "types")
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
}

func setType(t *testing.T, store *Store, opts WriteOptions, req FormationTypeRequest) FormationNode {
	t.Helper()
	if _, err := store.SetFormationType("types", req, opts); err != nil {
		t.Fatalf("set type %+v: %v", req, err)
	}
	formation, ok := findFormation(mustReadBoard(t, store, "types").Formations, req.FormationID)
	if !ok || formation.Type != req.Type {
		t.Fatalf("formation after %+v = %+v", req, formation)
	}
	return formation
}

type slotShape struct {
	ID, Label, AgentID string
	Controller         bool
}

func slotShapes(slots []FormationSlot) []slotShape {
	shapes := make([]slotShape, 0, len(slots))
	for _, slot := range slots {
		id := slot.ID
		if !strings.HasPrefix(id, "slot_") || strings.HasPrefix(id, "slot_01") {
			id = "new"
		}
		shapes = append(shapes, slotShape{id, slot.Label, slot.AgentID, slot.Controller})
	}
	return shapes
}

func TestSetFormationTypeAppliesSlotRulesAndKeepsEverythingElse(t *testing.T) {
	store, current := formationTypeFixture(t)
	before := mustReadBoard(t, store, "types")
	layout := readFile(t, store.LayoutPath("types"))
	notes := readFile(t, store.NotesPath("types"))

	peer := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer})
	want := []slotShape{{"slot_lead", "Lead", "codex-builder", false}, {"slot_reviewer", "Reviewer", "codex-reviewer", false}, {"slot_spare", "Spare", "", false}}
	if got := slotShapes(peer.Slots); !reflect.DeepEqual(got, want) {
		t.Fatalf("orchestrated->peer slots = %+v, want %+v", got, want)
	}

	orchestrated := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeOrchestrated})
	want = []slotShape{{"slot_lead", "Lead", "codex-builder", true}, {"slot_reviewer", "Reviewer", "codex-reviewer", false}, {"slot_spare", "Spare", "", false}}
	if got := slotShapes(orchestrated.Slots); !reflect.DeepEqual(got, want) {
		t.Fatalf("peer->orchestrated slots = %+v, want first slot controller %+v", got, want)
	}

	_, err := store.SetFormationType("types", FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo}, current())
	if !errors.Is(err, ErrSlotChoiceRequired) || !strings.Contains(err.Error(), "codex-builder") || !strings.Contains(err.Error(), "codex-reviewer") {
		t.Fatalf("solo without a slot choice = %v, want both staffed slots named", err)
	}
	solo := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo, KeepSlotID: "slot_reviewer"})
	if got := slotShapes(solo.Slots); !reflect.DeepEqual(got, []slotShape{{"slot_reviewer", "Reviewer", "codex-reviewer", false}}) {
		t.Fatalf("orchestrated->solo keeping reviewer = %+v", got)
	}
	if !strings.Contains(readFile(t, store.BoardPath("types")), `reviewNote = "keep this unknown key"`) {
		t.Fatal("kept slot lost an unknown key")
	}

	peer = setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer})
	if got := slotShapes(peer.Slots); !reflect.DeepEqual(got, []slotShape{{"slot_reviewer", "Reviewer", "codex-reviewer", false}, {"new", "Peer", "", false}}) {
		t.Fatalf("solo->peer = %+v, want an added empty peer", got)
	}
	solo = setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo})
	if got := slotShapes(solo.Slots); !reflect.DeepEqual(got, []slotShape{{"slot_reviewer", "Reviewer", "codex-reviewer", false}}) {
		t.Fatalf("peer->solo with one staffed slot = %+v, want it kept without a choice", got)
	}
	orchestrated = setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeOrchestrated})
	if got := slotShapes(orchestrated.Slots); !reflect.DeepEqual(got, []slotShape{{"slot_reviewer", "Reviewer", "codex-reviewer", true}, {"new", "Agent", "", false}}) {
		t.Fatalf("solo->orchestrated = %+v, want controller plus an empty worker", got)
	}

	after := mustReadBoard(t, store, "types")
	work, _ := findFormation(after.Formations, "fmn_work")
	original, _ := findFormation(before.Formations, "fmn_work")
	if !reflect.DeepEqual(work.Inputs, original.Inputs) || !reflect.DeepEqual(work.Outputs, original.Outputs) || !reflect.DeepEqual(work.Brief, original.Brief) || work.Title != original.Title {
		t.Fatalf("type changes altered ports, brief or title: %+v", work)
	}
	if !reflect.DeepEqual(after.Connections, before.Connections) || !reflect.DeepEqual(after.Gates, before.Gates) || !reflect.DeepEqual(after.Missions, before.Missions) {
		t.Fatal("type changes altered edges, gates or missions")
	}
	if readFile(t, store.LayoutPath("types")) != layout || readFile(t, store.NotesPath("types")) != notes {
		t.Fatal("type changes altered layout or notes")
	}
}

func TestSetFormationTypeRestoresExactSlotsForUndo(t *testing.T) {
	store, current := formationTypeFixture(t)
	original, _ := findFormation(mustReadBoard(t, store, "types").Formations, "fmn_work")
	setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo, KeepSlotID: "slot_reviewer"})

	restored := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeOrchestrated, Slots: original.Slots})
	if !reflect.DeepEqual(restored.Slots, original.Slots) {
		t.Fatalf("restored slots = %+v, want %+v in the original order", restored.Slots, original.Slots)
	}
	if !strings.Contains(readFile(t, store.BoardPath("types")), `reviewNote = "keep this unknown key"`) {
		t.Fatal("restore lost the kept slot's unknown key")
	}
}

func TestSetFormationTypeConvertsLegacyFlowAndRejectsBadRequests(t *testing.T) {
	store, current := formationTypeFixture(t)
	peer := setType(t, store, current(), FormationTypeRequest{FormationID: "fmn_sketch", Type: FormationTypePeer})
	if got := slotShapes(peer.Slots); !reflect.DeepEqual(got, []slotShape{{"slot_plan", "Plan", "", false}, {"slot_push", "Push", "", false}}) {
		t.Fatalf("flow->peer = %+v", got)
	}

	before := readFile(t, store.BoardPath("types"))
	for name, test := range map[string]struct {
		req  FormationTypeRequest
		want error
	}{
		"flow target":          {FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeFlow}, ErrInvalidTypeChange},
		"unknown target":       {FormationTypeRequest{FormationID: "fmn_work", Type: "swarm"}, ErrInvalidTypeChange},
		"keep for peer":        {FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer, KeepSlotID: "slot_lead"}, ErrInvalidTypeChange},
		"keep unknown slot":    {FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo, KeepSlotID: "slot_missing"}, ErrInvalidTypeChange},
		"duplicate restore":    {FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypePeer, Slots: []FormationSlot{{ID: "slot_a"}, {ID: "slot_a"}}}, ErrInvalidTypeChange},
		"missing formation":    {FormationTypeRequest{FormationID: "fmn_missing", Type: FormationTypeSolo}, ErrNotFound},
		"staffed without keep": {FormationTypeRequest{FormationID: "fmn_work", Type: FormationTypeSolo}, ErrSlotChoiceRequired},
	} {
		if _, err := store.SetFormationType("types", test.req, current()); !errors.Is(err, test.want) {
			t.Errorf("%s error = %v, want %v", name, err, test.want)
		}
		if after := readFile(t, store.BoardPath("types")); after != before {
			t.Fatalf("%s changed the board:\n%s", name, after)
		}
	}
}
