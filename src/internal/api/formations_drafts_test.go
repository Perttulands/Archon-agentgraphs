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

func TestFormationsAPIAcceptsDraftAuthoringAndReportsFindings(t *testing.T) {
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

	for _, want := range []string{"untitled-board", "untitled-board-2"} {
		rec := serve(http.MethodPost, "/api/formations/boards", "", `{"title":"  "}`)
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"slug":"`+want+`"`) {
			t.Fatalf("unnamed board create = %d %s, want %s", rec.Code, rec.Body.String(), want)
		}
	}

	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("untitled-board")
		if err != nil {
			t.Fatal(err)
		}
		return serve(http.MethodPatch, "/api/formations/boards/untitled-board", board.ETag, `{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{"))
	}
	for _, body := range []string{
		`{"createMission":{"title":"","goal":"","beadId":""}}`,
		`{"createFormation":{"type":"solo","title":""}}`,
		`{"createGate":{"title":"","kinds":["code"],"criterion":"","check":"output_absent","checkVersion":"1","checkValue":""}}`,
	} {
		if rec := patch(body); rec.Code != http.StatusOK {
			t.Fatalf("draft patch %s = %d %s, want saved", body, rec.Code, rec.Body.String())
		}
	}
	if rec := patch(`{"createMission":{"beadId":"../escape"}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unsafe Bead ID = %d %s, want rejected", rec.Code, rec.Body.String())
	}

	board, err := store.ReadBoard("untitled-board")
	if err != nil || len(board.Missions) != 1 || len(board.Formations) != 1 || len(board.Gates) != 1 {
		t.Fatalf("reloaded draft board = %+v (%v)", board, err)
	}
	mission, formation, gate := board.Missions[0], board.Formations[0], board.Gates[0]
	for _, wire := range [][2]string{
		{mission.ID + ":out", formation.ID + ":" + formation.Inputs[0].ID},
		{formation.ID + ":" + formation.Outputs[0].ID, gate.ID + ":in"},
	} {
		if rec := patch(`{"wireConnection":{"from":"` + wire[0] + `","to":"` + wire[1] + `"}}`); rec.Code != http.StatusOK {
			t.Fatalf("wire %v = %d %s", wire, rec.Code, rec.Body.String())
		}
	}

	rec := serve(http.MethodGet, "/api/formations/boards/untitled-board/validation", "", "")
	var validation struct {
		Data struct {
			Errors []formations.BoardFinding `json:"errors"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &validation); err != nil || rec.Code != http.StatusOK || len(validation.Data.Errors) != 2 {
		t.Fatalf("validation = %d %s (%v), want unstaffed slot and incomplete gate", rec.Code, rec.Body.String(), err)
	}

	board, _ = store.ReadBoard("untitled-board")
	rec = serve(http.MethodPost, "/api/formations/runs", board.ETag, `{"board":"untitled-board","missionId":"`+mission.ID+`","expectedRev":`+jsonInt(board.Rev)+`,"limits":{"maxDispatch":3,"maxAttempts":1,"wallClockSeconds":60}}`)
	var failure struct {
		Error struct {
			Code     string                    `json:"code"`
			Findings []formations.BoardFinding `json:"findings"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &failure); err != nil || rec.Code != http.StatusUnprocessableEntity || failure.Error.Code != RunAdmissionErrorCode || len(failure.Error.Findings) != 2 {
		t.Fatalf("draft run start = %d %s (%v), want 422 with both findings", rec.Code, rec.Body.String(), err)
	}
}

func jsonInt(value int) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
