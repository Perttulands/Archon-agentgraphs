package daemon

import (
	"fmt"
	"io"
	"os"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// migrateSlotSettings gives every legacy slot its role's current harness,
// model and effort before the daemon serves, so each mission states what its
// seats run. A board that cannot be migrated is reported and keeps working:
// admission still reads a legacy slot's settings from its role.
func migrateSlotSettings(store *formations.Store, personas *formations.PersonaStore) {
	writeSlotMigration(os.Stdout, store, personas)
}

func writeSlotMigration(out io.Writer, store *formations.Store, personas *formations.PersonaStore) {
	report, err := store.MigrateSlotSettings(personas, false)
	if err != nil {
		fmt.Fprintf(out, "Archon slot migration could not list missions: %v\n", err)
		return
	}
	for _, slot := range report.Slots {
		switch slot.Outcome {
		case formations.SlotMigrationMigrated:
			fmt.Fprintf(out, "Archon slot migration: %s %s/%s now runs %s · %s · %s (launch unchanged: %t)\n", slot.Board, slot.FormationID, slot.SlotID, slot.Harness, modelOrDefault(slot.Model), slot.Effort, slot.Identical)
		case formations.SlotMigrationSkipped:
			fmt.Fprintf(out, "Archon slot migration: %s %s/%s left as it was: %s\n", slot.Board, slot.FormationID, slot.SlotID, slot.Reason)
		}
	}
	if err := report.Failed(); err != nil {
		fmt.Fprintf(out, "Archon %v\n", err)
	}
}

func modelOrDefault(model string) string {
	if model == "" {
		return "default model"
	}
	return model
}
