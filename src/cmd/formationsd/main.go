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
	transcripts := flag.String("transcripts", "", "Codex native sessions directory")
	mission := flag.String("mission-label", "proof", "owned session name component")
	model := flag.String("model", "gpt-6-astra", "Codex model")
	effort := flag.String("effort", "xhigh", "Codex reasoning effort")
	timeout := flag.Duration("seat-timeout", 30*time.Minute, "maximum duration of each seat")
	flag.Parse()
	for name, value := range map[string]string{"state-dir": *state, "cwd": *cwd, "socket": *socket, "tmux-bin": *tmux, "transcripts": *transcripts} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("--%s requires an absolute path", name)
		}
	}
	for _, key := range []string{"TMUX", "TMUX_PANE", "TMUX_TMPDIR"} {
		os.Unsetenv(key)
	}
	os.Setenv("CHROTE_TMUX_BIN", *tmux)
	personas := formations.NewPersonaStore(filepath.Join(*state, "agents"))
	c, err := coordinator.Open(*state, personas, func(store *formations.Store) formations.FormationExecutor {
		return formations.NewCodexSeatExecutor(store, personas, formations.CodexSeatConfig{Socket: *socket, Cwd: *cwd, StateDir: *state, TranscriptRoot: *transcripts, Model: *model, Effort: *effort, Mission: *mission, Timeout: *timeout})
	})
	if err != nil {
		return err
	}
	defer c.Close()
	listener, err := coordinator.Listen(*address)
	if err != nil {
		return err
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
