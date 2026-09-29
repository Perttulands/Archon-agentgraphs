package formations

import (
	"errors"
	"fmt"
	"strings"
)

// Slot migration outcomes.
const (
	SlotMigrationMigrated  = "migrated"
	SlotMigrationWouldMove = "would-migrate"
	SlotMigrationOwned     = "owned"
	SlotMigrationSkipped   = "skipped"
)

// SlotMigrationActor records who wrote a migrated board revision.
const SlotMigrationActor = "archon:slot-settings-migration"

// SlotMigrationEntry is one staffed slot's migration: the settings it runs
// and the seat launch command before and after.
type SlotMigrationEntry struct {
	Board        string `json:"board"`
	FormationID  string `json:"formationId"`
	SlotID       string `json:"slotId"`
	Role         string `json:"role,omitempty"`
	Harness      string `json:"harness,omitempty"`
	Model        string `json:"model,omitempty"`
	Effort       string `json:"effort,omitempty"`
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason,omitempty"`
	LaunchBefore string `json:"launchBefore,omitempty"`
	LaunchAfter  string `json:"launchAfter,omitempty"`
	Identical    bool   `json:"identical"`
}

// SlotMigrationBoard is one board's migration: the revision written, if any.
type SlotMigrationBoard struct {
	Slug    string `json:"slug"`
	RevFrom int    `json:"revFrom,omitempty"`
	RevTo   int    `json:"revTo,omitempty"`
	Error   string `json:"error,omitempty"`
}

// SlotMigrationReport lists every staffed slot on every board.
type SlotMigrationReport struct {
	DryRun bool                 `json:"dryRun"`
	Boards []SlotMigrationBoard `json:"boards"`
	Slots  []SlotMigrationEntry `json:"slots"`
}

// MigrateSlotSettings writes each legacy slot's settings onto the slot. A
// legacy slot names a role and no model or effort of its own; it takes the
// role's current effective harness, model and effort, so its seats launch
// exactly as before. Slots that already own their settings are listed and left
// alone. With dryRun nothing is written. A board that cannot be read or
// written is reported and the others still migrate.
func (s *Store) MigrateSlotSettings(personas *PersonaStore, dryRun bool, only ...string) (SlotMigrationReport, error) {
	report := SlotMigrationReport{DryRun: dryRun, Boards: []SlotMigrationBoard{}, Slots: []SlotMigrationEntry{}}
	slugs := only
	if len(slugs) == 0 {
		names, err := s.listDefinitionNames(boardDefinitionKind)
		if err != nil {
			return report, err
		}
		for _, name := range names {
			slugs = append(slugs, strings.TrimSuffix(name, boardDefinitionKind.suffix))
		}
	}
	for _, slug := range slugs {
		boardReport, entries := s.migrateBoardSlots(slug, personas, dryRun)
		report.Boards = append(report.Boards, boardReport)
		report.Slots = append(report.Slots, entries...)
	}
	return report, nil
}

func (s *Store) migrateBoardSlots(slug string, personas *PersonaStore, dryRun bool) (SlotMigrationBoard, []SlotMigrationEntry) {
	result := SlotMigrationBoard{Slug: slug}
	board, err := s.ReadBoard(slug)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.RevFrom = board.Rev
	var entries []SlotMigrationEntry
	pending := map[[2]string]SlotSettings{}
	for _, formation := range board.Formations {
		for _, slot := range formation.Slots {
			if !slot.Staffed() {
				continue
			}
			entry := SlotMigrationEntry{Board: slug, FormationID: formation.ID, SlotID: slot.ID, Role: slot.AgentID}
			before, _, err := ResolveSlotSettings(slot, personas)
			if err != nil {
				entry.Outcome, entry.Reason = SlotMigrationSkipped, err.Error()
				entries = append(entries, entry)
				continue
			}
			entry.Harness, entry.Model, entry.Effort = before.Harness, before.Model, before.Effort
			entry.LaunchBefore = launchOrError(before)
			if !slot.legacyStaffing() {
				entry.Outcome, entry.LaunchAfter, entry.Identical = SlotMigrationOwned, entry.LaunchBefore, true
				entries = append(entries, entry)
				continue
			}
			if err := validateSlotSettings(slotName(slot), before.Harness, before.Model, before.Effort); err != nil {
				entry.Outcome, entry.Reason = SlotMigrationSkipped, err.Error()
				entries = append(entries, entry)
				continue
			}
			entry.Outcome = SlotMigrationWouldMove
			pending[[2]string{formation.ID, slot.ID}] = before
			entries = append(entries, entry)
		}
	}
	after := board
	if len(pending) > 0 && !dryRun {
		written, err := s.updateBoardDefinition(slug, SlotMigrationActor, WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}, func(raw []byte, _ *BoardDocument) ([]byte, error) {
			lines := splitLines(raw)
			for key, settings := range pending {
				lines = writeSlotSettings(lines, key[0], key[1], settings)
			}
			return renderTOMLLines(lines), nil
		})
		if err != nil {
			result.Error = err.Error()
			for i := range entries {
				if entries[i].Outcome == SlotMigrationWouldMove {
					entries[i].Outcome, entries[i].Reason = SlotMigrationSkipped, err.Error()
				}
			}
			return result, entries
		}
		after = written
		result.RevTo = written.Rev
	}
	// The after command comes from the slot as it now reads (or would read),
	// resolved through the same path a run takes.
	for i := range entries {
		if entries[i].Outcome != SlotMigrationWouldMove {
			continue
		}
		slot, ok := boardSlot(after, entries[i].FormationID, entries[i].SlotID)
		if !ok {
			entries[i].Outcome, entries[i].Reason = SlotMigrationSkipped, "slot disappeared during migration"
			continue
		}
		if dryRun {
			settings := pending[[2]string{entries[i].FormationID, entries[i].SlotID}]
			slot.Harness, slot.Model, slot.Effort = settings.Harness, settings.Model, settings.Effort
		} else {
			entries[i].Outcome = SlotMigrationMigrated
		}
		resolved, _, err := ResolveSlotSettings(slot, personas)
		if err != nil {
			entries[i].Reason = err.Error()
			continue
		}
		entries[i].LaunchAfter = launchOrError(resolved)
		entries[i].Identical = entries[i].LaunchAfter == entries[i].LaunchBefore
	}
	return result, entries
}

func boardSlot(board *BoardDocument, formationID, slotID string) (FormationSlot, bool) {
	for _, formation := range board.Formations {
		if formation.ID != formationID {
			continue
		}
		return findSlot(formation.Slots, slotID)
	}
	return FormationSlot{}, false
}

// launchOrError renders the seat command, or says why none renders. A CLI
// that is not on PATH still renders, named bare.
func launchOrError(settings SlotSettings) string {
	command, err := settings.LaunchCommand()
	if command != "" {
		return command
	}
	if err != nil {
		return "no seat command: " + err.Error()
	}
	return ""
}

// Failed names every board that could not be read or written, or is nil.
func (r SlotMigrationReport) Failed() error {
	var problems []string
	for _, board := range r.Boards {
		if board.Error != "" {
			problems = append(problems, fmt.Sprintf("%s: %s", board.Slug, board.Error))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.New("slot migration left boards unmigrated: " + strings.Join(problems, "; "))
}
