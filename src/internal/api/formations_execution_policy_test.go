package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestFormationExecutionPolicyHTTP(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("budget"), "schema = 1\nid = \"brd_budget\"\nslug = \"budget\"\ntitle = \"Budget\"\nrev = 1\n[[formation]]\nid = \"fmn_work\"\ntype = \"solo\"\ntitle = \"Work\"\n")
	mux := http.NewServeMux()
	NewFormationsHandlerWithStore(store).RegisterRoutes(mux)
	for _, seconds := range []int{19, 3600, 0} {
		board, err := store.ReadBoard("budget")
		if err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf(`{"setExecution":{"formationId":"fmn_work","timeoutSeconds":%d},"expectedRev":%d}`, seconds, board.Rev)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/budget", strings.NewReader(body))
		req.Header.Set("If-Match", board.ETag)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		board, err = store.ReadBoard("budget")
		if err != nil {
			t.Fatal(err)
		}
		policy := board.Formations[0].Execution
		if seconds == 0 && policy != nil || seconds > 0 && (policy == nil || policy.TimeoutSeconds != seconds) {
			t.Fatalf("policy: %+v", policy)
		}
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/api/formations/boards/budget", strings.NewReader(`{"setExecution":{"formationId":"fmn_work","timeoutSeconds":-3}}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid policy status %d: %s", rec.Code, rec.Body.String())
	}
}
