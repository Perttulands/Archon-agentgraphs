package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

type fakeAgentLiveness struct {
	live []formations.LiveAgentSession
	err  error
}

func (f fakeAgentLiveness) LiveAgentSessions() ([]formations.LiveAgentSession, error) {
	return f.live, f.err
}

func TestAgentsHandlerListsPersonaRosterJoinedWithLiveSessions(t *testing.T) {
	agentsDir := t.TempDir()
	writeAgentFixture(t, agentsDir, "susie", `schema = 1

[card]
id = "susie"
display_name = "Susie"
kind = "specialist"
tags = ["design", "react", "taste:visual"]
`)

	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{live: []formations.LiveAgentSession{
		{Name: "susie", Status: "idle", Attached: true},
		{Name: "scratch", Status: "working"},
	}})
	req := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	rec := httptest.NewRecorder()

	handler.ListAgents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Agents []formations.AgentProjection `json:"agents"`
			Count  int                          `json:"count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || body.Data.Count != 9 {
		t.Fatalf("response = %#v", body)
	}
	var susie, scratch *formations.AgentProjection
	for index := range body.Data.Agents {
		switch body.Data.Agents[index].ID {
		case "susie":
			susie = &body.Data.Agents[index]
		case "scratch":
			scratch = &body.Data.Agents[index]
		}
	}
	if susie == nil || susie.Liveness != formations.AgentLivenessLive || susie.SessionID != "susie" {
		t.Fatalf("susie projection = %#v, want the live session named after the role", susie)
	}
	if scratch == nil || !scratch.Unbound || scratch.Assignable {
		t.Fatalf("scratch projection = %#v, want unbound session", scratch)
	}
	if strings.Contains(rec.Body.String(), "harness\"") || strings.Contains(rec.Body.String(), "harnessDefault") {
		t.Fatalf("roster gives a role a harness: %s", rec.Body.String())
	}
}

func TestAgentsHandlerFiltersAssignableAndCapability(t *testing.T) {
	agentsDir := t.TempDir()
	writeAgentFixture(t, agentsDir, "susie", minimalAgentFixture("susie", "specialist", []string{"react", "taste:visual"}))
	writeAgentFixture(t, agentsDir, "codex", minimalAgentFixture("codex", "specialist", []string{"go"}))

	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{live: []formations.LiveAgentSession{{Name: "scratch", Status: "working"}}})
	req := httptest.NewRequest(http.MethodGet, "/api/agents?capable=react&assignable=1", nil)
	rec := httptest.NewRecorder()

	handler.ListAgents(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var filtered struct {
		Data struct {
			Agents []formations.AgentProjection `json:"agents"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &filtered); err != nil {
		t.Fatal(err)
	}
	if len(filtered.Data.Agents) != 1 || filtered.Data.Agents[0].ID != "susie" {
		t.Fatalf("filtered roster included non-matching/unbound agents: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "susie") {
		t.Fatalf("filtered roster missing susie: %s", rec.Body.String())
	}
}

func TestAgentsHandlerInspectServesNoRawTOML(t *testing.T) {
	agentsDir := t.TempDir()
	writeAgentFixture(t, agentsDir, "susie", minimalAgentFixture("susie", "specialist", []string{"react"}))

	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})
	req := httptest.NewRequest(http.MethodGet, "/api/agents/susie", nil)
	req.SetPathValue("agentId", "susie")
	rec := httptest.NewRecorder()

	handler.GetAgent(rec, req)

	if rec.Code != http.StatusOK || rec.Header().Get("ETag") == "" {
		t.Fatalf("status = %d, want 200 with an ETag: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, `"id":"susie"`) || strings.Contains(body, "toml") {
		t.Fatalf("inspect response = %s", body)
	}
}

func TestAgentsHandlerCreatesAndEditsThroughSharedWriter(t *testing.T) {
	agentsDir := t.TempDir()
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})

	createReq := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"writer","kind":"specialist","capabilities":["writing","voice"]}`))
	createRec := httptest.NewRecorder()
	handler.CreateAgent(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", createRec.Code, createRec.Body.String())
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/agents/writer", bytes.NewBufferString(`{"addCapability":"react","note":"ready for UI copy"}`))
	patchReq.SetPathValue("agentId", "writer")
	patchReq.Header.Set("If-Match", createRec.Header().Get("ETag"))
	patchRec := httptest.NewRecorder()
	handler.UpdateAgent(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200: %s", patchRec.Code, patchRec.Body.String())
	}
	raw := readAgentFixture(t, agentsDir, "writer")
	if !strings.Contains(raw, `"react"`) || !strings.Contains(raw, `text = "ready for UI copy"`) {
		t.Fatalf("shared writer did not persist edit:\n%s", raw)
	}
}

func TestAgentsHandlerOverridesABuiltinRoleThroughSharedWriter(t *testing.T) {
	agentsDir := t.TempDir()
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})

	getReq := httptest.NewRequest(http.MethodGet, "/api/agents/planner", nil)
	getReq.SetPathValue("agentId", "planner")
	getRec := httptest.NewRecorder()
	handler.GetAgent(getRec, getReq)
	if getRec.Code != http.StatusOK || getRec.Header().Get("ETag") == "" || !strings.Contains(getRec.Body.String(), `"preset":true`) {
		t.Fatalf("get preset = %d %s", getRec.Code, getRec.Body.String())
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/agents/planner", bytes.NewBufferString(`{"displayName":"Release Planner","summary":"Plans the selected release","capabilities":["planning","tickets"]}`))
	patchReq.SetPathValue("agentId", "planner")
	patchReq.Header.Set("If-Match", getRec.Header().Get("ETag"))
	patchRec := httptest.NewRecorder()
	handler.UpdateAgent(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch preset = %d %s", patchRec.Code, patchRec.Body.String())
	}
	var response struct {
		Data formations.PersonaCard `json:"data"`
	}
	if err := json.Unmarshal(patchRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode preset override: %v", err)
	}
	if response.Data.DisplayName != "Release Planner" || !response.Data.Customized {
		t.Fatalf("preset override = %+v", response.Data)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "planner.toml")); err != nil {
		t.Fatalf("materialized API override: %v", err)
	}
}

// A role is role text (ADR-0021): agent routes neither take nor serve a
// harness, a model, an effort or a seat launch; each slot states its own.
func TestAgentsHandlerServesARoleAsRoleTextOnly(t *testing.T) {
	agentsDir := t.TempDir()
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})
	created := httptest.NewRecorder()
	handler.CreateAgent(created, httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"critic","kind":"reviewer","summary":"Reviews against acceptance."}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/agents/critic", bytes.NewBufferString(`{"note":"judges plans too"}`))
	req.SetPathValue("agentId", "critic")
	req.Header.Set("If-Match", created.Header().Get("ETag"))
	edited := httptest.NewRecorder()
	handler.UpdateAgent(edited, req)
	if edited.Code != http.StatusOK {
		t.Fatalf("note = %d %s", edited.Code, edited.Body.String())
	}
	read := httptest.NewRequest(http.MethodGet, "/api/agents/critic", nil)
	read.SetPathValue("agentId", "critic")
	got := httptest.NewRecorder()
	handler.GetAgent(got, read)
	for _, body := range []string{created.Body.String(), edited.Body.String(), got.Body.String()} {
		for _, word := range []string{`"harness`, `"model"`, `"effort"`, `"session`, "effectiveEffort", "seatLaunch", `"efforts"`} {
			if strings.Contains(body, word) {
				t.Fatalf("agent answer carries %s: %s", word, body)
			}
		}
	}
	if raw := readAgentFixture(t, agentsDir, "critic"); strings.Contains(raw, "harness") || strings.Contains(raw, "model") || strings.Contains(raw, "effort") {
		t.Fatalf("card holds a harness, model or effort:\n%s", raw)
	}

	// A harness, model or effort sent anyway is refused by name, never dropped
	// silently.
	refusedCreate := httptest.NewRecorder()
	handler.CreateAgent(refusedCreate, httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"maker","kind":"builder","model":"opus"}`)))
	if refusedCreate.Code != http.StatusUnprocessableEntity || !strings.Contains(refusedCreate.Body.String(), `"code":"INVALID_AGENT_CARD"`) || !strings.Contains(refusedCreate.Body.String(), `agent request field \"model\" is not one a role takes`) {
		t.Fatalf("create with a model = %d %s", refusedCreate.Code, refusedCreate.Body.String())
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "maker.toml")); !os.IsNotExist(err) {
		t.Fatalf("a refused create wrote a card: %v", err)
	}
	refusedHarness := httptest.NewRecorder()
	handler.CreateAgent(refusedHarness, httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"maker","kind":"builder","harness":"claude-code"}`)))
	if refusedHarness.Code != http.StatusUnprocessableEntity || !strings.Contains(refusedHarness.Body.String(), `agent request field \"harness\" is not one a role takes`) {
		t.Fatalf("create with a harness = %d %s", refusedHarness.Code, refusedHarness.Body.String())
	}
	refusedEdit := httptest.NewRequest(http.MethodPatch, "/api/agents/critic", bytes.NewBufferString(`{"summary":"Reviews.","effort":"xhigh"}`))
	refusedEdit.SetPathValue("agentId", "critic")
	refusedEdit.Header.Set("If-Match", edited.Header().Get("ETag"))
	refused := httptest.NewRecorder()
	handler.UpdateAgent(refused, refusedEdit)
	if refused.Code != http.StatusUnprocessableEntity || !strings.Contains(refused.Body.String(), `agent request field \"effort\" is not one a role takes`) {
		t.Fatalf("edit with an effort = %d %s", refused.Code, refused.Body.String())
	}
	unknown := httptest.NewRecorder()
	handler.CreateAgent(unknown, httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"maker","launch":"claude"}`)))
	if unknown.Code != http.StatusUnprocessableEntity || !strings.Contains(unknown.Body.String(), `agent request field \"launch\" is not one Archon takes`) {
		t.Fatalf("create with a launch = %d %s", unknown.Code, unknown.Body.String())
	}

	list := httptest.NewRecorder()
	handler.ListAgents(list, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	var roster struct {
		Data struct {
			Harnesses    []formations.LaunchableHarness `json:"harnesses"`
			EffortPolicy []formations.EffortPolicyEntry `json:"effortPolicy"`
		} `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &roster); err != nil || len(roster.Data.Harnesses) != 2 || roster.Data.Harnesses[0].ID != "claude-code" || roster.Data.Harnesses[0].DefaultEffort != "medium" {
		t.Fatalf("roster harnesses = %+v (%v) from %s", roster.Data.Harnesses, err, list.Body.String())
	}
	wantPolicy := []formations.EffortPolicyEntry{
		{Effort: "low", Use: "errands", Kinds: []string{"verifier", "scout"}},
		{Effort: "medium", Use: "making things", Kinds: []string{"builder", "debugger", "operator"}},
		{Effort: "xhigh", Use: "architecture and review", Kinds: []string{"reviewer", "judge", "architect", "designer", "planner", "orchestrator"}},
		{Effort: "max", Use: "consequential reviews"},
	}
	if fmt.Sprint(roster.Data.EffortPolicy) != fmt.Sprint(wantPolicy) {
		t.Fatalf("roster effort policy = %+v, want %+v", roster.Data.EffortPolicy, wantPolicy)
	}
}

func TestAgentsHandlerRejectsStaleIfMatchWithoutClobber(t *testing.T) {
	agentsDir := t.TempDir()
	writeAgentFixture(t, agentsDir, "writer", minimalAgentFixture("writer", "specialist", []string{"writing"}))
	store := formations.NewPersonaStore(agentsDir)
	stale, err := store.ReadPersona("writer")
	if err != nil {
		t.Fatalf("read stale persona: %v", err)
	}
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})

	firstReq := httptest.NewRequest(http.MethodPatch, "/api/agents/writer", bytes.NewBufferString(`{"addCapability":"react"}`))
	firstReq.SetPathValue("agentId", "writer")
	firstReq.Header.Set("If-Match", stale.ETag)
	firstRec := httptest.NewRecorder()
	handler.UpdateAgent(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("first patch status = %d, want 200: %s", firstRec.Code, firstRec.Body.String())
	}
	afterFirst, err := store.ReadPersona("writer")
	if err != nil {
		t.Fatalf("read after first edit: %v", err)
	}
	if !containsString(afterFirst.Tags, "react") {
		t.Fatalf("first edit tags = %#v, want react", afterFirst.Tags)
	}

	staleReq := httptest.NewRequest(http.MethodPatch, "/api/agents/writer", bytes.NewBufferString(`{"removeCapability":"react","addCapability":"go"}`))
	staleReq.SetPathValue("agentId", "writer")
	staleReq.Header.Set("If-Match", stale.ETag)
	staleRec := httptest.NewRecorder()
	handler.UpdateAgent(staleRec, staleReq)
	if staleRec.Code != http.StatusConflict {
		t.Fatalf("stale patch status = %d, want 409: %s", staleRec.Code, staleRec.Body.String())
	}
	afterStale, err := store.ReadPersona("writer")
	if err != nil {
		t.Fatalf("read after stale edit: %v", err)
	}
	if !containsString(afterStale.Tags, "react") || containsString(afterStale.Tags, "go") {
		t.Fatalf("stale edit changed card tags = %#v, want react retained and go absent", afterStale.Tags)
	}
	if afterStale.ETag != afterFirst.ETag {
		t.Fatalf("stale edit changed ETag from %s to %s", afterFirst.ETag, afterStale.ETag)
	}
}

func TestAgentsHandlerDuplicateCreateFailsLoud(t *testing.T) {
	agentsDir := t.TempDir()
	writeAgentFixture(t, agentsDir, "writer", minimalAgentFixture("writer", "specialist", nil))
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})

	req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"writer","kind":"specialist"}`))
	rec := httptest.NewRecorder()
	handler.CreateAgent(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

func TestAgentsHandlerRejectsOversizedJSONBody(t *testing.T) {
	agentsDir := t.TempDir()
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})
	body := `{"id":"writer","kind":"specialist","summary":"` + strings.Repeat("x", 2*1024*1024) + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.CreateAgent(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
}

func writeAgentFixture(t *testing.T, agentsDir, id, raw string) {
	t.Helper()
	writeTextFile(t, filepath.Join(agentsDir, id+".toml"), raw)
}

func readAgentFixture(t *testing.T, agentsDir, id string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(agentsDir, id+".toml"))
	if err != nil {
		t.Fatalf("read agent fixture: %v", err)
	}
	return string(b)
}

func writeTextFile(t *testing.T, path, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func minimalAgentFixture(id, kind string, tags []string) string {
	return `schema = 1

[card]
id = "` + id + `"
kind = "` + kind + `"
tags = [` + formationsTestRenderStrings(tags) + `]
`
}

func formationsTestRenderStrings(values []string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, `"`+value+`"`)
	}
	return strings.Join(parts, ", ")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
