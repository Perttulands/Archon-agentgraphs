package coordinator

import (
	"encoding/json"
	"net/http/httptest"
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
	path := "/api/formations/runs/" + id + "/gates/gate_review/request"
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
		want := PendingGateRequest{GateID: "gate_review", RequestedSeq: seq, Criterion: "PRIVATE-CRITERION", Input: PendingGateInput{FromNodeID: "fmn_work", FromPortID: "port_out", Text: "PRIVATE-OUTPUT"}}
		if body.Data.Request != want {
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
	for _, unknown := range []string{"/api/formations/runs/" + id + "/gates/gate_other/request", "/api/formations/runs/run_missing/gates/gate_review/request"} {
		if w := get(c, unknown); w.Code != 404 {
			t.Fatalf("%s: %d %s", unknown, w.Code, w.Body.String())
		}
	}
	if w := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"pass","reason":"use Postgres"}`); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "succeeded")
	if w := get(c, path); w.Code != 409 {
		t.Fatalf("decided request: %d %s, want 409", w.Code, w.Body.String())
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
