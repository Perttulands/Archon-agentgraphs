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

type apiErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeAPIError(t *testing.T, rec *httptest.ResponseRecorder) apiErrorBody {
	t.Helper()
	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body %s: %v", rec.Body.String(), err)
	}
	return body
}

func TestFormationsAPIReportsControllerRoleAndPortDirectionByField(t *testing.T) {
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
	if rec := serve(http.MethodPost, "/api/formations/boards", "", `{"title":"Fields","slug":"fields"}`); rec.Code != http.StatusCreated {
		t.Fatalf("create board = %d %s", rec.Code, rec.Body.String())
	}
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("fields")
		if err != nil {
			t.Fatal(err)
		}
		return serve(http.MethodPatch, "/api/formations/boards/fields", board.ETag, `{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{"))
	}
	for _, body := range []string{`{"createFormation":{"type":"solo","title":"Solo"}}`, `{"createFormation":{"type":"orchestrated","title":"Lead"}}`} {
		if rec := patch(body); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", body, rec.Code, rec.Body.String())
		}
	}
	board, err := store.ReadBoard("fields")
	if err != nil {
		t.Fatal(err)
	}
	solo, lead := board.Formations[0], board.Formations[1]

	cases := []struct {
		body    string
		code    string
		message string
	}{
		{
			body:    `{"makeController":{"formationId":"` + solo.ID + `","slotId":"` + solo.Slots[0].ID + `"}}`,
			code:    "INVALID_CONTROLLER_ROLE",
			message: `formation "` + solo.ID + `" is type "solo"; the controller role needs type "orchestrated"`,
		},
		{
			body:    `{"addPort":{"formationId":"` + solo.ID + `","direction":"sideways","label":"Extra"}}`,
			code:    "INVALID_PORT_DIRECTION",
			message: `port direction "sideways" must be input or output`,
		},
	}
	for _, tc := range cases {
		rec := patch(tc.body)
		response := decodeAPIError(t, rec)
		if rec.Code != http.StatusBadRequest || response.Error.Code != tc.code || response.Error.Message != tc.message {
			t.Errorf("%s = %d %s, want %s %q", tc.body, rec.Code, rec.Body.String(), tc.code, tc.message)
		}
	}
	if after, err := store.ReadBoard("fields"); err != nil || after.ETag != board.ETag {
		t.Fatalf("rejected field errors changed the board: %v", err)
	}

	if rec := patch(`{"makeController":{"formationId":"` + lead.ID + `","slotId":"` + lead.Slots[len(lead.Slots)-1].ID + `"}}`); rec.Code != http.StatusOK {
		t.Fatalf("orchestrated controller = %d %s", rec.Code, rec.Body.String())
	}
	for _, path := range []string{"/api/formations/boards/bad..slug", "/api/formations/boards/bad%2Fpath"} {
		rec := serve(http.MethodPatch, path, board.ETag, `{"title":"x"}`)
		if response := decodeAPIError(t, rec); rec.Code != http.StatusBadRequest || response.Error.Message != "Invalid formation slug" {
			t.Errorf("%s = %d %s, want Invalid formation slug", path, rec.Code, rec.Body.String())
		}
	}
}

func TestAgentsAPIReportsInvalidCardFields(t *testing.T) {
	agentsDir := t.TempDir()
	writeAgentFixture(t, agentsDir, "half", `schema = 1

[card]
id = "half"
display_name = "Half"

[[harness.variant]]
id = "claude-code"
`)
	writeAgentFixture(t, agentsDir, "renamed", `schema = 1

[card]
id = "original"
kind = "builder"

[harness]
default = "claude-code"

[[harness.variant]]
id = "claude-code"
`)
	writeAgentFixture(t, agentsDir, "novariant", `schema = 1

[card]
id = "novariant"
kind = "builder"

[harness]
default = "claude-code"
`)
	mux := http.NewServeMux()
	NewAgentsHandler(agentsDir, nil).RegisterRoutes(mux)
	serve := func(path string) (*httptest.ResponseRecorder, apiErrorBody) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec, decodeAPIError(t, rec)
	}
	for id, message := range map[string]string{
		"half":      `agent card "half" is missing card.kind, harness.default`,
		"renamed":   `agent card file "renamed" holds card.id "original"; they must match`,
		"novariant": `agent card "novariant" needs a [[harness.variant]]`,
	} {
		if rec, response := serve("/api/agents/" + id); rec.Code != http.StatusUnprocessableEntity || response.Error.Code != "INVALID_AGENT_CARD" || response.Error.Message != message {
			t.Errorf("GET agent %s = %d %s, want INVALID_AGENT_CARD %q", id, rec.Code, rec.Body.String(), message)
		}
	}
	if rec, response := serve("/api/agents"); rec.Code != http.StatusUnprocessableEntity || response.Error.Code != "INVALID_AGENT_CARD" {
		t.Errorf("GET agents = %d %s, want INVALID_AGENT_CARD", rec.Code, rec.Body.String())
	}
	if rec, response := serve("/api/agents/Bad.Agent"); rec.Code != http.StatusBadRequest || response.Error.Code != "BAD_REQUEST" {
		t.Errorf("GET invalid agent id = %d %s, want BAD_REQUEST", rec.Code, rec.Body.String())
	}
}
