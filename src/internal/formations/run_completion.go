package formations

import "slices"

// When a run has nothing left to do (form-n7u.53). The engine, before it
// records success, and a human gate's routes, before they say approving ends
// the run, ask the same question of the same ledger through
// unfinishedRunWork, so the two cannot disagree.
//
// The answer follows deliveries, not the board's shape: a formation reached
// only through a fail port no verdict took is not work, and a formation fed
// again after its last output (a send-back, or a new output upstream) is.

// owedRunWork lists the formations and gates that have received a delivery in
// the ledger and not acted on it since: a formation with no output after it,
// or a gate with no evaluation after it. Deliveries are a node's outputs on
// its wired ports (judge formations excluded; their outputs are gate
// evidence) and a gate verdict's routes. A formation that received only some
// of its inputs is owed too; it cannot run, and the engine blocks it as
// starved rather than finishing.
func owedRunWork(board *BoardDocument, events []RunEvent) []string {
	if board == nil {
		return nil
	}
	judges := map[string]bool{}
	for _, gate := range board.Gates {
		for _, judge := range judgeChainForGate(board, gate.ID) {
			judges[judge.ID] = true
		}
	}
	isGate := map[string]bool{}
	for _, gate := range board.Gates {
		isGate[gate.ID] = true
	}
	isFormation := map[string]bool{}
	for _, formation := range board.Formations {
		isFormation[formation.ID] = true
	}
	var owed []string
	deliver := func(target string) {
		if (isFormation[target] || isGate[target]) && !judges[target] && !slices.Contains(owed, target) {
			owed = append(owed, target)
		}
	}
	for _, event := range events {
		gateID := event.GateID
		if gateID == "" {
			gateID = event.NodeID
		}
		switch event.Type {
		case RunEventNodeOutput:
			owed = slices.DeleteFunc(owed, func(id string) bool { return id == event.NodeID })
			if judges[event.NodeID] {
				continue
			}
			for _, connection := range outgoingConnections(board.Connections, event.NodeID) {
				_, fromPort := endpointParts(connection.From)
				if _, ok := outputPayloadForPortFromEvent(event, fromPort); !ok {
					continue
				}
				target, _ := endpointParts(connection.To)
				deliver(target)
			}
		case RunEventGateEvaluating:
			owed = slices.DeleteFunc(owed, func(id string) bool { return id == gateID })
		case RunEventGateVerdict:
			routePort := stringFromEventData(event, "routePort")
			if routePort != "pass" && routePort != "fail" {
				continue
			}
			for _, route := range gateVerdictRoutes(board, event, gateID, routePort) {
				target, _ := endpointParts(route.To)
				deliver(target)
			}
		}
	}
	return owed
}

// unfinishedRunWork is the one rule for whether anything in a run can still
// run: the nodes that are owed a delivery (see owedRunWork) or still open
// (a formation started without output, a gate evaluating or waiting for its
// human verdict). except names one node to leave out, such as the human gate
// whose approval is being considered. An empty result means nothing else can
// still run.
func unfinishedRunWork(board *BoardDocument, events []RunEvent, except string) []string {
	unfinished := []string{}
	for _, nodeID := range append(owedRunWork(board, events), runOpenNodesAt(events, len(events))...) {
		if nodeID != except && !slices.Contains(unfinished, nodeID) {
			unfinished = append(unfinished, nodeID)
		}
	}
	return unfinished
}

// runWorkOwedTo reports whether a formation still owes the run a delivery it
// received, so resume runs it; one that already acted on its last delivery
// is not run again.
func runWorkOwedTo(board *BoardDocument, events []RunEvent, nodeID string) bool {
	return slices.Contains(owedRunWork(board, events), nodeID)
}
