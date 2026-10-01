package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// OpenAPI documents the current routes and names only: every route is under
// /api without the old /formations prefix, nothing is deprecated, and no
// path, field or parameter says board.
func TestOpenAPIDocumentsOnlyCurrentRoutes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "openapi", "archon.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, path := range []string{"/api/missions", "/api/missions/{mission}", "/api/missions/{mission}/validation", "/api/missions/{mission}/changes",
		"/api/missions/{mission}/notes", "/api/missions/{mission}/layout", "/api/runs", "/api/runs/{runId}"} {
		if !strings.Contains(doc, "\n  "+path+":\n") {
			t.Fatalf("OpenAPI lacks %s", path)
		}
	}
	for _, stale := range []string{"/api/formations", "deprecated: true"} {
		if strings.Contains(doc, stale) {
			t.Fatalf("OpenAPI still documents %q", stale)
		}
	}
	if word := regexp.MustCompile(`(?i)\bboards?\b|board[A-Z]\w*`).FindString(doc); word != "" {
		t.Fatalf("OpenAPI still says %q", word)
	}
}
