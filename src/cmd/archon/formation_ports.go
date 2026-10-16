package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Port edits (archon-5tg1): relabel a step's port or set what an output port
// carries, text, a file or a folder.

// portChange is what one formation set-port command asks for.
type portChange struct {
	label *string
	kind  *string
}

func portChangeFlags(fs *flag.FlagSet) func() (portChange, error) {
	label := fs.String("label", "", "the port's label")
	kind := fs.String("kind", "", "an output port's kind: text, file or folder")
	return func() (portChange, error) {
		given := givenFlags(fs)
		change := portChange{}
		if given["label"] {
			change.label = label
		}
		if given["kind"] {
			change.kind = kind
		}
		if change.label == nil && change.kind == nil {
			return change, errors.New("give --label, --kind or both")
		}
		return change, nil
	}
}

// resolvePortSelector finds a step's port by ID or label.
func resolvePortSelector(board *formations.BoardDocument, formationID, selector string) (string, error) {
	var candidates []graphSelectorCandidate
	for _, formation := range board.Formations {
		if formation.ID != formationID {
			continue
		}
		for _, port := range append(append([]formations.FormationPort{}, formation.Inputs...), formation.Outputs...) {
			candidates = append(candidates, graphSelectorCandidate{ID: port.ID, Title: port.Label})
		}
	}
	return resolveGraphSelector("port", selector, candidates)
}

func runFormationSetPort(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("formation set-port", stderr)
	read := portChangeFlags(fs)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	change, err := read()
	if fs.NArg() != 3 || err != nil {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		fmt.Fprintln(stderr, commandUsage("formation set-port"))
		return 2
	}
	slug, board, formationID, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	portID, err := resolvePortSelector(board, formationID, fs.Arg(2))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "port", fs.Arg(2))
	}
	result, err := store.UpdateFormationPort(slug, formations.FormationPortUpdateRequest{
		FormationID: formationID,
		PortID:      portID,
		Label:       change.label,
		Kind:        change.kind,
		UpdatedBy:   *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "port", fs.Arg(2))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "updated port %s on %s\n", portID, formationID)
	return 0
}

func remoteFormationSetPort(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("formation set-port", stderr)
	read := portChangeFlags(fs)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	change, err := read()
	if fs.NArg() != 3 || err != nil {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		fmt.Fprintln(stderr, commandUsage("formation set-port"))
		return 2
	}
	formationID, portID := "", ""
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		id, err := remoteSelect("formation", fs.Arg(1), resolveFormationSelector, board)
		if err != nil {
			return "", nil, err
		}
		port, err := resolvePortSelector(board, id, fs.Arg(2))
		if err != nil {
			return "", nil, &remoteSelectorError{boundary: "port", selector: fs.Arg(2), err: err}
		}
		formationID, portID = id, port
		fields := map[string]any{"formationId": id, "portId": port}
		if change.label != nil {
			fields["label"] = *change.label
		}
		if change.kind != nil {
			fields["kind"] = *change.kind
		}
		return "updatePort", fields, nil
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "port", fs.Arg(2))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("updated port %s on %s", portID, formationID))
}
