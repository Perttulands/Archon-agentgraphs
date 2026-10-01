package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestWireErrorsAndAtomicJoinHTTP(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	raw := "schema = 1\nid = \"brd_join\"\nslug = \"join\"\nrev = 1\n"
	for _, id := range []string{"a", "b", "c", "sink"} {
		raw += fmt.Sprintf("\n[[formation]]\nid = %q\ntype = \"solo\"\n[[formation.input]]\nid = \"in\"\n[[formation.output]]\nid = \"out\"\n", id)
	}
	writeFormationsAPIFixture(t, store.BoardPath("join"), raw)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStore(store).RegisterRoutes(mux)
	board, _ := store.ReadBoard("join")
	patch := func(body, etag string, rev int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/api/missions/join", strings.NewReader(fmt.Sprintf(`{%s,"expectedRev":%d}`, body, rev)))
		req.Header.Set("If-Match", etag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	first := patch(`"wireConnection":{"from":"a:out","to":"sink:in"}`, board.ETag, board.Rev)
	if first.Code != 200 {
		t.Fatal(first.Body.String())
	}
	board, _ = store.ReadBoard("join")
	for _, tc := range []struct {
		name, body, code, message string
		status                    int
		stale                     bool
	}{
		{"occupied", `"wireConnection":{"from":"b:out","to":"sink:in"}`, "INPUT_OCCUPIED", "Input already has a feed", 409, false},
		{"duplicate", `"wireConnection":{"from":"a:out","to":"sink:in","joinIfOccupied":true}`, "DUPLICATE_CONNECTION", "already exists", 409, false},
		{"self", `"wireConnection":{"from":"sink:out","to":"sink:in","joinIfOccupied":true}`, "SELF_WIRE", "itself", 422, false},
		{"stale", `"wireConnection":{"from":"b:out","to":"sink:in","joinIfOccupied":true}`, "CONFLICT", "reload it and retry", 409, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			etag := board.ETag
			if tc.stale {
				etag = `"stale"`
			}
			rec := patch(tc.body, etag, board.Rev)
			if rec.Code != tc.status || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"`+tc.code+`"`)) || !strings.Contains(rec.Body.String(), tc.message) {
				t.Fatalf("response %d %s", rec.Code, rec.Body.String())
			}
			after, _ := store.ReadBoard("join")
			if after.ETag != board.ETag {
				t.Fatal("failed edit changed board")
			}
		})
	}
	for _, from := range []string{"b:out", "c:out"} {
		rec := patch(fmt.Sprintf(`"wireConnection":{"from":%q,"to":"sink:in","joinIfOccupied":true}`, from), board.ETag, board.Rev)
		if rec.Code != 200 {
			t.Fatal(rec.Body.String())
		}
		var response struct {
			Data struct {
				Board formations.BoardDocument `json:"mission"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		next := response.Data.Board
		if next.Rev != board.Rev+1 || len(next.Connections) != len(board.Connections)+1 || len(next.Formations[3].Inputs) != len(board.Formations[3].Inputs)+1 {
			t.Fatal("join was not one revision")
		}
		board = &next
	}
}

func TestToolWireIncompatibilityHTTP(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("tool-parity"), formationsAPIToolParityBoardFixture())
	board, _ := store.ReadBoard("tool-parity")
	mux := http.NewServeMux()
	NewFormationsHandlerWithStore(store).RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPatch, "/api/missions/tool-parity", strings.NewReader(fmt.Sprintf(`{"wireConnection":{"from":"mis_main:out","to":"tool_sink:port_sink_in","joinIfOccupied":true},"expectedRev":%d}`, board.Rev)))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"code":"INCOMPATIBLE_TOOL_CONNECTION"`) {
		t.Fatalf("response %d %s", rec.Code, rec.Body.String())
	}
}
