package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// The assignSlot patch states a slot's harness, model and effort; naming only
// a role (the cockpit's role drag) writes that role's current settings down.
func TestAssignSlotPatchCarriesTheSlotsSettings(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	personas := formations.NewPersonaStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("staff"), `schema = 1
id = "brd_staff"
slug = "staff"
title = "Staff"
rev = 1

[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.slot]]
id = "slot_a"
label = "A"
controller = false
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, personas).RegisterRoutes(mux)
	patch := func(assign string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("staff")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/staff", bytes.NewBufferString(`{"assignSlot":`+assign+`,"expectedRev":`+itoa(board.Rev)+`}`))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	slot := func(rec *httptest.ResponseRecorder) formations.FormationSlot {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
		}
		var response struct {
			Data struct {
				Board formations.BoardDocument `json:"board"`
			} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Data.Board.Formations[0].Slots[0]
	}

	if got := slot(patch(`{"formationId":"fmn_work","slotId":"slot_a","harness":"claude-code","model":"opus","effort":"low"}`)); got.AgentID != "" || got.Harness != "claude-code" || got.Model != "opus" || got.Effort != "low" {
		t.Fatalf("vanilla slot = %+v", got)
	}
	rec := patch(`{"formationId":"fmn_work","slotId":"slot_a","harness":"claude-code","model":"opus"}`)
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"code":"INVALID_SLOT_SETTINGS"`) || !strings.Contains(rec.Body.String(), "needs an effort; the policy is low for errands") {
		t.Fatalf("missing effort = %d %s", rec.Code, rec.Body.String())
	}
	if got := slot(patch(`{"formationId":"fmn_work","slotId":"slot_a","agentId":"delivery-lead","harness":"claude-code"}`)); got.AgentID != "delivery-lead" || got.Harness != "claude-code" || got.Model != "" || got.Effort != "medium" {
		t.Fatalf("role drag slot = %+v, want the role's current settings written down", got)
	}
	if got := slot(patch(`{"formationId":"fmn_work","slotId":"slot_a","agentId":"","harness":""}`)); got.Staffed() {
		t.Fatalf("unassigned slot = %+v", got)
	}
}

func itoa(value int) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
