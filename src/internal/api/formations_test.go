package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestFormationsHandlerListsBoardsThroughSharedStore(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/missions", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Boards []struct {
				ID    string `json:"id"`
				Slug  string `json:"slug"`
				Title string `json:"title"`
				Rev   int    `json:"rev"`
			} `json:"missions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
	}
	if !response.Success {
		t.Fatalf("success = false: %s", rec.Body.String())
	}
	if len(response.Data.Boards) != 1 {
		t.Fatalf("boards len = %d, want 1", len(response.Data.Boards))
	}
	board := response.Data.Boards[0]
	if board.ID != "brd_01J9_sesssearch" || board.Slug != "session-search" || board.Title != "Improve session search" || board.Rev != 7 {
		t.Fatalf("board summary = %+v, want persisted board metadata", board)
	}
}

func TestFormationsHandlerListsRegisteredCodeGateProfiles(t *testing.T) {
	handler := NewFormationsHandlerWithStore(formations.NewStore(t.TempDir()))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/gate-profiles", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Profiles []formations.CodeGateProfileDescriptor `json:"profiles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
	}
	if !response.Success || len(response.Data.Profiles) != 2 {
		t.Fatalf("profile response = %+v, want two registered profiles", response)
	}
	profile := response.Data.Profiles[0]
	if profile.ProfileID != "output_absent" ||
		profile.ProfileVersion != "1" ||
		profile.ExecutionClass != formations.CodeGateExecutionClassInProcess ||
		profile.EffectPolicy != formations.CodeGateEffectPolicyNone ||
		profile.ResultEncoding != formations.CodeGateResultEncoding {
		t.Fatalf("first profile = %+v, want exact safe output_absent@1 descriptor", profile)
	}
}

func TestFormationsHandlerBoardPatchReturnsConflictForStaleETag(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	if _, err := store.UpdateBoardMetadata("session-search", formations.BoardMetadataPatch{
		Title:     stringPtrForFormationsAPI("external edit"),
		UpdatedBy: "agent:external",
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}); err != nil {
		t.Fatalf("external update: %v", err)
	}

	handler := NewFormationsHandlerWithStore(store)
	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"title":"stale edit","expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	req.SetPathValue("mission", "session-search")
	rec := httptest.NewRecorder()

	handler.PatchBoard(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
}

func TestFormationsHandlerMapsInvalidDefinitionSourceBeforeMutation(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	raw := `schema = 1
id = "brd_invalid_source"
slug = "invalid-source"
title = "Invalid source"
rev = 7
updatedAt = "2026-07-21T12:00:00Z"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = 42
`
	writeFormationsAPIFixture(t, store.BoardPath("invalid-source"), raw)

	handler := NewFormationsHandlerWithStore(store)
	req := httptest.NewRequest(http.MethodPatch, "/api/missions/invalid-source", bytes.NewBufferString(`{"title":"must not persist","expectedRev":7}`))
	req.SetPathValue("mission", "invalid-source")
	digest := sha256.Sum256([]byte(raw))
	req.Header.Set("If-Match", hex.EncodeToString(digest[:]))
	rec := httptest.NewRecorder()

	handler.PatchBoard(rec, req)

	assertFormationsAPIToolError(t, rec, http.StatusUnprocessableEntity, "INVALID_DEFINITION_SOURCE")
	if got := readFormationsAPIFile(t, store.BoardPath("invalid-source")); got != raw {
		t.Fatalf("rejected API write changed board bytes:\n got %q\nwant %q", got, raw)
	}
}

func TestFormationsHandlerRequiresPreconditionsForWrites(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	writeFormationsAPIFixture(t, store.LayoutPath("session-search"), `schema = 1
missionId = "brd_01J9_sesssearch"
missionRev = 7
updatedAt = "2026-06-03T16:02:00Z"
`)
	handler := NewFormationsHandlerWithStore(store)

	boardReq := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"title":"blind edit"}`))
	boardReq.SetPathValue("mission", "session-search")
	boardRec := httptest.NewRecorder()
	handler.PatchBoard(boardRec, boardReq)
	if boardRec.Code != http.StatusPreconditionRequired {
		t.Fatalf("board status = %d, want %d: %s", boardRec.Code, http.StatusPreconditionRequired, boardRec.Body.String())
	}

	layoutReq := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search/layout", bytes.NewBufferString(`{}`))
	layoutReq.SetPathValue("mission", "session-search")
	layoutRec := httptest.NewRecorder()
	handler.PatchLayout(layoutRec, layoutReq)
	if layoutRec.Code != http.StatusPreconditionRequired {
		t.Fatalf("layout status = %d, want %d: %s", layoutRec.Code, http.StatusPreconditionRequired, layoutRec.Body.String())
	}
}

func TestFormationsHandlerSurfacesBoardChangedSignal(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "External edit"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	handler := NewFormationsHandlerWithStore(store)
	req := httptest.NewRequest(http.MethodGet, "/api/missions/session-search/changes?etag="+board.ETag, nil)
	req.SetPathValue("mission", "session-search")
	rec := httptest.NewRecorder()

	handler.GetBoardChanges(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Signal struct {
				Changed bool   `json:"changed"`
				Signal  string `json:"signal"`
			} `json:"signal"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
	}
	if !response.Data.Signal.Changed || response.Data.Signal.Signal != "mission.changed" {
		t.Fatalf("signal = %+v, want board.changed changed signal", response.Data.Signal)
	}
}

func TestFormationsHandlerS2CreatesFormationThroughSharedStore(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
customFuture = "keep me"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"createFormation":{"type":"peer","title":"Research huddle","x":840,"y":135},"expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Board struct {
				Rev        int                        `json:"rev"`
				Formations []formations.FormationNode `json:"formations"`
			} `json:"mission"`
			Formation formations.FormationNode `json:"formation"`
			Layout    struct {
				BoardRev int                     `json:"missionRev"`
				Nodes    []formations.LayoutNode `json:"nodes"`
			} `json:"layout"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
	}
	if !response.Success {
		t.Fatalf("success=false: %s", rec.Body.String())
	}
	if response.Data.Board.Rev != 8 {
		t.Fatalf("board rev = %d, want 8", response.Data.Board.Rev)
	}
	if response.Data.Formation.Type != formations.FormationTypePeer || len(response.Data.Formation.Slots) != 2 {
		t.Fatalf("formation = %+v, want peer with two default slots", response.Data.Formation)
	}
	if len(response.Data.Layout.Nodes) != 1 || response.Data.Layout.Nodes[0].X != 840 || response.Data.Layout.Nodes[0].Y != 135 {
		t.Fatalf("layout nodes = %+v, want created x/y", response.Data.Layout.Nodes)
	}
	raw := readFormationsAPIFile(t, store.BoardPath("session-search"))
	if !bytes.Contains([]byte(raw), []byte(`customFuture = "keep me"`)) || bytes.Contains([]byte(raw), []byte("x = 840")) {
		t.Fatalf("board file did not preserve unknown fields or leaked layout:\n%s", raw)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/missions/session-search/formations", bytes.NewBufferString(`{"type":"peer"}`))
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusNotFound {
		t.Fatalf("legacy POST create route status = %d, want %d; body=%s", postRec.Code, http.StatusNotFound, postRec.Body.String())
	}
}

// The offline pick route records who relayed the operator's pick, as the
// coordinator's does (archon-gihe).

func TestFormationsHandlerS3DeletesGateAndMissionThroughBoardPatch(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[inputCard]]
id = "mis_showcase"
title = "Showcase"
goal = "Build"
beadId = "home-7kc4.5"

[[formation]]
id = "fmn_frame"
type = "solo"
title = "Frame"

[[formation.input]]
id = "port_frame_in"
label = "Input"

[[formation.output]]
id = "port_frame_out"
label = "Output"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["code"]
criterion = "Review"

[[connection]]
id = "edge_mission_frame"
from = "mis_showcase:out"
to = "fmn_frame:port_frame_in"

[[connection]]
id = "edge_frame_gate"
from = "fmn_frame:port_frame_out"
to = "gate_review:in"
`)
	writeFormationsAPIFixture(t, store.LayoutPath("session-search"), `schema = 1
missionId = "brd_01J9_sesssearch"
missionRev = 7
updatedAt = "2026-06-03T16:02:00Z"

[[node]]
id = "mis_showcase"
x = 80
y = -120

[[node]]
id = "gate_review"
x = 440
y = 80
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)

	gateReq := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"deleteGate":{"id":"gate_review"},"expectedRev":7,"updatedBy":"agent:test"}`))
	gateReq.Header.Set("If-Match", board.ETag)
	gateReq.SetPathValue("mission", "session-search")
	gateRec := httptest.NewRecorder()
	handler.PatchBoard(gateRec, gateReq)
	if gateRec.Code != http.StatusOK {
		t.Fatalf("delete gate status = %d, want %d: %s", gateRec.Code, http.StatusOK, gateRec.Body.String())
	}
	var gateResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Board  formations.BoardDocument  `json:"mission"`
			Layout formations.LayoutDocument `json:"layout"`
		} `json:"data"`
	}
	if err := json.Unmarshal(gateRec.Body.Bytes(), &gateResponse); err != nil {
		t.Fatalf("decode gate response: %v\n%s", err, gateRec.Body.String())
	}
	if len(gateResponse.Data.Board.Gates) != 0 || len(gateResponse.Data.Board.Connections) != 1 {
		t.Fatalf("delete gate board = %+v, want gate and touched wire removed", gateResponse.Data.Board)
	}

	missionReq := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"deleteInputCard":{"id":"mis_showcase"},"expectedRev":8,"updatedBy":"agent:test"}`))
	missionReq.Header.Set("If-Match", gateRec.Header().Get("ETag"))
	missionReq.SetPathValue("mission", "session-search")
	missionRec := httptest.NewRecorder()
	handler.PatchBoard(missionRec, missionReq)
	if missionRec.Code != http.StatusOK {
		t.Fatalf("delete mission status = %d, want %d: %s", missionRec.Code, http.StatusOK, missionRec.Body.String())
	}
	var missionResponse struct {
		Success bool `json:"success"`
		Data    struct {
			Board  formations.BoardDocument  `json:"mission"`
			Layout formations.LayoutDocument `json:"layout"`
		} `json:"data"`
	}
	if err := json.Unmarshal(missionRec.Body.Bytes(), &missionResponse); err != nil {
		t.Fatalf("decode mission response: %v\n%s", err, missionRec.Body.String())
	}
	if len(missionResponse.Data.Board.Missions) != 0 || len(missionResponse.Data.Board.Connections) != 0 {
		t.Fatalf("delete mission board = %+v, want mission and touched wire removed", missionResponse.Data.Board)
	}
	if len(missionResponse.Data.Layout.Nodes) != 0 {
		t.Fatalf("layout nodes = %+v, want deleted gate/mission layout pruned", missionResponse.Data.Layout.Nodes)
	}
}

func TestFormationsHandlerS2ReadsBoardByIDAndMovesNodeInLayoutOnly(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	before, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	created, err := store.CreateFormation("session-search", formations.FormationCreateRequest{
		Type:      formations.FormationTypeSolo,
		Title:     "Frame the goal",
		X:         120,
		Y:         80,
		UpdatedBy: "agent:test",
	}, formations.WriteOptions{ExpectedETag: before.ETag, ExpectedRev: before.Rev})
	if err != nil {
		t.Fatalf("create fixture formation: %v", err)
	}
	boardBytesBeforeMove := readFormationsAPIFile(t, store.BoardPath("session-search"))

	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/api/missions/"+created.Board.ID, nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET by board id status = %d, want %d: %s", getRec.Code, http.StatusOK, getRec.Body.String())
	}
	if !bytes.Contains(getRec.Body.Bytes(), []byte(`"slug":"session-search"`)) || !bytes.Contains(getRec.Body.Bytes(), []byte(`"title":"Frame the goal"`)) {
		t.Fatalf("GET by id response missing board/formation fields: %s", getRec.Body.String())
	}

	layout, err := store.ReadLayout("session-search")
	if err != nil {
		t.Fatalf("read layout: %v", err)
	}
	patchReq := httptest.NewRequest(http.MethodPatch, "/api/missions/"+created.Board.ID+"/layout", bytes.NewBufferString(`{"nodes":[{"id":"`+created.Formation.ID+`","x":910,"y":220}]}`))
	patchReq.Header.Set("If-Match", layout.ETag)
	patchRec := httptest.NewRecorder()
	mux.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("PATCH layout by board id status = %d, want %d: %s", patchRec.Code, http.StatusOK, patchRec.Body.String())
	}
	if got := readFormationsAPIFile(t, store.BoardPath("session-search")); got != boardBytesBeforeMove {
		t.Fatalf("layout move changed board definition:\n%s", got)
	}
	moved, err := store.ReadLayout("session-search")
	if err != nil {
		t.Fatalf("read moved layout: %v", err)
	}
	if len(moved.Nodes) != 1 || moved.Nodes[0].X != 910 || moved.Nodes[0].Y != 220 {
		t.Fatalf("layout nodes after move = %+v, want moved node", moved.Nodes)
	}
}

func TestFormationsHandlerExplicitArrangeUsesSharedLayoutOperation(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("arrange"), `schema = 1
id = "brd_arrange"
slug = "arrange"
title = "Arrange"
rev = 2

[[inputCard]]
id = "mis_start"
title = "Start"

[[formation]]
id = "fmn_finish"
type = "solo"
title = "Finish"

[[connection]]
id = "edge_start_finish"
from = "mis_start:out"
to = "fmn_finish:in"
`)
	writeFormationsAPIFixture(t, store.LayoutPath("arrange"), `schema = 1
missionId = "brd_arrange"
missionRev = 2

[[node]]
id = "mis_start"
x = 500
y = 500

[[node]]
id = "fmn_finish"
x = 100
y = 100
`)
	layout, err := store.ReadLayout("arrange")
	if err != nil {
		t.Fatalf("read layout: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	missionBefore, err := os.ReadFile(store.BoardPath("arrange"))
	if err != nil {
		t.Fatal(err)
	}
	layoutBefore, err := os.ReadFile(store.LayoutPath("arrange"))
	if err != nil {
		t.Fatal(err)
	}
	// Inspection, change detection and validation are read-only even when a
	// hand-placed graph differs from what explicit Arrange would produce.
	for _, path := range []string{"/api/missions/arrange", "/api/missions/arrange/layout", "/api/missions/arrange/validation", "/api/missions/arrange/changes"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	if got, err := os.ReadFile(store.LayoutPath("arrange")); err != nil || !bytes.Equal(got, layoutBefore) {
		t.Fatalf("inspection changed saved layout: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/missions/arrange/layout", bytes.NewBufferString(`{"arrange":true}`))
	req.Header.Set("If-Match", layout.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	arranged, err := store.ReadLayout("arrange")
	if err != nil {
		t.Fatalf("read arranged layout: %v", err)
	}
	byID := map[string]formations.LayoutNode{}
	for _, node := range arranged.Nodes {
		byID[node.ID] = node
	}
	if byID["mis_start"].X >= byID["fmn_finish"].X {
		t.Fatalf("explicit arrange did not order connected nodes: %+v", byID)
	}
	if got, err := os.ReadFile(store.BoardPath("arrange")); err != nil || !bytes.Equal(got, missionBefore) {
		t.Fatalf("Arrange changed mission definition: %v", err)
	}
	arrangedBytes, err := os.ReadFile(store.LayoutPath("arrange"))
	if err != nil {
		t.Fatal(err)
	}
	stale := httptest.NewRequest(http.MethodPatch, "/api/missions/arrange/layout", bytes.NewBufferString(`{"arrange":true}`))
	stale.Header.Set("If-Match", layout.ETag)
	staleRec := httptest.NewRecorder()
	mux.ServeHTTP(staleRec, stale)
	if staleRec.Code != http.StatusConflict {
		t.Fatalf("stale Arrange: %d %s", staleRec.Code, staleRec.Body.String())
	}
	if got, err := os.ReadFile(store.LayoutPath("arrange")); err != nil || !bytes.Equal(got, arrangedBytes) {
		t.Fatalf("stale Arrange changed layout: %v", err)
	}
}

func TestFormationsHandlerS2RecreatesDeletedLayoutSidecarOnLayoutMove(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[formation]]
id = "fmn_01J9_research"
type = "peer"
title = "Research huddle"
`)
	boardBefore := readFormationsAPIFile(t, store.BoardPath("session-search"))
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search/layout", bytes.NewBufferString(`{"nodes":[{"id":"fmn_01J9_research","x":260,"y":180}]}`))
	req.Header.Set("If-Match", "*")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Layout struct {
				BoardRev int                     `json:"missionRev"`
				Nodes    []formations.LayoutNode `json:"nodes"`
			} `json:"layout"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
	}
	if len(response.Data.Layout.Nodes) != 1 || response.Data.Layout.Nodes[0].X != 260 || response.Data.Layout.Nodes[0].Y != 180 {
		t.Fatalf("layout nodes = %+v, want recreated moved position", response.Data.Layout.Nodes)
	}
	if response.Data.Layout.BoardRev != 7 {
		t.Fatalf("layout missionRev = %d, want 7", response.Data.Layout.BoardRev)
	}
	if got := readFormationsAPIFile(t, store.BoardPath("session-search")); got != boardBefore {
		t.Fatalf("layout move dirtied board definition:\n%s", got)
	}
}

func TestS3InverseMutationsRequireFreshETags(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	before, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	created, err := store.CreateFormation("session-search", formations.FormationCreateRequest{
		Type:      formations.FormationTypeSolo,
		Title:     "Undo me",
		X:         300,
		Y:         180,
		UpdatedBy: "agent:test",
	}, formations.WriteOptions{ExpectedETag: before.ETag, ExpectedRev: before.Rev})
	if err != nil {
		t.Fatalf("create formation: %v", err)
	}

	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	staleReq := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"deleteFormation":{"id":"`+created.Formation.ID+`"},"expectedRev":7,"updatedBy":"agent:test"}`))
	staleReq.Header.Set("If-Match", before.ETag)
	staleRec := httptest.NewRecorder()
	mux.ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusConflict {
		t.Fatalf("stale delete status = %d, want %d: %s", staleRec.Code, http.StatusConflict, staleRec.Body.String())
	}

	freshReq := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"deleteFormation":{"id":"`+created.Formation.ID+`"},"expectedRev":8,"updatedBy":"agent:test"}`))
	freshReq.Header.Set("If-Match", created.Board.ETag)
	freshRec := httptest.NewRecorder()
	mux.ServeHTTP(freshRec, freshReq)
	if freshRec.Code != http.StatusOK {
		t.Fatalf("fresh delete status = %d, want %d: %s", freshRec.Code, http.StatusOK, freshRec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Board struct {
				Rev        int                        `json:"rev"`
				Formations []formations.FormationNode `json:"formations"`
			} `json:"mission"`
			Layout struct {
				Nodes []formations.LayoutNode `json:"nodes"`
			} `json:"layout"`
		} `json:"data"`
	}
	if err := json.Unmarshal(freshRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, freshRec.Body.String())
	}
	if response.Data.Board.Rev != 9 || len(response.Data.Board.Formations) != 0 || len(response.Data.Layout.Nodes) != 0 {
		t.Fatalf("delete response = %+v, want rev 9 with formation/layout node removed", response.Data)
	}
}

func TestS3SlotAssignmentUsesPersonaIDsWithoutSessionNames(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[formation]]
id = "fmn_frame"
type = "peer"
title = "Frame"

[[formation.slot]]
id = "slot_peer_a"
label = "Peer A"
controller = false
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"assignSlot":{"formationId":"fmn_frame","slotId":"slot_peer_a","agentId":"conductor","harness":"openai-codex","effort":"high"},"expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("sessionName")) || bytes.Contains(rec.Body.Bytes(), []byte("sessionStem")) {
		t.Fatalf("assignment response leaked runtime session fields: %s", rec.Body.String())
	}
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Board struct {
				Rev        int                        `json:"rev"`
				Formations []formations.FormationNode `json:"formations"`
			} `json:"mission"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
	}
	slot := response.Data.Board.Formations[0].Slots[0]
	if response.Data.Board.Rev != 8 || slot.AgentID != "conductor" || slot.Harness != "openai-codex" || slot.Effort != "high" {
		t.Fatalf("assignment response = %+v, want rev 8 conductor/openai-codex", response.Data.Board)
	}
}

func formationsAPILegacyInlineVerificationFixture() string {
	return `schema = 1
id = "brd_legacy_inline"
slug = "legacy-inline"
title = "Legacy inline verification"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[inputCard]]
id = "mis_main"
title = "Main"
goal = "Ship it"
beadId = "ctx-ug7.17"

[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"

[[formation.input]]
id = "port_work_in"
label = "Input"

[[formation.output]]
id = "port_work_out"
label = "Output"

[formation.verification]
id = "ver_work"
kinds = ["code"]
criterion = "Tests pass"
onFail = "block"

[[gate]]
id = "gate_migrated"
title = "Migrated check"
kinds = ["code"]
criterion = "Tests pass"
check = "output_contains"
checkVersion = "1"
checkValue = "output from"

[[connection]]
id = "edge_main_work"
from = "mis_main:out"
to = "fmn_work:port_work_in"

[[connection]]
id = "edge_work_gate"
from = "fmn_work:port_work_out"
to = "gate_migrated:in"
`
}

func TestS3WireRejectsSelfDuplicateAndSecondInput(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[formation]]
id = "fmn_frame"
type = "solo"
title = "Frame"

[[formation.output]]
id = "port_frame_out"
label = "Output"

[[formation]]
id = "fmn_ship"
type = "solo"
title = "Ship"

[[formation.input]]
id = "port_ship_in"
label = "Input"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"wireConnection":{"from":"fmn_frame:port_frame_out","to":"fmn_ship:port_ship_in"},"expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("wire status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"from":"fmn_frame:port_frame_out"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"to":"fmn_ship:port_ship_in"`)) {
		t.Fatalf("wire response missing stable endpoints: %s", rec.Body.String())
	}
}

func TestS3RewireRejectsOccupiedTargetWithoutDroppingOriginal(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[formation]]
id = "fmn_frame"
type = "solo"
title = "Frame"

[[formation.output]]
id = "port_frame_out"
label = "Output"

[[formation]]
id = "fmn_research"
type = "solo"
title = "Research"

[[formation.input]]
id = "port_research_in"
label = "Input"

[[formation.output]]
id = "port_research_out"
label = "Output"

[[formation]]
id = "fmn_ship"
type = "solo"
title = "Ship"

[[formation.input]]
id = "port_ship_in"
label = "Input"

[[connection]]
id = "edge_frame_research"
from = "fmn_frame:port_frame_out"
to = "fmn_research:port_research_in"

[[connection]]
id = "edge_research_ship"
from = "fmn_research:port_research_out"
to = "fmn_ship:port_ship_in"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"rewireConnection":{"from":"fmn_frame:port_frame_out","previousTo":"fmn_research:port_research_in","to":"fmn_ship:port_ship_in"},"expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("rewire occupied target status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	after, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read after failed rewire: %v", err)
	}
	if len(after.Connections) != 2 || !hasAPIConnection(after.Connections, "fmn_frame:port_frame_out", "fmn_research:port_research_in") {
		t.Fatalf("failed rewire changed connections: %+v", after.Connections)
	}
}

func TestS3GatePersistsKindsCriterionWithoutVerdictOrOnFail(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"createGate":{"title":"Review gate","kinds":["code","human"],"criterion":"Research is sound.","x":420,"y":280},"expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gate create status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"kinds":["code","human"]`)) ||
		bytes.Contains(rec.Body.Bytes(), []byte(`"commandArgv"`)) ||
		bytes.Contains(rec.Body.Bytes(), []byte(`"commandShell"`)) ||
		bytes.Contains(rec.Body.Bytes(), []byte("verdict")) || bytes.Contains(rec.Body.Bytes(), []byte("onFail")) {
		t.Fatalf("gate response wrong: %s", rec.Body.String())
	}
	var response struct {
		Data struct {
			Board  formations.BoardDocument  `json:"mission"`
			Layout formations.LayoutDocument `json:"layout"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode gate response: %v\n%s", err, rec.Body.String())
	}
	if len(response.Data.Board.Gates) != 1 || len(response.Data.Layout.Nodes) != 1 {
		t.Fatalf("gate create response = %+v, want board and fresh layout", response.Data)
	}
	created := response.Data.Board.Gates[0]
	placed := response.Data.Layout.Nodes[0]
	if placed.ID != created.ID || placed.X != 420 || placed.Y != 280 || response.Data.Layout.ETag == "" {
		t.Fatalf("gate layout = %+v, want fresh created-node position and ETag", response.Data.Layout)
	}
}

func TestCodeGateAuthoringPersistsExactSelectedProfileTuple(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"createGate":{"title":"Lint","kinds":["code"],"criterion":"Lint passes","check":"output_contains","checkVersion":"1","checkValue":"LINT OK","x":420,"y":280},"expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gate create status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Data struct {
			Gate formations.GateNode `json:"gate"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v\n%s", err, rec.Body.String())
	}
	if response.Data.Gate.Check != "output_contains" ||
		response.Data.Gate.CheckVersion != "1" ||
		response.Data.Gate.CheckValue != "LINT OK" {
		t.Fatalf("created Gate = %+v, want exact selected profile tuple and parameter", response.Data.Gate)
	}
	raw := readFormationsAPIFile(t, store.BoardPath("session-search"))
	for _, want := range []string{
		`check = "output_contains"`,
		`checkVersion = "1"`,
		`checkValue = "LINT OK"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("board missing %q:\n%s", want, raw)
		}
	}
}

func TestCodeGateAuthoringRejectsUnknownProfileWithStableErrorBeforeMutation(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	before := readFormationsAPIFile(t, store.BoardPath("session-search"))
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"createGate":{"title":"Lint","kinds":["code"],"check":"unknown","checkVersion":"1","checkValue":"LINT OK"},"expectedRev":7}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity ||
		!bytes.Contains(rec.Body.Bytes(), []byte(`"code":"invalid_code_gate_profile"`)) {
		t.Fatalf("unknown profile response = %d %s, want stable 422", rec.Code, rec.Body.String())
	}
	if after := readFormationsAPIFile(t, store.BoardPath("session-search")); after != before {
		t.Fatalf("unknown profile changed board bytes\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}

func TestS3JudgeLoopAndChainUseConnectionsWithSingleSendAndReturn(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[gate]]
id = "gate_review"
title = "Review"
kinds = ["code"]
criterion = "Check it."

[[formation]]
id = "fmn_j1"
type = "solo"
title = "Judge 1"

[[formation.input]]
id = "port_j1_in"
label = "Input"

[[formation.output]]
id = "port_j1_out"
label = "Output"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"setGateJudge":{"gateId":"gate_review","chain":["fmn_j1"]},"expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("judge status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"from":"gate_review:judge"`)) || !bytes.Contains(rec.Body.Bytes(), []byte(`"to":"gate_review:judge"`)) {
		t.Fatalf("judge response missing send/return: %s", rec.Body.String())
	}
}

func TestS3MissionCreateAcceptsProjectBeadIDAndSingleOut(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	store.Now = fixedFormationsAPIClock()
	writeFormationsAPIFixture(t, store.BoardPath("session-search"), `schema = 1
id = "brd_01J9_sesssearch"
slug = "session-search"
title = "Improve session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"
`)
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatalf("read board: %v", err)
	}
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(`{"createInputCard":{"title":"Showcase","goal":"Build it","beadId":"chlab-123","x":196,"y":308},"expectedRev":7,"updatedBy":"agent:test"}`))
	req.Header.Set("If-Match", board.ETag)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mission status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"beadId"`)) || bytes.Contains(rec.Body.Bytes(), []byte("chain")) {
		t.Fatalf("mission response wrong: %s", rec.Body.String())
	}
	var response struct {
		Data struct {
			Board  formations.BoardDocument  `json:"mission"`
			Layout formations.LayoutDocument `json:"layout"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode mission response: %v\n%s", err, rec.Body.String())
	}
	if len(response.Data.Board.Missions) != 1 || len(response.Data.Layout.Nodes) != 1 {
		t.Fatalf("mission create response = %+v, want board and fresh layout", response.Data)
	}
	created := response.Data.Board.Missions[0]
	placed := response.Data.Layout.Nodes[0]
	if placed.ID != created.ID || placed.X != 196 || placed.Y != 308 || response.Data.Layout.ETag == "" {
		t.Fatalf("mission layout = %+v, want fresh created-node position and ETag", response.Data.Layout)
	}
}

func TestFormationsHandlerRejectsOversizedJSONBody(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	handler := NewFormationsHandlerWithStore(store)
	body := `{"title":"` + strings.Repeat("x", 2*1024*1024) + `"}`

	req := httptest.NewRequest(http.MethodPatch, "/api/missions/session-search", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	handler.PatchBoard(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413: %s", rec.Code, rec.Body.String())
	}
}

func writeFormationsAPIFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func readFormationsAPIFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func stringPtrForFormationsAPI(v string) *string {
	return &v
}

func hasAPIConnection(connections []formations.BoardConnection, from, to string) bool {
	for _, connection := range connections {
		if connection.From == from && connection.To == to {
			return true
		}
	}
	return false
}

func fixedFormationsAPIClock() func() time.Time {
	return func() time.Time {
		return time.Date(2026, 6, 3, 17, 0, 0, 0, time.UTC)
	}
}
