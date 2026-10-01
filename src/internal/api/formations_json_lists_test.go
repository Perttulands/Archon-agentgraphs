package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
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

// A mission whose symlink lost its target is listed as broken, with the link
// and target, and reading it says so; the other missions read as before
// (archon-4m4j review). A role card in the same state is named beside a roster
// that still lists.
func TestBrokenLinksAreNamedAndEverythingElseStillServes(t *testing.T) {
	root := t.TempDir()
	store := formations.NewStore(filepath.Join(root, "state"))
	writeFormationsAPIFixture(t, store.BoardPath("intact"), "schema = 1\nid = \"brd_intact\"\nslug = \"intact\"\ntitle = \"Intact\"\nrev = 1\n")
	if err := os.MkdirAll(filepath.Join(root, "repository"), 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(root, "repository", "moved.mission.toml")
	if err := os.Symlink(gone, store.BoardPath("moved")); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(root, "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "dotfiles", "critic.toml"), filepath.Join(agents, "critic.toml")); err != nil {
		t.Fatal(err)
	}
	personas := formations.NewPersonaStore(agents)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, personas).RegisterRoutes(mux)
	NewAgentsHandlerWithStore(personas).RegisterRoutes(mux)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	broken := store.BoardPath("moved") + " is a symlink to " + gone + ", which does not exist"
	if rec := get("/api/missions"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"slug":"intact"`) || !strings.Contains(rec.Body.String(), `"broken":"`+broken+`"`) {
		t.Fatalf("list = %d %s, want intact and moved, moved broken", rec.Code, rec.Body.String())
	}
	if rec := get("/api/missions/intact"); rec.Code != http.StatusOK {
		t.Fatalf("intact mission = %d %s", rec.Code, rec.Body.String())
	}
	if rec := get("/api/missions/moved"); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"code":"BROKEN_LINK"`) || !strings.Contains(rec.Body.String(), broken) {
		t.Fatalf("broken mission = %d %s, want 422 BROKEN_LINK naming the link", rec.Code, rec.Body.String())
	}
	if rec := get("/api/agents"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"unreadable":[{"name":"critic"`) || !strings.Contains(rec.Body.String(), `"agents":[`) {
		t.Fatalf("roster = %d %s, want the roster with critic named unreadable", rec.Code, rec.Body.String())
	}
}
