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

// errRoleBinding marks a legacy slot whose role has no harness variant to take
// its settings from.
var errRoleBinding = errors.New("role binding")

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

// legacyStaffing reports a slot written before slots owned their settings: it
// names a role and has no model or effort of its own, so the role's harness
// variant still supplies them until the slot is migrated.
func (slot FormationSlot) legacyStaffing() bool {
	return slot.AgentID != "" && slot.Model == "" && slot.Effort == ""
}

// SlotSettings is what a staffed slot's seat runs.
type SlotSettings struct {
	// Role is the slot's optional persona; blank means a vanilla agent.
	Role    string `json:"role,omitempty"`
	Harness string `json:"harness"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort"`
	// FromRole says the settings came from the role's harness variant, because
	// the slot predates slots owning them and has not been migrated.
	FromRole bool `json:"fromRole,omitempty"`
	// SessionStem, Launch and Source are carried from the role's variant for the
	// run record; seats never run the launch string.
	SessionStem string `json:"-"`
	Launch      string `json:"-"`
	Source      string `json:"-"`
}

// Variant is the harness variant a seat for these settings starts from.
func (s SlotSettings) Variant() HarnessVariant {
	return HarnessVariant{ID: s.Harness, SessionStem: s.SessionStem, Model: s.Model, Effort: s.Effort, Launch: s.Launch, Source: s.Source}
}

// LaunchCommand is the seat command these settings start, as HarnessVariant
// renders it.
func (s SlotSettings) LaunchCommand() (string, error) {
	return s.Variant().LaunchCommand()
}

// ResolveSlotSettings returns what a staffed slot runs and its role card, if
// it has one. A slot's own harness, model and effort are authoritative; its
// role only adds role text. A legacy slot (a role and no model or effort of its
// own) takes its role's current effective settings, which is also the rule the
// slot migration writes down.
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
	if slot.legacyStaffing() {
		settings, err := roleSettings(card, slot.Harness)
		if err != nil {
			return SlotSettings{}, nil, fmt.Errorf("%w: %w", errRoleBinding, err)
		}
		return settings, card, nil
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

// roleSettings reads a role's current effective settings from its legacy
// harness variant: the variant's harness and model, and its effort or the
// default effort. It is the migration rule for slots that predate slot-owned
// settings.
func roleSettings(card *PersonaCard, harness string) (SlotSettings, error) {
	variant, err := card.SelectHarnessVariant(harness)
	if err != nil {
		return SlotSettings{}, err
	}
	return SlotSettings{
		Role:        card.ID,
		Harness:     variant.ID,
		Model:       variant.Model,
		Effort:      variant.effectiveEffort(),
		FromRole:    true,
		SessionStem: variant.SessionStem,
		Launch:      variant.Launch,
		Source:      variant.Source,
	}, nil
}

// validateSlotSettings checks that a seat can start from these settings. The
// effort is required, so staffing always states it.
func validateSlotSettings(slot, harnessID, model, effort string) error {
	harnessIDs := make([]string, 0, len(launchableHarnesses))
	for _, harness := range launchableHarnesses {
		harnessIDs = append(harnessIDs, harness.ID)
	}
	if harnessID == "" {
		return fmt.Errorf("%w: slot %s needs a harness: %s", ErrInvalidSlotSettings, slot, strings.Join(harnessIDs, " or "))
	}
	harness, ok := launchableHarness(harnessID)
	if !ok {
		return fmt.Errorf("%w: slot %s harness %q cannot start seats; use %s", ErrInvalidSlotSettings, slot, harnessID, strings.Join(harnessIDs, " or "))
	}
	if strings.IndexFunc(model, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%w: slot %s model %q must be one model name without spaces", ErrInvalidSlotSettings, slot, model)
	}
	if effort == "" {
		return fmt.Errorf("%w: slot %s needs an effort; the policy is %s", ErrInvalidSlotSettings, slot, EffortPolicyText())
	}
	if !slices.Contains(harness.Efforts, effort) {
		return fmt.Errorf("%w: slot %s effort %q is not one %s accepts; use %s", ErrInvalidSlotSettings, slot, effort, harness.ID, strings.Join(harness.Efforts, ", "))
	}
	return nil
}

// RefuseRoleSettings refuses a model or effort on a new role card. Slots own
// what their seats run; a role is only role text. Existing cards' legacy
// model and effort are still read, for migration and the role drag.
func RefuseRoleSettings(model, effort string) error {
	if strings.TrimSpace(model) == "" && strings.TrimSpace(effort) == "" {
		return nil
	}
	return fmt.Errorf("%w: a new role carries no model or effort; set them on each slot that uses it (archon formation assign ... --harness --model --effort --role)", ErrInvalidAgentCard)
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
	if slot.legacyStaffing() {
		parts := []string{role}
		if slot.Harness != "" {
			parts = append(parts, slot.Harness)
		}
		return strings.Join(append(parts, "model and effort from the role"), " · ")
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
