package formations

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// readRunPersonaBinding resolves a seat entirely from the admitted run: the
// slot's frozen harness, model and effort, and its frozen role card (an empty
// card for a vanilla slot). Schema-1 bindings contain only a path and digest,
// so they cannot honestly recover a persona's original content or settings
// from today's editable card.
func (s *Store) readRunPersonaBinding(runID, nodeID string, slot FormationSlot) (*PersonaCard, HarnessVariant, error) {
	invalid := func(reason string, cause error) (*PersonaCard, HarnessVariant, error) {
		return nil, HarnessVariant{}, runExecutionError("persona_snapshot_invalid", reason, "executor", cause)
	}
	ledger, err := s.openRunLedger(runID, false)
	if err != nil {
		return invalid("could not open run persona snapshot", err)
	}
	defer ledger.close()
	events, err := readRunEventsFrom(ledger.file, runID)
	if err != nil || len(events) == 0 {
		return invalid("could not read run persona snapshot identity", err)
	}
	if err := s.validateRunSnapshotIdentity(events[0], runID, ledger); err != nil {
		return invalid("invalid run persona snapshot identity", err)
	}
	raw, err := readRunArtifactAt(ledger.directory, runID+".bindings.toml", runRecordMaxBytes)
	if err != nil {
		return invalid("could not read run persona snapshot", err)
	}
	var document struct {
		Schema    int          `toml:"schema"`
		RunID     string       `toml:"runId"`
		BoardID   string       `toml:"boardId"`
		BoardSlug string       `toml:"boardSlug"`
		BoardRev  int          `toml:"boardRev"`
		MissionID string       `toml:"missionId"`
		Bindings  []runBinding `toml:"binding"`
	}
	if err := toml.Unmarshal(raw, &document); err != nil {
		return invalid("invalid run persona snapshot", err)
	}
	started := events[0]
	if document.RunID != runID || document.BoardID != started.BoardID || document.BoardSlug != stringFromEventData(started, "boardSlug") || document.BoardRev != started.BoardRev || document.MissionID != started.MissionID {
		return invalid("run persona snapshot belongs to a different run or board", nil)
	}
	if document.Schema == 1 {
		return nil, HarnessVariant{}, runExecutionError("persona_snapshot_incomplete", "this older run has no frozen persona content or settings; start a new run to continue with current personas", "executor", nil)
	}
	if document.Schema != 2 && document.Schema != 3 {
		return invalid("unsupported run persona snapshot schema", nil)
	}
	var found *runBinding
	for i := range document.Bindings {
		binding := &document.Bindings[i]
		if binding.NodeID == nodeID && binding.SlotID == slot.ID {
			if found != nil {
				return invalid("duplicate run persona binding", nil)
			}
			found = binding
		}
	}
	if found == nil || found.AgentID != slot.AgentID {
		return invalid(fmt.Sprintf("missing or invalid frozen staffing for slot %q", slot.ID), nil)
	}
	if document.Schema == 2 {
		return schema2PersonaBinding(found, slot, invalid)
	}
	// Schema 3 freezes the slot's own harness, model and effort. A role card,
	// when the slot has one, adds only its role text.
	card := &PersonaCard{}
	if found.AgentID != "" {
		if found.CardTOML == "" || found.CardHash != etag([]byte(found.CardTOML)) {
			return invalid(fmt.Sprintf("missing or invalid frozen role for slot %q", slot.ID), nil)
		}
		parsed, err := parsePersonaCard(found.AgentID, []byte(found.CardTOML))
		if err != nil {
			return invalid("invalid frozen role content", err)
		}
		card = parsed
	}
	// The frozen settings must be what the frozen slot states, or, for a slot
	// that predates slot-owned settings, what its frozen role supplied.
	want := SlotSettings{Harness: slot.Harness, Model: slot.Model, Effort: slot.Effort, SessionStem: found.SessionStem}
	if slot.legacyStaffing() {
		settings, err := roleSettings(card, slot.Harness)
		if err != nil {
			return invalid("invalid frozen role harness", err)
		}
		want = settings
	}
	variant := HarnessVariant{ID: found.Harness, SessionStem: found.SessionStem, Model: found.Model, Effort: found.Effort, Launch: found.Launch, Source: found.Source}
	if found.Effort == "" || found.Harness != want.Harness || found.Model != want.Model || found.Effort != want.Effort || found.SessionStem != want.SessionStem || found.Launch != want.Launch || found.Source != want.Source {
		return invalid(fmt.Sprintf("frozen settings for slot %q do not match its frozen staffing", slot.ID), nil)
	}
	return card, variant, nil
}

// schema2PersonaBinding reads a run admitted before slots owned their
// settings: the frozen persona card's variant holds the seat's settings.
func schema2PersonaBinding(found *runBinding, slot FormationSlot, invalid func(string, error) (*PersonaCard, HarnessVariant, error)) (*PersonaCard, HarnessVariant, error) {
	if found.CardTOML == "" || found.CardHash != etag([]byte(found.CardTOML)) {
		return invalid(fmt.Sprintf("missing or invalid frozen persona for slot %q", slot.ID), nil)
	}
	card, err := parsePersonaCard(found.AgentID, []byte(found.CardTOML))
	if err != nil {
		return invalid("invalid frozen persona content", err)
	}
	variant, err := card.SelectHarnessVariant(slot.Harness)
	if err != nil {
		return invalid("invalid frozen persona harness", err)
	}
	if variant.ID != found.Harness || variant.SessionStem != found.SessionStem || variant.Model != found.Model || variant.effectiveEffort() != found.Effort || variant.Launch != found.Launch || variant.Source != found.Source {
		return invalid("frozen persona settings do not match its content", nil)
	}
	// Empty model deliberately preserves the persona's harness-default policy.
	// Pin a model in the persona when a run must retain one exact model identity.
	variant.Effort = found.Effort
	return card, variant, nil
}
