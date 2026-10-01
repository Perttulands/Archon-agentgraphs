package formations

import (
	"errors"
	"reflect"
	"testing"
)

// A human gate blocks only the work it gates (archon-o7p.11).

// A request waits from its ask until a verdict on its gate, a newer
// evaluation of that gate, which replaces it, or the run's end.
func TestOpenHumanRequestsCloseOnAVerdictANewerEvaluationOrTheEnd(t *testing.T) {
	events := []RunEvent{
		{Seq: 1, Type: RunEventStarted},
		{Seq: 2, Type: RunEventGateEvaluating, GateID: "g1"},
		{Seq: 3, Type: RunEventHumanInputRequested, GateID: "g1"},
		{Seq: 4, Type: RunEventGateEvaluating, GateID: "g2"},
		{Seq: 5, Type: RunEventHumanInputRequested, GateID: "g2"},
	}
	open := func(events []RunEvent) []int {
		seqs := []int{}
		for _, request := range OpenHumanRequests(events) {
			seqs = append(seqs, request.Seq)
		}
		return seqs
	}
	if got := open(events); !reflect.DeepEqual(got, []int{3, 5}) {
		t.Fatalf("open = %v, want both gates", got)
	}
	answered := append(append([]RunEvent{}, events...), RunEvent{Seq: 6, Type: RunEventHumanVerdictRecorded, GateID: "g2"})
	if got := open(answered); !reflect.DeepEqual(got, []int{3}) {
		t.Fatalf("after g2's verdict = %v", got)
	}
	// g1 evaluates a newer input whose code check fails: no new ask, and the
	// old one no longer waits.
	replaced := append(append([]RunEvent{}, events...), RunEvent{Seq: 6, Type: RunEventGateEvaluating, GateID: "g1"}, RunEvent{Seq: 7, Type: RunEventGateVerdict, GateID: "g1"})
	if got := open(replaced); !reflect.DeepEqual(got, []int{5}) {
		t.Fatalf("after g1 evaluated again = %v", got)
	}
	if _, ok := latestHumanRequest(replaced, "g1"); ok {
		t.Fatal("a replaced request still takes a verdict")
	}
	ended := append(append([]RunEvent{}, events...), RunEvent{Seq: 6, Type: RunEventCanceled})
	if got := open(ended); len(got) != 0 {
		t.Fatalf("after the run ended = %v", got)
	}
}

// A block beside a waiting gate is its own ask: the gate explains nothing
// about the branch that blocked.
func TestABlockBesideAWaitingGateIsAnAsk(t *testing.T) {
	events := []RunEvent{
		{Seq: 1, Type: RunEventStarted, RunID: "run"},
		{Seq: 2, Type: RunEventHumanInputRequested, GateID: "g1", RunID: "run", Data: map[string]any{"prompt": "Good?"}},
		{Seq: 3, Type: RunEventBlocked, NodeID: "fmn_b", RunID: "run", Data: map[string]any{"reason": "seat died", "resumeAllowed": true}},
	}
	asks, err := ProjectSettledNeedsYouAsks(events)
	if err != nil {
		t.Fatal(err)
	}
	if len(asks) != 2 || asks[0].Kind != NeedsYouKindHumanGate || asks[1].Kind != NeedsYouKindBlocked || asks[1].Seq != 3 || !asks[1].ResumeAllowed {
		t.Fatalf("asks = %+v, want the gate then the block", asks)
	}
}

// A verdict is accepted on a blocked run, which routes it once resumed, and a
// request takes one verdict only.
func TestAVerdictLandsOnABlockedRunAndOnlyOnce(t *testing.T) {
	executor := &seatLossOnceExecutor{failNodeID: "fmn_b"}
	store, _, engine, status := startBranchingRun(t, branchingGateBoardFixture(), executor)
	if status.Status != RunStatusBlocked {
		t.Fatalf("status = %+v, want B's block", status)
	}
	request, ok := latestHumanRequest(mustEvents(t, store, status.RunID), "gate_review")
	if !ok {
		t.Fatal("the gate does not wait")
	}
	verdict := HumanGateVerdictRequest{GateID: "gate_review", RequestedSeq: request.Seq, Verdict: "pass", Actor: "human:operator"}
	if status, err := engine.RecordHumanGateVerdict(status.RunID, verdict); err != nil || status.Status != RunStatusBlocked || !status.ResumeAllowed {
		t.Fatalf("verdict on the blocked run = %+v, %v", status, err)
	}
	if _, err := engine.RecordHumanGateVerdict(status.RunID, verdict); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a second verdict = %v, want the request gone", err)
	}
	// A verdict naming an older request of the gate is refused too.
	stale := verdict
	stale.RequestedSeq = request.Seq - 1
	if _, err := engine.RecordHumanGateVerdict(status.RunID, stale); err == nil {
		t.Fatal("a verdict naming another request was recorded")
	}
	events := mustEvents(t, store, status.RunID)
	if last := lifecycleLedger(events); last[len(last)-1].Type != RunEventBlocked {
		t.Fatalf("the verdict hid the block: %s", eventTypeTrail(events))
	}
	status, err := engine.ResumeRun(status.RunID, RunResumeRequest{Actor: "agent:test", Mode: "redispatch", Reason: "run B again"})
	if err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("resume = %+v, %v", status, err)
	}
}

// Two verdicts racing on one request: the ledger's append lock lets one land.
func TestOnlyOneOfTwoRacingVerdictsLands(t *testing.T) {
	store, _, engine, status := startBranchingRun(t, linearGateBoardFixture(), &fakeRunExecutor{})
	request, ok := latestHumanRequest(mustEvents(t, store, status.RunID), "gate_review")
	if !ok {
		t.Fatal("the gate does not wait")
	}
	results := make(chan error, 2)
	for _, verdict := range []string{"pass", "fail"} {
		go func() {
			_, err := engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", RequestedSeq: request.Seq, Verdict: verdict, Actor: "human:operator"})
			results <- err
		}()
	}
	landed := 0
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			landed++
		case errors.Is(err, ErrHumanRequestNotPending), errors.Is(err, ErrNotFound):
		default:
			t.Fatal(err)
		}
	}
	recorded := 0
	for _, event := range mustEvents(t, store, status.RunID) {
		if event.Type == RunEventHumanVerdictRecorded {
			recorded++
		}
	}
	if landed != 1 || recorded != 1 {
		t.Fatalf("landed %d, recorded %d; want one", landed, recorded)
	}
}
