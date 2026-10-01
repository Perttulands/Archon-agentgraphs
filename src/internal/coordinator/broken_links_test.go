package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A run ledger or mission behind a symlink whose target has gone never stops
// the daemon starting, hides the other runs or blocks starting the intact
// mission, and a start of the broken mission says why (archon-4m4j review).
func TestBrokenLinksNeverStopTheDaemonOrTheOtherMissions(t *testing.T) {
	c, executor, root := fixture(t)
	runsDir := filepath.Join(root, ".archon", "runs", "proof")
	if err := os.MkdirAll(runsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere", "run_01LOST.ndjson"), filepath.Join(runsDir, "run_01LOST.ndjson")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "repository", "moved.mission.toml"), c.store.BoardPath("moved")); err != nil {
		t.Fatal(err)
	}
	if err := c.RecoverInterruptedRuns(); err != nil {
		t.Fatalf("startup recovery with a lost ledger: %v", err)
	}
	id := startRun(t, c)
	<-executor.entered
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/runs", nil))
	var listed struct {
		Data []Projection `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || w.Code != 200 || len(listed.Data) != 1 || listed.Data[0].RunID != id {
		t.Fatalf("runs = %d %s, want the intact run", w.Code, w.Body.String())
	}
	start := post(t, c, "/api/runs", `{"mission":"moved","inputCardId":"mis_x","expectedRev":1}`)
	if start.Code != 422 || !strings.Contains(start.Body.String(), "moved.mission.toml is a symlink to") || !strings.Contains(start.Body.String(), "which does not exist") {
		t.Fatalf("start the broken mission = %d %s, want why", start.Code, start.Body.String())
	}
}
