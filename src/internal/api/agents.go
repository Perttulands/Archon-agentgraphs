package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/core"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

type AgentLivenessProvider interface {
	LiveAgentSessions() ([]formations.LiveAgentSession, error)
}

type AgentsHandler struct {
	store    *formations.PersonaStore
	liveness AgentLivenessProvider
	// missions answers which slots use a role, for usage and delete.
	missions *formations.Store
}

func NewAgentsHandler(agentsDir string, liveness AgentLivenessProvider) *AgentsHandler {
	return &AgentsHandler{
		store:    formations.NewPersonaStore(agentsDir),
		liveness: liveness,
	}
}

func NewAgentsHandlerWithStore(store *formations.PersonaStore) *AgentsHandler {
	return &AgentsHandler{store: store}
}

// NewAgentsHandlerWithStoreAndLiveness serves a persona store whose roster
// liveness comes from provider; a nil provider reports every agent offline.
func NewAgentsHandlerWithStoreAndLiveness(store *formations.PersonaStore, liveness AgentLivenessProvider) *AgentsHandler {
	return &AgentsHandler{store: store, liveness: liveness}
}

// UseMissions lets the handler say which slots use a role, and refuse to
// delete one that a slot still names.
func (h *AgentsHandler) UseMissions(missions *formations.Store) *AgentsHandler {
	h.missions = missions
	return h
}

func (h *AgentsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents", h.ListAgents)
	mux.HandleFunc("POST /api/agents", h.CreateAgent)
	mux.HandleFunc("GET /api/agents/{agentId}", h.GetAgent)
	mux.HandleFunc("PATCH /api/agents/{agentId}", h.UpdateAgent)
	mux.HandleFunc("DELETE /api/agents/{agentId}", h.DeleteAgent)
	mux.HandleFunc("GET /api/agents/{agentId}/usage", h.AgentUsage)
}

// AgentUsage lists every slot, in every mission, that names the role.
func (h *AgentsHandler) AgentUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("agentId")
	if _, err := h.store.ReadPersona(id); err != nil {
		writeAgentError(w, err)
		return
	}
	uses, err := h.usage(id)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	core.WriteSuccess(w, map[string]interface{}{"usage": uses})
}

// DeleteAgent removes a role's card once no slot names it. Deleting a card
// that overrides a built-in role brings the built-in role back.
func (h *AgentsHandler) DeleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("agentId")
	uses, err := h.usage(id)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	if len(uses) > 0 {
		writeAgentError(w, formations.RoleInUseError(id, uses))
		return
	}
	builtin, err := h.store.DeletePersona(id, r.Header.Get("If-Match"))
	if err != nil {
		writeAgentError(w, err)
		return
	}
	core.WriteSuccess(w, map[string]interface{}{"deleted": id, "builtinRemains": builtin})
}

func (h *AgentsHandler) usage(id string) ([]formations.RoleUse, error) {
	if h.missions == nil {
		return nil, errors.New("this agents handler has no missions to read role usage from")
	}
	return h.missions.RoleUsage(id)
}

func (h *AgentsHandler) ListAgents(w http.ResponseWriter, r *http.Request) {
	cards, unreadable, err := h.store.ListPersonasSkipping()
	if err != nil {
		writeAgentError(w, err)
		return
	}
	live, err := h.liveSessions()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, "AGENTS_LIVENESS_ERROR", err.Error())
		return
	}
	roster, err := formations.ProjectAgentRoster(cards, live, formations.AgentRosterFilter{
		Capable:        r.URL.Query().Get("capable"),
		AssignableOnly: truthy(r.URL.Query().Get("assignable")),
	})
	if err != nil {
		writeAgentError(w, err)
		return
	}
	data := map[string]interface{}{
		"agents":    roster.Agents,
		"count":     len(roster.Agents),
		"harnesses": formations.LaunchableHarnesses(),
		// The effort policy guides the effort a slot is staffed with.
		"effortPolicy": formations.EffortPolicy(),
	}
	if len(unreadable) > 0 {
		// Cards that cannot be read are named; the rest of the roster stands.
		data["unreadable"] = unreadable
	}
	core.WriteSuccess(w, data)
}

func (h *AgentsHandler) GetAgent(w http.ResponseWriter, r *http.Request) {
	card, err := h.store.ReadPersona(r.PathValue("agentId"))
	if err != nil {
		writeAgentError(w, err)
		return
	}
	card.TOML = ""
	w.Header().Set("ETag", card.ETag)
	core.WriteSuccess(w, card)
}

func (h *AgentsHandler) CreateAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID           string   `json:"id"`
		DisplayName  string   `json:"displayName"`
		Kind         string   `json:"kind"`
		Summary      string   `json:"summary"`
		Capabilities []string `json:"capabilities"`
		Personality  string   `json:"personality"`
	}
	if !decodeStrictJSONBody(w, r, &req, "INVALID_AGENT_CARD", unknownAgentField) {
		return
	}
	card, err := h.store.CreatePersona(formations.CreatePersonaRequest{
		ID:           req.ID,
		DisplayName:  req.DisplayName,
		Kind:         req.Kind,
		Summary:      req.Summary,
		Capabilities: req.Capabilities,
		Personality:  req.Personality,
	})
	if err != nil {
		writeAgentError(w, err)
		return
	}
	card.TOML = ""
	w.Header().Set("ETag", card.ETag)
	core.WriteJSON(w, http.StatusCreated, core.NewSuccessResponse(card))
}

func (h *AgentsHandler) UpdateAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AddCapability    string    `json:"addCapability"`
		RemoveCapability string    `json:"removeCapability"`
		Note             string    `json:"note"`
		UpdatedBy        string    `json:"updatedBy"`
		Retire           *bool     `json:"retire"`
		DisplayName      *string   `json:"displayName"`
		Kind             *string   `json:"kind"`
		Summary          *string   `json:"summary"`
		Capabilities     *[]string `json:"capabilities"`
	}
	if !decodeStrictJSONBody(w, r, &req, "INVALID_AGENT_CARD", unknownAgentField) {
		return
	}
	edit := formations.EditPersonaRequest{
		AddCapability:    req.AddCapability,
		RemoveCapability: req.RemoveCapability,
		Note:             req.Note,
		NoteBy:           req.UpdatedBy,
		SetRetired:       req.Retire,
		ExpectedETag:     r.Header.Get("If-Match"),
		SetDisplayName:   req.DisplayName,
		SetKind:          req.Kind,
		SetSummary:       req.Summary,
		SetCapabilities:  req.Capabilities,
	}
	card, err := h.store.EditPersona(r.PathValue("agentId"), edit)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	card.TOML = ""
	w.Header().Set("ETag", card.ETag)
	core.WriteSuccess(w, card)
}

func (h *AgentsHandler) liveSessions() ([]formations.LiveAgentSession, error) {
	if h.liveness == nil {
		return nil, nil
	}
	return h.liveness.LiveAgentSessions()
}

// unknownAgentField says why a role request cannot take a field: a role is
// role text, so a harness, model or effort belongs on each slot.
func unknownAgentField(field string) string {
	if field == "harness" || field == "model" || field == "effort" {
		return fmt.Sprintf("agent request field %q is not one a role takes: a role carries no harness, model or effort; state them on each slot (formation assign --harness --model --effort)", field)
	}
	return fmt.Sprintf("agent request field %q is not one Archon takes", field)
}

func writeAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, formations.ErrConflict):
		core.WriteError(w, http.StatusConflict, "CONFLICT", "Agent card changed; reload and retry")
	case errors.Is(err, formations.ErrPreconditionRequired):
		core.WriteError(w, http.StatusPreconditionRequired, "PRECONDITION_REQUIRED", "If-Match precondition is required")
	case errors.Is(err, formations.ErrAlreadyExists):
		core.WriteError(w, http.StatusConflict, "AGENT_EXISTS", "Agent id already exists")
	case errors.Is(err, formations.ErrRoleInUse):
		core.WriteError(w, http.StatusConflict, "ROLE_IN_USE", strings.TrimPrefix(err.Error(), formations.ErrRoleInUse.Error()+": "))
	case errors.Is(err, formations.ErrBuiltinRole):
		core.WriteError(w, http.StatusConflict, "BUILTIN_ROLE", strings.TrimPrefix(err.Error(), formations.ErrBuiltinRole.Error()+": "))
	case errors.Is(err, formations.ErrNotFound):
		core.WriteError(w, http.StatusNotFound, "NOT_FOUND", "Agent not found")
	case errors.Is(err, formations.ErrInvalidAgentCard):
		core.WriteError(w, http.StatusUnprocessableEntity, "INVALID_AGENT_CARD", fieldErrorMessage(err, formations.ErrInvalidAgentCard))
	case errors.Is(err, formations.ErrInvalidSlug):
		core.WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case errors.Is(err, formations.ErrUnsupportedSchema):
		core.WriteError(w, http.StatusUnprocessableEntity, "UNSUPPORTED_SCHEMA", err.Error())
	default:
		core.WriteError(w, http.StatusInternalServerError, "AGENTS_ERROR", err.Error())
	}
}

func truthy(raw string) bool {
	switch raw {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
