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

func TestFormationsAPIMissionAndGateFileReferences(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	created := httptest.NewRecorder()
	mux.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/missions", bytes.NewBufferString(`{"title":"Refs","slug":"refs"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create board = %d %s", created.Code, created.Body.String())
	}
	patch := func(body string) {
		t.Helper()
		board, err := store.ReadBoard("refs")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/missions/refs", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{")))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
	files := func() ([]string, []string) {
		t.Helper()
		board, err := store.ReadBoard("refs")
		if err != nil || len(board.Missions) != 1 || len(board.Gates) != 1 {
			t.Fatalf("board = %+v, %v", board, err)
		}
		return board.Missions[0].Files, board.Gates[0].Files
	}

	patch(`{"createInputCard":{"title":"Work","files":["/work/docs/brief.md"]}}`)
	patch(`{"createGate":{"title":"Review","files":["/work/rubrics/quality.md","/work/rubrics/style.md"]}}`)
	if mission, gate := files(); strings.Join(mission, ",") != "/work/docs/brief.md" || strings.Join(gate, ",") != "/work/rubrics/quality.md,/work/rubrics/style.md" {
		t.Fatalf("created files: mission %q gate %q", mission, gate)
	}
	board, _ := store.ReadBoard("refs")
	missionID, gateID := board.Missions[0].ID, board.Gates[0].ID

	patch(`{"updateInputCard":{"id":"` + missionID + `","files":["/work/docs/next.md"]}}`)
	patch(`{"updateGate":{"id":"` + gateID + `","criterion":"Scores at least 3"}}`)
	if mission, gate := files(); strings.Join(mission, ",") != "/work/docs/next.md" || strings.Join(gate, ",") != "/work/rubrics/quality.md,/work/rubrics/style.md" {
		t.Fatalf("after replacing mission files and an unrelated gate edit: mission %q gate %q", mission, gate)
	}
	board, _ = store.ReadBoard("refs")
	refused := httptest.NewRequest(http.MethodPatch, "/api/missions/refs", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,"updateGate":{"id":"`+gateID+`","files":["rubrics/quality.md"]}}`))
	refused.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, refused)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"RELATIVE_FILE_REFERENCE"`) || !strings.Contains(rec.Body.String(), `is relative: use an absolute path`) {
		t.Fatalf("relative gate file = %d %s, want 400 RELATIVE_FILE_REFERENCE", rec.Code, rec.Body.String())
	}

	patch(`{"updateInputCard":{"id":"` + missionID + `","files":[]}}`)
	patch(`{"updateGate":{"id":"` + gateID + `","files":[]}}`)
	if mission, gate := files(); len(mission) != 0 || len(gate) != 0 {
		t.Fatalf("cleared files: mission %q gate %q", mission, gate)
	}
}
