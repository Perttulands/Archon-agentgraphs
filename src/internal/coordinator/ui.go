package coordinator

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// WithUI serves a configured build and falls back to its index for SPA routes.
// API misses retain HTTP 404 rather than returning the cockpit document.
func WithUI(api http.Handler, dir string) (http.Handler, error) {
	if dir == "" {
		return api, nil
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	index := filepath.Join(root, "index.html")
	if info, err := os.Stat(index); err != nil || info.IsDir() {
		return nil, fmt.Errorf("UI directory must contain index.html")
	}
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", 405)
			return
		}
		path := filepath.Join(root, filepath.FromSlash(filepath.Clean("/"+r.URL.Path)))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, index)
	}), nil
}
