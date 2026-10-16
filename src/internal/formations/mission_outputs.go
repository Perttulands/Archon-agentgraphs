package formations

import (
	"errors"
	"fmt"
	"slices"
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

// RunOutput is one mission output a run that reached a done End produced:
// the latest accepted payload of its bound port, the snapshot path for a file
// or folder.
type RunOutput struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Value   string `json:"value"`
	NodeID  string `json:"nodeId"`
	PortID  string `json:"portId"`
	Attempt int    `json:"attempt"`
	// Picks are the picks the value came through, oldest first (ADR-0022):
	// each picked attempt is failed work a spent limit carried on, with the
	// verdict that failed it. A value no pick carried has none.
	Picks []RunOutputPick `json:"picks,omitempty"`
}

// RunOutputPick is one pick a run's work came through: the step's attempt a
// spent limit picked, through which gate, who picked it, and the verdict that
// gate gave that attempt before the pick (ADR-0022).
type RunOutputPick struct {
	NodeID    string `json:"nodeId"`
	Attempt   int    `json:"attempt"`
	GateID    string `json:"gateId"`
	Seq       int    `json:"seq"`
	DecidedBy string `json:"decidedBy"`
	// Verdict is the picked attempt's own verdict as the gate recorded it.
	Verdict PickedVerdict `json:"verdict"`
}

// PickedVerdict is the verdict a pick's gate gave the picked attempt: its
// ledger seq, the verdict (a fail, unless a pass route led back to the
// step), the judge's or operator's reason and the judge's findings.
type PickedVerdict struct {
	Seq      int      `json:"seq"`
	Verdict  string   `json:"verdict"`
	Reason   string   `json:"reason"`
	Findings []string `json:"findings,omitempty"`
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
// carried to a done End (doneEndOutputs), with that attempt and the picks it
// came through. picks are every pick the work that reached a done End came
// through, which make the run picked rather than succeeded (ADR-0022). It
// returns the failure reason when no attempt of a required output's port
// reached a done End or a produced path no longer exists.
func resolveMissionOutputs(board *BoardDocument, events []RunEvent) (outputs []RunOutput, picks []RunOutputPick, missing string) {
	reached, picks := doneEndOutputs(board, events)
	card := missionInputCard(board)
	if card == nil || len(card.Outputs) == 0 {
		return nil, picks, ""
	}
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
		outputs = append(outputs, RunOutput{Name: output.Name, Kind: kind, Value: value, NodeID: nodeID, PortID: portID, Attempt: got.attempt, Picks: got.picks})
	}
	if len(problems) > 0 {
		return outputs, picks, strings.Join(problems, "; ")
	}
	return outputs, picks, ""
}

// reachedOutput is one attempt's payload on a port and the picks it came
// through.
type reachedOutput struct {
	payload FormationOutputPayload
	attempt int
	picks   []RunOutputPick
}

// pickedOutputs maps each output a pick carried on, by the origin of the
// gate input its pass routed, to that pick with the picked attempt's own
// verdict.
func pickedOutputs(events []RunEvent) map[OutputOrigin]RunOutputPick {
	picked := map[OutputOrigin]RunOutputPick{}
	for _, event := range events {
		pick := pickData(event)
		if event.Type != RunEventGateVerdict || pick == nil {
			continue
		}
		origin := runInputRefFromAny(event.Data["inputRef"]).Origin
		if origin == nil {
			continue
		}
		entry := RunOutputPick{
			NodeID: stringFromAny(pick["nodeId"]), Attempt: intFromRunEventData(pick["attempt"]), GateID: eventGateID(event),
			Seq: event.Seq, DecidedBy: stringFromAny(pick["decidedBy"]),
		}
		if pickSeq := intFromRunEventData(pick["pickSeq"]); pickSeq > 0 && pickSeq <= len(events) {
			for _, candidate := range pickCandidatesFromEvent(events[pickSeq-1]) {
				if candidate.Attempt != entry.Attempt || candidate.VerdictSeq <= 0 || candidate.VerdictSeq > len(events) {
					continue
				}
				verdict := events[candidate.VerdictSeq-1]
				entry.Verdict = PickedVerdict{Seq: verdict.Seq, Verdict: stringFromEventData(verdict, "verdict"), Reason: stringFromEventData(verdict, "reason")}
				for _, ref := range gateEvidenceRefsFromRunEventData(verdict.Data["evidence"]) {
					if text := strings.TrimSpace(ref.Text); text != "" && text != entry.Verdict.Reason {
						entry.Verdict.Findings = append(entry.Verdict.Findings, text)
					}
				}
			}
		}
		picked[*origin] = entry
	}
	return picked
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
//
// An output came through a pick when a pick carried it on, or carried on an
// output that the attempt producing it took in, however far back
// (ADR-0022). Each reached output lists those picks, and the second result
// lists every pick anything delivered to a done End came through.
func doneEndOutputs(board *BoardDocument, events []RunEvent) (map[OutputOrigin]reachedOutput, []RunOutputPick) {
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
	picked := pickedOutputs(events)
	through := map[OutputOrigin][]RunOutputPick{}
	var picksThrough func(origin OutputOrigin) []RunOutputPick
	picksThrough = func(origin OutputOrigin) []RunOutputPick {
		if found, ok := through[origin]; ok {
			return found
		}
		// Inputs come from earlier outputs, so the walk ends; the entry
		// guards a ledger that says otherwise.
		through[origin] = nil
		var found []RunOutputPick
		if pick, ok := picked[origin]; ok {
			found = append(found, pick)
		}
		for _, input := range inputs[attemptKey{origin.NodeID, origin.Attempt}] {
			if input.Origin != nil && !(gates[input.FromNodeID] && input.FromPortID == "fail") {
				found = append(found, picksThrough(*input.Origin)...)
			}
		}
		found = uniquePicks(found)
		through[origin] = found
		return found
	}
	var crossed []RunOutputPick
	for _, origin := range queue {
		crossed = append(crossed, picksThrough(origin)...)
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
			reached[port] = reachedOutput{payload: payload, attempt: origin.Attempt, picks: picksThrough(origin)}
		}
		for _, input := range inputs[key] {
			follow(input)
		}
	}
	return reached, uniquePicks(crossed)
}

// uniquePicks lists each pick once, oldest first.
func uniquePicks(picks []RunOutputPick) []RunOutputPick {
	if len(picks) == 0 {
		return nil
	}
	picks = slices.Clone(picks)
	slices.SortFunc(picks, func(a, b RunOutputPick) int { return a.Seq - b.Seq })
	return slices.CompactFunc(picks, func(a, b RunOutputPick) bool { return a.Seq == b.Seq })
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

// RunOutputPicksFromAny reads the picks recorded on run_picked or on one of
// its outputs.
func RunOutputPicksFromAny(value any) []RunOutputPick {
	switch items := value.(type) {
	case []RunOutputPick:
		return items
	case []any:
		var picks []RunOutputPick
		for _, item := range items {
			fields, ok := item.(map[string]any)
			if !ok {
				continue
			}
			verdict, _ := fields["verdict"].(map[string]any)
			findings := stringSliceFromAny(verdict["findings"])
			if len(findings) == 0 {
				findings = nil
			}
			picks = append(picks, RunOutputPick{
				NodeID:    stringFromAny(fields["nodeId"]),
				Attempt:   intFromRunEventData(fields["attempt"]),
				GateID:    stringFromAny(fields["gateId"]),
				Seq:       intFromRunEventData(fields["seq"]),
				DecidedBy: stringFromAny(fields["decidedBy"]),
				Verdict: PickedVerdict{
					Seq:      intFromRunEventData(verdict["seq"]),
					Verdict:  stringFromAny(verdict["verdict"]),
					Reason:   stringFromAny(verdict["reason"]),
					Findings: findings,
				},
			})
		}
		return picks
	}
	return nil
}

// RunOutputsFromEventData reads the outputs recorded on run_succeeded or
// run_picked.
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
				Picks:   RunOutputPicksFromAny(fields["picks"]),
			})
		}
		return outputs
	}
	return nil
}
