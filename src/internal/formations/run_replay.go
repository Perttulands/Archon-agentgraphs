package formations

import (
	"slices"
)

// Replay (archon-1uyi). Steps whose inputs are ready run at the same time, so
// no fixed order says what a run did; its ledger does. A run's state at any
// point is its ledger read in order through these rules, which the run's
// worker reads before every decision and a restart reads again, so the two
// cannot disagree:
//
//   - A step reads its ports when it starts: node_started records the inputs
//     it took and marks its ports read. A delivery that reaches a step while
//     it works waits unread, and the step runs again after its output.
//   - A step's output delivers at its node_output: to each wired step's port,
//     into each wired gate's queue, and to each End node.
//   - A gate takes its queue oldest first, one input at a time: gate_evaluating
//     takes the oldest, and the evaluation ends at its gate_verdict, whose
//     routes deliver there, or at its request to the operator.
//   - An attempt without output, cut short by a restart, a failure or a
//     limit, runs again on the inputs it started with, before the step reads
//     anything newer: a start while the step has an attempt open is that
//     rerun, and reads no port.
//   - A pick runs from its pick_started, which settles the step's ports, to
//     its verdict or its request to the operator (archon-gihe).
//
// Replay writes nothing. Whatever the ledger leaves open, the run's worker
// takes up from here: open attempts run again, open evaluations and picks
// finish from the results the ledger holds, and queued gate inputs and unread
// ports run.
type runReplay struct {
	runID      string
	board      *BoardDocument
	gates      map[string]GateNode
	formations map[string]FormationNode
	judges     map[string]bool
	// ports holds what each step's input ports hold.
	ports map[string]map[string]RunInputRef
	// attempts is each step's latest attempt.
	attempts map[string]int
	// open holds each step's attempt that started and has no output yet.
	open map[string]openAttempt
	// queue holds each gate's deliveries not yet evaluated, oldest first.
	queue map[string][]RunInputRef
	// evaluating holds each gate's evaluation that has not ended.
	evaluating map[string]RunEvent
	// picks holds the pick_started of each pick that has not ended, oldest
	// first.
	picks []RunEvent
	// owed lists, in the order they became owed, the steps with unread
	// deliveries and the gates with queued ones.
	owed []string
	// ended lists every delivery to an End node, the path that ended there.
	ended []endedPath
	// fed marks each step's ports that have ever received a delivery.
	fed map[string]map[string]bool
	// waiting maps a delivering event's seq to the steps its deliveries left
	// waiting for their other inputs.
	waiting map[int][]string
	// humanActor is who gave each gate's latest recorded human verdict.
	humanActor map[string]string
	// err is the first contradiction the ledger holds; replay goes on past it
	// so readers that only count paths still can.
	err error
}

// openAttempt is a step's attempt that started and has no output.
type openAttempt struct {
	Attempt int
	Inputs  []RunInputRef
	Seq     int
}

// replayRun reads a run's ledger through the board (see runReplay).
func replayRun(board *BoardDocument, events []RunEvent) *runReplay {
	r := &runReplay{
		board:      board,
		gates:      map[string]GateNode{},
		formations: map[string]FormationNode{},
		judges:     map[string]bool{},
		ports:      map[string]map[string]RunInputRef{},
		attempts:   map[string]int{},
		open:       map[string]openAttempt{},
		queue:      map[string][]RunInputRef{},
		evaluating: map[string]RunEvent{},
		fed:        map[string]map[string]bool{},
		waiting:    map[int][]string{},
		humanActor: map[string]string{},
	}
	if board == nil {
		return r
	}
	if len(events) > 0 {
		r.runID = events[0].RunID
	}
	for _, gate := range board.Gates {
		r.gates[gate.ID] = gate
		for _, judge := range judgeChainForGate(board, gate.ID) {
			r.judges[judge.ID] = true
		}
	}
	for _, formation := range board.Formations {
		r.formations[formation.ID] = formation
	}
	for i := range events {
		r.apply(events[:i+1])
	}
	return r
}

// apply reads the last of events, the ledger up to and including it.
func (r *runReplay) apply(events []RunEvent) {
	event := events[len(events)-1]
	gateID := eventGateID(event)
	switch event.Type {
	case RunEventHumanVerdictRecorded:
		r.humanActor[gateID] = event.Actor
	case RunEventNodeStarted:
		if _, step := r.formations[event.NodeID]; !step || r.judges[event.NodeID] {
			return
		}
		r.attempts[event.NodeID] = max(r.attempts[event.NodeID], event.Attempt)
		if _, rerun := r.open[event.NodeID]; !rerun {
			markPortsRead(r.ports, event.NodeID)
			r.settle(event.NodeID)
		}
		r.open[event.NodeID] = openAttempt{Attempt: event.Attempt, Inputs: runInputRefsFromAny(event.Data["inputRefs"]), Seq: event.Seq}
	case RunEventNodeOutput:
		delete(r.open, event.NodeID)
		if r.judges[event.NodeID] {
			// A judge's output is its gate's evidence, never a delivery.
			return
		}
		for _, connection := range outgoingConnections(r.board.Connections, event.NodeID) {
			_, fromPort := endpointParts(connection.From)
			payload, ok := outputPayloadForPortFromEvent(event, fromPort)
			if !ok {
				continue
			}
			r.deliver(event, connection, runInputRefForConnection(r.runID, connection, payload, event.Attempt), "")
		}
	case RunEventPickStarted:
		// The pick settles the step's send-back, as its output would; the
		// attempt a limit cut short is never run again.
		markPortsRead(r.ports, event.NodeID)
		delete(r.open, event.NodeID)
		r.settle(event.NodeID)
		r.picks = append(r.picks, event)
	case RunEventGateEvaluating:
		if queued := r.queue[gateID]; len(queued) > 0 {
			r.queue[gateID] = queued[1:]
		}
		if len(r.queue[gateID]) == 0 {
			r.settle(gateID)
		}
		r.evaluating[gateID] = event
	case RunEventHumanInputRequested:
		if isPickEvent(event) {
			r.endPick(intFromRunEventData(pickData(event)["pickSeq"]))
			return
		}
		delete(r.evaluating, gateID)
	case RunEventGateVerdict:
		if isPickEvent(event) {
			r.endPick(intFromRunEventData(pickData(event)["pickSeq"]))
		} else if intFromRunEventData(event.Data["requestedSeq"]) == 0 {
			delete(r.evaluating, gateID)
		}
		r.route(events, event, gateID)
	}
}

// route delivers a gate verdict's routes: a pass carries the gate's input on,
// with the operator's response when one was given; a fail sends it back as
// feedback.
func (r *runReplay) route(events []RunEvent, event RunEvent, gateID string) {
	routePort := stringFromEventData(event, "routePort")
	if routePort != "pass" && routePort != "fail" {
		return
	}
	// A human gate's verdict is the operator's: the path it ends names them.
	from := event
	if actor := r.humanActor[gateID]; actor != "" {
		from.Actor = actor
		delete(r.humanActor, gateID)
	}
	input := runInputRefFromAny(event.Data["inputRef"])
	for _, route := range gateVerdictRoutes(r.board, event, gateID, routePort) {
		next := input
		if routePort == "fail" {
			next = gateFailInput(r.runID, route, gateID, event.Attempt, input, stringFromEventData(event, "reason"), gateEvidenceRefsFromRunEventData(event.Data["evidence"]))
		} else if passed, err := gatePassInput(events, event, gateID, input); err != nil {
			if r.err == nil {
				r.err = err
			}
		} else {
			next = passed
		}
		r.deliver(from, route, next, gateID)
	}
}

// deliver makes one delivery: into a step's port, a gate's queue or an End
// node, where the path ends.
func (r *runReplay) deliver(from RunEvent, connection BoardConnection, input RunInputRef, gateID string) {
	toNode, toPort := endpointParts(connection.To)
	if toNode == "" || toPort == "" {
		return
	}
	input.ToPortID = toPort
	if _, ok := r.gates[toNode]; ok {
		r.queue[toNode] = append(r.queue[toNode], input)
		r.owe(toNode)
		return
	}
	if end, ok := findEnd(r.board, toNode); ok {
		path := endedPath{EndID: end.ID, Outcome: end.Outcome, Title: end.Title, GateID: gateID, Actor: from.Actor, Seq: from.Seq}
		if gateID != "" {
			path.Reason = stringFromEventData(from, "reason")
		}
		r.ended = append(r.ended, path)
		return
	}
	formation, ok := r.formations[toNode]
	if !ok || r.judges[toNode] {
		return
	}
	if r.fed[toNode] == nil {
		r.fed[toNode] = map[string]bool{}
	}
	r.fed[toNode][toPort] = true
	deliverToPort(r.ports, toNode, toPort, input)
	r.owe(toNode)
	if !formationReady(formation, r.ports[toNode]) && !slices.Contains(r.waiting[from.Seq], toNode) {
		r.waiting[from.Seq] = append(r.waiting[from.Seq], toNode)
	}
}

func (r *runReplay) owe(nodeID string) {
	if !slices.Contains(r.owed, nodeID) {
		r.owed = append(r.owed, nodeID)
	}
}

func (r *runReplay) settle(nodeID string) {
	r.owed = slices.DeleteFunc(r.owed, func(id string) bool { return id == nodeID })
}

func (r *runReplay) endPick(seq int) {
	r.picks = slices.DeleteFunc(r.picks, func(started RunEvent) bool { return started.Seq == seq })
}

// unread reports whether a step's ports hold a delivery it has not run on.
func (r *runReplay) unread(nodeID string) bool {
	for _, input := range r.ports[nodeID] {
		if input.unread {
			return true
		}
	}
	return false
}

// picking reports whether a pick of the step's best attempt has not ended.
func (r *runReplay) picking(nodeID string) bool {
	return slices.ContainsFunc(r.picks, func(started RunEvent) bool { return started.NodeID == nodeID })
}

// pickingAt reports whether a pick by the gate has not ended.
func (r *runReplay) pickingAt(gateID string) bool {
	return slices.ContainsFunc(r.picks, func(started RunEvent) bool { return started.GateID == gateID })
}
