package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// runBoardMigrateSlots writes each legacy slot's role settings onto the slot,
// as archond does at startup, and prints every staffed slot's seat launch
// command before and after. --dry-run writes nothing.
func runBoardMigrateSlots(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mission migrate-slots", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "report without writing")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "dry-run": true})); err != nil {
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "usage: archon mission migrate-slots [<mission>] [--dry-run] [--json]\nRole cards are read from CHROTE_AGENTS_DIR (default ~/agents).")
		return 2
	}
	var only []string
	if fs.NArg() == 1 {
		slug, err := store.ResolveBoardSelector(fs.Arg(0))
		if err != nil {
			return failSelector(stderr, err, *jsonOut, "board", fs.Arg(0))
		}
		only = []string{slug}
	}
	report, err := store.MigrateSlotSettings(formations.NewPersonaStore(formations.DefaultAgentsDir()), *dryRun, only...)
	if err != nil {
		return fail(stderr, err)
	}
	if *jsonOut {
		if code := writeJSON(stdout, report); code != 0 {
			return code
		}
	} else {
		for _, slot := range report.Slots {
			fmt.Fprintf(stdout, "%s\t%s/%s\t%s\t%s\n", slot.Outcome, slot.Board, slot.FormationID, slot.SlotID, migrationSettingsText(slot))
			if slot.Reason != "" {
				fmt.Fprintf(stdout, "  reason: %s\n", slot.Reason)
			}
			if slot.LaunchBefore != "" {
				fmt.Fprintf(stdout, "  before: %s\n  after:  %s\n  identical: %t\n", slot.LaunchBefore, slot.LaunchAfter, slot.Identical)
			}
		}
		for _, board := range report.Boards {
			if board.Error != "" {
				fmt.Fprintf(stdout, "board %s not migrated: %s\n", board.Slug, board.Error)
			} else if board.RevTo != 0 {
				fmt.Fprintf(stdout, "board %s rev %d -> %d\n", board.Slug, board.RevFrom, board.RevTo)
			}
		}
	}
	if report.Failed() != nil {
		return 1
	}
	return 0
}

func migrationSettingsText(slot formations.SlotMigrationEntry) string {
	role := slot.Role
	if role == "" {
		role = "vanilla"
	}
	model := slot.Model
	if model == "" {
		model = "default model"
	}
	if slot.Harness == "" {
		return role
	}
	return role + " · " + slot.Harness + " · " + model + " · " + slot.Effort
}
