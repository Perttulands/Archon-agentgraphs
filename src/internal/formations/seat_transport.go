package formations

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/filewatch"
)

type nativeSeat struct {
	name, sessionID, paneID, root, brief, pointer string
	socket                                        string
	variant                                       HarnessVariant
	control                                       *seatControl
	watch                                         *filewatch.Watcher
	created                                       time.Time
}

func (s *nativeSeat) close() {
	if s.control != nil {
		s.control.Close()
	}
	if s.watch != nil {
		s.watch.Close()
	}
}

type seatTransport interface {
	Create(context.Context, string, string, string, string, HarnessVariant) (*nativeSeat, error)
	Ready(context.Context, string, *nativeSeat, string) error
	Stage(context.Context, string, *nativeSeat, string, string) error
	WaitTurn(context.Context, *nativeSeat, string, string, func(codexTranscriptTurn) error) (codexTranscriptTurn, error)
	Snapshot(context.Context, *nativeSeat, string, string) (codexTranscriptTurn, error)
	End(context.Context, string, *nativeSeat) error
}

type realSeatTransport struct {
	command func(context.Context, string, *strings.Reader, ...string) (string, error)
	control func(context.Context, string, string) (*seatControl, error)
}

func (t realSeatTransport) run(ctx context.Context, socket string, input *strings.Reader, args ...string) (string, error) {
	if t.command != nil {
		return t.command(ctx, socket, input, args...)
	}
	return runTmuxCommand(ctx, socket, input, args...)
}
func (t realSeatTransport) openControl(ctx context.Context, socket, session string) (*seatControl, error) {
	if t.control != nil {
		return t.control(ctx, socket, session)
	}
	return openSeatControl(ctx, socket, session)
}

func (realSeatTransport) Snapshot(parent context.Context, s *nativeSeat, cwd, pointer string) (codexTranscriptTurn, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	if _, err := (realTmuxHarnessClient{}).DescribeActivePane(ctx, s.socket, s.paneID); err != nil {
		return codexTranscriptTurn{}, err
	}
	turn, _, err := findSeatTurn(s, cwd, pointer)
	return turn, err
}

func (t realSeatTransport) Create(ctx context.Context, socket, name, cwd, root string, v HarnessVariant) (*nativeSeat, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("absolute transcript root required for %s", v.ID)
	}
	watch, err := filewatch.New(root)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			watch.Close()
		}
	}()
	bin := "codex"
	if v.ID == "claude-code" {
		bin = "claude"
	}
	bin, err = exec.LookPath(bin)
	if err != nil {
		return nil, err
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		return nil, err
	}
	launch, err := v.RenderLaunch(bin)
	if err != nil {
		return nil, err
	}
	created := time.Now()
	out, err := t.run(ctx, socket, nil, "new-session", "-d", "-P", "-F", "#{session_id} #{pane_id}", "-e", "TERM=xterm-256color", "-s", name, "-c", cwd, launch)
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(out)
	if len(ids) != 2 || !strings.HasPrefix(ids[0], "$") || !strings.HasPrefix(ids[1], "%") {
		return nil, errors.New("created session identity unavailable; inspect owned session " + name)
	}
	ok = true
	return &nativeSeat{name: name, sessionID: ids[0], paneID: ids[1], root: root, socket: socket, variant: v, watch: watch, created: created}, nil
}

func (t realSeatTransport) Ready(ctx context.Context, socket string, s *nativeSeat, harness string) error {
	dead, err := t.run(ctx, socket, nil, "display-message", "-p", "-t", s.paneID, "#{pane_dead}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(dead) == "1" {
		return runExecutionError("dead_pane", "owned seat exited before readiness", "adapter", ErrDispatchDeadPane)
	}
	if s.control == nil {
		control, err := t.openControl(ctx, socket, s.sessionID)
		if err != nil {
			return err
		}
		s.control = control
	}
	trustAnswered := false
	for {
		text, err := t.run(ctx, socket, nil, "capture-pane", "-p", "-J", "-t", s.paneID, "-S", "-80")
		if err != nil {
			return err
		}
		// This executor is for explicitly trusted local workspaces. A first-use
		// trust dialog belongs to bootstrap, before any brief is staged.
		trust := strings.Contains(text, "Yes, I trust this folder") && harness == "claude-code"
		codexTrust := strings.Contains(text, "Do you trust the contents of this directory?") && strings.Contains(text, "1. Yes, continue") && harness == "openai-codex"
		if !trustAnswered && (trust || codexTrust) {
			// The dialog can paint before its keyboard handler is mounted, just
			// as bracketed paste can paint before its transaction closes.
			settle := time.NewTimer(tmuxPasteSettleDelay)
			select {
			case <-ctx.Done():
				settle.Stop()
				return ctx.Err()
			case <-settle.C:
			}
			if trust {
				if _, err := t.run(ctx, socket, nil, "send-keys", "-t", s.paneID, "Down"); err != nil {
					return err
				}
				if err := t.waitSeatPane(ctx, socket, s, func(text string) bool { return strings.Contains(text, "❯ Yes, I trust this folder") }); err != nil {
					return err
				}
			}
			if _, err := t.run(ctx, socket, nil, "send-keys", "-t", s.paneID, "Enter"); err != nil {
				return err
			}
			trustAnswered = true
		} else if tmuxPaneShowsHarnessReady(harness, text) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-s.control.events:
			if !ok {
				return errors.New("tmux control stream ended before readiness")
			}
		}
	}
}

func (t realSeatTransport) waitSeatPane(ctx context.Context, socket string, s *nativeSeat, accept func(string) bool) error {
	for {
		text, err := t.run(ctx, socket, nil, "capture-pane", "-p", "-J", "-t", s.paneID, "-S", "-80")
		if err != nil {
			return err
		}
		if accept(text) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-s.control.events:
			if !ok {
				return errors.New("tmux control stream ended before readiness or staging")
			}
		}
	}
}

func (t realSeatTransport) Stage(ctx context.Context, socket string, s *nativeSeat, dispatch, pointer string) error {
	buffer := safeTmuxBufferName(dispatch)
	if _, err := t.run(ctx, socket, strings.NewReader(pointer), "load-buffer", "-b", buffer, "-"); err != nil {
		return err
	}
	if _, err := t.run(ctx, socket, nil, "paste-buffer", "-p", "-b", buffer, "-t", s.paneID, "-d"); err != nil {
		return err
	}
	if err := t.waitSeatPane(ctx, socket, s, func(text string) bool { return strings.Contains(text, s.brief) }); err != nil {
		return err
	}
	settle := time.NewTimer(tmuxPasteSettleDelay)
	defer settle.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-settle.C:
	}
	_, err := t.run(ctx, socket, nil, "send-keys", "-t", s.paneID, "Enter")
	return err
}

func readSeatTurn(s *nativeSeat, path, cwd, pointer string) (codexTranscriptTurn, error) {
	return readLatestSeatTurn(s, path, cwd, pointer)
}

// Scan once before waiting. This also recovers events consumed while a peer or
// controller was running. Every candidate still requires an exact cwd and pointer.
func findSeatTurn(s *nativeSeat, cwd, pointer string) (codexTranscriptTurn, string, error) {
	var found codexTranscriptTurn
	selected := ""
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(s.created.Add(-2 * time.Second)) {
			return nil
		}
		turn, err := readSeatTurn(s, path, cwd, pointer)
		if err != nil {
			return err
		}
		if turn.Consumed {
			if selected != "" && selected != path {
				return errors.New("multiple native sessions consumed the dispatched pointer")
			}
			found, selected = turn, path
		}
		return nil
	})
	return found, selected, err
}

func (realSeatTransport) WaitTurn(ctx context.Context, s *nativeSeat, cwd, pointer string, consumed func(codexTranscriptTurn) error) (codexTranscriptTurn, error) {
	turn, selected, err := findSeatTurn(s, cwd, pointer)
	if err != nil {
		return turn, err
	}
	recorded := false
	for {
		if turn.Consumed && !recorded {
			if err := consumed(turn); err != nil {
				return turn, err
			}
			recorded = true
		}
		if turn.Complete {
			return turn, nil
		}
		select {
		case <-ctx.Done():
			return turn, ctx.Err()
		case event, ok := <-s.watch.Events:
			if !ok {
				return turn, errors.New("transcript watch ended")
			}
			if event.Err != nil {
				return turn, event.Err
			}
			if filepath.Ext(event.Path) != ".jsonl" || selected != "" && event.Path != selected {
				continue
			}
			candidate, err := readSeatTurn(s, event.Path, cwd, pointer)
			if err != nil {
				return turn, err
			}
			if candidate.Consumed {
				turn, selected = candidate, event.Path
			}
		}
	}
}

func (t realSeatTransport) End(ctx context.Context, socket string, s *nativeSeat) error {
	defer s.close()
	_, err := t.run(ctx, socket, nil, "kill-session", "-t", s.sessionID)
	return err
}

func (e *TmuxFormationExecutor) writeSeatBrief(prompt string) (string, string, error) {
	root := e.config.StateDir
	if root == "" {
		root = e.store.Workspace
	}
	path, err := writeBriefFile(root, "seat-*.md", prompt)
	if err != nil {
		return "", "", err
	}
	return path, seatPointer(path), nil
}

// writeBriefFile stores a rendered prompt under <root>/briefs.
func writeBriefFile(root, pattern, prompt string) (string, error) {
	dir := filepath.Join(root, "briefs")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func seatPointer(path string) string {
	return "Read the file " + path + " and execute it exactly; it is your whole brief."
}
