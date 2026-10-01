package formations

// Where a human gate's answer leads (form-n7u.7). The operator decides with the
// consequence in view: the steps each verdict delivers to on the run's frozen
// board, whether a verdict ends the run, and whether the step a verdict starts
// would take the last of a limit or find it already spent, which blocks the run
// instead (form-n7u.6). The routes follow routeGateVerdict and the engine's
// attempt and dispatch counting.

// GateRouteTarget is a step, gate or End node a verdict delivers to. A
// formation also says which attempt it would start, the run's attempt limit
// (maxAttempts, omitted when the run set none and attempts are unlimited), and
// whether it still waits for other inputs. An End node says its outcome: the
// path ends there, done or rejected.
type GateRouteTarget struct {
	NodeID      string `json:"nodeId"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Outcome     string `json:"outcome,omitempty"`
	Attempt     int    `json:"attempt,omitempty"`
	MaxAttempts int    `json:"maxAttempts,omitempty"`
	// WaitsForInputs marks a join that receives this and still waits for
	// another input no step has delivered yet.
	WaitsForInputs bool `json:"waitsForInputs,omitempty"`
}

// GateRoute is what one verdict does. Every route leads somewhere, so its
// targets are never empty on an admitted mission. EndsRun marks a verdict
// whose every route ends its path at an End node while nothing else in the
// run can still run, so the run finishes (the engine's rule, runFinishState).
// RunFails marks a verdict after which the run fails when it ends: one of its
// End nodes is rejected, or a rejected path already ended. With EndsRun it
// fails now; without, once the rest of its open work has ended. Limit is a
// limit the route finds spent, so taking it blocks the run. Dispatches is the run's dispatch use so far and DispatchesNeeded the
// formation starts the route makes, judges included, under a dispatch limit.
type GateRoute struct {
	Verdict          string            `json:"verdict"`
	Targets          []GateRouteTarget `json:"targets"`
	EndsRun          bool              `json:"endsRun,omitempty"`
	RunFails         bool              `json:"runFails,omitempty"`
	Limit            *RunLimitReached  `json:"limit,omitempty"`
	Dispatches       *RunLimitReached  `json:"dispatches,omitempty"`
	DispatchesNeeded int               `json:"dispatchesNeeded,omitempty"`
}

// HumanGateRoutes reports the pass and fail routes of a gate as the run stands.
func HumanGateRoutes(board *BoardDocument, events []RunEvent, gateID string) []GateRoute {
	limits := RunLimits{}
	if len(events) > 0 && events[0].Type == RunEventStarted {
		limits = runLimitsFromEvent(events[0])
	}
	consumed := formationStartsBefore(events, len(events))
	routes := make([]GateRoute, 0, 2)
	for _, verdict := range []string{"pass", "fail"} {
		route := GateRoute{Verdict: verdict, Targets: []GateRouteTarget{}}
		connections := outgoingConnectionsFromPort(board.Connections, gateID, verdict)
		for _, connection := range connections {
			nodeID, portID := endpointParts(connection.To)
			if routeHasTarget(route.Targets, nodeID) {
				continue
			}
			target := GateRouteTarget{NodeID: nodeID, Title: boardNodeTitle(board, nodeID), Kind: boardNodeKind(board, nodeID)}
			switch target.Kind {
			case "formation":
				route.DispatchesNeeded++
				target.Attempt = nodeAttemptsBefore(events, len(events), nodeID) + 1
				target.MaxAttempts = limits.MaxAttempts
				// The engine's own rule, so the panel says what the engine will do.
				if route.Limit == nil && attemptsExhausted(limits, target.Attempt) {
					route.Limit = &RunLimitReached{Kind: RunLimitAttempts, NodeID: nodeID, Used: target.Attempt - 1, Max: target.MaxAttempts}
				}
				target.WaitsForInputs = formationWaitsForOtherInputs(board, events, nodeID, portID)
			case "gate":
				// A judge gate dispatches each formation of its judge chain.
				route.DispatchesNeeded += len(judgeChainForGate(board, nodeID))
			case "end":
				if end, ok := findEnd(board, nodeID); ok {
					target.Outcome = end.Outcome
				}
			}
			route.Targets = append(route.Targets, target)
		}
		route.RunFails = rejectedRunPath(board, events) != nil
		for _, target := range route.Targets {
			route.RunFails = route.RunFails || target.Outcome == EndOutcomeRejected
		}
		route.EndsRun = routeEndsRun(board, events, gateID, route.Targets, route.RunFails)
		if route.DispatchesNeeded > 0 && limits.MaxDispatch > 0 {
			route.Dispatches = &RunLimitReached{Kind: RunLimitDispatches, Used: consumed, Max: limits.MaxDispatch}
			if route.Limit == nil && consumed >= limits.MaxDispatch {
				route.Limit = route.Dispatches
			}
		}
		routes = append(routes, route)
	}
	return routes
}

// routeEndsRun reports whether a verdict whose routes all lead to End nodes
// finishes the run. It asks the engine's own rule (unfinishedRunWork), leaving
// out this gate's pending request, so the answer panel and the engine agree.
func routeEndsRun(board *BoardDocument, events []RunEvent, gateID string, targets []GateRouteTarget, fails bool) bool {
	if len(targets) == 0 {
		return false
	}
	for _, target := range targets {
		if target.Kind != "end" {
			return false
		}
	}
	// As the engine decides: nothing else can run, and a rejected path ends a
	// run even when it leaves a join starved of its other input.
	finish := runFinishState(board, events, gateID)
	return len(finish.runnable) == 0 && (fails || len(finish.starved) == 0)
}

// formationWaitsForOtherInputs reports whether a formation, given this input
// port, still lacks another input: one whose feeding steps have produced
// nothing in the run so far.
func formationWaitsForOtherInputs(board *BoardDocument, events []RunEvent, formationID, portID string) bool {
	formation, ok := findFormation(board.Formations, formationID)
	if !ok {
		return false
	}
	produced := map[string]bool{}
	for _, event := range events {
		switch event.Type {
		case RunEventNodeOutput:
			produced[event.NodeID] = true
		case RunEventGateVerdict:
			gateID := event.GateID
			if gateID == "" {
				gateID = event.NodeID
			}
			produced[gateID+":"+stringFromEventData(event, "routePort")] = true
		}
	}
	for _, input := range formation.Inputs {
		if input.ID == portID {
			continue
		}
		fed := false
		for _, connection := range board.Connections {
			if connection.To != formationID+":"+input.ID {
				continue
			}
			fromNode, _ := endpointParts(connection.From)
			if produced[fromNode] || produced[connection.From] {
				fed = true
				break
			}
		}
		if !fed {
			return true
		}
	}
	return false
}

func routeHasTarget(targets []GateRouteTarget, nodeID string) bool {
	for _, target := range targets {
		if target.NodeID == nodeID {
			return true
		}
	}
	return false
}

func boardNodeKind(board *BoardDocument, nodeID string) string {
	for _, formation := range board.Formations {
		if formation.ID == nodeID {
			return "formation"
		}
	}
	for _, gate := range board.Gates {
		if gate.ID == nodeID {
			return "gate"
		}
	}
	for _, mission := range board.Missions {
		if mission.ID == nodeID {
			return "mission"
		}
	}
	for _, tool := range board.Tools {
		if tool.ID == nodeID {
			return "tool"
		}
	}
	for _, end := range board.Ends {
		if end.ID == nodeID {
			return "end"
		}
	}
	return ""
}

func boardNodeTitle(board *BoardDocument, nodeID string) string {
	for _, formation := range board.Formations {
		if formation.ID == nodeID {
			return formation.Title
		}
	}
	for _, gate := range board.Gates {
		if gate.ID == nodeID {
			return gate.Title
		}
	}
	for _, mission := range board.Missions {
		if mission.ID == nodeID {
			return mission.Title
		}
	}
	for _, tool := range board.Tools {
		if tool.ID == nodeID {
			return tool.Title
		}
	}
	for _, end := range board.Ends {
		if end.ID == nodeID {
			return end.Title
		}
	}
	return ""
}
