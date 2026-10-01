package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestPendingGateRequestServesOnlyTheRoutedInput(t *testing.T) {
	c, executor, root := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	seq := awaitState(t, c, id, "waiting_human").WaitingGates[0].RequestedSeq
	get := func(c *Coordinator, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	path := "/api/runs/" + id + "/gates/gate_review/request"
	for restart := 0; restart < 2; restart++ {
		w := get(c, path)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var body struct {
			Data struct {
				Request PendingGateRequest `json:"request"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		want := PendingGateRequest{GateID: "gate_review", RequestedSeq: seq, Criterion: "PRIVATE-CRITERION", Input: PendingGateInput{FromNodeID: "fmn_work", FromPortID: "port_out", Text: "PRIVATE-OUTPUT"},
			// Approve starts After's first of one attempt with one of three dispatches
			// used; the send-back ends this path rejected, and so the run fails.
			Routes: []formations.GateRoute{
				{Verdict: "pass", Targets: []formations.GateRouteTarget{{NodeID: "fmn_after", Title: "After", Kind: "formation", Attempt: 1, MaxAttempts: 1}},
					Dispatches: &formations.RunLimitReached{Kind: formations.RunLimitDispatches, Used: 1, Max: 3}, DispatchesNeeded: 1},
				{Verdict: "fail", Targets: []formations.GateRouteTarget{{NodeID: "end_rejected", Title: "Rejected", Kind: "end", Outcome: formations.EndOutcomeRejected}}, EndsRun: true, RunFails: true},
			}}
		if !reflect.DeepEqual(body.Data.Request, want) {
			t.Fatalf("request = %+v, want %+v", body.Data.Request, want)
		}
		for _, private := range []string{"/private/", "PRIVATE-CAPTURE", "PRIVATE-OBJECTIVE", "run the proof", "prompt", `"ref"`, "sessionRef", "briefPath"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("gate request leaked %q: %s", private, w.Body.String())
			}
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		c, err = Open(root, c.personas, func(*formations.Store) formations.FormationExecutor { return executor })
		if err != nil {
			t.Fatal(err)
		}
		if err := c.RecoverInterruptedRuns(); err != nil {
			t.Fatal(err)
		}
	}
	defer c.Close()
	for _, unknown := range []string{"/api/runs/" + id + "/gates/gate_other/request", "/api/runs/run_missing/gates/gate_review/request"} {
		if w := get(c, unknown); w.Code != 404 {
			t.Fatalf("%s: %d %s", unknown, w.Code, w.Body.String())
		}
	}
	if w := post(t, c, "/api/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"pass","reason":"use Postgres"}`); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "succeeded")
	if w := get(c, path); w.Code != 409 {
		t.Fatalf("decided request: %d %s, want 409", w.Code, w.Body.String())
	}
}

// An unreadable frozen board leaves the routes out; the request is still served.
func TestPendingGateRequestServesWithoutRoutesWhenTheBoardIsUnreadable(t *testing.T) {
	c, executor, root := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	snapshots, err := filepath.Glob(filepath.Join(root, ".archon", "runs", "*", id+".snapshot.toml"))
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("snapshots = %v, %v", snapshots, err)
	}
	if err := os.WriteFile(snapshots[0], []byte("not = [toml"), 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/runs/"+id+"/gates/gate_review/request", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), `"routes"`) || !strings.Contains(w.Body.String(), "PRIVATE-OUTPUT") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestPendingGateTextIsCappedAtARuneBoundary(t *testing.T) {
	if text, truncated := capPendingGateText("short"); text != "short" || truncated {
		t.Fatalf("short text = %q, %v", text, truncated)
	}
	exact := strings.Repeat("a", pendingGateInputMaxBytes)
	if text, truncated := capPendingGateText(exact); text != exact || truncated {
		t.Fatal("text at the cap must not be truncated")
	}
	long := strings.Repeat("a", pendingGateInputMaxBytes-1) + "é and more"
	text, truncated := capPendingGateText(long)
	if !truncated || len(text) != pendingGateInputMaxBytes-1 || !utf8.ValidString(text) {
		t.Fatalf("truncated=%v len=%d valid=%v", truncated, len(text), utf8.ValidString(text))
	}
}

// A driver agent reads gate request and run wait text, so both serve the
// routed input exactly as the step produced it, secret-shaped text included.
func TestGateRequestAndWaitServeRoutedTextVerbatim(t *testing.T) {
	const routed = "use api_key=abc123 and sk-abcdefghijklmnop; password: string is a type; \"quoted\""
	c, executor, _ := fixture(t)
	executor.outputText = routed
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/runs/"+id+"/gates/gate_review/request", nil))
	var body struct {
		Data struct {
			Request PendingGateRequest `json:"request"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Data.Request.Input.Text != routed {
		t.Fatalf("gate request input = %q, %v; want %q", body.Data.Request.Input.Text, err, routed)
	}
	got := waitRequest(t, c, id, "until=needs-you&hold=1")
	if got.code != 200 || len(got.result.Asks) == 0 || got.result.Asks[0].Input == nil || got.result.Asks[0].Input.Text != routed {
		t.Fatalf("wait = %d %s", got.code, got.body)
	}
}
