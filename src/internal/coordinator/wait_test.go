package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

type waitResponse struct {
	code   int
	result RunWait
	body   string
}

func waitRequest(t *testing.T, c *Coordinator, id, query string) waitResponse {
	t.Helper()
	w := httptest.NewRecorder()
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/formations/runs/"+id+"/wait?"+query, nil))
	response := waitResponse{code: w.Code, body: w.Body.String()}
	if w.Code == 200 {
		var envelope struct {
			Data RunWait `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		response.result = envelope.Data
	}
	return response
}

// waitAsync starts a wait and returns its answer with the time it arrived.
func waitAsync(t *testing.T, c *Coordinator, id, query string) <-chan struct {
	waitResponse
	at time.Time
} {
	done := make(chan struct {
		waitResponse
		at time.Time
	}, 1)
	go func() {
		response := waitRequest(t, c, id, query)
		done <- struct {
			waitResponse
			at time.Time
		}{response, time.Now()}
	}()
	return done
}

func TestWaitReturnsTheGateTheRunNeedsWithinASecond(t *testing.T) {
	c, e, _ := fixture(t)
	id := startRun(t, c)
	<-e.entered
	pending := waitAsync(t, c, id, "until=needs-you&hold=10")
	select {
	case got := <-pending:
		t.Fatalf("wait answered before the run needed anyone: %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
	e.proceed <- struct{}{}
	got := <-pending
	if got.code != 200 {
		t.Fatalf("%d %s", got.code, got.body)
	}
	events, _ := c.store.ReadRunEvents(id)
	var requested formations.RunEvent
	for _, event := range events {
		if event.Type == formations.RunEventHumanInputRequested {
			requested = event
		}
	}
	at, err := time.Parse(time.RFC3339Nano, requested.Timestamp)
	if err != nil {
		t.Fatal(err)
	}
	if lag := got.at.Sub(at); lag > time.Second {
		t.Fatalf("wait answered %s after the request", lag)
	}
	r := got.result
	if r.Outcome != WaitOutcomeNeedsYou || r.Final || r.Seq < requested.Seq || r.Mission != "Proof" || len(r.Asks) != 1 {
		t.Fatalf("result = %+v", r)
	}
	ask := r.Asks[0]
	if ask.Kind != formations.NeedsYouKindHumanGate || !ask.New || ask.GateID != "gate_review" || ask.Title != "Review" || ask.Seq != requested.Seq || ask.Criterion != "PRIVATE-CRITERION" {
		t.Fatalf("ask = %+v", ask)
	}
	if ask.Input == nil || ask.Input.FromTitle != "Work" || ask.Input.Text != "PRIVATE-OUTPUT" || ask.Input.Truncated {
		t.Fatalf("input = %+v", ask.Input)
	}
	if len(ask.Routes) != 2 {
		t.Fatalf("routes = %+v", ask.Routes)
	}

	// Waiting again from the cursor holds: the driver already knows this ask.
	again := waitRequest(t, c, id, "until=needs-you&hold=0&since="+strconv.Itoa(r.Seq))
	if again.result.Outcome != WaitOutcomePending || again.result.Seq != r.Seq || len(again.result.Asks) != 1 || again.result.Asks[0].New {
		t.Fatalf("again = %+v", again.result)
	}

	// Any change after the cursor answers with the events since it.
	changes := waitAsync(t, c, id, "until=any-change&hold=10&since="+strconv.Itoa(r.Seq))
	if w := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(ask.Seq)+`,"verdict":"pass"}`); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	changed := <-changes
	if changed.result.Outcome != WaitOutcomeChanged || len(changed.result.Changes) == 0 || changed.result.Changes[0].Seq != r.Seq+1 || changed.result.Changes[0].Type != formations.RunEventHumanVerdictRecorded {
		t.Fatalf("changed = %+v", changed.result)
	}

	// Final waits through the rest and says how the run ended.
	final := waitAsync(t, c, id, "until=final&hold=10&since="+strconv.Itoa(changed.result.Seq))
	<-e.entered
	e.proceed <- struct{}{}
	ended := <-final
	if ended.result.Outcome != WaitOutcomeFinal || ended.result.End == nil || ended.result.End.Status != formations.RunStatusSucceeded || len(ended.result.Asks) != 0 {
		t.Fatalf("ended = %+v", ended.result)
	}
	// A final run answers every mode at once, even from its last sequence.
	for _, until := range []string{WaitUntilNeedsYou, WaitUntilAnyChange, WaitUntilFinal} {
		got := waitRequest(t, c, id, "hold=10&until="+until+"&since="+strconv.Itoa(ended.result.Seq))
		if got.result.Outcome != WaitOutcomeFinal {
			t.Fatalf("%s: %+v", until, got.result)
		}
	}
}

func TestWaitSaysWhoEndedACanceledRunAndWhy(t *testing.T) {
	c, e, _ := fixture(t)
	id := startRun(t, c)
	<-e.entered
	e.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	final := waitAsync(t, c, id, "until=final&hold=10")
	if w := post(t, c, "/api/formations/runs/"+id+"/abort", `{"reason":"wrong brief","requestedBy":"operator:archon"}`); w.Code >= 300 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got := <-final
	end := got.result.End
	if got.result.Outcome != WaitOutcomeFinal || end == nil || end.Status != formations.RunStatusCanceled || end.Reason != "wrong brief" || end.EndedBy != "operator:archon" {
		t.Fatalf("result = %+v end = %+v", got.result, end)
	}
	if len(end.Stopped) != 1 || end.Stopped[0].Title != "Review" {
		t.Fatalf("stopped = %+v", end.Stopped)
	}
}

func TestWaitAnswersStoppingDaemonsWithRetry(t *testing.T) {
	c, e, _ := fixture(t)
	id := startRun(t, c)
	<-e.entered
	pending := waitAsync(t, c, id, "until=final&hold=30")
	time.Sleep(100 * time.Millisecond)
	c.BeginShutdown()
	select {
	case got := <-pending:
		if got.code != 503 {
			t.Fatalf("%d %s", got.code, got.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait held through shutdown")
	}
}

func TestWaitRejectsBadRequests(t *testing.T) {
	c, e, _ := fixture(t)
	id := startRun(t, c)
	<-e.entered
	for query, code := range map[string]int{
		"until=soon":       400,
		"since=-1":         400,
		"since=9999":       400,
		"hold=61":          400,
		"hold=abc":         400,
		"until=any-change": 200,
	} {
		path := id
		if code == 200 {
			query += "&hold=0"
		}
		if got := waitRequest(t, c, path, query); got.code != code {
			t.Fatalf("%s: %d %s", query, got.code, got.body)
		}
	}
	if got := waitRequest(t, c, "run_missing", "hold=0"); got.code != 404 {
		t.Fatalf("missing run: %d %s", got.code, got.body)
	}
}

// A block recorded inside a command, as a verdict does before its automatic
// resume, is not an ask until the run settles, and the cursor stays below it.
func TestWaitCountsABlockOnlyOnceSettled(t *testing.T) {
	events := []formations.RunEvent{
		{RunID: "run_x", Seq: 1, Type: formations.RunEventStarted, Data: map[string]any{"boardSlug": "proof"}},
		{RunID: "run_x", Seq: 2, Type: formations.RunEventNodeStarted, NodeID: "fmn_work"},
		{RunID: "run_x", Seq: 3, Type: formations.RunEventBlocked, NodeID: "fmn_work", Data: map[string]any{"reason": "seat lost", "code": "seat_lost", "resumeAllowed": true}},
	}
	busy, err := projectWait("run_x", events, nil, WaitUntilNeedsYou, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if busy.Outcome != WaitOutcomePending || busy.Seq != 1 || len(busy.Asks) != 0 {
		t.Fatalf("busy = %+v", busy)
	}
	changed, err := projectWait("run_x", events, nil, WaitUntilAnyChange, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Outcome != WaitOutcomeChanged || changed.Seq != 3 {
		t.Fatalf("changed = %+v", changed)
	}
	settled, err := projectWait("run_x", events, nil, WaitUntilNeedsYou, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Outcome != WaitOutcomeNeedsYou || settled.Seq != 3 || len(settled.Asks) != 1 {
		t.Fatalf("settled = %+v", settled)
	}
	ask := settled.Asks[0]
	if ask.Kind != formations.NeedsYouKindBlocked || ask.Reason != "seat lost" || ask.Code != "seat_lost" || !ask.ResumeAllowed || ask.Title != "fmn_work" {
		t.Fatalf("ask = %+v", ask)
	}
}
