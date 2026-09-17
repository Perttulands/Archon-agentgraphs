package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// keeperExecutor finishes every formation at once, records seats the way the
// tmux executor does and keeps them as a SeatKeeper without tmux.
type keeperExecutor struct {
	store *formations.Store
	mu    sync.Mutex
	// refuse makes a slot's next pastes fail, as for a busy agent.
	refuse map[string]int
	// uncertain fails after a paste may have changed the seat's input.
	uncertain map[string]bool
	// retained models the input left after an uncertain paste, by seat identity.
	retained map[int]string
	askCalls []int
	gone     map[string]bool
	// fail makes a node's next executions fail before any seat is created.
	fail   map[string]int
	pastes []string
	ended  []string
}

func (k *keeperExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	k.mu.Lock()
	failing := k.fail[req.NodeID] > 0
	if failing {
		k.fail[req.NodeID]--
	}
	k.mu.Unlock()
	if failing {
		return formations.FormationExecutionResult{}, errors.New("formation crashed")
	}
	for _, slot := range req.Formation.Slots {
		if err := k.store.AppendRunEvent(req.RunID, formations.RunEvent{Type: formations.RunEventSeatCreated, NodeID: req.NodeID, SlotID: slot.ID, Data: map[string]any{
			"sessionName": "form-" + slot.ID, "sessionId": fmt.Sprintf("$%s-%d", slot.ID, req.Attempt), "paneId": fmt.Sprintf("%%%s-%d", slot.ID, req.Attempt), "harness": "claude-code",
		}}); err != nil {
			return formations.FormationExecutionResult{}, err
		}
	}
	for _, slot := range req.Formation.Slots {
		outcome := formations.SeatOutcomeEnded
		if req.KeepSeatsOnCall && (req.Formation.Type != formations.FormationTypeOrchestrated || slot.Controller) {
			outcome = formations.SeatOutcomeKeptOnCall
		}
		if err := k.store.AppendRunEvent(req.RunID, formations.RunEvent{Type: formations.RunEventSeatCleanup, NodeID: req.NodeID, SlotID: slot.ID, Data: map[string]any{"sessionName": "form-" + slot.ID, "outcome": outcome}}); err != nil {
			return formations.FormationExecutionResult{}, err
		}
	}
	outputs := map[string]formations.FormationOutputPayload{}
	for _, port := range req.Formation.Outputs {
		outputs[port.ID] = formations.FormationOutputPayload{Text: "work from " + req.NodeID}
	}
	return formations.FormationExecutionResult{Status: "done", Text: "done", Outputs: outputs}, nil
}

func (k *keeperExecutor) EndKeptSeat(_ context.Context, seat formations.KeptSeat) (string, string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.gone[seat.SessionID] {
		return formations.SeatOutcomeGone, ""
	}
	k.ended = append(k.ended, seat.SessionID)
	return formations.SeatOutcomeEnded, ""
}

func (k *keeperExecutor) ProbeKeptSeat(_ context.Context, seat formations.KeptSeat) (bool, string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.gone[seat.SessionID] {
		return false, formations.SeatOutcomeGone
	}
	return true, ""
}

func (k *keeperExecutor) PasteAsk(_ context.Context, seat formations.KeptSeat, pointer string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.askCalls = append(k.askCalls, seat.CreatedSeq)
	if k.retained[seat.CreatedSeq] != "" {
		return errors.New("input still holds the unsent ask pointer")
	}
	if k.refuse[seat.SlotID] > 0 {
		k.refuse[seat.SlotID]--
		return errors.New("agent is busy")
	}
	k.pastes = append(k.pastes, seat.SlotID+" "+pointer)
	if k.uncertain[seat.SlotID] {
		if k.retained == nil {
			k.retained = map[int]string{}
		}
		k.retained[seat.CreatedSeq] = pointer
		return fmt.Errorf("%w: Enter failed", formations.ErrHumanAskDeliveryUncertain)
	}
	return nil
}

func (k *keeperExecutor) snapshot() ([]string, []string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]string(nil), k.pastes...), append([]string(nil), k.ended...)
}

// onCallFixture opens a coordinator on board with executor, which may or may
// not keep seats, and enables delivery with the given notifier.
func onCallFixture(t *testing.T, board string, executor formations.FormationExecutor, notifier formations.NeedsYouNotifier) (*Coordinator, string) {
	t.Helper()
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
		if keeper, ok := executor.(*keeperExecutor); ok {
			keeper.store = store
		}
		return executor
	})
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
	c.EnableNeedsYou(NeedsYouConfig{Notifier: notifier, ServerURL: "http://127.0.0.1:18400", CLI: "/opt/archon tools/bin/archon", RetryInterval: time.Hour, SessionRetryInterval: 20 * time.Millisecond, SessionProbeInterval: 50 * time.Millisecond})
	return c, root
}

func sessionBoard(board string) string {
	return strings.Replace(board, `beadId = "form-2fb"`, `beadId = "form-2fb"
humanChannel = "session"`, 1)
}

func startProof(t *testing.T, c *Coordinator) string {
	t.Helper()
	w := post(t, c, "/api/formations/runs", `{"cwd":`+strconv.Quote(c.store.Workspace)+`,"brief":"run the proof","board":"proof","missionId":"mis_proof","expectedRev":1,"limits":{"maxDispatch":10,"maxAttempts":3,"wallClockSeconds":600,"redact":false}}`)
	if w.Code != 202 {
		t.Fatalf("start %d %s", w.Code, w.Body.String())
	}
	var receipt struct {
		Data struct {
			RunID string `json:"runId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt.Data.RunID
}

// awaitProjection waits until the run's projection satisfies ok.
func awaitProjection(t *testing.T, c *Coordinator, id string, ok func(*Projection) bool) *Projection {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		p, err := c.Project(id)
		if err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		busy := c.state(id).busy
		c.mu.Unlock()
		if !busy && ok(p) {
			return p
		}
		if time.Now().After(deadline) {
			raw, _ := json.Marshal(p)
			t.Fatalf("projection never matched: %s", raw)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func eventsOf(t *testing.T, c *Coordinator, id string) []formations.RunEvent {
	t.Helper()
	events, err := c.store.ReadRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// ledgerTrail lists seat, ask and run lifecycle events in order.
func ledgerTrail(events []formations.RunEvent) string {
	var trail []string
	for _, event := range events {
		switch event.Type {
		case formations.RunEventSeatCreated:
			trail = append(trail, "created "+event.SlotID)
		case formations.RunEventSeatCleanup:
			entry := fmt.Sprint(event.Data["outcome"]) + " " + event.SlotID
			if cause, ok := event.Data["cause"].(string); ok {
				entry += " " + cause
			}
			trail = append(trail, entry)
		case formations.RunEventHumanInputRequested:
			trail = append(trail, "ask "+event.GateID)
		case formations.RunEventHumanAskDelivered:
			trail = append(trail, "delivered "+event.SlotID)
		case formations.RunEventHumanAskFallback:
			trail = append(trail, "fallback "+fmt.Sprint(event.Data["code"]))
		case formations.RunEventSucceeded, formations.RunEventFailed, formations.RunEventCanceled, formations.RunEventBlocked, formations.RunEventResumed:
			trail = append(trail, event.Type)
		}
	}
	return strings.Join(trail, ", ")
}

func verdict(t *testing.T, c *Coordinator, id, gate string, seq int, pass bool, relayedBy string) {
	t.Helper()
	decision := "fail"
	if pass {
		decision = "pass"
	}
	body := `{"requestedSeq":` + strconv.Itoa(seq) + `,"verdict":"` + decision + `","reason":"confirmed with the operator","relayedBy":"` + relayedBy + `"}`
	if w := post(t, c, "/api/formations/runs/"+id+"/gates/"+gate+"/verdict", body); w.Code != 202 {
		t.Fatalf("verdict %d %s", w.Code, w.Body.String())
	}
}

func TestSessionAskReachesTheKeptWorkSeatWithoutANotifyCommand(t *testing.T) {
	keeper := &keeperExecutor{}
	c, root := onCallFixture(t, sessionBoard(testBoard), keeper, nil)
	id := startProof(t, c)
	p := awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1 })
	gate := p.WaitingGates[0]
	if p.HumanChannel != "session" || gate.AskedSeats[0].SlotID != "slot_work" || gate.AskedSeats[0].NodeID != "fmn_work" || gate.FallbackReason != "" {
		t.Fatalf("projection = %+v", p)
	}
	if len(p.OnCallSeats) != 1 || p.OnCallSeats[0].SlotID != "slot_work" || len(p.OnCallSeats[0].WaitingOn) != 1 || p.OnCallSeats[0].WaitingOn[0].RequestedSeq != gate.RequestedSeq {
		t.Fatalf("on-call seats = %+v", p.OnCallSeats)
	}
	var delivered *Event
	for i, event := range p.Events {
		if event.Type == formations.RunEventHumanAskDelivered {
			delivered = &p.Events[i]
		}
	}
	if delivered == nil || delivered.RequestedSeq != gate.RequestedSeq || delivered.SlotID != "slot_work" || delivered.GateID != "gate_review" {
		t.Fatalf("delivered event = %+v", delivered)
	}

	pastes, _ := keeper.snapshot()
	brief := filepath.Join(root, "briefs", fmt.Sprintf("gate-%s-%d-slot_work.md", id, gate.RequestedSeq))
	if len(pastes) != 1 || !strings.HasPrefix(pastes[0], "slot_work Read the file "+brief+" and follow it") {
		t.Fatalf("pastes = %v", pastes)
	}
	raw, err := os.ReadFile(brief)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Human gate: Review",
		"pending request: " + strconv.Itoa(gate.RequestedSeq),
		"asking formation: Work (fmn_work)",
		"PRIVATE-CRITERION",
		fmt.Sprintf("'/opt/archon tools/bin/archon' --server http://127.0.0.1:18400 gate approve %s gate_review --requested-seq %d --relayed-by slot_work --response RESPONSE", id, gate.RequestedSeq),
		fmt.Sprintf("'/opt/archon tools/bin/archon' --server http://127.0.0.1:18400 gate reject %s gate_review --requested-seq %d --relayed-by slot_work --response RESPONSE", id, gate.RequestedSeq),
		"Only the operator decides",
		"A complete, unambiguous operator verdict for this pending gate, with the exact response to record, is itself confirmation.",
		"Record those exact words immediately, without asking them to confirm again.",
		"'Approve. Response: Keep the scope as written.'",
		"'Send back. Response: Add the missing constraints.'",
		"If you draft or paraphrase any response, or the verdict, response, or intended gate is ambiguous, show the proposed verdict and exact response together and wait for the operator's confirmation before recording.",
		"Do not infer a verdict from discussion or invent missing response text.",
		"A 409 saying the coordinator is executing means the run is busy for a moment: wait a few seconds and run the same command again.",
		"A 409 saying the human gate request is no longer pending means another seat or the cockpit decided first",
	} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("brief lacks %q:\n%s", want, raw)
		}
	}
	seats := httptest.NewRecorder()
	c.Handler().ServeHTTP(seats, httptest.NewRequest("GET", "/api/formations/runs/"+id+"/seats", nil))
	if !strings.Contains(seats.Body.String(), `"onCall":{"keptSeq":`) || !strings.Contains(seats.Body.String(), `"waitingOn":[{"gateId":"gate_review","requestedSeq":`+strconv.Itoa(gate.RequestedSeq)+`}]`) {
		t.Fatalf("seats = %s", seats.Body.String())
	}

	verdict(t, c, id, "gate_review", gate.RequestedSeq, true, "slot_work")
	awaitState(t, c, id, "succeeded")
	want := "created slot_work, kept_on_call slot_work, ask gate_review, delivered slot_work, run_blocked, run_resumed, ended slot_work ask_answered, created slot_after, ended slot_after, run_succeeded"
	if got := ledgerTrail(eventsOf(t, c, id)); got != want {
		t.Fatalf("trail = %s\nwant    %s", got, want)
	}
	if _, ended := keeper.snapshot(); fmt.Sprint(ended) != "[$slot_work-1]" {
		t.Fatalf("ended = %v", ended)
	}
}

const peerProofBoard = `schema = 1
id = "brd_proof"
slug = "proof"
title = "Proof"
rev = 1
[[mission]]
id = "mis_proof"
title = "Proof"
goal = "PRIVATE-OBJECTIVE"
beadId = "form-2fb"
humanChannel = "session"
[[formation]]
id = "fmn_peers"
type = "peer"
title = "Peers"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "peer_a"
label = "Peer A"
agentId = "codex-builder"
harness = "openai-codex"
[[formation.slot]]
id = "peer_b"
label = "Peer B"
agentId = "codex-builder"
harness = "openai-codex"
[[formation]]
id = "fmn_team"
type = "orchestrated"
title = "Team"
[[formation.input]]
id = "port_team_in"
label = "Input"
[[formation.output]]
id = "port_team_out"
label = "Output"
[[formation.slot]]
id = "team_lead"
label = "Lead"
agentId = "codex-builder"
harness = "openai-codex"
controller = true
[[formation.slot]]
id = "team_worker"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
[[gate]]
id = "gate_peers"
title = "Peer questions"
kinds = ["human"]
criterion = "Answer the peers"
[[gate]]
id = "gate_team"
title = "Team sign-off"
kinds = ["human"]
criterion = "Sign off the team"
[[connection]]
id = "edge_peers"
from = "mis_proof:out"
to = "fmn_peers:port_in"
[[connection]]
id = "edge_team"
from = "gate_peers:pass"
to = "fmn_team:port_team_in"
[[connection]]
id = "edge_peers_gate"
from = "fmn_peers:port_out"
to = "gate_peers:in"
[[connection]]
id = "edge_team_gate"
from = "fmn_team:port_team_out"
to = "gate_team:in"
`

func TestSessionAskReachesEveryPeerSeatWithARetryAndOnlyTheOrchestratedController(t *testing.T) {
	keeper := &keeperExecutor{refuse: map[string]int{"peer_b": 2}}
	c, root := onCallFixture(t, peerProofBoard, keeper, nil)
	id := startProof(t, c)
	// askedAs waits until gate's ask reached want seats and checks each brief
	// relays the verdict as its own slot.
	askedAs := func(gateID string, want int) GateRequest {
		t.Helper()
		p := awaitProjection(t, c, id, func(p *Projection) bool {
			return len(p.WaitingGates) == 1 && p.WaitingGates[0].GateID == gateID && len(p.WaitingGates[0].AskedSeats) == want
		})
		gate := p.WaitingGates[0]
		for _, seat := range gate.AskedSeats {
			raw, err := os.ReadFile(filepath.Join(root, "briefs", fmt.Sprintf("gate-%s-%d-%s.md", id, gate.RequestedSeq, seat.SlotID)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(raw), "--relayed-by "+seat.SlotID+" --response") || strings.Count(string(raw), "--relayed-by") != 2 {
				t.Fatalf("%s brief does not relay as its own slot:\n%s", seat.SlotID, raw)
			}
		}
		return gate
	}
	peers := askedAs("gate_peers", 2)
	if slots := peers.AskedSeats[0].SlotID + " " + peers.AskedSeats[1].SlotID; slots != "peer_a peer_b" || peers.AskedSeats[1].DeliveredSeq <= peers.AskedSeats[0].DeliveredSeq {
		t.Fatalf("peer ask reached %s: %+v", slots, peers.AskedSeats)
	}
	if pastes, _ := keeper.snapshot(); len(pastes) != 2 || keeper.refuse["peer_b"] != 0 {
		t.Fatalf("pastes = %v, refusals left %d", pastes, keeper.refuse["peer_b"])
	}
	verdict(t, c, id, "gate_peers", peers.RequestedSeq, true, "peer_b")
	team := askedAs("gate_team", 1)
	if team.AskedSeats[0].SlotID != "team_lead" {
		t.Fatalf("team ask reached %+v", team.AskedSeats)
	}
	trail := ledgerTrail(eventsOf(t, c, id))
	want := "created peer_a, created peer_b, kept_on_call peer_a, kept_on_call peer_b, ask gate_peers, delivered peer_a, delivered peer_b, run_blocked, run_resumed, ended peer_a ask_answered, ended peer_b ask_answered, created team_lead, created team_worker, kept_on_call team_lead, ended team_worker, ask gate_team, delivered team_lead"
	if trail != want {
		t.Fatalf("trail = %s\nwant    %s", trail, want)
	}
}

func TestUncertainSessionPasteFallsBackOnceAndSurvivesRestart(t *testing.T) {
	for _, test := range []struct {
		name, board, slot string
	}{
		{"solo", sessionBoard(testBoard), "slot_work"},
		{"peer", peerProofBoard, "peer_a"},
	} {
		t.Run(test.name, func(t *testing.T) {
			notifier := &recordingNotifier{}
			keeper := &keeperExecutor{uncertain: map[string]bool{test.slot: true}}
			c, root := onCallFixture(t, test.board, keeper, notifier)
			id := startProof(t, c)
			p := awaitProjection(t, c, id, func(p *Projection) bool {
				return len(p.WaitingGates) == 1 && p.WaitingGates[0].FallbackReason != ""
			})
			gate := p.WaitingGates[0]
			if gate.FallbackReason != formations.AskFallbackReason(formations.AskFallbackDeliveryUncertain) || len(gate.AskedSeats) != 0 {
				t.Fatalf("waiting gate = %+v", gate)
			}
			awaitNotifications(t, notifier, 1)
			next := reopen(t, c, root, keeper)
			t.Cleanup(func() { next.Close() })
			// Reconcile synchronously, including repeated settle/probe work. A
			// new dispatcher has no in-memory knowledge of the failed paste.
			d := &needsYouDispatcher{c: next, config: NeedsYouConfig{Notifier: notifier}, watching: map[string]bool{}}
			for range 3 {
				d.deliver(context.Background(), id, true)
			}
			if pastes, _ := keeper.snapshot(); len(pastes) != 1 || !strings.HasPrefix(pastes[0], test.slot+" ") {
				t.Fatalf("failed ask was pasted again: %v", pastes)
			}
			if sent, _ := notifier.snapshot(); len(sent) != 1 || sent[0].Kind != formations.NeedsYouKindHumanGate || sent[0].Seq != gate.RequestedSeq {
				t.Fatalf("notifications = %+v", sent)
			}
			trail := ledgerTrail(eventsOf(t, next, id))
			if strings.Count(trail, "fallback delivery_uncertain") != 1 || strings.Contains(trail, "delivered ") {
				t.Fatalf("trail = %s", trail)
			}
		})
	}
}

func TestSessionAskFallsBackOnceWithAReason(t *testing.T) {
	t.Run("lab executor", func(t *testing.T) {
		notifier := &recordingNotifier{}
		c, _ := onCallFixture(t, sessionBoard(testBoard), &instantExecutor{}, notifier)
		id := startProof(t, c)
		p := awaitProjection(t, c, id, func(p *Projection) bool {
			return len(p.WaitingGates) == 1 && p.WaitingGates[0].FallbackReason != ""
		})
		if p.WaitingGates[0].FallbackReason != "the lab executor keeps no seats" {
			t.Fatalf("fallback = %+v", p.WaitingGates[0])
		}
		awaitNotifications(t, notifier, 1)
		settleQuietly()
		if sent, _ := notifier.snapshot(); len(sent) != 1 || sent[0].Kind != formations.NeedsYouKindHumanGate {
			t.Fatalf("notifications = %+v", sent)
		}
		if got := ledgerTrail(eventsOf(t, c, id)); strings.Count(got, "fallback lab_executor") != 1 {
			t.Fatalf("trail = %s", got)
		}
	})
	t.Run("no receivable seat, no notify command", func(t *testing.T) {
		keeper := &keeperExecutor{gone: map[string]bool{"$slot_work-1": true}}
		c, _ := onCallFixture(t, sessionBoard(testBoard), keeper, nil)
		id := startProof(t, c)
		p := awaitProjection(t, c, id, func(p *Projection) bool {
			return len(p.WaitingGates) == 1 && p.WaitingGates[0].FallbackReason != ""
		})
		if p.WaitingGates[0].FallbackReason != "no kept seat of the asking formation can receive the ask" || len(p.OnCallSeats) != 0 {
			t.Fatalf("projection = %+v", p)
		}
		settleQuietly()
		if got := ledgerTrail(eventsOf(t, c, id)); got != "created slot_work, kept_on_call slot_work, ask gate_review, gone slot_work, fallback no_receivable_seat" {
			t.Fatalf("trail = %s", got)
		}
	})
	t.Run("every asked seat gone while the request waits", func(t *testing.T) {
		notifier := &recordingNotifier{}
		keeper := &keeperExecutor{}
		c, _ := onCallFixture(t, sessionBoard(testBoard), keeper, notifier)
		id := startProof(t, c)
		awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1 })
		settleQuietly()
		if sent, _ := notifier.snapshot(); len(sent) != 0 {
			t.Fatalf("a delivered session ask was notified: %+v", sent)
		}
		keeper.mu.Lock()
		keeper.gone = map[string]bool{"$slot_work-1": true}
		keeper.mu.Unlock()
		p := awaitProjection(t, c, id, func(p *Projection) bool {
			return len(p.WaitingGates) == 1 && p.WaitingGates[0].FallbackReason != ""
		})
		if p.WaitingGates[0].FallbackReason != "every seat that received the ask is gone" || len(p.WaitingGates[0].AskedSeats) != 1 {
			t.Fatalf("projection = %+v", p.WaitingGates)
		}
		awaitNotifications(t, notifier, 1)
		time.Sleep(200 * time.Millisecond) // several probes
		if sent, _ := notifier.snapshot(); len(sent) != 1 {
			t.Fatalf("notifications = %d, want exactly one", len(sent))
		}
		if got := ledgerTrail(eventsOf(t, c, id)); strings.Count(got, "fallback") != 1 || !strings.HasSuffix(got, "delivered slot_work, gone slot_work, fallback asked_seats_gone") {
			t.Fatalf("trail = %s", got)
		}
	})
}

// instantExecutor finishes formations at once and keeps no seats, like lab.
type instantExecutor struct{}

func (instantExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	outputs := map[string]formations.FormationOutputPayload{}
	for _, port := range req.Formation.Outputs {
		outputs[port.ID] = formations.FormationOutputPayload{Text: "work"}
	}
	return formations.FormationExecutionResult{Status: "done", Text: "done", Outputs: outputs}, nil
}

const twoGateBoard = `schema = 1
id = "brd_proof"
slug = "proof"
title = "Proof"
rev = 1
[[mission]]
id = "mis_proof"
title = "Proof"
goal = "PRIVATE-OBJECTIVE"
beadId = "form-2fb"
humanChannel = "session"
[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
controller = true
[[gate]]
id = "gate_one"
title = "First look"
kinds = ["human"]
criterion = "First"
[[gate]]
id = "gate_two"
title = "Second look"
kinds = ["human"]
criterion = "Second"
[[connection]]
id = "edge_start"
from = "mis_proof:out"
to = "fmn_work:port_in"
[[connection]]
id = "edge_one"
from = "fmn_work:port_out"
to = "gate_one:in"
[[connection]]
id = "edge_two"
from = "gate_one:pass"
to = "gate_two:in"
`

func TestSessionSeatStaysForTheNextHumanGateAndEndsBeforeTheRunSucceeds(t *testing.T) {
	keeper := &keeperExecutor{}
	c, _ := onCallFixture(t, twoGateBoard, keeper, nil)
	id := startProof(t, c)
	first := awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1 })
	verdict(t, c, id, "gate_one", first.WaitingGates[0].RequestedSeq, true, "slot_work")
	second := awaitProjection(t, c, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && p.WaitingGates[0].GateID == "gate_two" && len(p.WaitingGates[0].AskedSeats) == 1
	})
	if second.OnCallSeats[0].CreatedSeq != first.OnCallSeats[0].CreatedSeq {
		t.Fatalf("the second gate reached another seat: %+v then %+v", first.OnCallSeats, second.OnCallSeats)
	}
	verdict(t, c, id, "gate_two", second.WaitingGates[0].RequestedSeq, true, "slot_work")
	awaitState(t, c, id, "succeeded")
	want := "created slot_work, kept_on_call slot_work, ask gate_one, delivered slot_work, run_blocked, run_resumed, ask gate_two, delivered slot_work, run_blocked, run_resumed, ended slot_work run_final, run_succeeded"
	if got := ledgerTrail(eventsOf(t, c, id)); got != want {
		t.Fatalf("trail = %s\nwant    %s", got, want)
	}
}

func TestUncertainSeatFallsBackForTheNextHumanGateAfterRestart(t *testing.T) {
	keeper := &keeperExecutor{uncertain: map[string]bool{"slot_work": true}}
	notifier := &recordingNotifier{}
	c, root := onCallFixture(t, twoGateBoard, keeper, notifier)
	id := startProof(t, c)
	first := awaitProjection(t, c, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && p.WaitingGates[0].FallbackReason != ""
	})
	awaitNotifications(t, notifier, 1)
	seat := first.OnCallSeats[0]
	next := reopen(t, c, root, keeper)
	t.Cleanup(func() { next.Close() })
	verdict(t, next, id, "gate_one", first.WaitingGates[0].RequestedSeq, true, "")
	second := awaitProjection(t, next, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && p.WaitingGates[0].GateID == "gate_two"
	})
	d := &needsYouDispatcher{c: next, config: NeedsYouConfig{Notifier: notifier}, watching: map[string]bool{}}
	for range 3 {
		if retry := d.deliverSession(context.Background(), id); retry {
			t.Fatal("the next ask retries against known retained input")
		}
		d.deliver(context.Background(), id, true)
	}
	p, err := next.Project(id)
	if err != nil {
		t.Fatal(err)
	}
	if gate := p.WaitingGates[0]; gate.FallbackReason != formations.AskFallbackReason(formations.AskFallbackDeliveryUncertain) || len(gate.AskedSeats) != 0 {
		t.Fatalf("second gate = %+v", gate)
	}
	keeper.mu.Lock()
	calls := append([]int(nil), keeper.askCalls...)
	retained := keeper.retained[seat.CreatedSeq]
	keeper.mu.Unlock()
	if len(calls) != 1 || retained == "" {
		t.Fatalf("the retained input was touched: calls %v, input %q", calls, retained)
	}
	events := eventsOf(t, next, id)
	for _, request := range []int{first.WaitingGates[0].RequestedSeq, second.WaitingGates[0].RequestedSeq} {
		fallback := formations.HumanAskRecords(events)[request].Fallback
		if fallback == nil || fmt.Sprint(fallback.Data["seatCreatedSeq"]) != strconv.Itoa(seat.CreatedSeq) {
			t.Fatalf("fallback omitted immutable seat identity: %+v", fallback)
		}
	}
	if trail := ledgerTrail(events); strings.Count(trail, "fallback delivery_uncertain") != 2 {
		t.Fatalf("trail = %s", trail)
	}
	if sent, _ := notifier.snapshot(); len(sent) != 2 || sent[1].Seq != second.WaitingGates[0].RequestedSeq {
		t.Fatalf("notifications = %+v", sent)
	}
}

func TestHealthyPeerReceivesTheNextHumanGateAfterUncertainPaste(t *testing.T) {
	board := strings.Replace(twoGateBoard, `type = "solo"`, `type = "peer"`, 1)
	board = strings.Replace(board, `controller = true`, `[[formation.slot]]
id = "slot_other"
label = "Other peer"
agentId = "codex-builder"
harness = "openai-codex"`, 1)
	keeper := &keeperExecutor{uncertain: map[string]bool{"slot_work": true}}
	c, root := onCallFixture(t, board, keeper, nil)
	id := startProof(t, c)
	first := awaitProjection(t, c, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && p.WaitingGates[0].FallbackReason != ""
	})
	next := reopen(t, c, root, keeper)
	t.Cleanup(func() { next.Close() })
	verdict(t, next, id, "gate_one", first.WaitingGates[0].RequestedSeq, true, "")
	awaitProjection(t, next, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && p.WaitingGates[0].GateID == "gate_two"
	})
	d := &needsYouDispatcher{c: next, watching: map[string]bool{}}
	if retry := d.deliverSession(context.Background(), id); retry {
		t.Fatal("healthy peer delivery should settle")
	}
	p, err := next.Project(id)
	if err != nil {
		t.Fatal(err)
	}
	gate := p.WaitingGates[0]
	if gate.FallbackReason != "" || len(gate.AskedSeats) != 1 || gate.AskedSeats[0].SlotID != "slot_other" {
		t.Fatalf("next gate = %+v", gate)
	}
	if pastes, _ := keeper.snapshot(); len(pastes) != 2 || !strings.HasPrefix(pastes[1], "slot_other ") {
		t.Fatalf("only the healthy peer should receive the next ask: %v", pastes)
	}
}

func TestPendingUncertainSeatIsRecordedWhenVerdictAdvancedItsAsk(t *testing.T) {
	// The dispatcher can lose the run reservation to an operator verdict
	// between PasteAsk returning an uncertain error and recording its fallback.
	keeper := &keeperExecutor{refuse: map[string]int{"slot_work": 1000}}
	c, root := onCallFixture(t, twoGateBoard, keeper, nil)
	id := startProof(t, c)
	first := awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 })
	next := reopen(t, c, root, keeper)
	t.Cleanup(func() { next.Close() })
	plan, err := next.engine.PlanRunOnCall(context.Background(), id)
	if err != nil || len(plan.Deliveries) != 1 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	delivery := plan.Deliveries[0]
	verdict(t, next, id, "gate_one", first.WaitingGates[0].RequestedSeq, true, "")
	awaitProjection(t, next, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && p.WaitingGates[0].GateID == "gate_two"
	})
	d := &needsYouDispatcher{c: next, watching: map[string]bool{}, askFailures: map[humanAskKey]formations.HumanAskFallback{
		{runID: id, seq: delivery.Request.Seq}: {Request: delivery.Request, Code: formations.AskFallbackDeliveryUncertain, Seat: delivery.Seat},
	}}
	if retry := d.deliverSession(context.Background(), id); retry || len(d.askFailures) != 0 {
		t.Fatalf("pending seat identity was not recorded before replanning: retry %v, pending %+v", retry, d.askFailures)
	}
	events := eventsOf(t, next, id)
	if !formations.HumanAskUncertainSeats(events)[delivery.Seat.CreatedSeq] || strings.Count(ledgerTrail(events), "fallback delivery_uncertain") != 2 {
		t.Fatalf("verdict lost the unsafe seat identity: %s", ledgerTrail(events))
	}
	// A delivery from a stale plan must also stop before touching this seat.
	if d.deliverAsk(context.Background(), id, plan, formations.HumanAskDelivery{Request: formations.RunEvent{Seq: len(events) + 1}, Seat: delivery.Seat}) {
		t.Fatal("a stale delivery plan must request replanning")
	}
}

func TestKeptSeatsEndBeforeAWaitingRunIsCanceledOrFails(t *testing.T) {
	t.Run("abort", func(t *testing.T) {
		keeper := &keeperExecutor{}
		c, _ := onCallFixture(t, sessionBoard(testBoard), keeper, nil)
		id := startProof(t, c)
		awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1 })
		if w := post(t, c, "/api/formations/runs/"+id+"/abort", `{"reason":"operator stop","requestedBy":"operator"}`); w.Code != 200 {
			t.Fatalf("abort %d %s", w.Code, w.Body.String())
		}
		if got := ledgerTrail(eventsOf(t, c, id)); !strings.HasSuffix(got, "delivered slot_work, ended slot_work run_final, run_canceled") {
			t.Fatalf("trail = %s", got)
		}
	})
	t.Run("failure", func(t *testing.T) {
		keeper := &keeperExecutor{}
		c, _ := onCallFixture(t, sessionBoard(testBoard), keeper, nil)
		id := startProof(t, c)
		awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1 })
		c.recordFailure(id, errors.New("coordinator lost its executor"))
		if got := ledgerTrail(eventsOf(t, c, id)); !strings.HasSuffix(got, "delivered slot_work, ended slot_work run_final, run_failed") {
			t.Fatalf("trail = %s", got)
		}
	})
}

func TestKeptSeatsSurviveARestartAndStillEndByTheRules(t *testing.T) {
	// Startup re-verifies kept seats at once; the periodic probe stays out of it.
	restart := NeedsYouConfig{ServerURL: "http://127.0.0.1:18400", RetryInterval: time.Hour, SessionRetryInterval: 20 * time.Millisecond, SessionProbeInterval: time.Hour}
	t.Run("seat still there", func(t *testing.T) {
		keeper := &keeperExecutor{}
		c, root := onCallFixture(t, sessionBoard(testBoard), keeper, nil)
		id := startProof(t, c)
		p := awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1 })
		next := reopen(t, c, root, keeper)
		t.Cleanup(func() { next.Close() })
		if _, ended := keeper.snapshot(); len(ended) != 0 {
			t.Fatalf("shutdown ended kept seats: %v", ended)
		}
		next.EnableNeedsYou(restart)
		settleQuietly()
		if pastes, _ := keeper.snapshot(); len(pastes) != 1 {
			t.Fatalf("restart delivered the ask again: %v", pastes)
		}
		verdict(t, next, id, "gate_review", p.WaitingGates[0].RequestedSeq, true, "slot_work")
		awaitState(t, next, id, "succeeded")
		if _, ended := keeper.snapshot(); fmt.Sprint(ended) != "[$slot_work-1]" {
			t.Fatalf("ended = %v", ended)
		}
		if got := ledgerTrail(eventsOf(t, next, id)); !strings.HasSuffix(got, "ended slot_work ask_answered, created slot_after, ended slot_after, run_succeeded") {
			t.Fatalf("trail = %s", got)
		}
	})
	t.Run("seat gone while the daemon was down", func(t *testing.T) {
		keeper := &keeperExecutor{}
		c, root := onCallFixture(t, sessionBoard(testBoard), keeper, nil)
		id := startProof(t, c)
		awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1 })
		next := reopen(t, c, root, keeper)
		t.Cleanup(func() { next.Close() })
		keeper.mu.Lock()
		keeper.gone = map[string]bool{"$slot_work-1": true}
		keeper.mu.Unlock()
		next.EnableNeedsYou(restart)
		p := awaitProjection(t, next, id, func(p *Projection) bool {
			return len(p.WaitingGates) == 1 && p.WaitingGates[0].FallbackReason != ""
		})
		if len(p.OnCallSeats) != 0 || !strings.HasSuffix(ledgerTrail(eventsOf(t, next, id)), "delivered slot_work, gone slot_work, fallback asked_seats_gone") {
			t.Fatalf("projection %+v, trail %s", p, ledgerTrail(eventsOf(t, next, id)))
		}
	})
}

func TestACrashBeforeABlockedRunIsCanceledLeavesItBlockedThroughRestart(t *testing.T) {
	// The ask never reaches the seat, so the seat is still on call when the
	// next formation fails and blocks the run.
	keeper := &keeperExecutor{refuse: map[string]int{"slot_work": 1 << 30}, fail: map[string]int{"fmn_after": 1}}
	notifier := &recordingNotifier{}
	c, root := onCallFixture(t, sessionBoard(testBoard), keeper, notifier)
	id := startProof(t, c)
	p := awaitProjection(t, c, id, func(p *Projection) bool { return len(p.WaitingGates) == 1 && len(p.OnCallSeats) == 1 })
	verdict(t, c, id, "gate_review", p.WaitingGates[0].RequestedSeq, true, "slot_work")
	blocked := awaitState(t, c, id, "blocked")
	sent := awaitNotifications(t, notifier, 1)
	if len(blocked.OnCallSeats) != 1 || sent[0].Kind != formations.NeedsYouKindBlocked {
		t.Fatalf("blocked projection %+v, notifications %+v", blocked, sent)
	}
	before := eventsOf(t, c, id)
	asks, err := formations.ProjectSettledNeedsYouAsks(before)
	if err != nil || len(asks) != 1 || asks[0].Seq != before[len(before)-1].Seq || sent[0].Seq != asks[0].Seq {
		t.Fatalf("asks %+v (%v), sent %+v", asks, err, sent)
	}

	// Abort ends the kept seat, and the daemon dies before run_canceled.
	if err := c.engine.EndKeptSeatsNow(id, formations.KeptSeats(before), formations.SeatCauseRunFinal, keeper); err != nil {
		t.Fatal(err)
	}
	next := reopen(t, c, root, keeper)
	t.Cleanup(func() { next.Close() })
	crashed := eventsOf(t, next, id)
	if got := ledgerTrail(crashed); !strings.HasSuffix(got, "run_blocked, ended slot_work run_final") {
		t.Fatalf("crash ledger = %s", got)
	}
	restarted := awaitState(t, next, id, "blocked")
	if restarted.ResumeAllowed != blocked.ResumeAllowed || restarted.Final || len(restarted.OnCallSeats) != 0 || len(eventsOf(t, next, id)) != len(crashed) {
		t.Fatalf("after restart %+v, before %+v", restarted, blocked)
	}
	if again, err := formations.ProjectSettledNeedsYouAsks(crashed); err != nil || !reflect.DeepEqual(again, asks) {
		t.Fatalf("asks after the cleanup %+v (%v), before %+v", again, err, asks)
	}
	renotified := &recordingNotifier{}
	next.EnableNeedsYou(NeedsYouConfig{Notifier: renotified, ServerURL: "http://127.0.0.1:18400", RetryInterval: time.Hour, SessionRetryInterval: 20 * time.Millisecond, SessionProbeInterval: 50 * time.Millisecond})
	settleQuietly()
	if again, _ := renotified.snapshot(); len(again) != 0 {
		t.Fatalf("restart announced the block again: %+v", again)
	}

	if w := post(t, next, "/api/formations/runs/"+id+"/abort", `{"reason":"operator stop","requestedBy":"operator"}`); w.Code != 200 {
		t.Fatalf("abort %d %s", w.Code, w.Body.String())
	}
	if got := ledgerTrail(eventsOf(t, next, id)); !strings.HasSuffix(got, "run_blocked, ended slot_work run_final, run_canceled") {
		t.Fatalf("trail = %s", got)
	}
	if _, ended := keeper.snapshot(); fmt.Sprint(ended) != "[$slot_work-1]" {
		t.Fatalf("ended = %v", ended)
	}
}

func TestNotifyChannelAsksStillReachTheNotifyCommand(t *testing.T) {
	notifier := &recordingNotifier{}
	keeper := &keeperExecutor{}
	c, _ := onCallFixture(t, testBoard, keeper, notifier)
	id := startProof(t, c)
	awaitNotifications(t, notifier, 1)
	settleQuietly()
	p, err := c.Project(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.HumanChannel != "notify" || len(p.OnCallSeats) != 0 || len(p.WaitingGates[0].AskedSeats) != 0 {
		t.Fatalf("projection = %+v", p)
	}
	if pastes, _ := keeper.snapshot(); len(pastes) != 0 || !strings.Contains(ledgerTrail(eventsOf(t, c, id)), "created slot_work, ended slot_work, ask gate_review") {
		t.Fatalf("notify run pasted %v, trail %s", pastes, ledgerTrail(eventsOf(t, c, id)))
	}
}
