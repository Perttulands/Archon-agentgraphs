package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Authoring is served under /api/missions only. The old /api/formations and
// /boards routes are gone and answer 404.
func TestFormationsAPIServesMissionsOnly(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	personas := formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, personas).RegisterRoutes(mux)
	serve := func(method, path, etag, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	dataKeys := func(rec *httptest.ResponseRecorder) []string {
		t.Helper()
		var envelope struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
		keys := make([]string, 0, len(envelope.Data))
		for key := range envelope.Data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys
	}

	rec := serve(http.MethodPost, "/api/missions", "", `{"title":"Routes"}`)
	if rec.Code != http.StatusCreated || !equalStrings(dataKeys(rec), []string{"mission"}) {
		t.Fatalf("POST /api/missions = %d %s", rec.Code, rec.Body.String())
	}
	board, err := store.ReadBoard("routes")
	if err != nil {
		t.Fatal(err)
	}
	rec = serve(http.MethodPatch, "/api/missions/routes", board.ETag, `{"expectedRev":`+jsonInt(board.Rev)+`,"updatedBy":"agent:test","createFormation":{"type":"solo","title":"Step"}}`)
	if rec.Code != http.StatusOK || !equalStrings(dataKeys(rec), []string{"formation", "layout", "mission"}) {
		t.Fatalf("PATCH /api/missions/routes = %d %s", rec.Code, rec.Body.String())
	}
	notes := serve(http.MethodGet, "/api/missions/routes/notes", "", "")
	rec = serve(http.MethodPatch, "/api/missions/routes/notes", notes.Header().Get("ETag"), `{"target":"mission","action":"append","text":"why","author":"agent:test"}`)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"mission":[{`)) {
		t.Fatalf("PATCH notes = %d %s", rec.Code, rec.Body.String())
	}
	board, err = store.ReadBoard("routes")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/missions", "/api/missions/routes", "/api/missions/routes/validation", "/api/missions/routes/notes", "/api/missions/routes/layout", "/api/missions/routes/changes?etag=" + board.ETag} {
		if rec := serve(http.MethodGet, path, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body.String())
		}
	}
	for _, path := range []string{"/api/formations/missions", "/api/formations/boards", "/api/formations/boards/routes", "/api/formations/missions/routes"} {
		if rec := serve(http.MethodGet, path, "", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404", path, rec.Code)
		}
	}
	rec = serve(http.MethodDelete, "/api/missions/routes", board.ETag, `{"expectedRev":`+jsonInt(board.Rev)+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/missions/routes = %d %s", rec.Code, rec.Body.String())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
