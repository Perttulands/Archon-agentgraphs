package formations

import (
	"context"
	"sync"
	"time"
)

// A step has no time limit unless its mission sets one, so a seat that waits on
// something Archon cannot end on its own must be visible instead. Each wait
// below is recorded as a seat_state event once it has lasted
// seatStateObservation, and again as "working" when it ends. Nothing here
// blocks or times out a step: the driver or the operator decides.
const (
	// SeatStateNotReady: the harness has not reached its ready prompt.
	SeatStateNotReady = "seat_not_ready"
	// SeatStateWaitingForIdleInput: the brief waits to be pasted because the
	// agent is busy or the input line holds text the operator has not sent.
	SeatStateWaitingForIdleInput = "waiting_for_idle_input"
	// SeatStateTurnEndedWithoutSentinel: after an operator turn, the agent
	// ended a turn without this run's completion sentinel.
	SeatStateTurnEndedWithoutSentinel = "turn_ended_without_sentinel"
	// SeatStateBackgroundWork: Claude ended its turn with background work
	// that has not resumed the conversation.
	SeatStateBackgroundWork = "background_work_pending"
	// SeatStateWorking ends a recorded wait.
	SeatStateWorking = "working"

	RunEventSeatState = "seat_state"
)

// seatStateObservation is how long a wait lasts before it is recorded.
var seatStateObservation = 60 * time.Second

var seatStateDetails = map[string]string{
	SeatStateNotReady:                 "the seat has not reached its ready prompt; open the seat to see its screen",
	SeatStateWaitingForIdleInput:      "the brief waits to be pasted: the agent is busy or the input line holds unsent text",
	SeatStateTurnEndedWithoutSentinel: "the agent ended a turn without the completion sentinel after an operator turn; ask it to finish or send the sentinel",
	SeatStateBackgroundWork:           "the agent ended its turn with background work that has not resumed",
}

// seatWait is what a seat is currently waiting on, set by the transport.
type seatWait struct {
	mu    sync.Mutex
	state string
	since time.Time
}

func (s *nativeSeat) setWaiting(state string) {
	if s == nil {
		return
	}
	s.wait.mu.Lock()
	defer s.wait.mu.Unlock()
	if s.wait.state != state {
		s.wait.state, s.wait.since = state, time.Now()
	}
}

func (s *nativeSeat) waiting() (string, time.Time) {
	if s == nil {
		return "", time.Time{}
	}
	s.wait.mu.Lock()
	defer s.wait.mu.Unlock()
	return s.wait.state, s.wait.since
}

// watchSeatState records the seat's waits for one dispatch until stop is
// called. It only appends events.
func (e *TmuxFormationExecutor) watchSeatState(ctx context.Context, runID, nodeID, slotID, dispatchID string, seat *nativeSeat) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := seatStateObservation / 4
		if tick < 10*time.Millisecond {
			tick = 10 * time.Millisecond
		}
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		recorded := ""
		for {
			state, since := seat.waiting()
			switch {
			case state != "" && state != recorded && time.Since(since) >= seatStateObservation:
				if e.appendSeatState(runID, nodeID, slotID, dispatchID, state, since) == nil {
					recorded = state
				}
			case state == "" && recorded != "":
				if e.appendSeatState(runID, nodeID, slotID, dispatchID, SeatStateWorking, time.Now()) == nil {
					recorded = ""
				}
			}
			select {
			case <-ctx.Done():
				// A wait that ended just before the dispatch moved on is closed;
				// one still open is superseded by the dispatch's own result or error.
				if state, _ := seat.waiting(); recorded != "" && state == "" {
					_ = e.appendSeatState(runID, nodeID, slotID, dispatchID, SeatStateWorking, time.Now())
				}
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func (e *TmuxFormationExecutor) appendSeatState(runID, nodeID, slotID, dispatchID, state string, since time.Time) error {
	data := map[string]any{"state": state, "since": since.UTC().Format(time.RFC3339Nano)}
	if dispatchID != "" {
		data["dispatchId"] = dispatchID
	}
	if detail := seatStateDetails[state]; detail != "" {
		data["detail"] = detail
	}
	return e.store.AppendRunEvent(runID, RunEvent{Type: RunEventSeatState, NodeID: nodeID, SlotID: slotID, Data: data})
}

// readySeat waits for a seat's harness to be ready, recording a long wait.
func (e *TmuxFormationExecutor) readySeat(ctx context.Context, runID, nodeID, slotID string, seat *nativeSeat, harness string) error {
	stop := e.watchSeatState(ctx, runID, nodeID, slotID, "", seat)
	defer stop()
	return e.seatClient.Ready(ctx, e.config.Socket, seat, harness)
}
