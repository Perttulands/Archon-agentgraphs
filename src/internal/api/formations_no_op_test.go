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

func TestFormationsAPIUnchangedEditAnswersTheCurrentBoard(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	created := httptest.NewRecorder()
	mux.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/formations/boards", bytes.NewBufferString(`{"title":"Same","slug":"same"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create board = %d %s", created.Code, created.Body.String())
	}
	patch := func(body string) (*httptest.ResponseRecorder, *formations.BoardDocument) {
		t.Helper()
		board, err := store.ReadBoard("same")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/same", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{")))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec, board
	}
	if rec, _ := patch(`{"createFormation":{"type":"solo","title":"Worker"}}`); rec.Code != http.StatusOK {
		t.Fatalf("create formation = %d %s", rec.Code, rec.Body.String())
	}
	board, err := store.ReadBoard("same")
	if err != nil {
		t.Fatal(err)
	}
	worker := board.Formations[0]
	for _, body := range []string{
		`{"assignSlot":{"formationId":"` + worker.ID + `","slotId":"` + worker.Slots[0].ID + `","agentId":"codex-builder","harness":"openai-codex"}}`,
		`{"updateFormation":{"id":"` + worker.ID + `","title":"Worker"}}`,
		`{"title":"Same"}`,
	} {
		if rec, _ := patch(body); rec.Code != http.StatusOK {
			t.Fatalf("first %s = %d %s", body, rec.Code, rec.Body.String())
		}
		rec, before := patch(body)
		var response struct {
			Data struct {
				Board formations.BoardDocument `json:"board"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("repeated %s = %d %s (%v), want 200", body, rec.Code, rec.Body.String(), err)
		}
		if rec.Header().Get("ETag") != before.ETag || response.Data.Board.Rev != before.Rev || response.Data.Board.ETag != before.ETag {
			t.Fatalf("repeated %s answered rev %d etag %q (header %q), want the current rev %d and etag %q", body, response.Data.Board.Rev, response.Data.Board.ETag, rec.Header().Get("ETag"), before.Rev, before.ETag)
		}
	}
}
