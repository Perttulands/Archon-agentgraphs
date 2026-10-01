package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Removing a formation's only input leaves it with no inputs, which the
// mission JSON carries as [] rather than null; so does every other list a
// client indexes, empty or not (archon-n7u.49).
func TestMissionJSONCarriesEmptyListsAsArrays(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("lists"), `schema = 1
id = "brd_lists"
slug = "lists"
title = "Lists"
rev = 1

[[formation]]
id = "fmn_plan"
type = "solo"
title = "Plan"
[[formation.input]]
id = "port_in"
label = "Input"

[[gate]]
id = "gate_review"
title = "Review"
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	board, err := store.ReadBoard("lists")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/missions/lists", bytes.NewBufferString(`{"expectedRev":1,"removePort":{"formationId":"fmn_plan","portId":"port_in"}}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove the only input = %d %s", rec.Code, rec.Body.String())
	}
	read := httptest.NewRecorder()
	mux.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/api/missions/lists", nil))
	layout := httptest.NewRecorder()
	mux.ServeHTTP(layout, httptest.NewRequest(http.MethodGet, "/api/missions/lists/layout", nil))
	for name, body := range map[string]string{"patch": rec.Body.String(), "read": read.Body.String()} {
		if strings.Contains(body, "null") {
			t.Fatalf("%s carries null: %s", name, body)
		}
		for _, list := range []string{`"inputs":[]`, `"outputs":[]`, `"slots":[]`, `"kinds":[]`, `"inputCards":[]`, `"tools":[]`, `"ends":[]`, `"connections":[]`} {
			if !strings.Contains(body, list) {
				t.Fatalf("%s lacks %s: %s", name, list, body)
			}
		}
	}
	for _, list := range []string{`"nodes":[]`, `"edges":[]`} {
		if !strings.Contains(layout.Body.String(), list) {
			t.Fatalf("layout lacks %s: %s", list, layout.Body.String())
		}
	}
}
