package formations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// This transport models agents that use the real conversation API during a
// native turn. It exercises wake-up and repeated contributions, rather than
// returning one canned answer per executor call.
type conversingSeats struct {
	store          *Store
	mu             sync.Mutex
	prompts        map[*nativeSeat]string
	created, ended int
	openings       int
	participants   int
	conversationID PeerConversationID
	mode           string
	openingBarrier chan struct{}
	cancel         context.CancelFunc
}

func (f *conversingSeats) Create(_ context.Context, _, name, _, _ string, variant HarnessVariant) (*nativeSeat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created++
	return &nativeSeat{name: name, sessionID: name, paneID: name, variant: variant}, nil
}
func (*conversingSeats) Ready(context.Context, string, *nativeSeat, string) error  { return nil }
func (*conversingSeats) WaitInputClear(context.Context, string, *nativeSeat) error { return nil }
func (*conversingSeats) Snapshot(context.Context, *nativeSeat, string, string) (codexTranscriptTurn, error) {
	return codexTranscriptTurn{}, nil
}
func (f *conversingSeats) End(_ context.Context, _ string, _ *nativeSeat) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ended++
	return nil
}
func (f *conversingSeats) Stage(_ context.Context, _ string, seat *nativeSeat, _, _ string) error {
	raw, err := os.ReadFile(seat.brief)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prompts == nil {
		f.prompts = map[*nativeSeat]string{}
	}
	f.prompts[seat] = string(raw)
	return nil
}
func peerPromptField(prompt, key string) string {
	for _, line := range strings.Split(prompt, "\n") {
		if value, ok := strings.CutPrefix(line, key+": "); ok {
			return value
		}
	}
	return ""
}
func (f *conversingSeats) WaitTurn(ctx context.Context, seat *nativeSeat, _, _ string, consumed func(codexTranscriptTurn) error) (codexTranscriptTurn, error) {
	f.mu.Lock()
	prompt := f.prompts[seat]
	f.mu.Unlock()
	turn := codexTranscriptTurn{Consumed: true, SessionID: "native-" + seat.name, Model: seat.variant.Model, Effort: seat.variant.effectiveEffort()}
	if err := consumed(turn); err != nil {
		return turn, err
	}
	runID, slot := runIDFromPrompt(prompt), peerPromptField(prompt, "slot")
	id := PeerConversationID{RunID: runID, NodeID: "fmn_peer", Attempt: 1}
	finish := func(text string) (codexTranscriptTurn, error) {
		turn.Complete = true
		turn.Text = text + "\n<<<CHROTE-DONE run-id=" + runID + " status=ok artifact=peer-proof>>>"
		return turn, nil
	}
	if strings.Contains(prompt, "orchestration phase: peer-opening") {
		if _, err := f.store.ReadPeerConversation(id); err == nil {
			return turn, fmt.Errorf("opening saw published conversation")
		}
		if strings.Contains(prompt, "All independent openings are now published") {
			return turn, fmt.Errorf("opening exposed conversation")
		}
		f.mu.Lock()
		f.openings++
		if f.openings == f.participants {
			close(f.openingBarrier)
		}
		f.mu.Unlock()
		select {
		case <-ctx.Done():
			return turn, ctx.Err()
		case <-f.openingBarrier:
		}
		if f.mode == "missing-opening" && slot == "slot_peer_c" {
			return finish("")
		}
		return finish("Independent opening from " + slot)
	}
	f.mu.Lock()
	f.conversationID = id
	n := f.openings
	f.mu.Unlock()
	if n != f.participants {
		return turn, fmt.Errorf("discussion before every opening completed")
	}
	state, err := f.store.ReadPeerConversation(id)
	if err != nil {
		return turn, err
	}
	if len(state.Participants) != f.participants {
		return turn, fmt.Errorf("missing participants")
	}
	if f.mode == "incomplete" {
		return finish("Stopped without acknowledging a result")
	}
	if _, err = f.store.AppendPeerConversation(id, PeerAppendRequest{SlotID: slot, Kind: "message", Text: "First contribution from " + slot}); err != nil {
		return turn, err
	}
	if f.mode == "cancel" {
		f.cancel()
		<-ctx.Done()
		return turn, ctx.Err()
	}
	waitUntil := func(predicate func(*PeerConversation) bool) (*PeerConversation, error) {
		state, err := f.store.ReadPeerConversation(id)
		for err == nil && !predicate(state) {
			if err := ctx.Err(); err != nil {
				return state, err
			}
			if state.Status != "open" {
				return state, fmt.Errorf("conversation ended before expected response: %s", state.Status)
			}
			state, err = f.store.WaitPeerConversation(ctx, id, state.LastSeq)
		}
		return state, err
	}
	countMessages := func(s *PeerConversation) int {
		n := 0
		for _, entry := range s.Entries {
			if entry.Kind == "message" {
				n++
			}
		}
		return n
	}
	if _, err = waitUntil(func(s *PeerConversation) bool { return countMessages(s) >= f.participants }); err != nil {
		return turn, err
	}
	if _, err = f.store.AppendPeerConversation(id, PeerAppendRequest{SlotID: slot, Kind: "message", Text: "Response after reading the other peers: " + slot}); err != nil {
		return turn, err
	}
	if _, err = waitUntil(func(s *PeerConversation) bool { return countMessages(s) >= 2*f.participants }); err != nil {
		return turn, err
	}
	if slot == "slot_peer_b" {
		text := "Evidence collected; unresolved tension: speed versus completeness. Operator should choose."
		text = withPromptOutputContract(text, prompt)
		if _, err = f.store.AppendPeerConversation(id, PeerAppendRequest{SlotID: slot, Kind: "proposal", Text: text}); err != nil {
			return turn, err
		}
	}
	state, err = waitUntil(func(s *PeerConversation) bool { return s.Proposal != nil })
	if err != nil {
		return turn, err
	}
	if _, err = f.store.AppendPeerConversation(id, PeerAppendRequest{SlotID: slot, Kind: "ack", ProposalSeq: state.Proposal.Seq}); err != nil {
		return turn, err
	}
	if _, err = waitUntil(func(s *PeerConversation) bool { return s.Status == "agreed" }); err != nil {
		return turn, err
	}
	return finish("Acknowledged the recorded result and tension")
}

func peerConversationExecutorFixture(t *testing.T, mode string) (*Store, *PersonaStore, *TmuxFormationExecutor, *conversingSeats) {
	t.Helper()
	store, personas := s4RunFixture(t)
	for _, id := range []string{"peer-a", "peer-b", "peer-c"} {
		createS4Persona(t, personas, id)
	}
	board := tmuxPeerBoardFixture() + "\n[[formation.slot]]\nid = \"slot_peer_c\"\nlabel = \"Peer C\"\nagentId = \"peer-c\"\nharness = \"openai-codex\"\n"
	writeFixture(t, store.BoardPath("session-search"), board)
	cfg := tmuxTestConfig(t)
	cfg.TimeoutSeconds = 20
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, &fakeTmuxHarnessClient{})
	seats := &conversingSeats{store: store, participants: 3, mode: mode, openingBarrier: make(chan struct{})}
	executor.seatClient = seats
	return store, personas, executor, seats
}

func TestTmuxPeersConverseOnSameSeatsAndRouteAcknowledgedTensions(t *testing.T) {
	store, personas, executor, seats := peerConversationExecutorFixture(t, "")
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunFormation("session-search", "fmn_peer", FormationRunRequest{Actor: "agent:test", Limits: RunLimits{MaxDispatch: 1, MaxAttempts: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusSucceeded {
		t.Fatalf("status: %+v", status)
	}
	if seats.created != 3 || seats.ended != 3 {
		t.Fatalf("created %d ended %d; want same three seats for both phases", seats.created, seats.ended)
	}
	state, err := store.ReadPeerConversation(seats.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "agreed" || !strings.Contains(state.FinalText, "unresolved tension") {
		t.Fatalf("conversation: %+v", state)
	}
	messages := map[string]int{}
	for _, entry := range state.Entries {
		if entry.Kind == "message" {
			messages[entry.SlotID]++
		}
	}
	for _, slot := range state.Participants {
		if messages[slot] != 2 {
			t.Fatalf("%s contributions=%d", slot, messages[slot])
		}
	}
	events, err := store.ReadRunEvents(status.RunID)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, event := range events {
		if event.Type == RunEventSlotDispatch {
			counts[fmt.Sprint(event.Data["phase"])]++
		}
	}
	if counts["peer-opening"] != 3 || counts["peer-conversation"] != 3 || len(counts) != 2 {
		t.Fatalf("phases: %v", counts)
	}
	output := eventOfType(t, events, RunEventNodeOutput)
	if !strings.Contains(fmt.Sprint(output.Data), "unresolved tension") {
		t.Fatalf("output: %+v", output)
	}
}

func TestTmuxPeerIncompleteResultBlocksAndRetainsOpenings(t *testing.T) {
	store, personas, executor, seats := peerConversationExecutorFixture(t, "incomplete")
	status, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_peer", FormationRunRequest{Actor: "agent:test", Limits: RunLimits{MaxDispatch: 1, MaxAttempts: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusBlocked {
		t.Fatalf("status: %+v", status)
	}
	state, err := store.ReadPeerConversation(seats.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Entries) < 3 || state.FinalText != "" || state.Status == "agreed" {
		t.Fatalf("lost evidence or fabricated success: %+v", state)
	}
	if seats.ended != 3 {
		t.Fatalf("seats ended=%d", seats.ended)
	}
}

func TestTmuxPeerCancellationRetainsConversationAndEndsOwnedSeats(t *testing.T) {
	store, personas, executor, seats := peerConversationExecutorFixture(t, "cancel")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seats.cancel = cancel
	engine := NewRunEngine(store, personas, executor)
	engine.SetExecutionContext(func(string) context.Context { return ctx })
	status, err := engine.RunFormation("session-search", "fmn_peer", FormationRunRequest{Actor: "agent:test", Limits: RunLimits{MaxDispatch: 1, MaxAttempts: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status == RunStatusSucceeded {
		t.Fatalf("canceled conversation succeeded: %+v", status)
	}
	state, err := store.ReadPeerConversation(seats.conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "closed" || len(state.Entries) < 4 {
		t.Fatalf("partial evidence: %+v", state)
	}
	if seats.ended != seats.created || seats.ended != 3 {
		t.Fatalf("cleanup: %d created, %d ended", seats.created, seats.ended)
	}
}

func TestTmuxPeerKeepsEverySeatOnCall(t *testing.T) {
	store, personas, executor, seats := peerConversationExecutorFixture(t, "")
	started, _, err := NewRunEngine(store, personas, executor).PrepareFormationRun("session-search", "fmn_peer", FormationRunRequest{})
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	_, err = executor.ExecuteFormationContext(context.Background(), FormationExecution{RunID: started.RunID, NodeID: "fmn_peer", Formation: board.Formations[0], Attempt: 1, KeepSeatsOnCall: true})
	if err != nil {
		t.Fatal(err)
	}
	if seats.created != 3 || seats.ended != 0 {
		t.Fatalf("created=%d ended=%d", seats.created, seats.ended)
	}
	if kept := KeptSeats(mustEvents(t, store, started.RunID)); len(kept) != 3 {
		t.Fatalf("kept seats: %+v", kept)
	}
}

func TestTmuxPeerOpeningFailureRetainsIndependentEvidence(t *testing.T) {
	store, personas, executor, seats := peerConversationExecutorFixture(t, "missing-opening")
	status, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_peer", FormationRunRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != RunStatusBlocked {
		t.Fatalf("status: %+v", status)
	}
	count := 0
	for _, event := range mustEvents(t, store, status.RunID) {
		if event.Type == "peer_opening" && event.SlotID != "slot_peer_c" {
			raw, err := os.ReadFile(filepath.Join(store.Workspace, stringFromEventData(event, "path")))
			if err != nil || !strings.Contains(string(raw), "Independent opening") {
				t.Fatalf("lost opening: %s %v", raw, err)
			}
			count++
		}
	}
	if count != 2 || seats.ended != 3 {
		t.Fatalf("retained openings=%d ended=%d", count, seats.ended)
	}
}

type peerReadinessSeats struct {
	*conversingSeats
	real realSeatTransport
}

func (f *peerReadinessSeats) Ready(ctx context.Context, socket string, seat *nativeSeat, harness string) error {
	return f.real.Ready(ctx, socket, seat, harness)
}

func TestTmuxEveryPeerReusesItsSeatAfterLongOpeningOutput(t *testing.T) {
	store, personas, executor, seats := peerConversationExecutorFixture(t, "")
	startup, _, _ := paneFixture(t, "codex-idle-empty")
	startup = regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(startup, "")
	history, err := os.ReadFile(filepath.Join("testdata", "panes", "codex-ready-capture-after-long-output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	idle, x, y := paneFixture(t, "codex-idle-after-long-output")
	var mu sync.Mutex
	checks := map[string]int{}
	executor.seatClient = &peerReadinessSeats{conversingSeats: seats, real: realSeatTransport{
		control: func(context.Context, string, string) (*seatControl, error) {
			return &seatControl{events: make(chan struct{})}, nil
		},
		command: func(_ context.Context, _ string, _ *strings.Reader, args ...string) (string, error) {
			target := ""
			for i, arg := range args {
				if arg == "-t" && i+1 < len(args) {
					target = args[i+1]
				}
			}
			switch {
			case args[0] == "display-message" && strings.Contains(args[len(args)-1], "cursor_x"):
				return fmt.Sprintf("%d %d 0", x, y), nil
			case args[0] == "display-message":
				return "0", nil
			case args[0] == "capture-pane" && hasString(args, "-e"):
				mu.Lock()
				checks[target]++
				mu.Unlock()
				return idle, nil
			case args[0] == "capture-pane":
				seats.mu.Lock()
				sent := false
				for seat := range seats.prompts {
					sent = sent || seat.name == target
				}
				seats.mu.Unlock()
				if sent {
					return string(history), nil
				}
				return startup, nil
			}
			return "", fmt.Errorf("unexpected tmux command %v", args)
		},
	}}
	status, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_peer", FormationRunRequest{})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("status: %+v %v", status, err)
	}
	if len(checks) != 3 {
		t.Fatalf("only %d peers reused readiness: %v", len(checks), checks)
	}
	for seat, count := range checks {
		if count != 1 {
			t.Fatalf("seat %s input checks=%d", seat, count)
		}
	}
}

func TestPeerOpeningEvidenceSupportsPermittedIDs(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	nodeID, slotID := strings.Repeat("n", 128), strings.Repeat("s", 128)
	if !peerPathID(nodeID) || !peerPathID(slotID) {
		t.Fatal("review identifiers outside peer contract")
	}
	board := strings.ReplaceAll(s4RunBoardFixture(), "fmn_research", nodeID)
	board = strings.ReplaceAll(board, "slot_research", slotID)
	writeFixture(t, store.BoardPath("session-search"), board)
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
	if err != nil {
		t.Fatal(err)
	}
	executor := &TmuxFormationExecutor{store: store}
	if err := executor.preservePeerOpening(FormationExecution{RunID: started.RunID, NodeID: nodeID, Attempt: 1}, tmuxSlotOutput{SlotID: slotID, Text: "Independent opening"}); err != nil {
		t.Fatalf("admitted IDs could not retain opening: %v", err)
	}
}
