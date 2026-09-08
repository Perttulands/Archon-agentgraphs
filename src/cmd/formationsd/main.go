package main

import (
	"context"
	"flag"
	"fmt"
	"net"
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
	var addresses []string
	flag.Func("listen", "listen address; repeat for each trusted interface", func(value string) error { addresses = append(addresses, value); return nil })
	uiDir := flag.String("ui-dir", "", "built dashboard directory; empty disables UI")
	executor := flag.String("executor", "codex", "seat executor: codex or lab")
	state := flag.String("state-dir", "", "private absolute runtime and definition workspace")
	cwd := flag.String("cwd", "", "absolute agent work directory")
	socket := flag.String("socket", "", "existing absolute tmux socket")
	tmux := flag.String("tmux-bin", "", "absolute guarded tmux wrapper")
	transcripts := flag.String("transcripts", "", "Codex native sessions directory")
	mission := flag.String("mission-label", "proof", "owned session name component")
	model := flag.String("model", "gpt-6-astra", "Codex model")
	effort := flag.String("effort", "xhigh", "Codex reasoning effort")
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
	if *executor != "codex" && *executor != "lab" {
		return fmt.Errorf("--executor must be codex or lab")
	}
	paths := map[string]string{"state-dir": *state}
	if *executor == "codex" {
		paths["cwd"] = *cwd
		paths["socket"] = *socket
		paths["tmux-bin"] = *tmux
		paths["transcripts"] = *transcripts
	}
	for name, value := range paths {
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
		if *executor == "lab" {
			return formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Harnesses: []string{"openai-codex", "claude-code"}, Cwd: *state, Roots: []string{*state}})
		}
		return formations.NewCodexSeatExecutor(store, personas, formations.CodexSeatConfig{Socket: *socket, Cwd: *cwd, StateDir: *state, TranscriptRoot: *transcripts, Model: *model, Effort: *effort, Mission: *mission, Timeout: *timeout, RecoveryTranscript: *recoveryTranscript, RecoveryBrief: *recoveryBrief})
	})
	if err != nil {
		return err
	}
	defer c.Close()
	var listeners []net.Listener
	defer func() {
		for _, listener := range listeners {
			listener.Close()
		}
	}()
	if len(addresses) == 0 {
		return fmt.Errorf("at least one --listen address is required")
	}
	for _, address := range addresses {
		listener, err := coordinator.Listen(address)
		if err != nil {
			return err
		}
		listeners = append(listeners, listener)
	}
	handler, err := coordinator.WithUI(c.Handler(), *uiDir)
	if err != nil {
		return err
	}
	if *resume != "" {
		if err := c.ResumeCompletedRun(*resume); err != nil {
			return err
		}
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	for _, listener := range listeners {
		fmt.Printf("Formations coordinator http://%s\n", listener.Addr())
	}
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
	results := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func() { results <- server.Serve(listener) }()
	}
	err = <-results
	stop()
	if err == http.ErrServerClosed {
		<-shutdownDone
		return nil
	}
	return err
}
