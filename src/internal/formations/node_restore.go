package formations

import (
	"errors"
	"fmt"
	"strconv"
)

// ErrInvalidNodeRestore rejects a restore that would not put back the node
// exactly as it was: its ID is taken, a connection no longer fits, or a field
// is malformed.
var ErrInvalidNodeRestore = errors.New("invalid_node_restore")

// NodeRestoreRequest puts back one deleted mission, formation or gate with its
// own IDs, fields, connections and layout position, as the board document
// showed it before the delete. It is how the cockpit undoes a node delete.
// Notes are keyed by node ID and survive the delete, so the restored node
// finds its notes again; lane routing kept for its connection IDs returns too.
type NodeRestoreRequest struct {
	Mission     *MissionNode
	Formation   *FormationNode
	Gate        *GateNode
	Connections []BoardConnection
	X           int
	Y           int
	UpdatedBy   string
}

type NodeRestoreResult struct {
	Board  *BoardDocument  `json:"board"`
	Layout *LayoutDocument `json:"layout"`
	NodeID string          `json:"nodeId"`
}

// RestoreNode publishes the node, its connections and its layout position as
// one board+layout pair, or nothing. Every connection must touch the node and
// still fit the board; otherwise the whole restore is refused.
func (s *Store) RestoreNode(slug string, req NodeRestoreRequest, opts WriteOptions) (*NodeRestoreResult, error) {
	if err := validateSlug(slug); err != nil {
		return nil, err
	}
	if opts.ExpectedETag == "" || opts.ExpectedRev == 0 {
		return nil, ErrPreconditionRequired
	}
	nodeID, appendNode, err := restoredNodeBlock(req)
	if err != nil {
		return nil, err
	}
	var connections []BoardConnection
	board, layout, err := s.createNode(slug, opts, nodeCreateCandidate{
		prepare: func(raw []byte, current *BoardDocument) error {
			planned, err := planRestoredConnections(raw, current, nodeID, req, appendNode)
			connections = planned
			return err
		},
		appendBoardBlock: func(raw []byte) []byte {
			raw = appendNode(raw)
			for _, connection := range connections {
				raw = appendConnectionBlock(raw, connection)
			}
			return raw
		},
		node:      LayoutNode{ID: nodeID, X: req.X, Y: req.Y},
		updatedBy: req.UpdatedBy,
	}, nil)
	if err != nil {
		return nil, err
	}
	return &NodeRestoreResult{Board: board, Layout: layout, NodeID: nodeID}, nil
}

func invalidNodeRestore(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidNodeRestore}, args...)...)
}

// restoredNodeBlock validates the node on its own and returns how to append it.
// It applies the same field rules as creating and editing that kind of node.
func restoredNodeBlock(req NodeRestoreRequest) (string, func([]byte) []byte, error) {
	count := 0
	for _, present := range []bool{req.Mission != nil, req.Formation != nil, req.Gate != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return "", nil, invalidNodeRestore("name exactly one mission, formation or gate")
	}
	switch {
	case req.Formation != nil:
		formation := *req.Formation
		if err := validateRestoredFormation(formation); err != nil {
			return "", nil, err
		}
		return formation.ID, func(raw []byte) []byte { return appendRestoredFormationBlock(raw, formation) }, nil
	case req.Gate != nil:
		gate := *req.Gate
		if !validToolDefinitionID(gate.ID) {
			return "", nil, invalidNodeRestore("gate id %q is invalid", gate.ID)
		}
		if err := rejectLegacyScriptGateWrite(false, gate.Command, gate.CommandArgv, gate.CommandCWD, gate.CommandShell); err != nil {
			return "", nil, err
		}
		kinds, err := normalizeGateKinds(gate.Kinds)
		if err != nil {
			return "", nil, err
		}
		if err := validateCodeGateAuthoring(gate.Check, gate.CheckVersion); err != nil {
			return "", nil, err
		}
		gate.Kinds = kinds
		gate.Files = normalizeFileRefs(gate.Files)
		return gate.ID, func(raw []byte) []byte { return appendGateBlock(raw, gate) }, nil
	default:
		mission := *req.Mission
		if !validToolDefinitionID(mission.ID) {
			return "", nil, invalidNodeRestore("mission id %q is invalid", mission.ID)
		}
		if mission.BeadID != "" && !isSafeBeadsIssueID(mission.BeadID) {
			return "", nil, invalidBeadID("mission beadId", mission.BeadID)
		}
		channel, err := NormalizeHumanChannel(mission.HumanChannel)
		if err != nil {
			return "", nil, err
		}
		mission.HumanChannel = channel
		mission.Files = normalizeFileRefs(mission.Files)
		return mission.ID, func(raw []byte) []byte { return appendMissionBlock(raw, mission) }, nil
	}
}

func validateRestoredFormation(formation FormationNode) error {
	if !validToolDefinitionID(formation.ID) {
		return invalidNodeRestore("formation id %q is invalid", formation.ID)
	}
	if err := validateFormationType(formation.Type); err != nil {
		return err
	}
	if formation.Verification != nil {
		return fmt.Errorf("%w: formation %q carries retired inline verification, which cannot be authored again", ErrLegacyInlineVerificationRequiresMigration, formation.ID)
	}
	if formation.Brief != nil && formation.Brief.BeadID != "" && !isSafeBeadsIssueID(formation.Brief.BeadID) {
		return invalidBeadID("brief beadId", formation.Brief.BeadID)
	}
	if formation.Execution != nil && !validExecutionSeconds(formation.Execution.TimeoutSeconds) {
		return fmt.Errorf("%w: timeoutSeconds must be a positive whole number of seconds", ErrInvalidExecutionPolicy)
	}
	ports := map[string]bool{}
	for _, port := range append(append([]FormationPort(nil), formation.Inputs...), formation.Outputs...) {
		if !validToolDefinitionID(port.ID) || ports[port.ID] {
			return invalidNodeRestore("formation %q port id %q is missing, invalid or repeated", formation.ID, port.ID)
		}
		ports[port.ID] = true
	}
	slots := map[string]bool{}
	for _, slot := range formation.Slots {
		if !validToolDefinitionID(slot.ID) || slots[slot.ID] {
			return invalidNodeRestore("formation %q slot id %q is missing, invalid or repeated", formation.ID, slot.ID)
		}
		slots[slot.ID] = true
	}
	return nil
}

// appendRestoredFormationBlock writes the ports and slots as creation does,
// then the brief and execution sections as setBrief and setExecution do.
func appendRestoredFormationBlock(raw []byte, formation FormationNode) []byte {
	next := appendFormationBlock(raw, formation)
	var sections []tomlLine
	if formation.Brief != nil {
		sections = append(sections, renderBriefSection(FormationBriefRequest{
			Goal:   formation.Brief.Goal,
			BeadID: formation.Brief.BeadID,
			Files:  formation.Brief.Files,
			Links:  formation.Brief.Links,
		})...)
	}
	if formation.Execution != nil {
		sections = append(sections,
			tomlLine{body: "[formation.execution]", newline: "\n"},
			tomlLine{body: "timeoutSeconds = " + strconv.Itoa(formation.Execution.TimeoutSeconds), newline: "\n"},
		)
	}
	if len(sections) == 0 {
		return next
	}
	lines := splitLines(next)
	start, end, ok := findFormationBlockByID(lines, formation.ID)
	if !ok {
		return next
	}
	return renderTOMLLines(insertTomLLines(lines, formationHeaderEnd(lines, start, end), sections))
}

// planRestoredConnections checks the node against the current board and
// returns the connections to append, keeping their IDs where still free.
func planRestoredConnections(raw []byte, current *BoardDocument, nodeID string, req NodeRestoreRequest, appendNode func([]byte) []byte) ([]BoardConnection, error) {
	if nodeIDTaken(current, nodeID) {
		return nil, invalidNodeRestore("node %q is already on the board", nodeID)
	}
	if req.Formation != nil {
		for _, slot := range req.Formation.Slots {
			for _, other := range current.Formations {
				if _, taken := findSlot(other.Slots, slot.ID); taken {
					return nil, invalidNodeRestore("slot id %q is already used by formation %q", slot.ID, other.ID)
				}
			}
		}
	}
	withNode := appendNode(raw)
	board, err := parseBoardForWrite(withNode)
	if err != nil {
		return nil, err
	}
	edgeIDs := map[string]bool{}
	for _, connection := range current.Connections {
		edgeIDs[connection.ID] = true
	}
	existing := append([]BoardConnection(nil), current.Connections...)
	planned := make([]BoardConnection, 0, len(req.Connections))
	for _, requested := range req.Connections {
		from, to := requested.From, requested.To
		fromNode, fromOK := endpointAllowsDirection(withNode, from, FormationPortOutput)
		toNode, toOK := endpointAllowsDirection(withNode, to, FormationPortInput)
		if !fromOK || !toOK {
			return nil, invalidNodeRestore("connection %s → %s no longer has both ends on the board", from, to)
		}
		if fromNode != nodeID && toNode != nodeID {
			return nil, invalidNodeRestore("connection %s → %s does not touch node %q", from, to, nodeID)
		}
		candidate := BoardConnection{ID: requested.ID, From: from, To: to}
		duplicate, err := validateConnectionCandidate(existing, board.Gates, candidate)
		if err != nil {
			return nil, err
		}
		if duplicate {
			return nil, fmt.Errorf("%w: connection %s → %s already exists", ErrDuplicateConnection, from, to)
		}
		if _, incompatible := toolConnectionCompatibilityFinding(board, candidate); incompatible {
			return nil, ErrIncompatibleToolConnection
		}
		if !validToolDefinitionID(candidate.ID) || edgeIDs[candidate.ID] {
			candidate.ID = newPrefixedID("edge")
		}
		edgeIDs[candidate.ID] = true
		existing = append(existing, candidate)
		planned = append(planned, candidate)
	}
	return planned, nil
}

func nodeIDTaken(board *BoardDocument, id string) bool {
	for _, mission := range board.Missions {
		if mission.ID == id {
			return true
		}
	}
	for _, formation := range board.Formations {
		if formation.ID == id {
			return true
		}
	}
	for _, gate := range board.Gates {
		if gate.ID == id {
			return true
		}
	}
	for _, tool := range board.Tools {
		if tool.ID == id {
			return true
		}
	}
	return false
}
