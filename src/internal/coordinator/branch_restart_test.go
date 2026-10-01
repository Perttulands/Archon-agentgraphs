package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// branchingProofBoard is mission -> A -> human gate (pass Done, fail
// Rejected), and mission -> B -> C -> Done (form-n7u.53).
func branchingProofBoard() string {
	formation := func(id string) string {
		return `
[[formation]]
id = "` + id + `"
type = "solo"
title = "` + strings.ToUpper(strings.TrimPrefix(id, "fmn_")) + `"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_` + id + `"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
effort = "medium"
controller = true
`
	}
	connection := func(id, from, to string) string {
		return "[[connection]]\nid = \"" + id + "\"\nfrom = \"" + from + "\"\nto = \"" + to + "\"\n"
	}
	return `schema = 1
id = "brd_proof"
slug = "proof"
title = "Proof"
rev = 1
[[inputCard]]
id = "mis_proof"
title = "Proof"
goal = "Branching proof"
beadId = "archon-n7u.53"
` + formation("fmn_a") + formation("fmn_b") + formation("fmn_c") + `
[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "Good enough"
` + endNodes + connection("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
		connection("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
		endWire("edge_pass", "gate_review:pass", "end_done") +
		endWire("edge_fail", "gate_review:fail", "end_rejected") +
		connection("edge_m_b", "mis_proof:out", "fmn_b:port_in") +
		connection("edge_b_c", "fmn_b:port_out", "fmn_c:port_in") +
		endWire("edge_c_done", "fmn_c:port_out", "end_done")
}

func openBranchingLab(t *testing.T, root string) *Coordinator {
	t.Helper()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
		return formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: root, Roots: []string{root}})
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func startBranchingLab(t *testing.T) (*Coordinator, string, string) {
	t.Helper()
	root := t.TempDir()
	c := openBranchingLab(t, root)
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(branchingProofBoard()), 0o600); err != nil {
		t.Fatal(err)
	}
	return c, root, startProof(t, c)
}

// restartBranchingLab closes the coordinator and opens a new one on the same
// state, running startup recovery as archond does.
func restartBranchingLab(t *testing.T, c *Coordinator, root string) *Coordinator {
	t.Helper()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	next := openBranchingLab(t, root)
	if err := next.RecoverInterruptedRuns(); err != nil {
		next.Close()
		t.Fatal(err)
	}
	return next
}

func formationOrder(events []formations.RunEvent, eventType string) []string {
	var nodes []string
	for _, event := range events {
		if event.Type == eventType && strings.HasPrefix(event.NodeID, "fmn_") {
			nodes = append(nodes, event.NodeID)
		}
	}
	return nodes
}

// requireBranchesRanBeforeSuccess checks the run succeeded once, last, after
// A, B and C each produced output, with no error on the way.
func requireBranchesRanBeforeSuccess(t *testing.T, events []formations.RunEvent) {
	t.Helper()
	if got := strings.Join(formationOrder(events, formations.RunEventNodeOutput), ","); got != "fmn_a,fmn_b,fmn_c" {
		t.Fatalf("formation outputs = %s, want fmn_a,fmn_b,fmn_c: %s", got, ledgerTrail(events))
	}
	succeeded := 0
	for _, event := range events {
		switch event.Type {
		case formations.RunEventSucceeded:
			succeeded++
		case formations.RunEventError:
			t.Fatalf("run recorded an error: %+v", event.Data)
		}
	}
	if succeeded != 1 || events[len(events)-1].Type != formations.RunEventSucceeded {
		t.Fatalf("want one run_succeeded, last: %s", ledgerTrail(events))
	}
}

// A daemon restart while a terminal human gate waits keeps the other branch
// pending; approving afterwards runs B and C before the run succeeds.
func TestRestartWhileATerminalGateWaitsStillRunsTheOtherBranchAfterApproval(t *testing.T) {
	c, root, id := startBranchingLab(t)
	request := awaitState(t, c, id, "waiting_human").WaitingGates[0]
	if got := strings.Join(formationOrder(eventsOf(t, c, id), formations.RunEventNodeStarted), ","); got != "fmn_a" {
		t.Fatalf("formations started while waiting = %s, want fmn_a", got)
	}
	next := restartBranchingLab(t, c, root)
	t.Cleanup(func() { next.Close() })
	if p := awaitState(t, next, id, "waiting_human"); len(p.WaitingGates) != 1 || p.WaitingGates[0].RequestedSeq != request.RequestedSeq {
		t.Fatalf("restart changed the wait: %+v", p)
	}
	verdict(t, next, id, "gate_review", request.RequestedSeq, true, "")
	awaitState(t, next, id, "succeeded")
	requireBranchesRanBeforeSuccess(t, eventsOf(t, next, id))
}

// A restart between recording the approval and resuming leaves a resumable
// pause, never a success; resuming then runs B and C before success.
func TestRestartBetweenApprovalAndResumeRunsTheOtherBranchOnResume(t *testing.T) {
	c, root, id := startBranchingLab(t)
	awaitState(t, c, id, "waiting_human")
	// Record the verdict the way the verdict route does, without its resume.
	if _, err := c.engine.RecordHumanGateVerdict(id, formations.HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	next := restartBranchingLab(t, c, root)
	t.Cleanup(func() { next.Close() })
	p := awaitState(t, next, id, "blocked")
	if p.Final || !p.ResumeAllowed {
		t.Fatalf("after restart = %+v, want the resumable verdict pause", p)
	}
	if got := strings.Join(formationOrder(eventsOf(t, next, id), formations.RunEventNodeStarted), ","); got != "fmn_a" {
		t.Fatalf("formations started before resume = %s, want fmn_a", got)
	}
	if w := post(t, next, "/api/runs/"+id+"/resume", `{"mode":"reattach","reason":"continue after the approval"}`); w.Code != 202 {
		t.Fatalf("resume %d %s", w.Code, w.Body.String())
	}
	awaitState(t, next, id, "succeeded")
	requireBranchesRanBeforeSuccess(t, eventsOf(t, next, id))
}
