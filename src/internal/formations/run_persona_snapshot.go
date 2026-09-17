package formations

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// readRunPersonaBinding resolves a seat entirely from the admitted run. Old
// bindings contain only a path and digest, so they cannot honestly recover a
// persona's original content or settings from today's editable card.
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
	if document.Schema == 1 {
		return nil, HarnessVariant{}, runExecutionError("persona_snapshot_incomplete", "this older run has no frozen persona content or settings; start a new run to continue with current personas", "executor", nil)
	}
	if document.Schema != 2 {
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
	if found == nil || found.AgentID != slot.AgentID || found.CardTOML == "" || found.CardHash != etag([]byte(found.CardTOML)) {
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
