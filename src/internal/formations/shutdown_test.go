package formations

import (
	"context"
	"errors"
	"testing"
	"time"
)

type pausedSnapshot struct {
	*fakeTmuxHarnessClient
	entered chan struct{}
	calls   int
}

func (s *pausedSnapshot) Snapshot(ctx context.Context, _ *nativeSeat, _, _ string) (codexTranscriptTurn, error) {
	s.calls++
	close(s.entered)
	<-ctx.Done()
	return codexTranscriptTurn{}, ctx.Err()
}

func TestShutdownCancelsWorkerSnapshotsBeforeNextWorker(t *testing.T) {
	transport := &pausedSnapshot{entered: make(chan struct{})}
	e := &TmuxFormationExecutor{seatClient: transport}
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	done := make(chan error, 1)
	go func() {
		done <- e.observeWorkerOutcomes(ctx, FormationExecution{}, tmuxSlotBinding{}, []workerBaseline{{seat: &nativeSeat{}}, {seat: &nativeSeat{}}})
	}()
	<-transport.entered
	cancel(ErrCoordinatorShutdown)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker snapshot ignored detach")
	}
	if transport.calls != 1 {
		t.Fatalf("observed %d workers after detach", transport.calls)
	}
}

type interruptedSeatTransport struct {
	*fakeTmuxHarnessClient
	entered chan struct{}
}

func (s *interruptedSeatTransport) WaitTurn(ctx context.Context, seat *nativeSeat, cwd, pointer string, consumed func(codexTranscriptTurn) error) (codexTranscriptTurn, error) {
	turn := codexTranscriptTurn{Consumed: true, SessionID: "native-" + seat.name}
	if err := consumed(turn); err != nil {
		return turn, err
	}
	close(s.entered)
	<-ctx.Done()
	return turn, ctx.Err()
}

func TestShutdownPreservesOrchestratedSeatsAndAbortEndsThem(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "abort"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			store, personas := s4RunFixture(t)
			for _, id := range []string{"lead", "worker-a", "worker-b"} {
				createS4Persona(t, personas, id)
			}
			writeFixture(t, store.BoardPath("session-search"), tmuxOrchestratedBoardFixture())
			cfg := tmuxTestConfig(t)
			client := &fakeTmuxHarnessClient{pane: tmuxPaneState{CurrentPath: cfg.Cwd}}
			transport := &interruptedSeatTransport{fakeTmuxHarnessClient: client, entered: make(chan struct{})}
			executor := newTmuxFormationExecutorWithClient(store, personas, cfg, client)
			executor.seatClient = transport
			engine := NewRunEngine(store, personas, executor)
			stopping := make(chan struct{})
			ctx, cancel := context.WithCancelCause(WithShutdownFence(context.Background(), stopping))
			defer cancel(nil)
			engine.SetExecutionContext(func(string) context.Context { return ctx })
			done := make(chan error, 1)
			go func() {
				_, err := engine.RunFormation("session-search", "fmn_orch", FormationRunRequest{Actor: "agent:test", Limits: RunLimits{MaxDispatch: 6, MaxAttempts: 2}})
				done <- err
			}()
			select {
			case <-transport.entered:
			case err := <-done:
				t.Fatalf("executor failed before wait: %v", err)
			case <-time.After(time.Second):
				t.Fatal("seat did not start")
			}
			if shutdown {
				close(stopping)
				cancel(ErrCoordinatorShutdown)
			} else {
				cancel(context.Canceled)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("seat observation did not stop")
			}
			if len(client.created) != 3 {
				t.Fatal(client.created)
			}
			wantKilled := 3
			if shutdown {
				wantKilled = 0
			}
			if len(client.killed) != wantKilled {
				t.Fatalf("killed %v during %s", client.killed, name)
			}
			events := readRunEvents(t, findOnlyRunLedger(t, store, "session-search"))
			if len(unresolvedDispatches(events)) != 1 {
				t.Fatal("dispatch lost")
			}
			if shutdown {
				preserved := 0
				for _, event := range events {
					if event.Type == "seat_cleanup" && stringFromEventData(event, "outcome") == "left_shutdown" {
						preserved++
					}
				}
				if preserved != 3 {
					t.Fatalf("preserved %d seats", preserved)
				}
			}
		})
	}
}
