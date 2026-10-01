package formations

import (
	"slices"
	"sort"
)

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
// gates owed a delivery they have not acted on, every path that ended, and
// the input ports of each formation that have ever received a delivery.
type runPathState struct {
	owed  []string
	ended []endedPath
	fed   map[string]map[string]bool
}

// runPaths reads which formations and gates have received a delivery in the
// ledger and not acted on it since (a formation with no output after it, or a
// gate with no evaluation after it), and which deliveries reached an End
// node. Deliveries are a node's outputs on its wired ports (judge formations
// excluded; their outputs are gate evidence) and a gate verdict's routes. A
// formation that received only some of its inputs is owed too; it cannot run,
// and the engine blocks it as starved rather than finishing.
func runPaths(board *BoardDocument, events []RunEvent) runPathState {
	state := runPathState{fed: map[string]map[string]bool{}}
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
	deliver := func(endpoint string, from RunEvent, gateID string) {
		target, port := endpointParts(endpoint)
		if isFormation[target] {
			if state.fed[target] == nil {
				state.fed[target] = map[string]bool{}
			}
			state.fed[target][port] = true
		}
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
				deliver(connection.To, event, "")
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
				deliver(route.To, from, gateID)
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
	return runPaths(board, events).rejected()
}

func (state runPathState) rejected() *endedPath {
	for _, path := range state.ended {
		if path.Outcome == EndOutcomeRejected {
			return &path
		}
	}
	return nil
}

// runFinish is how the one rule ends a run. Runnable is the work that can
// still run: unfinishedRunWork without the formations starved of an input,
// which received some of their inputs but can never receive the rest once
// nothing else runs. With runnable work the run goes on. Otherwise the run
// finishes: it fails with the first rejected path when one ended, because
// that rejection is why the run stops, including when it starved a join
// further on; it blocks as starved when formations wait for inputs nothing
// can deliver and no path was rejected (a wiring gap); and it succeeds when
// nothing waits.
type runFinish struct {
	runnable []string
	starved  []starvedFormation
	rejected *endedPath
}

func runFinishState(board *BoardDocument, events []RunEvent, except string) runFinish {
	state := runPaths(board, events)
	finish := runFinish{runnable: []string{}, rejected: state.rejected()}
	starving := map[string]bool{}
	for _, nodeID := range state.owed {
		formation, ok := findFormation(board.Formations, nodeID)
		if !ok {
			continue
		}
		var missing []string
		for _, input := range formation.Inputs {
			if !state.fed[nodeID][input.ID] {
				missing = append(missing, input.ID)
			}
		}
		if len(missing) > 0 {
			starving[nodeID] = true
			finish.starved = append(finish.starved, starvedFormation{ID: nodeID, Title: formation.Title, Missing: missing})
		}
	}
	sort.Slice(finish.starved, func(i, j int) bool { return finish.starved[i].ID < finish.starved[j].ID })
	for _, nodeID := range unfinishedRunWork(board, events, except) {
		if !starving[nodeID] {
			finish.runnable = append(finish.runnable, nodeID)
		}
	}
	return finish
}

// runWorkOwedTo reports whether a formation still owes the run a delivery it
// received, so resume runs it; one that already acted on its last delivery
// is not run again.
func runWorkOwedTo(board *BoardDocument, events []RunEvent, nodeID string) bool {
	return slices.Contains(owedRunWork(board, events), nodeID)
}
