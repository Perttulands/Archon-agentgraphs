package formations

import (
	"context"
	"errors"
	"sort"
)

// Seats on call (ADR-0019). On a session-channel run, a formation whose output
// reaches a human gate through gates alone keeps its seats when it finishes,
// and the gate's ask is pasted into the seats of the formation that asked. The
// ledger is the record: seat_cleanup kept_on_call keeps a seat, a later
// seat_cleanup ends that, and human_ask_delivered and human_ask_fallback record
// where each ask went. Execution replay ignores all three.

const (
	RunEventSeatCreated       = "seat_created"
	RunEventSeatCleanup       = "seat_cleanup"
	RunEventHumanAskDelivered = "human_ask_delivered"
	RunEventHumanAskFallback  = "human_ask_fallback"

	// SeatOutcomeKeptOnCall keeps a finished formation's seat for its gate's ask.
	SeatOutcomeKeptOnCall = "kept_on_call"
	SeatOutcomeEnded      = "ended"
	// SeatOutcomeGone records a kept seat whose session is no longer there.
	SeatOutcomeGone              = "gone"
	SeatOutcomeLeftSocketChanged = "left_socket_changed"
	SeatOutcomeLeftCleanupFailed = "left_cleanup_failed"

	// Why a kept seat ended.
	SeatCauseAskAnswered = "ask_answered"
	SeatCauseNewAttempt  = "new_attempt"
	SeatCauseRunFinal    = "run_final"

	// Why a human gate's ask fell back to the notify command.
	AskFallbackLabExecutor       = "lab_executor"
	AskFallbackNoAskingFormation = "no_asking_formation"
	AskFallbackNoReceivableSeat  = "no_receivable_seat"
	AskFallbackAskedSeatsGone    = "asked_seats_gone"
	AskFallbackDeliveryUncertain = "delivery_uncertain"
)

// ErrHumanAskDeliveryUncertain means a paste may have changed the seat's input
// but submission could not be confirmed. Never retry the paste or press Enter
// to recover it: the operator may have edited that input in the meantime.
var ErrHumanAskDeliveryUncertain = errors.New("human ask delivery is uncertain")

var askFallbackReasons = map[string]string{
	AskFallbackLabExecutor:       "the lab executor keeps no seats",
	AskFallbackNoAskingFormation: "no formation lies behind the gate",
	AskFallbackNoReceivableSeat:  "no kept seat of the asking formation can receive the ask",
	AskFallbackAskedSeatsGone:    "every seat that received the ask is gone",
	AskFallbackDeliveryUncertain: "an ask may be left unsent in a seat; automatic delivery stopped to preserve the operator's input. Answer in the cockpit or inspect the seat",
}

// AskFallbackReason is the operator-facing reason for a fallback code.
func AskFallbackReason(code string) string {
	if reason, ok := askFallbackReasons[code]; ok {
		return reason
	}
	return code
}

// SeatKeeper is an executor whose seats can stay on call after their formation
// finishes. The runtime decides which seats to keep, end and ask from the
// ledger; the keeper only acts on one recorded seat at a time.
type SeatKeeper interface {
	// EndKeptSeat waits up to a minute for the agent to go idle, then ends the
	// seat by its recorded identity. It returns the seat_cleanup outcome and a
	// detail for anything other than ended.
	EndKeptSeat(ctx context.Context, seat KeptSeat) (outcome, detail string)
	// ProbeKeptSeat reports whether the seat still runs on the server it was
	// created on, and otherwise the seat_cleanup outcome that records it.
	ProbeKeptSeat(ctx context.Context, seat KeptSeat) (present bool, outcome string)
	// PasteAsk pastes a pointer into the seat once its agent is idle with an
	// empty input line, and submits it. ErrHumanAskDeliveryUncertain requires
	// a recorded fallback; other errors before a paste may be retried.
	PasteAsk(ctx context.Context, seat KeptSeat, pointer string) error
}

// KeptSeat is a seat kept on call, with the identity its seat_created recorded.
type KeptSeat struct {
	RunID          string `json:"runId"`
	NodeID         string `json:"nodeId"`
	SlotID         string `json:"slotId"`
	CreatedSeq     int    `json:"createdSeq"`
	KeptSeq        int    `json:"keptSeq"`
	SessionName    string `json:"sessionName"`
	SessionID      string `json:"sessionId"`
	PaneID         string `json:"paneId"`
	SocketIdentity string `json:"socketIdentity,omitempty"`
	Harness        string `json:"harness,omitempty"`
}

// RunHumanChannel is the channel of a run's frozen mission: session or notify.
func RunHumanChannel(board *BoardDocument, events []RunEvent) string {
	if board == nil || len(events) == 0 {
		return HumanChannelNotify
	}
	if mission, ok := findMission(board, events[0].MissionID); ok && mission.HumanChannel == HumanChannelSession {
		return HumanChannelSession
	}
	return HumanChannelNotify
}

// FormationKeepsSeatsOnCall reports whether a formation's output can reach a
// human gate through gates alone, following pass and fail routes and never a
// judge port. Members of a gate's judge chain never keep their seats.
func FormationKeepsSeatsOnCall(board *BoardDocument, formationID string) bool {
	if board == nil {
		return false
	}
	gates := map[string]GateNode{}
	for _, gate := range board.Gates {
		gates[gate.ID] = gate
		for _, judge := range judgeChainForGate(board, gate.ID) {
			if judge.ID == formationID {
				return false
			}
		}
	}
	var frontier []string
	// follow queues the gates a node's connections reach, from any port when
	// routes is nil, otherwise only from those route ports.
	follow := func(nodeID string, routes map[string]bool) {
		for _, connection := range board.Connections {
			from, fromPort := endpointParts(connection.From)
			to, toPort := endpointParts(connection.To)
			if from != nodeID || fromPort == "judge" || toPort == "judge" || routes != nil && !routes[fromPort] {
				continue
			}
			if _, ok := gates[to]; ok {
				frontier = append(frontier, to)
			}
		}
	}
	follow(formationID, nil)
	seen := map[string]bool{}
	for len(frontier) > 0 {
		gateID := frontier[0]
		frontier = frontier[1:]
		if seen[gateID] {
			continue
		}
		seen[gateID] = true
		if hasGateKind(gates[gateID].Kinds, "human") {
			return true
		}
		follow(gateID, map[string]bool{"pass": true, "fail": true})
	}
	return false
}

// AskingFormation is the formation whose work a human request judges: the
// nearest formation behind the gate's input. A pass keeps the input's origin; a
// fail route names the failing gate, whose own input is followed back in turn.
func AskingFormation(board *BoardDocument, events []RunEvent, request RunEvent) string {
	if board == nil {
		return ""
	}
	formationIDs := map[string]bool{}
	for _, formation := range board.Formations {
		formationIDs[formation.ID] = true
	}
	gateIDs := map[string]bool{}
	for _, gate := range board.Gates {
		gateIDs[gate.ID] = true
	}
	from := runInputRefFromAny(request.Data["inputRef"]).FromNodeID
	before := request.Seq
	for hops := 0; from != "" && hops <= len(events); hops++ {
		if formationIDs[from] {
			return from
		}
		if !gateIDs[from] {
			return ""
		}
		evaluation, ok := latestGateEvaluationBefore(events, from, before)
		if !ok {
			return ""
		}
		before = evaluation.Seq
		from = runInputRefFromAny(evaluation.Data["inputRef"]).FromNodeID
	}
	return ""
}

func latestGateEvaluationBefore(events []RunEvent, gateID string, before int) (RunEvent, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event.Seq < before && event.Type == RunEventGateEvaluating && event.GateID == gateID {
			return event, true
		}
	}
	return RunEvent{}, false
}

// KeptSeats lists the run's seats kept on call and not ended since, oldest first.
func KeptSeats(events []RunEvent) []KeptSeat {
	seats := map[int]KeptSeat{}
	ended := map[int]bool{}
	for _, event := range events {
		switch event.Type {
		case RunEventSeatCreated:
			seats[event.Seq] = KeptSeat{
				RunID: event.RunID, NodeID: event.NodeID, SlotID: event.SlotID, CreatedSeq: event.Seq,
				SessionName: stringFromEventData(event, "sessionName"), SessionID: stringFromEventData(event, "sessionId"),
				PaneID: stringFromEventData(event, "paneId"), SocketIdentity: stringFromEventData(event, "socketIdentity"),
				Harness: stringFromEventData(event, "harness"),
			}
		case RunEventSeatCleanup:
			// A cleanup names its node, slot and session; a new attempt reuses the
			// session name only after the old seat ended, so the latest open seat
			// with that name is the one cleaned up.
			match := 0
			for seq, seat := range seats {
				if !ended[seq] && seq > match && seat.NodeID == event.NodeID && seat.SlotID == event.SlotID && seat.SessionName == stringFromEventData(event, "sessionName") {
					match = seq
				}
			}
			if match == 0 {
				continue
			}
			if stringFromEventData(event, "outcome") == SeatOutcomeKeptOnCall {
				seat := seats[match]
				seat.KeptSeq = event.Seq
				seats[match] = seat
			} else {
				ended[match] = true
			}
		}
	}
	kept := make([]KeptSeat, 0, len(seats))
	for seq, seat := range seats {
		if seat.KeptSeq > 0 && !ended[seq] {
			kept = append(kept, seat)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].CreatedSeq < kept[j].CreatedSeq })
	return kept
}

// OpenHumanRequests lists the human requests still waiting for a verdict.
func OpenHumanRequests(events []RunEvent) []RunEvent {
	for _, event := range events {
		if isFinalRunEvent(event.Type) {
			return nil
		}
	}
	latest := map[string]RunEvent{}
	for _, event := range events {
		switch event.Type {
		case RunEventHumanInputRequested:
			latest[event.GateID] = event
		case RunEventHumanVerdictRecorded:
			delete(latest, event.GateID)
		}
	}
	requests := make([]RunEvent, 0, len(latest))
	for _, request := range latest {
		requests = append(requests, request)
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].Seq < requests[j].Seq })
	return requests
}

// HumanAskRecord is where the ledger says one human request's ask went.
type HumanAskRecord struct {
	// Delivered maps each receiving seat's created sequence to the delivery.
	Delivered map[int]RunEvent
	// Fallback is the fallback event, if the ask fell back.
	Fallback *RunEvent
}

// HumanAskRecords indexes deliveries and fallbacks by requested sequence.
func HumanAskRecords(events []RunEvent) map[int]*HumanAskRecord {
	records := map[int]*HumanAskRecord{}
	record := func(seq int) *HumanAskRecord {
		if records[seq] == nil {
			records[seq] = &HumanAskRecord{Delivered: map[int]RunEvent{}}
		}
		return records[seq]
	}
	for _, event := range events {
		switch event.Type {
		case RunEventHumanAskDelivered:
			record(intFromRunEventData(event.Data["requestedSeq"])).Delivered[intFromRunEventData(event.Data["seatCreatedSeq"])] = event
		case RunEventHumanAskFallback:
			fallback := event
			record(intFromRunEventData(event.Data["requestedSeq"])).Fallback = &fallback
		}
	}
	return records
}

// HumanAskDelivery is an ask a kept seat is still owed.
type HumanAskDelivery struct {
	Request      RunEvent
	AskingNodeID string
	Seat         KeptSeat
}

// HumanAskFallback is an ask that must fall back to the notify command.
type HumanAskFallback struct {
	Request RunEvent
	Code    string
	// Seat identifies input an uncertain paste may have changed. Its immutable
	// CreatedSeq prevents future asks from touching that input, even after restart.
	Seat KeptSeat
}

// HumanAskUncertainSeats indexes immutable seat identities whose input may
// still contain an unsent ask. A replacement seat has a new created sequence.
func HumanAskUncertainSeats(events []RunEvent) map[int]bool {
	uncertain := map[int]bool{}
	for _, event := range events {
		if event.Type == RunEventHumanAskFallback && stringFromEventData(event, "code") == AskFallbackDeliveryUncertain {
			if seq := intFromRunEventData(event.Data["seatCreatedSeq"]); seq > 0 {
				uncertain[seq] = true
			}
		}
	}
	return uncertain
}

// OnCallPlan is what a settled session-channel run owes its kept seats.
type OnCallPlan struct {
	// Gone lists kept seats no longer present, with the outcome to record.
	Gone []GoneKeptSeat
	// Answered lists kept seats that received an ask while no open request
	// names their formation as the asker.
	Answered    []KeptSeat
	Deliveries  []HumanAskDelivery
	Fallbacks   []HumanAskFallback
	OpenAsks    int
	Undelivered int
}

// GoneKeptSeat is a kept seat a probe found missing.
type GoneKeptSeat struct {
	Seat    KeptSeat
	Outcome string
}

// SeatProbe reports whether a kept seat is present, and the outcome if not.
type SeatProbe func(KeptSeat) (bool, string)

// PlanOnCall decides, from the ledger and a probe of the kept seats, which seats
// to record gone or release and where each open ask goes. It plans nothing for a
// notify-channel, blocked or final run: a blocked run keeps its seats and owes
// its asks until it resumes.
func PlanOnCall(board *BoardDocument, events []RunEvent, keeper bool, probe SeatProbe) OnCallPlan {
	var plan OnCallPlan
	if len(events) == 0 || RunHumanChannel(board, events) != HumanChannelSession {
		return plan
	}
	status, err := ProjectRunEvents(events[0].RunID, events)
	if err != nil || status.Final || status.Status == RunStatusBlocked {
		return plan
	}
	present := map[int]KeptSeat{}
	for _, seat := range KeptSeats(events) {
		if probe == nil {
			present[seat.CreatedSeq] = seat
			continue
		}
		if ok, outcome := probe(seat); ok {
			present[seat.CreatedSeq] = seat
		} else {
			plan.Gone = append(plan.Gone, GoneKeptSeat{Seat: seat, Outcome: outcome})
		}
	}
	requests := OpenHumanRequests(events)
	records := HumanAskRecords(events)
	uncertain := HumanAskUncertainSeats(events)
	askers := map[string]bool{}
	for _, request := range requests {
		askers[AskingFormation(board, events, request)] = true
	}
	for _, seat := range present {
		if askers[seat.NodeID] {
			continue
		}
		for _, record := range records {
			if _, ok := record.Delivered[seat.CreatedSeq]; ok {
				plan.Answered = append(plan.Answered, seat)
				break
			}
		}
	}
	sort.Slice(plan.Answered, func(i, j int) bool { return plan.Answered[i].CreatedSeq < plan.Answered[j].CreatedSeq })
	for _, request := range requests {
		plan.OpenAsks++
		record := records[request.Seq]
		if record != nil && record.Fallback != nil {
			continue
		}
		if !keeper {
			plan.Fallbacks = append(plan.Fallbacks, HumanAskFallback{Request: request, Code: AskFallbackLabExecutor})
			continue
		}
		asker := AskingFormation(board, events, request)
		if asker == "" {
			plan.Fallbacks = append(plan.Fallbacks, HumanAskFallback{Request: request, Code: AskFallbackNoAskingFormation})
			continue
		}
		var receivers []KeptSeat
		var unsafe KeptSeat
		for _, seat := range askReceivers(board, asker, present) {
			if uncertain[seat.CreatedSeq] {
				unsafe = seat
				continue
			}
			receivers = append(receivers, seat)
		}
		delivered, reached := 0, 0
		if record != nil {
			delivered = len(record.Delivered)
			for createdSeq := range record.Delivered {
				if _, ok := present[createdSeq]; ok && !uncertain[createdSeq] {
					reached++
				}
			}
		}
		var owed []KeptSeat
		for _, seat := range receivers {
			if record == nil || record.Delivered[seat.CreatedSeq].Type == "" {
				owed = append(owed, seat)
			}
		}
		switch {
		case reached == 0 && len(receivers) == 0 && unsafe.CreatedSeq > 0:
			plan.Fallbacks = append(plan.Fallbacks, HumanAskFallback{Request: request, Code: AskFallbackDeliveryUncertain, Seat: unsafe})
		case delivered == 0 && len(receivers) == 0:
			plan.Fallbacks = append(plan.Fallbacks, HumanAskFallback{Request: request, Code: AskFallbackNoReceivableSeat})
		case delivered > 0 && reached == 0 && len(owed) == 0:
			plan.Fallbacks = append(plan.Fallbacks, HumanAskFallback{Request: request, Code: AskFallbackAskedSeatsGone})
		default:
			for _, seat := range owed {
				plan.Deliveries = append(plan.Deliveries, HumanAskDelivery{Request: request, AskingNodeID: asker, Seat: seat})
			}
			plan.Undelivered += len(owed)
		}
	}
	return plan
}

// askReceivers are the present kept seats of the asking formation's latest
// attempt: every seat of a solo or peer formation, the controller alone of an
// orchestrated one.
func askReceivers(board *BoardDocument, asker string, present map[int]KeptSeat) []KeptSeat {
	formation, ok := findFormation(board.Formations, asker)
	if !ok {
		return nil
	}
	latest := map[string]KeptSeat{}
	for _, seat := range present {
		if seat.NodeID != asker {
			continue
		}
		if formation.Type == FormationTypeOrchestrated && !slotIsController(formation, seat.SlotID) {
			continue
		}
		if seat.CreatedSeq > latest[seat.SlotID].CreatedSeq {
			latest[seat.SlotID] = seat
		}
	}
	receivers := make([]KeptSeat, 0, len(latest))
	for _, seat := range latest {
		receivers = append(receivers, seat)
	}
	sort.Slice(receivers, func(i, j int) bool { return receivers[i].CreatedSeq < receivers[j].CreatedSeq })
	return receivers
}

func slotIsController(formation FormationNode, slotID string) bool {
	for _, slot := range formation.Slots {
		if slot.ID == slotID {
			return slot.Controller
		}
	}
	return false
}
