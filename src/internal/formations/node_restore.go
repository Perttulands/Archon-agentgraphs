package formations

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
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
	// Index, when set, is the node's place among the board's nodes of its
	// kind, so the definition reads in its old order; nil appends it.
	Index     *int
	X         int
	Y         int
	UpdatedBy string
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
	nodeID, section, appendBlock, err := restoredNodeBlock(req)
	if err != nil {
		return nil, err
	}
	if req.Index != nil && *req.Index < 0 {
		return nil, invalidNodeRestore("node index %d is negative", *req.Index)
	}
	appendNode := func(raw []byte) []byte {
		if req.Index == nil {
			return appendBlock(raw)
		}
		return insertNodeBlock(raw, section, *req.Index, appendBlock(nil), appendBlock)
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

// insertNodeBlock puts block before the index-th top-level [[section]] table,
// or appends it with appendBlock when there are not that many.
func insertNodeBlock(raw []byte, section string, index int, block []byte, appendBlock func([]byte) []byte) []byte {
	lines := splitLines(raw)
	seen := 0
	for i, line := range lines {
		name, ok := tomlLineSectionName(line)
		if !ok || name != section || !strings.HasPrefix(strings.TrimSpace(line.body), "[[") {
			continue
		}
		if seen == index {
			body := append(splitLines(block), tomlLine{body: "", newline: "\n"})
			return renderTOMLLines(insertTomLLines(lines, i, body))
		}
		seen++
	}
	return appendBlock(raw)
}

// restoredNodeBlock validates the node on its own and returns its ID, its
// table name and how to append it. It applies the same field rules as creating
// and editing that kind of node.
func restoredNodeBlock(req NodeRestoreRequest) (string, string, func([]byte) []byte, error) {
	count := 0
	for _, present := range []bool{req.Mission != nil, req.Formation != nil, req.Gate != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return "", "", nil, invalidNodeRestore("name exactly one mission, formation or gate")
	}
	switch {
	case req.Formation != nil:
		formation := *req.Formation
		if err := validateRestoredFormation(formation); err != nil {
			return "", "", nil, err
		}
		return formation.ID, "formation", func(raw []byte) []byte { return appendRestoredFormationBlock(raw, formation) }, nil
	case req.Gate != nil:
		gate := *req.Gate
		if !validToolDefinitionID(gate.ID) {
			return "", "", nil, invalidNodeRestore("gate id %q is invalid", gate.ID)
		}
		if err := rejectLegacyScriptGateWrite(false, gate.Command, gate.CommandArgv, gate.CommandCWD, gate.CommandShell); err != nil {
			return "", "", nil, err
		}
		// The field rules of CreateGate and UpdateGate: at least one kind, a
		// registered profile when one is named, and code check fields only on a
		// gate with the code kind.
		kinds, err := normalizeGateKinds(gate.Kinds)
		if err != nil {
			return "", "", nil, err
		}
		if len(kinds) == 0 {
			return "", "", nil, fmt.Errorf("%w: gate %q must name at least one of code, formation and human", ErrInvalidGateKind, gate.ID)
		}
		if err := validateCodeGateAuthoring(gate.Check, gate.CheckVersion); err != nil {
			return "", "", nil, err
		}
		if !hasGateKind(kinds, "code") && strings.TrimSpace(gate.Check+gate.CheckVersion+gate.CheckValue) != "" {
			return "", "", nil, fmt.Errorf("%w: gate %q sets a code check without the code kind", ErrInvalidCodeGateProfile, gate.ID)
		}
		gate.Kinds = kinds
		gate.Files = normalizeFileRefs(gate.Files)
		return gate.ID, "gate", func(raw []byte) []byte { return appendGateBlock(raw, gate) }, nil
	default:
		mission := *req.Mission
		if !validToolDefinitionID(mission.ID) {
			return "", "", nil, invalidNodeRestore("mission id %q is invalid", mission.ID)
		}
		if mission.BeadID != "" && !isSafeBeadsIssueID(mission.BeadID) {
			return "", "", nil, invalidBeadID("mission beadId", mission.BeadID)
		}
		channel, err := NormalizeHumanChannel(mission.HumanChannel)
		if err != nil {
			return "", "", nil, err
		}
		mission.HumanChannel = channel
		mission.Files = normalizeFileRefs(mission.Files)
		return mission.ID, "mission", func(raw []byte) []byte { return appendMissionBlock(raw, mission) }, nil
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
	if id, bad := firstBadSlotID(formation.Slots); bad {
		return invalidNodeRestore("formation %q slot id %q is missing, invalid or repeated", formation.ID, id)
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
	touches := func(from, to string) bool { return endpointNodeID(from) == nodeID || endpointNodeID(to) == nodeID }
	return planRestoredWires(appendNode(raw), current, req.Connections, touches, fmt.Sprintf("node %q", nodeID))
}

// planRestoredWires validates connections against the board as it will be
// once the restored node or port is in withTarget. Each must touch the target
// and still fit; a connection ID already in use gets a fresh one.
func planRestoredWires(withTarget []byte, current *BoardDocument, requested []BoardConnection, touches func(from, to string) bool, target string) ([]BoardConnection, error) {
	board, err := parseBoardForWrite(withTarget)
	if err != nil {
		return nil, err
	}
	edgeIDs := map[string]bool{}
	for _, connection := range current.Connections {
		edgeIDs[connection.ID] = true
	}
	existing := append([]BoardConnection(nil), current.Connections...)
	planned := make([]BoardConnection, 0, len(requested))
	for _, wire := range requested {
		from, to := wire.From, wire.To
		_, fromOK := endpointAllowsDirection(withTarget, from, FormationPortOutput)
		_, toOK := endpointAllowsDirection(withTarget, to, FormationPortInput)
		if !fromOK || !toOK {
			missing := to
			if !fromOK {
				missing = from
			}
			gone := endpointLabel(board, missing)
			if nodeID := endpointNodeID(missing); !nodeIDTaken(board, nodeID) {
				gone = nodeID
			}
			return nil, invalidNodeRestore("what it was wired to (%s) is no longer on the board", gone)
		}
		if !touches(from, to) {
			return nil, invalidNodeRestore("connection %s → %s does not touch %s", from, to, target)
		}
		candidate := BoardConnection{ID: wire.ID, From: from, To: to}
		duplicate, err := validateConnectionCandidate(existing, board.Gates, candidate)
		if errors.Is(err, ErrInputOccupied) {
			return nil, invalidNodeRestore("%s is now fed by another wire", endpointLabel(board, to))
		}
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

// PortRestoreRequest puts back one removed formation port with its ID, label,
// place among the formation's ports of that direction, and connections. It is
// how the cockpit undoes Remove this input/output.
type PortRestoreRequest struct {
	FormationID string
	Direction   string
	Port        FormationPort
	Index       int
	Connections []BoardConnection
	UpdatedBy   string
}

// RestoreFormationPort publishes the port and its connections in one revision,
// or refuses with nothing written.
func (s *Store) RestoreFormationPort(slug string, req PortRestoreRequest, opts WriteOptions) (*BoardDocument, error) {
	if req.Direction != FormationPortInput && req.Direction != FormationPortOutput {
		return nil, fmt.Errorf("%w: port direction %q must be input or output", ErrInvalidPortDirection, req.Direction)
	}
	if !validToolDefinitionID(req.Port.ID) {
		return nil, invalidNodeRestore("port id %q is invalid", req.Port.ID)
	}
	if req.Index < 0 {
		return nil, invalidNodeRestore("port index %d is negative", req.Index)
	}
	return s.updateBoardDefinition(slug, req.UpdatedBy, opts, func(raw []byte, current *BoardDocument) ([]byte, error) {
		lines := splitLines(raw)
		formationStart, formationEnd, ok := findFormationBlockByID(lines, req.FormationID)
		if !ok {
			return nil, ErrNotFound
		}
		if _, _, taken := findFormationPortBlock(lines, formationStart, formationEnd, req.Port.ID); taken {
			return nil, invalidNodeRestore("formation %q already has port %q", req.FormationID, req.Port.ID)
		}
		section := "formation." + req.Direction
		insertAt := formationEnd
		seen := 0
		for i := formationStart + 1; i < formationEnd; i++ {
			name, ok := tomlLineSectionName(lines[i])
			if !ok || name != section {
				continue
			}
			if seen == req.Index {
				insertAt = i
				break
			}
			seen++
		}
		block := []tomlLine{
			{body: "[[" + section + "]]", newline: "\n"},
			{body: "id = " + renderString(req.Port.ID), newline: "\n"},
			{body: "label = " + renderString(req.Port.Label), newline: "\n"},
		}
		if insertAt < formationEnd {
			block = append(block, tomlLine{body: "", newline: "\n"})
		}
		withPort := renderTOMLLines(insertTomLLines(lines, insertAt, block))
		endpoint := req.FormationID + ":" + req.Port.ID
		touches := func(from, to string) bool { return from == endpoint || to == endpoint }
		wires, err := planRestoredWires(withPort, current, req.Connections, touches, "port "+endpoint)
		if err != nil {
			return nil, err
		}
		for _, wire := range wires {
			withPort = appendConnectionBlock(withPort, wire)
		}
		return withPort, nil
	})
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

// endpointLabel names a connection end for the operator: the node's title and
// the port's label when the board still has them, else the raw endpoint.
func endpointLabel(board *BoardDocument, endpoint string) string {
	nodeID, portID, ok := splitEndpoint(endpoint)
	if !ok {
		return endpoint
	}
	title, port := "", portID
	for _, mission := range board.Missions {
		if mission.ID == nodeID {
			title = mission.Title
		}
	}
	for _, gate := range board.Gates {
		if gate.ID == nodeID {
			title = gate.Title
		}
	}
	for _, formation := range board.Formations {
		if formation.ID != nodeID {
			continue
		}
		title = formation.Title
		for _, candidate := range append(append([]FormationPort(nil), formation.Inputs...), formation.Outputs...) {
			if candidate.ID == portID && candidate.Label != "" {
				port = candidate.Label
			}
		}
	}
	if title == "" {
		return endpoint
	}
	return fmt.Sprintf("%q (%s)", title, port)
}
