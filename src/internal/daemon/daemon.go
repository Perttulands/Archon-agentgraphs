package daemon

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/api"
	"github.com/Perttulands/Archon-agentgraphs/internal/buildinfo"
	"github.com/Perttulands/Archon-agentgraphs/internal/coordinator"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Main is shared by archond and its formationsd compatibility command.
func Main() {
	if err := Run(os.Args[1:]); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func Run(args []string) error {
	flags := flag.NewFlagSet(filepath.Base(os.Args[0]), flag.ContinueOnError)
	version := flags.Bool("version", false, "print Archon version and source commit")
	var addresses []string
	flags.Func("listen", "listen address; repeat for each trusted interface", func(value string) error { addresses = append(addresses, value); return nil })
	uiDir := flags.String("ui-dir", bundledUI(), "built UI directory; defaults to bundled UI when installed; empty disables UI")
	themeFile := flags.String("theme-file", "", "absolute schema-1 theme file; empty uses bundled chrote-dark")
	executor := flags.String("executor", "tmux", "seat executor: tmux (real seats) or lab (deterministic)")
	state := flags.String("state-dir", "", "private absolute runtime and definition workspace")
	agentsDir := flags.String("agents-dir", "", "absolute persona card directory; defaults to <state-dir>/agents")
	cwd := flags.String("cwd", "", "absolute agent work directory")
	socket := flags.String("socket", "", "existing absolute tmux socket")
	tmux := flags.String("tmux-bin", "", "absolute guarded tmux wrapper")
	codexTranscripts := flags.String("codex-transcripts", "", "Codex native sessions directory")
	claudeTranscripts := flags.String("claude-transcripts", "", "Claude native projects directory")
	mission := flags.String("mission-label", "", "optional prefix before the unique run session name")
	timeout := flags.Duration("seat-timeout", 30*time.Minute, "maximum duration of each seat")
	resume := flags.String("resume-run", "", "explicitly resume this blocked run from a completed native turn")
	recoveryTranscript := flags.String("completed-transcript", "", "absolute native transcript for the unresolved completed dispatch")
	recoveryBrief := flags.String("completed-brief", "", "absolute original brief file for that dispatch")
	notifyCommand := flags.String("notify-command", "", "absolute executable run with each needs-you notification as JSON on stdin; empty disables notifications")
	cockpitURL := flags.String("cockpit-url", "", "cockpit URL for links in notifications")
	var fileRoots []string
	flags.Func("file-root", "absolute directory whose files missions, briefs and gates may reference; repeat for each root", func(value string) error {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("--file-root requires an absolute path")
		}
		fileRoots = append(fileRoots, filepath.Clean(value))
		return nil
	})
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *version {
		fmt.Println(buildinfo.String())
		return nil
	}
	if err := validateNotifyFlags(*notifyCommand, *cockpitURL); err != nil {
		return err
	}
	themeHandler, err := api.NewThemeHandler(*themeFile)
	if err != nil {
		return err
	}
	if *resume != "" {
		if !filepath.IsAbs(*recoveryTranscript) || !filepath.IsAbs(*recoveryBrief) {
			return fmt.Errorf("--resume-run requires absolute --completed-transcript and --completed-brief")
		}
	} else if *recoveryTranscript != "" || *recoveryBrief != "" {
		return fmt.Errorf("completed-turn evidence requires --resume-run")
	}
	if *executor != "tmux" && *executor != "lab" {
		return fmt.Errorf("--executor must be tmux or lab")
	}
	paths := map[string]string{"state-dir": *state}
	if *agentsDir != "" {
		paths["agents-dir"] = *agentsDir
	}
	if *executor == "tmux" {
		if *cwd != "" {
			paths["cwd"] = *cwd
		}
		paths["socket"] = *socket
		paths["tmux-bin"] = *tmux
		paths["codex-transcripts"] = *codexTranscripts
		paths["claude-transcripts"] = *claudeTranscripts
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
	roots := []string{*state}
	if *cwd != "" {
		roots = append(roots, *cwd)
	}
	if *agentsDir == "" {
		*agentsDir = filepath.Join(*state, "agents")
	}
	personas := formations.NewPersonaStore(*agentsDir)
	c, err := coordinator.Open(*state, personas, func(store *formations.Store) formations.FormationExecutor {
		if *executor == "lab" {
			return formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Harnesses: []string{"openai-codex", "claude-code"}, Cwd: *state, Roots: []string{*state}})
		}
		return formations.NewTmuxFormationExecutor(store, personas, formations.TmuxExecutorConfig{Socket: *socket, Cwd: *cwd, Roots: roots, StateDir: *state, CodexTranscriptRoot: *codexTranscripts, ClaudeTranscriptRoot: *claudeTranscripts, Mission: *mission, SessionPrefix: "form-", Harnesses: []string{"openai-codex", "claude-code"}, TimeoutSeconds: int(timeout.Seconds()), OutputCapBytes: 1 << 20, RecoveryTranscript: *recoveryTranscript, RecoveryBrief: *recoveryBrief})
	})
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.ConfigureFileRoots(fileRoots); err != nil {
		return err
	}
	if *executor == "tmux" {
		if err := c.ConfigureTerminals(*socket, *tmux); err != nil {
			return err
		}
		c.ConfigureAgentLiveness(coordinator.TmuxSessionLiveness{Socket: *socket, TmuxBin: *tmux})
	}
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
	mux := http.NewServeMux()
	mux.Handle("GET /api/theme", themeHandler)
	mux.Handle("/", c.Handler())
	handler, err := coordinator.WithUI(mux, *uiDir)
	if err != nil {
		return err
	}
	if *resume == "" {
		if err := c.RecoverInterruptedRuns(); err != nil {
			return err
		}
	}
	if *resume != "" {
		if err := c.ResumeCompletedRun(*resume); err != nil {
			return err
		}
	}
	// Session-channel asks reach their seats with or without a notify command.
	needsYou := coordinator.NeedsYouConfig{CockpitURL: *cockpitURL, ServerURL: "http://" + listeners[0].Addr().String()}
	if *notifyCommand != "" {
		needsYou.Notifier = coordinator.CommandNotifier{Path: *notifyCommand}
	}
	c.EnableNeedsYou(needsYou)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	for _, listener := range listeners {
		fmt.Printf("Archon coordinator http://%s\n", listener.Addr())
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		<-ctx.Done()
		c.BeginShutdown()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			server.Close()
		}
	}()
	results := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func() { results <- server.Serve(listener) }()
	}
	err = <-results
	stop()
	if err == http.ErrServerClosed {
		<-shutdownDone
		return c.Close()
	}
	<-shutdownDone
	return err
}

// validateNotifyFlags checks the optional notification command and link base.
func validateNotifyFlags(command, cockpitURL string) error {
	if command != "" {
		info, err := os.Stat(command)
		if !filepath.IsAbs(command) || err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return fmt.Errorf("--notify-command requires an absolute path to an executable file")
		}
	}
	if cockpitURL != "" {
		u, err := url.Parse(cockpitURL)
		if err != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("--cockpit-url must be an http or https URL without credentials, query or fragment")
		}
	}
	return nil
}

// bundledUI resolves the installation target so prefix/bin symlinks work from
// any working directory. Source builds without the bundle remain API-only.
func bundledUI() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return ""
	}
	return uiBeside(executable)
}

func uiBeside(executable string) string {
	dir := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "share", "archon", "ui"))
	if info, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !info.IsDir() {
		return dir
	}
	return ""
}
