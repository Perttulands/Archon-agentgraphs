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

func TestFormationsAPIUpdateGateSetsOnlyPresentFields(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7

[[gate]]
id = "gate_review"
title = "Review gate"
kinds = ["code"]
criterion = "Human framing review"
check = "output_absent"
checkVersion = "1"
checkValue = "complaint text"
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("session-search")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/session-search", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{")))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	gate := func(rec *httptest.ResponseRecorder) formations.GateNode {
		t.Helper()
		var response struct {
			Data struct {
				Board formations.BoardDocument `json:"board"`
			} `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &response) != nil || len(response.Data.Board.Gates) != 1 {
			t.Fatalf("updateGate = %d %s", rec.Code, rec.Body.String())
		}
		return response.Data.Board.Gates[0]
	}

	got := gate(patch(`{"updateGate":{"id":"gate_review","title":"Framing review"}}`))
	if got.Title != "Framing review" || got.Criterion != "Human framing review" || got.CheckValue != "complaint text" {
		t.Fatalf("title-only update = %+v, want other fields untouched", got)
	}
	got = gate(patch(`{"updateGate":{"id":"gate_review","kinds":["human"],"check":"","checkVersion":"","checkValue":""}}`))
	if len(got.Kinds) != 1 || got.Kinds[0] != "human" || got.Check != "" || got.CheckVersion != "" || got.CheckValue != "" {
		t.Fatalf("conversion to human = %+v", got)
	}
	got = gate(patch(`{"updateGate":{"id":"gate_review","criterion":""}}`))
	if got.Criterion != "" || got.Title != "Framing review" {
		t.Fatalf("criterion clear = %+v", got)
	}

	before := readFormationsAPIFile(t, store.BoardPath("session-search"))
	for body, want := range map[string]string{
		`{"updateGate":{"id":"gate_review","kinds":[]}}`:                   `"code":"INVALID_GATE_KIND"`,
		`{"updateGate":{"id":"gate_review","kinds":["robot"]}}`:            `"code":"INVALID_GATE_KIND"`,
		`{"updateGate":{"id":"gate_review","check":"output_contains"}}`:    `"code":"invalid_code_gate_profile"`,
		`{"updateGate":{"id":"gate_review","commandShell":"./gate.sh"}}`:   `"code":"` + formations.LegacyScriptGateMigrationCode + `"`,
		`{"updateGate":{"id":"gate_missing","title":"Nothing to rename"}}`: `"code":"NOT_FOUND"`,
	} {
		rec := patch(body)
		if rec.Code < 400 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s = %d %s, want %s", body, rec.Code, rec.Body.String(), want)
		}
	}
	if after := readFormationsAPIFile(t, store.BoardPath("session-search")); after != before {
		t.Fatalf("rejected updates changed the board:\n%s", after)
	}
}
