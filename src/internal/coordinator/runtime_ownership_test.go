package coordinator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// These checks belong to the real HTTP owner, rather than an API-local engine
// with synchronous responses and different admission/projection contracts.
func TestRuntimeRoutesRejectMissingRunsWithoutExecution(t *testing.T) {
	c, e, _ := fixture(t)
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/runs/run_missing", ""}, {"GET", "/api/runs/run_missing/events", ""},
		{"GET", "/api/runs/run_missing/stream", ""}, {"GET", "/api/runs/run_missing/escalations", ""},
		{"POST", "/api/runs/run_missing/resume", `{}`}, {"POST", "/api/runs/run_missing/abort", `{}`},
		{"POST", "/api/runs/run_missing/gates/gate_missing/verdict", `{"requestedSeq":1,"verdict":"pass"}`},
		{"POST", "/api/runs/run_missing/gates/gate_missing/pick", `{"requestedSeq":1,"attempt":1}`},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.Handler().ServeHTTP(w, httptest.NewRequest(route.method, route.path, strings.NewReader(route.body)))
			var response struct {
				Success bool `json:"success"`
				Error   struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || w.Code != 404 || response.Success || response.Error.Message == "" {
				t.Fatalf("missing run = %d %s (%v)", w.Code, w.Body.String(), err)
			}
		})
	}
	select {
	case node := <-e.entered:
		t.Fatalf("missing run dispatched %s", node)
	default:
	}
}

func TestRuntimeAdmissionChecksDefinitionsBeforeExecution(t *testing.T) {
	for _, tc := range []struct {
		name, board, body, match, wantText string
		status                             int
	}{
		{name: "missing mission", body: `{"mission":"missing","inputCardId":"mis_proof","expectedRev":1}`, status: 404},
		{name: "missing Input card", body: `{"mission":"proof","inputCardId":"mis_missing","expectedRev":1}`, status: 404},
		{name: "missing step", body: `{"mission":"proof","formationId":"fmn_missing","expectedRev":1}`, status: 404},
		{name: "both targets", body: `{"mission":"proof","inputCardId":"mis_proof","formationId":"fmn_work","expectedRev":1}`, status: 400},
		{name: "neither target", body: `{"mission":"proof","expectedRev":1}`, status: 400},
		{name: "stale revision before Tool", board: runtimeToolBoard, body: `{"mission":"proof","inputCardId":"mis_proof","expectedRev":2}`, status: 409},
		{name: "stale ETag before Tool", board: runtimeToolBoard, body: `{"mission":"proof","inputCardId":"mis_proof","expectedRev":1}`, match: "stale", status: 409},
		{name: "reachable Tool", board: runtimeToolBoard, body: `{"mission":"proof","inputCardId":"mis_proof","expectedRev":1,"inputs":{"brief":"{}"}}`, status: 422, wantText: formations.ToolExecutionUnavailableCode},
		{name: "unrelated malformed peer", board: testBoard + "\n[[formation]]\nid=\"fmn_invalid\"\ntype=\"peer\"\n[[formation.slot]]\nid=\"slot_invalid\"\n", body: `{"mission":"proof","formationId":"fmn_work","expectedRev":1,"inputs":{"brief":"run"}}`, status: 422, wantText: formations.FindingInvalidFormationShape},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, e, _ := fixture(t)
			if tc.board != "" {
				if err := os.WriteFile(c.store.BoardPath("proof"), []byte(tc.board), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			r := httptest.NewRequest("POST", "/api/runs", strings.NewReader(tc.body))
			r.Header.Set("If-Match", tc.match)
			w := httptest.NewRecorder()
			c.Handler().ServeHTTP(w, r)
			if w.Code != tc.status || tc.wantText != "" && !strings.Contains(w.Body.String(), tc.wantText) {
				t.Fatalf("admission=%d %s", w.Code, w.Body.String())
			}
			runs, err := c.store.ListRuns(formations.RunListFilter{})
			if err != nil || len(runs) != 0 {
				t.Fatalf("rejected admission left runs: %+v %v", runs, err)
			}
			select {
			case node := <-e.entered:
				t.Fatalf("rejected admission dispatched %s", node)
			default:
			}
		})
	}
}

func TestRuntimeHTTPRecordsARelayedPick(t *testing.T) {
	c, e, _ := fixture(t)
	board := strings.Replace(testBoard, "from = \"gate_review:fail\"\nto = \"end_rejected:in\"", "from = \"gate_review:fail\"\nto = \"fmn_work:port_in\"", 1)
	board += "\n[[limit]]\nid=\"lim_work\"\ntarget=\"fmn_work\"\nrounds=2\nwhenSpent=\"best\"\n"
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(board), 0o600); err != nil {
		t.Fatal(err)
	}
	id := startRun(t, c)
	for range 2 {
		e.proceed <- struct{}{}
		first := awaitState(t, c, id, "waiting_human")
		w := post(t, c, "/api/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(first.WaitingGates[0].RequestedSeq)+`,"verdict":"fail","reason":"needs work"}`)
		if w.Code != http.StatusAccepted {
			t.Fatalf("send back=%d %s", w.Code, w.Body.String())
		}
	}
	pending := awaitState(t, c, id, "waiting_human")
	if len(pending.WaitingGates) != 1 || pending.WaitingGates[0].Pick == nil {
		t.Fatalf("pending pick missing: %+v", pending)
	}
	seq := pending.WaitingGates[0].RequestedSeq
	body := func(requested int) string {
		return `{"requestedSeq":` + strconv.Itoa(requested) + `,"attempt":1,"reason":"best available","relayedBy":"slot_work"}`
	}
	path := "/api/runs/" + id + "/gates/gate_review/pick"
	if stale := post(t, c, path, body(seq+1)); stale.Code != 409 {
		t.Fatalf("stale pick=%d %s", stale.Code, stale.Body.String())
	}
	if picked := post(t, c, path, body(seq)); picked.Code != 202 {
		t.Fatalf("pick=%d %s", picked.Code, picked.Body.String())
	}
	// The pass continues into the second formation, preserving asynchronous
	// execution ownership until that formation completes too.
	e.proceed <- struct{}{}
	final := awaitState(t, c, id, formations.RunStatusPicked)
	if !final.Final {
		t.Fatal("picked run not final")
	}
	events, err := c.store.ReadRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == formations.RunEventHumanVerdictRecorded && event.Data["pick"] != nil && event.Data["relayedBy"] == "slot_work" {
			return
		}
	}
	t.Fatal("recorded pick lost relayedBy")
}

const runtimeToolBoard = `schema=2
id="brd_proof"
slug="proof"
rev=1
[[inputCard]]
id="mis_proof"
goal="Normalize JSON"
[[tool]]
id="tool_normalize"
profileId="json.normalize"
profileVersion="1"
[tool.params]
mode="strict"
[[tool.input]]
id="port_in"
name="input"
label="Input"
direction="input"
kind="work"
acceptedMediaTypes=["application/json"]
required=true
role="data"
[[tool.output]]
id="port_out"
name="output"
label="Output"
direction="output"
kind="work"
acceptedMediaTypes=["application/json"]
[[end]]
id="end_done"
outcome="done"
[[connection]]
id="edge_start"
channel="workflow"
from="mis_proof:out"
to="tool_normalize:port_in"
[[connection]]
id="edge_done"
channel="workflow"
from="tool_normalize:port_out"
to="end_done:in"
`
