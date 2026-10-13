package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// CharactersHandler serves the saved characters a slot can be stamped with.
type CharactersHandler struct {
	store *formations.CharacterStore
}

func NewCharactersHandler(store *formations.CharacterStore) *CharactersHandler {
	return &CharactersHandler{store: store}
}

func (h *CharactersHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/characters", h.List)
	mux.HandleFunc("POST /api/characters", h.Create)
	mux.HandleFunc("PUT /api/characters/{characterId}", h.Replace)
	mux.HandleFunc("DELETE /api/characters/{characterId}", h.Delete)
}

func (h *CharactersHandler) List(w http.ResponseWriter, _ *http.Request) {
	characters, err := h.store.List()
	if err != nil {
		writeCharacterError(w, err)
		return
	}
	core.WriteSuccess(w, map[string]interface{}{"characters": characters})
}

// Create saves a new character; its id comes from the request or its name.
func (h *CharactersHandler) Create(w http.ResponseWriter, r *http.Request) {
	var character formations.Character
	if !decodeStrictJSONBody(w, r, &character, "INVALID_CHARACTER", unknownCharacterField) {
		return
	}
	saved, err := h.store.Create(character)
	if err != nil {
		writeCharacterError(w, err)
		return
	}
	core.WriteJSON(w, http.StatusCreated, core.NewSuccessResponse(saved))
}

// Replace states a character in full. Missions it stamped keep their staffing.
func (h *CharactersHandler) Replace(w http.ResponseWriter, r *http.Request) {
	var character formations.Character
	if !decodeStrictJSONBody(w, r, &character, "INVALID_CHARACTER", unknownCharacterField) {
		return
	}
	id := r.PathValue("characterId")
	if character.ID != "" && character.ID != id {
		core.WriteError(w, http.StatusUnprocessableEntity, "INVALID_CHARACTER", fmt.Sprintf("the request names character %q but the path names %q", character.ID, id))
		return
	}
	character.ID = id
	saved, err := h.store.Replace(character)
	if err != nil {
		writeCharacterError(w, err)
		return
	}
	core.WriteSuccess(w, saved)
}

func (h *CharactersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("characterId")
	if err := h.store.Delete(id); err != nil {
		writeCharacterError(w, err)
		return
	}
	core.WriteSuccess(w, map[string]interface{}{"deleted": id})
}

func unknownCharacterField(field string) string {
	return fmt.Sprintf("character request field %q is not one Archon takes", field)
}

func writeCharacterError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, formations.ErrAlreadyExists):
		core.WriteError(w, http.StatusConflict, "CHARACTER_EXISTS", strings.TrimPrefix(err.Error(), formations.ErrAlreadyExists.Error()+": "))
	case errors.Is(err, formations.ErrNotFound):
		core.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Character not found")
	case errors.Is(err, formations.ErrInvalidCharacter):
		core.WriteError(w, http.StatusUnprocessableEntity, "INVALID_CHARACTER", fieldErrorMessage(err, formations.ErrInvalidCharacter))
	case errors.Is(err, formations.ErrUnsupportedSchema):
		core.WriteError(w, http.StatusUnprocessableEntity, "UNSUPPORTED_SCHEMA", err.Error())
	default:
		core.WriteError(w, http.StatusInternalServerError, "CHARACTERS_ERROR", err.Error())
	}
}
