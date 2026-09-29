package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OpenAPI documents each mission route and marks every operation of the
// former board route deprecated.
func TestOpenAPIDocumentsMissionRoutesAndDeprecatesBoardRoutes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "openapi", "formations.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	block := func(path string) string {
		t.Helper()
		start := strings.Index(doc, "\n  "+path+":\n")
		if start < 0 {
			t.Fatalf("OpenAPI lacks %s", path)
		}
		rest := doc[start+1:]
		if end := strings.Index(rest[1:], "\n  /"); end >= 0 {
			rest = rest[:end+1]
		}
		return rest
	}
	for _, suffix := range []string{"", "/{id}", "/{id}/validation", "/{id}/changes", "/{id}/notes", "/{id}/layout"} {
		mission := block("/api/formations/missions" + strings.ReplaceAll(suffix, "{id}", "{mission}"))
		board := block("/api/formations/boards" + strings.ReplaceAll(suffix, "{id}", "{board}"))
		if strings.Contains(mission, "deprecated: true") {
			t.Fatalf("mission route%s is marked deprecated:\n%s", suffix, mission)
		}
		for _, method := range []string{"get", "post", "patch", "delete"} {
			if !strings.Contains(mission, "\n    "+method+":") {
				continue
			}
			if !strings.Contains(board, "\n    "+method+":\n      deprecated: true\n") {
				t.Fatalf("board route%s %s is not marked deprecated:\n%s", suffix, method, board)
			}
		}
	}
	runs := block("/api/formations/runs")
	if !strings.Contains(runs, "name: mission") || !strings.Contains(runs, "name: board\n        deprecated: true") {
		t.Fatalf("run list does not document ?mission= with ?board= deprecated:\n%s", runs)
	}
}
