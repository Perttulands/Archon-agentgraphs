package formations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Mission inputs (archon-o7p.3). A mission declares on its Input card the named
// values each run supplies; step briefs reference them as {name}. A mission
// that declares none has one implicit required text input named brief.

const (
	MissionInputText   = "text"
	MissionInputFile   = "file"
	MissionInputFolder = "folder"
	// ImplicitBriefInput names the input of a mission that declares none.
	ImplicitBriefInput = "brief"
	// notSuppliedInput stands in a brief for an optional input a run left out.
	notSuppliedInput = "(not supplied)"
)

// Admission and validation finding codes for inputs.
const (
	FindingMissingInput          = "missing_input"
	FindingUnknownInput          = "unknown_input"
	FindingInvalidInputValue     = "invalid_input"
	FindingInvalidMissionInput   = "invalid_mission_input"
	FindingUnknownInputReference = "unknown_input_reference"
)

var ErrInvalidMissionInput = errors.New("invalid_mission_input")

// MissionInput is one named value the mission's runs supply.
type MissionInput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required,omitempty"`
}

// RunInput is one value a run supplied, recorded on run_started in the
// mission's declared order.
type RunInput struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

var (
	missionInputNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	// inputReferencePattern matches an escaped {{name}} (group 1), which stands
	// for a literal {name}, or a reference {name} (group 2).
	inputReferencePattern = regexp.MustCompile(`\{\{([a-z][a-z0-9_]{0,63})\}\}|\{([a-z][a-z0-9_]{0,63})\}`)
)

// NormalizeMissionInputs trims each declaration and gives a blank kind the
// default, text. A name must be a lowercase letter followed by lowercase
// letters, digits or underscores, a kind text, file or folder, and names are
// unique. None is nil.
func NormalizeMissionInputs(inputs []MissionInput) ([]MissionInput, error) {
	var normalized []MissionInput
	seen := map[string]bool{}
	for _, input := range inputs {
		input.Name = strings.TrimSpace(input.Name)
		input.Description = strings.TrimSpace(input.Description)
		input.Kind = strings.TrimSpace(input.Kind)
		if input.Kind == "" {
			input.Kind = MissionInputText
		}
		if err := checkMissionInput(input, seen); err != nil {
			return nil, err
		}
		seen[input.Name] = true
		normalized = append(normalized, input)
	}
	return normalized, nil
}

func checkMissionInput(input MissionInput, seen map[string]bool) error {
	switch {
	case !missionInputNamePattern.MatchString(input.Name):
		return fmt.Errorf("%w: input name %q must start with a lowercase letter and hold only lowercase letters, digits and underscores, such as brief or target_repo", ErrInvalidMissionInput, input.Name)
	case input.Kind != MissionInputText && input.Kind != MissionInputFile && input.Kind != MissionInputFolder:
		return fmt.Errorf("%w: input %s has kind %q; use text, file or folder", ErrInvalidMissionInput, input.Name, input.Kind)
	case seen[input.Name]:
		return fmt.Errorf("%w: the mission already has an input named %s", ErrInvalidMissionInput, input.Name)
	}
	return nil
}

// missionInputCard is the mission's Input card, or nil when it has none.
func missionInputCard(board *BoardDocument) *MissionNode {
	if board == nil || len(board.Missions) == 0 {
		return nil
	}
	return &board.Missions[0]
}

// MissionRunInputs lists what a run of the mission supplies: its declared
// inputs, or the implicit required text input brief, described by the Input
// card's input hint.
func MissionRunInputs(board *BoardDocument) []MissionInput {
	card := missionInputCard(board)
	if card != nil && len(card.Inputs) > 0 {
		return card.Inputs
	}
	brief := MissionInput{Name: ImplicitBriefInput, Kind: MissionInputText, Required: true}
	if card != nil {
		brief.Description = card.InputHint
	}
	return []MissionInput{brief}
}

func missionInputNames(inputs []MissionInput) string {
	names := make([]string, 0, len(inputs))
	for _, input := range inputs {
		names = append(names, input.Name)
	}
	return strings.Join(names, ", ")
}

// runInputFindings checks the values a run supplies against the mission's
// inputs: every required one is given and not blank, every name is declared,
// and a file or folder is an absolute path to an existing file or directory.
func runInputFindings(board *BoardDocument, supplied map[string]string) []BoardFinding {
	declared := MissionRunInputs(board)
	nodeID := ""
	if card := missionInputCard(board); card != nil {
		nodeID = card.ID
	}
	var findings []BoardFinding
	add := func(code, format string, args ...any) {
		findings = append(findings, BoardFinding{Code: code, NodeID: nodeID, Message: fmt.Sprintf(format, args...)})
	}
	known := map[string]bool{}
	for _, input := range declared {
		known[input.Name] = true
		value, given := supplied[input.Name]
		if !given || strings.TrimSpace(value) == "" {
			if input.Required {
				if input.Description != "" {
					add(FindingMissingInput, "input %s is required: %s", input.Name, input.Description)
				} else {
					add(FindingMissingInput, "input %s is required", input.Name)
				}
			}
			continue
		}
		if problem := runInputPathProblem(input.Kind, strings.TrimSpace(value)); problem != "" {
			add(FindingInvalidInputValue, "input %s must be %s; %s", input.Name, inputKindPhrase(input.Kind), problem)
		}
	}
	var unknown []string
	for name := range supplied {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	for _, name := range unknown {
		add(FindingUnknownInput, "mission %s has no input named %q; its inputs are %s", board.Slug, name, missionInputNames(declared))
	}
	return findings
}

func inputKindPhrase(kind string) string {
	if kind == MissionInputFolder {
		return "the absolute path of an existing directory"
	}
	return "the absolute path of an existing file"
}

// runInputPathProblem says why a file or folder value cannot be used, or ""
// when it can. Text values need nothing.
func runInputPathProblem(kind, value string) string {
	if kind != MissionInputFile && kind != MissionInputFolder {
		return ""
	}
	if !filepath.IsAbs(value) {
		return fmt.Sprintf("%q is a relative path", value)
	}
	info, err := os.Stat(value)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Sprintf("%s does not exist", value)
	case err != nil:
		return fmt.Sprintf("%s cannot be read: %v", value, err)
	case kind == MissionInputFolder && !info.IsDir():
		return fmt.Sprintf("%s is not a directory", value)
	case kind == MissionInputFile && !info.Mode().IsRegular():
		return fmt.Sprintf("%s is not a regular file", value)
	}
	return ""
}

// ResolveRunInputs lists the supplied values in the mission's declared order,
// with their kinds. A file or folder path is trimmed; text is kept verbatim. A
// blank value and an undeclared name are left out.
func ResolveRunInputs(board *BoardDocument, supplied map[string]string) []RunInput {
	var resolved []RunInput
	for _, input := range MissionRunInputs(board) {
		value := supplied[input.Name]
		if strings.TrimSpace(value) == "" {
			continue
		}
		if input.Kind != MissionInputText {
			value = strings.TrimSpace(value)
		}
		resolved = append(resolved, RunInput{Name: input.Name, Kind: input.Kind, Value: value})
	}
	return resolved
}

// RenderRunInputs is the text the Input card hands its first step. The
// implicit brief is passed on unchanged; declared inputs are one
// "name: value" line each.
func RenderRunInputs(board *BoardDocument, inputs []RunInput) string {
	if card := missionInputCard(board); (card == nil || len(card.Inputs) == 0) && len(inputs) == 1 && inputs[0].Name == ImplicitBriefInput {
		return inputs[0].Value
	}
	lines := make([]string, 0, len(inputs))
	for _, input := range inputs {
		lines = append(lines, input.Name+": "+input.Value)
	}
	return strings.Join(lines, "\n")
}

// SubstituteRunInputs replaces each {name} reference to a mission input in a
// step brief with the run's value, or with "(not supplied)" for an optional
// input the run left out. An escaped {{name}} becomes a literal {name} and is
// never a reference. Other braces stay as written.
func SubstituteRunInputs(text string, declared []MissionInput, inputs []RunInput) string {
	if !strings.Contains(text, "{") {
		return text
	}
	values := make(map[string]string, len(inputs))
	for _, input := range inputs {
		values[input.Name] = input.Value
	}
	known := make(map[string]bool, len(declared))
	for _, input := range declared {
		known[input.Name] = true
	}
	return inputReferencePattern.ReplaceAllStringFunc(text, func(reference string) string {
		if strings.HasPrefix(reference, "{{") {
			return reference[1 : len(reference)-1]
		}
		name := reference[1 : len(reference)-1]
		if !known[name] {
			return reference
		}
		if value, ok := values[name]; ok {
			return value
		}
		return notSuppliedInput
	})
}

// RunInputsFromEventData reads the inputs recorded on run_started.
func RunInputsFromEventData(value any) []RunInput {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]RunInput); ok {
			return typed
		}
		return nil
	}
	inputs := make([]RunInput, 0, len(items))
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := fields["name"].(string)
		kind, _ := fields["kind"].(string)
		value, _ := fields["value"].(string)
		inputs = append(inputs, RunInput{Name: name, Kind: kind, Value: value})
	}
	return inputs
}

// missionInputFindings reports declarations a hand-written file got wrong, and
// each step brief reference to an input the mission does not have.
func missionInputFindings(board *BoardDocument) []BoardFinding {
	var findings []BoardFinding
	for _, card := range board.Missions {
		seen := map[string]bool{}
		for _, input := range card.Inputs {
			kind := input.Kind
			if kind == "" {
				kind = MissionInputText
			}
			if err := checkMissionInput(MissionInput{Name: input.Name, Kind: kind}, seen); err != nil {
				findings = append(findings, BoardFinding{
					Code:    FindingInvalidMissionInput,
					NodeID:  card.ID,
					Message: strings.TrimPrefix(err.Error(), ErrInvalidMissionInput.Error()+": "),
				})
			}
			seen[input.Name] = true
		}
	}
	declared := MissionRunInputs(board)
	known := make(map[string]bool, len(declared))
	for _, input := range declared {
		known[input.Name] = true
	}
	for _, formation := range board.Formations {
		if formation.Brief == nil {
			continue
		}
		reported := map[string]bool{}
		for _, match := range inputReferencePattern.FindAllStringSubmatch(formation.Brief.Goal, -1) {
			name := match[2]
			if name == "" || known[name] || reported[name] {
				continue
			}
			reported[name] = true
			findings = append(findings, BoardFinding{
				Code:   FindingUnknownInputReference,
				NodeID: formation.ID,
				Message: fmt.Sprintf("%s brief references {%s}, but the mission has no input named %s; its inputs are %s",
					possessive(nodeName(board, formation.ID)), name, name, missionInputNames(declared)),
			})
		}
	}
	return findings
}
