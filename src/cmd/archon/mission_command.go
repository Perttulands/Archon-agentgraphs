package main

import (
	"fmt"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// A mission is the reusable unit: one .formation.toml file whose Input card
// starts its runs. Internal names still say board; everything a person or
// agent types or reads says mission, and "archon board" stays a deprecated
// alias for one release.

const missionHelp = `usage: archon mission <command> [arguments] [--json]

A mission is one reusable .formation.toml file. Its Input card starts each run.

  new <slug> [--title <title>]             create an empty mission
  list                                     list the missions
  inspect <mission>                        print the whole mission
  inspect <mission> <input>                print an Input card and the steps it reaches
  notes <mission>                          print the mission's note threads
  note <mission> [--node <id>] --text <t>  add to a note thread (see "archon mission note")
  validate <mission>                       list every finding that would stop a run
  arrange <mission>                        lay the canvas out along the run
  create <mission> [--title --goal ...]    add the Input card to a mission that has none
  update <mission> [<input>] --goal <g>    change the Input card (see "archon mission update")
  wire <mission> [<input>] <node:port>     wire the Input card to the first step
  run <mission> [--brief <text>]           start a run

<input> may be left out when the mission has one Input card.
"archon board <command>" is a deprecated alias for new, list, inspect, notes,
note, validate and arrange, and will be removed in a later release.
`

const missionInspectUsage = "usage: archon mission inspect <mission> [<input>] [--json]\n" +
	"With <input>, prints that Input card and the steps it reaches."

// boardAliasVerbs are the archon board commands that still work, each as the
// archon mission command of the same name.
var boardAliasVerbs = map[string]bool{
	"new": true, "list": true, "inspect": true, "notes": true, "note": true, "validate": true, "arrange": true,
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h" || arg == "-help"
}

// missionListTakesNoArgument answers the pre-rename "mission list <board>",
// which listed a board's mission nodes, with the command that now shows them.
func missionListTakesNoArgument(selector string) error {
	return fmt.Errorf("archon mission list lists missions and takes no argument; to see the Input card of %q, run: archon mission inspect %s", selector, selector)
}

// secondInputCard refuses mission create on a mission that already has an
// Input card: one mission per file.
func secondInputCard(board *formations.BoardDocument, selector string) error {
	if len(board.Missions) == 0 {
		return nil
	}
	return fmt.Errorf("%w: mission %q already has its Input card %q; change it with: archon mission update %s --title <title> --goal <goal>, or start another mission with: archon mission new <slug>", formations.ErrConflict, selector, board.Missions[0].ID, selector)
}

// inputCardArgs splits a command's positional arguments after the mission
// selector into the Input card selector and the rest. Given one argument
// fewer than full, the Input card is the mission's only one.
func inputCardArgs(board *formations.BoardDocument, missionSelector string, args []string, full int) (string, []string, error) {
	if len(args) == full {
		id, err := resolveMissionSelector(board, args[0])
		return id, args[1:], err
	}
	id, err := soleInputCard(board, missionSelector)
	return id, args, err
}

// soleInputCard names the mission's Input card when it has exactly one.
func soleInputCard(board *formations.BoardDocument, missionSelector string) (string, error) {
	switch len(board.Missions) {
	case 0:
		return "", fmt.Errorf("%w: mission %q has no Input card; add one with: archon mission create %s", formations.ErrNotFound, missionSelector, missionSelector)
	case 1:
		return board.Missions[0].ID, nil
	default:
		return "", fmt.Errorf("%w: mission %q has %d Input cards; name one of them", formations.ErrAmbiguousSelector, missionSelector, len(board.Missions))
	}
}
