package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// archon end create|update|delete (archon-o7p.10). An End node ends a path on
// purpose: wire a step's output or a gate's pass or fail route to its input
// (<end-id>:in) with archon formation wire.

const endOutcomeHelp = "how the path ends: done, or rejected, which fails the run with the gate's reason"

func runEndCommand(store *formations.Store, verb string, args []string, stdout, stderr io.Writer) int {
	switch verb {
	case "create":
		return runEndCreate(store, args, stdout, stderr)
	case "update":
		return runEndUpdate(store, args, stdout, stderr)
	case "delete":
		return runEndDelete(store, args, stdout, stderr)
	default:
		return unknownCommand(stderr, "end", verb)
	}
}

func runEndCreate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("end create", stderr)
	outcome := fs.String("outcome", formations.EndOutcomeDone, endOutcomeHelp)
	title := fs.String("title", "", "End node title (default Done or Rejected)")
	x := fs.Int("x", 0, "layout x coordinate")
	y := fs.Int("y", 0, "layout y coordinate")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("end create"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return fail(stderr, err)
	}
	createX, createY, err := resolveCreateCoordinates(store, slug, fs, *x, *y)
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result, err := store.CreateEnd(slug, formations.EndCreateRequest{
		Title:     *title,
		Outcome:   *outcome,
		X:         createX,
		Y:         createY,
		UpdatedBy: *updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeCreated(stdout, *jsonOut, result, result.Board, result.Layout, result.End.ID)
}

func endUpdateFlags(name string, stderr io.Writer) (*flag.FlagSet, *string, *string, *string, *bool) {
	fs := commandFlags(name, stderr)
	outcome := fs.String("outcome", "", endOutcomeHelp)
	title := fs.String("title", "", "End node title")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	return fs, outcome, title, updatedBy, jsonOut
}

func runEndUpdate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs, outcome, title, updatedBy, jsonOut := endUpdateFlags("end update", stderr)
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("end update"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return fail(stderr, err)
	}
	endID, err := resolveEndSelector(board, fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "end", fs.Arg(1))
	}
	given := givenFlags(fs)
	update := formations.EndUpdateRequest{EndID: endID, UpdatedBy: *updatedBy}
	if given["title"] {
		update.Title = title
	}
	if given["outcome"] {
		update.Outcome = outcome
	}
	result, err := store.UpdateEnd(slug, update, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintln(stdout, "updated End node")
	return 0
}

func runEndDelete(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("end delete", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("end delete"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return fail(stderr, err)
	}
	endID, err := resolveEndSelector(board, fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "end", fs.Arg(1))
	}
	result, err := store.DeleteEnd(slug, formations.EndDeleteRequest{ID: endID, UpdatedBy: *updatedBy}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeEndDeleted(stdout, *jsonOut, result)
}

func writeEndDeleted(stdout io.Writer, jsonOut bool, result *formations.EndDeleteResult) int {
	if result.Board != nil {
		result.Board.TOML = ""
	}
	if result.Layout != nil {
		result.Layout.TOML = ""
	}
	if jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "deleted End node %s\n", result.EndID)
	return 0
}

func resolveEndSelector(board *formations.BoardDocument, selector string) (string, error) {
	candidates := make([]graphSelectorCandidate, 0, len(board.Ends))
	for _, end := range board.Ends {
		candidates = append(candidates, graphSelectorCandidate{ID: end.ID, Title: end.Title})
	}
	return resolveGraphSelector("end", selector, candidates)
}

func remoteEndCreate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("end create", stderr)
	outcome := fs.String("outcome", formations.EndOutcomeDone, endOutcomeHelp)
	title := fs.String("title", "", "End node title (default Done or Rejected)")
	x := fs.Int("x", 0, "layout x coordinate")
	y := fs.Int("y", 0, "layout y coordinate")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("end create"))
		return 2
	}
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		createX, createY, err := c.freePosition(board, fs, *x, *y)
		return "createEnd", map[string]any{"title": *title, "outcome": *outcome, "x": createX, "y": createY}, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result, err := decodeRemote[formations.EndCreateResult](data, "")
	if err != nil || result.Board == nil || result.Layout == nil {
		return fail(stderr, fmt.Errorf("coordinator response has no created End node"))
	}
	return writeCreated(stdout, *jsonOut, result, result.Board, result.Layout, result.End.ID)
}

func remoteEndUpdate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs, outcome, title, updatedBy, jsonOut := endUpdateFlags("end update", stderr)
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("end update"))
		return 2
	}
	given := givenFlags(fs)
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		endID, err := remoteSelect("end", fs.Arg(1), resolveEndSelector, board)
		fields := map[string]any{"id": endID}
		if given["title"] {
			fields["title"] = *title
		}
		if given["outcome"] {
			fields["outcome"] = *outcome
		}
		return "updateEnd", fields, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, "updated End node")
}

func remoteEndDelete(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("end delete", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("end delete"))
		return 2
	}
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		endID, err := remoteSelect("end", fs.Arg(1), resolveEndSelector, board)
		return "deleteEnd", map[string]any{"id": endID}, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result, err := decodeRemote[formations.EndDeleteResult](data, "")
	if err != nil || result.Board == nil {
		return fail(stderr, fmt.Errorf("coordinator response has no deleted End node"))
	}
	return writeEndDeleted(stdout, *jsonOut, result)
}
