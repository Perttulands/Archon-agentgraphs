package formations

import (
	"fmt"
	"sort"
	"strings"
)

// Finding codes reported by ValidateBoard. They are stable strings so CLI and
// API consumers can branch on them.
const (
	FindingDanglingConnection       = "dangling_connection"
	FindingGateNotRoutable          = "gate_not_routable"
	FindingInvalidCodeGateProfile   = "invalid_code_gate_profile"
	FindingInvalidFormationType     = "invalid_formation_type"
	FindingInvalidHumanChannel      = "invalid_human_channel"
	FindingMissionCount             = "mission_count"
	FindingSeveralInputCards        = "several_input_cards"
	FindingMissionNotRunnable       = "mission_not_runnable"
	FindingInvalidTool              = "invalid_tool"
	FindingDuplicateNodeID          = "duplicate_node_id"
	FindingDuplicateSlotID          = "duplicate_slot_id"
	FindingDuplicateInputProducer   = "duplicate_input_producer"
	FindingIncompatibleMedia        = "incompatible_media"
	FindingIncompatiblePayloadKind  = "incompatible_payload_kind"
	FindingInvalidJudgeRelationship = "invalid_judge_relationship"
	FindingInvalidEnd               = "invalid_end"
	FindingRouteLeadsNowhere        = "route_leads_nowhere"
	FindingUnreachableNode          = "unreachable_node"
)

// BoardFinding is a single structural problem located on the board. NodeID names
// the offending node, or the edge id for connection problems.
type BoardFinding struct {
	Code    string `json:"code"`
	NodeID  string `json:"nodeId"`
	Message string `json:"message"`
}

// BoardValidationReport separates blocking errors from advisory warnings.
type BoardValidationReport struct {
	Errors   []BoardFinding `json:"errors"`
	Warnings []BoardFinding `json:"warnings"`
}

// ValidateBoard performs a read-only structural integrity check. It reuses the
// same endpoint, formation-type, judge-chain, and graph helpers used by the
// authoring/run paths so findings match runtime behavior.
func ValidateBoard(board *BoardDocument) BoardValidationReport {
	var report BoardValidationReport
	if board == nil {
		return report
	}

	raw := []byte(board.TOML)
	for _, connection := range board.Connections {
		if _, ok := endpointAllowsDirection(raw, connection.From, FormationPortOutput); !ok {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingDanglingConnection,
				NodeID:  connection.ID,
				Message: fmt.Sprintf("connection %q has a broken 'from' endpoint %q: it does not reference an existing node output", connection.ID, connection.From),
			})
		}
		if _, ok := endpointAllowsDirection(raw, connection.To, FormationPortInput); !ok {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingDanglingConnection,
				NodeID:  connection.ID,
				Message: fmt.Sprintf("connection %q has a broken 'to' endpoint %q: it does not reference an existing node input", connection.ID, connection.To),
			})
		}
		if finding, incompatible := toolConnectionCompatibilityFinding(board, connection); incompatible {
			report.Errors = append(report.Errors, finding)
		}
	}
	inputProducers := make(map[string]string, len(board.Connections))
	for _, connection := range board.Connections {
		// A gate-fail edge into an occupied input is the sanctioned typed
		// pushback route (ADR-0012); only non-pushback producers count toward
		// the one-producer rule.
		if isGateFailPushbackEndpoint(board.Gates, connection.From) || isEndEndpoint(board, connection.To) {
			continue
		}
		if first, exists := inputProducers[connection.To]; exists {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingDuplicateInputProducer,
				NodeID:  connection.ID,
				Message: fmt.Sprintf("connection %q is a second producer for input %q; it is already produced by connection %q", connection.ID, connection.To, first),
			})
			continue
		}
		inputProducers[connection.To] = connection.ID
	}

	for _, gate := range board.Gates {
		if gaps := gateRouteGaps(board, gate); len(gaps) > 0 {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingGateNotRoutable,
				NodeID:  gate.ID,
				Message: fmt.Sprintf("gate %q needs %s", gate.ID, strings.Join(gaps, "; ")),
			})
		}
	}

	for _, formation := range board.Formations {
		if err := validateFormationType(formation.Type); err != nil {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingInvalidFormationType,
				NodeID:  formation.ID,
				Message: fmt.Sprintf("formation %q has unsupported type %q; change it to solo, peer or orchestrated with formation set-type, or delete it", formation.ID, formation.Type),
			})
		}
	}

	report.Errors = append(report.Errors, duplicateSlotFindings(board.Formations)...)
	report.Errors = append(report.Errors, executionPolicyFindings(board)...)

	seenNodeIDs := make(map[string]string, len(board.Missions)+len(board.Formations)+len(board.Gates)+len(board.Tools))
	for _, mission := range board.Missions {
		seenNodeIDs[mission.ID] = "Mission"
	}
	for _, formation := range board.Formations {
		seenNodeIDs[formation.ID] = "Formation"
	}
	for _, gate := range board.Gates {
		seenNodeIDs[gate.ID] = "Gate"
	}
	for _, end := range board.Ends {
		if firstKind, exists := seenNodeIDs[end.ID]; end.ID != "" && exists {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingDuplicateNodeID,
				NodeID:  end.ID,
				Message: fmt.Sprintf("End node id %q duplicates an existing %s node id", end.ID, firstKind),
			})
		} else if end.ID != "" {
			seenNodeIDs[end.ID] = "End"
		}
		if _, err := NormalizeEndOutcome(end.Outcome); err != nil || end.Outcome == "" {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingInvalidEnd,
				NodeID:  end.ID,
				Message: fmt.Sprintf("End node %s has outcome %q; set it to done or rejected", nodeName(board, end.ID), end.Outcome),
			})
		}
	}
	report.Errors = append(report.Errors, routeLeadsNowhereFindings(board)...)
	report.Errors = append(report.Errors, missionInputFindings(board)...)
	report.Warnings = append(report.Warnings, unreachableNodeFindings(board)...)

	for _, tool := range board.Tools {
		if firstKind, exists := seenNodeIDs[tool.ID]; tool.ID != "" && exists {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingDuplicateNodeID,
				NodeID:  tool.ID,
				Message: fmt.Sprintf("Tool node id %q duplicates an existing %s node id", tool.ID, firstKind),
			})
		} else if tool.ID != "" {
			seenNodeIDs[tool.ID] = "Tool"
		}
		if board.Schema != CurrentBoardSchema {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingInvalidTool,
				NodeID:  tool.ID,
				Message: fmt.Sprintf("Tool %q requires mission file schema %d", tool.ID, CurrentBoardSchema),
			})
			continue
		}
		descriptor, ok := LookupToolProfileDescriptor(tool.ProfileID, tool.ProfileVersion)
		if !ok {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingInvalidTool,
				NodeID:  tool.ID,
				Message: fmt.Sprintf("Tool %q uses unknown profile tuple %q@%q", tool.ID, tool.ProfileID, tool.ProfileVersion),
			})
			continue
		}
		if err := validateToolNodeAgainstDescriptor(tool, descriptor); err != nil {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingInvalidTool,
				NodeID:  tool.ID,
				Message: err.Error(),
			})
		}
	}

	if len(board.Missions) == 0 {
		report.Errors = append(report.Errors, BoardFinding{
			Code:    FindingMissionCount,
			Message: "a mission needs its Input card to run; add one on the canvas or with archon mission create",
		})
	}
	if len(board.Missions) > 1 {
		report.Errors = append(report.Errors, SeveralInputCardsFinding(board))
	}
	for _, mission := range board.Missions {
		if _, err := NormalizeHumanChannel(mission.HumanChannel); err != nil {
			report.Errors = append(report.Errors, BoardFinding{
				Code:    FindingInvalidHumanChannel,
				NodeID:  mission.ID,
				Message: fmt.Sprintf("Input card %q has human channel %q; set it to notify or session with archon mission update --human-channel", mission.ID, mission.HumanChannel),
			})
		}
		if len(outgoingConnections(board.Connections, mission.ID)) == 0 {
			report.Warnings = append(report.Warnings, BoardFinding{
				Code:    FindingMissionNotRunnable,
				NodeID:  mission.ID,
				Message: fmt.Sprintf("Input card %q has no outgoing connection, so it cannot start a run; wire it to a first step", mission.ID),
			})
		}
	}

	sortFindings(report.Errors)
	sortFindings(report.Warnings)
	return report
}

// SeveralInputCardsFinding reports a file saved before one mission per file,
// when a board could hold several mission nodes. It still loads and stays
// editable; the finding names its Input cards and how to split it, and
// admission refuses every run from it with the same message.
func SeveralInputCardsFinding(board *BoardDocument) BoardFinding {
	cards := make([]string, 0, len(board.Missions))
	for _, mission := range board.Missions {
		cards = append(cards, fmt.Sprintf("%q (%s)", mission.Title, mission.ID))
	}
	return BoardFinding{
		Code: FindingSeveralInputCards,
		Message: fmt.Sprintf("mission %q holds %d Input cards, %s; a mission has one, so no run can start from it. "+
			"Split it: copy %s.mission.toml beside itself under a new slug and give the copy a new id, slug and title, "+
			"then delete from each file the Input cards, and the steps only they reach, that belong to the other",
			board.Slug, len(board.Missions), strings.Join(cards, ", "), board.Slug),
	}
}

// duplicateSlotFindings reports a slot ID that more than one slot uses. A seat's
// session is named after its run and slot, so a repeated slot ID gives two
// seats one name, and a verdict relayed by that slot names neither. Each
// formation holding the ID gets the finding, so any run reaching one of them
// is refused.
func duplicateSlotFindings(formations []FormationNode) []BoardFinding {
	holders := map[string][]string{}
	var order []string
	for _, formation := range formations {
		for _, slot := range formation.Slots {
			if slot.ID == "" {
				continue
			}
			if len(holders[slot.ID]) == 0 {
				order = append(order, slot.ID)
			}
			holders[slot.ID] = append(holders[slot.ID], formation.ID)
		}
	}
	var findings []BoardFinding
	for _, slotID := range order {
		if len(holders[slotID]) < 2 {
			continue
		}
		var nodes []string
		for _, node := range holders[slotID] {
			if !hasString(nodes, node) {
				nodes = append(nodes, node)
			}
		}
		described := fmt.Sprintf("formations %s", strings.Join(quoteAll(nodes), " and "))
		if len(nodes) == 1 {
			described = fmt.Sprintf("%d slots of formation %q", len(holders[slotID]), nodes[0])
		}
		for _, node := range nodes {
			findings = append(findings, BoardFinding{
				Code:    FindingDuplicateSlotID,
				NodeID:  node,
				Message: fmt.Sprintf("slot id %q is used by %s; give every slot in the mission its own id", slotID, described),
			})
		}
	}
	return findings
}

func quoteAll(values []string) []string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fmt.Sprintf("%q", value)
	}
	return quoted
}

// gateRouteGaps names what a gate still needs before a run can route through
// it. An empty result means every declared kind has an executable route.
func gateRouteGaps(board *BoardDocument, gate GateNode) []string {
	if len(gate.Kinds) == 0 {
		return []string{"a kind: code, formation or human"}
	}
	var gaps []string
	seen := make(map[string]bool, len(gate.Kinds))
	for _, kind := range gate.Kinds {
		if seen[kind] {
			continue
		}
		seen[kind] = true
		switch kind {
		case "code":
			if gap := codeGateRouteGap(gate); gap != "" {
				gaps = append(gaps, gap)
			}
		case "formation":
			if len(judgeChainForGate(board, gate.ID)) == 0 {
				gaps = append(gaps, "a judge chain wired from its judge port through a formation and back")
			}
		case "human":
		default:
			gaps = append(gaps, fmt.Sprintf("a supported kind in place of %q (code, formation or human)", kind))
		}
	}
	return gaps
}

// codeGateRouteGap applies the run preflight's code check rules
// (preflightSelectedCodeGates) and names the missing or malformed part.
func codeGateRouteGap(gate GateNode) string {
	check, version := strings.TrimSpace(gate.Check), strings.TrimSpace(gate.CheckVersion)
	registered := strings.Join(knownCodeGateProfiles(), " or ")
	if check == "" && version == "" {
		return "a code check (" + registered + ")"
	}
	descriptor, ok := LookupCodeGateProfileDescriptor(check, version)
	switch {
	case !ok && version == "":
		return fmt.Sprintf("a version for code check %q", check)
	case !ok && check == "":
		return fmt.Sprintf("a code check for version %q", version)
	case !ok:
		return fmt.Sprintf("a registered code check in place of unknown %s@%s (%s)", check, version, registered)
	}
	if err := validateCodeGateProfileDescriptor(descriptor); err != nil {
		return fmt.Sprintf("an admissible code check: %v", err)
	}
	if strings.TrimSpace(gate.CheckValue) == "" {
		return fmt.Sprintf("%s for code check %s@%s", strings.ToLower(descriptor.ParameterLabel), descriptor.ProfileID, descriptor.ProfileVersion)
	}
	return ""
}

func sortFindings(findings []BoardFinding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		if findings[i].NodeID != findings[j].NodeID {
			return findings[i].NodeID < findings[j].NodeID
		}
		return findings[i].Message < findings[j].Message
	})
}

// routeLeadsNowhereFindings reports every formation output and gate pass or
// fail port with no wire (archon-o7p.10). Every route leads to a step, a gate
// or an End node, so no path stops by accident. Judge formations report too:
// their output returns the verdict to the gate's judge port.
func routeLeadsNowhereFindings(board *BoardDocument) []BoardFinding {
	wired := make(map[string]bool, len(board.Connections))
	for _, connection := range board.Connections {
		wired[connection.From] = true
	}
	var findings []BoardFinding
	add := func(nodeID, route string) {
		findings = append(findings, BoardFinding{
			Code:    FindingRouteLeadsNowhere,
			NodeID:  nodeID,
			Message: fmt.Sprintf("%s %s leads nowhere: wire it to a step or an End node", possessive(nodeName(board, nodeID)), route),
		})
	}
	for _, formation := range board.Formations {
		for _, port := range formation.Outputs {
			if wired[formation.ID+":"+port.ID] {
				continue
			}
			route := "output"
			if len(formation.Outputs) > 1 {
				route = fmt.Sprintf("%q output", portLabel(port))
			}
			add(formation.ID, route)
		}
	}
	for _, gate := range board.Gates {
		for _, port := range []string{"pass", "fail"} {
			if !wired[gate.ID+":"+port] {
				add(gate.ID, port+" route")
			}
		}
	}
	return findings
}

// unreachableNodeFindings warns about every step, gate and End node that no
// path from the Input card reaches, so a run would never get there. A judge
// is reached through its gate's judge port.
func unreachableNodeFindings(board *BoardDocument) []BoardFinding {
	if len(board.Missions) == 0 {
		return nil
	}
	reached := map[string]bool{}
	for _, mission := range board.Missions {
		for id := range reachableNodeIDs(board, mission.ID) {
			reached[id] = true
		}
	}
	var findings []BoardFinding
	add := func(nodeID, kind string) {
		if reached[nodeID] {
			return
		}
		findings = append(findings, BoardFinding{
			Code:    FindingUnreachableNode,
			NodeID:  nodeID,
			Message: fmt.Sprintf("No path from the Input card reaches %s %s, so no run will get there; wire a route into it or delete it", kind, nodeName(board, nodeID)),
		})
	}
	for _, formation := range board.Formations {
		add(formation.ID, "step")
	}
	for _, gate := range board.Gates {
		add(gate.ID, "gate")
	}
	for _, end := range board.Ends {
		add(end.ID, "End node")
	}
	return findings
}

// nodeName names a node for the operator by its title, or its ID when it has
// none.
func nodeName(board *BoardDocument, nodeID string) string {
	if title := strings.TrimSpace(boardNodeTitle(board, nodeID)); title != "" {
		return title
	}
	return nodeID
}

// possessive is "Brief sign-off's", or "Research notes'" for a name ending in s.
func possessive(name string) string {
	if strings.HasSuffix(name, "s") || strings.HasSuffix(name, "S") {
		return name + "'"
	}
	return name + "'s"
}

func portLabel(port FormationPort) string {
	if strings.TrimSpace(port.Label) != "" {
		return port.Label
	}
	return port.ID
}
