package formations

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// waitingSeats makes each phase of a dispatch wait on something for a while,
// as a real seat can: the harness is slow to become ready, the operator's text
// holds the input line, and the turn ends without the sentinel after an
// operator turn. The waits end on their own, so the run still succeeds.
type waitingSeats struct {
	*fakeTmuxHarnessClient
	hold time.Duration
}

func (w *waitingSeats) Ready(ctx context.Context, socket string, s *nativeSeat, h string) error {
	s.setWaiting(SeatStateNotReady)
	time.Sleep(w.hold)
	s.setWaiting("")
	return w.fakeTmuxHarnessClient.Ready(ctx, socket, s, h)
}

func (w *waitingSeats) Stage(ctx context.Context, socket string, s *nativeSeat, dispatch, pointer string) error {
	s.setWaiting(SeatStateWaitingForIdleInput)
	time.Sleep(w.hold)
	s.setWaiting("")
	return w.fakeTmuxHarnessClient.Stage(ctx, socket, s, dispatch, pointer)
}

func (w *waitingSeats) WaitTurn(ctx context.Context, s *nativeSeat, cwd, pointer string, consumed func(codexTranscriptTurn) error) (codexTranscriptTurn, error) {
	s.setWaiting(SeatStateTurnEndedWithoutSentinel)
	time.Sleep(w.hold)
	s.setWaiting("")
	return w.fakeTmuxHarnessClient.WaitTurn(ctx, s, cwd, pointer, consumed)
}

// With no step time limit, each wait a seat cannot end on its own becomes a
// seat_state event once it lasts the observation window, and a "working"
// event when it ends; nothing blocks or fails the step.
func TestSeatWaitsAreRecordedWithoutBlockingTheStep(t *testing.T) {
	previous := seatStateObservation
	seatStateObservation = 20 * time.Millisecond
	defer func() { seatStateObservation = previous }()
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	cfg := tmuxTestConfig(t)
	fake := &fakeTmuxHarnessClient{
		pane:     tmuxPaneState{CurrentPath: cfg.Cwd},
		captures: []string{"done\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=out.md>>>"},
	}
	executor := newTmuxFormationExecutorWithClient(store, personas, cfg, fake)
	executor.seatClient = &waitingSeats{fakeTmuxHarnessClient: fake, hold: 300 * time.Millisecond}
	status, err := NewRunEngine(store, personas, executor).RunFormation("session-search", "fmn_research", FormationRunRequest{})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("status %+v %v", status, err)
	}
	var states []string
	for _, event := range mustEvents(t, store, status.RunID) {
		if event.Type != RunEventSeatState {
			continue
		}
		state := stringFromEventData(event, "state")
		if event.SlotID != "slot_research" || stringFromEventData(event, "since") == "" || (state != SeatStateWorking && stringFromEventData(event, "detail") == "") {
			t.Fatalf("seat_state event = %+v", event)
		}
		states = append(states, state)
	}
	// Each wait is recorded in order, and the seat reads working again once
	// the last one ends. A wait that follows another at once may skip the
	// working between them.
	var waits []string
	for _, state := range states {
		if state != SeatStateWorking {
			waits = append(waits, state)
		}
	}
	want := strings.Join([]string{SeatStateNotReady, SeatStateWaitingForIdleInput, SeatStateTurnEndedWithoutSentinel}, ",")
	if got := strings.Join(waits, ","); got != want || states[len(states)-1] != SeatStateWorking {
		t.Fatalf("seat states = %v, want waits %s ending in working", states, want)
	}
}

// A short wait is not recorded.
func TestBriefSeatWaitsAreNotRecorded(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s4RunBoardFixture())
	started, err := store.StartRun("session-search", RunStartRequest{MissionID: "mis_showcase", Personas: personas})
	if err != nil {
		t.Fatal(err)
	}
	executor := &TmuxFormationExecutor{store: store}
	seat := &nativeSeat{}
	stop := executor.watchSeatState(context.Background(), started.RunID, "fmn_research", "slot_research", "", seat)
	seat.setWaiting(SeatStateWaitingForIdleInput)
	time.Sleep(50 * time.Millisecond)
	seat.setWaiting("")
	stop()
	for _, event := range mustEvents(t, store, started.RunID) {
		if event.Type == RunEventSeatState {
			t.Fatalf("a brief wait was recorded: %+v", event)
		}
	}
}

// fixtureWaits replays a captured transcript and lists, in order, each
// distinct reason the dispatch was parked before it completed.
func fixtureWaits(t *testing.T, harness, name, brief string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "seats", name+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(raw), "\n")
	seat := &nativeSeat{variant: HarnessVariant{ID: harness}, runID: fixtureRunID}
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	var waits []string
	for index := range lines {
		if err := os.WriteFile(path, []byte(strings.Join(lines[:index+1], "")), 0600); err != nil {
			t.Fatal(err)
		}
		turn, err := readLatestSeatTurn(seat, path, "/work", fixturePointer(brief))
		if err != nil || turn.Complete {
			return waits
		}
		if turn.Waiting != "" && (len(waits) == 0 || waits[len(waits)-1] != turn.Waiting) {
			waits = append(waits, turn.Waiting)
		}
	}
	return waits
}

// The real transcripts say why an incomplete dispatch is parked: a reply to
// the operator that ended without the sentinel, or Claude background work.
func TestTranscriptTurnsSayWhyTheyWait(t *testing.T) {
	for _, harness := range fixtureHarnesses {
		name := harness.name + "-operator-chat-then-sentinel"
		if got := fixtureWaits(t, harness.id, name, "chat"); !slices.Contains(got, SeatStateTurnEndedWithoutSentinel) {
			t.Errorf("%s waits = %v, want %s", name, got, SeatStateTurnEndedWithoutSentinel)
		}
	}
	if got := fixtureWaits(t, "claude-code", "claude-background-then-operator", "midturn"); !slices.Contains(got, SeatStateBackgroundWork) {
		t.Errorf("claude background waits = %v, want %s", got, SeatStateBackgroundWork)
	}
}
