package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

func executionFlags(args []string, stderr io.Writer) (*flag.FlagSet, *int, *string, *bool, error) {
	fs := remoteFlags("formation set-execution", stderr)
	seconds := fs.Int("timeout-seconds", -1, "total formation duration in seconds; zero inherits the run default")
	actor := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	err := fs.Parse(reorderFlags(args, map[string]bool{"json": true}))
	if err == nil && (fs.NArg() != 2 || *seconds < 0) {
		err = fmt.Errorf("usage: archon formation set-execution <board> <formation> --timeout-seconds <seconds|0> [--json]")
		fmt.Fprintln(stderr, err)
	}
	return fs, seconds, actor, jsonOut, err
}

func runFormationSetExecution(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs, seconds, actor, jsonOut, err := executionFlags(args, stderr)
	if err != nil {
		return 2
	}
	slug, board, id, err := resolveFormationCommandTarget(store, fs.Arg(0), fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result, err := store.SetFormationExecutionPolicy(slug, formations.FormationExecutionPolicyRequest{
		FormationID: id, TimeoutSeconds: *seconds, UpdatedBy: *actor,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "updated execution policy for %s\n", id)
	return 0
}

func remoteFormationSetExecution(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs, seconds, actor, jsonOut, err := executionFlags(args, stderr)
	if err != nil {
		return 2
	}
	data, id, err := c.patchFormation(fs.Arg(0), fs.Arg(1), *actor, func(id string) (string, map[string]any) {
		return "setExecution", map[string]any{"formationId": id, "timeoutSeconds": *seconds}
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("updated execution policy for %s", id))
}
