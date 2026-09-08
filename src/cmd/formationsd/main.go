package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Perttulands/chrote-agent-formations/internal/coordinator"
	"github.com/Perttulands/chrote-agent-formations/internal/formations"
)

func main() {
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func serve() error {
	address := flag.String("listen", "127.0.0.1:8091", "literal loopback listen address")
	state := flag.String("state-dir", "", "private absolute runtime and definition workspace")
	cwd := flag.String("cwd", "", "absolute agent work directory")
	socket := flag.String("socket", "", "existing absolute tmux socket")
	tmux := flag.String("tmux-bin", "", "absolute guarded tmux wrapper")
	codexTranscripts := flag.String("codex-transcripts", "", "Codex native sessions directory")
	claudeTranscripts := flag.String("claude-transcripts", "", "Claude native projects directory")
	mission := flag.String("mission-label", "proof", "owned session name component")
	timeout := flag.Duration("seat-timeout", 30*time.Minute, "maximum duration of each seat")
	resume := flag.String("resume-run", "", "explicitly resume this blocked run from a completed native turn")
	recoveryTranscript := flag.String("completed-transcript", "", "absolute native transcript for the unresolved completed dispatch")
	recoveryBrief := flag.String("completed-brief", "", "absolute original brief file for that dispatch")
	flag.Parse()
	if *resume != "" {
		if !filepath.IsAbs(*recoveryTranscript) || !filepath.IsAbs(*recoveryBrief) {
			return fmt.Errorf("--resume-run requires absolute --completed-transcript and --completed-brief")
		}
	} else if *recoveryTranscript != "" || *recoveryBrief != "" {
		return fmt.Errorf("completed-turn evidence requires --resume-run")
	}
	for name, value := range map[string]string{"state-dir": *state, "cwd": *cwd, "socket": *socket, "tmux-bin": *tmux, "codex-transcripts": *codexTranscripts, "claude-transcripts": *claudeTranscripts} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("--%s requires an absolute path", name)
		}
	}
	for _, key := range []string{"TMUX", "TMUX_PANE", "TMUX_TMPDIR"} {
		os.Unsetenv(key)
	}
	// The guarded tmux client and Codex bootstrap both need a real terminal type.
	os.Setenv("TERM", "xterm-256color")
	os.Setenv("CHROTE_TMUX_BIN", *tmux)
	personas := formations.NewPersonaStore(filepath.Join(*state, "agents"))
	c, err := coordinator.Open(*state, personas, func(store *formations.Store) formations.FormationExecutor {
		return formations.NewTmuxFormationExecutor(store, personas, formations.TmuxExecutorConfig{Socket: *socket, Cwd: *cwd, Roots: []string{*cwd, *state}, StateDir: *state, CodexTranscriptRoot: *codexTranscripts, ClaudeTranscriptRoot: *claudeTranscripts, Mission: *mission, SessionPrefix: "form-", Harnesses: []string{"openai-codex", "claude-code"}, TimeoutSeconds: int(timeout.Seconds()), OutputCapBytes: 1 << 20, RecoveryTranscript: *recoveryTranscript, RecoveryBrief: *recoveryBrief})
	})
	if err != nil {
		return err
	}
	defer c.Close()
	listener, err := coordinator.Listen(*address)
	if err != nil {
		return err
	}
	defer listener.Close()
	if *resume != "" {
		if err := c.ResumeCompletedRun(*resume); err != nil {
			return err
		}
	}
	server := &http.Server{Handler: c.Handler(), ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("Formations coordinator http://%s\n", listener.Addr())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		server.Shutdown(shutdown)
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		<-shutdownDone
		return nil
	}
	return err
}
