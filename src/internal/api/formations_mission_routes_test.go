package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Authoring is served under /api/formations/missions. The /boards routes, its
// name before the rename, stay for one release and answer identically.
func TestFormationsAPIServesMissionsAndKeepsBoardRoutes(t *testing.T) {
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
	timestamp := regexp.MustCompile(`"timestamp":"[^"]*"`)
	same := func(path string) {
		t.Helper()
		mission := serve(http.MethodGet, "/api/formations/missions"+path, "", "")
		board := serve(http.MethodGet, "/api/formations/boards"+path, "", "")
		if mission.Code != board.Code || timestamp.ReplaceAllString(mission.Body.String(), "") != timestamp.ReplaceAllString(board.Body.String(), "") ||
			mission.Header().Get("ETag") != board.Header().Get("ETag") {
			t.Fatalf("GET %s: missions %d %s\nboards %d %s", path, mission.Code, mission.Body.String(), board.Code, board.Body.String())
		}
		if mission.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, mission.Code, mission.Body.String())
		}
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
	current := func(slug string) *formations.BoardDocument {
		t.Helper()
		board, err := store.ReadBoard(slug)
		if err != nil {
			t.Fatal(err)
		}
		return board
	}

	// Each write answers the same way under both names.
	var createKeys []string
	for _, base := range []string{"/api/formations/missions", "/api/formations/boards"} {
		rec := serve(http.MethodPost, base, "", `{"title":"Routes via `+base[len("/api/formations/"):]+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", base, rec.Code, rec.Body.String())
		}
		if keys := dataKeys(rec); createKeys == nil {
			createKeys = keys
		} else if !equalStrings(keys, createKeys) {
			t.Fatalf("POST %s keys %v, want %v", base, keys, createKeys)
		}
	}
	slug := "routes-via-missions"
	for _, base := range []string{"/api/formations/missions/", "/api/formations/boards/"} {
		board := current(slug)
		rec := serve(http.MethodPatch, base+slug, board.ETag, `{"expectedRev":`+jsonInt(board.Rev)+`,"updatedBy":"agent:test","createFormation":{"type":"solo","title":"Step via `+base[len("/api/formations/"):len(base)-1]+`"}}`)
		if rec.Code != http.StatusOK || !equalStrings(dataKeys(rec), []string{"board", "formation", "layout"}) {
			t.Fatalf("PATCH %s = %d %s", base, rec.Code, rec.Body.String())
		}
		notes := serve(http.MethodGet, base+slug+"/notes", "", "")
		rec = serve(http.MethodPatch, base+slug+"/notes", notes.Header().Get("ETag"), `{"target":"board","action":"append","text":"via `+base+`","author":"agent:test"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH %snotes = %d %s", base, rec.Code, rec.Body.String())
		}
		layout := serve(http.MethodGet, base+slug+"/layout", "", "")
		rec = serve(http.MethodPatch, base+slug+"/layout", layout.Header().Get("ETag"), `{"arrange":true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH %slayout = %d %s", base, rec.Code, rec.Body.String())
		}
	}
	if board := current(slug); len(board.Formations) != 2 {
		t.Fatalf("formations after both PATCH routes = %+v", board.Formations)
	}

	// Each read answers identically.
	board := current(slug)
	for _, path := range []string{"", "/" + slug, "/" + slug + "/validation", "/" + slug + "/notes", "/" + slug + "/layout", "/" + slug + "/changes?etag=" + board.ETag} {
		same(path)
	}

	for _, base := range []string{"/api/formations/missions/", "/api/formations/boards/"} {
		target := "routes-via-boards"
		if base == "/api/formations/missions/" {
			target = slug
		}
		rec := serve(http.MethodDelete, base+target, current(target).ETag, `{"expectedRev":`+jsonInt(current(target).Rev)+`}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("DELETE %s%s = %d %s", base, target, rec.Code, rec.Body.String())
		}
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
