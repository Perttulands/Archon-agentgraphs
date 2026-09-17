package formations

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
	"github.com/Perttulands/Archon-agentgraphs/internal/terminal"
	"github.com/gorilla/websocket"
)

// A seat kept on call has no clients once its control client leaves. The
// operator's terminal is then the only viewer, and its resizes must size its
// own view while the seat window keeps the grid the executor gave it (form-de9).
func TestScratchSeatWindowKeepsItsSizeForALoneViewer(t *testing.T) {
	bin, err := exec.LookPath(core.TmuxBin())
	if err != nil {
		t.Skip("scratch seat window test requires tmux")
	}
	bin, err = filepath.Abs(bin)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	socket := filepath.Join(root, "socket")
	tmux := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := runTmuxCommand(ctx, socket, nil, args...)
		if err != nil {
			t.Fatalf("scratch tmux %v: %v", args, err)
		}
		return strings.TrimSpace(out)
	}
	// Ending every session ends the scratch server; host guards may refuse
	// kill-server. Registered before the server starts, so setup cannot leak it.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, _ := runTmuxCommand(ctx, socket, nil, "list-sessions", "-F", "#{session_id}")
		for _, session := range strings.Fields(out) {
			_, _ = runTmuxCommand(ctx, socket, nil, "kill-session", "-t", session)
		}
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
	waitUntil := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for !done() {
			if time.Now().After(deadline) {
				t.Fatalf("%s not reached", what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	// The executor's standing server, then a seat created as realSeatTransport.Create
	// creates one, with cat standing in for the harness.
	tmux("new-session", "-d", "-s", "keeper", "exec cat")
	ids := strings.Fields(tmux("new-session", "-d", "-P", "-F", "#{session_id} #{pane_id}", "-e", "TERM=xterm-256color", "-s", "form-scratch-slot", "-c", root, "exec cat"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	control, err := openSeatControl(ctx, socket, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	window := func() string {
		return tmux("display-message", "-p", "-t", ids[1], "#{window_width}x#{window_height} #{window-size} pane #{pane_width}x#{pane_height}")
	}
	const pinned = "160x48 manual pane 160x48"
	waitUntil("seat window pinned at 160x48", func() bool { return window() == pinned })
	// The formation finished and kept its seat: the control client leaves.
	control.Close()
	waitUntil("control client gone", func() bool { return tmux("list-clients", "-t", ids[0], "-F", "#{client_flags}") == "" })

	socketIdentity, err := core.SocketIdentity(socket)
	if err != nil {
		t.Fatal(err)
	}
	observer, err := terminal.New(terminal.Config{Socket: socket, TmuxBin: bin})
	if err != nil {
		t.Fatal(err)
	}
	target := terminal.Target{SessionID: ids[0], PaneID: ids[1], SocketIdentity: socketIdentity}
	status := observer.Probe(ctx, target)
	if status.State != "live" {
		t.Fatalf("seat probe: %+v", status)
	}
	serveCtx, stop := context.WithCancel(context.Background())
	defer stop()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { observer.Serve(serveCtx, w, r, target) }))
	defer server.Close()
	conn, _, err := (&websocket.Dialer{Subprotocols: []string{"tty"}}).Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(map[string]int{"columns": status.Columns, "rows": status.Rows}); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
	send := func(frame string) {
		t.Helper()
		if err := conn.WriteMessage(websocket.BinaryMessage, []byte(frame)); err != nil {
			t.Fatal(err)
		}
	}
	viewer := func() string { return tmux("list-clients", "-t", ids[0], "-F", "#{client_width}x#{client_height}") }
	waitUntil("viewer attached", func() bool { return viewer() != "" })

	for _, size := range []struct{ frame, client string }{
		{`1{"columns":142,"rows":30}`, "142x30"},
		{`1{"columns":90,"rows":20}`, "90x20"},
	} {
		send(size.frame)
		waitUntil("viewer at "+size.client, func() bool { return viewer() == size.client })
		if got := window(); got != pinned {
			t.Fatalf("viewer at %s resized the seat window: %s", size.client, got)
		}
	}
	// Typing still reaches the seat, whose screen keeps the pinned width.
	send("0TYPED_IN_A_SMALL_VIEW\r")
	waitUntil("typed text in the pane", func() bool {
		return strings.Contains(tmux("capture-pane", "-p", "-t", ids[1]), "TYPED_IN_A_SMALL_VIEW")
	})
	if got := window(); got != pinned {
		t.Fatalf("typing resized the seat window: %s", got)
	}
}
