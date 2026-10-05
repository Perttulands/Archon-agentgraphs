package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestFormationShapeRefusalsOverHTTP(t *testing.T) {
	a := formations.FormationSlot{ID: "slot_x"}
	b := formations.FormationSlot{ID: "slot_y"}
	c := a
	c.Controller = true
	d := b
	d.Controller = true
	for _, formation := range []formations.FormationNode{
		{ID: "empty_solo", Type: "solo", Slots: []formations.FormationSlot{}},
		{ID: "large_solo", Type: "solo", Slots: []formations.FormationSlot{a, b}},
		{ID: "empty_peer", Type: "peer", Slots: []formations.FormationSlot{}},
		{ID: "small_peer", Type: "peer", Slots: []formations.FormationSlot{a}},
		{ID: "controlled_peer", Type: "peer", Slots: []formations.FormationSlot{c, b}},
		{ID: "empty_orchestrated", Type: "orchestrated", Slots: []formations.FormationSlot{}},
		{ID: "small_orchestrated", Type: "orchestrated", Slots: []formations.FormationSlot{c}},
		{ID: "no_controller", Type: "orchestrated", Slots: []formations.FormationSlot{a, b}},
		{ID: "two_controllers", Type: "orchestrated", Slots: []formations.FormationSlot{c, d}},
	} {
		t.Run(formation.ID, func(t *testing.T) {
			store := formations.NewStore(t.TempDir())
			writeFormationsAPIFixture(t, store.BoardPath("shapes"), "schema=1\nid=\"brd_shapes\"\nslug=\"shapes\"\nrev=1\n[[formation]]\nid=\"fmn_work\"\ntype=\"solo\"\n[[formation.slot]]\nid=\"slot_current\"\n")
			mux := http.NewServeMux()
			NewFormationsHandlerWithStore(store).RegisterRoutes(mux)
			board, err := store.ReadBoard("shapes")
			if err != nil {
				t.Fatal(err)
			}
			before := readFormationsAPIFile(t, store.BoardPath("shapes"))
			for _, op := range []struct {
				key    string
				value  any
				status int
				code   string
			}{
				{"setFormationType", map[string]any{"id": "fmn_work", "type": formation.Type, "slots": formation.Slots}, http.StatusBadRequest, "INVALID_TYPE_CHANGE"},
				{"restoreNode", map[string]any{"formation": formation, "x": 400, "y": 200}, http.StatusConflict, "INVALID_NODE_RESTORE"},
			} {
				body, _ := json.Marshal(map[string]any{"expectedRev": board.Rev, op.key: op.value})
				req := httptest.NewRequest(http.MethodPatch, "/api/missions/shapes", strings.NewReader(string(body)))
				req.Header.Set("If-Match", board.ETag)
				rec := httptest.NewRecorder()
				mux.ServeHTTP(rec, req)
				if rec.Code != op.status || !strings.Contains(rec.Body.String(), `"code":"`+op.code+`"`) {
					t.Errorf("%s = %d %s", op.key, rec.Code, rec.Body.String())
				}
				if readFormationsAPIFile(t, store.BoardPath("shapes")) != before {
					t.Fatal("refused restoration changed mission")
				}
			}
		})
	}
}

func TestImportedPeerShapeBlocksRuntimeAPIBeforeEffects(t *testing.T) {
	workspace := t.TempDir()
	store := formations.NewStore(workspace)
	writeFormationsAPIFixture(t, store.BoardPath("shapes"), `schema=1
id="brd_shapes"
slug="shapes"
rev=1
[[formation]]
id="fmn_valid"
type="solo"
[[formation.slot]]
id="slot_valid"
harness="openai-codex"
effort="medium"
[[formation]]
id="fmn_invalid"
type="peer"
[[formation.slot]]
id="slot_invalid"
`)
	tmuxCapture := installRuntimeAPITmuxTripwire(t, workspace)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStore(store).RegisterRoutes(mux)
	for _, request := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/missions/shapes/validation", ""},
		{http.MethodPost, "/api/runs", `{"mission":"shapes","formationId":"fmn_valid"}`},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(request.method, request.path, strings.NewReader(request.body)))
		want := http.StatusOK
		if request.method == http.MethodPost {
			want = http.StatusUnprocessableEntity
		}
		if rec.Code != want || !strings.Contains(rec.Body.String(), `"code":"invalid_formation_shape"`) || !strings.Contains(rec.Body.String(), `"nodeId":"fmn_invalid"`) {
			t.Fatalf("%s = %d %s", request.path, rec.Code, rec.Body.String())
		}
	}
	assertNoRuntimeAPIEffects(t, workspace, tmuxCapture)
}
