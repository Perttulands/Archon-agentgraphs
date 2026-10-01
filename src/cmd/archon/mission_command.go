package main

import (
	"fmt"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// A mission is the reusable unit: one .mission.toml file whose Input card
// starts its runs. Internal names still say board; everything a person or
// agent types or reads says mission.

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h" || arg == "-help"
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

// runInputCard picks the Input card a run starts from when none is named. A
// file with several cannot run at all, so it answers with the admission
// finding that says how to split it rather than asking which card.
func runInputCard(board *formations.BoardDocument, missionSelector string) (string, error) {
	if len(board.Missions) > 1 {
		return "", &formations.RunAdmissionError{Findings: []formations.BoardFinding{formations.SeveralInputCardsFinding(board)}}
	}
	return soleInputCard(board, missionSelector)
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
