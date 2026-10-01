package formations

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// readRunPersonaBinding resolves a seat entirely from the admitted run: the
// slot's frozen harness, model and effort, and its frozen role card (an empty
// card for a vanilla slot).
func (s *Store) readRunPersonaBinding(runID, nodeID string, slot FormationSlot) (*PersonaCard, HarnessVariant, error) {
	invalid := func(reason string, cause error) (*PersonaCard, HarnessVariant, error) {
		return nil, HarnessVariant{}, runExecutionError("persona_snapshot_invalid", reason, "executor", cause)
	}
	ledger, err := s.openRunLedger(runID, false)
	if err != nil {
		return invalid("could not open run persona snapshot", err)
	}
	defer ledger.close()
	events, err := classifyAndReadRunEvents(ledger.file, runID)
	if err != nil || len(events) == 0 {
		return invalid("could not read run persona snapshot identity", err)
	}
	if err := s.validateRunSnapshotIdentity(events[0], runID, ledger); err != nil {
		return invalid("invalid run persona snapshot identity", err)
	}
	raw, err := readRunArtifactAt(ledger.directory, runID+".bindings.toml", runtimeAuthorityMaxRecordBytes)
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
	if document.Schema != 3 {
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
	// The snapshot freezes the slot's own harness, model and effort. A role
	// card, when the slot has one, adds only its role text.
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
	// The frozen settings must be what the frozen slot states.
	variant := HarnessVariant{ID: found.Harness, SessionStem: found.SessionStem, Model: found.Model, Effort: found.Effort, Source: found.Source}
	if found.Effort == "" || found.Harness != slot.Harness || found.Model != slot.Model || found.Effort != slot.Effort {
		return invalid(fmt.Sprintf("frozen settings for slot %q do not match its frozen staffing", slot.ID), nil)
	}
	return card, variant, nil
}
