package formations

import (
	"errors"
	"testing"
)

func TestFormationJoinAtomicAndRewireUndo(t *testing.T) {
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("session-search"), s3ConnectionsBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	opts := func() WriteOptions { return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev} }
	wire := func(from, to string) {
		t.Helper()
		before := board
		board, err = store.WireFormationPorts(board.Slug, FormationWireRequest{From: from, To: to, JoinIfOccupied: true}, opts())
		if err != nil {
			t.Fatal(err)
		}
		if board.Rev != before.Rev+1 {
			t.Fatalf("join revision %d after %d", board.Rev, before.Rev)
		}
	}
	wire("fmn_frame:port_frame_out", "fmn_ship:port_ship_in")
	if len(board.Formations[2].Inputs) != 1 {
		t.Fatal("free input grew a port")
	}
	wire("fmn_research:port_research_out", "fmn_ship:port_ship_in")
	if len(board.Formations[2].Inputs) != 2 || len(board.Connections) != 2 {
		t.Fatalf("join: %+v", board)
	}
	joined := board.Connections[1]
	originalID := joined.ID
	if joined.To == "fmn_ship:port_ship_in" {
		t.Fatal("join reused occupied input")
	}
	before := board
	for _, req := range []FormationWireRequest{
		{From: "fmn_frame:port_frame_out", To: "fmn_ship:port_ship_in", JoinIfOccupied: true},
		{From: "fmn_ship:port_ship_out", To: "fmn_ship:port_ship_in", JoinIfOccupied: true},
	} {
		if _, err := store.WireFormationPorts(board.Slug, req, opts()); err == nil {
			t.Fatal("invalid join accepted")
		}
		after, _ := store.ReadBoard(board.Slug)
		if after.ETag != before.ETag {
			t.Fatal("rejected join changed board")
		}
	}
	// Move the second wire away, then join it back and undo in single revisions.
	board, err = store.RewireFormationTarget(board.Slug, FormationRewireRequest{From: joined.From, PreviousTo: joined.To, To: "fmn_frame:port_frame_in", RemovePreviousInput: true}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Formations[2].Inputs) != 1 {
		t.Fatal("join undo left its input")
	}
	if board.Connections[1].ID != originalID {
		t.Fatalf("reconnect identity = %q, want %q", board.Connections[1].ID, originalID)
	}
	before = board
	board, err = store.RewireFormationTarget(board.Slug, FormationRewireRequest{From: joined.From, PreviousTo: "fmn_frame:port_frame_in", To: "fmn_ship:port_ship_in", JoinIfOccupied: true}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if board.Rev != before.Rev+1 || len(board.Formations[2].Inputs) != 2 || len(board.Connections) != 2 {
		t.Fatal("target join not atomic")
	}
	joined = board.Connections[1]
	if joined.ID != originalID {
		t.Fatalf("rejoin identity = %q, want %q", joined.ID, originalID)
	}
	before = board
	board, err = store.RewireFormationTarget(board.Slug, FormationRewireRequest{From: joined.From, PreviousTo: joined.To, To: "fmn_frame:port_frame_in", RemovePreviousInput: true}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if board.Rev != before.Rev+1 || len(board.Formations[2].Inputs) != 1 || len(board.Connections) != 2 {
		t.Fatal("target join undo not atomic")
	}
	if board.Connections[1].ID != originalID {
		t.Fatalf("join undo identity = %q, want %q", board.Connections[1].ID, originalID)
	}
}

func TestJoinKeepsGateSingleFeedAndStaleWritesFail(t *testing.T) {
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("session-search"), s3ConnectionsBoardFixture()+"\n[[gate]]\nid = \"gate_review\"\nkinds = [\"human\"]\n")
	board, _ := store.ReadBoard("session-search")
	stale := WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	board, err := store.WireFormationPorts(board.Slug, FormationWireRequest{From: "fmn_frame:port_frame_out", To: "gate_review:in", JoinIfOccupied: true}, stale)
	if err != nil {
		t.Fatal(err)
	}
	req := FormationWireRequest{From: "fmn_research:port_research_out", To: "gate_review:in", JoinIfOccupied: true}
	if _, err := store.WireFormationPorts(board.Slug, req, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); !errors.Is(err, ErrInputOccupied) {
		t.Fatalf("gate error: %v", err)
	}
	if _, err := store.WireFormationPorts(board.Slug, req, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale error: %v", err)
	}
	after, _ := store.ReadBoard(board.Slug)
	if after.ETag != board.ETag {
		t.Fatal("failed gate join changed board")
	}
}

func TestJoinKeepsToolInputSingleFeed(t *testing.T) {
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("tool-duplicate-producer"), toolStructuralDuplicateProducerBoardFixture(false))
	board, err := store.ReadBoard("tool-duplicate-producer")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.WireFormationPorts(board.Slug, FormationWireRequest{From: "tool_source_b:port_source_b_out", To: "tool_target:port_target_in", JoinIfOccupied: true}, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if !errors.Is(err, ErrInputOccupied) {
		t.Fatalf("Tool join error: %v", err)
	}
	after, _ := store.ReadBoard(board.Slug)
	if after.ETag != board.ETag {
		t.Fatal("Tool join changed board")
	}
}

func TestRewireJoinAndUndoPreservePushbackException(t *testing.T) {
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("session-search"), s3ConnectionsBoardFixture()+`
[[gate]]
id = "gate_review"
kinds = ["human"]
[[connection]]
id = "edge_feed"
from = "fmn_frame:port_frame_out"
to = "fmn_ship:port_ship_in"
[[connection]]
id = "edge_feedback"
from = "gate_review:fail"
to = "fmn_research:port_research_in"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	opts := func() WriteOptions { return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev} }
	// A feedback rewire must not create a new prerequisite that waits for its own output.
	board, err = store.RewireFormationTarget(board.Slug, FormationRewireRequest{From: "gate_review:fail", PreviousTo: "fmn_research:port_research_in", To: "fmn_ship:port_ship_in", JoinIfOccupied: true}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Formations[2].Inputs) != 1 || !hasConnection(board.Connections, "gate_review:fail", "fmn_ship:port_ship_in") {
		t.Fatal("pushback created a required input")
	}
	board, err = store.WireFormationPorts(board.Slug, FormationWireRequest{From: "fmn_ship:port_ship_out", To: "fmn_research:port_research_in"}, opts())
	if err != nil {
		t.Fatal(err)
	}
	board, err = store.RewireFormationTarget(board.Slug, FormationRewireRequest{From: "fmn_frame:port_frame_out", PreviousTo: "fmn_ship:port_ship_in", To: "fmn_research:port_research_in", JoinIfOccupied: true}, opts())
	if err != nil {
		t.Fatal(err)
	}
	joinedTo := board.Connections[len(board.Connections)-1].To
	if joinedTo == "fmn_research:port_research_in" {
		t.Fatal("ordinary feed did not join")
	}
	board, err = store.RewireFormationTarget(board.Slug, FormationRewireRequest{From: "fmn_frame:port_frame_out", PreviousTo: joinedTo, To: "fmn_ship:port_ship_in", RemovePreviousInput: true}, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Formations[1].Inputs) != 1 || !hasConnection(board.Connections, "fmn_frame:port_frame_out", "fmn_ship:port_ship_in") || !hasConnection(board.Connections, "gate_review:fail", "fmn_ship:port_ship_in") {
		t.Fatal("undo did not restore primary feed beside pushback")
	}
}
