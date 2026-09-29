package formations

// Where a human gate's answer leads (form-n7u.7). The operator decides with the
// consequence in view: the steps each verdict delivers to on the run's frozen
// board, whether approving ends the run, and whether the step a verdict starts
// would take the last of a limit or find it already spent, which blocks the run
// instead (form-n7u.6). The routes follow routeGateVerdict and the engine's
// attempt and dispatch counting.

// GateRouteTarget is a step a verdict delivers to. A formation also says which
// attempt it would start and the run's attempt limit.
type GateRouteTarget struct {
	NodeID      string `json:"nodeId"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	Attempt     int    `json:"attempt,omitempty"`
	MaxAttempts int    `json:"maxAttempts,omitempty"`
}

// GateRoute is what one verdict does. EndsRun marks an approval that finishes
// the run; Unwired a send-back with no route, which blocks it. Limit is a limit
// the route finds spent, so taking it blocks the run. Dispatches is the run's
// dispatch use so far when the route starts formations under a dispatch limit.
type GateRoute struct {
	Verdict    string            `json:"verdict"`
	Targets    []GateRouteTarget `json:"targets"`
	EndsRun    bool              `json:"endsRun,omitempty"`
	Unwired    bool              `json:"unwired,omitempty"`
	Limit      *RunLimitReached  `json:"limit,omitempty"`
	Dispatches *RunLimitReached  `json:"dispatches,omitempty"`
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
		if len(connections) == 0 {
			// A pass with nothing downstream finishes the run unless a send-back
			// is still owed; a fail with nowhere to go blocks it.
			route.EndsRun = verdict == "pass" && !pendingPushback(board, events)
			route.Unwired = verdict == "fail"
		}
		starts := 0
		for _, connection := range connections {
			nodeID, _ := endpointParts(connection.To)
			if routeHasTarget(route.Targets, nodeID) {
				continue
			}
			target := GateRouteTarget{NodeID: nodeID, Title: boardNodeTitle(board, nodeID), Kind: boardNodeKind(board, nodeID)}
			if target.Kind == "formation" {
				starts++
				target.Attempt = nodeAttemptsBefore(events, len(events), nodeID) + 1
				// Only a limit the run was admitted with is reported; a run
				// without one gets no attempt warning (form-n7u.6).
				target.MaxAttempts = limits.MaxAttempts
				if route.Limit == nil && target.MaxAttempts > 0 && target.Attempt > target.MaxAttempts {
					route.Limit = &RunLimitReached{Kind: RunLimitAttempts, NodeID: nodeID, Used: target.Attempt - 1, Max: target.MaxAttempts}
				}
			}
			route.Targets = append(route.Targets, target)
		}
		if starts > 0 && limits.MaxDispatch > 0 {
			route.Dispatches = &RunLimitReached{Kind: RunLimitDispatches, Used: consumed, Max: limits.MaxDispatch}
			if route.Limit == nil && consumed >= limits.MaxDispatch {
				route.Limit = route.Dispatches
			}
		}
		routes = append(routes, route)
	}
	return routes
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
	return ""
}
