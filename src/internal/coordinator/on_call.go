package coordinator

import (
	"context"
	"log"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Session-channel delivery (ADR-0019). When a run's command settles, its human
// gates' asks are pasted into the kept seats of the formation that asked, and
// the ledger records each delivery or the fallback to the notify command. Seats
// found gone are recorded, and seats whose asks were all answered end. Ledger
// writes happen under the run's command reservation, so a seat's gate command
// in that moment gets "coordinator is executing" and retries; pasting happens
// outside it, so an operator typing into a seat never holds up a verdict.

const (
	defaultSessionRetryInterval = 3 * time.Second
	defaultSessionProbeInterval = 15 * time.Second
	sessionCommandWait          = 5 * time.Second
	sessionPasteBudget          = 30 * time.Second
)

// withRunCommand runs fn holding the run's command reservation. It waits
// briefly for a busy run and reports false if the reservation never came. The
// release does not announce a settle: the dispatcher decides its own retries.
func (c *Coordinator) withRunCommand(ctx context.Context, runID string, fn func()) bool {
	deadline := time.Now().Add(sessionCommandWait)
	for !c.acquire(runID) {
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if closed || time.Now().After(deadline) || ctx.Err() != nil {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer c.releaseQuietly(runID)
	fn()
	return true
}

// releaseQuietly ends a reservation like release, without queueing a settle.
func (c *Coordinator) releaseQuietly(id string) {
	c.mu.Lock()
	state := c.state(id)
	state.cancel()
	state.busy = false
	close(state.done)
	close(state.changed)
	state.changed = make(chan struct{})
	c.mu.Unlock()
	c.workers.Done()
}

// deliverSession applies a settled session-channel run's plan. It reports
// whether some seat was not reached and the run should be tried again soon.
func (d *needsYouDispatcher) deliverSession(ctx context.Context, runID string) (retry bool) {
	c := d.c
	plan, err := c.engine.PlanRunOnCall(ctx, runID)
	if err != nil {
		log.Printf("session channel: run %s: %v", runID, err)
		return false
	}
	d.watch(runID, len(plan.Kept) > 0 || plan.OpenAsks > 0)
	if len(plan.Gone)+len(plan.Answered)+len(plan.Fallbacks) > 0 {
		if !c.withRunCommand(ctx, runID, func() {
			// Plan again under the reservation, so nothing recorded is stale.
			fresh, err := c.engine.PlanRunOnCall(ctx, runID)
			if err != nil {
				log.Printf("session channel: run %s: %v", runID, err)
				return
			}
			for _, gone := range fresh.Gone {
				if err := c.engine.RecordKeptSeatCleanup(runID, gone.Seat, gone.Outcome, "", ""); err != nil {
					log.Printf("session channel: run %s seat %s: %v", runID, gone.Seat.SlotID, err)
				}
			}
			if err := c.engine.EndKeptSeatsNow(runID, fresh.Answered, formations.SeatCauseAskAnswered, fresh.Keeper); err != nil {
				log.Printf("session channel: run %s: ending answered seats: %v", runID, err)
			}
			for _, fallback := range fresh.Fallbacks {
				if _, err := c.engine.RecordHumanAskFallback(runID, fallback); err != nil {
					log.Printf("session channel: run %s ask %d: %v", runID, fallback.Request.Seq, err)
				}
			}
			plan = fresh
		}) {
			return true
		}
	}
	for _, delivery := range plan.Deliveries {
		if ctx.Err() != nil {
			return true
		}
		if !d.deliverAsk(ctx, runID, plan, delivery) {
			retry = true
		}
	}
	return retry
}

// deliverAsk writes one seat's brief, pastes its pointer and records the
// delivery. It reports whether the seat was reached and recorded.
func (d *needsYouDispatcher) deliverAsk(ctx context.Context, runID string, plan formations.RunOnCallPlan, delivery formations.HumanAskDelivery) bool {
	c := d.c
	brief, pointer, err := c.engine.WriteHumanAskBrief(runID, plan.Board, plan.Events, delivery, d.config.ServerURL)
	if err != nil {
		log.Printf("session channel: run %s ask %d brief for %s: %v", runID, delivery.Request.Seq, delivery.Seat.SlotID, err)
		return false
	}
	paste, cancel := context.WithTimeout(ctx, sessionPasteBudget)
	err = plan.Keeper.PasteAsk(paste, delivery.Seat, pointer)
	cancel()
	if err != nil {
		log.Printf("session channel: run %s ask %d not yet pasted into %s: %v", runID, delivery.Request.Seq, delivery.Seat.SlotID, err)
		return false
	}
	recorded := false
	if !c.withRunCommand(ctx, runID, func() {
		_, err := c.engine.RecordHumanAskDelivered(runID, delivery, brief)
		recorded = err == nil
		if err != nil {
			log.Printf("session channel: run %s ask %d delivered to %s but not recorded: %v", runID, delivery.Request.Seq, delivery.Seat.SlotID, err)
		}
	}) {
		return false
	}
	return recorded
}

// watch keeps a run on the periodic probe while it has kept seats or open
// asks, so a seat that disappears while an ask waits is noticed.
func (d *needsYouDispatcher) watch(runID string, on bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if on {
		d.watching[runID] = true
	} else {
		delete(d.watching, runID)
	}
}

// retrySoon queues the run again after the session retry interval.
func (d *needsYouDispatcher) retrySoon(runID string) {
	d.mu.Lock()
	if d.retrying[runID] {
		d.mu.Unlock()
		return
	}
	d.retrying[runID] = true
	d.mu.Unlock()
	time.AfterFunc(d.config.SessionRetryInterval, func() {
		d.mu.Lock()
		delete(d.retrying, runID)
		d.pending[runID] = true
		d.mu.Unlock()
		select {
		case d.wake <- struct{}{}:
		default:
		}
	})
}

// queueWatched queues every run on the periodic probe.
func (d *needsYouDispatcher) queueWatched() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for runID := range d.watching {
		d.pending[runID] = true
	}
}

// humanGateFellBack reports whether a session-channel ask fell back to the
// notify command, the only way such an ask is notified.
func humanGateFellBack(events []formations.RunEvent, requestedSeq int) bool {
	record := formations.HumanAskRecords(events)[requestedSeq]
	return record != nil && record.Fallback != nil
}
