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

// A mission patch carries one operation. One naming two is refused before
// either applies, rather than applying whichever the handler reads first
// (archon-n7u.46).
func TestMissionPatchNamingTwoOperationsIsRefusedWhole(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("pair"), `schema = 1
id = "brd_pair"
slug = "pair"
title = "Pair"
rev = 1

[[inputCard]]
id = "mis_go"
title = "Go"
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	before, err := store.ReadBoard("pair")
	if err != nil {
		t.Fatal(err)
	}

	for body, named := range map[string]string{
		`{"expectedRev":1,"deleteInputCard":{"id":"mis_go"},"title":"Renamed"}`:                        "deleteInputCard and title",
		`{"expectedRev":1,"createGate":{"title":"Review"},"createEnd":{"outcome":"done"},"title":"X"}`: "createGate, createEnd and title",
	} {
		req := httptest.NewRequest(http.MethodPatch, "/api/missions/pair", bytes.NewBufferString(body))
		req.Header.Set("If-Match", before.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		want := "a mission patch names one operation, and this one names " + named + ": send them one at a time"
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s = %d %s, want 400 %q", body, rec.Code, rec.Body.String(), want)
		}
		after, err := store.ReadBoard("pair")
		if err != nil || after.ETag != before.ETag || after.Rev != before.Rev {
			t.Fatalf("a refused patch changed the mission: rev %d etag %s (%v)", after.Rev, after.ETag, err)
		}
	}
}
