package formations

import (
	"context"
	"os"
	"strings"
)

// The schedule fake supplies native turns at the transport boundary. Parser
// tests independently exercise actual JSONL records for each harness.
func (f *fakeTmuxHarnessClient) Create(ctx context.Context, socket, name, cwd, root string, v HarnessVariant) (*nativeSeat, error) {
	if err := f.CreateSession(ctx, socket, name, cwd, v.Launch); err != nil {
		return nil, err
	}
	return &nativeSeat{name: name, sessionID: name, paneID: name, variant: v}, nil
}
func (f *fakeTmuxHarnessClient) Ready(ctx context.Context, socket string, s *nativeSeat, h string) error {
	// The legacy fake scripts startup captures and target failures.
	if _, err := f.DescribeActivePane(ctx, socket, s.name); err != nil {
		return err
	}
	if f.pane.Dead {
		return runExecutionError("dead_pane", "owned seat exited before readiness", "adapter", ErrDispatchDeadPane)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		text, err := f.CapturePane(ctx, socket, s.name, 8192)
		if err != nil {
			return err
		}
		if tmuxPaneShowsHarnessReady(h, text) {
			return nil
		}
		if len(f.startupCaptures) == 1 && f.startupCaptures[0] == text {
			<-ctx.Done()
			return ctx.Err()
		}
	}
}
func (f *fakeTmuxHarnessClient) Stage(ctx context.Context, socket string, s *nativeSeat, dispatch, pointer string) error {
	raw, err := os.ReadFile(s.brief)
	if err != nil {
		return err
	}
	if pointer != seatPointer(s.brief) {
		panic("not a brief pointer")
	}
	return f.SendPrompt(ctx, socket, s.name, dispatch, string(raw))
}
func (f *fakeTmuxHarnessClient) WaitTurn(ctx context.Context, s *nativeSeat, cwd, pointer string, consumed func(codexTranscriptTurn) error) (codexTranscriptTurn, error) {
	turn := codexTranscriptTurn{Consumed: true, SessionID: "native-" + s.name, Model: s.variant.Model, Effort: s.variant.effectiveEffort()}
	if err := consumed(turn); err != nil {
		return turn, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return turn, err
		}
		text, err := f.CapturePane(ctx, "", s.name, 8192)
		if err != nil {
			return turn, err
		}
		if strings.Contains(text, "<<<CHROTE-DONE") {
			turn.Text = text
			turn.Complete = true
			return turn, nil
		}
	}
}
func (f *fakeTmuxHarnessClient) Snapshot(s *nativeSeat, cwd, pointer string) (codexTranscriptTurn, error) {
	text, err := f.CapturePane(context.Background(), "", s.name, 8192)
	return codexTranscriptTurn{Consumed: text != "", Complete: !tmuxPaneShowsAgentWorking(text), Text: text, Model: s.variant.Model, Effort: s.variant.effectiveEffort()}, err
}
func (f *fakeTmuxHarnessClient) End(ctx context.Context, socket string, s *nativeSeat) error {
	return f.KillSession(ctx, socket, s.sessionID)
}
