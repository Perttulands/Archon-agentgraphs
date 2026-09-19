package formations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The engine applies ADR-0019's end rules at the two points a run may
// reconsider its kept seats: when it starts dispatching a formation, and just
// before the event that makes it final. The coordinator applies the rest when a
// command settles, through PlanRunOnCall and the record methods below.

func (e *RunEngine) seatKeeper() (SeatKeeper, bool) {
	keeper, ok := e.executor.(SeatKeeper)
	return keeper, ok
}

// EndKeptSeats ends every kept seat of the run and records their cleanup. Call
// it just before appending run_succeeded, run_failed or run_canceled, since the
// ledger accepts nothing after a final event.
func (e *RunEngine) EndKeptSeats(runID string) error {
	return e.endKeptSeats(runID, SeatCauseRunFinal, nil)
}

// endKeptSeats ends the selected kept seats (all when selected is nil) in
// parallel, each after its own idle wait, then records their cleanup in order.
func (e *RunEngine) endKeptSeats(runID, cause string, selected func(KeptSeat) bool) error {
	keeper, ok := e.seatKeeper()
	if !ok || e.store == nil {
		return nil
	}
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	var seats []KeptSeat
	for _, seat := range KeptSeats(events) {
		if selected == nil || selected(seat) {
			seats = append(seats, seat)
		}
	}
	return e.EndKeptSeatsNow(runID, seats, cause, keeper)
}

// EndKeptSeatsNow ends the given seats and records seat_cleanup for each.
func (e *RunEngine) EndKeptSeatsNow(runID string, seats []KeptSeat, cause string, keeper SeatKeeper) error {
	if len(seats) == 0 {
		return nil
	}
	if keeper == nil {
		var ok bool
		if keeper, ok = e.seatKeeper(); !ok {
			return nil
		}
	}
	type ending struct{ outcome, detail string }
	endings := make([]ending, len(seats))
	var wg sync.WaitGroup
	for i, seat := range seats {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), keptSeatIdleWait+15*time.Second)
			defer cancel()
			endings[i].outcome, endings[i].detail = keeper.EndKeptSeat(ctx, seat)
		}()
	}
	wg.Wait()
	for i, seat := range seats {
		if err := e.RecordKeptSeatCleanup(runID, seat, endings[i].outcome, cause, endings[i].detail); err != nil {
			return err
		}
	}
	return nil
}

// RecordKeptSeatCleanup records one kept seat's end, unless the ledger already
// shows it ended.
func (e *RunEngine) RecordKeptSeatCleanup(runID string, seat KeptSeat, outcome, cause, detail string) error {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return err
	}
	still := false
	for _, kept := range KeptSeats(events) {
		still = still || kept.CreatedSeq == seat.CreatedSeq
	}
	if !still {
		return nil
	}
	data := map[string]any{"sessionName": seat.SessionName, "outcome": outcome}
	if cause != "" {
		data["cause"] = cause
	}
	if detail != "" {
		data["detail"] = detail
	}
	return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventSeatCleanup, NodeID: seat.NodeID, SlotID: seat.SlotID, Data: data})
}

// reconsiderKeptSeats runs when a formation is about to dispatch. The
// formation's own kept seats end before its new attempt creates seats with the
// same names; seats whose asks were all answered end too, and seats found gone
// are recorded.
func (e *RunEngine) reconsiderKeptSeats(runID, dispatching string) error {
	plan, err := e.PlanRunOnCall(context.Background(), runID)
	if err != nil || plan.Keeper == nil {
		return err
	}
	for _, gone := range plan.Gone {
		if err := e.RecordKeptSeatCleanup(runID, gone.Seat, gone.Outcome, "", ""); err != nil {
			return err
		}
	}
	var attempt []KeptSeat
	for _, seat := range plan.Kept {
		if seat.NodeID == dispatching && !plan.isGone(seat) {
			attempt = append(attempt, seat)
		}
	}
	if err := e.EndKeptSeatsNow(runID, attempt, SeatCauseNewAttempt, plan.Keeper); err != nil {
		return err
	}
	var answered []KeptSeat
	for _, seat := range plan.Answered {
		if seat.NodeID != dispatching {
			answered = append(answered, seat)
		}
	}
	return e.EndKeptSeatsNow(runID, answered, SeatCauseAskAnswered, plan.Keeper)
}

// recordGoneKeptSeats records the kept seats a resumed run finds gone. While a
// run is blocked it keeps its seats, and a cleanup found then waits for resume.
func (e *RunEngine) recordGoneKeptSeats(runID string) error {
	plan, err := e.PlanRunOnCall(context.Background(), runID)
	if err != nil {
		return err
	}
	for _, gone := range plan.Gone {
		if err := e.RecordKeptSeatCleanup(runID, gone.Seat, gone.Outcome, "", ""); err != nil {
			return err
		}
	}
	return nil
}

// RunOnCallPlan is a run's plan with the context needed to act on it.
type RunOnCallPlan struct {
	OnCallPlan
	Board  *BoardDocument
	Events []RunEvent
	// Kept lists the run's kept seats, present or not.
	Kept []KeptSeat
	// Keeper is nil when the executor keeps no seats.
	Keeper SeatKeeper
}

func (p RunOnCallPlan) isGone(seat KeptSeat) bool {
	for _, gone := range p.Gone {
		if gone.Seat.CreatedSeq == seat.CreatedSeq {
			return true
		}
	}
	return false
}

// PlanRunOnCall reads the run and probes its kept seats. The plan is empty for
// a notify-channel, blocked or final run.
func (e *RunEngine) PlanRunOnCall(ctx context.Context, runID string) (RunOnCallPlan, error) {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return RunOnCallPlan{}, err
	}
	keeper, hasKeeper := e.seatKeeper()
	kept := KeptSeats(events)
	if len(kept) == 0 && len(OpenHumanRequests(events)) == 0 {
		return RunOnCallPlan{Events: events, Keeper: keeper}, nil
	}
	board, err := e.readRunBoard(runID)
	if err != nil {
		return RunOnCallPlan{}, err
	}
	var probe SeatProbe
	if hasKeeper {
		probe = func(seat KeptSeat) (bool, string) {
			probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			return keeper.ProbeKeptSeat(probeCtx, seat)
		}
	}
	plan := PlanOnCall(board, events, hasKeeper, probe)
	return RunOnCallPlan{OnCallPlan: plan, Board: board, Events: events, Kept: kept, Keeper: keeper}, nil
}

// HumanAskBriefPath is where a seat's copy of a human gate's ask is written.
func (e *RunEngine) HumanAskBriefPath(runID string, requestedSeq int, slotID string) string {
	return filepath.Join(e.store.workspaceRoot(), "briefs", fmt.Sprintf("gate-%s-%d-%s.md", sanitizeSessionComponent(runID), requestedSeq, sanitizeSessionComponent(slotID)))
}

// WriteHumanAskBrief writes the ask for one receiving seat and returns the
// pointer to paste. The brief carries that seat's slot in --relayed-by, and
// its commands run cli, the archon CLI matching this daemon ("archon" on PATH
// when empty).
func (e *RunEngine) WriteHumanAskBrief(runID string, board *BoardDocument, events []RunEvent, delivery HumanAskDelivery, serverURL, cli string) (string, string, error) {
	path := e.HumanAskBriefPath(runID, delivery.Request.Seq, delivery.Seat.SlotID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".gate-*.md")
	if err != nil {
		return "", "", err
	}
	if _, err := temp.WriteString(renderHumanAskBrief(runID, board, events, delivery, serverURL, cli)); err != nil {
		temp.Close()
		os.Remove(temp.Name())
		return "", "", err
	}
	if err := temp.Close(); err != nil {
		os.Remove(temp.Name())
		return "", "", err
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		os.Remove(temp.Name())
		return "", "", err
	}
	gateTitle := nodeTitleOnBoard(board, delivery.Request.GateID)
	return path, fmt.Sprintf("Read the file %s and follow it: the operator's gate %q is waiting on your work.", path, gateTitle), nil
}

func renderHumanAskBrief(runID string, board *BoardDocument, events []RunEvent, delivery HumanAskDelivery, serverURL, cli string) string {
	request := delivery.Request
	gateTitle := nodeTitleOnBoard(board, request.GateID)
	server := serverURL
	if server == "" {
		server = `"$FORM_SERVER"`
	}
	switch {
	case cli == "":
		cli = "archon"
	case strings.Trim(cli, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789/._+-") != "":
		cli = shellQuote(cli)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Human gate: %s\n\n", gateTitle)
	fmt.Fprintf(&b, "run: %s\ngate: %s (%s)\npending request: %d\nasking formation: %s (%s)\nyour slot: %s\n\n",
		runID, request.GateID, gateTitle, request.Seq, nodeTitleOnBoard(board, delivery.AskingNodeID), delivery.AskingNodeID, delivery.Seat.SlotID)
	fmt.Fprintf(&b, "The operator's human gate %q is waiting for a decision on your formation's work. The operator talks it through with you here.\n\n", gateTitle)
	if criterion := strings.TrimSpace(stringFromEventData(request, "prompt")); criterion != "" {
		fmt.Fprintf(&b, "## Criterion\n\n%s\n\n", criterion)
	}
	b.WriteString("## Decisions already recorded in this run\n\n")
	decisions := 0
	for _, event := range events {
		if event.Type != RunEventHumanVerdictRecorded {
			continue
		}
		decisions++
		verdict := "approved"
		if stringFromEventData(event, "verdict") == "fail" {
			verdict = "sent back"
		}
		fmt.Fprintf(&b, "- %s: %s", nodeTitleOnBoard(board, event.GateID), verdict)
		if response := stringFromEventData(event, "reason"); response != "" {
			fmt.Fprintf(&b, ", with the response: %s", response)
		}
		b.WriteString("\n")
	}
	if decisions == 0 {
		b.WriteString("None yet.\n")
	}
	slot := delivery.Seat.SlotID
	b.WriteString("\n## How to help\n\n")
	b.WriteString("- Present the gate's question plainly, from your own work, and help the operator think it through. Offer a view only when asked, and label it as yours.\n")
	b.WriteString("- Only the operator decides. A complete, unambiguous operator verdict for this pending gate, with the exact response to record, is itself confirmation. Record those exact words immediately, without asking them to confirm again. This applies to both approval and send-back. For example, 'Approve. Response: Keep the scope as written.' or 'Send back. Response: Add the missing constraints.' confirms that verdict and response when addressed to this gate.\n")
	b.WriteString("- If you draft or paraphrase any response, or the verdict, response, or intended gate is ambiguous, show the proposed verdict and exact response together and wait for the operator's confirmation before recording. Do not infer a verdict from discussion or invent missing response text.\n")
	b.WriteString("- Record the confirmed decision with exactly one of these commands. Replace RESPONSE with the operator's confirmed words, quoted for the shell:\n\n")
	fmt.Fprintf(&b, "      %s --server %s gate approve %s %s --requested-seq %d --relayed-by %s --response RESPONSE\n", cli, server, runID, request.GateID, request.Seq, slot)
	fmt.Fprintf(&b, "      %s --server %s gate reject %s %s --requested-seq %d --relayed-by %s --response RESPONSE\n\n", cli, server, runID, request.GateID, request.Seq, slot)
	b.WriteString("- For a long answer, the operator can give an explicit verdict and a UTF-8 response file. That confirms the complete file text as the response: preserve it verbatim and unabridged, including whitespace and final newlines. Use --response-file instead of --response; do not summarize, rewrite, or use shell command substitution to read the text. A file alone is not a verdict. If you cannot read it, tell the operator. Quote FILE for the shell:\n\n")
	fmt.Fprintf(&b, "      %s --server %s gate approve %s %s --requested-seq %d --relayed-by %s --response-file FILE\n", cli, server, runID, request.GateID, request.Seq, slot)
	fmt.Fprintf(&b, "      %s --server %s gate reject %s %s --requested-seq %d --relayed-by %s --response-file FILE\n\n", cli, server, runID, request.GateID, request.Seq, slot)
	b.WriteString("- Approve sends the response to the next step with the gate's input. Reject sends the work back, and the next attempt reads only the response, so it must carry what the conversation settled.\n")
	b.WriteString("- A 409 saying the coordinator is executing means the run is busy for a moment: wait a few seconds and run the same command again.\n")
	b.WriteString("- A 409 saying the human gate request is no longer pending means another seat or the cockpit decided first: tell the operator.\n")
	b.WriteString("- Your formation brief's limits still apply. Running the gate command above is the one exception to its bans.\n")
	return b.String()
}

// RecordHumanAskDelivered records that a seat received a pending ask, unless the
// request stopped waiting, the seat ended or the delivery is already recorded.
func (e *RunEngine) RecordHumanAskDelivered(runID string, delivery HumanAskDelivery, briefPath string) (bool, error) {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return false, err
	}
	if !requestStillOpen(events, delivery.Request) || !seatStillKept(events, delivery.Seat) {
		return false, nil
	}
	if record := HumanAskRecords(events)[delivery.Request.Seq]; record != nil && record.Delivered[delivery.Seat.CreatedSeq].Type != "" {
		return false, nil
	}
	return true, e.store.AppendRunEvent(runID, RunEvent{
		Type: RunEventHumanAskDelivered, GateID: delivery.Request.GateID, NodeID: delivery.AskingNodeID, SlotID: delivery.Seat.SlotID,
		Data: map[string]any{"requestedSeq": delivery.Request.Seq, "gateId": delivery.Request.GateID, "seatCreatedSeq": delivery.Seat.CreatedSeq, "sessionName": delivery.Seat.SessionName, "brief": briefPath},
	})
}

// RecordHumanAskFallback records, once, why an ask falls back. An uncertain
// paste is also recorded after its request was answered, while its seat is
// still kept: a verdict must not lose the identity of input we cannot touch.
func (e *RunEngine) RecordHumanAskFallback(runID string, fallback HumanAskFallback) (bool, error) {
	events, err := e.store.ReadRunEvents(runID)
	if err != nil {
		return false, err
	}
	uncertainSeat := fallback.Code == AskFallbackDeliveryUncertain && fallback.Seat.CreatedSeq > 0 && seatStillKept(events, fallback.Seat)
	if !requestStillOpen(events, fallback.Request) && !uncertainSeat {
		return false, nil
	}
	if record := HumanAskRecords(events)[fallback.Request.Seq]; record != nil && record.Fallback != nil {
		return false, nil
	}
	event := RunEvent{
		Type: RunEventHumanAskFallback, GateID: fallback.Request.GateID,
		Data: map[string]any{"requestedSeq": fallback.Request.Seq, "gateId": fallback.Request.GateID, "code": fallback.Code, "reason": AskFallbackReason(fallback.Code)},
	}
	if fallback.Seat.CreatedSeq > 0 {
		event.NodeID, event.SlotID = fallback.Seat.NodeID, fallback.Seat.SlotID
		event.Data["seatCreatedSeq"] = fallback.Seat.CreatedSeq
		event.Data["sessionName"] = fallback.Seat.SessionName
	}
	return true, e.store.AppendRunEvent(runID, event)
}

func requestStillOpen(events []RunEvent, request RunEvent) bool {
	status, err := ProjectRunEvents(request.RunID, events)
	if err != nil || status.Final || status.Status == RunStatusBlocked {
		return false
	}
	for _, open := range OpenHumanRequests(events) {
		if open.Seq == request.Seq {
			return true
		}
	}
	return false
}

func seatStillKept(events []RunEvent, seat KeptSeat) bool {
	for _, kept := range KeptSeats(events) {
		if kept.CreatedSeq == seat.CreatedSeq {
			return true
		}
	}
	return false
}

func nodeTitleOnBoard(board *BoardDocument, id string) string {
	if board != nil {
		for _, node := range board.Formations {
			if node.ID == id && node.Title != "" {
				return node.Title
			}
		}
		for _, node := range board.Gates {
			if node.ID == id && node.Title != "" {
				return node.Title
			}
		}
		for _, node := range board.Missions {
			if node.ID == id && node.Title != "" {
				return node.Title
			}
		}
	}
	return id
}
