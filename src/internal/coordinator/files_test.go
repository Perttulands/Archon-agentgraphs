package coordinator

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestFileRoutesServeReferencesUnderConfiguredRoots(t *testing.T) {
	c, _, _ := fixture(t)
	base := t.TempDir()
	root := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	for path, content := range map[string]string{
		filepath.Join(root, "rubrics", "quality.md"): "# Quality\n\ntoken: abc123\n",
		filepath.Join(outside, "secret.md"):          "outside\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, "rubrics", "link.md")); err != nil {
		t.Fatal(err)
	}
	route := func(kind, ref string) string {
		return "/api/formations/files/" + kind + "?path=" + url.QueryEscape(ref)
	}

	if w := getEvidence(c, route("preview", filepath.Join(root, "rubrics", "quality.md"))); w.Code != 403 {
		t.Fatalf("preview before any root = %d %s, want 403", w.Code, w.Body.String())
	}
	if err := c.ConfigureFileRoots([]string{root}); err != nil {
		t.Fatal(err)
	}
	w := getEvidence(c, route("preview", "rubrics/quality.md"))
	preview := decodeEvidence[formations.ReferencedFilePreview](t, w, "file")
	if preview.Path != filepath.Join(root, "rubrics", "quality.md") || preview.Kind != "markdown" || preview.Text == nil || !strings.Contains(preview.Text.Text, "# Quality") || strings.Contains(preview.Text.Text, "abc123") {
		t.Fatalf("preview = %s", w.Body.String())
	}

	raw := getEvidence(c, route("raw", filepath.Join(root, "rubrics", "quality.md")))
	if raw.Code != 200 || raw.Header().Get("Content-Type") != "text/plain; charset=utf-8" || raw.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!strings.HasPrefix(raw.Header().Get("Content-Security-Policy"), "sandbox") || raw.Header().Get("Cache-Control") != "no-store" || strings.Contains(raw.Body.String(), "abc123") {
		t.Fatalf("raw = %d %v %s", raw.Code, raw.Header(), raw.Body.String())
	}

	for ref, want := range map[string]int{
		filepath.Join(outside, "secret.md"):          403,
		filepath.Join(root, "rubrics", "link.md"):    404,
		root + "/../outside/secret.md":               404,
		"../outside/secret.md":                       404,
		filepath.Join(root, "rubrics", "missing.md"): 404,
	} {
		for _, kind := range []string{"preview", "raw"} {
			if w := getEvidence(c, route(kind, ref)); w.Code != want || strings.Contains(w.Body.String(), "outside\n") {
				t.Errorf("%s %q = %d %s, want %d", kind, ref, w.Code, w.Body.String(), want)
			}
		}
	}
	if err := c.ConfigureFileRoots([]string{"relative"}); err == nil {
		t.Fatal("a relative file root was accepted")
	}
}
