package coordinator

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/api"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// TmuxSessionLiveness lists the sessions on the daemon's tmux socket, so the
// agent roster marks a persona live when a session named by its session stem
// runs there, as the offline agent list does. It only reads session names.
type TmuxSessionLiveness struct {
	Socket  string
	TmuxBin string
}

func (l TmuxSessionLiveness) LiveAgentSessions() ([]formations.LiveAgentSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, l.TmuxBin, "-S", l.Socket, "list-sessions", "-F", "#{session_name}:#{session_attached}").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && formations.TmuxHasNoServer(string(exitErr.Stderr)) {
			return nil, nil
		}
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("list tmux sessions: %w: %s", err, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("list tmux sessions: %w", err)
	}
	return formations.ParseTmuxSessionList(string(output)), nil
}

// ConfigureAgentLiveness sets where the agents roster reads live sessions.
// Without it, as with the lab executor, every agent reports offline.
func (c *Coordinator) ConfigureAgentLiveness(provider api.AgentLivenessProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agentLiveness = provider
}
