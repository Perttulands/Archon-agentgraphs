package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/buildinfo"
)

// /healthz names the running build, so a deploy can prove which source it
// started (archon-1ea).
func TestHealthReportsTheRunningBuild(t *testing.T) {
	previousVersion, previousCommit := buildinfo.Version, buildinfo.Commit
	buildinfo.Version, buildinfo.Commit = "0.9.0", "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { buildinfo.Version, buildinfo.Commit = previousVersion, previousCommit })
	c, _, _ := fixture(t)
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	var body struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
		t.Fatalf("healthz = %d %s", w.Code, w.Body.String())
	}
	want := map[string]string{"status": "ok", "version": "0.9.0", "commit": "0123456789abcdef0123456789abcdef01234567"}
	if len(body.Data) != len(want) || body.Data["status"] != want["status"] || body.Data["version"] != want["version"] || body.Data["commit"] != want["commit"] {
		t.Fatalf("healthz data = %v, want %v", body.Data, want)
	}
}
