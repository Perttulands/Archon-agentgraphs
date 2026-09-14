package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

type testExecutor struct {
	entered chan string
	proceed chan struct{}
}

func (e *testExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	e.entered <- req.NodeID
	<-e.proceed
	outputs := map[string]formations.FormationOutputPayload{}
	for _, port := range req.Formation.Outputs {
		outputs[port.ID] = formations.FormationOutputPayload{Text: "PRIVATE-OUTPUT", Ref: "/private/raw"}
	}
	return formations.FormationExecutionResult{Status: "done", Text: "PRIVATE-CAPTURE", ReportRef: "/private/report", Outputs: outputs}, nil
}
func fixture(t *testing.T) (*Coordinator, *testExecutor, string) {
	t.Helper()
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	e := &testExecutor{entered: make(chan string, 8), proceed: make(chan struct{})}
	c, err := Open(root, personas, func(*formations.Store) formations.FormationExecutor { return e })
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(e.proceed) }); c.Close() })
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(testBoard), 0600); err != nil {
		t.Fatal(err)
	}
	return c, e, root
}
func post(t *testing.T, c *Coordinator, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, r)
	return w
}
func startRun(t *testing.T, c *Coordinator) string {
	t.Helper()
	w := post(t, c, "/api/formations/runs", `{"cwd":`+strconv.Quote(c.store.Workspace)+`,"brief":"run the proof", "board":"proof","missionId":"mis_proof","expectedRev":1,"limits":{"maxDispatch":3,"maxAttempts":1,"wallClockSeconds":600,"redact":false}}`)
	if w.Code != 202 {
		t.Fatalf("start %d %s", w.Code, w.Body.String())
	}
	var receipt struct {
		Data struct {
			RunID string `json:"runId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt.Data.RunID
}
func awaitState(t *testing.T, c *Coordinator, id, state string) *Projection {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		change := c.nextChange(id)
		p, err := c.Project(id)
		if err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		busy := c.state(id).busy
		c.mu.Unlock()
		if p.Status == state && !busy {
			return p
		}
		select {
		case <-change:
		case <-ctx.Done():
			t.Fatalf("wanted %s, got %+v", state, p)
		}
	}
}
func TestAdmissionSurvivesDisconnectAndHumanGateRequiresExactRequest(t *testing.T) {
	c, e, root := fixture(t)
	if other, err := Open(root, c.personas, func(*formations.Store) formations.FormationExecutor { return e }); err == nil {
		other.Close()
		t.Fatal("second writer acquired lock")
	}
	// The request has returned while execution is still awaiting its result.
	id := startRun(t, c)
	select {
	case node := <-e.entered:
		if node != "fmn_work" {
			t.Fatal(node)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("seat not dispatched")
	}
	e.proceed <- struct{}{}
	p := awaitState(t, c, id, "waiting_human")
	if len(p.WaitingGates) != 1 || p.Final {
		t.Fatalf("human request missing: %+v", p)
	}
	select {
	case node := <-e.entered:
		t.Fatalf("downstream %s ran before verdict", node)
	default:
	}
	wrong := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":999,"verdict":"pass","reason":"stale"}`)
	if wrong.Code != 409 {
		t.Fatal(wrong.Code)
	}
	raw, _ := json.Marshal(p)
	for _, private := range []string{"PRIVATE-", "/private/", "inputRef", "prompt", "bindingsSnapshot"} {
		if bytes.Contains(raw, []byte(private)) {
			t.Fatalf("private data leaked: %s", private)
		}
	}
	list := httptest.NewRecorder()
	c.Handler().ServeHTTP(list, httptest.NewRequest("GET", "/api/formations/runs", nil))
	if !strings.Contains(list.Body.String(), `"status":"waiting_human"`) {
		t.Fatal(list.Body.String())
	}
	body, _ := json.Marshal(map[string]any{"requestedSeq": p.WaitingGates[0].RequestedSeq, "verdict": "pass", "reason": "operator approves"})
	w := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", string(body))
	if w.Code != 202 {
		t.Fatalf("verdict %d %s", w.Code, w.Body.String())
	}
	select {
	case node := <-e.entered:
		if node != "fmn_after" {
			t.Fatal(node)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("continuation not dispatched")
	}
	e.proceed <- struct{}{}
	final := awaitState(t, c, id, "succeeded")
	if !final.Final || len(final.WaitingGates) != 0 {
		t.Fatal(final)
	}
	if w := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", string(body)); w.Code != 409 {
		t.Fatal("duplicate verdict accepted")
	}
}
func TestConfiguredListenAddress(t *testing.T) {
	l, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	l, err = Listen("0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
}

const testBoard = `schema = 1
id = "brd_proof"
slug = "proof"
title = "Proof"
rev = 1
[[mission]]
id = "mis_proof"
title = "Proof"
goal = "PRIVATE-OBJECTIVE"
beadId = "form-2fb"
[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
controller = true
[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "PRIVATE-CRITERION"
[[formation]]
id = "fmn_after"
type = "solo"
title = "After"
[[formation.input]]
id = "port_after_in"
label = "Input"
[[formation.output]]
id = "port_after_out"
label = "Output"
[[formation.slot]]
id = "slot_after"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
controller = true
[[connection]]
id = "edge_start"
from = "mis_proof:out"
to = "fmn_work:port_in"
[[connection]]
id = "edge_gate"
from = "fmn_work:port_out"
to = "gate_review:in"
[[connection]]
id = "edge_pass"
from = "gate_review:pass"
to = "fmn_after:port_after_in"
`
