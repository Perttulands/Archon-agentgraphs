package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestFormationsAPISetFormationTypeWithSlotChoiceAndRestore(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("types"), `schema = 1
id = "brd_types"
slug = "types"
title = "Types"
rev = 2

[[formation]]
id = "fmn_work"
type = "peer"
title = "Huddle"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.slot]]
id = "slot_a"
label = "Peer A"
agentId = "codex-builder"
[[formation.slot]]
id = "slot_b"
label = "Peer B"
agentId = "codex-reviewer"

[[mission]]
id = "mis_start"
title = "Start"
goal = ""
beadId = ""

[[connection]]
id = "edge_start"
from = "mis_start:out"
to = "fmn_work:port_in"
`)
	mux := http.NewServeMux()
	NewFormationsHandlerWithStores(store, formations.NewPersonaStore(filepath.Join(t.TempDir(), "agents"))).RegisterRoutes(mux)
	patch := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		board, err := store.ReadBoard("types")
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/types", bytes.NewBufferString(`{"expectedRev":`+jsonInt(board.Rev)+`,`+strings.TrimPrefix(body, "{")))
		req.Header.Set("If-Match", board.ETag)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}
	formation := func(rec *httptest.ResponseRecorder) formations.FormationNode {
		t.Helper()
		var response struct {
			Data struct {
				Board formations.BoardDocument `json:"board"`
			} `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &response) != nil {
			t.Fatalf("setFormationType = %d %s", rec.Code, rec.Body.String())
		}
		if len(response.Data.Board.Connections) != 1 {
			t.Fatalf("connections changed: %+v", response.Data.Board.Connections)
		}
		return response.Data.Board.Formations[0]
	}

	before := readFormationsAPIFile(t, store.BoardPath("types"))
	for body, want := range map[string]string{
		`{"setFormationType":{"id":"fmn_work","type":"solo"}}`: `"code":"SLOT_CHOICE_REQUIRED"`,
		`{"setFormationType":{"id":"fmn_work","type":"flow"}}`: `"code":"INVALID_TYPE_CHANGE"`,
	} {
		if rec := patch(body); rec.Code < 400 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s = %d %s, want %s", body, rec.Code, rec.Body.String(), want)
		}
	}
	if after := readFormationsAPIFile(t, store.BoardPath("types")); after != before {
		t.Fatalf("rejected type changes wrote the board:\n%s", after)
	}

	original := formation(patch(`{"setFormationType":{"id":"fmn_work","type":"orchestrated"}}`))
	if original.Type != "orchestrated" || !original.Slots[0].Controller || original.Slots[1].Controller {
		t.Fatalf("peer->orchestrated = %+v", original)
	}
	solo := formation(patch(`{"setFormationType":{"id":"fmn_work","type":"solo","keepSlotId":"slot_b"}}`))
	if solo.Type != "solo" || len(solo.Slots) != 1 || solo.Slots[0].AgentID != "codex-reviewer" {
		t.Fatalf("orchestrated->solo keeping slot_b = %+v", solo)
	}
	slots, _ := json.Marshal(original.Slots)
	restored := formation(patch(`{"setFormationType":{"id":"fmn_work","type":"orchestrated","slots":` + string(slots) + `}}`))
	if restored.Type != "orchestrated" || len(restored.Slots) != 2 || restored.Slots[0].ID != "slot_a" || restored.Slots[0].AgentID != "codex-builder" || !restored.Slots[0].Controller {
		t.Fatalf("restored = %+v, want the original orchestrated slots", restored)
	}
}
