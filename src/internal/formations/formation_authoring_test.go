package formations

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// removePushbackFixtureBlock removes one exact connection block from the S5
// pushback fixture so tests can re-wire it through the authoring API.
func removePushbackFixtureBlock(t *testing.T, fixture, block string) string {
	t.Helper()
	if !strings.Contains(fixture, block) {
		t.Fatalf("fixture does not contain block:\n%s", block)
	}
	return strings.Replace(fixture, block, "", 1)
}

const s5MissionWorkEdgeBlock = `[[connection]]
id = "edge_mission_work"
from = "mis_showcase:out"
to = "fmn_work:port_work_in"
`

const s5GateFailEdgeBlock = `[[connection]]
id = "edge_gate_fail_work"
from = "gate_review:fail"
to = "fmn_work:port_work_in"
`

// ADR-0012: the gate-fail pushback edge is exempt from the one-producer rule
// at the wire site, in either authoring order; a second non-pushback producer
// stays a typed conflict.
func TestWireFormationPortsAllowsGateFailPushback(t *testing.T) {
	t.Run("pushback wired second", func(t *testing.T) {
		store, _ := s4RunFixture(t)
		fixture := removePushbackFixtureBlock(t, s5HumanGatePushbackBoardFixture(), s5GateFailEdgeBlock)
		writeFixture(t, store.BoardPath("session-search"), fixture)
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatalf("read board: %v", err)
		}
		board, err = store.WireFormationPorts("session-search", FormationWireRequest{
			From: "gate_review:fail", To: "fmn_work:port_work_in", UpdatedBy: "agent:test",
		}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
		if err != nil {
			t.Fatalf("wire gate-fail pushback into occupied input: %v", err)
		}
		if _, err := store.WireFormationPorts("session-search", FormationWireRequest{
			From: "fmn_ship:port_ship_out", To: "fmn_work:port_work_in", UpdatedBy: "agent:test",
		}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); !errors.Is(err, ErrInputOccupied) {
			t.Fatalf("second non-pushback producer error = %v, want ErrInputOccupied", err)
		}
	})
	t.Run("pushback wired first", func(t *testing.T) {
		store, _ := s4RunFixture(t)
		fixture := removePushbackFixtureBlock(t, s5HumanGatePushbackBoardFixture(), s5GateFailEdgeBlock)
		fixture = removePushbackFixtureBlock(t, fixture, s5MissionWorkEdgeBlock)
		writeFixture(t, store.BoardPath("session-search"), fixture)
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatalf("read board: %v", err)
		}
		board, err = store.WireFormationPorts("session-search", FormationWireRequest{
			From: "gate_review:fail", To: "fmn_work:port_work_in", UpdatedBy: "agent:test",
		}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
		if err != nil {
			t.Fatalf("wire gate-fail pushback first: %v", err)
		}
		if _, err := store.WireFormationPorts("session-search", FormationWireRequest{
			From: "mis_showcase:out", To: "fmn_work:port_work_in", UpdatedBy: "agent:test",
		}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); err != nil {
			t.Fatalf("wire primary producer after pushback edge: %v", err)
		}
	})
	t.Run("exact duplicate pushback edge still conflicts", func(t *testing.T) {
		store, _ := s4RunFixture(t)
		writeFixture(t, store.BoardPath("session-search"), s5HumanGatePushbackBoardFixture())
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatalf("read board: %v", err)
		}
		if _, err := store.WireFormationPorts("session-search", FormationWireRequest{
			From: "gate_review:fail", To: "fmn_work:port_work_in", UpdatedBy: "agent:test",
		}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); !errors.Is(err, ErrDuplicateConnection) {
			t.Fatalf("duplicate pushback edge error = %v, want ErrDuplicateConnection", err)
		}
	})
}

// ADR-0012: the engine's own S5 pushback topology must pass board validation —
// the duplicate-producer scan counts only non-pushback producers.
func TestValidateBoardAcceptsGateFailPushbackTopology(t *testing.T) {
	report := ValidateBoard(mustParseValidateBoardFixture(t, s5HumanGatePushbackBoardFixture()))
	if len(report.Errors) != 0 {
		t.Fatalf("pushback board produced errors: %+v", report.Errors)
	}
}

func TestWireRestoreIdentityAndCollision(t *testing.T) {
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("session-search"), s3ConnectionsBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	opts := func() WriteOptions { return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev} }
	req := FormationWireRequest{ID: "edge_restored", From: "fmn_frame:port_frame_out", To: "fmn_ship:port_ship_in"}
	board, err = store.WireFormationPorts(board.Slug, req, opts())
	if err != nil {
		t.Fatal(err)
	}
	if board.Connections[0].ID != req.ID {
		t.Fatalf("restored identity = %q", board.Connections[0].ID)
	}
	writeFixture(t, store.LayoutPath(board.Slug), fmt.Sprintf("schema = 1\nmissionId = %q\nmissionRev = %d\n[[edge]]\nid = %q\nlane = \"y:40\"\n", board.ID, board.Rev, req.ID))
	before := board
	// Even a joining request cannot publish a new port if its wire identity is taken.
	_, err = store.WireFormationPorts(board.Slug, FormationWireRequest{ID: req.ID, From: "fmn_research:port_research_out", To: req.To, JoinIfOccupied: true}, opts())
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("collision = %v", err)
	}
	after, err := store.ReadBoard(board.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if after.ETag != before.ETag {
		t.Fatal("rejected collision changed graph")
	}
	board, err = store.UnwireFormationPorts(board.Slug, req, opts())
	if err != nil {
		t.Fatal(err)
	}
	board, err = store.WireFormationPorts(board.Slug, req, opts())
	if err != nil || board.Connections[0].ID != req.ID {
		t.Fatalf("remove/restore identity: %+v %v", board, err)
	}
	layout, err := store.ReadLayout(board.Slug)
	if err != nil || len(layout.Edges) != 1 || layout.Edges[0].ID != req.ID || layout.Edges[0].Lane != "y:40" {
		t.Fatalf("restored wire lane: %+v %v", layout, err)
	}
	board, err = store.WireFormationPorts(board.Slug, FormationWireRequest{From: "fmn_research:port_research_out", To: "fmn_frame:port_frame_in"}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if board.Connections[1].ID == "" || board.Connections[1].ID == req.ID {
		t.Fatal("ordinary wire did not allocate a distinct identity")
	}
}
