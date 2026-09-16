package coordinator

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

type fakeLiveness []formations.LiveAgentSession

func (f fakeLiveness) LiveAgentSessions() ([]formations.LiveAgentSession, error) { return f, nil }

func agentRoster(t *testing.T, c *Coordinator) map[string]formations.AgentProjection {
	t.Helper()
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/agents", nil))
	var body struct {
		Data struct {
			Agents []formations.AgentProjection `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
		t.Fatalf("GET /api/agents = %d %s", w.Code, w.Body.String())
	}
	roster := map[string]formations.AgentProjection{}
	for _, agent := range body.Data.Agents {
		roster[agent.ID] = agent
	}
	return roster
}

func TestCoordinatorAgentRosterReportsConfiguredLiveness(t *testing.T) {
	c, _, _ := fixture(t)
	if agent := agentRoster(t, c)["codex-scout"]; agent.Liveness != formations.AgentLivenessOffline {
		t.Fatalf("without a provider codex-scout = %+v, want offline", agent)
	}
	c.ConfigureAgentLiveness(fakeLiveness{{Name: "codex-scout", Status: "live", Attached: true}, {Name: "operator-notes", Status: "live"}})
	roster := agentRoster(t, c)
	if agent := roster["codex-scout"]; agent.Liveness != formations.AgentLivenessLive || agent.SessionID != "codex-scout" || !agent.Attached {
		t.Fatalf("codex-scout = %+v, want live and attached", agent)
	}
	if agent := roster["codex-judge"]; agent.Liveness != formations.AgentLivenessOffline {
		t.Fatalf("codex-judge = %+v, want offline", agent)
	}
	if agent := roster["operator-notes"]; !agent.Unbound || agent.Liveness != formations.AgentLivenessLive {
		t.Fatalf("operator-notes = %+v, want an unbound live session", agent)
	}
}

func TestTmuxSessionLivenessReadsSessionNames(t *testing.T) {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("requires tmux")
	}
	socket := filepath.Join(t.TempDir(), "socket")
	tmux := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, append([]string{"-S", socket}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	liveness := TmuxSessionLiveness{Socket: socket, TmuxBin: bin}
	if live, err := liveness.LiveAgentSessions(); err != nil || len(live) != 0 {
		t.Fatalf("no server = %+v, %v; want no sessions", live, err)
	}
	// End each session rather than kill-server, which host guards refuse, and
	// require the scratch server to be gone before the test ends.
	t.Cleanup(func() {
		out, _ := tmux("list-sessions", "-F", "#{session_id}")
		for _, session := range strings.Fields(out) {
			tmux("kill-session", "-t", session)
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
	for _, name := range []string{"codex-scout", "operator-notes"} {
		if out, err := tmux("new-session", "-d", "-s", name, "sh", "-c", "exec cat"); err != nil {
			t.Fatalf("new-session %s: %s %v", name, out, err)
		}
	}
	live, err := liveness.LiveAgentSessions()
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, session := range live {
		if session.Status != "live" || session.Attached {
			t.Fatalf("session = %+v, want live and detached", session)
		}
		names = append(names, session.Name)
	}
	if strings.Join(names, ",") != "codex-scout,operator-notes" {
		t.Fatalf("sessions = %v", names)
	}
}
