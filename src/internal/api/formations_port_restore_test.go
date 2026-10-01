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

// The cockpit undoes Remove this input with restorePort; over HTTP it puts the
// port back in its place with its wire, and refuses a second time unchanged.
func TestRestorePortUndoesARemoveOverHTTP(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("ports"), `schema = 1
id = "brd_ports"
slug = "ports"
rev = 1

[[inputCard]]
id = "mis_go"
title = "Go"
goal = ""
beadId = ""

[[formation]]
id = "fmn_plan"
type = "solo"
title = "Plan"
[[formation.input]]
id = "port_a"
label = "Brief"
[[formation.input]]
id = "port_b"
label = "Rework"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_plan"
label = "Agent"
controller = false

[[connection]]
id = "edge_go"
from = "mis_go:out"
to = "fmn_plan:port_a"
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStore(store).RegisterRoutes(mux)
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("ports")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/missions/ports", strings.NewReader(fmt.Sprintf(`{%s,"expectedRev":%d}`, body, board.Rev)))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	before, _ := store.ReadBoard("ports")
	if rec := patch(`"removePort":{"formationId":"fmn_plan","portId":"port_a"}`); rec.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", rec.Code, rec.Body.String())
	}
	restore := `"restorePort":{"formationId":"fmn_plan","direction":"input","port":{"id":"port_a","label":"Brief"},"index":0,"connections":[{"id":"edge_go","from":"mis_go:out","to":"fmn_plan:port_a"}]}`
	rec := patch(restore)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data struct {
			Board formations.BoardDocument `json:"mission"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Header().Get("ETag") != response.Data.Board.ETag {
		t.Fatalf("ETag header %q, board %q", rec.Header().Get("ETag"), response.Data.Board.ETag)
	}
	got, _ := json.Marshal([]any{response.Data.Board.Formations, response.Data.Board.Connections})
	want, _ := json.Marshal([]any{before.Formations, before.Connections})
	if string(got) != string(want) {
		t.Fatalf("restored %s\nwant %s", got, want)
	}

	current, _ := store.ReadBoard("ports")
	again := patch(restore)
	if again.Code != http.StatusConflict || !strings.Contains(again.Body.String(), `"code":"INVALID_NODE_RESTORE"`) {
		t.Fatalf("second restore: %d %s", again.Code, again.Body.String())
	}
	if bad := patch(`"restorePort":{"formationId":"fmn_plan","direction":"in","port":{"id":"port_x"}}`); bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), `"code":"INVALID_PORT_DIRECTION"`) {
		t.Fatalf("bad direction: %d %s", bad.Code, bad.Body.String())
	}
	if after, _ := store.ReadBoard("ports"); after.ETag != current.ETag {
		t.Fatal("a refused restore changed the board")
	}
}
