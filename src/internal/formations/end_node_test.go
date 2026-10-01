package formations

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// End nodes are authored like any node (form-o7p.10): create, rename, change
// outcome, wire any number of routes into one, delete, and restore on undo.
func TestEndNodesAreAuthoredWiredDeletedAndRestored(t *testing.T) {
	store := NewStore(t.TempDir())
	store.Now = fixedClock()
	writeFixture(t, store.BoardPath("sketch"), minimalBoard("sketch", 1))
	current := func() WriteOptions {
		t.Helper()
		board, err := store.ReadBoard("sketch")
		if err != nil {
			t.Fatal(err)
		}
		return WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	}
	gate, err := store.CreateGate("sketch", GateCreateRequest{Title: "Brief sign-off"}, current())
	if err != nil {
		t.Fatal(err)
	}
	done, err := store.CreateEnd("sketch", EndCreateRequest{X: 840, Y: 168}, current())
	if err != nil {
		t.Fatal(err)
	}
	if done.End.Outcome != EndOutcomeDone || done.End.Title != "Done" || !strings.HasPrefix(done.End.ID, "end_") {
		t.Fatalf("default End node = %+v, want Done with outcome done", done.End)
	}
	rejected, err := store.CreateEnd("sketch", EndCreateRequest{Outcome: "rejected"}, current())
	if err != nil {
		t.Fatal(err)
	}
	if rejected.End.Title != "Rejected" || rejected.End.Outcome != EndOutcomeRejected {
		t.Fatalf("rejected End node = %+v", rejected.End)
	}
	if _, err := store.CreateEnd("sketch", EndCreateRequest{Outcome: "maybe"}, current()); !errors.Is(err, ErrInvalidEndOutcome) {
		t.Fatalf("End node with outcome maybe: %v, want ErrInvalidEndOutcome", err)
	}

	// Both gate routes may lead into one End node; an End node leads nowhere.
	for _, from := range []string{gate.Gate.ID + ":pass", gate.Gate.ID + ":fail"} {
		if _, err := store.WireFormationPorts("sketch", FormationWireRequest{From: from, To: done.End.ID + ":in"}, current()); err != nil {
			t.Fatalf("wire %s to the End node: %v", from, err)
		}
	}
	if _, err := store.WireFormationPorts("sketch", FormationWireRequest{From: done.End.ID + ":in", To: gate.Gate.ID + ":in"}, current()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wire out of an End node: %v, want refused", err)
	}
	if _, err := store.RewireFormationTarget("sketch", FormationRewireRequest{From: gate.Gate.ID + ":fail", PreviousTo: done.End.ID + ":in", To: rejected.End.ID + ":in"}, current()); err != nil {
		t.Fatalf("rewire the fail route to the rejected End node: %v", err)
	}
	renamed, err := store.UpdateEnd("sketch", EndUpdateRequest{EndID: done.End.ID, Title: stringPtr("Shipped")}, current())
	if err != nil {
		t.Fatal(err)
	}
	if end, _ := findEnd(renamed, done.End.ID); end.Title != "Shipped" || end.Outcome != EndOutcomeDone {
		t.Fatalf("renamed End node = %+v", end)
	}
	if _, err := store.UpdateEnd("sketch", EndUpdateRequest{EndID: done.End.ID, Outcome: stringPtr("lost")}, current()); !errors.Is(err, ErrInvalidEndOutcome) {
		t.Fatalf("update to outcome lost: %v", err)
	}
	flipped, err := store.UpdateEnd("sketch", EndUpdateRequest{EndID: rejected.End.ID, Outcome: stringPtr("done")}, current())
	if err != nil {
		t.Fatal(err)
	}
	if end, _ := findEnd(flipped, rejected.End.ID); end.Outcome != EndOutcomeDone {
		t.Fatalf("flipped End node = %+v", end)
	}
	if report := ValidateBoard(flipped); len(findBoardFindings(report.Errors, FindingRouteLeadsNowhere)) != 0 || len(findBoardFindings(report.Errors, FindingDuplicateInputProducer)) != 0 {
		t.Fatalf("wired gate has findings %+v", report.Errors)
	}

	// Delete, then restore with its connections and position, as undo does.
	before, err := store.ReadBoard("sketch")
	if err != nil {
		t.Fatal(err)
	}
	end, _ := findEnd(before, done.End.ID)
	var touching []BoardConnection
	for _, connection := range before.Connections {
		if endpointNodeID(connection.To) == end.ID {
			touching = append(touching, connection)
		}
	}
	deleted, err := store.DeleteEnd("sketch", EndDeleteRequest{ID: end.ID}, current())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := findEnd(deleted.Board, end.ID); ok || len(deleted.Board.Connections) != len(before.Connections)-len(touching) {
		t.Fatalf("after delete: board %+v", deleted.Board)
	}
	if !hasBoardFinding(ValidateBoard(deleted.Board).Errors, gate.Gate.ID, "Brief sign-off's pass route leads nowhere: wire it to a step or an End node") {
		t.Fatalf("after delete: %+v, want the pass route to lead nowhere", ValidateBoard(deleted.Board).Errors)
	}
	restored, err := store.RestoreNode("sketch", NodeRestoreRequest{End: &end, Connections: touching, X: 840, Y: 168}, current())
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := findEnd(restored.Board, end.ID); !reflect.DeepEqual(got, end) || len(restored.Board.Connections) != len(before.Connections) {
		t.Fatalf("restored End node %+v with %d connections, want %+v with %d", got, len(restored.Board.Connections), end, len(before.Connections))
	}
	if !layoutHasNode(restored.Layout, end.ID, 840, 168) {
		t.Fatalf("restored layout = %+v", restored.Layout.Nodes)
	}
}

func layoutHasNode(layout *LayoutDocument, id string, x, y int) bool {
	for _, node := range layout.Nodes {
		if node.ID == id {
			return node.X == x && node.Y == y
		}
	}
	return false
}

const routedEndBoard = `schema = 1
id = "brd_routes"
slug = "routes"
title = "Routes"
rev = 1

[[inputCard]]
id = "mis_main"
title = "Input"
goal = "Write the brief"

[[formation]]
id = "fmn_draft"
type = "solo"
title = "Draft"
[[formation.input]]
id = "port_draft_in"
label = "Input"
[[formation.output]]
id = "port_draft_out"
label = "Draft"
[[formation.output]]
id = "port_draft_notes"
label = "Notes"
[[formation.slot]]
id = "slot_draft"
label = "Writer"
harness = "claude-code"
effort = "medium"
controller = true

[[formation]]
id = "fmn_orphan"
type = "solo"
title = "Orphan"
[[formation.input]]
id = "port_orphan_in"
label = "Input"
[[formation.output]]
id = "port_orphan_out"
label = "Output"
[[formation.slot]]
id = "slot_orphan"
label = "Writer"
harness = "claude-code"
effort = "low"
controller = true

[[gate]]
id = "gate_signoff"
title = "Brief sign-off"
kinds = ["human"]
criterion = "The brief is ready"

[[end]]
id = "end_done"
title = "Done"
outcome = "done"

[[end]]
id = "end_spare"
title = "Spare"
outcome = "rejected"

[[connection]]
id = "edge_start"
from = "mis_main:out"
to = "fmn_draft:port_draft_in"

[[connection]]
id = "edge_gate"
from = "fmn_draft:port_draft_out"
to = "gate_signoff:in"

[[connection]]
id = "edge_fail"
from = "gate_signoff:fail"
to = "fmn_draft:port_draft_in"
`

// Validation and admission refuse every output and gate route that leads
// nowhere, in words, and warn about nodes no path from the Input card
// reaches. Drafts stay editable, and a single step's run ignores its routes.
func TestDanglingRoutesAreRefusedAndUnreachableNodesWarned(t *testing.T) {
	board, err := parseBoard([]byte(routedEndBoard))
	if err != nil {
		t.Fatal(err)
	}
	report := ValidateBoard(board)
	var dangling []string
	for _, finding := range findBoardFindings(report.Errors, FindingRouteLeadsNowhere) {
		dangling = append(dangling, finding.NodeID+": "+finding.Message)
	}
	wantDangling := []string{
		`fmn_draft: Draft's "Notes" output leads nowhere: wire it to a step or an End node`,
		"fmn_orphan: Orphan's output leads nowhere: wire it to a step or an End node",
		"gate_signoff: Brief sign-off's pass route leads nowhere: wire it to a step or an End node",
	}
	if !reflect.DeepEqual(dangling, wantDangling) {
		t.Fatalf("dangling routes = %q, want %q", dangling, wantDangling)
	}
	var unreachable []string
	for _, finding := range findBoardFindings(report.Warnings, FindingUnreachableNode) {
		unreachable = append(unreachable, finding.Message)
	}
	wantUnreachable := []string{
		"No path from the Input card reaches End node Done, so no run will get there; wire a route into it or delete it",
		"No path from the Input card reaches End node Spare, so no run will get there; wire a route into it or delete it",
		"No path from the Input card reaches step Orphan, so no run will get there; wire a route into it or delete it",
	}
	if !reflect.DeepEqual(unreachable, wantUnreachable) {
		t.Fatalf("unreachable = %q, want %q", unreachable, wantUnreachable)
	}

	// Admission names the routes on the run path; the orphan is off it.
	err = CheckRunAdmission(board, nil, RunAdmissionScope{MissionID: "mis_main"})
	var admission *RunAdmissionError
	if !errors.As(err, &admission) || !strings.Contains(err.Error(), "Brief sign-off's pass route leads nowhere: wire it to a step or an End node") ||
		!strings.Contains(err.Error(), `Draft's "Notes" output leads nowhere`) || strings.Contains(err.Error(), "Orphan") {
		t.Fatalf("admission = %v, want the run path's dangling routes", err)
	}
	if err := CheckRunAdmission(board, nil, RunAdmissionScope{FormationID: "fmn_orphan"}); err != nil {
		t.Fatalf("single-step run of the orphan = %v, want its routes ignored", err)
	}

	// Wiring every route clears the errors; the drafts saved along the way.
	store := NewStore(t.TempDir())
	writeFixture(t, store.BoardPath("routes"), routedEndBoard)
	for _, wire := range [][2]string{{"gate_signoff:pass", "end_done:in"}, {"fmn_draft:port_draft_notes", "end_done:in"}, {"fmn_orphan:port_orphan_out", "end_spare:in"}} {
		current, err := store.ReadBoard("routes")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.WireFormationPorts("routes", FormationWireRequest{From: wire[0], To: wire[1]}, WriteOptions{ExpectedETag: current.ETag, ExpectedRev: current.Rev}); err != nil {
			t.Fatalf("wire %v on a draft: %v", wire, err)
		}
	}
	wired, err := store.ReadBoard("routes")
	if err != nil {
		t.Fatal(err)
	}
	report = ValidateBoard(wired)
	if len(report.Errors) != 0 {
		t.Fatalf("wired board errors = %+v", report.Errors)
	}
	if warnings := findBoardFindings(report.Warnings, FindingUnreachableNode); len(warnings) != 2 || warnings[0].NodeID != "end_spare" || warnings[1].NodeID != "fmn_orphan" {
		t.Fatalf("wired board unreachable warnings = %+v, want Spare and Orphan", warnings)
	}
	if err := CheckRunAdmission(wired, nil, RunAdmissionScope{MissionID: "mis_main"}); err != nil {
		t.Fatalf("wired board admission: %v", err)
	}
}

func TestRouteLeadsNowhereNamesTitlesEndingInS(t *testing.T) {
	board, err := parseBoard([]byte(strings.Replace(routedEndBoard, `title = "Orphan"`, `title = "Research notes"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if !hasBoardFinding(ValidateBoard(board).Errors, "fmn_orphan", "Research notes' output leads nowhere: wire it to a step or an End node") {
		t.Fatalf("errors = %+v", ValidateBoard(board).Errors)
	}
}

func TestEndNodeOutcomesAreValidated(t *testing.T) {
	raw := strings.Replace(routedEndBoard, `outcome = "rejected"`, `outcome = "abandoned"`, 1)
	board, err := parseBoard([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !hasBoardFinding(ValidateBoard(board).Errors, "end_spare", `End node Spare has outcome "abandoned"; set it to done or rejected`) {
		t.Fatalf("errors = %+v", ValidateBoard(board).Errors)
	}
	for _, value := range []string{"", "done", " rejected "} {
		if _, err := NormalizeEndOutcome(value); err != nil {
			t.Fatalf("NormalizeEndOutcome(%q) = %v", value, err)
		}
	}
}
