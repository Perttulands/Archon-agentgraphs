package formations

import (
	"reflect"
	"strings"
	"testing"
)

// joinRejectBoard is Input -> A -> gate (pass -> J.a, fail -> Rejected) and
// Input -> B -> J.b, with J -> Done (archon-o7p.10 review). bFirst wires B
// before A, so B has fed J before the gate decides; otherwise B runs after.
// The gate is a human gate, or a code gate when code is set.
func joinRejectBoard(bFirst, code bool) string {
	gate := branchingBoardHumanGate("gate_review")
	if code {
		gate = `
[[gate]]
id = "gate_review"
title = "Review"
kinds = ["code"]
criterion = "Good enough"
check = "output_contains"
checkVersion = "1"
checkValue = "output from"
`
	}
	join := `
[[formation]]
id = "fmn_j"
type = "solo"
title = "J"

[[formation.input]]
id = "port_fmn_j_a"
label = "From A"

[[formation.input]]
id = "port_fmn_j_b"
label = "From B"

[[formation.output]]
id = "port_fmn_j_out"
label = "Output"

[[formation.slot]]
id = "slot_fmn_j"
label = "Worker"
agentId = "scout"
harness = "openai-codex"
effort = "medium"
controller = true
`
	toA := branchingBoardConnection("edge_m_a", "mis_showcase:out", "fmn_a:port_fmn_a_in")
	toB := branchingBoardConnection("edge_m_b", "mis_showcase:out", "fmn_b:port_fmn_b_in")
	starts := toA + toB
	if bFirst {
		starts = toB + toA
	}
	return s4MissionOnlyBoardFixture() +
		branchingBoardFormation("fmn_a", "A") + branchingBoardFormation("fmn_b", "B") + join +
		gate + branchingBoardEnds() + starts +
		branchingBoardConnection("edge_a_gate", "fmn_a:port_fmn_a_out", "gate_review:in") +
		branchingBoardConnection("edge_pass_j", "gate_review:pass", "fmn_j:port_fmn_j_a") +
		branchingBoardConnection("edge_gate_review_fail", "gate_review:fail", "end_rejected:in") +
		branchingBoardConnection("edge_b_j", "fmn_b:port_fmn_b_out", "fmn_j:port_fmn_j_b") +
		branchingBoardConnection("edge_j_done", "fmn_j:port_fmn_j_out", "end_done:in")
}

// A rejection upstream of a join fails the run with the gate's reason, not a
// starved block: once the rejected path has ended, a join it starved is not
// work that can still run. B feeds the join while the gate waits, whichever
// branch is wired first (archon-o7p.11), on the same engine or after a
// restart.
func TestARejectionUpstreamOfAJoinFailsTheRunWithTheGatesReason(t *testing.T) {
	for _, bFirst := range []bool{true, false} {
		for _, restart := range []bool{false, true} {
			name := map[bool]string{true: "B wired first", false: "A wired first"}[bFirst] + map[bool]string{false: "", true: ", after a restart"}[restart]
			t.Run(name, func(t *testing.T) {
				executor := &fakeRunExecutor{}
				store, personas, engine, status := startBranchingRun(t, joinRejectBoard(bFirst, false), executor)
				if got := pendingHumanGates(t, store, status.RunID); !reflect.DeepEqual(got, []string{"gate_review"}) {
					t.Fatalf("pending gates = %v", got)
				}
				frozen, err := store.ReadRunBoard(status.RunID)
				if err != nil {
					t.Fatal(err)
				}
				events, err := store.ReadRunEvents(status.RunID)
				if err != nil {
					t.Fatal(err)
				}
				// The answer panel agrees with the engine: rejecting fails the run
				// at once, since B has fed the join.
				reject := HumanGateRoutes(frozen, events, "gate_review")[1]
				if !reject.EndsRun || !reject.RunFails {
					t.Fatalf("reject route = %+v, want endsRun and runFails", reject)
				}
				if restart {
					engine = NewRunEngine(store, personas, executor)
				}
				executor.calls = nil
				status = rejectAndContinue(t, engine, status.RunID, "gate_review", "A is wrong")
				if got := executor.nodeIDs(); len(got) != 0 {
					t.Fatalf("nodes after the rejection = %v, want none", got)
				}
				if status.Status != RunStatusFailed || !status.Final {
					t.Fatalf("status = %+v, want failed", status)
				}
				requireRunFailedWithReason(t, store, status.RunID, "end_rejected", "gate_review", "A is wrong")
			})
		}
	}
}

// The same board fails on first execution when a code gate rejects; the human
// gate cases above cover resume, on the same engine and after a restart.
func TestARejectionUpstreamOfAJoinFailsOnFirstExecution(t *testing.T) {
	store, personas := s4RunFixture(t)
	store.Now = fixedClock()
	personas.Now = fixedClock()
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), joinRejectBoard(false, true))
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)
	engine.SetGateEvaluator(&fakeGateEvaluator{verdicts: []string{"fail"}})
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := executor.nodeIDs(); !reflect.DeepEqual(got, []string{"fmn_a", "fmn_b"}) {
		t.Fatalf("nodes = %v, want A and B, and never the starved join", got)
	}
	if status.Status != RunStatusFailed {
		t.Fatalf("status = %+v, want failed", status)
	}
	requireRunFailedWithReason(t, store, status.RunID, "end_rejected", "gate_review", "fake fail")

}

// Without a rejection, a join that can never receive its other input is still
// a wiring gap: the run blocks as starved instead of succeeding.
func TestAJoinStarvedWithoutARejectionStillBlocksAsStarved(t *testing.T) {
	fixture := strings.Replace(joinRejectBoard(true, false), "to = \"fmn_j:port_fmn_j_a\"", "to = \"end_done:in\"", 1)
	store, _, engine, status := startBranchingRun(t, fixture, &fakeRunExecutor{})
	status = approveAndContinue(t, engine, status.RunID, "gate_review")
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if status.Status != RunStatusBlocked || last.Data["code"] != "reachable_node_starved" || last.NodeID != "fmn_j" {
		t.Fatalf("status %s, last %+v, want a starved block at J", status.Status, last)
	}
}
