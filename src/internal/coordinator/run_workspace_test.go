package coordinator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestMissionAdmissionWithoutCwdExecutesInItsProjectedWorkspace(t *testing.T) {
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	e := &pausedLab{entered: make(chan formations.FormationExecution, 8), finish: make(chan struct{})}
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
		e.store = store
		e.lab = formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Cwd: root, Roots: []string{root}, Harnesses: []string{"openai-codex"}})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { close(e.finish); c.Close() })
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(testBoard), 0600); err != nil {
		t.Fatal(err)
	}
	contextPaths := []string{t.TempDir(), filepath.Join(t.TempDir(), "prior-art.md")}
	if err := os.WriteFile(contextPaths[1], []byte("existing work"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []any{nil, ""} {
		body := map[string]any{"board": "proof", "missionId": "mis_proof", "expectedRev": 1, "brief": "work in an automatic workspace", "contextPaths": contextPaths, "limits": formations.RunLimits{MaxDispatch: 3, MaxAttempts: 1, WallClockSeconds: 60}}
		if cwd != nil {
			body["cwd"] = cwd
		}
		raw, _ := json.Marshal(body)
		w := post(t, c, "/api/formations/runs", string(raw))
		if w.Code != 202 {
			t.Fatalf("admission: %d %s", w.Code, w.Body.String())
		}
		var receipt struct {
			Data struct {
				RunID string `json:"runId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
			t.Fatal(err)
		}
		id := receipt.Data.RunID
		want := filepath.Join(root, "workspaces", id)
		select {
		case req := <-e.entered:
			if !reflect.DeepEqual(req.ContextPaths, contextPaths) {
				t.Fatalf("seat context paths = %v", req.ContextPaths)
			}
			if req.Cwd != want {
				t.Fatalf("seat cwd = %s, want %s", req.Cwd, want)
			}
			if info, err := os.Stat(req.Cwd); err != nil || !info.IsDir() {
				t.Fatalf("seat directory missing: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("seat did not execute")
		}
		p, err := c.Project(id)
		if err != nil || p.Cwd != want || !reflect.DeepEqual(p.ContextPaths, contextPaths) {
			t.Fatalf("cwd projection: %+v %v", p, err)
		}
		e.finish <- struct{}{}
		awaitState(t, c, id, "waiting_human")
		w = post(t, c, "/api/formations/runs/"+id+"/abort", `{"reason":"test complete"}`)
		if w.Code != 200 {
			t.Fatalf("abort: %d %s", w.Code, w.Body.String())
		}
		awaitState(t, c, id, "canceled")
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("workspace not retained: %v", err)
		}
	}
}
