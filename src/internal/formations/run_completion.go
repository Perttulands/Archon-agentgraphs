package formations

import "slices"

// When a run has nothing left to do, and how it ended (form-n7u.53,
// form-o7p.10). The engine, before it finishes a run, and a human gate's
// routes, before they say approving ends the run, ask the same question of
// the same ledger through runPaths, so the two cannot disagree.
//
// The answer follows deliveries, not the board's shape: a formation reached
// only through a fail port no verdict took is not work, and a formation fed
// again after its last output (a send-back, or a new output upstream) is.
// Every route leads somewhere, so a path ends only by a delivery to an End
// node; the run finishes when nothing else can run, and fails when a path
// ended at a rejected End node.

// endedPath is one delivery to an End node: the path that reached it ended
// there. Reason and GateID come from the gate verdict that routed there, and
// Actor is who gave that verdict (the operator, for a human gate); a step's
// output carries no reason or gate.
type endedPath struct {
	EndID   string
	Outcome string
	Title   string
	GateID  string
	Reason  string
	Actor   string
	Seq     int
}

// runPathState is the ledger read through the board: the formations and
// gates owed a delivery they have not acted on, and every path that ended.
type runPathState struct {
	owed  []string
	ended []endedPath
}

// runPaths reads which formations and gates have received a delivery in the
// ledger and not acted on it since (a formation with no output after it, or a
// gate with no evaluation after it), and which deliveries reached an End
// node. Deliveries are a node's outputs on its wired ports (judge formations
// excluded; their outputs are gate evidence) and a gate verdict's routes. A
// formation that received only some of its inputs is owed too; it cannot run,
// and the engine blocks it as starved rather than finishing.
func runPaths(board *BoardDocument, events []RunEvent) runPathState {
	var state runPathState
	if board == nil {
		return state
	}
	judges := map[string]bool{}
	isGate := map[string]bool{}
	for _, gate := range board.Gates {
		isGate[gate.ID] = true
		for _, judge := range judgeChainForGate(board, gate.ID) {
			judges[judge.ID] = true
		}
	}
	isFormation := map[string]bool{}
	for _, formation := range board.Formations {
		isFormation[formation.ID] = true
	}
	deliver := func(target string, from RunEvent, gateID string) {
		if end, ok := findEnd(board, target); ok {
			path := endedPath{EndID: end.ID, Outcome: end.Outcome, Title: end.Title, GateID: gateID, Actor: from.Actor, Seq: from.Seq}
			if gateID != "" {
				path.Reason = stringFromEventData(from, "reason")
			}
			state.ended = append(state.ended, path)
			return
		}
		if (isFormation[target] || isGate[target]) && !judges[target] && !slices.Contains(state.owed, target) {
			state.owed = append(state.owed, target)
		}
	}
	// A human gate's verdict is the operator's: the path it ends names them.
	humanActor := map[string]string{}
	for _, event := range events {
		gateID := event.GateID
		if gateID == "" {
			gateID = event.NodeID
		}
		switch event.Type {
		case RunEventHumanVerdictRecorded:
			humanActor[gateID] = event.Actor
		case RunEventNodeOutput:
			state.owed = slices.DeleteFunc(state.owed, func(id string) bool { return id == event.NodeID })
			if judges[event.NodeID] {
				continue
			}
			for _, connection := range outgoingConnections(board.Connections, event.NodeID) {
				_, fromPort := endpointParts(connection.From)
				if _, ok := outputPayloadForPortFromEvent(event, fromPort); !ok {
					continue
				}
				target, _ := endpointParts(connection.To)
				deliver(target, event, "")
			}
		case RunEventGateEvaluating:
			state.owed = slices.DeleteFunc(state.owed, func(id string) bool { return id == gateID })
		case RunEventGateVerdict:
			routePort := stringFromEventData(event, "routePort")
			if routePort != "pass" && routePort != "fail" {
				continue
			}
			from := event
			if actor := humanActor[gateID]; actor != "" {
				from.Actor = actor
				delete(humanActor, gateID)
			}
			for _, route := range gateVerdictRoutes(board, event, gateID, routePort) {
				target, _ := endpointParts(route.To)
				deliver(target, from, gateID)
			}
		}
	}
	return state
}

// owedRunWork lists the formations and gates owed a delivery (see runPaths).
func owedRunWork(board *BoardDocument, events []RunEvent) []string {
	return runPaths(board, events).owed
}

// unfinishedRunWork is the one rule for whether anything in a run can still
// run: the nodes that are owed a delivery (see runPaths) or still open (a
// formation started without output, a gate evaluating or waiting for its
// human verdict). except names one node to leave out, such as the human gate
// whose approval is being considered. An empty result means every path has
// ended and nothing else can run, so the run is finished.
func unfinishedRunWork(board *BoardDocument, events []RunEvent, except string) []string {
	unfinished := []string{}
	for _, nodeID := range append(owedRunWork(board, events), runOpenNodesAt(events, len(events))...) {
		if nodeID != except && !slices.Contains(unfinished, nodeID) {
			unfinished = append(unfinished, nodeID)
		}
	}
	return unfinished
}

// rejectedRunPath is the first path that ended at a rejected End node, which
// fails a finished run with its gate's reason; nil means the run succeeds.
func rejectedRunPath(board *BoardDocument, events []RunEvent) *endedPath {
	for _, path := range runPaths(board, events).ended {
		if path.Outcome == EndOutcomeRejected {
			return &path
		}
	}
	return nil
}

// runWorkOwedTo reports whether a formation still owes the run a delivery it
// received, so resume runs it; one that already acted on its last delivery
// is not run again.
func runWorkOwedTo(board *BoardDocument, events []RunEvent, nodeID string) bool {
	return slices.Contains(owedRunWork(board, events), nodeID)
}
