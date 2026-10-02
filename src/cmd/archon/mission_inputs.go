package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Mission inputs (archon-o7p.3): the named values a mission's runs supply,
// declared on its Input card and given at run start with --input.

// missionInputChange is what one mission input command asks for.
type missionInputChange struct {
	name        string
	kind        *string
	description *string
	required    *bool
	delete      bool
}

func missionInputFlags(fs *flag.FlagSet) func() (missionInputChange, error) {
	kind := fs.String("kind", "", "text (the default), file or folder")
	description := fs.String("description", "", "what the run should supply")
	required := fs.Bool("required", false, "a run must supply it")
	optional := fs.Bool("optional", false, "a run may leave it out")
	remove := fs.Bool("delete", false, "remove the input")
	return func() (missionInputChange, error) {
		given := givenFlags(fs)
		change := missionInputChange{name: fs.Arg(1), delete: *remove}
		if given["required"] && given["optional"] {
			return change, errors.New("give --required or --optional, not both")
		}
		if change.delete && (given["kind"] || given["description"] || given["required"] || given["optional"]) {
			return change, errors.New("--delete takes no other input flags")
		}
		if given["kind"] {
			change.kind = kind
		}
		if given["description"] {
			change.description = description
		}
		if given["required"] || given["optional"] {
			value := *required && !*optional
			change.required = &value
		}
		return change, nil
	}
}

var missionInputBoolFlags = map[string]bool{"json": true, "required": true, "optional": true, "delete": true}

// applyMissionInputChange returns the Input card's declared inputs after one
// change: a new input is appended, an existing one keeps the fields not given.
func applyMissionInputChange(current []formations.MissionInput, change missionInputChange) ([]formations.MissionInput, error) {
	next := append([]formations.MissionInput{}, current...)
	index := -1
	for i, input := range next {
		if input.Name == change.name {
			index = i
		}
	}
	if change.delete {
		if index < 0 {
			return nil, fmt.Errorf("%w: the mission has no input named %s", formations.ErrNotFound, change.name)
		}
		return append(next[:index], next[index+1:]...), nil
	}
	if index < 0 {
		next = append(next, formations.MissionInput{Name: change.name, Kind: formations.MissionInputText})
		index = len(next) - 1
	}
	if change.kind != nil {
		next[index].Kind = *change.kind
	}
	if change.description != nil {
		next[index].Description = *change.description
	}
	if change.required != nil {
		next[index].Required = *change.required
	}
	return formations.NormalizeMissionInputs(next)
}

func missionInputVerb(change missionInputChange) string {
	if change.delete {
		return "removed"
	}
	return "set"
}

// writeMissionInputs lists what a run of the mission supplies.
func writeMissionInputs(stdout io.Writer, board *formations.BoardDocument, jsonOut bool) int {
	inputs := formations.MissionRunInputs(board)
	if jsonOut {
		return writeJSON(stdout, map[string]any{"mission": board.Slug, "inputs": inputs})
	}
	for _, input := range inputs {
		fmt.Fprintln(stdout, missionInputLine(input))
	}
	return 0
}

func missionInputLine(input formations.MissionInput) string {
	need := "optional"
	if input.Required {
		need = "required"
	}
	return strings.TrimRight(fmt.Sprintf("%s\t%s\t%s\t%s", input.Name, input.Kind, need, input.Description), "\t")
}

func runMissionInput(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission input", stderr)
	read := missionInputFlags(fs)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, missionInputBoolFlags)); err != nil {
		return 2
	}
	change, err := read()
	if fs.NArg() < 1 || fs.NArg() > 2 || err != nil {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		fmt.Fprintln(stderr, commandUsage("mission input"))
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
	if fs.NArg() == 1 {
		return writeMissionInputs(stdout, board, *jsonOut)
	}
	missionID, err := soleInputCard(board, fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "inputCard", fs.Arg(0))
	}
	card, _ := missionByID(board, missionID)
	inputs, err := applyMissionInputChange(card.Inputs, change)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "input", change.name)
	}
	result, err := store.UpdateMission(slug, formations.MissionUpdateRequest{MissionID: missionID, Inputs: &inputs, UpdatedBy: *updatedBy}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "input", change.name)
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "%s input %s on Input card %s\n", missionInputVerb(change), change.name, missionID)
	return 0
}

func remoteMissionInput(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("mission input", stderr)
	read := missionInputFlags(fs)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, missionInputBoolFlags)); err != nil {
		return 2
	}
	change, err := read()
	if fs.NArg() < 1 || fs.NArg() > 2 || err != nil {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		fmt.Fprintln(stderr, commandUsage("mission input"))
		return 2
	}
	if fs.NArg() == 1 {
		board, err := c.readBoard(fs.Arg(0))
		if err != nil {
			return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
		}
		return writeMissionInputs(stdout, board, *jsonOut)
	}
	missionID := ""
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		id, err := soleInputCard(board, fs.Arg(0))
		if err != nil {
			return "", nil, &remoteSelectorError{boundary: "inputCard", selector: fs.Arg(0), err: err}
		}
		missionID = id
		card, _ := missionByID(board, id)
		inputs, err := applyMissionInputChange(card.Inputs, change)
		if err != nil {
			return "", nil, &remoteSelectorError{boundary: "input", selector: change.name, err: err}
		}
		if inputs == nil {
			inputs = []formations.MissionInput{}
		}
		return "updateInputCard", map[string]any{"id": id, "inputs": inputs}, nil
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "input", change.name)
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("%s input %s on Input card %s", missionInputVerb(change), change.name, missionID))
}

// runInputFlags collects the values a run start supplies by input name.
type runInputFlags struct {
	values stringList
	files  stringList
}

func (r *runInputFlags) register(fs *flag.FlagSet) {
	fs.Var(&r.values, "input", "an input value, name=value; repeat for each input (see archon mission input <mission>)")
	fs.Var(&r.files, "input-file", "an input read from a UTF-8 file, name=path; repeat for more")
}

// collect returns the supplied values, never nil, so admission checks them.
func (r *runInputFlags) collect() (map[string]string, error) {
	inputs := map[string]string{}
	add := func(flagName, raw string, read func(string) (string, error)) error {
		name, value, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(name) == "" {
			return fmt.Errorf("--%s %q must be name=value", flagName, raw)
		}
		name = strings.TrimSpace(name)
		if _, seen := inputs[name]; seen {
			return fmt.Errorf("input %s is given twice", name)
		}
		value, err := read(value)
		if err != nil {
			return err
		}
		inputs[name] = value
		return nil
	}
	literal := func(value string) (string, error) { return value, nil }
	for _, raw := range r.values {
		if err := add("input", raw, literal); err != nil {
			return nil, err
		}
	}
	for _, raw := range r.files {
		if err := add("input-file", raw, readInputFile); err != nil {
			return nil, err
		}
	}
	return inputs, nil
}

func readInputFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read --input-file: %w", err)
	}
	if !utf8.Valid(raw) {
		return "", fmt.Errorf("--input-file %s must contain valid UTF-8", path)
	}
	return string(raw), nil
}
