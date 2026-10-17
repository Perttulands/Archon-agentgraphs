package coordinator

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestMediaRawRoutesStreamRanges(t *testing.T) {
	c, executor, root := fixture(t)
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	dir := filepath.Join(root, ".archon", "artifacts", id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	size := int64(formations.EvidenceArtifactRawMaxBytes + 1024)
	for ext, contentType := range map[string]string{
		"mp4": "video/mp4", "webm": "video/webm",
		"glb": "model/gltf-binary", "gltf": "model/gltf+json", "bin": "application/octet-stream",
	} {
		t.Run(ext, func(t *testing.T) {
			name := "asset." + ext
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("0123456789"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(path, size); err != nil {
				t.Fatal(err)
			}
			for _, route := range []string{"/api/files/raw?path=" + url.QueryEscape(path), "/api/runs/" + id + "/artifacts/" + name} {
				for _, span := range []string{"bytes=2-5", "bytes=-4", fmt.Sprintf("bytes=%d-", size)} {
					r := httptest.NewRequest("GET", route, nil)
					r.Header.Set("Range", span)
					w := httptest.NewRecorder()
					c.Handler().ServeHTTP(w, r)
					if strings.HasPrefix(span, fmt.Sprintf("bytes=%d-", size)) {
						if w.Code != 416 {
							t.Fatalf("unsatisfiable range: %d", w.Code)
						}
						continue
					}
					if w.Code != 206 || w.Header().Get("Content-Type") != contentType || w.Body.Len() != 4 {
						t.Fatalf("%s %s: %d %v (%d bytes)", route, span, w.Code, w.Header(), w.Body.Len())
					}
					if span == "bytes=2-5" && (w.Body.String() != "2345" || w.Header().Get("Content-Range") != fmt.Sprintf("bytes 2-5/%d", size)) {
						t.Fatal(w.Header(), w.Body.String())
					}
					if ext != "bin" && !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline;") {
						t.Fatal(w.Header())
					}
					if ext == "mp4" || ext == "webm" {
						if !strings.Contains(w.Header().Get("Content-Security-Policy"), "media-src 'self'") {
							t.Fatal(w.Header())
						}
					}
				}
			}
			if ext != "bin" {
				preview := decodeEvidence[formations.ReferencedFilePreview](t, getEvidence(c, "/api/files/preview?path="+url.QueryEscape(path)), "file")
				want := "model"
				if ext == "mp4" || ext == "webm" {
					want = "video"
				}
				if preview.Kind != want || preview.Text != nil {
					t.Fatalf("preview: %+v", preview)
				}
			}
		})
	}
}
