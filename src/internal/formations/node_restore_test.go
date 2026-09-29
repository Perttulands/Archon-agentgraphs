package formations

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

// A board where every node kind carries each field the cockpit can author, and
// the nodes are wired through ordinary, judge and pushback connections.
const nodeRestoreBoardFixture = `schema = 1
id = "brd_restore"
slug = "restore"
title = "Restore"
rev = 3

[[mission]]
id = "mis_ship"
title = "Ship it"
goal = "Ship the widget"
beadId = "form-abc.1"
files = ["docs/plan.md"]
inputHint = "Name the widget"
humanChannel = "session"

[[formation]]
id = "fmn_build"
type = "orchestrated"
title = "Build"
[formation.brief]
goal = "Build the widget"
beadId = "form-abc.2"
files = ["src/widget.go", "docs/widget.md"]
links = ["https://example.test/spec"]
[formation.execution]
timeoutSeconds = 900
[[formation.input]]
id = "port_build_in"
label = "Plan"
[[formation.input]]
id = "port_build_rework"
label = "Rework"
[[formation.output]]
id = "port_build_out"
label = "Result"
[[formation.output]]
id = "port_build_log"
label = "Log"
[[formation.slot]]
id = "slot_build_lead"
label = "Orchestrator"
controller = true
agentId = "codex-orchestrator"
harness = "openai-codex"
[[formation.slot]]
id = "slot_build_worker"
label = "Worker"
controller = false
agentId = "codex-builder"
harness = "openai-codex"

[[formation]]
id = "fmn_judge"
type = "solo"
title = "Judge"
[[formation.input]]
id = "port_judge_in"
label = "Input"
[[formation.output]]
id = "port_judge_out"
label = "Verdict"
[[formation.slot]]
id = "slot_judge"
label = "Agent"
controller = false

[[gate]]
id = "gate_tests"
title = "Tests pass"
kinds = ["code", "formation"]
criterion = "The suite is green"
check = "output_contains"
checkVersion = "1"
checkValue = "PASS"
files = ["docs/rubric.md"]

[[connection]]
id = "edge_start"
from = "mis_ship:out"
to = "fmn_build:port_build_in"

[[connection]]
id = "edge_review"
from = "fmn_build:port_build_out"
to = "gate_tests:in"

[[connection]]
id = "edge_pushback"
from = "gate_tests:fail"
to = "fmn_build:port_build_rework"

[[connection]]
id = "edge_judge_send"
from = "gate_tests:judge"
to = "fmn_judge:port_judge_in"

[[connection]]
id = "edge_judge_return"
from = "fmn_judge:port_judge_out"
to = "gate_tests:judge"
`

func nodeRestoreFixture(t *testing.T) *Store {
	t.Helper()
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("restore"), nodeRestoreBoardFixture)
	writeFixture(t, store.LayoutPath("restore"), `schema = 1
boardId = "brd_restore"
boardRev = 3

[[node]]
id = "mis_ship"
x = 80
y = 80

[[node]]
id = "fmn_build"
x = 420
y = 96

[[node]]
id = "fmn_judge"
x = 760
y = 420

[[node]]
id = "gate_tests"
x = 760
y = 96

[[edge]]
id = "edge_pushback"
lane = "y:40"
`)
	for _, target := range []string{"mis_ship", "fmn_build", "gate_tests", "fmn_judge"} {
		if _, err := store.UpdateBoardNote("restore", BoardNotePatch{Target: target, Text: "About " + target, UpdatedBy: "human:operator"}, NoteWriteOptions{ExpectedETag: currentNotesETag(t, store)}); err != nil {
			t.Fatalf("seed note on %s: %v", target, err)
		}
	}
	return store
}

func currentNotesETag(t *testing.T, store *Store) string {
	t.Helper()
	notes, err := store.ReadBoardNotes("restore")
	if err != nil {
		t.Fatal(err)
	}
	if notes.ETag == "" {
		return "*"
	}
	return notes.ETag
}

func restoreOptions(t *testing.T, store *Store) WriteOptions {
	t.Helper()
	board, err := store.ReadBoard("restore")
	if err != nil {
		t.Fatal(err)
	}
	return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
}

// touching returns the connections with an end on nodeID, as the cockpit
// captures them before a delete, sorted by ID for comparison.
func touching(board *BoardDocument, nodeID string) []BoardConnection {
	var out []BoardConnection
	for _, connection := range board.Connections {
		if endpointNodeID(connection.From) == nodeID || endpointNodeID(connection.To) == nodeID {
			out = append(out, connection)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func layoutPosition(t *testing.T, store *Store, id string) (LayoutNode, bool) {
	t.Helper()
	layout, err := store.ReadLayout("restore")
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range layout.Nodes {
		if node.ID == id {
			return node, true
		}
	}
	return LayoutNode{}, false
}

func deleteNode(t *testing.T, store *Store, id string) {
	t.Helper()
	var err error
	switch id[:3] {
	case "mis":
		_, err = store.DeleteMission("restore", MissionDeleteRequest{ID: id}, restoreOptions(t, store))
	case "fmn":
		_, err = store.DeleteFormation("restore", FormationDeleteRequest{ID: id}, restoreOptions(t, store))
	default:
		_, err = store.DeleteGate("restore", GateDeleteRequest{ID: id}, restoreOptions(t, store))
	}
	if err != nil {
		t.Fatalf("delete %s: %v", id, err)
	}
}

func TestRestoreNodePutsBackEachDeletedNodeExactly(t *testing.T) {
	for _, id := range []string{"mis_ship", "fmn_build", "gate_tests", "fmn_judge"} {
		t.Run(id, func(t *testing.T) {
			store := nodeRestoreFixture(t)
			before, err := store.ReadBoard("restore")
			if err != nil {
				t.Fatal(err)
			}
			position, _ := layoutPosition(t, store, id)
			req := NodeRestoreRequest{Connections: touching(before, id), X: position.X, Y: position.Y, UpdatedBy: "agent:ui"}
			if mission, ok := findMissionForTest(before, id); ok {
				req.Mission = &mission
			} else if formation, ok := findFormation(before.Formations, id); ok {
				req.Formation = &formation
			} else if gate, ok := findGate(before.Gates, id); ok {
				req.Gate = &gate
			}

			deleteNode(t, store, id)
			result, err := store.RestoreNode("restore", req, restoreOptions(t, store))
			if err != nil {
				t.Fatalf("restore %s: %v", id, err)
			}
			if result.NodeID != id || result.Board.Rev != before.Rev+2 {
				t.Fatalf("restore result = %q rev %d, want %q rev %d", result.NodeID, result.Board.Rev, id, before.Rev+2)
			}

			after, err := store.ReadBoard("restore")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after.Missions, before.Missions) && req.Mission != nil {
				t.Fatalf("missions = %+v\nwant %+v", after.Missions, before.Missions)
			}
			if req.Formation != nil {
				got, _ := findFormation(after.Formations, id)
				if !reflect.DeepEqual(got, *req.Formation) {
					t.Fatalf("formation = %+v\nwant %+v", got, *req.Formation)
				}
			}
			if req.Gate != nil {
				got, _ := findGate(after.Gates, id)
				if !reflect.DeepEqual(got, *req.Gate) {
					t.Fatalf("gate = %+v\nwant %+v", got, *req.Gate)
				}
			}
			if got := touching(after, id); !reflect.DeepEqual(got, req.Connections) {
				t.Fatalf("connections = %+v\nwant %+v", got, req.Connections)
			}
			if len(after.Connections) != len(before.Connections) {
				t.Fatalf("board has %d connections, want %d", len(after.Connections), len(before.Connections))
			}
			if got, ok := layoutPosition(t, store, id); !ok || got.X != position.X || got.Y != position.Y {
				t.Fatalf("layout = %+v %v, want %+v", got, ok, position)
			}
			layout, _ := store.ReadLayout("restore")
			if len(layout.Edges) != 1 || layout.Edges[0].ID != "edge_pushback" || layout.Edges[0].Lane != "y:40" {
				t.Fatalf("lane routing = %+v", layout.Edges)
			}
			notes, err := store.ReadBoardNotes("restore")
			if err != nil {
				t.Fatal(err)
			}
			if thread := notes.thread(id); len(thread) != 1 || thread[0].Text != "About "+id {
				t.Fatalf("notes for %s = %+v", id, thread)
			}
			if validation := ValidateBoard(after); len(validation.Errors) != len(ValidateBoard(before).Errors) {
				t.Fatalf("validation changed: %+v", validation.Errors)
			}
		})
	}
}

func findMissionForTest(board *BoardDocument, id string) (MissionNode, bool) {
	for _, mission := range board.Missions {
		if mission.ID == id {
			return mission, true
		}
	}
	return MissionNode{}, false
}

func TestRestoreNodeRefusesWithoutWritingWhenTheBoardNoLongerFits(t *testing.T) {
	store := nodeRestoreFixture(t)
	before, _ := store.ReadBoard("restore")
	build, _ := findFormation(before.Formations, "fmn_build")
	connections := touching(before, "fmn_build")
	deleteNode(t, store, "fmn_build")

	cases := map[string]struct {
		req  NodeRestoreRequest
		want error
	}{
		"no node":         {NodeRestoreRequest{}, ErrInvalidNodeRestore},
		"two nodes":       {NodeRestoreRequest{Formation: &build, Mission: &before.Missions[0]}, ErrInvalidNodeRestore},
		"node still here": {NodeRestoreRequest{Mission: &before.Missions[0]}, ErrInvalidNodeRestore},
		"missing end": {NodeRestoreRequest{Formation: &build, Connections: []BoardConnection{
			{ID: "edge_gone", From: "fmn_build:port_build_out", To: "gate_missing:in"},
		}}, ErrInvalidNodeRestore},
		"foreign connection": {NodeRestoreRequest{Formation: &build, Connections: []BoardConnection{
			{ID: "edge_other", From: "gate_tests:pass", To: "fmn_judge:port_judge_in"},
		}}, ErrInvalidNodeRestore},
		"occupied input": {NodeRestoreRequest{Formation: &build, Connections: []BoardConnection{
			{ID: "edge_twice", From: "fmn_build:port_build_out", To: "fmn_judge:port_judge_in"},
		}}, ErrInputOccupied},
		"retired type":  {NodeRestoreRequest{Formation: &FormationNode{ID: "fmn_old", Type: "flow"}}, ErrUnsupportedFormationType},
		"unsafe bead":   {NodeRestoreRequest{Mission: &MissionNode{ID: "mis_new", BeadID: "../x"}}, ErrInvalidBeadID},
		"legacy script": {NodeRestoreRequest{Gate: &GateNode{ID: "gate_new", Kinds: []string{"code"}, Command: "make test"}}, ErrLegacyScriptGateRequiresFencedMigration},
		"inline verification": {NodeRestoreRequest{Formation: &FormationNode{ID: "fmn_v", Type: "solo",
			Verification: &FormationVerification{ID: "ver", Kinds: []string{"human"}}}}, ErrLegacyInlineVerificationRequiresMigration},
		"taken slot": {NodeRestoreRequest{Formation: &FormationNode{ID: "fmn_copy", Type: "solo",
			Slots: []FormationSlot{{ID: "slot_judge", Label: "Agent"}}}}, ErrInvalidNodeRestore},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			current, _ := store.ReadBoard("restore")
			_, err := store.RestoreNode("restore", tc.req, restoreOptions(t, store))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			after, _ := store.ReadBoard("restore")
			if after.ETag != current.ETag {
				t.Fatal("a refused restore changed the board")
			}
			if _, ok := layoutPosition(t, store, "fmn_build"); ok {
				t.Fatal("a refused restore wrote a layout node")
			}
		})
	}

	// The intact restore still succeeds afterwards, and a stale revision conflicts.
	stale := restoreOptions(t, store)
	stale.ExpectedRev--
	if _, err := store.RestoreNode("restore", NodeRestoreRequest{Formation: &build, Connections: connections}, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale restore err = %v, want conflict", err)
	}
	if _, err := store.RestoreNode("restore", NodeRestoreRequest{Formation: &build, Connections: connections, X: 420, Y: 96}, restoreOptions(t, store)); err != nil {
		t.Fatalf("restore: %v", err)
	}
}

func TestRestoreNodeGivesATakenConnectionIDAFreshOne(t *testing.T) {
	store := nodeRestoreFixture(t)
	before, _ := store.ReadBoard("restore")
	judge, _ := findFormation(before.Formations, "fmn_judge")
	deleteNode(t, store, "fmn_judge")
	connections := []BoardConnection{
		{ID: "edge_start", From: "gate_tests:judge", To: "fmn_judge:port_judge_in"},
		{ID: "", From: "fmn_judge:port_judge_out", To: "gate_tests:judge"},
	}
	result, err := store.RestoreNode("restore", NodeRestoreRequest{Formation: &judge, Connections: connections}, restoreOptions(t, store))
	if err != nil {
		t.Fatal(err)
	}
	restored := touching(result.Board, "fmn_judge")
	if len(restored) != 2 {
		t.Fatalf("connections = %+v", restored)
	}
	for _, connection := range restored {
		if connection.ID == "edge_start" || connection.ID == "" {
			t.Fatalf("restored connection kept a taken or empty id: %+v", connection)
		}
	}
}
