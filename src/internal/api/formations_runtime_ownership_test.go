package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Authoring can be mounted without a coordinator. It must not acquire an
// alternate execution path when runtime requests reach that mount.
func TestAuthoringHandlerDoesNotMountRuntimeRoutes(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	mux := http.NewServeMux()
	NewFormationsHandlerWithStore(store).RegisterRoutes(mux)
	for _, route := range []struct{ method, path string }{
		{"POST", "/api/runs"}, {"GET", "/api/runs/run_missing"},
		{"GET", "/api/runs/run_missing/events"}, {"GET", "/api/runs/run_missing/stream"},
		{"POST", "/api/runs/run_missing/resume"}, {"POST", "/api/runs/run_missing/abort"},
		{"POST", "/api/runs/run_missing/gates/gate_missing/verdict"},
		{"POST", "/api/runs/run_missing/gates/gate_missing/pick"},
		{"GET", "/api/runs/run_missing/escalations"},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("runtime route mounted: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
