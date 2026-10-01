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

	for _, want := range []string{"untitled-mission", "untitled-mission-2"} {
		rec := serve(http.MethodPost, "/api/missions", "", `{"title":"  "}`)
		if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"slug":"`+want+`"`) {
			t.Fatalf("unnamed board create = %d %s, want %s", rec.Code, rec.Body.String(), want)
		}
	}

	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("untitled-mission")
		if err != nil {
			t.Fatal(err)
		}
		return serve(http.MethodPatch, "/api/missions/untitled-mission", board.ETag, `{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{"))
	}
	for _, body := range []string{
		`{"createInputCard":{"title":"","goal":""}}`,
		`{"createFormation":{"type":"solo","title":""}}`,
		`{"createGate":{"title":"","kinds":["code"],"criterion":"","check":"output_absent","checkVersion":"1","checkValue":""}}`,
	} {
		if rec := patch(body); rec.Code != http.StatusOK {
			t.Fatalf("draft patch %s = %d %s, want saved", body, rec.Code, rec.Body.String())
		}
	}

	board, err := store.ReadBoard("untitled-mission")
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

	var validation struct {
		Data struct {
			Errors []formations.BoardFinding `json:"errors"`
		} `json:"data"`
	}
	validate := func() []formations.BoardFinding {
		t.Helper()
		rec := serve(http.MethodGet, "/api/missions/untitled-mission/validation", "", "")
		validation.Data.Errors = nil
		if err := json.Unmarshal(rec.Body.Bytes(), &validation); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("validation = %d %s (%v)", rec.Code, rec.Body.String(), err)
		}
		return validation.Data.Errors
	}
	// The draft saves with its gate's routes leading nowhere; validation names
	// them (form-o7p.10).
	if errors := validate(); len(errors) != 4 || len(findingsWithCode(errors, formations.FindingRouteLeadsNowhere)) != 2 {
		t.Fatalf("validation = %+v, want unstaffed slot, incomplete gate and two routes leading nowhere", errors)
	}
	// One End node takes any number of routes: both of the gate's routes end
	// the path there.
	if rec := patch(`{"createEnd":{"outcome":"done"}}`); rec.Code != http.StatusOK {
		t.Fatalf("create End = %d %s", rec.Code, rec.Body.String())
	}
	board, _ = store.ReadBoard("untitled-mission")
	if len(board.Ends) != 1 || board.Ends[0].Title != "Done" || board.Ends[0].Outcome != formations.EndOutcomeDone {
		t.Fatalf("End nodes = %+v", board.Ends)
	}
	for _, port := range []string{"pass", "fail"} {
		if rec := patch(`{"wireConnection":{"from":"` + gate.ID + `:` + port + `","to":"` + board.Ends[0].ID + `:in"}}`); rec.Code != http.StatusOK {
			t.Fatalf("wire %s to the End node = %d %s", port, rec.Code, rec.Body.String())
		}
	}
	if errors := validate(); len(errors) != 2 {
		t.Fatalf("validation = %+v, want unstaffed slot and incomplete gate", errors)
	}

	board, _ = store.ReadBoard("untitled-mission")
	rec := serve(http.MethodPost, "/api/runs", board.ETag, `{"mission":"untitled-mission","inputCardId":"`+mission.ID+`","expectedRev":`+jsonInt(board.Rev)+`,"limits":{"maxDispatch":3,"maxAttempts":1,"wallClockSeconds":60}}`)
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

func findingsWithCode(findings []formations.BoardFinding, code string) []formations.BoardFinding {
	var out []formations.BoardFinding
	for _, finding := range findings {
		if finding.Code == code {
			out = append(out, finding)
		}
	}
	return out
}

func jsonInt(value int) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
