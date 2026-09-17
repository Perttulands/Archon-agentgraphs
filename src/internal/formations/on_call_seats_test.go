package formations

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// keptSeatFake adds kept-seat pane access to the executor's fake tmux client.
// Pane frames are read in order; the last frame repeats.
type keptSeatFake struct {
	*fakeTmuxHarnessClient
	frames map[string][]string
	reads  map[string]int
	pasted []string
	events []string
	// shown, when set, is the pane a paste leaves, in place of Claude Code
	// wrapping the pasted text.
	shown string
}

func (f *keptSeatFake) DescribeSeat(_ context.Context, _, paneID string) (string, bool, error) {
	for _, killed := range f.killed {
		if killed == paneID {
			return "", false, fakeTmuxMissingTarget(paneID)
		}
	}
	return paneID, false, nil
}

func (f *keptSeatFake) CaptureSeat(_ context.Context, _, paneID string) (string, error) {
	frames := f.frames[paneID]
	if len(frames) == 0 {
		return "Claude Code\n❯ ", nil
	}
	if f.reads == nil {
		f.reads = map[string]int{}
	}
	index := min(f.reads[paneID], len(frames)-1)
	f.reads[paneID]++
	f.events = append(f.events, "read "+frames[index])
	return frames[index], nil
}

// WaitInputClear reads frames until one shows an idle agent under an empty
// Claude Code prompt, standing in for the transport's pane check.
func (f *keptSeatFake) WaitInputClear(ctx context.Context, socket string, s *nativeSeat) error {
	f.events = append(f.events, "wait "+s.sessionID+" "+s.paneID+" "+s.variant.ID)
	for {
		text, _ := f.CaptureSeat(ctx, socket, s.paneID)
		if strings.HasSuffix(text, "❯ ") && !tmuxPaneShowsAgentWorking(s.variant.ID, text) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(keptSeatPoll):
		}
	}
}

func (f *keptSeatFake) PasteSeat(_ context.Context, _, paneID, _, text string) error {
	f.pasted = append(f.pasted, text)
	f.events = append(f.events, "paste")
	// The harness wraps the input line itself, as Claude Code and Codex do.
	wrapped := text
	if len(wrapped) > 40 {
		wrapped = wrapped[:40] + "\n  " + wrapped[40:]
	}
	f.frames[paneID] = []string{"Claude Code\n❯ " + wrapped}
	if f.shown != "" {
		f.frames[paneID] = []string{f.shown}
	}
	f.reads[paneID] = 0
	return nil
}

func (f *keptSeatFake) SubmitSeat(context.Context, string, string) error {
	f.events = append(f.events, "submit")
	return nil
}

func (f *keptSeatFake) KillSeat(ctx context.Context, socket, sessionID string) error {
	f.events = append(f.events, "kill "+sessionID)
	return f.KillSession(ctx, socket, sessionID)
}

func quickKeptSeatWaits(t *testing.T) {
	t.Helper()
	poll, settle, paste := keptSeatPoll, tmuxPasteSettleDelay, keptSeatPasteWait
	keptSeatPoll, tmuxPasteSettleDelay, keptSeatPasteWait = time.Millisecond, time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { keptSeatPoll, tmuxPasteSettleDelay, keptSeatPasteWait = poll, settle, paste })
}

// runKeptFormation starts a formation run on board and executes the formation
// once with its seats kept on call.
func runKeptFormation(t *testing.T, board, formationID string, personas []string, client *fakeTmuxHarnessClient) (*Store, []RunEvent) {
	t.Helper()
	store, personaStore := s4RunFixture(t)
	store.Now = fixedClock()
	personaStore.Now = fixedClock()
	for _, id := range personas {
		createS4Persona(t, personaStore, id)
	}
	writeFixture(t, store.BoardPath("session-search"), board)
	cfg := tmuxTestConfig(t)
	client.pane = tmuxPaneState{CurrentPath: cfg.Cwd}
	executor := newTmuxFormationExecutorWithClient(store, personaStore, cfg, &keptSeatFake{fakeTmuxHarnessClient: client, frames: map[string][]string{}})
	started, _, err := NewRunEngine(store, personaStore, executor).PrepareFormationRun("session-search", formationID, FormationRunRequest{Actor: "agent:test", Limits: RunLimits{MaxDispatch: 5, MaxAttempts: 1}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	formation, _ := findFormation(document.Formations, formationID)
	if _, err := executor.ExecuteFormationContext(context.Background(), FormationExecution{RunID: started.RunID, NodeID: formationID, Formation: formation, Attempt: 1, KeepSeatsOnCall: true}); err != nil {
		t.Fatalf("execute %s: %v", formationID, err)
	}
	return store, mustEvents(t, store, started.RunID)
}

func cleanupOutcomes(events []RunEvent) string {
	var outcomes []string
	for _, event := range events {
		if event.Type == RunEventSeatCleanup {
			outcomes = append(outcomes, event.SlotID+"="+stringFromEventData(event, "outcome"))
		}
	}
	return strings.Join(outcomes, " ")
}

func TestTmuxExecutorKeepsSoloAndPeerSeatsAndOnlyTheOrchestratedController(t *testing.T) {
	t.Run("solo", func(t *testing.T) {
		client := &fakeTmuxHarnessClient{artifact: "reports/solo.md"}
		_, events := runKeptFormation(t, s4RunBoardFixture(), "fmn_research", []string{"scout"}, client)
		if got := cleanupOutcomes(events); got != "slot_research=kept_on_call" || len(client.killed) != 0 {
			t.Fatalf("solo cleanups %q, killed %v", got, client.killed)
		}
		if seats := KeptSeats(events); len(seats) != 1 || seats[0].SessionID != client.created[0] || seats[0].PaneID != client.created[0] {
			t.Fatalf("kept seats = %+v, created %v", seats, client.created)
		}
	})
	t.Run("peer", func(t *testing.T) {
		client := &fakeTmuxHarnessClient{captures: []string{
			"A\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=a.md>>>",
			"B\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=b.md>>>",
			"SYNTHESIS\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=final.md>>>",
		}}
		_, events := runKeptFormation(t, tmuxPeerBoardFixture(), "fmn_peer", []string{"peer-a", "peer-b"}, client)
		if got := cleanupOutcomes(events); !strings.Contains(got, "slot_peer_a=kept_on_call") || !strings.Contains(got, "slot_peer_b=kept_on_call") || len(client.killed) != 0 {
			t.Fatalf("peer cleanups %q, killed %v", got, client.killed)
		}
	})
	t.Run("orchestrated", func(t *testing.T) {
		client := &fakeTmuxHarnessClient{captures: []string{"FINAL\n<<<CHROTE-DONE run-id=run_missing status=ok artifact=final.md>>>"}}
		_, events := runKeptFormation(t, tmuxOrchestratedBoardFixture(), "fmn_orch", []string{"lead", "worker-a", "worker-b"}, client)
		got := cleanupOutcomes(events)
		if !strings.Contains(got, "slot_lead=kept_on_call") || !strings.Contains(got, "slot_worker_a=ended") || !strings.Contains(got, "slot_worker_b=ended") || len(client.killed) != 2 {
			t.Fatalf("orchestrated cleanups %q, killed %v", got, client.killed)
		}
		if seats := KeptSeats(events); len(seats) != 1 || seats[0].SlotID != "slot_lead" {
			t.Fatalf("kept seats = %+v", seats)
		}
	})
}

func keptSeatExecutor(t *testing.T, frames map[string][]string) (*TmuxFormationExecutor, *keptSeatFake) {
	t.Helper()
	quickKeptSeatWaits(t)
	fake := &keptSeatFake{fakeTmuxHarnessClient: &fakeTmuxHarnessClient{}, frames: frames, reads: map[string]int{}}
	return newTmuxFormationExecutorWithClient(nil, nil, tmuxTestConfig(t), fake), fake
}

func TestKeptSeatAskWaitsForAnIdleAgentWithAnEmptyInputLine(t *testing.T) {
	executor, fake := keptSeatExecutor(t, map[string][]string{"%1": {
		"Claude Code\n✶ Puzzling… (4s · ↓ 60 tokens)\n❯ ",
		"Claude Code\n❯ my unsent thought",
		"Claude Code\n❯ ",
	}})
	seat := KeptSeat{SlotID: "slot_work", SessionID: "%1", PaneID: "%1", Harness: "claude-code"}
	if err := executor.PasteAsk(context.Background(), seat, "Read the file /state/briefs/gate-run-9-slot_work.md and follow it."); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(fake.events, " | ")
	want := "wait %1 %1 claude-code | read Claude Code\n✶ Puzzling… (4s · ↓ 60 tokens)\n❯  | read Claude Code\n❯ my unsent thought | read Claude Code\n❯  | paste | read Claude Code\n❯ Read the file /state/briefs/gate-run-9-s\n  lot_work.md and follow it. | submit"
	if got != want {
		t.Fatalf("paste events:\n%s\nwant\n%s", got, want)
	}

	// An agent that stays busy is not pasted into; the caller retries later.
	busy, busyFake := keptSeatExecutor(t, map[string][]string{"%2": {"Claude Code\n❯ still typing"}})
	start := time.Now()
	err := busy.PasteAsk(context.Background(), KeptSeat{SessionID: "%2", PaneID: "%2", Harness: "claude-code"}, "pointer")
	if err == nil || len(busyFake.pasted) != 0 || time.Since(start) < keptSeatPasteWait {
		t.Fatalf("busy paste err %v, pasted %v after %v", err, busyFake.pasted, time.Since(start))
	}
	busyFake.killed = append(busyFake.killed, "%gone")
	if err := busy.PasteAsk(context.Background(), KeptSeat{SessionID: "%gone", PaneID: "%gone"}, "pointer"); !errors.Is(err, errKeptSeatGone) {
		t.Fatalf("gone seat paste err = %v", err)
	}
}

func TestKeptSeatAskIsSubmittedAmongCodexStars(t *testing.T) {
	// A captured Codex pane wraps the pointer inside its brief path and draws
	// stars between the words.
	screen, _, _ := paneFixture(t, "codex-staged-wrapped-pointer")
	const brief = "/home/operator/archon/state-dirs/a-state-directory-with-a-long-path-so-the-codex-input-line-wraps-inside-the-brief-pat/state/briefs/seat-644037887.md"
	for pointer, submitted := range map[string]bool{seatPointer(brief): true, seatPointer("/state/briefs/other.md"): false} {
		executor, fake := keptSeatExecutor(t, map[string][]string{"%1": {"Claude Code\n❯ "}})
		fake.shown = ansiSGR.ReplaceAllString(screen, "")
		err := executor.PasteAsk(context.Background(), KeptSeat{SlotID: "slot_work", SessionID: "%1", PaneID: "%1", Harness: "openai-codex"}, pointer)
		if got := slices.Contains(fake.events, "submit"); got != submitted || (err == nil) != submitted {
			t.Errorf("pointer %q: submitted = %t, err = %v; want submitted %t", pointer, got, err, submitted)
		}
	}
}

func TestEndingAKeptSeatWaitsForIdleAndKillsItsImmutableSession(t *testing.T) {
	executor, fake := keptSeatExecutor(t, map[string][]string{"%3": {
		"Claude Code\n✢ Replying… (2s · ↓ 12 tokens)",
		"Claude Code\n❯ ",
	}})
	outcome, detail := executor.EndKeptSeat(context.Background(), KeptSeat{SessionID: "%3", PaneID: "%3", Harness: "claude-code"})
	if outcome != SeatOutcomeEnded || detail != "" || fmt.Sprint(fake.killed) != "[%3]" || !strings.HasPrefix(strings.Join(fake.events, " | "), "wait %3 %3 claude-code | read Claude Code\n✢ Replying… (2s · ↓ 12 tokens) | read Claude Code\n❯  | kill %3") {
		t.Fatalf("end = %s %q, killed %v, events %v", outcome, detail, fake.killed, fake.events)
	}
	if outcome, _ := executor.EndKeptSeat(context.Background(), KeptSeat{SessionID: "%3", PaneID: "%3"}); outcome != SeatOutcomeGone || len(fake.killed) != 1 {
		t.Fatalf("ending a gone seat = %s, killed %v", outcome, fake.killed)
	}
	if present, outcome := executor.ProbeKeptSeat(context.Background(), KeptSeat{SessionID: "$other", PaneID: "%4"}); present || outcome != SeatOutcomeGone {
		t.Fatalf("a pane now in another session probed %v %s", present, outcome)
	}
}
