package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func TestFormationsHandlerReadsAndWritesBoardNotesWithETagFences(t *testing.T) {
	workspace := t.TempDir()
	store := formations.NewStore(workspace)
	writeBoardNotesAPIFixture(t, store.BoardPath("session-search"))
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/formations/boards/session-search/notes", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET notes status = %d, body=%s", get.Code, get.Body.String())
	}
	var empty struct {
		Data struct {
			Notes formations.BoardNotesDocument `json:"notes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &empty); err != nil {
		t.Fatalf("decode empty notes: %v", err)
	}
	if empty.Data.Notes.ETag != "*" || len(empty.Data.Notes.Board) != 0 {
		t.Fatalf("empty notes = %+v", empty.Data.Notes)
	}

	missingFence := httptest.NewRecorder()
	mux.ServeHTTP(missingFence, httptest.NewRequest(http.MethodPatch, "/api/formations/boards/session-search/notes", bytes.NewBufferString(`{"target":"board","text":"shared"}`)))
	if missingFence.Code != http.StatusPreconditionRequired {
		t.Fatalf("PATCH without If-Match status = %d, body=%s", missingFence.Code, missingFence.Body.String())
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/session-search/notes", bytes.NewBufferString(`{"target":"board","text":"shared\nplan","updatedBy":"human:operator"}`))
	patch.Header.Set("If-Match", "*")
	patched := httptest.NewRecorder()
	mux.ServeHTTP(patched, patch)
	if patched.Code != http.StatusOK {
		t.Fatalf("PATCH notes status = %d, body=%s", patched.Code, patched.Body.String())
	}
	var current struct {
		Data struct {
			Notes formations.BoardNotesDocument `json:"notes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(patched.Body.Bytes(), &current); err != nil {
		t.Fatalf("decode patched notes: %v", err)
	}
	if len(current.Data.Notes.Board) != 1 || current.Data.Notes.Board[0].Text != "shared\nplan" || current.Data.Notes.Board[0].Author != "human:operator" || current.Data.Notes.ETag == "*" || current.Data.Notes.UpdatedBy != "human:operator" {
		t.Fatalf("patched notes = %+v", current.Data.Notes)
	}

	stale := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/session-search/notes", bytes.NewBufferString(`{"target":"board","text":"stale","author":"human:operator"}`))
	stale.Header.Set("If-Match", "*")
	staleResponse := httptest.NewRecorder()
	mux.ServeHTTP(staleResponse, stale)
	if staleResponse.Code != http.StatusConflict {
		t.Fatalf("stale PATCH status = %d, body=%s", staleResponse.Code, staleResponse.Body.String())
	}

	element := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/session-search/notes", bytes.NewBufferString(`{"target":"fmn_frame","text":"keep this narrow","author":"agent:archon"}`))
	element.Header.Set("If-Match", current.Data.Notes.ETag)
	elementResponse := httptest.NewRecorder()
	mux.ServeHTTP(elementResponse, element)
	if elementResponse.Code != http.StatusOK {
		t.Fatalf("element PATCH status = %d, body=%s", elementResponse.Code, elementResponse.Body.String())
	}
	var withReply struct {
		Data struct {
			Notes formations.BoardNotesDocument `json:"notes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(elementResponse.Body.Bytes(), &withReply); err != nil || len(withReply.Data.Notes.Elements) != 1 {
		t.Fatalf("element notes = %s (%v)", elementResponse.Body.String(), err)
	}
	agentEntry := withReply.Data.Notes.Elements[0].Entries[0].ID
	for body, want := range map[string]int{
		`{"target":"fmn_frame","action":"edit","entryId":"` + agentEntry + `","text":"overwrite","author":"human:operator"}`: http.StatusForbidden,
		`{"target":"fmn_frame","action":"delete","entryId":"nte_missing","author":"agent:archon"}`:                           http.StatusNotFound,
		`{"target":"fmn_frame","text":"no author"}`:                                                                          http.StatusBadRequest,
	} {
		request := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/session-search/notes", bytes.NewBufferString(body))
		request.Header.Set("If-Match", withReply.Data.Notes.ETag)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != want {
			t.Errorf("%s = %d %s, want %d", body, response.Code, response.Body.String(), want)
		}
	}
	edit := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/session-search/notes", bytes.NewBufferString(`{"target":"fmn_frame","action":"edit","entryId":"`+agentEntry+`","text":"keep it narrower","author":"agent:archon"}`))
	edit.Header.Set("If-Match", withReply.Data.Notes.ETag)
	edited := httptest.NewRecorder()
	mux.ServeHTTP(edited, edit)
	if edited.Code != http.StatusOK {
		t.Fatalf("own edit = %d %s", edited.Code, edited.Body.String())
	}
}

func TestFormationsHandlerRejectsUnknownBoardNoteTarget(t *testing.T) {
	store := formations.NewStore(t.TempDir())
	writeBoardNotesAPIFixture(t, store.BoardPath("session-search"))
	handler := NewFormationsHandlerWithStore(store)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodPatch, "/api/formations/boards/session-search/notes", bytes.NewBufferString(`{"target":"fmn_missing","text":"nope","author":"human:ui"}`))
	request.Header.Set("If-Match", "*")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown note target status = %d, body=%s", response.Code, response.Body.String())
	}
}

func writeBoardNotesAPIFixture(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o770); err != nil {
		t.Fatalf("mkdir board fixture: %v", err)
	}
	const fixture = `schema = 1
id = "brd_notes"
slug = "session-search"
title = "Session search"
rev = 7
updatedAt = "2026-06-03T16:00:00Z"

[[formation]]
id = "fmn_frame"
type = "solo"
title = "Frame"

[[formation.slot]]
id = "slot_frame"
label = "Builder"
`
	if err := os.WriteFile(path, []byte(fixture), 0o660); err != nil {
		t.Fatalf("write board fixture: %v", err)
	}
}
