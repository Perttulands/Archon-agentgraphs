package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	gone   map[string]bool
	pastes []string
	ended  []string
}

func (k *keeperExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
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
	if k.refuse[seat.SlotID] > 0 {
		k.refuse[seat.SlotID]--
		return errors.New("agent is busy")
	}
	k.pastes = append(k.pastes, seat.SlotID+" "+pointer)
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
	c.EnableNeedsYou(NeedsYouConfig{Notifier: notifier, ServerURL: "http://127.0.0.1:18400", RetryInterval: time.Hour, SessionRetryInterval: 20 * time.Millisecond, SessionProbeInterval: 50 * time.Millisecond})
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
		fmt.Sprintf("archon --server http://127.0.0.1:18400 gate approve %s gate_review --requested-seq %d --relayed-by slot_work --response RESPONSE", id, gate.RequestedSeq),
		fmt.Sprintf("archon --server http://127.0.0.1:18400 gate reject %s gate_review --requested-seq %d --relayed-by slot_work --response RESPONSE", id, gate.RequestedSeq),
		"Only the operator decides",
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
