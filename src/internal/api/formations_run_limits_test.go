package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// A negative limit is refused on the local API path with admission's message
// (form-o7p.7), for a mission and for a single formation, and writes no run.
func TestFormationsHandlerRefusesNegativeRunLimits(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	personas := formations.NewPersonaStore(t.TempDir())
	if _, err := personas.CreatePersona(formations.CreatePersonaRequest{ID: "scout", Kind: "specialist", Harness: "openai-codex"}); err != nil {
		t.Fatalf("create persona: %v", err)
	}
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), formationsAPIS5CascadeBoardFixture())
	handler := NewFormationsHandlerWithStores(store, personas)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	for _, body := range []string{
		`{"board":"session-search","missionId":"mis_showcase","limits":{"maxAttempts":-1}}`,
		`{"board":"session-search","missionId":"mis_showcase","limits":{"wallClockSeconds":-1}}`,
		`{"board":"session-search","formationId":"fmn_work","limits":{"maxDispatch":-1}}`,
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/formations/runs", bytes.NewBufferString(body)))
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte("INVALID_RUN_LIMITS")) || !bytes.Contains(rec.Body.Bytes(), []byte(formations.ErrInvalidRunLimits.Error())) {
			t.Fatalf("%s: status %d body %s, want 400 INVALID_RUN_LIMITS", body, rec.Code, rec.Body.String())
		}
	}
	if entries, err := os.ReadDir(filepath.Join(store.Workspace, ".formations", "runs", "session-search")); err == nil && len(entries) != 0 {
		t.Fatalf("refused starts wrote run artifacts: %v", entries)
	}
}
