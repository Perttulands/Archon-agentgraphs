package formations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/filewatch"
)

type nativeSeat struct {
	name, sessionID, paneID, root, brief, pointer string
	socket                                        string
	// runID names the run whose completion sentinel ends a dispatch.
	runID string
	// pid is the harness process, and threads the Codex conversations it held
	// open when it took the pointer.
	pid     int
	threads map[string]bool
	variant HarnessVariant
	control *seatControl
	watch   *filewatch.Watcher
	created time.Time
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
	WaitInputClear(context.Context, string, *nativeSeat) error
	WaitTurn(context.Context, *nativeSeat, string, string, func(codexTranscriptTurn) error) (codexTranscriptTurn, error)
	Snapshot(context.Context, *nativeSeat, string, string) (codexTranscriptTurn, error)
	End(context.Context, string, *nativeSeat) error
}

type realSeatTransport struct {
	command func(context.Context, string, *strings.Reader, ...string) (string, error)
	control func(context.Context, string, string) (*seatControl, error)
	// proc is where process file descriptors are read; empty means /proc.
	proc string
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
	// A seat that has taken a pointer is past its startup screen, and its output
	// scrolls the harness banner out of the capture. It is ready when its agent
	// is idle with an empty input line.
	if s.pointer != "" {
		return t.WaitInputClear(ctx, socket, s)
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

// WaitInputClear waits until the seat's agent is idle and its input line is
// empty, so a paste never lands mid-turn or merges with text the operator has
// typed but not sent. It rechecks on every pane change until ctx ends, and
// fails at once when the pane has died.
func (t realSeatTransport) WaitInputClear(ctx context.Context, socket string, s *nativeSeat) error {
	if s.control == nil {
		control, err := t.openControl(ctx, socket, s.sessionID)
		if err != nil {
			return err
		}
		s.control = control
	}
	for {
		clear, err := t.inputClear(ctx, socket, s)
		if err != nil || clear {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case _, ok := <-s.control.events:
			if !ok {
				return errors.New("tmux control stream ended while waiting for an idle, empty input line")
			}
		}
	}
}

// inputClear reads the cursor, the screen with its styles, and the cursor
// again; a cursor that moved in between means the pane is changing.
func (t realSeatTransport) inputClear(ctx context.Context, socket string, s *nativeSeat) (bool, error) {
	const cursor = "#{cursor_x} #{cursor_y} #{pane_dead}"
	before, err := t.run(ctx, socket, nil, "display-message", "-p", "-t", s.paneID, cursor)
	if err != nil {
		return false, err
	}
	screen, err := t.run(ctx, socket, nil, "capture-pane", "-p", "-e", "-t", s.paneID)
	if err != nil {
		return false, err
	}
	after, err := t.run(ctx, socket, nil, "display-message", "-p", "-t", s.paneID, cursor)
	if err != nil {
		return false, err
	}
	var x, y, dead int
	if _, err := fmt.Sscanf(after, "%d %d %d", &x, &y, &dead); err != nil {
		return false, fmt.Errorf("seat cursor unavailable: %q", strings.TrimSpace(after))
	}
	if dead == 1 {
		return false, runExecutionError("dead_pane", "owned seat exited while waiting to paste", "adapter", ErrDispatchDeadPane)
	}
	return before == after && seatInputClear(s.variant.ID, screen, x, y), nil
}

func (t realSeatTransport) Stage(ctx context.Context, socket string, s *nativeSeat, dispatch, pointer string) error {
	if err := t.WaitInputClear(ctx, socket, s); err != nil {
		return err
	}
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

// replacementCheckInterval bounds how long a replaced conversation, such as
// after /clear or /new, goes unnoticed when no transcript changes.
const replacementCheckInterval = 5 * time.Second

// WaitTurn waits for the dispatch's native turn to complete. The operator may
// type into the seat meanwhile; the transcript readers tolerate those turns.
// It fails when the seat ends, or when the harness moves to another
// conversation.
func (t realSeatTransport) WaitTurn(ctx context.Context, s *nativeSeat, cwd, pointer string, consumed func(codexTranscriptTurn) error) (codexTranscriptTurn, error) {
	turn, selected, err := findSeatTurn(s, cwd, pointer)
	if err != nil {
		return turn, err
	}
	var seatEvents <-chan struct{}
	if s.control != nil {
		seatEvents = s.control.events
	}
	ticker := time.NewTicker(replacementCheckInterval)
	defer ticker.Stop()
	recorded, check := false, false
	for {
		if turn.Consumed && !recorded {
			if err := consumed(turn); err != nil {
				return turn, err
			}
			recorded = true
			t.recordConversation(ctx, s, selected)
		}
		if turn.Complete {
			return turn, nil
		}
		if check && turn.Consumed {
			if err := t.conversationReplaced(ctx, s, turn); err != nil {
				return turn, err
			}
		}
		check = false
		select {
		case <-ctx.Done():
			return turn, ctx.Err()
		case _, ok := <-seatEvents:
			if !ok {
				return turn, runExecutionError("dead_pane", "owned seat ended before its turn completed", "adapter", ErrDispatchDeadPane)
			}
		case <-ticker.C:
			check = true
		case event, ok := <-s.watch.Events:
			if !ok {
				return turn, errors.New("transcript watch ended")
			}
			if event.Err != nil {
				return turn, event.Err
			}
			if filepath.Ext(event.Path) != ".jsonl" {
				continue
			}
			if selected != "" && event.Path != selected {
				// Another transcript changed; this seat may have started it.
				check = true
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

// recordConversation finds the seat's harness process and notes the
// conversations it holds open when it takes the pointer, so a conversation it
// opens later stands out. Without a process, replacement goes unchecked.
func (t realSeatTransport) recordConversation(ctx context.Context, s *nativeSeat, selected string) {
	if s.pid == 0 && s.paneID != "" {
		if out, err := t.run(ctx, s.socket, nil, "display-message", "-p", "-t", s.paneID, "#{pane_pid}"); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(out)); err == nil && pid > 0 {
				s.pid = pid
			}
		}
	}
	if s.variant.ID != "openai-codex" || s.pid == 0 {
		return
	}
	s.threads = map[string]bool{selected: true}
	for _, path := range t.openCodexThreads(s) {
		s.threads[path] = true
	}
}

// conversationReplaced fails a dispatch whose seat moved to another
// conversation: Claude Code records its current session for each process, and
// Codex opens the rollout of a new or resumed thread.
func (t realSeatTransport) conversationReplaced(ctx context.Context, s *nativeSeat, turn codexTranscriptTurn) error {
	if s.pid == 0 {
		return nil
	}
	switch s.variant.ID {
	case "claude-code":
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(s.root), "sessions", strconv.Itoa(s.pid)+".json"))
		if err != nil {
			return nil
		}
		var current struct {
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(raw, &current) == nil && current.SessionID != "" && turn.SessionID != "" && current.SessionID != turn.SessionID {
			return runExecutionError("conversation_replaced", "the operator replaced the Claude conversation (/clear or /resume) before the dispatch completed", "adapter", nil)
		}
	case "openai-codex":
		for _, path := range t.openCodexThreads(s) {
			if !s.threads[path] {
				return runExecutionError("conversation_replaced", "the operator replaced the Codex conversation (/new or /resume) before the dispatch completed", "adapter", nil)
			}
		}
	}
	return nil
}

// openCodexThreads lists the user-thread rollouts under the transcript root
// that the seat's Codex process holds open. Threads Codex starts for its own
// subagents are not the operator's conversation.
func (t realSeatTransport) openCodexThreads(s *nativeSeat) []string {
	proc := t.proc
	if proc == "" {
		proc = "/proc"
	}
	fds := filepath.Join(proc, strconv.Itoa(s.pid), "fd")
	entries, err := os.ReadDir(fds)
	if err != nil {
		return nil
	}
	var threads []string
	for _, entry := range entries {
		path, err := os.Readlink(filepath.Join(fds, entry.Name()))
		if err != nil || filepath.Ext(path) != ".jsonl" || !strings.HasPrefix(path, s.root+string(filepath.Separator)) {
			continue
		}
		if codexUserThread(path) {
			threads = append(threads, path)
		}
	}
	return threads
}

// codexUserThread reports a rollout whose session_meta names a thread the user
// started, or names no source at all.
func codexUserThread(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	line, err := bufio.NewReader(f).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return false
	}
	var meta struct {
		Type    string `json:"type"`
		Payload struct {
			ThreadSource string `json:"thread_source"`
		} `json:"payload"`
	}
	return json.Unmarshal(bytes.TrimSpace(line), &meta) == nil && meta.Type == "session_meta" && (meta.Payload.ThreadSource == "" || meta.Payload.ThreadSource == "user")
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

// seatInputPrompt is the glyph that starts each harness's input line.
var seatInputPrompt = map[string]string{
	"claude-code":  "❯",
	"openai-codex": "›",
}

// A Claude Code spinner reads "✶ Puzzling… (4s · ↓ 60 tokens)" while the agent
// works and "✻ Crunched for 14s" once it is done.
var claudeWorkingLine = regexp.MustCompile(`…\s*\(\d+[smh]`)

var ansiSGR = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// seatInputClear reports whether a harness waits idle with nothing typed, from
// a capture of the visible pane with its styles (capture-pane -p -e) and the
// cursor position. The input line is the prompt line under the cursor, and
// typed text starts in the cell after the prompt glyph and its space. The line
// is empty when the cursor sits in that cell and the cell is blank or dim, as
// Codex's "Ask Codex to do anything" placeholder is. Text the operator typed
// before moving the cursor back fills that cell, while decorations further
// along the line, such as the stars Codex animates in blank cells, do not
// count. The harness is busy when a working line sits just above the input
// box, as tmuxPaneShowsAgentWorking reads it.
func seatInputClear(harness, screen string, cursorX, cursorY int) bool {
	prompt := seatInputPrompt[harness]
	lines := strings.Split(screen, "\n")
	if prompt == "" || cursorX != 2 || cursorY < 0 || cursorY >= len(lines) {
		return false
	}
	cells := styledCells(lines[cursorY])
	if len(cells) == 0 || string(cells[0].r) != prompt {
		return false
	}
	if len(cells) > 2 && !cells[2].dim && !blankCell(cells[2].r) {
		return false
	}
	seen := 0
	for index := cursorY - 1; index >= 0 && seen < 2; index-- {
		above := strings.TrimSpace(ansiSGR.ReplaceAllString(lines[index], ""))
		if above == "" || strings.Trim(above, "─━") == "" {
			continue
		}
		seen++
		if tmuxPaneShowsAgentWorking(harness, above) {
			return false
		}
	}
	return true
}

// styledCell is one character of a styled pane line and whether it is dim.
type styledCell struct {
	r   rune
	dim bool
}

// styledCells splits a line captured with its styles into characters, tracking
// the dim attribute (SGR 2) across the line's escapes.
func styledCells(line string) []styledCell {
	var cells []styledCell
	dim := false
	rest := line
	for rest != "" {
		match := ansiSGR.FindStringSubmatchIndex(rest)
		text := rest
		if match != nil {
			text = rest[:match[0]]
		}
		for _, r := range text {
			cells = append(cells, styledCell{r: r, dim: dim})
		}
		if match == nil {
			break
		}
		params := strings.Split(rest[match[2]:match[3]], ";")
		for index := 0; index < len(params); index++ {
			switch code, _ := strconv.Atoi(params[index]); code {
			case 0, 22:
				dim = false
			case 2:
				dim = true
			case 38, 48, 58:
				// An extended colour's own parameters are not attributes:
				// 5;n for a palette index, 2;r;g;b for RGB.
				if index+1 < len(params) && params[index+1] == "5" {
					index += 2
				} else if index+1 < len(params) && params[index+1] == "2" {
					index += 4
				}
			}
		}
		rest = rest[match[1]:]
	}
	return cells
}

// blankCell reports a cell that holds no typed text: a space, a no-break space,
// or a braille pattern, as Codex's star animation draws in blank cells.
func blankCell(r rune) bool {
	return r == ' ' || r == '\u00a0' || r >= 0x2800 && r <= 0x28ff
}
