package formations

import (
	"errors"
	"reflect"
	"testing"
)

const nodeUpdateBoardFixture = `schema = 1
id = "brd_rename"
slug = "rename"
title = "Rename"
rev = 4

[[mission]]
id = "mis_frame"
title = "New mission"
goal = ""
beadId = ""

[[formation]]
id = "fmn_map"
type = "solo"
title = "New formation"
[formation.brief]
goal = "Map the territory"
[[formation.input]]
id = "port_map_in"
label = "Input"
[[formation.output]]
id = "port_map_out"
label = "Output"
[[formation.slot]]
id = "slot_map"
label = "Scout"
agentId = "codex-scout"
harness = "openai-codex"

[[formation]]
id = "fmn_peers"
type = "peer"
[[formation.input]]
id = "port_peers_in"
label = "Input"
[[formation.slot]]
id = "slot_peer"
label = "Peer"

[[gate]]
id = "gate_review"
title = "Review gate"
kinds = ["human"]
criterion = "Framing is right"

[[connection]]
id = "edge_start"
from = "mis_frame:out"
to = "fmn_map:port_map_in"

[[connection]]
id = "edge_review"
from = "fmn_map:port_map_out"
to = "gate_review:in"
`

func nodeUpdateFixture(t *testing.T) (*Store, func() WriteOptions) {
	t.Helper()
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("rename"), nodeUpdateBoardFixture)
	writeFixture(t, store.LayoutPath("rename"), "schema = 1\nboardId = \"brd_rename\"\nboardRev = 4\n\n[[node]]\nid = \"fmn_map\"\nx = 320\ny = 80\n")
	board, err := store.ReadBoard("rename")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := store.UpdateBoardNote("rename", BoardNotePatch{Target: "fmn_map", Text: "Keep the map narrow", UpdatedBy: "human:operator"}, NoteWriteOptions{ExpectedETag: "*"})
	if err != nil || notes.BoardID != board.ID {
		t.Fatalf("seed notes: %+v %v", notes, err)
	}
	return store, func() WriteOptions {
		t.Helper()
		current, err := store.ReadBoard("rename")
		if err != nil {
			t.Fatal(err)
		}
		return WriteOptions{ExpectedETag: current.ETag, ExpectedRev: current.Rev}
	}
}

// withoutNodeFields blanks the fields an update may change so the rest of the
// board can be compared before and after.
func withoutNodeFields(board *BoardDocument) BoardDocument {
	copy := *board
	copy.Rev, copy.ETag, copy.TOML, copy.UpdatedAt, copy.UpdatedBy = 0, "", "", "", ""
	copy.Formations = append([]FormationNode(nil), board.Formations...)
	for i := range copy.Formations {
		copy.Formations[i].Title = ""
	}
	copy.Missions = append([]MissionNode(nil), board.Missions...)
	for i := range copy.Missions {
		copy.Missions[i].Title, copy.Missions[i].Goal, copy.Missions[i].BeadID = "", "", ""
	}
	return copy
}

func TestUpdateFormationRenamesAndClearsTitleOnly(t *testing.T) {
	store, current := nodeUpdateFixture(t)
	before, _ := store.ReadBoard("rename")
	layout := readFile(t, store.LayoutPath("rename"))
	notes := readFile(t, store.NotesPath("rename"))

	for _, test := range []struct{ id, title string }{
		{"fmn_map", "Map the territory"},
		{"fmn_peers", "Question peers"}, // the header has no title key yet
		{"fmn_map", ""},
		{"fmn_map", "Map the territory"},
	} {
		title := test.title
		board, err := store.UpdateFormation("rename", FormationUpdateRequest{FormationID: test.id, Title: &title, UpdatedBy: "agent:test"}, current())
		if err != nil {
			t.Fatalf("rename %s to %q: %v", test.id, test.title, err)
		}
		reloaded, err := store.ReadBoard("rename")
		if err != nil || reloaded.ETag != board.ETag {
			t.Fatalf("reload after rename: %v", err)
		}
		formation, _ := findFormation(reloaded.Formations, test.id)
		if formation.Title != test.title {
			t.Fatalf("%s title = %q, want %q", test.id, formation.Title, test.title)
		}
		if !reflect.DeepEqual(withoutNodeFields(reloaded), withoutNodeFields(before)) {
			t.Fatalf("rename changed more than the title:\nbefore %+v\nafter  %+v", withoutNodeFields(before), withoutNodeFields(reloaded))
		}
	}
	peers, _ := findFormation(mustReadBoard(t, store, "rename").Formations, "fmn_peers")
	if peers.Title != "Question peers" || len(peers.Slots) != 1 || peers.Inputs[0].ID != "port_peers_in" {
		t.Fatalf("inserted title landed outside the formation header: %+v", peers)
	}
	if got := readFile(t, store.LayoutPath("rename")); got != layout {
		t.Fatalf("rename changed layout:\n%s", got)
	}
	if got := readFile(t, store.NotesPath("rename")); got != notes {
		t.Fatalf("rename changed notes:\n%s", got)
	}
}

func TestUpdateMissionSetsAndClearsEachField(t *testing.T) {
	store, current := nodeUpdateFixture(t)
	before, _ := store.ReadBoard("rename")
	mission := func(req MissionUpdateRequest) MissionNode {
		t.Helper()
		req.MissionID = "mis_frame"
		if _, err := store.UpdateMission("rename", req, current()); err != nil {
			t.Fatalf("update mission %+v: %v", req, err)
		}
		reloaded := mustReadBoard(t, store, "rename")
		if !reflect.DeepEqual(withoutNodeFields(reloaded), withoutNodeFields(before)) {
			t.Fatalf("mission update changed more than its fields")
		}
		got, _ := findMission(reloaded, "mis_frame")
		return got
	}

	got := mission(MissionUpdateRequest{Title: stringPtr("Frame the goal"), Goal: stringPtr("Draft a framing"), BeadID: stringPtr("form-3yd.10")})
	if got.Title != "Frame the goal" || got.Goal != "Draft a framing" || got.BeadID != "form-3yd.10" {
		t.Fatalf("set all = %+v", got)
	}
	got = mission(MissionUpdateRequest{Goal: stringPtr("Draft a sharper framing")})
	if got.Title != "Frame the goal" || got.Goal != "Draft a sharper framing" || got.BeadID != "form-3yd.10" {
		t.Fatalf("goal only = %+v, want other fields untouched", got)
	}
	got = mission(MissionUpdateRequest{Title: stringPtr(""), Goal: stringPtr(""), BeadID: stringPtr("")})
	if got.Title != "" || got.Goal != "" || got.BeadID != "" {
		t.Fatalf("clear all = %+v", got)
	}
}

func TestNodeUpdatesRejectMalformedOrMissingTargetsWithoutMutation(t *testing.T) {
	store, current := nodeUpdateFixture(t)
	before := readFile(t, store.BoardPath("rename"))
	title := "Anything"
	for name, test := range map[string]struct {
		write func() error
		want  error
	}{
		"unsafe Bead ID": {func() error {
			_, err := store.UpdateMission("rename", MissionUpdateRequest{MissionID: "mis_frame", BeadID: stringPtr("../escape")}, current())
			return err
		}, ErrInvalidSlug},
		"missing mission": {func() error {
			_, err := store.UpdateMission("rename", MissionUpdateRequest{MissionID: "mis_missing", Title: &title}, current())
			return err
		}, ErrNotFound},
		"missing formation": {func() error {
			_, err := store.UpdateFormation("rename", FormationUpdateRequest{FormationID: "fmn_missing", Title: &title}, current())
			return err
		}, ErrNotFound},
		"stale revision": {func() error {
			opts := current()
			opts.ExpectedRev--
			_, err := store.UpdateFormation("rename", FormationUpdateRequest{FormationID: "fmn_map", Title: &title}, opts)
			return err
		}, ErrConflict},
	} {
		if err := test.write(); !errors.Is(err, test.want) {
			t.Errorf("%s error = %v, want %v", name, err, test.want)
		}
		if after := readFile(t, store.BoardPath("rename")); after != before {
			t.Fatalf("%s changed the board:\n%s", name, after)
		}
	}
}

func mustReadBoard(t *testing.T, store *Store, slug string) *BoardDocument {
	t.Helper()
	board, err := store.ReadBoard(slug)
	if err != nil {
		t.Fatal(err)
	}
	return board
}
