package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// useCodexFixture gives the test a Codex model cache with two listed models.
func useCodexFixture(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cache := `{"models":[
  {"slug":"gpt-5.5","visibility":"list","priority":13,"supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"}]},
  {"slug":"gpt-6-astra","visibility":"list","priority":2,"supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"},{"effort":"max"},{"effort":"ultra"}]}
]}`
	if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", dir)
}

// The roster offers each harness's known models, so the cockpit names no
// model of its own (archon-v53).
func TestAgentsRosterOffersEachHarnessesModels(t *testing.T) {
	useCodexFixture(t)
	handler := NewAgentsHandler(t.TempDir(), nil)
	rec := httptest.NewRecorder()
	handler.ListAgents(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			Harnesses []formations.LaunchableHarness `json:"harnesses"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	models := map[string][]formations.HarnessModel{}
	for _, harness := range body.Data.Harnesses {
		models[harness.ID] = harness.Models
	}
	want := map[string][]formations.HarnessModel{
		"claude-code": {{ID: "opus"}, {ID: "sonnet"}, {ID: "haiku"}, {ID: "fable"}},
		"openai-codex": {
			{ID: "gpt-6-astra", Efforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
			{ID: "gpt-5.5", Efforts: []string{"low", "medium", "high", "xhigh"}},
		},
	}
	if !reflect.DeepEqual(models, want) {
		t.Fatalf("harness models = %+v, want %+v", models, want)
	}
}

// A model outside the catalog is staffed, and the response says so; a known
// model's levels bound the effort.
func TestAssignSlotWarnsOfAModelOutsideTheCatalog(t *testing.T) {
	useCodexFixture(t)
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("staff"), `schema = 1
id = "brd_staff"
slug = "staff"
title = "Staff"
rev = 1

[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.slot]]
id = "slot_a"
label = "A"
controller = false
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(t.TempDir())).RegisterRoutes(mux)
	patch := func(assign string) (int, map[string]json.RawMessage) {
		t.Helper()
		board, err := store.ReadBoard("staff")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/missions/staff", bytes.NewBufferString(`{"assignSlot":`+assign+`,"expectedRev":`+itoa(board.Rev)+`}`))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		var response struct {
			Data  map[string]json.RawMessage `json:"data"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK {
			return rec.Code, map[string]json.RawMessage{"code": json.RawMessage(`"` + response.Error.Code + `"`), "message": mustJSON(response.Error.Message)}
		}
		return rec.Code, response.Data
	}

	code, data := patch(`{"formationId":"fmn_work","slotId":"slot_a","harness":"openai-codex","model":"gpt-7-nova","effort":"ultra"}`)
	if code != http.StatusOK || string(data["warnings"]) != `["slot \"A\" (slot_a) model \"gpt-7-nova\" is not in the openai-codex catalog; the harness decides"]` {
		t.Fatalf("off-catalog model = %d %s", code, data["warnings"])
	}
	if code, data = patch(`{"formationId":"fmn_work","slotId":"slot_a","harness":"openai-codex","model":"gpt-6-astra","effort":"ultra"}`); code != http.StatusOK || data["warnings"] != nil {
		t.Fatalf("known model = %d warnings %s", code, data["warnings"])
	}
	if code, data = patch(`{"formationId":"fmn_work","slotId":"slot_a","harness":"openai-codex","model":"gpt-5.5","effort":"ultra"}`); code != http.StatusUnprocessableEntity || string(data["code"]) != `"INVALID_SLOT_SETTINGS"` || string(data["message"]) != mustJSONString(`slot "slot_a" effort "ultra" is not one gpt-5.5 accepts; use low, medium, high, xhigh`) {
		t.Fatalf("a level gpt-5.5 lacks = %d %s %s", code, data["code"], data["message"])
	}
}

func mustJSON(value string) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}

func mustJSONString(value string) string { return string(mustJSON(value)) }

// The roster carries each role's one-line summary, which the staffing role
// list shows beside the role.
func TestAgentsRosterCarriesRoleSummaries(t *testing.T) {
	agentsDir := t.TempDir()
	writeAgentFixture(t, agentsDir, "critic", strings.Replace(minimalAgentFixture("critic", "reviewer", nil), "kind = \"reviewer\"\n", "kind = \"reviewer\"\nsummary = \"Reviews against acceptance.\"\n", 1))
	rec := httptest.NewRecorder()
	NewAgentsHandler(agentsDir, nil).ListAgents(rec, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	var body struct {
		Data struct {
			Agents []formations.AgentProjection `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, agent := range body.Data.Agents {
		if agent.ID == "critic" {
			if agent.Summary != "Reviews against acceptance." {
				t.Fatalf("critic summary = %q", agent.Summary)
			}
			return
		}
	}
	t.Fatalf("roster has no critic: %+v", body.Data.Agents)
}
