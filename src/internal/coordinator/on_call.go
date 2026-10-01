package coordinator

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Session-channel delivery (ADR-0019). As soon as a human gate asks, even while
// the run's worker executes other steps (archon-o7p.11), its ask is pasted into
// the kept seats of the formation that asked, and the ledger records each
// delivery or the fallback to the notify command. Each record checks, under the
// ledger's append lock, that its request still waits and its seat is still
// kept, so it never lands after the verdict or seat end it raced with. Once the
// run settles, seats found gone are recorded and seats whose asks were all
// answered end; while a worker executes, it ends kept seats itself. Pasting
// never holds anything a verdict needs, so an operator typing into a seat
// never holds up a verdict.

const (
	defaultSessionRetryInterval = 3 * time.Second
	defaultSessionProbeInterval = 15 * time.Second
	sessionPasteBudget          = 30 * time.Second
)

type humanAskKey struct {
	runID string
	seq   int
}

// deliverSession applies a session-channel run's plan. Seats found gone are
// recorded and asks and their fallbacks go out whether or not the run has
// settled; answered seats end only once it has, because while a worker
// executes it ends kept seats itself. It reports whether some seat was not
// reached and the run should be tried again soon.
func (d *needsYouDispatcher) deliverSession(ctx context.Context, runID string, settled bool) (retry bool) {
	c := d.c
	// A verdict can advance to another gate while PasteAsk runs. Persist the
	// affected seat before planning any later ask.
	for key, fallback := range d.askFailures {
		if key.runID == runID && !d.recordAskFailure(key, fallback) {
			return true
		}
	}
	plan, err := c.engine.PlanRunOnCall(ctx, runID)
	if err != nil {
		log.Printf("session channel: run %s: %v", runID, err)
		return false
	}
	d.watch(runID, len(plan.Kept) > 0 || plan.OpenAsks > 0)
	for _, gone := range plan.Gone {
		if err := c.engine.RecordKeptSeatCleanup(runID, gone.Seat, gone.Outcome, "", ""); err != nil {
			log.Printf("session channel: run %s seat %s: %v", runID, gone.Seat.SlotID, err)
		}
	}
	if settled && len(plan.Answered) > 0 {
		d.endAnsweredSeats(ctx, runID)
	}
	for _, fallback := range plan.Fallbacks {
		if _, err := c.engine.RecordHumanAskFallback(runID, fallback); err != nil {
			log.Printf("session channel: run %s ask %d: %v", runID, fallback.Request.Seq, err)
		}
	}
	for _, delivery := range plan.Deliveries {
		if ctx.Err() != nil {
			return true
		}
		if !d.deliverAsk(ctx, runID, plan, delivery) {
			retry = true
			if _, pending := d.askFailures[humanAskKey{runID: runID, seq: delivery.Request.Seq}]; pending {
				// Persist the affected seat before any other ask can reach it.
				return true
			}
		}
	}
	return retry
}

// endAnsweredSeats ends the seats whose asks were all answered, holding the
// run's command reservation as a worker does, so no worker starts on the run
// while they end. A run that turned busy meanwhile ends them itself.
func (d *needsYouDispatcher) endAnsweredSeats(ctx context.Context, runID string) {
	c := d.c
	if !c.acquire(runID) {
		return
	}
	defer c.releaseQuietly(runID)
	plan, err := c.engine.PlanRunOnCall(ctx, runID)
	if err != nil {
		log.Printf("session channel: run %s: %v", runID, err)
		return
	}
	if err := c.engine.EndKeptSeatsNow(runID, plan.Answered, formations.SeatCauseAskAnswered, plan.Keeper); err != nil {
		log.Printf("session channel: run %s: ending answered seats: %v", runID, err)
	}
}

// deliverAsk writes one seat's brief, pastes its pointer and records the
// delivery. It reports whether the seat was reached and recorded.
func (d *needsYouDispatcher) deliverAsk(ctx context.Context, runID string, plan formations.RunOnCallPlan, delivery formations.HumanAskDelivery) bool {
	c := d.c
	key := humanAskKey{runID: runID, seq: delivery.Request.Seq}
	if fallback, ok := d.askFailures[key]; ok {
		return d.recordAskFailure(key, fallback)
	}
	// An earlier seat in this same plan may already have fallen back. Do not
	// continue pasting the stale plan into its other seats.
	events, err := c.store.ReadRunEvents(runID)
	if err != nil {
		return false
	}
	if humanGateFellBack(events, delivery.Request.Seq) {
		return true
	}
	if formations.HumanAskUncertainSeats(events)[delivery.Seat.CreatedSeq] {
		return false // a different ask in this plan made the seat unsafe; replan
	}
	brief, pointer, err := c.engine.WriteHumanAskBrief(runID, plan.Board, plan.Events, delivery, d.config.ServerURL, d.config.CLI)
	if err != nil {
		log.Printf("session channel: run %s ask %d brief for %s: %v", runID, delivery.Request.Seq, delivery.Seat.SlotID, err)
		return false
	}
	paste, cancel := context.WithTimeout(ctx, sessionPasteBudget)
	err = plan.Keeper.PasteAsk(paste, delivery.Seat, pointer)
	cancel()
	if err != nil {
		if errors.Is(err, formations.ErrHumanAskDeliveryUncertain) {
			log.Printf("session channel: run %s ask %d delivery into %s stopped: %v", runID, delivery.Request.Seq, delivery.Seat.SlotID, err)
			if d.askFailures == nil {
				d.askFailures = map[humanAskKey]formations.HumanAskFallback{}
			}
			fallback := formations.HumanAskFallback{Request: delivery.Request, Code: formations.AskFallbackDeliveryUncertain, Seat: delivery.Seat}
			d.askFailures[key] = fallback
			return d.recordAskFailure(key, fallback)
		}
		log.Printf("session channel: run %s ask %d not yet pasted into %s: %v", runID, delivery.Request.Seq, delivery.Seat.SlotID, err)
		return false
	}
	if _, err := c.engine.RecordHumanAskDelivered(runID, delivery, brief); err != nil {
		log.Printf("session channel: run %s ask %d delivered to %s but not recorded: %v", runID, delivery.Request.Seq, delivery.Seat.SlotID, err)
		return false
	}
	return true
}

func (d *needsYouDispatcher) recordAskFailure(key humanAskKey, fallback formations.HumanAskFallback) bool {
	if _, err := d.c.engine.RecordHumanAskFallback(key.runID, fallback); err != nil {
		log.Printf("session channel: run %s ask %d fallback: %v", key.runID, key.seq, err)
		return false
	}
	delete(d.askFailures, key)
	return true
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
