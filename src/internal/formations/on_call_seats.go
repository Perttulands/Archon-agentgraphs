package formations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/core"
)

// The tmux executor keeps seats on call (ADR-0019). A kept seat is addressed
// only by the identity its seat_created recorded: its immutable session and
// pane IDs on the server its socket identity names. Nothing here enumerates or
// touches a session it did not create.

var (
	// keptSeatIdleWait bounds the wait for a mid-turn seat to go idle before it
	// ends, so the agent's closing reply stays readable.
	keptSeatIdleWait = 60 * time.Second
	// keptSeatPasteWait bounds one attempt to paste an ask into a busy seat; the
	// runtime retries an unreached seat within seconds.
	keptSeatPasteWait = 10 * time.Second
	errKeptSeatGone   = errors.New("kept seat is gone")
	// keptSeatPoll paces pane reads while waiting for a seat. Tests shorten it.
	keptSeatPoll = 250 * time.Millisecond
	// seatReadyForPaste reports an idle agent whose input line is empty.
	seatReadyForPaste = func(harness, captured string) bool {
		return !tmuxPaneShowsAgentWorking(captured) && seatInputLineEmpty(harness, captured)
	}
)

// keptSeatTransport is the pane access a kept seat needs, by immutable target.
type keptSeatTransport interface {
	DescribeSeat(ctx context.Context, socket, paneID string) (sessionID string, dead bool, err error)
	CaptureSeat(ctx context.Context, socket, paneID string) (string, error)
	PasteSeat(ctx context.Context, socket, paneID, buffer, text string) error
	SubmitSeat(ctx context.Context, socket, paneID string) error
	KillSeat(ctx context.Context, socket, sessionID string) error
}

func (t realSeatTransport) DescribeSeat(ctx context.Context, socket, paneID string) (string, bool, error) {
	out, err := t.run(ctx, socket, nil, "display-message", "-p", "-t", paneID, "#{session_id}\t#{pane_dead}")
	if err != nil {
		if strings.Contains(err.Error(), "can't find") || strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no server running") {
			return "", false, fmt.Errorf("%w: %s", errTmuxTargetMissing, err.Error())
		}
		return "", false, err
	}
	sessionID, dead, _ := strings.Cut(strings.TrimSpace(out), "\t")
	return sessionID, dead == "1", nil
}

func (t realSeatTransport) CaptureSeat(ctx context.Context, socket, paneID string) (string, error) {
	return t.run(ctx, socket, nil, "capture-pane", "-p", "-J", "-t", paneID, "-S", "-80")
}

func (t realSeatTransport) PasteSeat(ctx context.Context, socket, paneID, buffer, text string) error {
	if _, err := t.run(ctx, socket, strings.NewReader(text), "load-buffer", "-b", buffer, "-"); err != nil {
		return err
	}
	_, err := t.run(ctx, socket, nil, "paste-buffer", "-p", "-b", buffer, "-t", paneID, "-d")
	return err
}

func (t realSeatTransport) SubmitSeat(ctx context.Context, socket, paneID string) error {
	_, err := t.run(ctx, socket, nil, "send-keys", "-t", paneID, "Enter")
	return err
}

func (t realSeatTransport) KillSeat(ctx context.Context, socket, sessionID string) error {
	_, err := t.run(ctx, socket, nil, "kill-session", "-t", sessionID)
	return err
}

func (e *TmuxFormationExecutor) keptTransport() keptSeatTransport {
	if transport, ok := e.seatClient.(keptSeatTransport); ok {
		return transport
	}
	return realSeatTransport{}
}

// keptSeatServerChanged reports a socket that now names a different server
// from the one the seat was created on.
func (e *TmuxFormationExecutor) keptSeatServerChanged(seat KeptSeat) bool {
	if seat.SocketIdentity == "" {
		return false
	}
	current, err := core.SocketIdentity(e.config.Socket)
	return err != nil || current != seat.SocketIdentity
}

// ProbeKeptSeat reports whether the seat's pane still belongs to its recorded
// session on the same server. A probe that cannot tell counts as present, so a
// passing tmux error never ends a conversation or reroutes an ask.
func (e *TmuxFormationExecutor) ProbeKeptSeat(ctx context.Context, seat KeptSeat) (bool, string) {
	if e.keptSeatServerChanged(seat) {
		return false, SeatOutcomeLeftSocketChanged
	}
	sessionID, dead, err := e.keptTransport().DescribeSeat(ctx, e.config.Socket, seat.PaneID)
	switch {
	case errors.Is(err, errTmuxTargetMissing):
		return false, SeatOutcomeGone
	case err != nil:
		return true, ""
	case dead || sessionID != seat.SessionID:
		return false, SeatOutcomeGone
	}
	return true, ""
}

// EndKeptSeat waits up to a minute for the agent to go idle, then kills the
// seat's session by its immutable ID.
func (e *TmuxFormationExecutor) EndKeptSeat(ctx context.Context, seat KeptSeat) (string, string) {
	if present, outcome := e.ProbeKeptSeat(ctx, seat); !present {
		return outcome, ""
	}
	transport := e.keptTransport()
	idle, cancel := context.WithTimeout(ctx, keptSeatIdleWait)
	_ = waitKeptSeat(idle, func() (bool, error) {
		text, err := transport.CaptureSeat(idle, e.config.Socket, seat.PaneID)
		return err != nil || !tmuxPaneShowsAgentWorking(text), nil
	})
	cancel()
	if err := transport.KillSeat(ctx, e.config.Socket, seat.SessionID); err != nil {
		return SeatOutcomeLeftCleanupFailed, redactLedgerText(err.Error())
	}
	return SeatOutcomeEnded, ""
}

// PasteAsk pastes the pointer once the agent is idle with an empty input line,
// waits for it to render and submits it. An operator typing into the seat, or
// an agent still working, makes it return an error for a retry soon after.
func (e *TmuxFormationExecutor) PasteAsk(ctx context.Context, seat KeptSeat, pointer string) error {
	if present, _ := e.ProbeKeptSeat(ctx, seat); !present {
		return errKeptSeatGone
	}
	transport := e.keptTransport()
	attempt, cancel := context.WithTimeout(ctx, keptSeatPasteWait)
	defer cancel()
	if err := waitKeptSeat(attempt, func() (bool, error) {
		text, err := transport.CaptureSeat(attempt, e.config.Socket, seat.PaneID)
		return err == nil && seatReadyForPaste(seat.Harness, text), err
	}); err != nil {
		return fmt.Errorf("seat %s is not ready for an ask: %w", seat.SlotID, err)
	}
	if err := transport.PasteSeat(ctx, e.config.Socket, seat.PaneID, safeTmuxBufferName("ask-"+seat.SlotID), pointer); err != nil {
		return err
	}
	if err := waitKeptSeat(attempt, func() (bool, error) {
		text, err := transport.CaptureSeat(attempt, e.config.Socket, seat.PaneID)
		return err == nil && strings.Contains(strings.ReplaceAll(text, "\n", ""), pointer), err
	}); err != nil {
		return fmt.Errorf("the ask did not render in seat %s: %w", seat.SlotID, err)
	}
	settle := time.NewTimer(tmuxPasteSettleDelay)
	defer settle.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-settle.C:
	}
	return transport.SubmitSeat(ctx, e.config.Socket, seat.PaneID)
}

// waitKeptSeat polls done until it holds or ctx ends. A transient read error
// is retried until the deadline.
func waitKeptSeat(ctx context.Context, done func() (bool, error)) error {
	var last error
	for {
		ok, err := done()
		if ok {
			return nil
		}
		if err != nil {
			last = err
		}
		timer := time.NewTimer(keptSeatPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			if last != nil {
				return fmt.Errorf("%w (last read: %v)", ctx.Err(), last)
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// seatInputLineEmpty reports whether the harness's input line holds no
// operator text: nothing after Claude Code's ❯, or Codex's › showing only its
// placeholder suggestion.
func seatInputLineEmpty(harness, captured string) bool {
	prompt := "❯"
	if harness == "openai-codex" {
		prompt = "›"
	}
	lines := strings.Split(strings.TrimRight(captured, " \t\r\n"), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-12; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, prompt) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, prompt))
		return rest == "" || harness == "openai-codex" && codexPlaceholder(rest)
	}
	return false
}

// Codex fills an empty input line with a dim suggestion.
func codexPlaceholder(text string) bool {
	for _, suggestion := range []string{"Explain this codebase", "Summarize recent commits", "Implement {feature}", "Find and fix a bug in @filename", "Write tests for @filename", "Improve documentation in @filename", "Run /review on my current changes", "Use /skills to list available skills"} {
		if text == suggestion {
			return true
		}
	}
	return false
}
