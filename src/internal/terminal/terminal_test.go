package terminal

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"github.com/gorilla/websocket"
)

func scratchSeat(t *testing.T) (*Observer, Target, func(...string) string) {
	t.Helper()
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("scratch terminal test requires tmux")
	}
	root := t.TempDir()
	socket := filepath.Join(root, "socket")
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, append([]string{"-S", socket}, args...)...)
		cmd.Env = attachEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("scratch tmux %v: %s %v", args, out, err)
		}
		return strings.TrimSpace(string(out))
	}
	// Ending every session ends the scratch server. Host tmux guards may refuse
	// kill-server, which would otherwise leave the server running. Registered
	// before the server starts, so a failed setup cannot leak it either.
	t.Cleanup(func() {
		tmux := func(args ...string) string {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			out, _ := exec.CommandContext(ctx, bin, append([]string{"-S", socket}, args...)...).Output()
			return string(out)
		}
		for _, session := range strings.Fields(tmux("list-sessions", "-F", "#{session_id}")) {
			tmux("kill-session", "-t", session)
		}
		// No scratch server may outlive its test.
		deadline := time.Now().Add(3 * time.Second)
		for {
			conn, err := net.Dial("unix", socket)
			if err != nil {
				return
			}
			conn.Close()
			if time.Now().After(deadline) {
				t.Errorf("scratch tmux server on %s outlived its test", socket)
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	})
	identity := strings.Fields(run("new-session", "-d", "-P", "-F", "#{session_id} #{pane_id}", "-s", "owned-proof", "-x", "120", "-y", "40", "sh", "-c", "printf 'NATIVE_TERMINAL_PROOF\\n'; exec cat"))
	socketIdentity, err := core.SocketIdentity(socket)
	if err != nil {
		t.Fatal(err)
	}
	observer, err := New(Config{Socket: socket, TmuxBin: bin})
	if err != nil {
		t.Fatal(err)
	}
	return observer, Target{SessionID: identity[0], PaneID: identity[1], SocketIdentity: socketIdentity}, run
}

func readProof(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var output strings.Builder
	for !strings.Contains(output.String(), "NATIVE_TERMINAL_PROOF") {
		kind, frame, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("terminal output %q: %v", output.String(), err)
		}
		if kind != websocket.BinaryMessage || len(frame) == 0 || frame[0] != '0' {
			t.Fatalf("invalid output frame %d %q", kind, frame)
		}
		output.Write(frame[1:])
	}
}

func TestScratchTerminalForwardsInputAndSizesOnlyItsView(t *testing.T) {
	observer, target, run := scratchSeat(t)
	initial := run("display-message", "-p", "-t", target.SessionID, "#{window_width}x#{window_height}")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { observer.Serve(ctx, w, r, target) }))
	defer server.Close()
	dial := func() *websocket.Conn {
		dialer := websocket.Dialer{Subprotocols: []string{"tty"}}
		conn, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteJSON(map[string]int{"columns": 33, "rows": 8}); err != nil {
			t.Fatal(err)
		}
		readProof(t, conn)
		return conn
	}
	send := func(conn *websocket.Conn, frame string) {
		t.Helper()
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte(frame)); err != nil {
			t.Fatal(err)
		}
	}
	// Output keeps flowing while the test acts on the pane.
	drain := func(conn *websocket.Conn) {
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
	}

	conn := dial()
	drain(conn)
	flags := run("list-clients", "-t", target.SessionID, "-F", "#{client_flags}")
	if strings.Contains(flags, "read-only") || !strings.Contains(flags, "ignore-size") {
		t.Fatalf("terminal client flags: %s", flags)
	}
	// The seat runs cat, which echoes what reaches it.
	send(conn, "0TYPED_BY_THE_OPERATOR\r")
	waitFor(t, func() bool {
		return strings.Count(run("capture-pane", "-p", "-t", target.PaneID), "TYPED_BY_THE_OPERATOR") == 2
	})
	// With nobody else sizing the window, a resize keeps the view on the seat's
	// grid, because tmux would otherwise size the window to this client.
	send(conn, `1{"columns":52,"rows":12}`)
	send(conn, "0AFTER_THE_FIRST_RESIZE\r")
	waitFor(t, func() bool {
		return strings.Contains(run("capture-pane", "-p", "-t", target.PaneID), "AFTER_THE_FIRST_RESIZE")
	})
	if got := run("display-message", "-p", "-t", target.SessionID, "#{window_width}x#{window_height}"); got != initial {
		t.Fatalf("terminal resized the seat window: before=%s after=%s", initial, got)
	}
	// While another client sizes the window, as the executor's control client
	// does, a resize sizes only this view.
	sizing := exec.Command(observer.config.TmuxBin, "-S", observer.config.Socket, "-C", "attach-session", "-t", target.SessionID)
	sizing.Env = attachEnv()
	stdin, err := sizing.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := sizing.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); _ = sizing.Wait() }()
	if _, err := stdin.Write([]byte("refresh-client -C 120,40\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		return strings.Contains(run("list-clients", "-t", target.SessionID, "-F", "#{client_flags}"), "control-mode")
	})
	send(conn, `1{"columns":52,"rows":12}`)
	waitFor(t, func() bool {
		return strings.Contains(run("list-clients", "-t", target.SessionID, "-F", "#{client_flags} #{client_width}x#{client_height}"), "ignore-size,UTF-8 52x12")
	})
	if got := run("display-message", "-p", "-t", target.SessionID, "#{window_width}x#{window_height}"); got != initial {
		t.Fatalf("terminal resized the seat window: before=%s after=%s", initial, got)
	}
	// Unknown and malformed frames are ignored; the connection keeps working.
	send(conn, "4")
	send(conn, "1{not json")
	send(conn, "0STILL_CONNECTED\r")
	waitFor(t, func() bool {
		return strings.Contains(run("capture-pane", "-p", "-t", target.PaneID), "STILL_CONNECTED")
	})
	conn.Close()
	waitFor(t, func() bool {
		return !strings.Contains(run("list-clients", "-t", target.SessionID, "-F", "#{client_flags}"), "ignore-size")
	})
	if status := observer.Probe(context.Background(), target); status.State != "live" {
		t.Fatalf("closing the terminal changed seat availability: %+v", status)
	}

	// A paused reader must also detach promptly on daemon shutdown.
	conn = dial()
	send(conn, "2")
	started := time.Now()
	cancel()
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseGoingAway) {
				t.Fatal(err)
			}
			break
		}
	}
	conn.Close()
	if time.Since(started) > 3*time.Second {
		t.Fatal("shutdown did not close the terminal promptly")
	}
	if observer.Probe(context.Background(), target).State != "live" {
		t.Fatal("shutdown ended seat")
	}
}

// Each seat state is reached deterministically. The scratch server holds a
// keeper session because tmux exits a server whose last session ends, and a
// probe then correctly reports the unverifiable socket as unavailable.
func TestSeatProbeRejectsMissingOrReplacedIdentity(t *testing.T) {
	observer, target, run := scratchSeat(t)
	run("new-session", "-d", "-s", "keeper", "sh", "-c", "exec cat")
	original := target
	target.SocketIdentity = ""
	if s := observer.Probe(context.Background(), target); s.State != "unavailable" {
		t.Fatal(s)
	}
	target = original
	target.SocketIdentity = "different"
	if s := observer.Probe(context.Background(), target); s.State != "unavailable" {
		t.Fatal(s)
	}
	target = original
	target.SessionID = "$999999"
	if s := observer.Probe(context.Background(), target); s.State != "missing" {
		t.Fatal(s)
	}

	// The seat process exits and remain-on-exit keeps its dead pane.
	run("set-option", "-w", "-t", original.SessionID, "remain-on-exit", "on")
	run("send-keys", "-t", original.PaneID, "C-d")
	waitFor(t, func() bool { return run("display-message", "-p", "-t", original.PaneID, "#{pane_dead}") == "1" })
	if s := observer.Probe(context.Background(), original); s.State != "ended" {
		t.Fatal(s)
	}

	// The pane is gone while the server lives on.
	run("kill-pane", "-t", original.PaneID)
	if s := observer.Probe(context.Background(), original); s.State != "missing" {
		t.Fatal(s)
	}

	// The server exits with its last session; its identity can no longer be proven.
	run("kill-session", "-t", "keeper")
	waitFor(t, func() bool {
		conn, err := net.Dial("unix", observer.config.Socket)
		if err == nil {
			conn.Close()
		}
		return err != nil
	})
	if s := observer.Probe(context.Background(), original); s.State != "unavailable" {
		t.Fatal(s)
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
