package coordinator

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
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
	c.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/runs/"+id+"/wait?"+query, nil))
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
	if w := post(t, c, "/api/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(ask.Seq)+`,"verdict":"pass"}`); w.Code != 202 {
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
	if w := post(t, c, "/api/runs/"+id+"/abort", `{"reason":"wrong brief","requestedBy":"operator:archon"}`); w.Code >= 300 {
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
		{RunID: "run_x", Seq: 1, Type: formations.RunEventStarted, Data: map[string]any{"missionSlug": "proof"}},
		{RunID: "run_x", Seq: 2, Type: formations.RunEventNodeStarted, NodeID: "fmn_work"},
		{RunID: "run_x", Seq: 3, Type: formations.RunEventBlocked, NodeID: "fmn_work", Data: map[string]any{"reason": "seat lost", "code": "seat_lost", "resumeAllowed": true}},
	}
	busy, err := projectWait("run_x", events, nil, WaitUntilNeedsYou, 1, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if busy.Outcome != WaitOutcomePending || busy.Seq != 1 || len(busy.Asks) != 0 {
		t.Fatalf("busy = %+v", busy)
	}
	// Any change reports the events below the block and stops there, and the
	// run reads as still running, never as blocked, until it settles.
	changed, err := projectWait("run_x", events, nil, WaitUntilAnyChange, 1, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Outcome != WaitOutcomeChanged || changed.Seq != 2 || len(changed.Changes) != 1 || changed.Changes[0].Seq != 2 || changed.Status != formations.RunStatusRunning {
		t.Fatalf("changed = %+v", changed)
	}
	// With nothing new below the block, any change stays pending.
	held, err := projectWait("run_x", events, nil, WaitUntilAnyChange, 2, false, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if held.Outcome != WaitOutcomePending || held.Seq != 2 || held.Status != formations.RunStatusRunning {
		t.Fatalf("held = %+v", held)
	}
	// Once settled, the same cursor reports the block as a new ask in every mode.
	for _, until := range []string{WaitUntilAnyChange, WaitUntilNeedsYou} {
		got, err := projectWait("run_x", events, nil, until, 2, true, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if got.Outcome != WaitOutcomeNeedsYou || len(got.Asks) != 1 || !got.Asks[0].New || got.Status != formations.RunStatusBlocked {
			t.Fatalf("%s settled = %+v", until, got)
		}
	}
	settled, err := projectWait("run_x", events, nil, WaitUntilNeedsYou, 1, true, time.Time{})
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

// A driver agent consumes wait text, so the gate criterion and input are
// verbatim (no redaction) and only the input is capped.
func TestWaitServesGateTextVerbatimAndCapsTheInput(t *testing.T) {
	long := strings.Repeat("x", waitInputExcerptBytes+10)
	events := []formations.RunEvent{
		{RunID: "run_x", Seq: 1, Type: formations.RunEventStarted, Data: map[string]any{"missionSlug": "proof"}},
		{RunID: "run_x", Seq: 2, Type: formations.RunEventHumanInputRequested, GateID: "gate_review", NodeID: "gate_review", Data: map[string]any{
			"prompt":   "Check with password=hunter2",
			"inputRef": map[string]any{"fromNodeId": "fmn_work", "text": "token: abc123\n" + long},
		}},
	}
	got, err := projectWait("run_x", events, nil, WaitUntilNeedsYou, 0, true, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	ask := got.Asks[0]
	if ask.Criterion != "Check with password=hunter2" || !strings.HasPrefix(ask.Input.Text, "token: abc123\n") || !ask.Input.Truncated || len(ask.Input.Text) != waitInputExcerptBytes || ask.Input.Bytes <= waitInputExcerptBytes {
		t.Fatalf("criterion %q input bytes %d truncated %v", ask.Criterion, len(ask.Input.Text), ask.Input.Truncated)
	}
}

// The reviewer's probe: a driver looping any-change waits over a run that a
// gate approval pushes into the mission's Limit card. Every loop must end
// with the block as a new ask, never a changed answer that skips it and then
// hangs, and a verdict sent the moment the gate is reported must be accepted.
func TestAnyChangeLoopReportsALimitBlockAndItsGateVerdictIsAccepted(t *testing.T) {
	for round := 0; round < 5; round++ {
		c, e, _ := fixture(t)
		// The mission may run one step: Work. Approving starts After, which
		// finds the mission's rounds spent.
		limited := testBoard + "[[limit]]\nid = \"lim_mission\"\ntitle = \"Cap\"\ntarget = \"mis_proof\"\nrounds = 1\n"
		if err := os.WriteFile(c.store.BoardPath("proof"), []byte(limited), 0600); err != nil {
			t.Fatal(err)
		}
		w := post(t, c, "/api/runs", `{"cwd":`+strconv.Quote(c.store.Workspace)+`,"brief":"probe","mission":"proof","inputCardId":"mis_proof","expectedRev":1}`)
		if w.Code != 202 {
			t.Fatalf("start %d %s", w.Code, w.Body.String())
		}
		var receipt struct {
			Data struct {
				RunID string `json:"runId"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
			t.Fatal(err)
		}
		id := receipt.Data.RunID
		go func() { <-e.entered; e.proceed <- struct{}{} }()
		since, blocked := 0, false
		for i := 0; i < 100 && !blocked; i++ {
			got := waitRequest(t, c, id, "until=any-change&hold=5&since="+strconv.Itoa(since))
			r := got.result
			if got.code != 200 || r.Outcome == WaitOutcomePending || r.Outcome == WaitOutcomeFinal {
				t.Fatalf("round %d: the loop stalled or ended at since %d: %d %+v", round, since, got.code, r)
			}
			if !r.Settled && r.Status == formations.RunStatusBlocked {
				t.Fatalf("round %d: an unsettled run read as blocked: %+v", round, r)
			}
			for _, ask := range r.Asks {
				if !ask.New {
					continue
				}
				switch ask.Kind {
				case formations.NeedsYouKindHumanGate:
					// Answer at once, as a driver does; the verdict must not race
					// the command that is still settling the run.
					if v := post(t, c, "/api/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(ask.Seq)+`,"verdict":"pass"}`); v.Code != 202 {
						t.Fatalf("round %d: verdict right after needs-you: %d %s", round, v.Code, v.Body.String())
					}
				case formations.NeedsYouKindBlocked:
					blocked = true
				}
			}
			since = r.Seq
		}
		if !blocked {
			t.Fatalf("round %d: the limit block was never reported", round)
		}
	}
}

// A resume sent the moment a blocking escalation is reported waits for the
// command that recorded it instead of answering 409.
func TestResumeWaitsForTheCommandThatRecordedTheEscalation(t *testing.T) {
	c, _, _ := fixture(t)
	started, err := c.store.StartRun("proof", formations.RunStartRequest{MissionID: "mis_proof", ExpectedBoardRev: 1, Personas: c.personas})
	if err != nil {
		t.Fatal(err)
	}
	id := started.RunID
	if !c.acquire(id) { // the command that captures the escalation
		t.Fatal("reserve")
	}
	if _, err := c.store.RecordEscalationFromCapture(id, "fmn_work", `<<<ARCHON-ESCALATE run-id=`+id+` severity=stop reason="credentials missing">>>`); err != nil {
		t.Fatal(err)
	}
	got := waitRequest(t, c, id, "until=needs-you&hold=0")
	if got.result.Outcome != WaitOutcomeNeedsYou || len(got.result.Asks) != 1 || got.result.Asks[0].Kind != formations.NeedsYouKindEscalation {
		t.Fatalf("wait = %+v", got.result)
	}
	go func() { time.Sleep(200 * time.Millisecond); c.release(id) }()
	if w := post(t, c, "/api/runs/"+id+"/resume", `{"reason":"credentials added"}`); w.Code != 202 {
		t.Fatalf("resume right after the escalation: %d %s", w.Code, w.Body.String())
	}
}
