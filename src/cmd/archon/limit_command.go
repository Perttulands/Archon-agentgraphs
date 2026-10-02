package main

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// archon limit create|update|delete (archon-o7p.8). A Limit card covers a step,
// or the Input card for the whole mission, and caps its rounds, time or tokens.
// A run has no limits unless a card sets one.

const (
	limitTargetHelp = "the step it covers by ID or title, or input (the Input card) for the whole mission"
	limitRoundsHelp = "how many times the step may run, send-backs included (a peer step: its journal messages), or how many steps the whole mission may run"
	limitTimeHelp   = "how long the step, or every step of the mission, may work in whole seconds, such as 45s, 30m or 1h30m; waiting on a human gate does not count"
	limitWarnHelp   = "how much time is left when the covered seats are warned, such as 5m"
	limitTokensHelp = "how many tokens the step, or every step of the mission, may spend, approximately: input not read from the cache, cache writes included, plus output, subagents included"
)

func runLimitCommand(store *formations.Store, verb string, args []string, stdout, stderr io.Writer) int {
	switch verb {
	case "create":
		return runLimitCreate(store, args, stdout, stderr)
	case "update":
		return runLimitUpdate(store, args, stdout, stderr)
	case "delete":
		return runLimitDelete(store, args, stdout, stderr)
	default:
		return unknownCommand(stderr, "limit", verb)
	}
}

// limitFlags are the knobs create and update share. Each knob is a string so
// an empty value can clear it.
type limitFlags struct {
	fs        *flag.FlagSet
	target    *string
	rounds    *string
	time      *string
	warn      *string
	tokens    *string
	title     *string
	updatedBy *string
	jsonOut   *bool
}

func newLimitFlags(name string, stderr io.Writer) limitFlags {
	fs := commandFlags(name, stderr)
	return limitFlags{
		fs:        fs,
		target:    fs.String("target", "", limitTargetHelp),
		rounds:    fs.String("rounds", "", limitRoundsHelp),
		time:      fs.String("time", "", limitTimeHelp),
		warn:      fs.String("warn", "", limitWarnHelp),
		tokens:    fs.String("tokens", "", limitTokensHelp),
		title:     fs.String("title", "", "Limit card title (default Limit)"),
		updatedBy: fs.String("updated-by", "agent:archon", "update actor"),
		jsonOut:   fs.Bool("json", false, "write JSON"),
	}
}

// parseLimitCount reads --rounds or --tokens: empty clears (0), otherwise a
// positive whole number.
func parseLimitCount(flag, value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count <= 0 {
		return 0, fmt.Errorf("%w: --%s %q must be a positive whole number", formations.ErrInvalidLimit, flag, value)
	}
	return count, nil
}

// parseLimitSeconds reads --time or --warn: empty clears (0), otherwise a
// positive duration in whole seconds.
func parseLimitSeconds(flag, value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 || duration%time.Second != 0 {
		return 0, fmt.Errorf("%w: --%s %q must be a positive whole number of seconds, such as 45s, 30m or 1h30m", formations.ErrInvalidLimit, flag, value)
	}
	return int(duration / time.Second), nil
}

// limitKnobValues are the knobs the flags give; 0 is unset.
type limitKnobValues struct {
	rounds, seconds, warn, tokens int
}

// limitKnobs reads the knobs from the flags.
func limitKnobs(flags limitFlags) (knobs limitKnobValues, err error) {
	if knobs.rounds, err = parseLimitCount("rounds", *flags.rounds); err != nil {
		return
	}
	if knobs.seconds, err = parseLimitSeconds("time", *flags.time); err != nil {
		return
	}
	if knobs.warn, err = parseLimitSeconds("warn", *flags.warn); err != nil {
		return
	}
	knobs.tokens, err = parseLimitCount("tokens", *flags.tokens)
	return
}

// resolveLimitTarget names the step or Input card a card covers, by ID or
// title, or the mission's one Input card as "input"; an empty selector
// unwires it.
func resolveLimitTarget(board *formations.BoardDocument, selector string) (string, error) {
	if strings.TrimSpace(selector) == "" {
		return "", nil
	}
	if strings.EqualFold(strings.TrimSpace(selector), "input") && len(board.Missions) == 1 {
		return board.Missions[0].ID, nil
	}
	candidates := make([]graphSelectorCandidate, 0, len(board.Formations)+len(board.Missions))
	for _, formation := range board.Formations {
		candidates = append(candidates, graphSelectorCandidate{ID: formation.ID, Title: formation.Title})
	}
	for _, mission := range board.Missions {
		candidates = append(candidates, graphSelectorCandidate{ID: mission.ID, Title: mission.Title})
	}
	return resolveGraphSelector("step or Input card", selector, candidates)
}

func resolveLimitSelector(board *formations.BoardDocument, selector string) (string, error) {
	candidates := make([]graphSelectorCandidate, 0, len(board.Limits))
	for _, limit := range board.Limits {
		candidates = append(candidates, graphSelectorCandidate{ID: limit.ID, Title: limit.Title})
	}
	return resolveGraphSelector("limit", selector, candidates)
}

func runLimitCreate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	flags := newLimitFlags("limit create", stderr)
	x := flags.fs.Int("x", 0, "layout x coordinate")
	y := flags.fs.Int("y", 0, "layout y coordinate")
	if err := flags.fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if flags.fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("limit create"))
		return 2
	}
	knobs, err := limitKnobs(flags)
	if err != nil {
		return failJSON(stderr, err, *flags.jsonOut, "limit", "")
	}
	slug, err := store.ResolveBoardSelector(flags.fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	target, err := resolveLimitTarget(board, *flags.target)
	if err != nil {
		return failSelector(stderr, err, *flags.jsonOut, "step or Input card", *flags.target)
	}
	createX, createY, err := resolveCreateCoordinates(store, slug, flags.fs, *x, *y)
	if err != nil {
		return failDefinitionWrite(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	result, err := store.CreateLimit(slug, formations.LimitCreateRequest{
		Title: *flags.title, Target: target, Rounds: knobs.rounds, Seconds: knobs.seconds, WarnSeconds: knobs.warn, Tokens: knobs.tokens, X: createX, Y: createY, UpdatedBy: *flags.updatedBy,
	}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	return writeCreated(stdout, *flags.jsonOut, result, result.Board, result.Layout, result.Limit.ID)
}

func runLimitUpdate(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	flags := newLimitFlags("limit update", stderr)
	if err := flags.fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if flags.fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("limit update"))
		return 2
	}
	if _, err := limitKnobs(flags); err != nil {
		return failJSON(stderr, err, *flags.jsonOut, "limit", flags.fs.Arg(1))
	}
	slug, err := store.ResolveBoardSelector(flags.fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	board, err := store.ReadBoard(slug)
	if err != nil {
		return failJSON(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	limitID, err := resolveLimitSelector(board, flags.fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *flags.jsonOut, "limit", flags.fs.Arg(1))
	}
	update, err := limitUpdate(board, flags)
	if err != nil {
		return failJSON(stderr, err, *flags.jsonOut, "limit", flags.fs.Arg(1))
	}
	update.LimitID = limitID
	result, err := store.UpdateLimit(slug, update, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	result.TOML = ""
	if *flags.jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintln(stdout, "updated Limit card")
	return 0
}

// limitUpdate builds the change the given flags ask for.
func limitUpdate(board *formations.BoardDocument, flags limitFlags) (formations.LimitUpdateRequest, error) {
	given := givenFlags(flags.fs)
	update := formations.LimitUpdateRequest{UpdatedBy: *flags.updatedBy}
	if given["title"] {
		update.Title = flags.title
	}
	if given["target"] {
		target, err := resolveLimitTarget(board, *flags.target)
		if err != nil {
			return update, err
		}
		update.Target = &target
	}
	knobs, err := limitKnobs(flags)
	if err != nil {
		return update, err
	}
	if given["rounds"] {
		update.Rounds = &knobs.rounds
	}
	if given["time"] {
		update.Seconds = &knobs.seconds
	}
	if given["warn"] {
		update.WarnSeconds = &knobs.warn
	}
	if given["tokens"] {
		update.Tokens = &knobs.tokens
	}
	return update, nil
}

func runLimitDelete(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("limit delete", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("limit delete"))
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
	limitID, err := resolveLimitSelector(board, fs.Arg(1))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "limit", fs.Arg(1))
	}
	result, err := store.DeleteLimit(slug, formations.LimitDeleteRequest{ID: limitID, UpdatedBy: *updatedBy}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	return writeLimitDeleted(stdout, *jsonOut, result)
}

func writeLimitDeleted(stdout io.Writer, jsonOut bool, result *formations.LimitDeleteResult) int {
	if result.Board != nil {
		result.Board.TOML = ""
	}
	if result.Layout != nil {
		result.Layout.TOML = ""
	}
	if jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "deleted Limit card %s\n", result.LimitID)
	return 0
}

func remoteLimitCreate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	flags := newLimitFlags("limit create", stderr)
	x := flags.fs.Int("x", 0, "layout x coordinate")
	y := flags.fs.Int("y", 0, "layout y coordinate")
	if err := flags.fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if flags.fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("limit create"))
		return 2
	}
	knobs, err := limitKnobs(flags)
	if err != nil {
		return failJSON(stderr, err, *flags.jsonOut, "limit", "")
	}
	data, _, err := c.patchBoard(flags.fs.Arg(0), *flags.updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		target, err := remoteSelect("step or Input card", *flags.target, resolveLimitTarget, board)
		if err != nil {
			return "", nil, err
		}
		createX, createY, err := c.freePosition(board, flags.fs, *x, *y)
		return "createLimit", map[string]any{"title": *flags.title, "target": target, "rounds": knobs.rounds, "seconds": knobs.seconds, "warnSeconds": knobs.warn, "tokens": knobs.tokens, "x": createX, "y": createY}, err
	})
	if err != nil {
		return remoteFail(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	result, err := decodeRemote[formations.LimitCreateResult](data, "")
	if err != nil || result.Board == nil || result.Layout == nil {
		return fail(stderr, fmt.Errorf("coordinator response has no created Limit card"))
	}
	return writeCreated(stdout, *flags.jsonOut, result, result.Board, result.Layout, result.Limit.ID)
}

func remoteLimitUpdate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	flags := newLimitFlags("limit update", stderr)
	if err := flags.fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if flags.fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("limit update"))
		return 2
	}
	if _, err := limitKnobs(flags); err != nil {
		return failJSON(stderr, err, *flags.jsonOut, "limit", flags.fs.Arg(1))
	}
	data, _, err := c.patchBoard(flags.fs.Arg(0), *flags.updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		limitID, err := remoteSelect("limit", flags.fs.Arg(1), resolveLimitSelector, board)
		if err != nil {
			return "", nil, err
		}
		update, err := limitUpdate(board, flags)
		if err != nil {
			return "", nil, err
		}
		fields := map[string]any{"id": limitID}
		if update.Title != nil {
			fields["title"] = *update.Title
		}
		if update.Target != nil {
			fields["target"] = *update.Target
		}
		if update.Rounds != nil {
			fields["rounds"] = *update.Rounds
		}
		if update.Seconds != nil {
			fields["seconds"] = *update.Seconds
		}
		if update.WarnSeconds != nil {
			fields["warnSeconds"] = *update.WarnSeconds
		}
		if update.Tokens != nil {
			fields["tokens"] = *update.Tokens
		}
		return "updateLimit", fields, nil
	})
	if err != nil {
		return remoteFail(stderr, err, *flags.jsonOut, "mission", flags.fs.Arg(0))
	}
	return writeRemoteBoard(stdout, stderr, data, *flags.jsonOut, "updated Limit card")
}

func remoteLimitDelete(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("limit delete", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, commandUsage("limit delete"))
		return 2
	}
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		limitID, err := remoteSelect("limit", fs.Arg(1), resolveLimitSelector, board)
		return "deleteLimit", map[string]any{"id": limitID}, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
	}
	result, err := decodeRemote[formations.LimitDeleteResult](data, "")
	if err != nil || result.Board == nil {
		return fail(stderr, fmt.Errorf("coordinator response has no deleted Limit card"))
	}
	return writeLimitDeleted(stdout, *jsonOut, result)
}
