package coordinator

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// archon-o7p.19: many waiters and streams on one run stay fast and do not
// delay the run's appends, and unknown run IDs allocate nothing.

// bigLedgerRun starts a run whose ledger holds about 580 KB of events,
// written straight to the file as appends would write them.
func bigLedgerRun(t *testing.T, c *Coordinator) (string, int) {
	t.Helper()
	started, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", ExpectedBoardRev: 1, Personas: c.personas})
	if err != nil {
		t.Fatal(err)
	}
	events, err := c.store.ReadRunEvents(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	first := events[0]
	ledger, err := os.OpenFile(filepath.Join(c.store.Workspace, ".archon", "runs", "proof", started.RunID+".ndjson"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ledger.Close()
	detail := strings.Repeat("the seat has not reached its ready prompt ", 8)
	var lines bytes.Buffer
	count := 1
	for lines.Len() < 580<<10 {
		count++
		raw, err := json.Marshal(formations.RunEvent{Seq: count, RunID: started.RunID, Type: formations.RunEventSeatState, Timestamp: first.Timestamp, Actor: first.Actor,
			BoardID: first.BoardID, BoardRev: first.BoardRev, MissionID: first.MissionID, NodeID: "fmn_work", SlotID: "slot_work",
			Data: map[string]any{"state": formations.SeatStateNotReady, "detail": detail}})
		if err != nil {
			t.Fatal(err)
		}
		lines.Write(append(raw, '\n'))
	}
	if _, err := ledger.Write(lines.Bytes()); err != nil {
		t.Fatal(err)
	}
	return started.RunID, count
}

func TestFiftyWaitersOnABigLedgerAnswerWithinASecond(t *testing.T) {
	c, _, _ := fixture(t)
	id, count := bigLedgerRun(t, c)
	const waiters = 50
	var ready, done sync.WaitGroup
	answered := make(chan time.Duration, waiters)
	var appended time.Time
	var appendedMu sync.Mutex
	for range waiters {
		ready.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			w := httptest.NewRecorder()
			request := httptest.NewRequest("GET", "/api/runs/"+id+"/wait?until=any-change&hold=30&since="+strconv.Itoa(count), nil)
			ready.Done()
			c.Handler().ServeHTTP(w, request)
			appendedMu.Lock()
			at := appended
			appendedMu.Unlock()
			if w.Code != 200 || !strings.Contains(w.Body.String(), `"outcome":"changed"`) {
				t.Errorf("waiter answered %d %s", w.Code, w.Body.String()[:min(200, w.Body.Len())])
			}
			answered <- time.Since(at)
		}()
	}
	ready.Wait()
	// Let every waiter read the ledger and hold.
	time.Sleep(500 * time.Millisecond)
	// The clock starts once the event is durable: a slow disk under host load
	// delays the event itself, not the waiters.
	appendedMu.Lock()
	before := time.Now()
	if err := c.store.AppendRunEvent(id, formations.RunEvent{Type: formations.RunEventSeatState, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"state": formations.SeatStateWorking}}); err != nil {
		t.Fatal(err)
	}
	appended = time.Now()
	appendTook := appended.Sub(before)
	appendedMu.Unlock()
	done.Wait()
	close(answered)
	slowest := time.Duration(0)
	for took := range answered {
		slowest = max(slowest, took)
	}
	t.Logf("append %v, slowest of %d waiters %v", appendTook, waiters, slowest)
	if slowest > time.Second {
		t.Fatalf("the slowest of %d waiters answered after %v, want within 1 s", waiters, slowest)
	}
	// Another append while the waiters' reads are fresh is not held up by them.
	start := time.Now()
	if err := c.store.AppendRunEvent(id, formations.RunEvent{Type: formations.RunEventSeatState, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"state": formations.SeatStateNotReady}}); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("an append took %v", took)
	}
}

func TestUnknownRunIDsAllocateNothing(t *testing.T) {
	c, _, _ := fixture(t)
	for _, path := range []string{"/api/runs/run_unknown/wait?until=any-change&hold=0", "/api/runs/run_unknown/stream"} {
		w := httptest.NewRecorder()
		c.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Fatalf("%s answered %d %s", path, w.Code, w.Body.String())
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, allocated := c.runs["run_unknown"]; allocated {
		t.Fatal("an unknown run ID allocated state")
	}
}
