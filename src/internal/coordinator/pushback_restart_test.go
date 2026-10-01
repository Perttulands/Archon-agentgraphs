package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// pushbackProofBoard is mission -> A -> terminal human gate, and mission ->
// work -> judge gate whose fail sends back to work and whose pass goes to
// ship (form-n7u.53 review).
func pushbackProofBoard() string {
	board := strings.SplitN(branchingProofBoard(), "[[formation]]", 2)[0]
	formation := func(id string) string {
		return `
[[formation]]
id = "` + id + `"
type = "solo"
title = "` + id + `"
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
	return board + formation("fmn_a") + formation("fmn_work") + formation("fmn_judge") + formation("fmn_ship") + `
[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "Good enough"
[[gate]]
id = "gate_judge"
title = "Judge"
kinds = ["formation"]
criterion = "Judge the work"
` + connection("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
		connection("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
		connection("edge_m_work", "mis_proof:out", "fmn_work:port_in") +
		connection("edge_work_judge", "fmn_work:port_out", "gate_judge:in") +
		connection("edge_gate_judge", "gate_judge:judge", "fmn_judge:port_in") +
		connection("edge_judge_verdict", "fmn_judge:port_out", "gate_judge:judge") +
		connection("edge_judge_fail", "gate_judge:fail", "fmn_work:port_in") +
		connection("edge_judge_pass", "gate_judge:pass", "fmn_ship:port_in")
}

// judgeFailsOnceExecutor runs formations with the lab executor, except that
// the judge's first verdict is fail and later ones pass. One instance spans
// daemon restarts, as a judge's history would.
type judgeFailsOnceExecutor struct {
	mu     sync.Mutex
	lab    formations.FormationExecutor
	judged int
}

func (e *judgeFailsOnceExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	e.mu.Lock()
	lab := e.lab
	e.mu.Unlock()
	result, err := lab.ExecuteFormation(req)
	if err != nil || req.NodeID != "fmn_judge" {
		return result, err
	}
	e.mu.Lock()
	e.judged++
	verdict := "pass"
	if e.judged == 1 {
		verdict = "fail"
	}
	e.mu.Unlock()
	result.Text = "```archon-verdict\n{\"verdict\":\"" + verdict + "\",\"reason\":\"judged " + verdict + "\",\"evidence\":[]}\n```"
	for port := range result.Outputs {
		result.Outputs[port] = formations.FormationOutputPayload{Text: result.Text}
	}
	return result, nil
}

func (e *judgeFailsOnceExecutor) open(t *testing.T, root string) *Coordinator {
	t.Helper()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
		e.mu.Lock()
		e.lab = formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: root, Roots: []string{root}})
		e.mu.Unlock()
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// A send-back a judge routes during the resume after an approval, with a
// daemon restart while the human gate waited, runs the work again and then
// ship before the run succeeds.
func TestASendBackAfterApprovalAcrossARestartRunsTheWorkAgain(t *testing.T) {
	root := t.TempDir()
	executor := &judgeFailsOnceExecutor{}
	c := executor.open(t, root)
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(pushbackProofBoard()), 0o600); err != nil {
		t.Fatal(err)
	}
	id := startProof(t, c)
	request := awaitState(t, c, id, "waiting_human").WaitingGates[0]
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	next := executor.open(t, root)
	t.Cleanup(func() { next.Close() })
	if err := next.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	if p := awaitState(t, next, id, "waiting_human"); p.WaitingGates[0].RequestedSeq != request.RequestedSeq {
		t.Fatalf("restart changed the wait: %+v", p)
	}
	verdict(t, next, id, "gate_review", request.RequestedSeq, true, "")
	awaitState(t, next, id, "succeeded")
	events := eventsOf(t, next, id)
	if got := strings.Join(formationOrder(events, formations.RunEventNodeOutput), ","); got != "fmn_a,fmn_work,fmn_judge,fmn_work,fmn_judge,fmn_ship" {
		t.Fatalf("formation outputs = %s: %s", got, ledgerTrail(events))
	}
	if events[len(events)-1].Type != formations.RunEventSucceeded {
		t.Fatalf("ledger does not end in success: %s", ledgerTrail(events))
	}
}
