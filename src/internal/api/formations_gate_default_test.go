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

func TestFormationsAPICreateGateWithoutKindsStartsAsRoutableHumanGate(t *testing.T) {
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
	if rec := serve(http.MethodPost, "/api/formations/boards", "", `{"title":"Gates","slug":"gates"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create board = %d %s", rec.Code, rec.Body.String())
	}
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("gates")
		if err != nil {
			t.Fatal(err)
		}
		return serve(http.MethodPatch, "/api/formations/boards/gates", board.ETag, `{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{"))
	}
	createGate := func(body string) formations.GateNode {
		t.Helper()
		rec := patch(body)
		var response struct {
			Data formations.GateCreateResult `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s (%v)", body, rec.Code, rec.Body.String(), err)
		}
		return response.Data.Gate
	}

	human := createGate(`{"createGate":{"title":"Signoff"}}`)
	if strings.Join(human.Kinds, ",") != "human" {
		t.Fatalf("createGate without kinds = %+v, want kinds [human]", human)
	}
	if code := createGate(`{"createGate":{"title":"Lint","kinds":["code"]}}`); strings.Join(code.Kinds, ",") != "code" {
		t.Fatalf("createGate with kinds [code] = %+v, want them unchanged", code)
	}
	rec := patch(`{"createGate":{"title":"Stray","check":"output_contains","checkVersion":"1","checkValue":"OK"}}`)
	if response := decodeAPIError(t, rec); rec.Code != http.StatusUnprocessableEntity || response.Error.Code != formations.FindingInvalidCodeGateProfile {
		t.Fatalf("createGate with a check and no kinds = %d %s, want the code kind required", rec.Code, rec.Body.String())
	}

	for _, body := range []string{
		`{"createMission":{"title":"Work","goal":"Do it","beadId":"form-demo"}}`,
		`{"createFormation":{"type":"solo","title":"Worker"}}`,
	} {
		if rec := patch(body); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
	board, err := store.ReadBoard("gates")
	if err != nil {
		t.Fatal(err)
	}
	mission, worker := board.Missions[0], board.Formations[0]
	for _, body := range []string{
		`{"assignSlot":{"formationId":"` + worker.ID + `","slotId":"` + worker.Slots[0].ID + `","agentId":"codex-builder","harness":"openai-codex"}}`,
		`{"setBrief":{"formationId":"` + worker.ID + `","goal":"Produce the result"}}`,
		`{"wireConnection":{"from":"` + mission.ID + `:out","to":"` + worker.ID + `:` + worker.Inputs[0].ID + `"}}`,
		`{"wireConnection":{"from":"` + worker.ID + `:` + worker.Outputs[0].ID + `","to":"` + human.ID + `:in"}}`,
	} {
		if rec := patch(body); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
	rec = serve(http.MethodGet, "/api/formations/boards/gates/validation", "", "")
	var validation struct {
		Data struct {
			Errors   []formations.BoardFinding `json:"errors"`
			Warnings []formations.BoardFinding `json:"warnings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &validation); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("validation = %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	for _, finding := range append(validation.Data.Errors, validation.Data.Warnings...) {
		if finding.NodeID == human.ID || finding.NodeID == worker.ID || finding.NodeID == mission.ID {
			t.Errorf("wired human gate path has finding %+v", finding)
		}
	}
}
