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

func TestCharactersAreSavedReplacedAndDeleted(t *testing.T) {
	dir := t.TempDir()
	mux := http.NewServeMux()
	NewCharactersHandler(formations.NewCharacterStore(dir)).RegisterRoutes(mux)
	call := func(method, path, body string) (int, map[string]json.RawMessage) {
		t.Helper()
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
		var envelope struct {
			Data  map[string]json.RawMessage `json:"data"`
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("%s %s: %v: %s", method, path, err, rec.Body.String())
		}
		if envelope.Data == nil {
			envelope.Data = map[string]json.RawMessage{"code": json.RawMessage(`"` + envelope.Error.Code + `"`), "message": json.RawMessage(`"` + strings.ReplaceAll(envelope.Error.Message, `"`, `'`) + `"`)}
		}
		return rec.Code, envelope.Data
	}

	if code, data := call("GET", "/api/characters", ""); code != http.StatusOK || string(data["characters"]) != "[]" {
		t.Fatalf("empty list = %d %s", code, data)
	}
	code, data := call("POST", "/api/characters", `{"name":"Careful Builder","description":"Builds slowly","agentId":"codex-builder","harness":"openai-codex","effort":"high"}`)
	if code != http.StatusCreated || string(data["id"]) != `"careful-builder"` {
		t.Fatalf("create = %d %s", code, data)
	}
	if code, data := call("POST", "/api/characters", `{"name":"Careful builder","harness":"claude-code","effort":"low"}`); code != http.StatusConflict || string(data["code"]) != `"CHARACTER_EXISTS"` {
		t.Fatalf("duplicate create = %d %s", code, data)
	}
	for name, body := range map[string]string{
		"no name":       `{"harness":"claude-code","effort":"low"}`,
		"no effort":     `{"name":"A","harness":"claude-code"}`,
		"bad harness":   `{"name":"A","harness":"pi","effort":"low"}`,
		"bad effort":    `{"name":"A","harness":"claude-code","effort":"ultra"}`,
		"bad picture":   `{"name":"A","harness":"claude-code","effort":"low","picture":"https://example.test/a.png"}`,
		"unknown field": `{"name":"A","harness":"claude-code","effort":"low","kind":"x"}`,
	} {
		if code, data := call("POST", "/api/characters", body); code != http.StatusUnprocessableEntity || string(data["code"]) != `"INVALID_CHARACTER"` {
			t.Fatalf("%s = %d %s", name, code, data)
		}
	}

	code, data = call("PUT", "/api/characters/careful-builder", `{"name":"Careful Builder","harness":"claude-code","model":"opus","effort":"max","picture":"data:image/png;base64,AAAA"}`)
	if code != http.StatusOK || string(data["harness"]) != `"claude-code"` || data["agentId"] != nil {
		t.Fatalf("replace = %d %s", code, data)
	}
	if code, _ := call("PUT", "/api/characters/nobody", `{"name":"Nobody","harness":"claude-code","effort":"low"}`); code != http.StatusNotFound {
		t.Fatalf("replace missing = %d", code)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "characters.json"))
	if err != nil || !strings.Contains(string(raw), `"schema": 1`) || !strings.Contains(string(raw), `"model": "opus"`) {
		t.Fatalf("characters.json = %s, %v", raw, err)
	}

	if code, _ := call("DELETE", "/api/characters/careful-builder", ""); code != http.StatusOK {
		t.Fatalf("delete = %d", code)
	}
	if code, _ := call("DELETE", "/api/characters/careful-builder", ""); code != http.StatusNotFound {
		t.Fatalf("delete again = %d", code)
	}
}
