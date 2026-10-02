package formations

import "time"

// Where a human gate's answer leads (archon-n7u.7). The operator decides with the
// consequence in view: the steps each verdict delivers to on the run's frozen
// board, whether a verdict ends the run, and whether a step the verdict starts
// finds a Limit card's rounds or time spent, which blocks the run instead
// (archon-o7p.8). The routes count as the engine does (roundsUse, timeUse,
// limitSpentBefore).

// GateRouteTarget is a step, gate or End node a verdict delivers to. A
// formation also says which attempt it would start, its Limit card's rounds
// used so far when one caps it (omitted otherwise), and whether it still waits
// for other inputs. An End node says its outcome: the path ends there, done or
// rejected.
type GateRouteTarget struct {
	NodeID  string `json:"nodeId"`
	Title   string `json:"title"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
	// Rounds is the step's Limit card use before the route starts it: Used
	// of Max, Max counting grants.
	Rounds *RunLimitReached `json:"rounds,omitempty"`
	// Time is the step's time card use so far, in seconds.
	Time *RunLimitReached `json:"time,omitempty"`
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
// Limit card the route finds spent, so taking it blocks the run until a grant.
// MissionRounds is the mission card's use so far and RoundsNeeded the step
// starts the route makes, judges included, when a card caps the mission's
// rounds; MissionTime is the mission card's time use when it sets time.
type GateRoute struct {
	Verdict       string            `json:"verdict"`
	Targets       []GateRouteTarget `json:"targets"`
	EndsRun       bool              `json:"endsRun,omitempty"`
	RunFails      bool              `json:"runFails,omitempty"`
	Limit         *RunLimitReached  `json:"limit,omitempty"`
	MissionRounds *RunLimitReached  `json:"missionRounds,omitempty"`
	RoundsNeeded  int               `json:"roundsNeeded,omitempty"`
	MissionTime   *RunLimitReached  `json:"missionTime,omitempty"`
}

// HumanGateRoutes reports the pass and fail routes of a gate as the run stands
// at now.
func HumanGateRoutes(board *BoardDocument, events []RunEvent, gateID string, now time.Time) []GateRoute {
	var missionRounds, missionTime *RunLimitReached
	for _, mission := range board.Missions {
		if limit, ok := limitCovering(board, mission.ID); ok {
			missionRounds = roundsUse(board, events, limit)
			missionTime = timeUse(board, events, limit, now)
		}
	}
	routes := make([]GateRoute, 0, 2)
	for _, verdict := range []string{"pass", "fail"} {
		route := GateRoute{Verdict: verdict, Targets: []GateRouteTarget{}}
		needed := 0
		connections := outgoingConnectionsFromPort(board.Connections, gateID, verdict)
		for _, connection := range connections {
			nodeID, portID := endpointParts(connection.To)
			if routeHasTarget(route.Targets, nodeID) {
				continue
			}
			target := GateRouteTarget{NodeID: nodeID, Title: boardNodeTitle(board, nodeID), Kind: boardNodeKind(board, nodeID)}
			switch target.Kind {
			case "formation":
				needed++
				target.Attempt = nodeLatestAttempt(events, nodeID) + 1
				formation, _ := findFormation(board.Formations, nodeID)
				if limit, ok := limitCovering(board, nodeID); ok {
					if use := roundsUse(board, events, limit); use != nil && formation.Type != FormationTypePeer {
						target.Rounds = use
					}
					target.Time = timeUse(board, events, limit, now)
				}
				// The engine's own rule, so the panel says what the engine will do.
				if route.Limit == nil {
					route.Limit = limitSpentBefore(board, events, formation, now)
				}
				target.WaitsForInputs = formationWaitsForOtherInputs(board, events, nodeID, portID)
			case "gate":
				// A judge gate starts each formation of its judge chain.
				needed += len(judgeChainForGate(board, nodeID))
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
		if needed > 0 && missionRounds != nil {
			route.MissionRounds, route.RoundsNeeded = missionRounds, needed
			if route.Limit == nil && missionRounds.Used >= missionRounds.Max {
				route.Limit = missionRounds
			}
		}
		if needed > 0 && missionTime != nil {
			route.MissionTime = missionTime
			if route.Limit == nil && missionTime.Used >= missionTime.Max {
				route.Limit = missionTime
			}
		}
		routes = append(routes, route)
	}
	return routes
}

// nodeLatestAttempt is the node's latest recorded attempt.
func nodeLatestAttempt(events []RunEvent, nodeID string) int {
	attempts := 0
	for _, event := range events {
		if event.Type == RunEventNodeStarted && event.NodeID == nodeID && event.Attempt > attempts {
			attempts = event.Attempt
		}
	}
	return attempts
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
			return "inputCard"
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
	for _, limit := range board.Limits {
		if limit.ID == nodeID {
			return "limit"
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
	for _, limit := range board.Limits {
		if limit.ID == nodeID {
			return limit.Title
		}
	}
	return ""
}
