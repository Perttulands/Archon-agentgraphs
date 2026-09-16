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

func TestFormationsAPIUpdatesFormationAndMissionFields(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("rename"), `schema = 1
id = "brd_rename"
slug = "rename"
title = "Rename"
rev = 2

[[mission]]
id = "mis_frame"
title = "New mission"
goal = ""
beadId = ""

[[formation]]
id = "fmn_map"
type = "solo"
title = "New formation"
[[formation.input]]
id = "port_map_in"
label = "Input"

[[connection]]
id = "edge_start"
from = "mis_frame:out"
to = "fmn_map:port_map_in"
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("rename")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/rename", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{")))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	board := func(rec *httptest.ResponseRecorder) formations.BoardDocument {
		t.Helper()
		var response struct {
			Data struct {
				Board formations.BoardDocument `json:"board"`
			} `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &response) != nil {
			t.Fatalf("update = %d %s", rec.Code, rec.Body.String())
		}
		return response.Data.Board
	}

	got := board(patch(`{"updateFormation":{"id":"fmn_map","title":"Map the territory"}}`))
	if got.Formations[0].Title != "Map the territory" || len(got.Connections) != 1 {
		t.Fatalf("formation rename = %+v", got)
	}
	got = board(patch(`{"updateMission":{"id":"mis_frame","goal":"Draft a framing","beadId":"form-3yd.10"}}`))
	if got.Missions[0].Title != "New mission" || got.Missions[0].Goal != "Draft a framing" || got.Missions[0].BeadID != "form-3yd.10" {
		t.Fatalf("mission update = %+v", got.Missions)
	}
	got = board(patch(`{"updateMission":{"id":"mis_frame","beadId":""}}`))
	if got.Missions[0].BeadID != "" || got.Missions[0].Goal != "Draft a framing" {
		t.Fatalf("mission bead clear = %+v", got.Missions)
	}

	before := readFormationsAPIFile(t, store.BoardPath("rename"))
	for body, status := range map[string]int{
		`{"updateMission":{"id":"mis_frame","beadId":"Home-123"}}`: http.StatusBadRequest,
		`{"updateMission":{"id":"mis_missing","title":"x"}}`:       http.StatusNotFound,
		`{"updateFormation":{"id":"fmn_missing","title":"x"}}`:     http.StatusNotFound,
	} {
		if rec := patch(body); rec.Code != status {
			t.Errorf("%s = %d %s, want %d", body, rec.Code, rec.Body.String(), status)
		}
	}
	if after := readFormationsAPIFile(t, store.BoardPath("rename")); after != before {
		t.Fatalf("rejected updates changed the board:\n%s", after)
	}
}
