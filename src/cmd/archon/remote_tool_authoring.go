package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Tool commands through --server send the same Tool frame the cockpit does.
// Tool writes fence the layout as well as the board, so each write carries the
// layout's state and ETag from the read it was built on.

// patchTool applies one Tool operation, retrying a lost write race like
// patchBoard. build resolves selectors against the board it is given.
func (c *remoteClient) patchTool(selector, updatedBy string, build func(*formations.BoardDocument) (string, map[string]any, error)) (json.RawMessage, error) {
	for attempt := 1; ; attempt++ {
		board, err := c.readBoard(selector)
		if err != nil {
			return nil, err
		}
		data, _, err := c.call("GET", boardPath(board.Slug, "layout"), nil, "")
		if err != nil {
			return nil, &remoteSelectorError{boundary: "board", selector: selector, err: err}
		}
		layout, err := decodeRemote[formations.LayoutDocument](data, "layout")
		if err != nil {
			return nil, &remoteSelectorError{boundary: "board", selector: selector, err: err}
		}
		// The daemon reports a missing layout with ETag "*".
		expectation := map[string]any{"state": formations.LayoutWriteAbsent}
		if layout.ETag != "*" {
			expectation = map[string]any{"state": formations.LayoutWritePresent, "etag": layout.ETag}
		}
		operation, fields, err := build(board)
		if err != nil {
			return nil, err
		}
		body := map[string]any{operation: fields, "expectedRev": board.Rev, "layoutExpectation": expectation, "updatedBy": updatedBy}
		data, _, err = c.call("PATCH", boardPath(board.Slug), body, board.ETag)
		if isRemoteWriteRace(err) && attempt < remoteWriteAttempts {
			continue
		}
		return data, err
	}
}

// remoteToolFail reports a Tool failure with a JSON error, as the offline
// Tool commands do.
func remoteToolFail(stderr io.Writer, err error, jsonOut bool, boundary, selector string) int {
	var selectorErr *remoteSelectorError
	if errors.As(err, &selectorErr) {
		return failSelector(stderr, selectorErr.err, jsonOut, selectorErr.boundary, selectorErr.selector)
	}
	return failJSON(stderr, err, jsonOut, boundary, selector)
}

// remoteToolParams checks --params-json locally, as offline, and sends it as given.
func remoteToolParams(raw string) (json.RawMessage, error) {
	if _, err := parseArchonToolParametersJSON(raw); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func remoteToolCreate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("tool create", stderr)
	profileID := fs.String("profile-id", "", "exact host Tool profile id")
	profileVersion := fs.String("profile-version", "", "exact host Tool profile version")
	title := fs.String("title", "", "Tool title")
	paramsJSON := fs.String("params-json", "", "complete Tool parameter JSON object")
	x := fs.Int("x", 0, "exact layout x coordinate")
	y := fs.Int("y", 0, "exact layout y coordinate")
	predecessorNodeID := fs.String("predecessor-node-id", "", "exact predecessor node id placement hint")
	successorNodeID := fs.String("successor-node-id", "", "exact successor node id placement hint")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon tool create <board> --profile-id <id> --profile-version <version> --title <title> --params-json <object> [--x n --y n|--predecessor-node-id <id>|--successor-node-id <id>] [--json]")
		return 2
	}
	selector := archonToolProfileSelector(*profileID, *profileVersion)
	fields := map[string]any{"profileId": *profileID, "profileVersion": *profileVersion, "title": *title}
	if archonToolFlagPresent(fs, "params-json") {
		params, err := remoteToolParams(*paramsJSON)
		if err != nil {
			return failJSON(stderr, err, *jsonOut, "tool", selector)
		}
		fields["params"] = params
	}
	placement, err := archonToolPlacementFromFlags(fs, *x, *y, *predecessorNodeID, *successorNodeID)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "tool", selector)
	}
	// The daemon validates the placement union, as the offline store does.
	hints := map[string]any{}
	if placement.X != nil {
		hints["x"] = *placement.X
	}
	if placement.Y != nil {
		hints["y"] = *placement.Y
	}
	if placement.PredecessorNodeID != "" {
		hints["predecessorNodeId"] = placement.PredecessorNodeID
	}
	if placement.SuccessorNodeID != "" {
		hints["successorNodeId"] = placement.SuccessorNodeID
	}
	fields["placement"] = hints
	data, err := c.patchTool(fs.Arg(0), *updatedBy, func(*formations.BoardDocument) (string, map[string]any, error) {
		return "createTool", fields, nil
	})
	if err != nil {
		return remoteToolFail(stderr, err, *jsonOut, "tool", selector)
	}
	result, err := decodeRemote[formations.ToolCreateResult](data, "")
	if err != nil || result.Board == nil {
		return fail(stderr, fmt.Errorf("coordinator response has no created Tool"))
	}
	return writeToolResult(stdout, *jsonOut, result, result.Board, result.Layout, "created", result.Tool.ID)
}

func remoteToolUpdate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("tool update", stderr)
	title := fs.String("title", "", "replacement Tool title")
	paramsJSON := fs.String("params-json", "", "complete replacement Tool parameter JSON object")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: archon tool update <board> <tool> [--title <title>] [--params-json <object>] [--json]")
		return 2
	}
	fields := map[string]any{}
	if archonToolFlagPresent(fs, "title") {
		fields["title"] = *title
	}
	if archonToolFlagPresent(fs, "params-json") {
		params, err := remoteToolParams(*paramsJSON)
		if err != nil {
			return failJSON(stderr, err, *jsonOut, "tool", fs.Arg(1))
		}
		fields["params"] = params
	}
	data, err := c.patchTool(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		toolID, err := remoteSelect("tool", fs.Arg(1), resolveToolSelector, board)
		fields["id"] = toolID
		return "updateTool", fields, err
	})
	if err != nil {
		return remoteToolFail(stderr, err, *jsonOut, "tool", fs.Arg(1))
	}
	result, err := decodeRemote[formations.ToolUpdateResult](data, "")
	if err != nil || result.Board == nil {
		return fail(stderr, fmt.Errorf("coordinator response has no updated Tool"))
	}
	return writeToolResult(stdout, *jsonOut, result, result.Board, result.Layout, "updated", result.Tool.ID)
}

func remoteToolDelete(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("tool delete", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: archon tool delete <board> <tool> [--json]")
		return 2
	}
	data, err := c.patchTool(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		toolID, err := remoteSelect("tool", fs.Arg(1), resolveToolSelector, board)
		return "deleteTool", map[string]any{"id": toolID}, err
	})
	if err != nil {
		return remoteToolFail(stderr, err, *jsonOut, "tool", fs.Arg(1))
	}
	result, err := decodeRemote[formations.ToolDeleteResult](data, "")
	if err != nil || result.Board == nil {
		return fail(stderr, fmt.Errorf("coordinator response has no deleted Tool"))
	}
	return writeToolResult(stdout, *jsonOut, result, result.Board, result.Layout, "deleted", result.ToolID)
}

func remoteToolInspect(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("tool inspect", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: archon tool inspect <board> <tool> [--json]")
		return 2
	}
	board, err := c.readBoard(fs.Arg(0))
	if err != nil {
		return remoteToolFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	return writeToolInspect(stdout, stderr, board, fs.Arg(1), *jsonOut)
}
