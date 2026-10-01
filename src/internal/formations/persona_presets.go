package formations

import "sort"

type personaPreset struct {
	ID           string
	DisplayName  string
	Kind         string
	Summary      string
	Capabilities []string
}

// personaPresetCatalog holds the generic roles every Archon has. A role is
// role text only; the slot that uses it states its harness, model and effort.
var personaPresetCatalog = []personaPreset{
	{ID: "scout", DisplayName: "Scout", Kind: "scout", Summary: "Explores a codebase read-only before changes begin: maps files, callers, tests, risks and unknowns, and reports the evidence.", Capabilities: []string{"research", "inspect", "read-only"}},
	{ID: "planner", DisplayName: "Planner", Kind: "planner", Summary: "Turns grounded requirements into an ordered implementation plan with explicit verification.", Capabilities: []string{"planning", "design"}},
	{ID: "builder", DisplayName: "Builder", Kind: "builder", Summary: "Implements scoped changes, exercises them, and leaves a working artifact.", Capabilities: []string{"implement", "test"}},
	{ID: "judge", DisplayName: "Judge", Kind: "judge", Summary: "Judges work against its acceptance criteria, scope, safety and evidence, and returns a clear pass or fail verdict with the blocking findings.", Capabilities: []string{"judge", "verify", "review"}},
	{ID: "orchestrator", DisplayName: "Orchestrator", Kind: "orchestrator", Summary: "Coordinates role-specialized agents, dependencies, handoffs, and completion gates.", Capabilities: []string{"orchestrate", "coordinate"}},
	{ID: "debugger", DisplayName: "Debugger", Kind: "debugger", Summary: "Diagnoses failures from evidence, isolates root causes, and verifies repairs.", Capabilities: []string{"debug", "diagnose"}},
	{ID: "reviewer", DisplayName: "Reviewer", Kind: "reviewer", Summary: "Reviews changes independently for correctness, regressions, and maintainability.", Capabilities: []string{"review", "audit"}},
}

func builtinPresetPersona(id string) (*PersonaCard, bool) {
	for _, preset := range personaPresetCatalog {
		if preset.ID != id {
			continue
		}
		req := CreatePersonaRequest{
			ID:           preset.ID,
			DisplayName:  preset.DisplayName,
			Kind:         preset.Kind,
			Summary:      preset.Summary,
			Capabilities: preset.Capabilities,
		}
		raw := renderPersona(req, normalizeTags(req.Capabilities))
		card, err := parsePersonaCard(id, []byte(raw))
		if err != nil {
			panic("invalid built-in persona preset " + id + ": " + err.Error())
		}
		card.Preset = true
		return card, true
	}
	return nil, false
}

func builtinPresetPersonas() []PersonaCard {
	cards := make([]PersonaCard, 0, len(personaPresetCatalog))
	for _, preset := range personaPresetCatalog {
		card, _ := builtinPresetPersona(preset.ID)
		cards = append(cards, *card)
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
	return cards
}
