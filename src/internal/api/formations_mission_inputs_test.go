package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// updateInputCard declares, replaces and clears a mission's inputs, and
// refuses a malformed one (archon-o7p.3).
func TestFormationsAPIMissionInputsRoundTrip(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	created := httptest.NewRecorder()
	mux.ServeHTTP(created, httptest.NewRequest(http.MethodPost, "/api/missions", bytes.NewBufferString(`{"title":"Inputs","slug":"inputs"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create mission = %d %s", created.Code, created.Body.String())
	}
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("inputs")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/missions/inputs", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{")))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	if rec := patch(`{"createInputCard":{"title":"Start"}}`); rec.Code != http.StatusOK {
		t.Fatalf("create Input card = %d %s", rec.Code, rec.Body.String())
	}
	board, _ := store.ReadBoard("inputs")
	cardID := board.Missions[0].ID

	rec := patch(`{"updateInputCard":{"id":"` + cardID + `","inputs":[{"name":"topic","required":true,"description":"What to explore"},{"name":"sketch","kind":"file"}]}}`)
	var body struct {
		Data struct {
			Board formations.BoardDocument `json:"mission"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); rec.Code != http.StatusOK || err != nil {
		t.Fatalf("declare inputs = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	want := []formations.MissionInput{{Name: "topic", Kind: "text", Required: true, Description: "What to explore"}, {Name: "sketch", Kind: "file"}}
	if !reflect.DeepEqual(body.Data.Board.Missions[0].Inputs, want) {
		t.Fatalf("served inputs = %+v, want %+v", body.Data.Board.Missions[0].Inputs, want)
	}
	if raw := rec.Body.String(); !strings.Contains(raw, `"inputs":[{"name":"topic","description":"What to explore","kind":"text","required":true},{"name":"sketch","kind":"file"}]`) {
		t.Fatalf("inputs JSON shape: %s", raw)
	}

	rec = patch(`{"updateInputCard":{"id":"` + cardID + `","inputs":[{"name":"Sketch Path"}]}}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "INVALID_MISSION_INPUT") || !strings.Contains(rec.Body.String(), `input name \"Sketch Path\" must start with a lowercase letter`) {
		t.Fatalf("bad name = %d %s", rec.Code, rec.Body.String())
	}

	if rec := patch(`{"updateInputCard":{"id":"` + cardID + `","inputs":[]}}`); rec.Code != http.StatusOK {
		t.Fatalf("clear inputs = %d %s", rec.Code, rec.Body.String())
	}
	if board, _ := store.ReadBoard("inputs"); board.Missions[0].Inputs != nil {
		t.Fatalf("cleared inputs = %+v", board.Missions[0].Inputs)
	}
}
