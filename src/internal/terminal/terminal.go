// Package terminal observes one durably identified Formations seat. Its PTY
// relay is adapted from CHROTE's native terminal transport; input and sizing
// ownership are deliberately absent from this observer.
package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"github.com/gorilla/websocket"
)

type Config struct{ Socket, TmuxBin string }
type Target struct{ SessionID, PaneID, SocketIdentity string }
type Status struct {
	State         string
	Reason        string
	Columns, Rows int
}
type Observer struct{ config Config }

func New(config Config) (*Observer, error) {
	if !filepath.IsAbs(config.Socket) || !filepath.IsAbs(config.TmuxBin) {
		return nil, errors.New("terminal socket and tmux binary must be absolute paths")
	}
	return &Observer{config: config}, nil
}

var sessionIDPattern = regexp.MustCompile(`^\$[0-9]+$`)
var paneIDPattern = regexp.MustCompile(`^%[0-9]+$`)

func (o *Observer) verifySocket(target Target) error {
	if !sessionIDPattern.MatchString(target.SessionID) || !paneIDPattern.MatchString(target.PaneID) || target.SocketIdentity == "" {
		return errors.New("original seat identity was not recorded")
	}
	identity, err := core.SocketIdentity(o.config.Socket)
	if err != nil || identity != target.SocketIdentity {
		return errors.New("original tmux socket is unavailable or has changed")
	}
	resolved, err := filepath.EvalSymlinks(o.config.Socket)
	if err != nil || resolved != filepath.Clean(o.config.Socket) {
		return errors.New("tmux socket path is no longer stable")
	}
	return nil
}

// Probe targets an immutable session ID; it never enumerates or adopts sessions.
func (o *Observer) Probe(parent context.Context, target Target) Status {
	if err := o.verifySocket(target); err != nil {
		return Status{State: "unavailable", Reason: err.Error()}
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, o.config.TmuxBin, "-S", o.config.Socket, "display-message", "-p", "-t", target.SessionID, "#{session_id}\t#{pane_id}\t#{pane_dead}\t#{window_width}\t#{window_height}\t#{status}")
	cmd.Env = attachEnv()
	raw, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return Status{State: "unavailable", Reason: "tmux did not answer the seat probe"}
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return Status{State: "unavailable", Reason: "configured tmux command could not run"}
		}
		return Status{State: "missing", Reason: "owned session is no longer present"}
	}
	if err := o.verifySocket(target); err != nil {
		return Status{State: "unavailable", Reason: err.Error()}
	}
	fields := strings.Split(strings.TrimSpace(string(raw)), "\t")
	if len(fields) != 6 || fields[0] != target.SessionID || fields[1] != target.PaneID {
		return Status{State: "missing", Reason: "original seat pane is no longer active"}
	}
	if fields[2] == "1" {
		return Status{State: "ended", Reason: "seat process has ended"}
	}
	cols, colErr := strconv.Atoi(fields[3])
	rows, rowErr := strconv.Atoi(fields[4])
	// tmux subtracts its status rows from the attached client's PTY. Match
	// the full native client grid, otherwise even ignore-size can shrink a
	// window by one row when this is its only attached client.
	statusRows := 0
	switch fields[5] {
	case "on":
		statusRows = 1
	case "off":
	default:
		var err error
		statusRows, err = strconv.Atoi(fields[5])
		if err != nil || statusRows < 0 || statusRows > 5 {
			return Status{State: "unavailable", Reason: "tmux status dimensions are unavailable"}
		}
	}
	rows += statusRows
	if colErr != nil || rowErr != nil || cols < 1 || rows < 1 || cols > 65535 || rows > 65535 {
		return Status{State: "unavailable", Reason: "seat dimensions are unavailable"}
	}
	return Status{State: "live", Columns: cols, Rows: rows}
}

// Serve requires the caller to resolve and authorize the exact run seat first.
// ctx is canceled on daemon shutdown, including hijacked WebSockets that HTTP
// Server.Shutdown does not manage. No browser frame can write to the PTY.
func (o *Observer) Serve(ctx context.Context, w http.ResponseWriter, r *http.Request, target Target) {
	hasTTY := false
	for _, protocol := range websocket.Subprotocols(r) {
		if protocol == "tty" {
			hasTTY = true
		}
	}
	if !hasTTY {
		core.WriteError(w, 400, "TERMINAL_PROTOCOL", "tty WebSocket subprotocol required")
		return
	}
	upgrader := websocket.Upgrader{Subprotocols: []string{"tty"}}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer conn.Close()
	var finishOnce sync.Once
	finish := func(code int, reason string) {
		finishOnce.Do(func() {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
			_ = conn.Close()
			cancel()
		})
	}
	go func() { <-ctx.Done(); finish(websocket.CloseGoingAway, "terminal observer stopped") }()
	conn.SetReadLimit(4096)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return
	}
	var handshake struct {
		Columns int `json:"columns"`
		Rows    int `json:"rows"`
	}
	if err := json.Unmarshal(raw, &handshake); err != nil || handshake.Columns < 1 || handshake.Rows < 1 || handshake.Columns > 65535 || handshake.Rows > 65535 {
		finish(websocket.ClosePolicyViolation, "invalid terminal handshake")
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	status := o.Probe(ctx, target)
	if status.State != "live" {
		finish(websocket.CloseNormalClosure, status.Reason)
		return
	}
	if err := o.attach(ctx, conn, target, status, finish); err != nil {
		finish(websocket.CloseNormalClosure, "terminal attach unavailable")
	}
}

func (o *Observer) attach(ctx context.Context, conn *websocket.Conn, target Target, size Status, finish func(int, string)) error {
	p, err := openPTY()
	if err != nil {
		return err
	}
	defer p.master.Close()
	defer p.slave.Close()
	// Preserve the native grid even when this is the first/only attached client.
	if err := p.resize(size.Columns, size.Rows); err != nil {
		return err
	}
	if err := o.verifySocket(target); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, o.config.TmuxBin, "-S", o.config.Socket, "attach-session", "-r", "-f", "ignore-size", "-t", target.SessionID)
	cmd.Env = attachEnv()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = p.slave, p.slave, p.slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = p.slave.Close()
	defer func() {
		_ = p.master.Close()
		kill := time.AfterFunc(2*time.Second, func() { _ = cmd.Process.Kill() })
		defer kill.Stop()
		_ = cmd.Wait()
	}()
	if err := o.verifySocket(target); err != nil {
		return err
	}
	flow := &flowGate{}
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buffer := make([]byte, 32*1024)
		for {
			if !flow.wait(ctx) {
				return
			}
			n, err := p.master.Read(buffer)
			if n > 0 {
				frame := make([]byte, n+1)
				frame[0] = '0'
				copy(frame[1:], buffer[:n])
				_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
				if sendErr := conn.WriteMessage(websocket.BinaryMessage, frame); sendErr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		finish(websocket.CloseNormalClosure, "terminal ended")
	}()
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		if len(message) != 1 || (message[0] != '2' && message[0] != '3') {
			finish(websocket.ClosePolicyViolation, "terminal is view-only; input and resize are forbidden")
			break
		}
		if message[0] == '2' {
			flow.pause()
		} else {
			flow.resume()
		}
	}
	finish(websocket.CloseNormalClosure, "terminal viewer closed")
	_ = p.master.Close()
	flow.resume()
	<-outputDone
	return nil
}

// Flow control applies only to reading output; it cannot send terminal input.
type flowGate struct {
	mu      sync.Mutex
	resumed chan struct{}
}

func (g *flowGate) wait(ctx context.Context) bool {
	g.mu.Lock()
	resumed := g.resumed
	g.mu.Unlock()
	if resumed == nil {
		return ctx.Err() == nil
	}
	select {
	case <-resumed:
		return ctx.Err() == nil
	case <-ctx.Done():
		return false
	}
}
func (g *flowGate) pause() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.resumed == nil {
		g.resumed = make(chan struct{})
	}
}
func (g *flowGate) resume() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.resumed != nil {
		close(g.resumed)
		g.resumed = nil
	}
}

func attachEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	lang := "C.UTF-8"
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "TMUX=") || strings.HasPrefix(entry, "TMUX_PANE=") || strings.HasPrefix(entry, "TMUX_TMPDIR=") || strings.HasPrefix(entry, "TERM=") {
			continue
		}
		if strings.HasPrefix(entry, "LANG=") {
			value := strings.ToLower(strings.TrimPrefix(entry, "LANG="))
			if strings.HasSuffix(value, ".utf-8") || strings.HasSuffix(value, ".utf8") {
				lang = strings.TrimPrefix(entry, "LANG=")
			}
			continue
		}
		env = append(env, entry)
	}
	return append(env, "TERM=xterm-256color", fmt.Sprintf("LANG=%s", lang))
}
