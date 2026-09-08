package formations

import "sort"

type personaPreset struct {
	ID           string
	DisplayName  string
	Kind         string
	Summary      string
	Capabilities []string
	Harness      string
	Model        string
	Effort       string
}

var personaPresetCatalog = []personaPreset{
	{ID: "codex-scout", DisplayName: "Codex Scout", Kind: "scout", Summary: "Explores a codebase, gathers evidence, and reports the terrain before changes begin.", Capabilities: []string{"research", "inspect"}},
	{ID: "codex-planner", DisplayName: "Codex Planner", Kind: "planner", Summary: "Turns grounded requirements into an ordered implementation plan with explicit verification.", Capabilities: []string{"planning", "design"}},
	{ID: "codex-builder", DisplayName: "Codex Builder", Kind: "builder", Summary: "Implements scoped changes, exercises them, and leaves a working artifact.", Capabilities: []string{"implement", "test"}},
	{ID: "codex-judge", DisplayName: "Codex Judge", Kind: "judge", Summary: "Evaluates evidence against acceptance criteria and returns a clear verdict.", Capabilities: []string{"judge", "verify"}},
	{ID: "codex-orchestrator", DisplayName: "Codex Orchestrator", Kind: "orchestrator", Summary: "Coordinates role-specialized agents, dependencies, handoffs, and completion gates.", Capabilities: []string{"orchestrate", "coordinate"}},
	{ID: "codex-debugger", DisplayName: "Codex Debugger", Kind: "debugger", Summary: "Diagnoses failures from evidence, isolates root causes, and verifies repairs.", Capabilities: []string{"debug", "diagnose"}},
	{ID: "codex-reviewer", DisplayName: "Codex Reviewer", Kind: "reviewer", Summary: "Reviews changes independently for correctness, regressions, and maintainability.", Capabilities: []string{"review", "audit"}},
	{ID: "delivery-planner", DisplayName: "Delivery Planner", Kind: "planner", Summary: "Turns the supplied brief into a scoped plan with verifiable outcomes.", Capabilities: []string{"planning", "design"}, Harness: "claude-code", Effort: "medium"},
	{ID: "delivery-beads-drafter", DisplayName: "Delivery Beads Drafter", Kind: "planner", Summary: "Drafts and revises the delivery Bead graph in the target repository's store.", Capabilities: []string{"planning", "beads"}, Harness: "claude-code", Effort: "medium"},
	{ID: "delivery-beads-reviewer", DisplayName: "Delivery Beads Reviewer", Kind: "judge", Summary: "Checks Bead goals, parent links, dependencies and lint evidence before execution.", Capabilities: []string{"judge", "beads"}, Harness: "claude-code", Effort: "medium"},
	{ID: "delivery-lead", DisplayName: "Delivery Lead", Kind: "orchestrator", Summary: "Directs the bound workers through reviewed Beads and verifies their delivery.", Capabilities: []string{"orchestrate", "verify"}, Harness: "claude-code", Effort: "medium"},
	{ID: "delivery-worker", DisplayName: "Delivery Worker", Kind: "builder", Summary: "Implements only the lead's assigned Bead and commits verified work within its scope.", Capabilities: []string{"implement", "test"}, Harness: "openai-codex", Effort: "medium"},
	{ID: "delivery-final-reviewer", DisplayName: "Delivery Final Reviewer", Kind: "reviewer", Summary: "Independently reviews the delivered diff, Bead closure evidence and plan, then writes a report.", Capabilities: []string{"review", "audit"}, Harness: "openai-codex", Model: "gpt-6-astra", Effort: "medium"},
}

func builtinPresetPersona(id string) (*PersonaCard, bool) {
	for _, preset := range personaPresetCatalog {
		if preset.ID != id {
			continue
		}
		tags := append([]string{}, preset.Capabilities...)
		harness := preset.Harness
		if harness == "" {
			harness = "openai-codex"
		}
		launch := "codex --yolo -c check_for_update_on_startup=false"
		if harness == "claude-code" {
			launch = "claude --dangerously-skip-permissions"
		}
		tags = append(tags, "role:"+preset.Kind, "provider:"+harness)
		req := CreatePersonaRequest{
			ID:           preset.ID,
			DisplayName:  preset.DisplayName,
			Kind:         preset.Kind,
			Summary:      preset.Summary,
			Capabilities: tags,
			Harness:      harness,
			SessionStem:  preset.ID,
			Launch:       launch,
			Model:        preset.Model,
			Effort:       preset.Effort,
		}
		raw := renderPersona(req, req.Harness, req.SessionStem, normalizeTags(req.Capabilities))
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
