package formations

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// ErrInvalidSlotSettings marks a slot's harness, model or effort that no seat
// could start with, or staffing that leaves the effort unchosen.
var ErrInvalidSlotSettings = errors.New("invalid slot settings")

// EffortPolicyEntry is one line of the effort policy: which effort suits which
// kind of work.
type EffortPolicyEntry struct {
	Effort string `json:"effort"`
	Use    string `json:"use"`
}

// effortPolicy is Perttu's effort policy (2026-09-29). It guides the choice;
// the slot still states its own effort, and any effort its harness accepts is
// valid.
var effortPolicy = []EffortPolicyEntry{
	{Effort: "low", Use: "errands"},
	{Effort: "medium", Use: "making things"},
	{Effort: "xhigh", Use: "architecture and review"},
	{Effort: "max", Use: "consequential reviews"},
}

// EffortPolicy returns the effort policy for readers such as the agents API
// and CLI usage.
func EffortPolicy() []EffortPolicyEntry {
	return slices.Clone(effortPolicy)
}

// EffortPolicyText reads the policy as one clause: "low for errands, ...".
func EffortPolicyText() string {
	parts := make([]string, 0, len(effortPolicy))
	for _, entry := range effortPolicy {
		parts = append(parts, entry.Effort+" for "+entry.Use)
	}
	return strings.Join(parts, ", ")
}

// Staffed reports whether the slot names anything to run: a harness, a model,
// an effort or a role.
func (slot FormationSlot) Staffed() bool {
	return slot.AgentID != "" || slot.Harness != "" || slot.Model != "" || slot.Effort != ""
}

// SlotSettings is what a staffed slot's seat runs.
type SlotSettings struct {
	// Role is the slot's optional persona; blank means a vanilla agent.
	Role    string `json:"role,omitempty"`
	Harness string `json:"harness"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort"`
	// SessionStem and Source are carried from the role's variant for the run
	// record.
	SessionStem string `json:"-"`
	Source      string `json:"-"`
}

// Variant is the harness variant a seat for these settings starts from.
func (s SlotSettings) Variant() HarnessVariant {
	return HarnessVariant{ID: s.Harness, SessionStem: s.SessionStem, Model: s.Model, Effort: s.Effort, Source: s.Source}
}

// LaunchCommand is the seat command these settings start, as HarnessVariant
// renders it.
func (s SlotSettings) LaunchCommand() (string, error) {
	return s.Variant().LaunchCommand()
}

// ResolveSlotSettings returns what a staffed slot runs and its role card, if
// it has one. A slot's own harness, model and effort are authoritative; its
// role only adds role text.
func ResolveSlotSettings(slot FormationSlot, personas *PersonaStore) (SlotSettings, *PersonaCard, error) {
	if !slot.Staffed() {
		return SlotSettings{}, nil, fmt.Errorf("%w: slot %s is not staffed", ErrInvalidSlotSettings, slotName(slot))
	}
	var card *PersonaCard
	if slot.AgentID != "" {
		if personas == nil {
			return SlotSettings{}, nil, fmt.Errorf("%w: persona store required for slot %s", ErrNotFound, slotName(slot))
		}
		read, err := personas.ReadPersona(slot.AgentID)
		if err != nil {
			return SlotSettings{}, nil, fmt.Errorf("%w: role %q", err, slot.AgentID)
		}
		card = read
	}
	if err := validateSlotSettings(slotName(slot), slot.Harness, slot.Model, slot.Effort); err != nil {
		return SlotSettings{}, nil, err
	}
	settings := SlotSettings{Role: slot.AgentID, Harness: slot.Harness, Model: slot.Model, Effort: slot.Effort, SessionStem: slot.ID}
	if card != nil {
		settings.SessionStem = card.ID
		if variant, err := card.SelectHarnessVariant(slot.Harness); err == nil && variant.SessionStem != "" {
			settings.SessionStem = variant.SessionStem
		}
	}
	return settings, card, nil
}

// validateSlotSettings checks that a seat can start from these settings. The
// effort is required, so staffing always states it.
func validateSlotSettings(slot, harnessID, model, effort string) error {
	return validateRunSettings("slot "+slot, harnessID, model, effort)
}

// ValidateSpawnSettings checks what `archon agent spawn` starts a role's
// session with; a role card carries no model or effort, so the spawn states
// them.
func ValidateSpawnSettings(agentID, harnessID, model, effort string) error {
	return validateRunSettings(fmt.Sprintf("agent %q", agentID), harnessID, model, effort)
}

// validateRunSettings checks a harness, model and effort a session starts
// with; subject names what is checked, such as `slot "Worker" (w1)`.
func validateRunSettings(subject, harnessID, model, effort string) error {
	harnessIDs := make([]string, 0, len(launchableHarnesses))
	for _, harness := range launchableHarnesses {
		harnessIDs = append(harnessIDs, harness.ID)
	}
	if harnessID == "" {
		return fmt.Errorf("%w: %s needs a harness: %s", ErrInvalidSlotSettings, subject, strings.Join(harnessIDs, " or "))
	}
	harness, ok := launchableHarness(harnessID)
	if !ok {
		return fmt.Errorf("%w: %s harness %q cannot start seats; use %s", ErrInvalidSlotSettings, subject, harnessID, strings.Join(harnessIDs, " or "))
	}
	if strings.IndexFunc(model, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%w: %s model %q must be one model name without spaces", ErrInvalidSlotSettings, subject, model)
	}
	if effort == "" {
		return fmt.Errorf("%w: %s needs an effort; the policy is %s", ErrInvalidSlotSettings, subject, EffortPolicyText())
	}
	// A model whose levels the host knows narrows the harness's efforts.
	efforts, accepts := harness.Efforts, harness.ID
	if known, ok := modelEfforts(harness.ID, model); ok {
		efforts, accepts = known, model
	}
	if !slices.Contains(efforts, effort) {
		return fmt.Errorf("%w: %s effort %q is not one %s accepts; use %s", ErrInvalidSlotSettings, subject, effort, accepts, strings.Join(efforts, ", "))
	}
	return nil
}

// roleName names a seat's role in lab output: the persona id, or "vanilla"
// for a slot without one.
func roleName(card PersonaCard) string {
	if card.ID == "" {
		return "vanilla"
	}
	return card.ID
}

// roleLine is a seat brief's agent line. A role slot's line is unchanged from
// before slots owned their settings; a vanilla slot says it has no role.
func roleLine(card PersonaCard, _ HarnessVariant) string {
	if card.ID == "" {
		return "agent: vanilla (no role)\n"
	}
	return "agent: " + card.ID + "\n"
}

// StaffingSummary reads a slot's staffing in one line: role or "vanilla",
// then harness, model and effort, as in "vanilla · claude-code · opus · low".
func (slot FormationSlot) StaffingSummary() string {
	if !slot.Staffed() {
		return "not staffed"
	}
	role := slot.AgentID
	if role == "" {
		role = "vanilla"
	}
	model := slot.Model
	if model == "" {
		model = "default model"
	}
	effort := slot.Effort
	if effort == "" {
		effort = "no effort"
	}
	harness := slot.Harness
	if harness == "" {
		harness = "no harness"
	}
	return strings.Join([]string{role, harness, model, effort}, " · ")
}
