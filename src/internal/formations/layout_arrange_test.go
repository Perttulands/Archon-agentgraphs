package formations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if len(byID) != len(board.Missions)+len(board.Formations)+len(board.Gates)+len(board.Tools)+len(board.Ends) {
		t.Fatalf("arranged %d nodes, want every node of %s: %+v", len(byID), board.Slug, arranged.Nodes)
	}
	return store, board.Slug, byID
}

// assertRunOrder checks reading order: right along a row, then down to the next.
func assertRunOrder(t *testing.T, byID map[string]LayoutNode, runOrder []string) {
	t.Helper()
	for index := 1; index < len(runOrder); index++ {
		previous, current := byID[runOrder[index-1]], byID[runOrder[index]]
		if current.Y < previous.Y || current.Y == previous.Y && current.X <= previous.X {
			t.Errorf("%s at %+v does not follow %s at %+v in reading order", runOrder[index], current, runOrder[index-1], previous)
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

func TestArrangeLayoutFollowsScoutingRunOrder(t *testing.T) {
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
	fixture := filepath.Join("testdata", "arrange", "scouting.mission.toml")
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

	t.Run("a gate showing file chips keeps its judge clear of them", func(t *testing.T) {
		raw, err := os.ReadFile(fixture)
		if err != nil {
			t.Fatal(err)
		}
		withFiles := func(name, old, replacement string) map[string]LayoutNode {
			if !strings.Contains(string(raw), old) {
				t.Fatalf("fixture has no %q", old)
			}
			path := filepath.Join(t.TempDir(), name)
			writeFixture(t, path, strings.Replace(string(raw), old, replacement, 1))
			_, _, byID := arrangeFixture(t, path, "")
			return byID
		}
		const adversarialGate = "title = \"Adversarial review\"\nkinds = [\"formation\"]\n"
		const criticTitle = "title = \"Brief critic\"\n"
		for label, arranged := range map[string]map[string]LayoutNode{
			"the gate's rubric":       withFiles("gate.mission.toml", adversarialGate, adversarialGate+"files = [\"rubrics/review.md\"]\n"),
			"the judge's brief files": withFiles("judge.mission.toml", criticTitle, criticTitle+"[formation.brief]\nfiles = [\"rubrics/review.md\"]\n"),
		} {
			// The gate's card, its chip row and the note gap all fit above the judge.
			if want := arranged[adversarial].Y + 124 + arrangementFileRow + arrangementRowGap; arranged[critic].X != arranged[adversarial].X || arranged[critic].Y < want {
				t.Errorf("with %s the judge sits at %+v, want below the gate at y>=%d", label, arranged[critic], want)
			}
		}
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
	_, _, byID := arrangeFixture(t, filepath.Join("..", "..", "..", "examples", "delivery.mission.toml"), "")
	assertRunOrder(t, byID, []string{"mis_delivery", "fmn_plan", "fmn_beads", "gate_beads_review", "fmn_execution", "fmn_final_review", "end_delivered"})
	assertJudgeBelow(t, byID, "gate_beads_review", "fmn_beads_reviewer")
}

// This fixture is also asserted by formationsArrange.test.ts in the dashboard.
func TestArrangedLayoutGolden(t *testing.T) {
	for _, name := range []string{"arrange-golden.json", "arrange-long-golden.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("../../../dashboard/src/components/testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Board     BoardDocument `json:"board"`
				Positions []LayoutNode  `json:"positions"`
				Previous  []LayoutNode  `json:"previousPositions"`
			}
			if err := json.Unmarshal(raw, &fixture); err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(fixture.Board)
			got := arrangedLayoutNodes(&fixture.Board)
			if !reflect.DeepEqual(got, fixture.Positions) {
				t.Fatalf("Arrange parity: got %+v, want %+v", got, fixture.Positions)
			}
			if again := arrangedLayoutNodes(&fixture.Board); !reflect.DeepEqual(got, again) {
				t.Fatal("repeated arrangement moved cards")
			}
			after, _ := json.Marshal(fixture.Board)
			if string(after) != string(before) {
				t.Fatal("arrangement changed mission definition")
			}
			// Conservative room includes findings/file rows; each occupied card
			// remains disjoint even when a wrap follows a tall judge/Limit stack.
			sizes := map[string][2]int{}
			for _, n := range fixture.Board.Missions {
				sizes[n.ID] = [2]int{260, 280}
			}
			for _, n := range fixture.Board.Formations {
				w, h := arrangementItemSize(arrangementItem{kind: n.Type, files: n.Brief != nil && len(n.Brief.Files) > 0})
				sizes[n.ID] = [2]int{w, h}
			}
			for _, n := range fixture.Board.Gates {
				sizes[n.ID] = [2]int{332, 244}
			}
			for _, n := range fixture.Board.Ends {
				sizes[n.ID] = [2]int{200, 176}
			}
			for _, n := range fixture.Board.Limits {
				sizes[n.ID] = [2]int{220, 184}
			}
			for i, a := range got {
				for _, b := range got[i+1:] {
					aw, ah, bw, bh := sizes[a.ID][0], sizes[a.ID][1], sizes[b.ID][0], sizes[b.ID][1]
					if a.X < b.X+bw && b.X < a.X+aw && a.Y < b.Y+bh && b.Y < a.Y+ah {
						t.Fatalf("overlapping cards: %+v and %+v", a, b)
					}
				}
			}
			if len(fixture.Previous) > 0 {
				bounds := func(nodes []LayoutNode) (int, int) {
					x, y := 0, 0
					for _, n := range nodes {
						if right := n.X + sizes[n.ID][0]; right > x {
							x = right
						}
						if bottom := n.Y + sizes[n.ID][1]; bottom > y {
							y = bottom
						}
					}
					return x, y
				}
				oldWidth, _ := bounds(fixture.Previous)
				width, height := bounds(got)
				if width > 1700 || width*2 >= oldWidth || height > 2200 {
					t.Fatalf("long graph bounds %dx%d, previous width %d", width, height, oldWidth)
				}
				t.Logf("long graph outer bounds: width %d -> %d, new height %d (saved layout unchanged until explicit Arrange)", oldWidth, width, height)
			}
		})
	}
}
