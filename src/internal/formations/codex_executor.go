package formations

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Perttulands/chrote-agent-formations/internal/core"
	"github.com/Perttulands/chrote-agent-formations/internal/filewatch"
)

// CodexSeatConfig is explicit host configuration, never authored board data.
type CodexSeatConfig struct {
	Socket, Cwd, StateDir, TranscriptRoot, Model, Effort, Mission string
	Timeout                                                       time.Duration
}

type CodexSeatExecutor struct {
	store    *Store
	personas *PersonaStore
	config   CodexSeatConfig
}

func NewCodexSeatExecutor(store *Store, personas *PersonaStore, config CodexSeatConfig) *CodexSeatExecutor {
	return &CodexSeatExecutor{store, personas, config}
}

// ExecuteFormation runs a single owned Codex seat. Graph edges provide ordered
// handoff between seats; peer and leader schedules retain their separate adapter.
func (e *CodexSeatExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	return e.ExecuteFormationContext(context.Background(), req)
}

func (e *CodexSeatExecutor) ExecuteFormationContext(parent context.Context, req FormationExecution) (FormationExecutionResult, error) {
	c := e.config
	if req.Formation.Type != FormationTypeSolo || len(req.Formation.Slots) != 1 {
		return FormationExecutionResult{}, errors.New("standalone Codex seats require a solo formation with one slot")
	}
	if c.Timeout <= 0 {
		return FormationExecutionResult{}, errors.New("positive seat timeout required")
	}
	if !filepath.IsAbs(core.TmuxBin()) {
		return FormationExecutionResult{}, errors.New("CHROTE_TMUX_BIN must pin an absolute guarded wrapper")
	}
	legacy := NewTmuxFormationExecutor(e.store, e.personas, TmuxExecutorConfig{Socket: c.Socket, Cwd: c.Cwd, Roots: []string{c.Cwd, c.StateDir}, SessionPrefix: "form-", Harnesses: []string{"openai-codex"}, TimeoutSeconds: int(c.Timeout.Seconds()), OutputCapBytes: 1 << 20})
	legacy.client = existingServerClient{}
	// Never lazy-start the operator's socket. Existing socket ownership and identity
	// are verified by the shared adapter before any session is created.
	if _, err := os.Stat(c.Socket); err != nil {
		return FormationExecutionResult{}, err
	}
	if err := legacy.validateConfiguredBoundary(); err != nil {
		return FormationExecutionResult{}, err
	}
	ctx, cancel := context.WithTimeout(parent, c.Timeout)
	defer cancel()
	watch, err := filewatch.New(c.TranscriptRoot)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	defer watch.Close()
	slot := req.Formation.Slots[0]
	card, err := e.personas.ReadPersona(slot.AgentID)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	name := "form-" + sanitizeSessionComponent(c.Mission) + "-" + sanitizeSessionComponent(slot.ID)
	if !safeTmuxSessionName(name) {
		return FormationExecutionResult{}, errors.New("invalid mission or slot session name")
	}
	launch := "exec env TERM=xterm-256color codex --model " + shellQuote(c.Model) + " -c " + shellQuote("model_reasoning_effort=\""+c.Effort+"\"") + " -c check_for_update_on_startup=false --dangerously-bypass-approvals-and-sandbox -C " + shellQuote(c.Cwd)
	created, err := runTmuxCommand(ctx, c.Socket, nil, "new-session", "-d", "-P", "-F", "#{session_id} #{pane_id}", "-s", name, "-c", c.Cwd, launch)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	ids := strings.Fields(created)
	if len(ids) != 2 {
		return FormationExecutionResult{}, errors.New("created session identity unavailable; inspect owned session " + name)
	}
	session, pane := ids[0], ids[1]
	// Cleanup uses the immutable id returned by creation, never a name lookup.
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		outcome := "left_socket_changed"
		if legacy.validatePinnedTmuxSocket() == nil {
			_, err := runTmuxCommand(cleanupCtx, c.Socket, nil, "kill-session", "-t", session)
			outcome = "ended"
			if err != nil {
				outcome = "left_cleanup_failed"
			}
		}
		_ = e.store.AppendRunEvent(req.RunID, RunEvent{Type: "seat_cleanup", NodeID: req.NodeID, SlotID: slot.ID, Data: map[string]any{"sessionName": name, "outcome": outcome}})
	}()
	if err := e.store.AppendRunEvent(req.RunID, RunEvent{Type: "seat_created", NodeID: req.NodeID, SlotID: slot.ID, Data: map[string]any{"sessionName": name, "sessionId": session, "paneId": pane, "model": c.Model, "effort": c.Effort}}); err != nil {
		return FormationExecutionResult{}, err
	}
	control, err := openSeatControl(ctx, c.Socket, session)
	if err != nil {
		return FormationExecutionResult{}, err
	}
	defer control.Close()
	for {
		captured, err := runTmuxCommand(ctx, c.Socket, nil, "capture-pane", "-p", "-t", pane, "-S", "-80")
		if err != nil {
			return FormationExecutionResult{}, err
		}
		if tmuxPaneShowsHarnessReady("openai-codex", captured) {
			break
		}
		select {
		case <-ctx.Done():
			return FormationExecutionResult{}, ctx.Err()
		case _, ok := <-control.events:
			if !ok {
				return FormationExecutionResult{}, errors.New("tmux control stream ended before readiness")
			}
		}
	}
	prompt := legacy.renderPromptWithContext(req, slot, *card, HarnessVariant{ID: "openai-codex"}, "solo", outputContractExtraLines(req.Formation))
	briefDir := filepath.Join(c.StateDir, "briefs")
	if err := os.MkdirAll(briefDir, 0700); err != nil {
		return FormationExecutionResult{}, err
	}
	brief, err := os.CreateTemp(briefDir, "seat-*.md")
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if _, err := brief.WriteString(prompt); err != nil {
		brief.Close()
		return FormationExecutionResult{}, err
	}
	if err := brief.Close(); err != nil {
		return FormationExecutionResult{}, err
	}
	pointer := "Read the file " + brief.Name() + " and execute it exactly; it is your whole brief."
	dispatcher := NewSlotDispatcher(e.store, nil)
	lease, err := dispatcher.DispatchSlot(req.RunID, SlotDispatchRequest{NodeID: req.NodeID, SlotID: slot.ID, AgentID: slot.AgentID, Harness: "openai-codex", SessionStem: name, SessionRef: "tmux:" + name, Prompt: prompt, Phase: "solo", Attempt: req.Attempt})
	if err != nil {
		return FormationExecutionResult{}, err
	}
	if err := legacy.validatePinnedTmuxSocket(); err != nil {
		return FormationExecutionResult{}, err
	}
	buffer := safeTmuxBufferName(lease.DispatchID)
	if _, err := runTmuxCommand(ctx, c.Socket, strings.NewReader(pointer), "load-buffer", "-b", buffer, "-"); err != nil {
		return FormationExecutionResult{}, err
	}
	if _, err := runTmuxCommand(ctx, c.Socket, nil, "paste-buffer", "-p", "-b", buffer, "-t", pane, "-d"); err != nil {
		return FormationExecutionResult{}, err
	}
	// Inspect the staged single-line pointer, then submit once.
	for {
		staged, err := runTmuxCommand(ctx, c.Socket, nil, "capture-pane", "-p", "-J", "-t", pane, "-S", "-80")
		if err != nil {
			return FormationExecutionResult{}, err
		}
		if strings.Contains(staged, brief.Name()) {
			break
		}
		select {
		case <-ctx.Done():
			return FormationExecutionResult{}, ctx.Err()
		case _, ok := <-control.events:
			if !ok {
				return FormationExecutionResult{}, errors.New("tmux control stream ended before prompt staging")
			}
		}
	}
	// Codex can render the pointer while its bracketed-paste transaction is still
	// open. One bounded settle delay keeps the submit out of that transaction.
	settle := time.NewTimer(tmuxPasteSettleDelay)
	select {
	case <-ctx.Done():
		settle.Stop()
		return FormationExecutionResult{}, ctx.Err()
	case <-settle.C:
	}
	if _, err := runTmuxCommand(ctx, c.Socket, nil, "send-keys", "-t", pane, "Enter"); err != nil {
		return FormationExecutionResult{}, err
	}
	selected := ""
	for {
		select {
		case <-ctx.Done():
			return FormationExecutionResult{}, ctx.Err()
		case event, ok := <-watch.Events:
			if !ok {
				return FormationExecutionResult{}, errors.New("transcript watch ended")
			}
			if event.Err != nil {
				return FormationExecutionResult{}, event.Err
			}
			if filepath.Ext(event.Path) != ".jsonl" || selected != "" && selected != event.Path {
				continue
			}
			turn, err := readCodexTurn(event.Path, c.Cwd, pointer)
			if err != nil {
				return FormationExecutionResult{}, err
			}
			if !turn.Consumed {
				continue
			}
			if selected == "" {
				selected = event.Path
				if err := e.store.AppendRunEvent(req.RunID, RunEvent{Type: "seat_prompt_consumed", NodeID: req.NodeID, SlotID: slot.ID, Data: map[string]any{"sessionName": name, "nativeSessionId": turn.SessionID, "dispatchId": lease.DispatchID}}); err != nil {
					return FormationExecutionResult{}, err
				}
			}
			if !turn.Complete {
				continue
			}
			if turn.Model != c.Model || turn.Effort != c.Effort {
				return FormationExecutionResult{}, fmt.Errorf("seat model/effort mismatch: %s/%s", turn.Model, turn.Effort)
			}
			result, err := legacy.formationResultFromText(req, "", turn.Text)
			if err != nil {
				return FormationExecutionResult{}, err
			}
			if err := dispatcher.CompleteFromCapture(req.RunID, lease.DispatchID, turn.Text); err != nil {
				return FormationExecutionResult{}, err
			}
			return result, nil
		}
	}
}

type seatControl struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan struct{}
}

type existingServerClient struct{ realTmuxHarnessClient }

func (existingServerClient) StartKeeper(context.Context, string, string) error {
	return errors.New("standalone seats require an already running tmux server")
}

func openSeatControl(ctx context.Context, socket, session string) (*seatControl, error) {
	cmd := exec.CommandContext(ctx, core.TmuxBin(), "-S", socket, "-C", "attach-session", "-t", session)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, err
	}
	c := &seatControl{cmd: cmd, stdin: stdin, events: make(chan struct{}, 1)}
	go func() {
		defer close(c.events)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 65536), 4<<20)
		for scanner.Scan() {
			select {
			case c.events <- struct{}{}:
			default:
			}
		}
	}()
	// This is the only sizing operation, once per newly created session.
	if _, err := io.WriteString(stdin, "refresh-client -C 160,48\n"); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}
func (c *seatControl) Close() { c.stdin.Close(); c.cmd.Wait() }
