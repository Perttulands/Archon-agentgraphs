package coordinator

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// A human gate blocks only the work it gates (archon-o7p.11). These boards
// run a held branch beside one or two human gates, so the tests can answer the
// gates while a seat still works.

func gateBoardFormation(id string) string {
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

func gateBoardHumanGate(id string) string {
	return `
[[gate]]
id = "` + id + `"
title = "` + strings.ToUpper(strings.TrimPrefix(id, "gate_")) + `"
kinds = ["human"]
criterion = "Good enough"
`
}

func gateBoardConnection(id, from, to string) string {
	return "[[connection]]\nid = \"" + id + "\"\nfrom = \"" + from + "\"\nto = \"" + to + "\"\n"
}

func gateBoard(body string) string {
	return `schema = 1
id = "brd_proof"
slug = "proof"
title = "Proof"
rev = 1
[[inputCard]]
id = "mis_proof"
title = "Proof"
goal = "Concurrent gates"
` + body
}

// oneGateBesideABranch is Input -> A -> Review (pass Done, fail Rejected)
// and Input -> B -> Done.
func oneGateBesideABranch() string {
	return gateBoard(gateBoardFormation("fmn_a") + gateBoardFormation("fmn_b") + gateBoardHumanGate("gate_review") + endNodes +
		gateBoardConnection("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
		gateBoardConnection("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
		endWire("edge_pass", "gate_review:pass", "end_done") +
		endWire("edge_fail", "gate_review:fail", "end_rejected") +
		gateBoardConnection("edge_m_b", "mis_proof:out", "fmn_b:port_in") +
		endWire("edge_b_done", "fmn_b:port_out", "end_done"))
}

// twoGatesBesideABranch is Input -> A -> One and Input -> B -> Two, each gate
// ending Done or Rejected, and Input -> C -> Done.
func twoGatesBesideABranch() string {
	return gateBoard(gateBoardFormation("fmn_a") + gateBoardFormation("fmn_b") + gateBoardFormation("fmn_c") +
		gateBoardHumanGate("gate_one") + gateBoardHumanGate("gate_two") + endNodes +
		gateBoardConnection("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
		gateBoardConnection("edge_a_one", "fmn_a:port_out", "gate_one:in") +
		endWire("edge_one_pass", "gate_one:pass", "end_done") +
		endWire("edge_one_fail", "gate_one:fail", "end_rejected") +
		gateBoardConnection("edge_m_b", "mis_proof:out", "fmn_b:port_in") +
		gateBoardConnection("edge_b_two", "fmn_b:port_out", "gate_two:in") +
		endWire("edge_two_pass", "gate_two:pass", "end_done") +
		endWire("edge_two_fail", "gate_two:fail", "end_rejected") +
		gateBoardConnection("edge_m_c", "mis_proof:out", "fmn_c:port_in") +
		endWire("edge_c_done", "fmn_c:port_out", "end_done"))
}

// sendBackIntoAWorkingStep is Input -> A -> Review (pass Done, fail back to
// B) and Input -> B -> Done, so rejecting Review sends work back to B while B
// is still working on its first attempt.
func sendBackIntoAWorkingStep() string {
	return gateBoard(gateBoardFormation("fmn_a") + gateBoardFormation("fmn_b") + gateBoardHumanGate("gate_review") + endNodes +
		gateBoardConnection("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
		gateBoardConnection("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
		endWire("edge_pass", "gate_review:pass", "end_done") +
		gateBoardConnection("edge_fail_b", "gate_review:fail", "fmn_b:port_in") +
		gateBoardConnection("edge_m_b", "mis_proof:out", "fmn_b:port_in") +
		endWire("edge_b_done", "fmn_b:port_out", "end_done"))
}

// holdingExecutor finishes every step at once except the held ones, whose
// first attempt waits until the test releases it.
type holdingExecutor struct {
	mu       sync.Mutex
	releases map[string]chan struct{}
	held     map[string]bool
	started  chan string
	calls    []formations.FormationExecution
}

func newHoldingExecutor(held ...string) *holdingExecutor {
	e := &holdingExecutor{releases: map[string]chan struct{}{}, held: map[string]bool{}, started: make(chan string, 32)}
	for _, nodeID := range held {
		e.releases[nodeID] = make(chan struct{})
		e.held[nodeID] = true
	}
	return e
}

// release lets the held node's first attempt finish.
func (e *holdingExecutor) release(nodeID string) {
	close(e.releases[nodeID])
}

func (e *holdingExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, req)
	var release chan struct{}
	if e.held[req.NodeID] {
		release = e.releases[req.NodeID]
		delete(e.held, req.NodeID)
	}
	e.mu.Unlock()
	e.started <- req.NodeID
	if release != nil {
		<-release
	}
	text := "output from " + req.NodeID
	outputs := map[string]formations.FormationOutputPayload{}
	for _, port := range req.Formation.Outputs {
		outputs[port.ID] = formations.FormationOutputPayload{Text: text}
	}
	return formations.FormationExecutionResult{Status: "done", Text: text, Outputs: outputs}, nil
}

func (e *holdingExecutor) snapshot() []formations.FormationExecution {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]formations.FormationExecution(nil), e.calls...)
}

// awaitStarted waits until the executor starts nodeID.
func (e *holdingExecutor) awaitStarted(t *testing.T, nodeID string) {
	t.Helper()
	// Generous: under host load each durable append can take a while.
	timeout := time.After(20 * time.Second)
	for {
		select {
		case started := <-e.started:
			if started == nodeID {
				return
			}
		case <-timeout:
			t.Fatalf("%s never started", nodeID)
		}
	}
}

func openGateLab(t *testing.T, board string, executor formations.FormationExecutor) *Coordinator {
	t.Helper()
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	c, err := Open(root, personas, func(*formations.Store) formations.FormationExecutor { return executor })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(board), 0o600); err != nil {
		t.Fatal(err)
	}
	return c
}

// awaitWhileBusy waits until the projection satisfies ok, whether or not the
// run's worker is still executing.
func awaitWhileBusy(t *testing.T, c *Coordinator, id string, ok func(*Projection) bool) *Projection {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, err := c.Project(id)
		if err != nil {
			t.Fatal(err)
		}
		if ok(p) {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatalf("projection never matched: status %s, waiting %+v: %s", p.Status, p.WaitingGates, ledgerTrail(eventsOf(t, c, id)))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitingGateIDs(p *Projection) []string {
	ids := []string{}
	for _, gate := range p.WaitingGates {
		ids = append(ids, gate.GateID)
	}
	return ids
}

func requestedSeq(t *testing.T, p *Projection, gateID string) int {
	t.Helper()
	for _, gate := range p.WaitingGates {
		if gate.GateID == gateID {
			return gate.RequestedSeq
		}
	}
	t.Fatalf("%s is not waiting: %+v", gateID, p.WaitingGates)
	return 0
}

// postVerdict answers a gate and reports how long the daemon took to accept it.
func postVerdict(t *testing.T, c *Coordinator, id, gateID string, seq int, verdict, response string) time.Duration {
	t.Helper()
	began := time.Now()
	w := post(t, c, "/api/runs/"+id+"/gates/"+gateID+"/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"`+verdict+`","reason":`+strconv.Quote(response)+`}`)
	if w.Code != 202 {
		t.Fatalf("verdict on %s: %d %s", gateID, w.Code, w.Body.String())
	}
	return time.Since(began)
}

func seqOf(t *testing.T, events []formations.RunEvent, eventType, nodeID string) int {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType && (event.NodeID == nodeID || event.GateID == nodeID) {
			return event.Seq
		}
	}
	t.Fatalf("no %s for %s: %s", eventType, nodeID, ledgerTrail(events))
	return 0
}

// While B works, the gate on A's path waits; the operator's verdict is
// accepted at once, without waiting for B, and the run routes it once B has
// recorded its output.
func TestAVerdictIsAcceptedWhileAnotherBranchWorks(t *testing.T) {
	executor := newHoldingExecutor("fmn_b")
	c := openGateLab(t, oneGateBesideABranch(), executor)
	id := startProof(t, c)
	executor.awaitStarted(t, "fmn_b")
	p := awaitWhileBusy(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 })
	if p.Status != "waiting_human" || p.Final {
		t.Fatalf("while B works and Review waits: %+v", p)
	}
	if took := postVerdict(t, c, id, "gate_review", p.WaitingGates[0].RequestedSeq, "pass", "ship A"); took >= runCommandWait {
		t.Fatalf("the verdict waited %v for B", took)
	}
	p = awaitWhileBusy(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 0 })
	if p.Status != "running" {
		t.Fatalf("after the verdict, while B works: %+v", p)
	}
	executor.release("fmn_b")
	awaitState(t, c, id, "succeeded")
	events := eventsOf(t, c, id)
	recorded, bDone, routed := seqOf(t, events, formations.RunEventHumanVerdictRecorded, "gate_review"), seqOf(t, events, formations.RunEventNodeOutput, "fmn_b"), seqOf(t, events, formations.RunEventGateVerdict, "gate_review")
	if !(recorded < bDone && bDone < routed) {
		t.Fatalf("verdict recorded %d, B done %d, routed %d: %s", recorded, bDone, routed, ledgerTrail(events))
	}
	for _, event := range events {
		if event.Type == formations.RunEventBlocked || event.Type == formations.RunEventResumed {
			t.Fatalf("the verdict blocked or resumed the run: %s", ledgerTrail(events))
		}
	}
}

// Two gates on separate branches wait at once while a third branch works, and
// each is answered, in either order, while that branch still works.
func TestTwoGatesWaitAtOnceAndAreAnsweredInEitherOrder(t *testing.T) {
	for _, order := range [][]string{{"gate_one", "gate_two"}, {"gate_two", "gate_one"}} {
		t.Run(strings.Join(order, " then "), func(t *testing.T) {
			executor := newHoldingExecutor("fmn_c")
			c := openGateLab(t, twoGatesBesideABranch(), executor)
			id := startProof(t, c)
			executor.awaitStarted(t, "fmn_c")
			p := awaitWhileBusy(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 2 })
			if got := waitingGateIDs(p); !slices.Equal(got, []string{"gate_one", "gate_two"}) || p.Status != "waiting_human" {
				t.Fatalf("waiting gates %v, status %s", got, p.Status)
			}
			for i, gateID := range order {
				postVerdict(t, c, id, gateID, requestedSeq(t, p, gateID), "pass", "fine")
				left := awaitWhileBusy(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1-i })
				if i == 0 && (left.Status != "waiting_human" || waitingGateIDs(left)[0] != order[1]) {
					t.Fatalf("after %s: %+v", gateID, left)
				}
			}
			executor.release("fmn_c")
			awaitState(t, c, id, "succeeded")
			if got := formationOrder(eventsOf(t, c, id), formations.RunEventNodeOutput); !slices.Equal(got, []string{"fmn_a", "fmn_b", "fmn_c"}) {
				t.Fatalf("outputs = %v", got)
			}
		})
	}
}

// A send-back the operator gives while its target is still working its first
// attempt waits for that attempt's output, then runs the target again with
// the operator's feedback.
func TestASendBackIntoAWorkingStepRunsItAgainAfterItsOutput(t *testing.T) {
	executor := newHoldingExecutor("fmn_b")
	c := openGateLab(t, sendBackIntoAWorkingStep(), executor)
	id := startProof(t, c)
	executor.awaitStarted(t, "fmn_b")
	p := awaitWhileBusy(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 })
	postVerdict(t, c, id, "gate_review", p.WaitingGates[0].RequestedSeq, "fail", "B must cite A")
	executor.release("fmn_b")
	awaitState(t, c, id, "succeeded")
	var attempts []formations.FormationExecution
	for _, call := range executor.snapshot() {
		if call.NodeID == "fmn_b" {
			attempts = append(attempts, call)
		}
	}
	if len(attempts) != 2 || attempts[1].Attempt != 2 {
		t.Fatalf("B attempts = %d: %s", len(attempts), ledgerTrail(eventsOf(t, c, id)))
	}
	if feedback := attempts[1].Inputs[0].Feedback; feedback == nil || feedback.Reason != "B must cite A" || feedback.OriginalText != "output from fmn_a" {
		t.Fatalf("B's second attempt feedback = %+v", feedback)
	}
	if feedback := attempts[0].Inputs[0].Feedback; feedback != nil {
		t.Fatalf("B's first attempt saw the send-back: %+v", feedback)
	}
}

// A resume still waits up to five seconds for a run's worker and then answers
// 409, while a verdict never waits for it.
func TestAResumeStillWaitsForTheWorkerWhileAVerdictDoesNot(t *testing.T) {
	executor := newHoldingExecutor("fmn_b")
	c := openGateLab(t, oneGateBesideABranch(), executor)
	id := startProof(t, c)
	executor.awaitStarted(t, "fmn_b")
	awaitWhileBusy(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 })
	began := time.Now()
	if w := post(t, c, "/api/runs/"+id+"/resume", `{"mode":"reattach","reason":"too early"}`); w.Code != 409 || !strings.Contains(w.Body.String(), "coordinator is executing") {
		t.Fatalf("resume while B works: %d %s", w.Code, w.Body.String())
	}
	if waited := time.Since(began); waited < runCommandWait {
		t.Fatalf("resume answered after %v, want the bounded wait", waited)
	}
	executor.release("fmn_b")
	awaitState(t, c, id, "waiting_human")
}

// A gate's ask goes to the notify command while another branch still works,
// rather than when the run's worker settles.
func TestAGateAsksTheNotifyCommandWhileAnotherBranchWorks(t *testing.T) {
	executor := newHoldingExecutor("fmn_b")
	notifier := &recordingNotifier{}
	c, _ := onCallFixture(t, oneGateBesideABranch(), executor, notifier)
	id := startProof(t, c)
	executor.awaitStarted(t, "fmn_b")
	sent := awaitNotifications(t, notifier, 1)
	if sent[0].Kind != formations.NeedsYouKindHumanGate || sent[0].GateID != "gate_review" || sent[0].RunStatus != "waiting_human" {
		t.Fatalf("notification = %+v", sent[0])
	}
	p := awaitWhileBusy(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 })
	postVerdict(t, c, id, "gate_review", p.WaitingGates[0].RequestedSeq, "pass", "")
	executor.release("fmn_b")
	awaitState(t, c, id, "succeeded")
	sent = awaitNotifications(t, notifier, 2)
	if sent[1].Kind != formations.NeedsYouKindFinal {
		t.Fatalf("notifications = %+v", sent)
	}
}

// On a session-channel run a gate's ask reaches the asking formation's kept
// seat while another branch still works, and the seat's relayed verdict is
// accepted then too.
func TestASessionAskReachesItsSeatWhileAnotherBranchWorks(t *testing.T) {
	release := make(chan struct{})
	keeper := &keeperExecutor{holds: map[string]chan struct{}{"fmn_b": release}}
	board := strings.Replace(oneGateBesideABranch(), `goal = "Concurrent gates"`, `goal = "Concurrent gates"
humanChannel = "session"`, 1)
	c, _ := onCallFixture(t, board, keeper, nil)
	id := startProof(t, c)
	p := awaitWhileBusy(t, c, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1
	})
	if asked := p.WaitingGates[0].AskedSeats[0]; asked.NodeID != "fmn_a" || asked.SlotID != "slot_fmn_a" {
		t.Fatalf("asked seat = %+v", asked)
	}
	if pastes, _ := keeper.snapshot(); len(pastes) != 1 || !strings.HasPrefix(pastes[0], "slot_fmn_a ") {
		t.Fatalf("pastes = %v", pastes)
	}
	if outputs := formationOrder(eventsOf(t, c, id), formations.RunEventNodeOutput); !slices.Equal(outputs, []string{"fmn_a"}) {
		t.Fatalf("B finished before the ask: %v", outputs)
	}
	verdict(t, c, id, "gate_review", p.WaitingGates[0].RequestedSeq, true, "slot_fmn_a")
	close(release)
	awaitState(t, c, id, "succeeded")
	want := "created slot_fmn_a, kept_on_call slot_fmn_a, ask gate_review, delivered slot_fmn_a, created slot_fmn_b, ended slot_fmn_b, ended slot_fmn_a run_final, run_succeeded"
	if got := ledgerTrail(eventsOf(t, c, id)); got != want {
		t.Fatalf("trail = %s\nwant    %s", got, want)
	}
}
