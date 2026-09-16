package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestFormationsAPIReportsUnsafeBeadIDsByField(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
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
	if rec := serve(http.MethodPost, "/api/formations/boards", "", `{"title":"Beads","slug":"beads"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create board = %d %s", rec.Code, rec.Body.String())
	}
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("beads")
		if err != nil {
			t.Fatal(err)
		}
		return serve(http.MethodPatch, "/api/formations/boards/beads", board.ETag, `{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{"))
	}
	if rec := patch(`{"createMission":{"title":"Work","beadId":"form-3yd.4"}}`); rec.Code != http.StatusOK {
		t.Fatalf("safe mission = %d %s", rec.Code, rec.Body.String())
	}
	if rec := patch(`{"createFormation":{"type":"solo","title":"Worker"}}`); rec.Code != http.StatusOK {
		t.Fatalf("formation = %d %s", rec.Code, rec.Body.String())
	}
	board, err := store.ReadBoard("beads")
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ReadBoard("beads")
	if err != nil {
		t.Fatal(err)
	}
	for body, field := range map[string]string{
		`{"createMission":{"title":"Other","beadId":"Home-123"}}`:                                          `mission beadId \"Home-123\"`,
		`{"updateMission":{"id":"` + board.Missions[0].ID + `","beadId":"../escape"}}`:                     `mission beadId \"../escape\"`,
		`{"setBrief":{"formationId":"` + board.Formations[0].ID + `","goal":"Work","beadId":"chlab/123"}}`: `brief beadId \"chlab/123\"`,
	} {
		rec := patch(body)
		var response struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusBadRequest || response.Error.Code != "INVALID_BEAD_ID" || !strings.HasPrefix(response.Error.Message, strings.ReplaceAll(field, `\"`, `"`)+" must be a safe Beads issue id") {
			t.Errorf("%s = %d %s, want INVALID_BEAD_ID naming %s", body, rec.Code, rec.Body.String(), field)
		}
	}
	if after, err := store.ReadBoard("beads"); err != nil || after.ETag != before.ETag {
		t.Fatalf("rejected Bead IDs changed the board: %v", err)
	}
	for _, path := range []string{"/api/formations/boards/bad..slug", "/api/formations/boards/bad%2Fpath"} {
		rec := serve(http.MethodPatch, path, before.ETag, `{"title":"x"}`)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"message":"Invalid formation slug"`) {
			t.Errorf("%s = %d %s, want Invalid formation slug", path, rec.Code, rec.Body.String())
		}
	}
}
