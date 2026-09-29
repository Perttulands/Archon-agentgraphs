package api

import (
	"bytes"
	"encoding/json"
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

[harness]
default = "claude-code"

[[harness.variant]]
id = "claude-code"
session_stem = "susie"
source = "/tmp/CLAUDE.md"
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
	if !body.Success || body.Data.Count != 15 {
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
	if susie == nil || susie.Liveness != formations.AgentLivenessLive || susie.HarnessDefault != "claude-code" {
		t.Fatalf("susie projection = %#v, want live claude-code persona", susie)
	}
	if scratch == nil || !scratch.Unbound || scratch.Assignable {
		t.Fatalf("scratch projection = %#v, want unbound session", scratch)
	}
	if strings.Contains(rec.Body.String(), "CLAUDE.md contents") ||
		strings.Contains(rec.Body.String(), "/tmp/CLAUDE.md") ||
		strings.Contains(rec.Body.String(), "harnessVariants") {
		t.Fatalf("roster response leaked source or harness internals: %s", rec.Body.String())
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

func TestAgentsHandlerInspectReturnsPointersWithoutInliningSource(t *testing.T) {
	agentsDir := t.TempDir()
	sourcePath := filepath.Join(t.TempDir(), "CLAUDE.md")
	writeTextFile(t, sourcePath, "CLAUDE.md contents must stay out of API responses")
	writeAgentFixture(t, agentsDir, "susie", `schema = 1

[card]
id = "susie"
kind = "specialist"
tags = ["react"]

[harness]
default = "claude-code"

[[harness.variant]]
id = "claude-code"
session_stem = "susie"
source = "`+sourcePath+`"
`)

	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})
	req := httptest.NewRequest(http.MethodGet, "/api/agents/susie", nil)
	req.SetPathValue("agentId", "susie")
	rec := httptest.NewRecorder()

	handler.GetAgent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, sourcePath) {
		t.Fatalf("inspect response missing source pointer: %s", body)
	}
	if strings.Contains(body, "CLAUDE.md contents") || strings.Contains(body, "toml") {
		t.Fatalf("inspect response leaked source contents or raw TOML: %s", body)
	}
}

func TestAgentsHandlerCreatesAndEditsThroughSharedWriter(t *testing.T) {
	agentsDir := t.TempDir()
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})

	createReq := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"writer","kind":"specialist","harness":"claude-code","capabilities":["writing","voice"]}`))
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

func TestAgentsHandlerOverridesBuiltInCodexPresetThroughSharedWriter(t *testing.T) {
	agentsDir := t.TempDir()
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})

	getReq := httptest.NewRequest(http.MethodGet, "/api/agents/codex-planner", nil)
	getReq.SetPathValue("agentId", "codex-planner")
	getRec := httptest.NewRecorder()
	handler.GetAgent(getRec, getReq)
	if getRec.Code != http.StatusOK || getRec.Header().Get("ETag") == "" || !strings.Contains(getRec.Body.String(), `"preset":true`) {
		t.Fatalf("get preset = %d %s", getRec.Code, getRec.Body.String())
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/api/agents/codex-planner", bytes.NewBufferString(`{"displayName":"Delivery Planner","summary":"Plans the selected delivery","capabilities":["planning","tickets"],"sessionStem":"planner-main","launch":"codex --yolo --model gpt-5.6-codex"}`))
	patchReq.SetPathValue("agentId", "codex-planner")
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
	if response.Data.DisplayName != "Delivery Planner" || !response.Data.Customized || response.Data.DefaultVariant().SessionStem != "planner-main" {
		t.Fatalf("preset override = %+v", response.Data)
	}
	if _, err := os.Stat(filepath.Join(agentsDir, "codex-planner.toml")); err != nil {
		t.Fatalf("materialized API override: %v", err)
	}
}

func TestAgentsHandlerCreatesAndEditsModelAndEffortAndShowsTheSeatLaunch(t *testing.T) {
	bin := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	agentsDir := t.TempDir()
	handler := NewAgentsHandler(agentsDir, fakeAgentLiveness{})
	decode := func(rec *httptest.ResponseRecorder) formations.PersonaCard {
		t.Helper()
		var response struct {
			Data formations.PersonaCard `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
		return response.Data
	}
	patch := func(body, etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/agents/critic", bytes.NewBufferString(body))
		req.SetPathValue("agentId", "critic")
		req.Header.Set("If-Match", etag)
		rec := httptest.NewRecorder()
		handler.UpdateAgent(rec, req)
		return rec
	}

	bad := httptest.NewRecorder()
	handler.CreateAgent(bad, httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"critic","harness":"claude-code","effort":"ultra"}`)))
	if bad.Code != http.StatusUnprocessableEntity || !strings.Contains(bad.Body.String(), "low, medium, high, xhigh, max") {
		t.Fatalf("invalid effort create = %d %s", bad.Code, bad.Body.String())
	}

	created := httptest.NewRecorder()
	handler.CreateAgent(created, httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"critic","harness":"claude-code","model":"claude-opus-5"}`)))
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", created.Code, created.Body.String())
	}
	claude := decode(created).DefaultVariant()
	want := "exec '" + filepath.Join(bin, "claude") + "' --model 'claude-opus-5' --effort 'medium' --dangerously-skip-permissions"
	if claude.Launch != "" || claude.Model != "claude-opus-5" || claude.Effort != "" || claude.EffectiveEffort != "medium" || claude.SeatLaunch != want {
		t.Fatalf("created variant = %+v, want seat launch %s", claude, want)
	}

	added := patch(`{"addHarness":"openai-codex","model":"gpt-6-sol","effort":"ultra"}`, created.Header().Get("ETag"))
	if added.Code != http.StatusOK {
		t.Fatalf("add harness = %d %s", added.Code, added.Body.String())
	}
	edited := patch(`{"variant":"openai-codex","effort":"xhigh","model":""}`, added.Header().Get("ETag"))
	if edited.Code != http.StatusOK {
		t.Fatalf("edit variant = %d %s", edited.Code, edited.Body.String())
	}
	card := decode(edited)
	codex, _ := card.SelectHarnessVariant("openai-codex")
	if codex.Model != "" || codex.Effort != "xhigh" || !strings.Contains(codex.SeatLaunch, `model_reasoning_effort="xhigh"`) || strings.Contains(codex.SeatLaunch, "--model") {
		t.Fatalf("edited codex variant = %+v", codex)
	}
	if card.DefaultVariant().Model != "claude-opus-5" {
		t.Fatalf("default variant changed: %+v", card.DefaultVariant())
	}
	if rec := patch(`{"variant":"openai-codex","effort":"extreme"}`, edited.Header().Get("ETag")); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "ultra") {
		t.Fatalf("invalid codex effort = %d %s", rec.Code, rec.Body.String())
	}
	both := patch(`{"variants":[{"id":"claude-code","effort":"high"},{"id":"openai-codex","model":"gpt-6-luna"}]}`, edited.Header().Get("ETag"))
	if both.Code != http.StatusOK {
		t.Fatalf("edit both variants = %d %s", both.Code, both.Body.String())
	}
	card = decode(both)
	codex, _ = card.SelectHarnessVariant("openai-codex")
	if claude := card.DefaultVariant(); claude.Effort != "high" || claude.Model != "claude-opus-5" || codex.Model != "gpt-6-luna" || codex.Effort != "xhigh" {
		t.Fatalf("edited both variants = %+v / %+v", claude, codex)
	}
	if rec := patch(`{"variants":[{"id":"claude-code","effort":"low"},{"id":"openai-codex","effort":"nope"}]}`, both.Header().Get("ETag")); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("one invalid variant = %d %s", rec.Code, rec.Body.String())
	}
	if claude := decode(both).DefaultVariant(); claude.Effort != "high" {
		t.Fatalf("partially applied edit: %+v", claude)
	}
	if raw := readAgentFixture(t, agentsDir, "critic"); strings.Contains(raw, `effort = "low"`) {
		t.Fatalf("an edit with one invalid variant was partly written:\n%s", raw)
	}
	if raw := readAgentFixture(t, agentsDir, "critic"); strings.Contains(raw, "launch") || strings.Contains(raw, "seatLaunch") || strings.Contains(raw, "extreme") {
		t.Fatalf("card stores derived or rejected values:\n%s", raw)
	}

	list := httptest.NewRecorder()
	handler.ListAgents(list, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	var roster struct {
		Data struct {
			Harnesses []formations.LaunchableHarness `json:"harnesses"`
		} `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &roster); err != nil || len(roster.Data.Harnesses) != 2 || roster.Data.Harnesses[0].ID != "claude-code" || roster.Data.Harnesses[0].DefaultEffort != "medium" {
		t.Fatalf("roster harnesses = %+v (%v) from %s", roster.Data.Harnesses, err, list.Body.String())
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

	req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"id":"writer","kind":"specialist","harness":"claude-code"}`))
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

[harness]
default = "claude-code"

[[harness.variant]]
id = "claude-code"
session_stem = "` + id + `"
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
