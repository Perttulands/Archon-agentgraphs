package main

import (
	"fmt"
	"io"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func runMissionDelete(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission delete", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission delete"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result, err := store.DeleteBoard(slug, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeMissionDeleted(stdout, *jsonOut, result)
}

func remoteMissionDelete(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("mission delete", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("mission delete"))
		return 2
	}
	for attempt := 1; ; attempt++ {
		board, err := c.readBoard(fs.Arg(0))
		if err != nil {
			return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
		}
		data, _, err := c.call("DELETE", boardPath(board.Slug), map[string]any{"expectedRev": board.Rev}, board.ETag)
		if isRemoteWriteRace(err) && attempt < remoteWriteAttempts {
			continue
		}
		if err != nil {
			return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
		}
		result, err := decodeRemote[formations.BoardDeletion](data, "deletion")
		if err != nil {
			return fail(stderr, err)
		}
		return writeMissionDeleted(stdout, *jsonOut, result)
	}
}

func writeMissionDeleted(stdout io.Writer, jsonOut bool, result *formations.BoardDeletion) int {
	if jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "deleted mission %s\n", result.Slug)
	return 0
}

func resolveDeleteNode(board *formations.BoardDocument, noun, selector string) (string, error) {
	if noun == "formation" {
		return resolveFormationSelector(board, selector)
	}
	return resolveGateSelector(board, selector)
}

func runNodeDelete(store *formations.Store, noun string, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags(noun+" delete", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage(noun+" delete"))
		return 2
	}
	slug, err := store.ResolveBoardSelector(fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	id, err := resolveDeleteNode(board, noun, fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, noun, fs.Arg(1))
	}
	opts := formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev}
	if noun == "formation" {
		result, err := store.DeleteFormation(slug, formations.FormationDeleteRequest{ID: id, UpdatedBy: *updatedBy}, opts)
		if err != nil {
			return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
		}
		return writeNodeDeleted(stdout, *jsonOut, noun, id, result, result.Board, result.Layout)
	}
	result, err := store.DeleteGate(slug, formations.GateDeleteRequest{ID: id, UpdatedBy: *updatedBy}, opts)
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeNodeDeleted(stdout, *jsonOut, noun, id, result, result.Board, result.Layout)
}

func remoteNodeDelete(noun string) remoteAuthoringCommand {
	return func(c *remoteClient, args []string, stdout, stderr io.Writer) int {
		fs := remoteFlags(noun+" delete", stderr)
		updatedBy := fs.String("updated-by", "agent:archon", "update actor")
		jsonOut := fs.Bool("json", false, "write JSON")
		if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
			return 2
		}
		if fs.NArg() != 2 {
			fmt.Fprintln(stderr, commandUsage(noun+" delete"))
			return 2
		}
		id := ""
		data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
			var err error
			id, err = remoteSelect(noun, fs.Arg(1), func(b *formations.BoardDocument, selector string) (string, error) {
				return resolveDeleteNode(b, noun, selector)
			}, board)
			operation := "deleteGate"
			if noun == "formation" {
				operation = "deleteFormation"
			}
			return operation, map[string]any{"id": id}, err
		})
		if err != nil {
			return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
		}
		if noun == "formation" {
			result, err := decodeRemote[formations.FormationDeleteResult](data, "")
			if err != nil || result.Board == nil {
				return fail(stderr, fmt.Errorf("coordinator response has no deleted formation"))
			}
			return writeNodeDeleted(stdout, *jsonOut, noun, id, result, result.Board, result.Layout)
		}
		result, err := decodeRemote[formations.GateDeleteResult](data, "")
		if err != nil || result.Board == nil {
			return fail(stderr, fmt.Errorf("coordinator response has no deleted gate"))
		}
		return writeNodeDeleted(stdout, *jsonOut, noun, id, result, result.Board, result.Layout)
	}
}

func writeNodeDeleted(stdout io.Writer, jsonOut bool, noun, id string, result any, board *formations.BoardDocument, layout *formations.LayoutDocument) int {
	board.TOML = ""
	if layout != nil {
		layout.TOML = ""
	}
	if jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "deleted %s %s\n", noun, id)
	return 0
}
