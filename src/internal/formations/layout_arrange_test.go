package formations

import (
	"os"
	"path/filepath"
	"testing"
)

func arrangeFixture(t *testing.T, path string, extra string) (*Store, string, map[string]LayoutNode) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	board, err := parseBoard(append(raw, extra...))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath(board.Slug), string(raw)+extra)
	arranged, err := store.ArrangeLayout(board.Slug, WriteOptions{ExpectedETag: "*"})
	if err != nil {
		t.Fatalf("arrange %s: %v", board.Slug, err)
	}
	byID := make(map[string]LayoutNode, len(arranged.Nodes))
	for _, node := range arranged.Nodes {
		byID[node.ID] = node
	}
	if len(byID) != len(board.Missions)+len(board.Formations)+len(board.Gates)+len(board.Tools) {
		t.Fatalf("arranged %d nodes, want every node of %s: %+v", len(byID), board.Slug, arranged.Nodes)
	}
	return store, board.Slug, byID
}

// assertRunOrder checks each step sits in a column right of the one before.
func assertRunOrder(t *testing.T, byID map[string]LayoutNode, runOrder []string) {
	t.Helper()
	for index := 1; index < len(runOrder); index++ {
		previous, current := byID[runOrder[index-1]], byID[runOrder[index]]
		if current.X <= previous.X {
			t.Errorf("%s (x=%d) is not right of %s (x=%d)", runOrder[index], current.X, runOrder[index-1], previous.X)
		}
	}
}

func assertJudgeBelow(t *testing.T, byID map[string]LayoutNode, gateID, judgeID string) {
	t.Helper()
	gate, judge := byID[gateID], byID[judgeID]
	// The gap under the gate card leaves room for its note preview.
	if judge.X != gate.X || judge.Y < gate.Y+124+96 {
		t.Errorf("judge %s at %+v, want directly below gate %s at %+v with room for a note", judgeID, judge, gateID, gate)
	}
}

func TestArrangeLayoutFollowsWayfindingRunOrder(t *testing.T) {
	const (
		mission     = "mis_01M2N9N1SG7J0D3YVRZY3V8TNT"
		mapStep     = "fmn_01M2N3G4PQCK21EXT7NC5CC3R9"
		framing     = "gate_01M2N3KMP5CYV3BHBRNJSAGN9Q"
		questions   = "fmn_01M2N3HQ5A5W4CAD3VFN86MC31"
		answers     = "gate_01M2N9N1MQDE88MWF3C1E6GJTG"
		draft       = "fmn_01M2N9N1N69CYV5KYCSQ2M1DPD"
		adversarial = "gate_01M2N9N1PEPTXX7XV5QW96681W"
		critic      = "fmn_01M2N9N1PXQ39NHQY46F0J0V63"
		signoff     = "gate_01M2N9N1R4R184Y5G3R45BB5G1"
	)
	fixture := filepath.Join("testdata", "arrange", "wayfinding.formation.toml")
	store, slug, byID := arrangeFixture(t, fixture, "")
	assertRunOrder(t, byID, []string{mission, mapStep, framing, questions, answers, draft, adversarial, signoff})
	assertJudgeBelow(t, byID, adversarial, critic)
	for id, node := range byID {
		if node.X%formationLayoutGrid != 0 || node.Y%formationLayoutGrid != 0 {
			t.Errorf("%s at %+v is off the grid", id, node)
		}
	}

	layout, err := store.ReadLayout(slug)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.ArrangeLayout(slug, WriteOptions{ExpectedETag: layout.ETag})
	if err != nil {
		t.Fatalf("repeat arrange: %v", err)
	}
	for _, node := range again.Nodes {
		if byID[node.ID] != node {
			t.Fatalf("repeat arrange moved %s from %+v to %+v", node.ID, byID[node.ID], node)
		}
	}

	t.Run("drafts no mission reaches follow the main path", func(t *testing.T) {
		_, _, withDraft := arrangeFixture(t, fixture, `
[[formation]]
id = "fmn_00_draft"
type = "solo"
title = "Unwired draft"
`)
		assertRunOrder(t, withDraft, []string{mission, mapStep, framing, questions, answers, draft, adversarial, signoff, "fmn_00_draft"})
	})

	t.Run("a step reached only by a fail edge follows its gate", func(t *testing.T) {
		_, _, withRework := arrangeFixture(t, fixture, `
[[formation]]
id = "fmn_00_rework"
type = "solo"
title = "Rework"

[[formation.input]]
id = "port_rework_in"
label = "Input"

[[connection]]
id = "edge_signoff_rework"
from = "`+signoff+`:fail"
to = "fmn_00_rework:port_rework_in"
`)
		assertRunOrder(t, withRework, []string{draft, adversarial, signoff, "fmn_00_rework"})
	})
}

func TestArrangeLayoutFollowsDeliveryRunOrder(t *testing.T) {
	_, _, byID := arrangeFixture(t, filepath.Join("..", "..", "..", "examples", "delivery.formation.toml"), "")
	assertRunOrder(t, byID, []string{"mis_delivery", "fmn_plan", "fmn_beads", "gate_beads_review", "fmn_execution", "fmn_final_review"})
	assertJudgeBelow(t, byID, "gate_beads_review", "fmn_beads_reviewer")
}
