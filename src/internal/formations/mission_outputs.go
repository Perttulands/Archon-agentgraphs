package formations

import (
	"errors"
	"fmt"
	"strings"
)

// Mission outputs and port kinds (archon-5tg1). A step output port carries
// text, a file or a folder. A mission declares on its Input card the named
// results a successful run promises; each is bound to one step output port by
// from = "<step-id>:<port-id>", because generated port IDs cannot follow the
// output naming rule and gated steps reach their End only through a gate.

// Port kinds share the mission input kinds.
const (
	PortKindText   = MissionInputText
	PortKindFile   = MissionInputFile
	PortKindFolder = MissionInputFolder
)

// The Tool schema writes its payload kind on every formation port under the
// same key: work, or gate_feedback on a retry-control input. Both carry text.
const (
	toolPayloadWork         = "work"
	toolPayloadGateFeedback = "gate_feedback"
)

// Validation finding codes for port kinds and mission outputs.
const (
	FindingInvalidPortKind           = "invalid_port_kind"
	FindingInvalidMissionOutput      = "invalid_mission_output"
	FindingUnboundMissionOutput      = "unbound_mission_output"
	FindingMissionOutputKindMismatch = "mission_output_kind_mismatch"
)

// RunFailureMissingOutput is the run_failed code of a run that reached a done
// End without a required mission output, or with an output path that is gone.
const RunFailureMissingOutput = "missing_output"

var (
	ErrInvalidMissionOutput = errors.New("invalid_mission_output")
	ErrInvalidPort          = errors.New("invalid_port")
)

// MissionOutput is one named result a successful run of the mission promises.
type MissionOutput struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`
	Required    bool   `json:"required,omitempty"`
	// From is the bound step output port, "<step-id>:<port-id>". Blank is a
	// draft the validation report names.
	From string `json:"from,omitempty"`
}

// RunOutput is one mission output a successful run produced: the latest
// accepted payload of its bound port, the snapshot path for a file or folder.
type RunOutput struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	NodeID  string `json:"nodeId"`
	PortID  string `json:"portId"`
	Attempt int    `json:"attempt"`
}

// NormalizePortKind returns the stored form of an output port kind: "" for
// text or blank, file or folder as given.
func NormalizePortKind(kind string) (string, error) {
	switch kind = strings.TrimSpace(kind); kind {
	case "", PortKindText:
		return "", nil
	case PortKindFile, PortKindFolder:
		return kind, nil
	}
	return "", fmt.Errorf("%w: port kind %q; use text, file or folder", ErrInvalidPort, kind)
}

// PortKind is a port's kind, text when unset.
func PortKind(port FormationPort) string {
	if port.Kind == "" {
		return PortKindText
	}
	return port.Kind
}

// NormalizeMissionOutputs trims each declaration and gives a blank kind the
// default, text. Names follow the mission input rule and are unique; a from
// that is set names a step and a port. None is nil.
func NormalizeMissionOutputs(outputs []MissionOutput) ([]MissionOutput, error) {
	var normalized []MissionOutput
	seen := map[string]bool{}
	for _, output := range outputs {
		output.Name = strings.TrimSpace(output.Name)
		output.Description = strings.TrimSpace(output.Description)
		output.Kind = strings.TrimSpace(output.Kind)
		output.From = strings.TrimSpace(output.From)
		if output.Kind == "" {
			output.Kind = MissionInputText
		}
		if err := checkMissionOutput(output, seen); err != nil {
			return nil, err
		}
		seen[output.Name] = true
		normalized = append(normalized, output)
	}
	return normalized, nil
}

func checkMissionOutput(output MissionOutput, seen map[string]bool) error {
	switch {
	case !missionInputNamePattern.MatchString(output.Name):
		return fmt.Errorf("%w: output name %q must start with a lowercase letter and hold only lowercase letters, digits and underscores, such as report or site_dir", ErrInvalidMissionOutput, output.Name)
	case output.Kind != MissionInputText && output.Kind != MissionInputFile && output.Kind != MissionInputFolder:
		return fmt.Errorf("%w: output %s has kind %q; use text, file or folder", ErrInvalidMissionOutput, output.Name, output.Kind)
	case seen[output.Name]:
		return fmt.Errorf("%w: the mission already has an output named %s", ErrInvalidMissionOutput, output.Name)
	}
	if output.From != "" {
		if _, _, ok := splitEndpoint(output.From); !ok {
			return fmt.Errorf("%w: output %s has from %q; name a step output port as <step-id>:<port-id>", ErrInvalidMissionOutput, output.Name, output.From)
		}
	}
	return nil
}

// MissionOutputs lists what a successful run of the mission promises: the
// outputs its Input card declares.
func MissionOutputs(board *BoardDocument) []MissionOutput {
	if card := missionInputCard(board); card != nil {
		return card.Outputs
	}
	return nil
}

// boundOutputPort finds the step output port a mission output names.
func boundOutputPort(board *BoardDocument, from string) (FormationNode, FormationPort, bool) {
	nodeID, portID, ok := splitEndpoint(from)
	if !ok {
		return FormationNode{}, FormationPort{}, false
	}
	formation, ok := findFormation(board.Formations, nodeID)
	if !ok {
		return FormationNode{}, FormationPort{}, false
	}
	for _, port := range formation.Outputs {
		if port.ID == portID {
			return formation, port, true
		}
	}
	return FormationNode{}, FormationPort{}, false
}

// portKindFindings reports a port kind a hand-written file got wrong: an
// output kind other than text, file or folder, or a kind on an input port.
func portKindFindings(board *BoardDocument) []BoardFinding {
	var findings []BoardFinding
	for _, formation := range board.Formations {
		for _, port := range formation.Inputs {
			if port.Kind != "" {
				findings = append(findings, BoardFinding{
					Code:    FindingInvalidPortKind,
					NodeID:  formation.ID,
					Message: fmt.Sprintf("%s input %s has kind %q; only output ports carry a kind", possessive(nodeName(board, formation.ID)), port.ID, port.Kind),
				})
			}
		}
		for _, port := range formation.Outputs {
			if _, err := NormalizePortKind(port.Kind); err != nil {
				findings = append(findings, BoardFinding{
					Code:    FindingInvalidPortKind,
					NodeID:  formation.ID,
					Message: fmt.Sprintf("%s output %s has kind %q; use text, file or folder", possessive(nodeName(board, formation.ID)), port.ID, port.Kind),
				})
			}
		}
	}
	return findings
}

// missionOutputFindings reports output declarations a hand-written file got
// wrong, an output bound to no step output port, and an output whose kind is
// not its port's. They are located on the Input card.
func missionOutputFindings(board *BoardDocument) []BoardFinding {
	var findings []BoardFinding
	for _, card := range board.Missions {
		seen := map[string]bool{}
		add := func(code, format string, args ...any) {
			findings = append(findings, BoardFinding{Code: code, NodeID: card.ID, Message: fmt.Sprintf(format, args...)})
		}
		for _, output := range card.Outputs {
			kind := output.Kind
			if kind == "" {
				kind = MissionInputText
			}
			checked := MissionOutput{Name: output.Name, Kind: kind, From: output.From}
			if err := checkMissionOutput(checked, seen); err != nil {
				add(FindingInvalidMissionOutput, "%s", strings.TrimPrefix(err.Error(), ErrInvalidMissionOutput.Error()+": "))
				seen[output.Name] = true
				continue
			}
			seen[output.Name] = true
			if output.From == "" {
				add(FindingUnboundMissionOutput, "output %s is bound to no step output; set its from to <step-id>:<port-id>", output.Name)
				continue
			}
			formation, port, ok := boundOutputPort(board, output.From)
			if !ok {
				add(FindingUnboundMissionOutput, "output %s is bound to %s, which is not a step output port", output.Name, output.From)
				continue
			}
			if portKind := PortKind(port); portKind != kind {
				add(FindingMissionOutputKindMismatch, "output %s is %s, but %s output %s carries %s", output.Name, kind, possessive(nodeName(board, formation.ID)), portName(port), portKind)
			}
		}
	}
	return findings
}

func portName(port FormationPort) string {
	if strings.TrimSpace(port.Label) != "" {
		return port.Label
	}
	return port.ID
}

// resolveMissionOutputs lists the outputs of a run that reached a done End:
// each declared output is its bound port's payload from the attempt the run
// carried to a done End (doneEndOutputs), with that attempt. It returns the
// failure reason when no attempt of a required output's port reached a done
// End or a produced path no longer exists.
func resolveMissionOutputs(board *BoardDocument, events []RunEvent) ([]RunOutput, string) {
	card := missionInputCard(board)
	if card == nil || len(card.Outputs) == 0 {
		return nil, ""
	}
	reached := doneEndOutputs(board, events)
	var outputs []RunOutput
	var problems []string
	for _, output := range card.Outputs {
		kind := output.Kind
		if kind == "" {
			kind = MissionInputText
		}
		nodeID, portID, _ := splitEndpoint(output.From)
		got, ok := reached[OutputOrigin{NodeID: nodeID, PortID: portID}]
		value := ""
		if ok {
			value = got.payload.Text
			if kind != MissionInputText {
				value = got.payload.Ref
			}
		}
		if !ok || strings.TrimSpace(value) == "" {
			if output.Required {
				problems = append(problems, fmt.Sprintf("required output %s: no output of %s reached a done End", output.Name, output.From))
			}
			continue
		}
		if problem := runInputPathProblem(kind, value); problem != "" {
			problems = append(problems, fmt.Sprintf("output %s must be %s; %s", output.Name, inputKindPhrase(kind), problem))
			continue
		}
		outputs = append(outputs, RunOutput{Name: output.Name, Kind: kind, Value: value, NodeID: nodeID, PortID: portID, Attempt: got.attempt})
	}
	if len(problems) > 0 {
		return outputs, strings.Join(problems, "; ")
	}
	return outputs, ""
}

// reachedOutput is one attempt's payload on a port.
type reachedOutput struct {
	payload FormationOutputPayload
	attempt int
}

// doneEndOutputs follows what reached each done End back to the outputs it
// came from, keyed by node and port with Attempt zero. A delivery names the
// output it carries (RunInputRef.Origin), which passes through gates
// unchanged, and each attempt's node_started names what it took in. From
// every delivery to a done End, a step's output and a gate's pass route
// alike, the walk goes to the output delivered and on through the inputs of
// the attempt that produced it. A delivery a gate's fail route made carries
// rejected work and is not followed, so a rejected attempt is never reached
// through it. A pass that routes an earlier attempt's output, as best of N
// does, reaches that attempt. Where several attempts of one port were
// reached, the latest counts.
func doneEndOutputs(board *BoardDocument, events []RunEvent) map[OutputOrigin]reachedOutput {
	gates := map[string]bool{}
	for _, gate := range board.Gates {
		gates[gate.ID] = true
	}
	judges := judgedGates(board)
	type attemptKey struct {
		nodeID  string
		attempt int
	}
	inputs := map[attemptKey][]RunInputRef{}
	produced := map[attemptKey]map[string]FormationOutputPayload{}
	var queue []OutputOrigin
	follow := func(input RunInputRef) {
		if input.Origin != nil && !(gates[input.FromNodeID] && input.FromPortID == "fail") {
			queue = append(queue, *input.Origin)
		}
	}
	toDoneEnd := func(connection BoardConnection) bool {
		toNode, _ := endpointParts(connection.To)
		end, ok := findEnd(board, toNode)
		return ok && end.Outcome != EndOutcomeRejected
	}
	for _, event := range events {
		switch event.Type {
		case RunEventNodeStarted:
			inputs[attemptKey{event.NodeID, event.Attempt}] = runInputRefsFromAny(event.Data["inputRefs"])
		case RunEventNodeOutput:
			if len(judges[event.NodeID]) > 0 {
				continue
			}
			payloads := outputPayloadsFromAny(event.Data["outputs"])
			produced[attemptKey{event.NodeID, event.Attempt}] = payloads
			for _, connection := range outgoingConnections(board.Connections, event.NodeID) {
				_, fromPort := endpointParts(connection.From)
				if _, ok := payloads[fromPort]; ok && toDoneEnd(connection) {
					queue = append(queue, OutputOrigin{NodeID: event.NodeID, PortID: fromPort, Attempt: event.Attempt})
				}
			}
		case RunEventGateVerdict:
			if stringFromEventData(event, "routePort") != "pass" {
				continue
			}
			for _, route := range gateVerdictRoutes(board, event, eventGateID(event), "pass") {
				if toDoneEnd(route) {
					follow(runInputRefFromAny(event.Data["inputRef"]))
				}
			}
		}
	}
	reached := map[OutputOrigin]reachedOutput{}
	seen := map[OutputOrigin]bool{}
	for len(queue) > 0 {
		origin := queue[0]
		queue = queue[1:]
		if seen[origin] {
			continue
		}
		seen[origin] = true
		key := attemptKey{origin.NodeID, origin.Attempt}
		payload, ok := produced[key][origin.PortID]
		if !ok {
			continue
		}
		port := OutputOrigin{NodeID: origin.NodeID, PortID: origin.PortID}
		if got, ok := reached[port]; !ok || origin.Attempt > got.attempt {
			reached[port] = reachedOutput{payload: payload, attempt: origin.Attempt}
		}
		for _, input := range inputs[key] {
			follow(input)
		}
	}
	return reached
}

// runInputRefsFromAny reads a node_started's inputRefs.
func runInputRefsFromAny(value any) []RunInputRef {
	items, _ := value.([]any)
	refs := make([]RunInputRef, 0, len(items))
	for _, item := range items {
		refs = append(refs, runInputRefFromAny(item))
	}
	return refs
}

// RunOutputsFromEventData reads the outputs recorded on run_succeeded.
func RunOutputsFromEventData(value any) []RunOutput {
	switch items := value.(type) {
	case []RunOutput:
		return items
	case []any:
		outputs := make([]RunOutput, 0, len(items))
		for _, item := range items {
			fields, ok := item.(map[string]any)
			if !ok {
				continue
			}
			outputs = append(outputs, RunOutput{
				Name:    stringFromAny(fields["name"]),
				Kind:    stringFromAny(fields["kind"]),
				Value:   stringFromAny(fields["value"]),
				NodeID:  stringFromAny(fields["nodeId"]),
				PortID:  stringFromAny(fields["portId"]),
				Attempt: intFromRunEventData(fields["attempt"]),
			})
		}
		return outputs
	}
	return nil
}
