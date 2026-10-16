package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Mission outputs (archon-5tg1): the named results a successful run promises,
// declared on the Input card and each bound to one step output port.

// missionOutputChange is what one mission output command asks for.
type missionOutputChange struct {
	name        string
	kind        *string
	from        *string
	description *string
	required    *bool
	delete      bool
}

func missionOutputFlags(fs *flag.FlagSet) func() (missionOutputChange, error) {
	kind := fs.String("kind", "", "text (the default), file or folder")
	from := fs.String("from", "", "the step output port it comes from, <step-id>:<port-id>")
	description := fs.String("description", "", "what the run produces")
	required := fs.Bool("required", false, "a successful run must produce it")
	optional := fs.Bool("optional", false, "a successful run may leave it out")
	remove := fs.Bool("delete", false, "remove the output")
	return func() (missionOutputChange, error) {
		given := givenFlags(fs)
		change := missionOutputChange{name: fs.Arg(1), delete: *remove}
		if given["required"] && given["optional"] {
			return change, errors.New("give --required or --optional, not both")
		}
		if change.delete && (given["kind"] || given["from"] || given["description"] || given["required"] || given["optional"]) {
			return change, errors.New("--delete takes no other output flags")
		}
		if given["kind"] {
			change.kind = kind
		}
		if given["from"] {
			change.from = from
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

var missionOutputBoolFlags = map[string]bool{"json": true, "required": true, "optional": true, "delete": true}

// applyMissionOutputChange returns the Input card's declared outputs after one
// change: a new output is appended, an existing one keeps the fields not given.
func applyMissionOutputChange(current []formations.MissionOutput, change missionOutputChange) ([]formations.MissionOutput, error) {
	next := append([]formations.MissionOutput{}, current...)
	index := -1
	for i, output := range next {
		if output.Name == change.name {
			index = i
		}
	}
	if change.delete {
		if index < 0 {
			return nil, fmt.Errorf("%w: the mission has no output named %s", formations.ErrNotFound, change.name)
		}
		return formations.NormalizeMissionOutputs(append(next[:index], next[index+1:]...))
	}
	if index < 0 {
		next = append(next, formations.MissionOutput{Name: change.name, Kind: formations.MissionInputText})
		index = len(next) - 1
	}
	if change.kind != nil {
		next[index].Kind = *change.kind
	}
	if change.from != nil {
		next[index].From = *change.from
	}
	if change.description != nil {
		next[index].Description = *change.description
	}
	if change.required != nil {
		next[index].Required = *change.required
	}
	return formations.NormalizeMissionOutputs(next)
}

func missionOutputVerb(change missionOutputChange) string {
	if change.delete {
		return "removed"
	}
	return "set"
}

// writeMissionOutputs lists what a successful run of the mission produces.
func writeMissionOutputs(stdout io.Writer, board *formations.BoardDocument, jsonOut bool) int {
	outputs := formations.MissionOutputs(board)
	if jsonOut {
		if outputs == nil {
			outputs = []formations.MissionOutput{}
		}
		return writeJSON(stdout, map[string]any{"mission": board.Slug, "outputs": outputs})
	}
	for _, output := range outputs {
		fmt.Fprintln(stdout, missionOutputLine(output))
	}
	return 0
}

func missionOutputLine(output formations.MissionOutput) string {
	need := "optional"
	if output.Required {
		need = "required"
	}
	from := output.From
	if from == "" {
		from = "unbound"
	}
	return strings.TrimRight(fmt.Sprintf("%s\t%s\t%s\t%s\t%s", output.Name, output.Kind, need, from, output.Description), "\t")
}

func runMissionOutput(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("mission output", stderr)
	read := missionOutputFlags(fs)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, missionOutputBoolFlags)); err != nil {
		return 2
	}
	change, err := read()
	if fs.NArg() < 1 || fs.NArg() > 2 || err != nil {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		fmt.Fprintln(stderr, commandUsage("mission output"))
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
		return writeMissionOutputs(stdout, board, *jsonOut)
	}
	missionID, err := soleInputCard(board, fs.Arg(0))
	if err != nil {
		return failSelector(stderr, err, *jsonOut, "inputCard", fs.Arg(0))
	}
	card, _ := missionByID(board, missionID)
	outputs, err := applyMissionOutputChange(card.Outputs, change)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "output", change.name)
	}
	result, err := store.UpdateMission(slug, formations.MissionUpdateRequest{MissionID: missionID, Outputs: &outputs, UpdatedBy: *updatedBy}, formations.WriteOptions{ExpectedETag: board.ETag, ExpectedRev: board.Rev})
	if err != nil {
		return failDefinitionWrite(stderr, err, *jsonOut, "output", change.name)
	}
	result.TOML = ""
	if *jsonOut {
		return writeJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "%s output %s on Input card %s\n", missionOutputVerb(change), change.name, missionID)
	return 0
}

func remoteMissionOutput(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("mission output", stderr)
	read := missionOutputFlags(fs)
	updatedBy := fs.String("updated-by", "agent:archon", "update actor")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, missionOutputBoolFlags)); err != nil {
		return 2
	}
	change, err := read()
	if fs.NArg() < 1 || fs.NArg() > 2 || err != nil {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		fmt.Fprintln(stderr, commandUsage("mission output"))
		return 2
	}
	if fs.NArg() == 1 {
		board, err := c.readBoard(fs.Arg(0))
		if err != nil {
			return remoteFail(stderr, err, *jsonOut, "mission", fs.Arg(0))
		}
		return writeMissionOutputs(stdout, board, *jsonOut)
	}
	missionID := ""
	data, _, err := c.patchBoard(fs.Arg(0), *updatedBy, func(board *formations.BoardDocument) (string, map[string]any, error) {
		id, err := soleInputCard(board, fs.Arg(0))
		if err != nil {
			return "", nil, &remoteSelectorError{boundary: "inputCard", selector: fs.Arg(0), err: err}
		}
		missionID = id
		card, _ := missionByID(board, id)
		outputs, err := applyMissionOutputChange(card.Outputs, change)
		if err != nil {
			return "", nil, &remoteSelectorError{boundary: "output", selector: change.name, err: err}
		}
		if outputs == nil {
			outputs = []formations.MissionOutput{}
		}
		return "updateInputCard", map[string]any{"id": id, "outputs": outputs}, nil
	})
	if err != nil {
		return remoteFail(stderr, err, *jsonOut, "output", change.name)
	}
	return writeRemoteBoard(stdout, stderr, data, *jsonOut, fmt.Sprintf("%s output %s on Input card %s", missionOutputVerb(change), change.name, missionID))
}

// runOutputLine is one mission output of a run that succeeded, on one line: a
// text value of several lines shows its first, and --json holds it whole.
func runOutputLine(output formations.RunOutput) string {
	value := output.Value
	if output.Kind == formations.PortKindText {
		if first := firstLine(value); first != strings.TrimSpace(value) {
			value = first + " [...]"
		}
	}
	return fmt.Sprintf("output\t%s\t%s\t%s", output.Name, output.Kind, value)
}

// runOutputLines is a run output's line, then for each pick it came through
// a picked line naming the failed verdict and a finding line per finding
// (ADR-0022). Text of several lines shows its first; --json holds it whole.
func runOutputLines(output formations.RunOutput) []string {
	lines := []string{runOutputLine(output)}
	for _, pick := range output.Picks {
		lines = append(lines, fmt.Sprintf("picked\t%s\t%s attempt %d through %s by %s; its verdict: %s: %s",
			output.Name, pick.NodeID, pick.Attempt, pick.GateID, pick.DecidedBy, pick.Verdict.Verdict, oneLine(pick.Verdict.Reason)))
		for _, finding := range pick.Verdict.Findings {
			lines = append(lines, fmt.Sprintf("finding\t%s\t%s", output.Name, oneLine(finding)))
		}
	}
	return lines
}

// oneLine is text's first line, marked when there is more.
func oneLine(text string) string {
	if first := firstLine(text); first != strings.TrimSpace(text) {
		return first + " [...]"
	}
	return strings.TrimSpace(text)
}
