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
	mux.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/formations/boards", bytes.NewBufferString(`{"title":"Refs","slug":"refs"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create board = %d %s", created.Code, created.Body.String())
	}
	patch := func(body string) {
		t.Helper()
		board, err := store.ReadBoard("refs")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/refs", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{")))
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

	patch(`{"createMission":{"title":"Work","files":["docs/brief.md"]}}`)
	patch(`{"createGate":{"title":"Review","files":["rubrics/quality.md","rubrics/style.md"]}}`)
	if mission, gate := files(); strings.Join(mission, ",") != "docs/brief.md" || strings.Join(gate, ",") != "rubrics/quality.md,rubrics/style.md" {
		t.Fatalf("created files: mission %q gate %q", mission, gate)
	}
	board, _ := store.ReadBoard("refs")
	missionID, gateID := board.Missions[0].ID, board.Gates[0].ID

	patch(`{"updateMission":{"id":"` + missionID + `","files":["docs/next.md"]}}`)
	patch(`{"updateGate":{"id":"` + gateID + `","criterion":"Scores at least 3"}}`)
	if mission, gate := files(); strings.Join(mission, ",") != "docs/next.md" || strings.Join(gate, ",") != "rubrics/quality.md,rubrics/style.md" {
		t.Fatalf("after replacing mission files and an unrelated gate edit: mission %q gate %q", mission, gate)
	}
	patch(`{"updateMission":{"id":"` + missionID + `","files":[]}}`)
	patch(`{"updateGate":{"id":"` + gateID + `","files":[]}}`)
	if mission, gate := files(); len(mission) != 0 || len(gate) != 0 {
		t.Fatalf("cleared files: mission %q gate %q", mission, gate)
	}
}
