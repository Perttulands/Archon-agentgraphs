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

func TestFormationsAPIMissionHumanChannel(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	created := httptest.NewRecorder()
	mux.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/missions", bytes.NewBufferString(`{"title":"Channel","slug":"channel"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create board = %d %s", created.Code, created.Body.String())
	}
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("channel")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/missions/channel", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{")))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	channel := func() (string, int) {
		t.Helper()
		board, err := store.ReadBoard("channel")
		if err != nil || len(board.Missions) != 1 {
			t.Fatalf("board = %+v, %v", board, err)
		}
		return board.Missions[0].HumanChannel, board.Rev
	}

	if rec := patch(`{"createInputCard":{"title":"Talk","humanChannel":"session"}}`); rec.Code != http.StatusOK {
		t.Fatalf("create with session = %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := channel(); got != "session" {
		t.Fatalf("created channel = %q", got)
	}
	board, _ := store.ReadBoard("channel")
	missionID := board.Missions[0].ID

	rec := patch(`{"updateInputCard":{"id":"` + missionID + `","goal":"Answer in the session"}}`)
	var body struct {
		Data struct {
			Board formations.BoardDocument `json:"mission"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); rec.Code != http.StatusOK || err != nil || body.Data.Board.Missions[0].HumanChannel != "session" {
		t.Fatalf("an unrelated edit keeps the channel: %d %s", rec.Code, rec.Body.String())
	}

	_, rev := channel()
	for _, value := range []string{`"email"`, `"Session"`} {
		rec := patch(`{"updateInputCard":{"id":"` + missionID + `","humanChannel":` + value + `}}`)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_HUMAN_CHANNEL") || !strings.Contains(rec.Body.String(), "must be notify or session") {
			t.Fatalf("%s = %d %s", value, rec.Code, rec.Body.String())
		}
	}
	if rec := patch(`{"createInputCard":{"title":"Nope","humanChannel":"email"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("create with email = %d %s", rec.Code, rec.Body.String())
	}
	if got, after := channel(); got != "session" || after != rev {
		t.Fatalf("rejected channels changed the board: channel %q rev %d -> %d", got, rev, after)
	}

	if rec := patch(`{"updateInputCard":{"id":"` + missionID + `","humanChannel":"notify"}}`); rec.Code != http.StatusOK {
		t.Fatalf("notify = %d %s", rec.Code, rec.Body.String())
	}
	if raw, _ := store.ReadBoard("channel"); raw.Missions[0].HumanChannel != "" || strings.Contains(raw.TOML, "humanChannel") {
		t.Fatalf("notify leaves channel %q in:\n%s", raw.Missions[0].HumanChannel, raw.TOML)
	}
}
