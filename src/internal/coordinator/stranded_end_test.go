package coordinator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Hold the worker after durable cleanup, with writes still available so an
// erroneous premature block would land. Releasing the barrier cuts off only
// this disposable ledger's writes before the worker attempts its final event.
// Reads and the original callback remain available throughout.
func holdFinalWriteFailure(t *testing.T, c *Coordinator) (awaitCleanup func(), release func(), awaitFault func(), restore func()) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("ledger permission fault requires an unprivileged test process")
	}
	var path string
	var pathMu sync.Mutex
	cleaned := make(chan error, 1)
	faulted := make(chan error, 1)
	proceed := make(chan struct{})
	var faultOnce, releaseOnce sync.Once
	original := c.store.OnRunEvent
	c.store.OnRunEvent = func(event formations.RunEvent) {
		if original != nil {
			original(event)
		}
		if event.Type == formations.RunEventSeatCleanup && event.Data["cause"] == formations.SeatCauseRunFinal {
			faultOnce.Do(func() {
				pathMu.Lock()
				path = filepath.Join(c.store.Workspace, ".archon", "runs", "proof", event.RunID+".ndjson")
				pathMu.Unlock()
				cleaned <- nil
				<-proceed
				faulted <- os.Chmod(path, 0o400)
			})
		}
	}
	release = func() { releaseOnce.Do(func() { close(proceed) }) }
	restore = func() {
		pathMu.Lock()
		defer pathMu.Unlock()
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		release()
		pathMu.Lock()
		defer pathMu.Unlock()
		_ = os.Chmod(path, 0o600)
	})
	awaitCleanup = func() {
		select {
		case err := <-cleaned:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(testPatience):
			t.Fatal("final cleanup did not reach the write-failure barrier")
		}
		if file, err := os.OpenFile(path, os.O_WRONLY, 0); err != nil {
			t.Fatalf("active barrier must leave writes available: %v", err)
		} else {
			file.Close()
		}
	}
	awaitFault = func() {
		select {
		case err := <-faulted:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(testPatience):
			t.Fatal("fixture ledger write fault was not installed")
		}
		if file, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
			file.Close()
			t.Fatal("fixture ledger still accepts writes")
		}
	}
	return awaitCleanup, release, awaitFault, restore
}

func awaitRunReleased(t *testing.T, c *Coordinator, id string) {
	t.Helper()
	c.mu.Lock()
	done := c.state(id).done
	c.mu.Unlock()
	select {
	case <-done:
	case <-time.After(testPatience):
		t.Fatal("run reservation did not release")
	}
}

func TestStrandedFinalWriteBecomesBlockedAfterStorageRecovers(t *testing.T) {
	keeper := &keeperExecutor{}
	notifier := &recordingNotifier{}
	c, root := onCallFixture(t, sessionBoard(testBoard), keeper, notifier)
	awaitCleanup, release, awaitFault, restore := holdFinalWriteFailure(t, c)
	id := startProof(t, c)
	first := awaitProjection(t, c, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1
	})
	response := "  rejected after inspecting the actual input\nkeep this exact response  "
	body, _ := json.Marshal(map[string]any{"requestedSeq": first.WaitingGates[0].RequestedSeq, "verdict": "fail", "reason": response, "relayedBy": "slot_work"})
	if w := post(t, c, "/api/runs/"+id+"/gates/gate_review/verdict", string(body)); w.Code != 202 {
		t.Fatalf("verdict %d %s", w.Code, w.Body.String())
	}
	awaitCleanup()
	before := eventsOf(t, c, id)
	if last := before[len(before)-1]; last.Type != formations.RunEventSeatCleanup || last.Data["cause"] != formations.SeatCauseRunFinal {
		t.Fatalf("durable cleanup missing: %+v", last)
	}
	// A dispatcher observing a busy finalization must never block or paste.
	c.needsYou.queue(id)
	settleQuietly()
	if after := eventsOf(t, c, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("busy finalization changed the ledger: %s", ledgerTrail(after))
	}
	if sent, _ := notifier.snapshot(); len(sent) != 0 {
		t.Fatalf("busy finalization announced %+v", sent)
	}
	release()
	awaitFault()
	awaitRunReleased(t, c, id)
	// Both the intended run_failed and recordFailure's fallback failed while
	// this ledger was unwritable. Cleanup is durable, no final event is.
	if after := eventsOf(t, c, id); !reflect.DeepEqual(before, after) {
		t.Fatalf("write fault did not strand the ending: %s", ledgerTrail(after))
	}
	// Witness an automatic failed recovery attempt before restoring storage,
	// so success must come from the existing scheduled retry, not a lucky
	// first dispatcher pass after the worker released.
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.needsYou.mu.Lock()
		retrying := c.needsYou.retrying[id]
		c.needsYou.mu.Unlock()
		if retrying {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stranded ending did not schedule an automatic storage retry")
		}
		time.Sleep(time.Millisecond)
	}
	restore()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		change := c.nextChange(id)
		p, err := c.Project(id)
		if err != nil {
			t.Fatal(err)
		}
		if p.Status == "blocked" && p.ResumeAllowed && !p.Final {
			break
		}
		select {
		case <-change:
		case <-ctx.Done():
			t.Fatalf("storage recovered but settled ending stayed silent: %+v, trail %s", p, ledgerTrail(eventsOf(t, c, id)))
		}
	}
	blockedEvents := eventsOf(t, c, id)
	if len(blockedEvents) != len(before)+1 || blockedEvents[len(before)].Type != formations.RunEventBlocked || blockedEvents[len(before)].Data["code"] != "run_finalization_interrupted" || !reflect.DeepEqual(before, blockedEvents[:len(before)]) {
		t.Fatalf("expected exactly one diagnostic block preserving history: %s", ledgerTrail(blockedEvents))
	}
	if sent := awaitNotifications(t, notifier, 1); sent[0].Kind != formations.NeedsYouKindBlocked {
		t.Fatalf("notification = %+v", sent)
	}
	settleQuietly()
	if after := eventsOf(t, c, id); len(after) != len(blockedEvents) {
		t.Fatalf("automatic retries duplicated the block: %s", ledgerTrail(after))
	}
	if sent, _ := notifier.snapshot(); len(sent) != 1 {
		t.Fatalf("automatic retries repeated the block notification: %+v", sent)
	}
	// Restart preserves the diagnostic, then explicit resume uses recorded
	// rejection and inputs without running the completed formation again.
	next := reopen(t, c, root, keeper)
	t.Cleanup(func() { next.Close() })
	if after := eventsOf(t, next, id); !reflect.DeepEqual(blockedEvents, after) {
		t.Fatalf("restart changed the block: %s", ledgerTrail(after))
	}
	if w := post(t, next, "/api/runs/"+id+"/resume", `{"actor":"operator","reason":"storage repaired"}`); w.Code != 202 {
		t.Fatalf("resume %d %s", w.Code, w.Body.String())
	}
	final := awaitState(t, next, id, "failed")
	after := eventsOf(t, next, id)
	if !final.Final || len(after) != len(blockedEvents)+2 || after[len(after)-1].Data["code"] != formations.RunFailurePathRejected || after[len(after)-1].Data["reason"] != response || !reflect.DeepEqual(blockedEvents, after[:len(blockedEvents)]) || !reflect.DeepEqual(first.Inputs, final.Inputs) {
		t.Fatalf("resume changed recorded outcome/input or repeated work: %+v, trail %s", final, ledgerTrail(after))
	}
	if pastes, ended := keeper.snapshot(); len(pastes) != 1 || len(ended) != 1 {
		t.Fatalf("unexpected seat work: pastes %v, ended %v", pastes, ended)
	}
}

func TestStrandedPendingGateIsBlockedAtStartup(t *testing.T) {
	keeper := &keeperExecutor{}
	c, root := onCallFixture(t, sessionBoard(testBoard), keeper, nil)
	awaitCleanup, release, awaitFault, restore := holdFinalWriteFailure(t, c)
	id := startProof(t, c)
	first := awaitProjection(t, c, id, func(p *Projection) bool {
		return len(p.WaitingGates) == 1 && len(p.WaitingGates[0].AskedSeats) == 1
	})
	finished := make(chan int, 1)
	go func() {
		finished <- post(t, c, "/api/runs/"+id+"/abort", `{"reason":"operator stop","requestedBy":"operator"}`).Code
	}()
	awaitCleanup()
	release()
	awaitFault()
	select {
	case code := <-finished:
		if code == 200 {
			t.Fatal("abort incorrectly claimed its final event was written")
		}
	case <-time.After(testPatience):
		t.Fatal("failed abort did not settle")
	}
	before := eventsOf(t, c, id)
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	restore()
	next, err := Open(root, c.personas, func(store *formations.Store) formations.FormationExecutor { keeper.store = store; return keeper })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { next.Close() })
	if err := next.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	p, err := next.Project(id)
	if err != nil || p.Status != "blocked" || !p.ResumeAllowed || p.Final || len(p.WaitingGates) != 1 || p.WaitingGates[0].RequestedSeq != first.WaitingGates[0].RequestedSeq || !reflect.DeepEqual(first.Inputs, p.Inputs) {
		t.Fatalf("startup left a quiet ending or changed its pending input: %+v, %v", p, err)
	}
	after := eventsOf(t, next, id)
	if len(after) != len(before)+1 || after[len(before)].Data["code"] != "run_finalization_interrupted" || !reflect.DeepEqual(before, after[:len(before)]) {
		t.Fatalf("startup invented a final outcome or changed history: %s", ledgerTrail(after))
	}
	if err := next.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	if again := eventsOf(t, next, id); !reflect.DeepEqual(after, again) {
		t.Fatalf("second startup duplicated the block: %s", ledgerTrail(again))
	}
	if w := post(t, next, "/api/runs/"+id+"/resume", `{"actor":"operator","reason":"storage repaired"}`); w.Code != 202 {
		t.Fatalf("resume %d %s", w.Code, w.Body.String())
	}
	waiting := awaitState(t, next, id, "waiting_human")
	if len(waiting.WaitingGates) != 1 || waiting.WaitingGates[0].RequestedSeq != first.WaitingGates[0].RequestedSeq {
		t.Fatalf("resume replaced the original request: %+v", waiting)
	}
	verdict(t, next, id, "gate_review", first.WaitingGates[0].RequestedSeq, false, "slot_work")
	awaitState(t, next, id, "failed")
	if pastes, ended := keeper.snapshot(); len(pastes) != 1 || len(ended) != 1 {
		t.Fatalf("resume repeated completed work: pastes %v, ended %v", pastes, ended)
	}
}
