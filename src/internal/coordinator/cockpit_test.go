package coordinator

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

type cockpitClient struct {
	t      *testing.T
	server *httptest.Server
}

func (client cockpitClient) request(method, path, etag string, body any, code int, target any) string {
	client.t.Helper()
	var input io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			client.t.Fatal(err)
		}
		input = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, client.server.URL+path, input)
	if err != nil {
		client.t.Fatal(err)
	}
	if etag != "" {
		req.Header.Set("If-Match", etag)
	}
	response, err := client.server.Client().Do(req)
	if err != nil {
		client.t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		client.t.Fatal(err)
	}
	var envelope struct {
		Success   bool            `json:"success"`
		Timestamp string          `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
		Error     any             `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		client.t.Fatalf("%s %s: %s: %v", method, path, raw, err)
	}
	if response.StatusCode != code || envelope.Success != (code < 400) || envelope.Timestamp == "" {
		client.t.Fatalf("%s %s: HTTP %d %s", method, path, response.StatusCode, raw)
	}
	if target != nil {
		if err := json.Unmarshal(envelope.Data, target); err != nil {
			client.t.Fatalf("%s: %v", raw, err)
		}
	}
	return response.Header.Get("ETag")
}

func TestMountedCockpitLabWorkflow(t *testing.T) {
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
		return formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: root, Roots: []string{root}})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	server := httptest.NewServer(c.Handler())
	defer server.Close()
	client := cockpitClient{t, server}
	var doc struct {
		Board *formations.BoardDocument `json:"board"`
	}
	etag := client.request("POST", "/api/formations/boards", "", map[string]any{"title": "Cockpit proof"}, 201, &doc)
	base := "/api/formations/boards/" + doc.Board.Slug
	patch := func(operation string, value any) {
		t.Helper()
		etag = client.request("PATCH", base, etag, map[string]any{operation: value, "expectedRev": doc.Board.Rev}, 200, &doc)
	}
	patch("createMission", map[string]any{"title": "Proof", "goal": "Return a bounded lab result", "beadId": "form-proof"})
	mission := doc.Board.Missions[0].ID
	patch("createFormation", map[string]any{"type": "solo", "title": "Work"})
	work := doc.Board.Formations[0]
	patch("assignSlot", map[string]any{"formationId": work.ID, "slotId": work.Slots[0].ID, "agentId": "codex-builder", "harness": "openai-codex"})
	patch("wireConnection", map[string]any{"from": mission + ":out", "to": work.ID + ":" + work.Inputs[0].ID})
	patch("createGate", map[string]any{"title": "Review", "kinds": []string{"human"}, "criterion": "Accept the lab result"})
	gate := doc.Board.Gates[0].ID
	patch("wireConnection", map[string]any{"from": work.ID + ":" + work.Outputs[0].ID, "to": gate + ":in"})
	var layout struct {
		Layout *formations.LayoutDocument `json:"layout"`
	}
	layoutETag := client.request("GET", base+"/layout", "", nil, 200, &layout)
	client.request("PATCH", base+"/layout", layoutETag, map[string]bool{"arrange": true}, 200, &layout)
	if len(layout.Layout.Nodes) < 3 {
		t.Fatalf("arrange: %+v", layout)
	}
	var notes struct {
		Notes *formations.BoardNotesDocument `json:"notes"`
	}
	notesETag := client.request("GET", base+"/notes", "", nil, 200, &notes)
	notesETag = client.request("PATCH", base+"/notes", notesETag, map[string]string{"target": "board", "text": "Operator brief"}, 200, &notes)
	client.request("PATCH", base+"/notes", notesETag, map[string]string{"target": work.ID, "text": "Work note"}, 200, &notes)
	// Offline Archon's shared package sees HTTP edits, and HTTP sees its writes.
	offline := formations.NewStore(root)
	board, err := offline.ReadBoard(doc.Board.Slug)
	if err != nil {
		t.Fatal(err)
	}
	title := "Offline edit"
	board, err = offline.UpdateBoardMetadata(board.Slug, formations.BoardMetadataPatch{Title: &title}, formations.WriteOptions{ExpectedRev: board.Rev, ExpectedETag: board.ETag})
	if err != nil {
		t.Fatal(err)
	}
	etag = client.request("GET", base, "", nil, 200, &doc)
	if doc.Board.Title != title {
		t.Fatal("offline edit not visible")
	}
	client.request("GET", "/api/agents", "", nil, 200, nil)
	client.request("GET", "/api/formations/boards", "", nil, 200, nil)
	client.request("GET", "/api/formations/gate-profiles", "", nil, 200, nil)
	var receipt struct {
		RunID string `json:"runId"`
	}
	start := func() string {
		t.Helper()
		client.request("POST", "/api/formations/runs", etag, map[string]any{"cwd": c.store.Workspace, "brief": "run the proof", "board": board.Slug, "missionId": mission, "expectedRev": board.Rev, "limits": formations.RunLimits{MaxDispatch: 6, MaxAttempts: 2, WallClockSeconds: 30}}, 202, &receipt)
		return receipt.RunID
	}
	id := start()
	p := awaitState(t, c, id, "waiting_human")
	response, err := server.Client().Get(server.URL + "/api/formations/runs/" + id + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	projections := make(chan Projection, 16)
	streamErrors := make(chan error, 1)
	go func() {
		defer close(projections)
		scanner := bufio.NewScanner(response.Body)
		for scanner.Scan() {
			raw, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok {
				continue
			}
			var envelope struct {
				Success   bool       `json:"success"`
				Timestamp string     `json:"timestamp"`
				Data      Projection `json:"data"`
			}
			if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
				streamErrors <- err
				return
			}
			if !envelope.Success || envelope.Timestamp == "" {
				streamErrors <- io.ErrUnexpectedEOF
				return
			}
			projections <- envelope.Data
		}
		streamErrors <- scanner.Err()
	}()
	select {
	case initial := <-projections:
		if initial.Status != "waiting_human" {
			t.Fatal(initial)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not send waiting projection")
	}
	client.request("POST", "/api/formations/runs/"+id+"/gates/"+gate+"/verdict", "", map[string]any{"requestedSeq": p.WaitingGates[0].RequestedSeq, "verdict": "pass"}, 202, nil)
	var final Projection
	for p := range projections {
		final = p
	}
	if err := <-streamErrors; err != nil {
		t.Fatal(err)
	}
	if !final.Final || final.Status != "succeeded" {
		t.Fatalf("final SSE: %+v", final)
	}
	awaitState(t, c, id, "succeeded")
	for _, suffix := range []string{"", "/events", "/escalations"} {
		client.request("GET", "/api/formations/runs/"+id+suffix, "", nil, 200, nil)
	}
	second := start()
	awaitState(t, c, second, "waiting_human")
	var canceled Projection
	client.request("POST", "/api/formations/runs/"+second+"/abort", "", map[string]string{"reason": "operator stop"}, 200, &canceled)
	if !canceled.Final || canceled.Status != "canceled" {
		t.Fatal(canceled)
	}
	// The isolated-formation button uses the same admission and snapshot ETag.
	isolated := map[string]any{"board": board.Slug, "formationId": work.ID, "expectedRev": board.Rev, "limits": formations.RunLimits{MaxDispatch: 6, MaxAttempts: 2, WallClockSeconds: 30}}
	client.request("POST", "/api/formations/runs", "stale-etag", isolated, 409, nil)
	client.request("POST", "/api/formations/runs", etag, isolated, 202, &receipt)
	awaitState(t, c, receipt.RunID, "succeeded")

}

type cancelExecutor struct {
	store    *formations.Store
	entered  chan struct{}
	canceled chan struct{}
	finish   chan struct{}
}

func (e *cancelExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	panic("context required")
}
func (e *cancelExecutor) ExecuteFormationContext(ctx context.Context, req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	close(e.entered)
	<-ctx.Done()
	close(e.canceled)
	<-e.finish
	if err := e.store.AppendRunEvent(req.RunID, formations.RunEvent{Type: "seat_cleanup", NodeID: req.NodeID, Data: map[string]any{"outcome": "left_cleanup_failed"}}); err != nil {
		return formations.FormationExecutionResult{}, err
	}
	return formations.FormationExecutionResult{}, ctx.Err()
}
func TestAbortCancelsOwnedSeatAndHoldsAdmissionUntilCleanup(t *testing.T) {
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	executor := &cancelExecutor{entered: make(chan struct{}), canceled: make(chan struct{}), finish: make(chan struct{})}
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor { executor.store = store; return executor })
	if err != nil {
		t.Fatal(err)
	}
	var finish sync.Once
	defer c.Close()
	defer finish.Do(func() { close(executor.finish) })
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(testBoard), 0600); err != nil {
		t.Fatal(err)
	}
	id := startRun(t, c)
	select {
	case <-executor.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("executor not entered")
	}
	responses := make(chan *httptest.ResponseRecorder, 1)
	go func() { responses <- post(t, c, "/api/formations/runs/"+id+"/abort", `{"reason":"stop owned seat"}`) }()
	select {
	case <-executor.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("abort did not cancel seat")
	}
	if c.acquire(id) {
		c.release(id)
		t.Fatal("admission released before cleanup")
	}
	finish.Do(func() { close(executor.finish) })
	response := <-responses
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	p := awaitState(t, c, id, "canceled")
	found := false
	for _, event := range p.Events {
		if event.Type == "seat_cleanup" && event.Outcome == "left_cleanup_failed" && event.Seq < p.EventCount {
			found = true
		}
	}
	if !found {
		t.Fatalf("cleanup outcome missing before cancellation: %+v", p.Events)
	}
	if !p.Final {
		t.Fatal(p)
	}
}

func TestUIBuildAndSPAFallbackKeepAPIRouting(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "index.html"), []byte("<main>Cockpit</main>"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("window.cockpit=true"), 0600); err != nil {
		t.Fatal(err)
	}
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply(w, 404, map[string]string{"error": "unknown route"})
	})
	handler, err := WithUI(api, root)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/": "<main>Cockpit</main>", "/boards/example": "<main>Cockpit</main>", "/app.js": "window.cockpit=true"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.String() != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/unknown", nil))
	if w.Code != 404 || !strings.Contains(w.Body.String(), `"success":false`) {
		t.Fatal(w.Body.String())
	}
}
