package coordinator

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// The daemon opens any file a reference names, with no flags (ADR-0021).
func TestFileRoutesServeAnyAbsolutePath(t *testing.T) {
	c, _, _ := fixture(t)
	base := t.TempDir()
	project := filepath.Join(base, "project")
	elsewhere := filepath.Join(base, "elsewhere")
	for path, content := range map[string]string{
		filepath.Join(project, "rubrics", "quality.md"): "# Quality\n\ntoken: abc123\n",
		filepath.Join(elsewhere, "notes.md"):            "elsewhere\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(elsewhere, "notes.md"), filepath.Join(project, "rubrics", "link.md")); err != nil {
		t.Fatal(err)
	}
	route := func(kind, ref string) string {
		return "/api/files/" + kind + "?path=" + url.QueryEscape(ref)
	}

	w := getEvidence(c, route("preview", filepath.Join(project, "rubrics", "quality.md")))
	preview := decodeEvidence[formations.ReferencedFilePreview](t, w, "file")
	if preview.Path != filepath.Join(project, "rubrics", "quality.md") || preview.Kind != "markdown" || preview.Text == nil || !strings.Contains(preview.Text.Text, "# Quality") || strings.Contains(preview.Text.Text, "abc123") {
		t.Fatalf("preview = %s", w.Body.String())
	}

	raw := getEvidence(c, route("raw", filepath.Join(project, "rubrics", "quality.md")))
	if raw.Code != 200 || raw.Header().Get("Content-Type") != "text/plain; charset=utf-8" || raw.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(raw.Header().Get("Content-Security-Policy"), "sandbox") || raw.Header().Get("Cache-Control") != "no-store" || strings.Contains(raw.Body.String(), "abc123") {
		t.Fatalf("raw = %d %v %s", raw.Code, raw.Header(), raw.Body.String())
	}

	for _, ref := range []string{filepath.Join(elsewhere, "notes.md"), filepath.Join(project, "rubrics", "link.md"), project + "/../elsewhere/notes.md"} {
		for _, kind := range []string{"preview", "raw"} {
			if w := getEvidence(c, route(kind, ref)); w.Code != 200 || !strings.Contains(w.Body.String(), "elsewhere") {
				t.Errorf("%s %q = %d %s, want the file", kind, ref, w.Code, w.Body.String())
			}
		}
	}
	for ref, want := range map[string]int{
		"rubrics/quality.md":                            400,
		filepath.Join(project, "rubrics", "missing.md"): 404,
		filepath.Join(project, "rubrics"):               404,
	} {
		for _, kind := range []string{"preview", "raw"} {
			if w := getEvidence(c, route(kind, ref)); w.Code != want {
				t.Errorf("%s %q = %d %s, want %d", kind, ref, w.Code, w.Body.String(), want)
			}
		}
	}
}
