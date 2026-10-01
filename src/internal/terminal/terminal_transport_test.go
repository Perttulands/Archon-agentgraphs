package terminal

// Ported from CHROTE's terminal transport tests
// (/srv/chrote/src/internal/proxy/terminal_test.go, CHROTE 355ace49) under
// archon-o7p.13.1. As there, a fake tmux records its argv and then behaves like
// an ordinary program on a tty, which exercises the real pty, the real relay
// and the real hangup without a live session anywhere near the suite. The
// socket is a real Unix listener so the seat's socket-identity proof runs
// unchanged. terminal_test.go keeps the scratch-tmux proofs of pinning and
// identity.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"github.com/gorilla/websocket"
)

type terminalHarness struct {
	t        *testing.T
	server   *httptest.Server
	argsPath string
	target   Target
	socket   string
}

// The seat the fake tmux reports: session $7, pane %9, a 160x48 window with a
// status line, which is a 160x49 client grid.
const fakeSeatProbe = "$7\t%9\t0\t160\t48\ton"

func newTerminalHarness(t *testing.T) *terminalHarness {
	t.Helper()
	dir := t.TempDir()
	harness := &terminalHarness{t: t, argsPath: filepath.Join(dir, "tmux.args"), socket: filepath.Join(dir, "tmux.sock")}

	listener, err := net.Listen("unix", harness.socket)
	if err != nil {
		t.Fatalf("listen on scratch socket: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	identity, err := core.SocketIdentity(harness.socket)
	if err != nil {
		t.Fatalf("scratch socket identity: %v", err)
	}
	harness.target = Target{SessionID: "$7", PaneID: "%9", SocketIdentity: identity}

	fake := filepath.Join(dir, "fake-tmux")
	script := `#!/bin/bash
printf '%s\n' "$*" >> "$FAKE_TMUX_ARGS"
for arg in "$@"; do
  case "$arg" in
    display-message)
      [ "${FAKE_TMUX_PROBE_STATUS:-0}" = 0 ] || exit "$FAKE_TMUX_PROBE_STATUS"
      printf '%s\n' "$FAKE_TMUX_PROBE"; exit 0 ;;
    attach-session) exec bash "$FAKE_TMUX_ATTACH" ;;
  esac
done
exit 1
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake tmux: %v", err)
	}
	t.Setenv("FAKE_TMUX_ARGS", harness.argsPath)
	t.Setenv("FAKE_TMUX_PROBE", fakeSeatProbe)
	t.Setenv("FAKE_TMUX_ATTACH", harness.attachScript("exit 0"))

	observer, err := New(Config{Socket: harness.socket, TmuxBin: fake})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	harness.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observer.Serve(ctx, w, r, harness.target)
	}))
	t.Cleanup(harness.server.Close)
	return harness
}

// attachScript writes the program the fake tmux execs in place of an attach.
func (h *terminalHarness) attachScript(body string) string {
	h.t.Helper()
	path := filepath.Join(h.t.TempDir(), "attach.sh")
	if err := os.WriteFile(path, []byte("#!/bin/bash\n"+body+"\n"), 0o755); err != nil {
		h.t.Fatalf("write attach script: %v", err)
	}
	return path
}

func (h *terminalHarness) tmuxArgs() string {
	h.t.Helper()
	raw, err := os.ReadFile(h.argsPath)
	if err != nil {
		return ""
	}
	return string(raw)
}

// stampedFrame records when a server frame reached the client, so a test can
// tell output that was already on its way from output released later.
type stampedFrame struct {
	text string
	at   time.Time
}

// terminalClient is a browser, reading in the background the way one does.
type terminalClient struct {
	t      *testing.T
	conn   *websocket.Conn
	frames chan stampedFrame
	// closed carries the error that ended the read loop. The browser reads a
	// close frame as the terminal ending and its absence as a lost connection,
	// so which one arrived is part of the contract.
	closed chan error
}

// dial opens a terminal connection and sends the opening handshake the way the
// cockpit does, AuthToken included, as CHROTE's client sends it.
func (h *terminalHarness) dial(cols, rows int) *terminalClient {
	h.t.Helper()
	url := "ws" + strings.TrimPrefix(h.server.URL, "http")
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second, Subprotocols: []string{"tty"}}).Dial(url, nil)
	if err != nil {
		h.t.Fatalf("dial terminal WebSocket: %v", err)
	}
	h.t.Cleanup(func() { conn.Close() })
	handshake, err := json.Marshal(map[string]any{"AuthToken": "", "columns": cols, "rows": rows})
	if err != nil {
		h.t.Fatalf("marshal handshake: %v", err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, handshake); err != nil {
		h.t.Fatalf("send handshake: %v", err)
	}
	client := &terminalClient{t: h.t, conn: conn, frames: make(chan stampedFrame, 256), closed: make(chan error, 1)}
	go func() {
		defer close(client.frames)
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				client.closed <- err
				return
			}
			if len(message) == 0 || message[0] != serverOutput {
				continue
			}
			client.frames <- stampedFrame{text: string(message[1:]), at: time.Now()}
		}
	}()
	return client
}

func (c *terminalClient) send(frame []byte) {
	c.t.Helper()
	if err := c.conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		c.t.Fatalf("send frame %q: %v", frame, err)
	}
}

// readUntil collects output until want appears, and reports when it arrived.
func (c *terminalClient) readUntil(want string) stampedFrame {
	c.t.Helper()
	collected := &strings.Builder{}
	timeout := time.After(testPatience)
	for {
		select {
		case frame, open := <-c.frames:
			if !open {
				c.t.Fatalf("terminal closed before %q arrived; got %q", want, collected.String())
			}
			collected.WriteString(frame.text)
			if strings.Contains(collected.String(), want) {
				return stampedFrame{text: collected.String(), at: frame.at}
			}
		case <-timeout:
			c.t.Fatalf("timed out waiting for %q; got %q", want, collected.String())
		}
	}
}

// readUntilWithin collects output until want appears or the wait ends, and
// reports whether it appeared.
func (c *terminalClient) readUntilWithin(want string, wait time.Duration) (stampedFrame, bool) {
	c.t.Helper()
	collected := &strings.Builder{}
	timeout := time.After(wait)
	for {
		select {
		case frame, open := <-c.frames:
			if !open {
				c.t.Fatalf("terminal closed before %q arrived; got %q", want, collected.String())
			}
			collected.WriteString(frame.text)
			if strings.Contains(collected.String(), want) {
				return stampedFrame{text: collected.String(), at: frame.at}, true
			}
		case <-timeout:
			if collected.Len() > 0 {
				c.t.Fatalf("output other than %q arrived while paused: %q", want, collected.String())
			}
			return stampedFrame{}, false
		}
	}
}

// received reports what has already arrived and waits for nothing more.
func (c *terminalClient) received() string {
	c.t.Helper()
	collected := &strings.Builder{}
	for {
		select {
		case frame, open := <-c.frames:
			if !open {
				return collected.String()
			}
			collected.WriteString(frame.text)
		default:
			return collected.String()
		}
	}
}

// closeCode reports the WebSocket close code the server sent.
func (c *terminalClient) closeCode() int {
	c.t.Helper()
	timeout := time.After(testPatience)
	for {
		select {
		case <-c.frames:
		case err := <-c.closed:
			var closeErr *websocket.CloseError
			if errors.As(err, &closeErr) {
				return closeErr.Code
			}
			c.t.Fatalf("the terminal socket ended without a close frame: %v", err)
			return 0
		case <-timeout:
			c.t.Fatal("the terminal socket stayed open")
			return 0
		}
	}
}

// handshakeFrom performs only the WebSocket upgrade from a browser origin and
// reports the status Archon answered with.
func (h *terminalHarness) handshakeFrom(origin string) (int, error) {
	h.t.Helper()
	header := http.Header{}
	if origin != "" {
		header.Set("Origin", origin)
	}
	conn, response, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second, Subprotocols: []string{"tty"}}).
		Dial("ws"+strings.TrimPrefix(h.server.URL, "http"), header)
	if conn != nil {
		conn.Close()
	}
	if response == nil {
		h.t.Fatalf("no handshake response for origin %q: %v", origin, err)
	}
	return response.StatusCode, err
}

func TestTerminal_SameOriginIsServed(t *testing.T) {
	harness := newTerminalHarness(t)
	status, err := harness.handshakeFrom(harness.server.URL)
	if err != nil || status != http.StatusSwitchingProtocols {
		t.Fatalf("the cockpit's own origin was refused: %v (status %d)", err, status)
	}
}

func TestTerminal_ForeignOriginIsRefusedBeforeTmux(t *testing.T) {
	harness := newTerminalHarness(t)
	status, err := harness.handshakeFrom("https://evil.example")
	if err == nil {
		t.Fatal("a foreign browser origin opened a terminal socket")
	}
	if status != http.StatusForbidden {
		t.Fatalf("foreign-origin handshake status = %d, want %d", status, http.StatusForbidden)
	}
	if args := harness.tmuxArgs(); args != "" {
		t.Fatalf("a refused origin still reached tmux: %q", args)
	}
}

// Every seat terminal watches: it attaches to the recorded session ID with
// ignore-size, never with -d, so a second viewer (CHROTE's tile, a second
// cockpit window) keeps watching, and nothing resizes the pinned window.
func TestTerminal_AttachesToTheRecordedSessionIDAsAnObserver(t *testing.T) {
	harness := newTerminalHarness(t)
	t.Setenv("FAKE_TMUX_ATTACH", harness.attachScript(`printf 'attached\n'; sleep 10`))

	harness.dial(80, 24).readUntil("attached")

	args := harness.tmuxArgs()
	want := "-S " + harness.socket + " attach-session -f ignore-size -t $7"
	if !strings.Contains(args, want) {
		t.Fatalf("attach args %q do not contain %q", args, want)
	}
	if !strings.Contains(args, "display-message -p -t $7") {
		t.Fatalf("attach did not probe the recorded session first; args=%q", args)
	}
	for _, forbidden := range []string{"attach-session -d", "resize-window", "refresh-client"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("attach used %q; args=%q", forbidden, args)
		}
	}
}

// CHROTE's claim frame asks to size the window. A seat window is pinned, so
// Archon declines it the way CHROTE declines a peek's, and the connection keeps
// working.
func TestTerminal_ClaimIsDeclinedBecauseTheSeatIsPinned(t *testing.T) {
	harness := newTerminalHarness(t)
	t.Setenv("FAKE_TMUX_ATTACH", harness.attachScript(`printf 'attached\n'; cat`))

	client := harness.dial(80, 24)
	client.readUntil("attached")
	client.send([]byte{clientClaim})
	// Frames are dispatched one at a time on one goroutine, so an echo of the
	// next frame proves the claim before it has already been handled.
	client.send(append([]byte{clientInput}, "still typing\n"...))
	client.readUntil("still typing")

	args := harness.tmuxArgs()
	if strings.Contains(args, "refresh-client") || strings.Contains(args, "resize-window") || strings.Count(args, "\n") != 2 {
		t.Fatalf("a claim reached tmux; args=%q", args)
	}
}

// A refusal is an answer, and it has to arrive as one: said in the pane, and
// closed with a close frame rather than an abnormal close, because the browser
// reads an abnormal close as a lost connection and would dial the refusal
// again. None of them may attach.
func TestTerminal_RefusesWhatItCannotAttachAndSaysSo(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		prepare func(t *testing.T, h *terminalHarness)
		want    string
	}{
		{
			name:    "a session tmux no longer holds",
			prepare: func(t *testing.T, h *terminalHarness) { t.Setenv("FAKE_TMUX_PROBE_STATUS", "1") },
			want:    "owned session is no longer present",
		},
		{
			name:    "a seat whose process has ended",
			prepare: func(t *testing.T, h *terminalHarness) { t.Setenv("FAKE_TMUX_PROBE", "$7\t%9\t1\t160\t48\ton") },
			want:    "seat process has ended",
		},
		{
			name:    "a session ID now naming another pane",
			prepare: func(t *testing.T, h *terminalHarness) { t.Setenv("FAKE_TMUX_PROBE", "$7\t%10\t0\t160\t48\ton") },
			want:    "original seat pane is no longer active",
		},
		{
			name:    "a tmux server that is not the one the seat was created on",
			prepare: func(t *testing.T, h *terminalHarness) { h.target.SocketIdentity = "another server" },
			want:    "original tmux socket is unavailable or has changed",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newTerminalHarness(t)
			testCase.prepare(t, harness)

			client := harness.dial(80, 24)
			refusal := client.readUntil(testCase.want)

			if !strings.Contains(refusal.text, "Archon: ") {
				t.Fatalf("refusal %q is not attributed to Archon", refusal.text)
			}
			if code := client.closeCode(); code != websocket.CloseNormalClosure {
				t.Fatalf("close code = %d, want %d: a refused attach must not look like a lost connection", code, websocket.CloseNormalClosure)
			}
			if args := harness.tmuxArgs(); strings.Contains(args, "attach-session") {
				t.Fatalf("Archon attached anyway after the probe failed; args=%q", args)
			}
		})
	}
}

// The pty is real, so input, echo and output all have to survive the round trip.
func TestTerminal_RelaysInputAndOutput(t *testing.T) {
	harness := newTerminalHarness(t)
	t.Setenv("FAKE_TMUX_ATTACH", harness.attachScript(`
printf 'ready\n'
while IFS= read -r line; do printf 'GOT:%s\n' "$line"; done`))

	client := harness.dial(80, 24)
	client.readUntil("ready")

	client.send(append([]byte{clientInput}, []byte("hello\r")...))
	client.readUntil("GOT:hello")
}

// CHROTE sizes the pty from the handshake. A seat's pty starts at the seat's
// native client grid instead, whatever the handshake says, so a sole viewer
// cannot shrink the window; a resize frame still sizes this view.
func TestTerminal_StartsAtTheNativeGridAndResizesOnlyTheView(t *testing.T) {
	harness := newTerminalHarness(t)
	t.Setenv("FAKE_TMUX_ATTACH", harness.attachScript(`while :; do stty size; sleep 0.05; done`))

	client := harness.dial(137, 40)
	client.readUntil("49 160")

	resize, err := json.Marshal(map[string]int{"columns": 100, "rows": 30})
	if err != nil {
		t.Fatalf("marshal resize: %v", err)
	}
	client.send(append([]byte{clientResize}, resize...))
	client.readUntil("30 100")
}

// The browser tells "the terminal ended" from "the connection was lost" by the
// close frame alone, and acts on the difference: a lost connection is dialled
// again, an ended one is not.
func TestTerminal_EndOfTheAttachClosesWithACloseFrame(t *testing.T) {
	harness := newTerminalHarness(t)
	t.Setenv("FAKE_TMUX_ATTACH", harness.attachScript(`printf 'bye\n'`))

	client := harness.dial(80, 24)
	client.readUntil("bye")

	if code := client.closeCode(); code != websocket.CloseNormalClosure {
		t.Fatalf("close code = %d, want %d: a terminal that ended must not look like a lost connection", code, websocket.CloseNormalClosure)
	}
}

// A browser that goes away must not leave a tmux client attached to a live
// seat. Closing the pty master hangs the attach up.
func TestTerminal_ClientDisconnectEndsTheAttach(t *testing.T) {
	harness := newTerminalHarness(t)
	pidPath := filepath.Join(t.TempDir(), "attach.pid")
	t.Setenv("FAKE_TMUX_ATTACH", harness.attachScript(
		fmt.Sprintf("printf '%%s\\n' \"$$\" > %q\nprintf 'attached\\n'\nwhile :; do sleep 0.05; done", pidPath)))

	client := harness.dial(80, 24)
	client.readUntil("attached")

	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("attach never reported its pid: %v", err)
	}
	pid := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &pid); err != nil || pid <= 0 {
		t.Fatalf("attach pid %q is unusable: %v", raw, err)
	}

	client.conn.Close()

	deadline := time.Now().Add(testPatience)
	for time.Now().Before(deadline) {
		// Kill(0) still succeeds on a zombie, so this also proves the exit was
		// reaped rather than merely signalled.
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("attach client %d is still running after the browser disconnected", pid)
}

// Flow control is the client's: while it says stop, no further pty read is
// issued, and what the seat wrote meanwhile arrives on resume. One read can
// already be outstanding when the pause lands, or none when the pause reaches
// the reader before it reads again, which a loaded host makes likely. Every
// step waits for an event; see CHROTE's TestTerminal_HonoursClientFlowControl
// for the full argument (archon-miz).
func TestTerminal_HonoursClientFlowControl(t *testing.T) {
	harness := newTerminalHarness(t)
	scratch := t.TempDir()
	inFlightWritten := filepath.Join(scratch, "in-flight-written")
	writingDone := filepath.Join(scratch, "writing-done")
	t.Setenv("FAKE_TMUX_ATTACH", harness.attachScript(fmt.Sprintf(`
stty -echo
printf 'ready\n'
read -r _
printf 'in-flight\n'
: > %q
read -r _
printf 'held-back\n'
printf 'end-of-writing\n'
: > %q
sleep 10`, inFlightWritten, writingDone)))

	client := harness.dial(80, 24)
	client.readUntil("ready")

	client.send([]byte{clientPause})
	client.send(append([]byte{clientInput}, []byte("release the outstanding read\r")...))
	waitForFile(t, inFlightWritten)
	// A read issued before the pause returns in-flight; without one it waits
	// for the resume. Either way the seat writes again only once that is known,
	// so no outstanding read can carry what follows.
	_, outstanding := client.readUntilWithin("in-flight", 5*time.Second)

	client.send(append([]byte{clientInput}, []byte("write behind the pause\r")...))
	waitForFile(t, writingDone)
	if withheld := client.received(); strings.Contains(withheld, "held-back") || !outstanding && withheld != "" {
		t.Fatalf("a pty read was issued while the client had paused it; got %q", withheld)
	}

	resumedAt := time.Now()
	client.send([]byte{clientResume})
	released := client.readUntil("end-of-writing")
	if !strings.Contains(released.text, "held-back") || !outstanding && !strings.Contains(released.text, "in-flight") {
		t.Fatalf("the resumed stream skipped what the seat wrote behind the pause; got %q", released.text)
	}
	if released.at.Before(resumedAt) {
		t.Fatal("output written behind the pause reached the client before it resumed the stream")
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(testPatience)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("the seat never reported finishing its writes: %s", path)
}

func TestPTY_ResizeRefusesAnUnusableSize(t *testing.T) {
	pty, err := openPTY()
	if err != nil {
		t.Fatalf("openPTY: %v", err)
	}
	defer pty.master.Close()
	defer pty.slave.Close()

	for _, size := range [][2]int{{0, 24}, {80, 0}, {-1, 24}, {80, 70000}} {
		if err := pty.resize(size[0], size[1]); err == nil {
			t.Fatalf("resize accepted %dx%d", size[0], size[1])
		}
	}
	if err := pty.resize(80, 24); err != nil {
		t.Fatalf("resize refused a usable size: %v", err)
	}
}

// The terminal type is pinned whatever the daemon inherited, because the
// browser's terminal is the one being drawn for. A UTF-8 locale is kept and
// any other replaced, because tmux draws box characters as mojibake under a
// single-byte locale. Archon also keeps the attach out of any tmux the daemon
// itself runs inside.
func TestAttachEnv_PinsTheTerminalTypeAndKeepsAUTF8Locale(t *testing.T) {
	t.Setenv("TERM", "dumb")
	t.Setenv("LANG", "fi_FI.UTF-8")
	t.Setenv("TMUX", "/tmp/other,1,0")
	t.Setenv("TMUX_PANE", "%3")

	env := attachEnv()
	for _, entry := range env {
		if entry == "TERM=dumb" || strings.HasPrefix(entry, "TMUX=") || strings.HasPrefix(entry, "TMUX_PANE=") {
			t.Fatalf("attach environment still carries %q", entry)
		}
	}
	if !hasEntry(env, "TERM=xterm-256color") || !hasEntry(env, "LANG=fi_FI.UTF-8") {
		t.Fatalf("attach environment did not pin TERM and keep the UTF-8 LANG; env=%v", env)
	}

	for _, inherited := range []string{"", "C", "POSIX", "de_DE@euro", "fi_FI.ISO-8859-1"} {
		t.Setenv("LANG", inherited)
		if env := attachEnv(); !hasEntry(env, "LANG=C.UTF-8") {
			t.Fatalf("LANG=%q did not fall back to C.UTF-8; env=%v", inherited, env)
		}
	}
	for _, inherited := range []string{"en_GB.utf8", "de_DE.utf-8", "C.UTF-8"} {
		t.Setenv("LANG", inherited)
		if env := attachEnv(); !hasEntry(env, "LANG="+inherited) {
			t.Fatalf("LANG=%q was not kept; env=%v", inherited, env)
		}
	}
}

func hasEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
