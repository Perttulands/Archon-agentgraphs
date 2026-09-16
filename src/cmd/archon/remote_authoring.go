package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Authoring through --server translates each offline authoring command into the
// daemon's board, notes, layout and agent routes, so an agent's edits reach an
// open cockpit through its change polling. A command reads the document it
// changes, resolves selectors as the offline command does, and writes with that
// document's ETag (and board revision). When another editor wrote first, it
// reads again and retries. Output matches the offline command: unwrapped JSON
// without TOML, or the same line of text.
//
// A new board operation is one entry here: parse its flags, then patchBoard
// with a builder that resolves selectors and returns the operation.

type remoteAuthoringCommand func(c *remoteClient, args []string, stdout, stderr io.Writer) int

var remoteAuthoringCommands = map[string]remoteAuthoringCommand{
	"board new":            remoteBoardNew,
	"board notes":          remoteBoardNotes,
	"board note":           remoteBoardNote,
	"board validate":       remoteBoardValidate,
	"board arrange":        remoteBoardArrange,
	"mission create":       remoteMissionCreate,
	"mission update":       remoteMissionUpdate,
	"mission wire":         remoteMissionWire,
	"formation create":     remoteFormationCreate,
	"formation rename":     remoteFormationRename,
	"formation assign":     remoteFormationAssign,
	"formation unassign":   remoteFormationUnassign,
	"formation set-brief":  remoteFormationSetBrief,
	"formation add-input":  remoteFormationAddPort(formations.FormationPortInput),
	"formation add-output": remoteFormationAddPort(formations.FormationPortOutput),
	"formation wire":       remoteFormationWire(false),
	"formation unwire":     remoteFormationWire(true),
	"gate create":          remoteGateCreate,
	"gate update":          remoteGateUpdate,
	"gate judge":           remoteGateJudge,
	"agent new":            remoteAgentNew,
	"agent edit":           remoteAgentEdit,
}

// remoteWriteAttempts bounds retries after losing a write race.
const remoteWriteAttempts = 3

// remoteHTTPError is a coordinator error response. It unwraps to the matching
// formations error so JSON error codes match the offline command.
type remoteHTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e *remoteHTTPError) Error() string {
	return fmt.Sprintf("coordinator HTTP %d: %s", e.Status, e.Message)
}

func (e *remoteHTTPError) Unwrap() error {
	switch e.Code {
	case "NOT_FOUND":
		return formations.ErrNotFound
	case "CONFLICT":
		return formations.ErrConflict
	case "BOARD_EXISTS", "AGENT_EXISTS":
		return formations.ErrAlreadyExists
	case "AMBIGUOUS_SELECTOR":
		return formations.ErrAmbiguousSelector
	case "INVALID_GATE_KIND":
		return formations.ErrInvalidGateKind
	case formations.FindingInvalidCodeGateProfile:
		return formations.ErrInvalidCodeGateProfile
	case "INVALID_DEFINITION_SOURCE":
		return formations.ErrInvalidDefinitionSource
	case "PRECONDITION_REQUIRED":
		return formations.ErrPreconditionRequired
	case "UNSUPPORTED_SCHEMA":
		return formations.ErrUnsupportedSchema
	case formations.LegacyScriptGateMigrationCode:
		return formations.ErrLegacyScriptGateRequiresFencedMigration
	case formations.LegacyInlineVerificationMigrationCode:
		return formations.ErrLegacyInlineVerificationRequiresMigration
	}
	return nil
}

func isRemoteWriteRace(err error) bool {
	var httpErr *remoteHTTPError
	return errors.As(err, &httpErr) && httpErr.Status == 409 && httpErr.Code == "CONFLICT"
}

// call performs one request and returns the envelope's data and the ETag.
func (c *remoteClient) call(method, path string, value any, ifMatch string) (json.RawMessage, string, error) {
	status, raw, etag, err := c.send(method, path, value, ifMatch)
	if err != nil {
		return nil, "", err
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &envelope)
	if status >= 300 {
		message := envelope.Error.Message
		if decodeErr != nil || message == "" {
			message = strings.TrimSpace(string(raw))
		}
		return nil, "", &remoteHTTPError{Status: status, Code: envelope.Error.Code, Message: message}
	}
	if decodeErr != nil {
		return nil, "", fmt.Errorf("coordinator response: %w", decodeErr)
	}
	return envelope.Data, etag, nil
}

// remoteSelectorError marks a failed selector, reported like the offline one.
type remoteSelectorError struct {
	boundary string
	selector string
	err      error
}

func (e *remoteSelectorError) Error() string { return e.err.Error() }
func (e *remoteSelectorError) Unwrap() error { return e.err }

func remoteFail(stderr io.Writer, err error, jsonOut bool, boundary, selector string) int {
	var selectorErr *remoteSelectorError
	if errors.As(err, &selectorErr) {
		return failSelector(stderr, selectorErr.err, jsonOut, selectorErr.boundary, selectorErr.selector)
	}
	return failDefinitionWrite(stderr, err, jsonOut, boundary, selector)
}

func boardPath(selector string, rest ...string) string {
	path := "/api/formations/boards/" + url.PathEscape(selector)
	for _, part := range rest {
		path += "/" + part
	}
	return path
}

func decodeRemote[T any](data json.RawMessage, key string) (*T, error) {
	var value T
	if key == "" {
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("coordinator response: %w", err)
		}
		return &value, nil
	}
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(data, &wrapper); err != nil || wrapper[key] == nil {
		return nil, fmt.Errorf("coordinator response has no %s", key)
	}
	if err := json.Unmarshal(wrapper[key], &value); err != nil {
		return nil, fmt.Errorf("coordinator response: %w", err)
	}
	return &value, nil
}

func (c *remoteClient) readBoard(selector string) (*formations.BoardDocument, error) {
	data, _, err := c.call("GET", boardPath(selector), nil, "")
	if err != nil {
		return nil, &remoteSelectorError{boundary: "board", selector: selector, err: err}
	}
	return decodeRemote[formations.BoardDocument](data, "board")
}

// patchBoard applies one board operation. build resolves selectors against
// the board it is given and returns the operation's name and fields.
func (c *remoteClient) patchBoard(selector, updatedBy string, build func(*formations.BoardDocument) (string, map[string]any, error)) (json.RawMessage, *formations.BoardDocument, error) {
	for attempt := 1; ; attempt++ {
		board, err := c.readBoard(selector)
		if err != nil {
			return nil, nil, err
		}
		operation, fields, err := build(board)
		if err != nil {
			return nil, board, err
		}
		body := map[string]any{operation: fields, "expectedRev": board.Rev, "updatedBy": updatedBy}
		data, _, err := c.call("PATCH", boardPath(board.Slug), body, board.ETag)
		if isRemoteWriteRace(err) && attempt < remoteWriteAttempts {
			continue
		}
		return data, board, err
	}
}

// freePosition places a new node like the offline command: explicit
// coordinates stay, otherwise the first free grid position from x, y.
func (c *remoteClient) freePosition(board *formations.BoardDocument, fs *flag.FlagSet, x, y int) (int, int, error) {
	explicit := false
	fs.Visit(func(current *flag.Flag) { explicit = explicit || current.Name == "x" || current.Name == "y" })
	if explicit {
		return x, y, nil
	}
	data, _, err := c.call("GET", boardPath(board.Slug, "layout"), nil, "")
	if err != nil {
		return 0, 0, err
	}
	layout, err := decodeRemote[formations.LayoutDocument](data, "layout")
	if err != nil {
		return 0, 0, err
	}
	position, err := formations.FreeLayoutPosition(board, layout, x, y)
	return position.X, position.Y, err
}

func remoteFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func givenFlags(fs *flag.FlagSet) map[string]bool {
	given := map[string]bool{}
	fs.Visit(func(current *flag.Flag) { given[current.Name] = true })
	return given
}

func remoteSelect(boundary, selector string, resolve func(*formations.BoardDocument, string) (string, error), board *formations.BoardDocument) (string, error) {
	id, err := resolve(board, selector)
	if err != nil {
		return "", &remoteSelectorError{boundary: boundary, selector: selector, err: err}
	}
	return id, nil
}

// writeRemoteBoard prints a {"board": ...} response like the offline command.
func writeRemoteBoard(stdout, stderr io.Writer, data json.RawMessage, jsonOut bool, text string) int {
	board, err := decodeRemote[formations.BoardDocument](data, "board")
	if err != nil {
		return fail(stderr, err)
	}
	board.TOML = ""
	if jsonOut {
		return writeJSON(stdout, board)
	}
	fmt.Fprintln(stdout, text)
	return 0
}

func remoteBoardNew(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("board new", stderr)
	title := fs.String("title", "", "board title")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon board new <slug> [--title <title>] [--json]")
		return 2
	}
	data, _, err := c.call("POST", "/api/formations/boards", map[string]any{"slug": fs.Arg(0), "title": *title, "updatedBy": *updatedBy}, "")
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	board, err := decodeRemote[formations.BoardDocument](data, "board")
	if err != nil {
		return fail(stderr, err)
	}
	board.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, board)
	}
	fmt.Fprintf(stdout, "created %s\n", board.Slug)
	return 0
}

func remoteBoardNotes(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("board notes", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon board notes <board> [--json]")
		return 2
	}
	board, err := c.readBoard(fs.Arg(0))
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	data, _, err := c.call("GET", boardPath(board.Slug, "notes"), nil, "")
	if err != nil {
		return fail(stderr, err)
	}
	notes, err := decodeRemote[formations.BoardNotesDocument](data, "notes")
	if err != nil {
		return fail(stderr, err)
	}
	if *jsonOut {
		return writeJSON(stdout, notes)
	}
	if notes.Board == "" && len(notes.Elements) == 0 {
		fmt.Fprintf(stdout, "%s\tno notes\n", board.Slug)
		return 0
	}
	if notes.Board != "" {
		fmt.Fprintf(stdout, "[board]\n%s\n", notes.Board)
	}
	for _, note := range notes.Elements {
		fmt.Fprintf(stdout, "\n[%s]\n%s\n", note.NodeID, note.Text)
	}
	return 0
}

func remoteBoardNote(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("board note", stderr)
	text := fs.String("text", "", "note text")
	file := fs.String("file", "", "read note text from file")
	node := fs.String("node", "", "element id; omit for the board note")
	clear := fs.Bool("clear", false, "clear the selected note")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"clear": true, "json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 || boolCount(givenFlags(fs)["text"], *file != "", *clear) != 1 {
		fmt.Fprintln(stderr, "usage: archon board note <board> (--text <text> | --file <path> | --clear) [--node <element-id>] [--json]")
		return 2
	}
	value := *text
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			return fail(stderr, err)
		}
		value = string(raw)
	}
	if *clear {
		value = ""
	}
	target := strings.TrimSpace(*node)
	if target == "" {
		target = formations.BoardNoteTarget
	}
	board, err := c.readBoard(fs.Arg(0))
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	var data json.RawMessage
	for attempt := 1; ; attempt++ {
		_, etag, readErr := c.call("GET", boardPath(board.Slug, "notes"), nil, "")
		if readErr != nil {
			return fail(stderr, readErr)
		}
		data, _, err = c.call("PATCH", boardPath(board.Slug, "notes"), formations.BoardNotePatch{Target: target, Text: value, UpdatedBy: *updatedBy}, etag)
		if !isRemoteWriteRace(err) || attempt == remoteWriteAttempts {
			break
		}
	}
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	updated, err := decodeRemote[formations.BoardNotesDocument](data, "notes")
	if err != nil {
		return fail(stderr, err)
	}
	if *jsonOut {
		return writeJSON(stdout, updated)
	}
	fmt.Fprintf(stdout, "updated %s note on %s (notes rev %d)\n", target, board.Slug, updated.Rev)
	return 0
}

func remoteBoardValidate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("board validate", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon board validate <board> [--json]")
		return 2
	}
	board, err := c.readBoard(fs.Arg(0))
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	slug := board.Slug
	if slug == "" {
		slug = fs.Arg(0)
	}
	data, _, err := c.call("GET", boardPath(slug, "validation"), nil, "")
	if err != nil {
		return fail(stderr, err)
	}
	report, err := decodeRemote[struct {
		Errors   []formations.BoardFinding `json:"errors"`
		Warnings []formations.BoardFinding `json:"warnings"`
	}](data, "")
	if err != nil {
		return fail(stderr, err)
	}
	if *jsonOut {
		if code := writeJSON(stdout, map[string]interface{}{
			"board":    identityFromBoard(board),
			"errors":   report.Errors,
			"warnings": report.Warnings,
		}); code != 0 {
			return code
		}
	} else {
		fmt.Fprintf(stdout, "%s\t%d errors\t%d warnings\n", slug, len(report.Errors), len(report.Warnings))
		writeFindingsText(stdout, "ERROR", report.Errors)
		writeFindingsText(stdout, "WARN", report.Warnings)
	}
	if len(report.Errors) > 0 {
		return 1
	}
	return 0
}

func remoteBoardArrange(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("board arrange", stderr)
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon board arrange <board> [--json]")
		return 2
	}
	board, err := c.readBoard(fs.Arg(0))
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	var data json.RawMessage
	for attempt := 1; ; attempt++ {
		_, etag, readErr := c.call("GET", boardPath(board.Slug, "layout"), nil, "")
		if readErr != nil {
			return fail(stderr, readErr)
		}
		data, _, err = c.call("PATCH", boardPath(board.Slug, "layout"), map[string]any{"arrange": true}, etag)
		if !isRemoteWriteRace(err) || attempt == remoteWriteAttempts {
			break
		}
	}
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	layout, err := decodeRemote[formations.LayoutDocument](data, "layout")
	if err != nil {
		return fail(stderr, err)
	}
	layout.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, layout)
	}
	fmt.Fprintf(stdout, "arranged %s\n", board.Slug)
	return 0
}

func remoteMissionCreate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("mission create", stderr)
	title := fs.String("title", "", "mission title")
	goal := fs.String("goal", "", "mission goal")
	beadID := fs.String("bead", "", "project Beads id")
	x := fs.Int("x", 0, "layout x coordinate")
	y := fs.Int("y", 0, "layout y coordinate")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon mission create <board> [--title <title>] [--goal <goal>] [--bead <beads-id>] [--x n] [--y n] [--json]")
		return 2
	}
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		createX, createY, err := c.freePosition(board, fs, *x, *y)
		return "createMission", map[string]any{"title": *title, "goal": *goal, "beadId": *beadID, "x": createX, "y": createY}, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, "created mission")
}

func remoteMissionUpdate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("mission update", stderr)
	title := fs.String("title", "", "mission title")
	goal := fs.String("goal", "", "mission goal")
	beadID := fs.String("bead", "", "project Beads id")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	given := givenFlags(fs)
	if fs.NArg() != 2 || !given["title"] && !given["goal"] && !given["bead"] {
		fmt.Fprintln(stderr, "usage: archon mission update <board> <mission> [--title text] [--goal text] [--bead beads-id] [--json]")
		fmt.Fprintln(stderr, "Only the flags you give change the mission; an empty value clears that field.")
		return 2
	}
	missionID := ""
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		id, err := remoteSelect("mission", fs.Arg(1), resolveMissionSelector, board)
		missionID = id
		fields := map[string]any{"id": id}
		for flagName, field := range map[string]struct {
			key   string
			value *string
		}{"title": {"title", title}, "goal": {"goal", goal}, "bead": {"beadId", beadID}} {
			if given[flagName] {
				fields[field.key] = *field.value
			}
		}
		return "updateMission", fields, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(1))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("updated mission %s", missionID))
}

func remoteMissionWire(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("mission wire", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 3 {
		fmt.Fprintln(stderr, "usage: archon mission wire <board> <mission> <to-node:port> [--json]")
		return 2
	}
	missionID := ""
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		id, err := remoteSelect("mission", fs.Arg(1), resolveMissionSelector, board)
		missionID = id
		return "wireConnection", map[string]any{"from": id + ":out", "to": fs.Arg(2)}, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("wired mission %s -> %s", missionID, fs.Arg(2)))
}

func remoteFormationCreate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("formation create", stderr)
	title := fs.String("title", "", "formation title")
	x := fs.Int("x", 120, "layout x")
	y := fs.Int("y", 120, "layout y")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fmt.Fprintln(stderr, "usage: archon formation create <board> [solo|peer|orchestrated] [--title <title>] [--json]")
		return 2
	}
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		createX, createY, err := c.freePosition(board, fs, *x, *y)
		return "createFormation", map[string]any{"type": fs.Arg(1), "title": *title, "x": createX, "y": createY}, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	result, err := decodeRemote[formations.FormationCreateResult](data, "")
	if err != nil || result.Board == nil || result.Layout == nil {
		return fail(stderr, fmt.Errorf("coordinator response has no created formation"))
	}
	result.Board.TOML = ""
	result.Layout.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "created %s\n", result.Formation.ID)
	return 0
}

// patchFormation applies an operation to one formation selected by title or ID.
func (c *remoteClient) patchFormation(boardSelector, formationSelector, updatedBy string, build func(formationID string) (string, map[string]any)) (json.RawMessage, string, error) {
	formationID := ""
	data, _, err := c.patchBoard(boardSelector, updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		id, err := remoteSelect("formation", formationSelector, resolveFormationSelector, board)
		if err != nil {
			return "", nil, err
		}
		formationID = id
		operation, fields := build(id)
		return operation, fields, nil
	})
	return data, formationID, err
}

func remoteFormationRename(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("formation rename", stderr)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 3 {
		fmt.Fprintln(stderr, "usage: archon formation rename <board> <formation> <title> [--json]")
		fmt.Fprintln(stderr, "An empty title clears it. The ID, ports, slots, brief, edges, layout and notes stay unchanged.")
		return 2
	}
	data, formationID, err := c.patchFormation(fs.Arg(0), fs.Arg(1), *updatedBy, func(id string) (string, map[string]any) {
		return "updateFormation", map[string]any{"id": id, "title": fs.Arg(2)}
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("renamed %s", formationID))
}

func remoteFormationAssign(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("formation assign", stderr)
	slotID := fs.String("slot", "", "slot id")
	agentID := fs.String("agent", "", "persona id")
	harness := fs.String("harness", "", "harness variant")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 || *slotID == "" || *agentID == "" {
		fmt.Fprintln(stderr, "usage: archon formation assign <board> <formation> --slot <slot> --agent <agent> [--harness <h>] [--json]")
		return 2
	}
	data, _, err := c.patchFormation(fs.Arg(0), fs.Arg(1), *updatedBy, func(id string) (string, map[string]any) {
		return "assignSlot", map[string]any{"formationId": id, "slotId": *slotID, "agentId": *agentID, "harness": *harness}
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("assigned %s to %s", *agentID, *slotID))
}

func remoteFormationUnassign(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("formation unassign", stderr)
	slotID := fs.String("slot", "", "slot id")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 || *slotID == "" {
		fmt.Fprintln(stderr, "usage: archon formation unassign <board> <formation> --slot <slot> [--json]")
		return 2
	}
	data, formationID, err := c.patchFormation(fs.Arg(0), fs.Arg(1), *updatedBy, func(id string) (string, map[string]any) {
		return "assignSlot", map[string]any{"formationId": id, "slotId": *slotID}
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("unassigned %s from %s", *slotID, formationID))
}

func remoteFormationSetBrief(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("formation set-brief", stderr)
	goal := fs.String("goal", "", "brief goal")
	beadID := fs.String("bead", "", "project Beads id")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	var files stringList
	var links stringList
	fs.Var(&files, "file", "file reference")
	fs.Var(&links, "link", "link reference")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(stderr, "usage: archon formation set-brief <board> <formation> --goal <goal> [--bead <beads-id>] [--file <path>] [--link <url>] [--json]")
		return 2
	}
	data, formationID, err := c.patchFormation(fs.Arg(0), fs.Arg(1), *updatedBy, func(id string) (string, map[string]any) {
		return "setBrief", map[string]any{"formationId": id, "goal": *goal, "beadId": *beadID, "files": []string(files), "links": []string(links)}
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "formation", fs.Arg(1))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("updated brief for %s", formationID))
}

func remoteFormationAddPort(direction string) remoteAuthoringCommand {
	return func(c *remoteClient, args []string, stdout, stderr io.Writer) int {
		name := "formation add-input"
		if direction == formations.FormationPortOutput {
			name = "formation add-output"
		}
		fs := remoteFlags(name, stderr)
		label := fs.String("label", "", "port label")
		updatedBy := fs.String("updated-by", "agent:archon", "update actor")
		jsonOut := fs.Bool("json", false, "write JSON")
		if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
			return 2
		}
		if fs.NArg() != 2 {
			fmt.Fprintf(stderr, "usage: archon %s <board> <formation> --label <label> [--json]\n", name)
			return 2
		}
		data, formationID, err := c.patchFormation(fs.Arg(0), fs.Arg(1), *updatedBy, func(id string) (string, map[string]any) {
			return "addPort", map[string]any{"formationId": id, "direction": direction, "label": *label}
		})
		if err != nil {
			return remoteFail(stderr, err, *jsonOut, "formation", fs.Arg(1))
		}
		return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("added %s to %s", direction, formationID))
	}
}

func remoteFormationWire(remove bool) remoteAuthoringCommand {
	return func(c *remoteClient, args []string, stdout, stderr io.Writer) int {
		name, operation := "formation wire", "wireConnection"
		if remove {
			name, operation = "formation unwire", "unwireConnection"
		}
		fs := remoteFlags(name, stderr)
		updatedBy := fs.String("updated-by", "agent:archon", "update actor")
		jsonOut := fs.Bool("json", false, "write JSON")
		if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
			return 2
		}
		if fs.NArg() != 3 {
			fmt.Fprintf(stderr, "usage: archon %s <board> <from-node:port> <to-node:port> [--json]\n", name)
			return 2
		}
		data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(*formations.BoardDocument) (string, map[string]any, error) {
			return operation, map[string]any{"from": fs.Arg(1), "to": fs.Arg(2)}, nil
		})
		if err != nil {
			return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
		}
		text := fmt.Sprintf("wired %s -> %s", fs.Arg(1), fs.Arg(2))
		if remove {
			text = fmt.Sprintf("removed connection %s -> %s", fs.Arg(1), fs.Arg(2))
		}
		return writeRemoteBoard(stdout, stderr, data, *jsonOut, text)
	}
}

// legacyGateCommandFields forwards retired command flags so the daemon returns
// the same migration error as the offline command.
func legacyGateCommandFields(fs *flag.FlagSet, fields map[string]any, command, argv, cwd, shell string) {
	given := givenFlags(fs)
	for flagName, field := range map[string]struct {
		key   string
		value any
	}{"command": {"command", command}, "command-argv": {"commandArgv", splitCSV(argv)}, "command-cwd": {"commandCwd", cwd}, "command-shell": {"commandShell", shell}} {
		if given[flagName] {
			fields[field.key] = field.value
		}
	}
}

func remoteGateCreate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("gate create", stderr)
	title := fs.String("title", "Review gate", "gate title")
	kinds := fs.String("kinds", "code", "comma-separated gate kinds")
	criterion := fs.String("criterion", "", "gate criterion")
	check := fs.String("check", "", "registered code Gate profile id")
	checkVersion := fs.String("check-version", "", "exact code Gate profile version")
	checkValue := fs.String("check-value", "", "code Gate profile value parameter")
	command := fs.String("command", "", "retired legacy Gate field; new writes fail with a migration error")
	commandArgv := fs.String("command-argv", "", "retired legacy Gate argv; new writes fail with a migration error")
	commandCWD := fs.String("command-cwd", "", "retired legacy Gate cwd; new writes fail with a migration error")
	commandShell := fs.String("command-shell", "", "retired legacy Gate shell command; new writes fail with a migration error")
	x := fs.Int("x", 0, "layout x coordinate")
	y := fs.Int("y", 0, "layout y coordinate")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon gate create <board> [--kinds code,formation,human] [--title text] [--criterion text] [--check id --check-version version --check-value value] [--x n] [--y n] [--json]")
		return 2
	}
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		createX, createY, err := c.freePosition(board, fs, *x, *y)
		fields := map[string]any{"title": *title, "kinds": splitCSV(*kinds), "criterion": *criterion, "check": *check, "checkVersion": *checkVersion, "checkValue": *checkValue, "x": createX, "y": createY}
		legacyGateCommandFields(fs, fields, *command, *commandArgv, *commandCWD, *commandShell)
		return "createGate", fields, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, "created gate")
}

func remoteGateUpdate(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("gate update", stderr)
	title := fs.String("title", "", "gate title")
	kinds := fs.String("kinds", "", "comma-separated gate kinds")
	criterion := fs.String("criterion", "", "gate criterion")
	check := fs.String("check", "", "registered code Gate profile id")
	checkVersion := fs.String("check-version", "", "exact code Gate profile version")
	checkValue := fs.String("check-value", "", "code Gate profile value parameter")
	command := fs.String("command", "", "retired legacy Gate field; new writes fail with a migration error")
	commandArgv := fs.String("command-argv", "", "retired legacy Gate argv; new writes fail with a migration error")
	commandCWD := fs.String("command-cwd", "", "retired legacy Gate cwd; new writes fail with a migration error")
	commandShell := fs.String("command-shell", "", "retired legacy Gate shell command; new writes fail with a migration error")
	clearCheck := fs.Bool("clear-check", false, "clear the code check profile, version and value")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "clear-check": true})); err != nil {
		return 2
	}
	given := givenFlags(fs)
	if fs.NArg() != 2 || *clearCheck && (given["check"] || given["check-version"] || given["check-value"]) {
		fmt.Fprintln(stderr, "usage: archon gate update <board> <gate> [--title text] [--kinds code,formation,human] [--criterion text] [--check id] [--check-version version] [--check-value value | --clear-check] [--json]")
		fmt.Fprintln(stderr, "Only the flags you give change the gate; an empty value clears that field. Dropping formation detaches the judge chain and dropping code clears the check.")
		return 2
	}
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		gateID, err := remoteSelect("gate", fs.Arg(1), resolveGateSelector, board)
		fields := map[string]any{"id": gateID}
		if given["kinds"] {
			fields["kinds"] = append([]string{}, splitCSV(*kinds)...)
		}
		for flagName, field := range map[string]struct {
			key   string
			value *string
		}{"title": {"title", title}, "criterion": {"criterion", criterion}, "check": {"check", check}, "check-version": {"checkVersion", checkVersion}, "check-value": {"checkValue", checkValue}} {
			if given[flagName] {
				fields[field.key] = *field.value
			}
		}
		if *clearCheck {
			fields["check"], fields["checkVersion"], fields["checkValue"] = "", "", ""
		}
		legacyGateCommandFields(fs, fields, *command, *commandArgv, *commandCWD, *commandShell)
		return "updateGate", fields, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, "updated gate")
}

func remoteGateJudge(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("gate judge", stderr)
	chain := fs.String("chain", "", "comma-separated formation chain")
	detach := fs.Bool("detach", false, "detach judge")
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true, "detach": true})); err != nil {
		return 2
	}
	if fs.NArg() != 2 || (!*detach && *chain == "") {
		fmt.Fprintln(stderr, "usage: archon gate judge <board> <gate> --chain f1,f2 | --detach [--json]")
		return 2
	}
	gateID := ""
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		id, err := remoteSelect("gate", fs.Arg(1), resolveGateSelector, board)
		gateID = id
		if *detach {
			return "detachGateJudge", map[string]any{"gateId": id}, err
		}
		return "setGateJudge", map[string]any{"gateId": id, "chain": splitCSV(*chain)}, err
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "board", fs.Arg(0))
	}
	text := fmt.Sprintf("updated judge for %s", gateID)
	if *detach {
		text = fmt.Sprintf("detached judge from %s", gateID)
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, text)
}

func writeRemoteAgent(stdout, stderr io.Writer, data json.RawMessage, jsonOut bool, verb string) int {
	card, err := decodeRemote[formations.PersonaCard](data, "")
	if err != nil {
		return fail(stderr, err)
	}
	card.TOML = ""
	if jsonOut {
		return writeJSON(stdout, card)
	}
	fmt.Fprintf(stdout, "%s %s\n", verb, card.ID)
	return 0
}

// Agent cards are the daemon's (its --agents-dir), and --from names a path on
// the daemon host.
func remoteAgentNew(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("agent new", stderr)
	kind := fs.String("kind", "", "agent kind")
	harness := fs.String("harness", "", "default harness")
	capable := fs.String("capable", "", "comma-separated bare capabilities")
	personality := fs.String("personality", "", "personality facet")
	from := fs.String("from", "", "source config path")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon agent new <id> [--kind <kind>] [--harness <h>] [--from <path>]")
		return 2
	}
	data, _, err := c.call("POST", "/api/agents", map[string]any{"id": fs.Arg(0), "kind": *kind, "harness": *harness, "capabilities": splitCSV(*capable), "personality": *personality, "source": *from}, "")
	if err != nil {
		return fail(stderr, err)
	}
	return writeRemoteAgent(stdout, stderr, data, *jsonOut, "created")
}

func remoteAgentEdit(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("agent edit", stderr)
	addCapability := fs.String("add-capability", "", "add bare capability")
	removeCapability := fs.String("remove-capability", "", "remove bare capability")
	addHarness := fs.String("add-harness", "", "add harness variant")
	sessionStem := fs.String("session-stem", "", "session stem for added harness")
	launch := fs.String("launch", "", "default or added-harness launch command")
	displayName := fs.String("display-name", "", "replace display name")
	kind := fs.String("kind", "", "replace role kind")
	summary := fs.String("summary", "", "replace summary")
	capable := fs.String("capable", "", "replace comma-separated bare capabilities")
	note := fs.String("note", "", "append note")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: archon agent edit <id> [--display-name n] [--kind k] [--summary s] [--capable a,b] [--session-stem s] [--launch command] [--add-capability t|--remove-capability t|--add-harness h|--note text]")
		return 2
	}
	given := givenFlags(fs)
	body := map[string]any{"addCapability": *addCapability, "removeCapability": *removeCapability, "addHarness": *addHarness, "note": *note}
	for flagName, field := range map[string]struct {
		key   string
		value *string
	}{"display-name": {"displayName", displayName}, "kind": {"kind", kind}, "summary": {"summary", summary}} {
		if given[flagName] {
			body[field.key] = *field.value
		}
	}
	if given["capable"] {
		body["capabilities"] = append([]string{}, splitCSV(*capable)...)
	}
	if *addHarness != "" || given["session-stem"] {
		body["sessionStem"] = *sessionStem
	}
	if *addHarness != "" || given["launch"] {
		body["launch"] = *launch
	}
	path := "/api/agents/" + url.PathEscape(fs.Arg(0))
	var data json.RawMessage
	var err error
	for attempt := 1; ; attempt++ {
		_, etag, readErr := c.call("GET", path, nil, "")
		if readErr != nil {
			return fail(stderr, readErr)
		}
		data, _, err = c.call("PATCH", path, body, etag)
		if !isRemoteWriteRace(err) || attempt == remoteWriteAttempts {
			break
		}
	}
	if err != nil {
		return fail(stderr, err)
	}
	return writeRemoteAgent(stdout, stderr, data, *jsonOut, "updated")
}
