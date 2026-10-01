package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// The cockpit undoes a delete by sending back the node JSON it read from the
// board document; the round trip through HTTP must restore it unchanged.
func TestRestoreNodeUndoesADeleteOverHTTP(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("undo"), `schema = 1
id = "brd_undo"
slug = "undo"
rev = 1

[[inputCard]]
id = "mis_go"
title = "Go"
goal = ""
beadId = ""

[[formation]]
id = "fmn_plan"
type = "orchestrated"
title = "Plan"
[formation.brief]
goal = "Plan the change"
beadId = "archon-abc.1"
files = ["docs/plan.md"]
links = []
[formation.execution]
timeoutSeconds = 600
[[formation.input]]
id = "port_plan_in"
label = "Input"
[[formation.output]]
id = "port_plan_out"
label = "Output"
[[formation.slot]]
id = "slot_lead"
label = "Orchestrator"
controller = true
agentId = "planner"
harness = "openai-codex"
effort = "medium"
[[formation.slot]]
id = "slot_worker"
label = "Agent"
controller = false

[[connection]]
id = "edge_go"
from = "mis_go:out"
to = "fmn_plan:port_plan_in"
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStore(store).RegisterRoutes(mux)
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("undo")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/missions/undo", strings.NewReader(fmt.Sprintf(`{%s,"expectedRev":%d}`, body, board.Rev)))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/missions/undo", nil))
	var read struct {
		Data struct {
			Board struct {
				Formations  []json.RawMessage            `json:"formations"`
				Connections []formations.BoardConnection `json:"connections"`
			} `json:"mission"`
		} `json:"data"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &read); err != nil || len(read.Data.Board.Formations) != 1 {
		t.Fatalf("read board: %v %s", err, get.Body.String())
	}
	before, _ := store.ReadBoard("undo")
	formationJSON := read.Data.Board.Formations[0]
	connectionsJSON, _ := json.Marshal(read.Data.Board.Connections)

	if rec := patch(`"deleteFormation":{"id":"fmn_plan"}`); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	restore := fmt.Sprintf(`"restoreNode":{"formation":%s,"connections":%s,"x":320,"y":96}`, formationJSON, connectionsJSON)
	rec := patch(restore)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	var restored struct {
		Data struct {
			Board  formations.BoardDocument  `json:"mission"`
			Layout formations.LayoutDocument `json:"layout"`
			NodeID string                    `json:"nodeId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Data.NodeID != "fmn_plan" || rec.Header().Get("ETag") != restored.Data.Board.ETag {
		t.Fatalf("restore response: %s", rec.Body.String())
	}
	// Compared as clients see them: the JSON of the formations and connections.
	gotJSON, _ := json.Marshal([]any{restored.Data.Board.Formations, restored.Data.Board.Connections})
	wantJSON, _ := json.Marshal([]any{before.Formations, before.Connections})
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("restored board = %+v\nwant %+v", restored.Data.Board, before)
	}
	if len(restored.Data.Layout.Nodes) != 1 || restored.Data.Layout.Nodes[0] != (formations.LayoutNode{ID: "fmn_plan", X: 320, Y: 96}) {
		t.Fatalf("layout = %+v", restored.Data.Layout.Nodes)
	}

	// A second restore of the same node is refused and changes nothing.
	current, _ := store.ReadBoard("undo")
	again := patch(restore)
	if again.Code != http.StatusConflict || !strings.Contains(again.Body.String(), `"code":"INVALID_NODE_RESTORE"`) || !strings.Contains(again.Body.String(), `node \"fmn_plan\" is already in the mission`) {
		t.Fatalf("second restore: %d %s", again.Code, again.Body.String())
	}
	if after, _ := store.ReadBoard("undo"); after.ETag != current.ETag {
		t.Fatal("a refused restore changed the board")
	}
}
