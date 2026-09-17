package formations

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// keepingExecutor scripts formation outputs like chainScriptExecutor and records
// seats the way the tmux executor does: a seat_created per slot, then either
// kept_on_call or ended as the formation finishes. It keeps seats as a
// SeatKeeper, without tmux.
type keepingExecutor struct {
	chainScriptExecutor
	store *Store
	mu    sync.Mutex
	keeps map[string][]bool
	ended []string
	gone  map[string]bool
}

func (k *keepingExecutor) ExecuteFormation(req FormationExecution) (FormationExecutionResult, error) {
	k.mu.Lock()
	if k.keeps == nil {
		k.keeps = map[string][]bool{}
	}
	k.keeps[req.NodeID] = append(k.keeps[req.NodeID], req.KeepSeatsOnCall)
	k.mu.Unlock()
	for _, slot := range req.Formation.Slots {
		if err := k.store.AppendRunEvent(req.RunID, RunEvent{Type: RunEventSeatCreated, NodeID: req.NodeID, SlotID: slot.ID, Data: map[string]any{
			"sessionName": "form-" + slot.ID, "sessionId": fmt.Sprintf("$%s-%d", slot.ID, req.Attempt), "paneId": fmt.Sprintf("%%%s-%d", slot.ID, req.Attempt), "harness": "openai-codex",
		}}); err != nil {
			return FormationExecutionResult{}, err
		}
	}
	result, err := k.chainScriptExecutor.ExecuteFormation(req)
	for _, slot := range req.Formation.Slots {
		outcome := SeatOutcomeEnded
		if err == nil && req.KeepSeatsOnCall && (req.Formation.Type != FormationTypeOrchestrated || slot.Controller) {
			outcome = SeatOutcomeKeptOnCall
		}
		if appendErr := k.store.AppendRunEvent(req.RunID, RunEvent{Type: RunEventSeatCleanup, NodeID: req.NodeID, SlotID: slot.ID, Data: map[string]any{"sessionName": "form-" + slot.ID, "outcome": outcome}}); appendErr != nil {
			return FormationExecutionResult{}, appendErr
		}
	}
	return result, err
}

func (k *keepingExecutor) EndKeptSeat(_ context.Context, seat KeptSeat) (string, string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.gone[seat.SessionID] {
		return SeatOutcomeGone, ""
	}
	k.ended = append(k.ended, seat.SessionID)
	return SeatOutcomeEnded, ""
}

func (k *keepingExecutor) ProbeKeptSeat(_ context.Context, seat KeptSeat) (bool, string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.gone[seat.SessionID] {
		return false, SeatOutcomeGone
	}
	return true, ""
}

func (k *keepingExecutor) PasteAsk(context.Context, KeptSeat, string) error { return nil }

func sessionChannel(board string) string {
	return strings.Replace(board, `beadId = "home-7kc4.7"`, `beadId = "home-7kc4.7"
humanChannel = "session"`, 1)
}

func startKeepingRun(t *testing.T, board string, executor *keepingExecutor) (*RunEngine, *Store, string) {
	t.Helper()
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), board)
	executor.store = store
	document, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{
		MissionID: "mis_showcase", Actor: "agent:test", ExpectedBoardETag: document.ETag, ExpectedBoardRev: document.Rev,
		Limits: RunLimits{MaxDispatch: 20, MaxAttempts: 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, store, status.RunID
}

// seatTrail lists seat and run lifecycle events after a sequence.
func seatTrail(t *testing.T, store *Store, runID string) []string {
	t.Helper()
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	var trail []string
	for _, event := range events {
		switch event.Type {
		case RunEventSeatCreated:
			trail = append(trail, "created "+event.SlotID)
		case RunEventSeatCleanup:
			entry := stringFromEventData(event, "outcome") + " " + event.SlotID
			if cause := stringFromEventData(event, "cause"); cause != "" {
				entry += " " + cause
			}
			trail = append(trail, entry)
		case RunEventHumanInputRequested:
			trail = append(trail, "ask "+event.GateID)
		case RunEventBlocked, RunEventResumed, RunEventSucceeded, RunEventFailed, RunEventCanceled:
			trail = append(trail, event.Type)
		}
	}
	return trail
}

func TestSessionChannelKeepsTheWorkSeatAndEndsItBeforeTheRunSucceeds(t *testing.T) {
	executor := &keepingExecutor{}
	engine, store, runID := startKeepingRun(t, sessionChannel(s5HumanGateBoardFixture()), executor)
	if got := KeptSeats(mustEvents(t, store, runID)); len(got) != 1 || got[0].SlotID != "slot_work" {
		t.Fatalf("kept seats = %+v, want the work seat", got)
	}
	if _, err := engine.RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator", RelayedBy: "slot_work"}); err != nil {
		t.Fatal(err)
	}
	if status, err := engine.ResumeRun(runID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"}); err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("resume = %+v, %v", status, err)
	}
	want := []string{
		"created slot_work", "kept_on_call slot_work", "ask gate_review", "run_blocked", "run_resumed",
		"created slot_ship", "ended slot_ship", "ended slot_work run_final", "run_succeeded",
	}
	if got := seatTrail(t, store, runID); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("trail = %v\nwant    %v", got, want)
	}
	if fmt.Sprint(executor.keeps) != "map[fmn_ship:[false] fmn_work:[true]]" || fmt.Sprint(executor.ended) != "[$slot_work-1]" {
		t.Fatalf("keep flags %v, ended %v", executor.keeps, executor.ended)
	}
}

func mustEvents(t *testing.T, store *Store, runID string) []RunEvent {
	t.Helper()
	events, err := store.ReadRunEvents(runID)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestNotifyChannelEndsSeatsAsBefore(t *testing.T) {
	executor := &keepingExecutor{}
	_, store, runID := startKeepingRun(t, s5HumanGateBoardFixture(), executor)
	if got := seatTrail(t, store, runID); fmt.Sprint(got) != "[created slot_work ended slot_work ask gate_review]" || fmt.Sprint(executor.keeps) != "map[fmn_work:[false]]" {
		t.Fatalf("notify trail = %v, keeps %v", got, executor.keeps)
	}
}

func TestJudgeGateBeforeHumanGateKeepsTheWorkSeatAndASendBackEndsItFirst(t *testing.T) {
	executor := &keepingExecutor{chainScriptExecutor: chainScriptExecutor{script: map[string][]string{
		"fmn_review_judge": {judgeBlock("pass", "survives"), judgeBlock("pass", "survives again")},
	}}}
	engine, store, runID := startKeepingRun(t, sessionChannel(gateChainBoardFixture(`["human"]`)), executor)
	want := []string{"created fmn_draft_slot", "kept_on_call fmn_draft_slot", "created fmn_review_judge_slot", "ended fmn_review_judge_slot", "ask gate_signoff"}
	if got := seatTrail(t, store, runID); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("trail = %v\nwant    %v", got, want)
	}
	before := len(mustEvents(t, store, runID))
	if _, err := engine.RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: "gate_signoff", Verdict: "fail", Reason: "tighten it", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ResumeRun(runID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"}); err != nil {
		t.Fatal(err)
	}
	var after []string
	for _, event := range mustEvents(t, store, runID)[before:] {
		if event.Type == RunEventSeatCreated || event.Type == RunEventSeatCleanup {
			after = append(after, event.Type+" "+event.SlotID+" "+stringFromEventData(event, "outcome")+" "+stringFromEventData(event, "cause"))
		}
	}
	// The old draft seat ends before the new attempt creates a seat with its name.
	if len(after) < 2 || after[0] != "seat_cleanup fmn_draft_slot ended new_attempt" || after[1] != "seat_created fmn_draft_slot  " {
		t.Fatalf("send-back seat events = %v", after)
	}
	if fmt.Sprint(executor.keeps["fmn_review_judge"]) != "[false false]" || fmt.Sprint(executor.keeps["fmn_draft"]) != "[true true]" {
		t.Fatalf("keep flags = %v", executor.keeps)
	}
}

// blockedKeepingRun reaches a blocked session run that still keeps its work
// seat: the operator approved, then the next formation lost its seat.
func blockedKeepingRun(t *testing.T) (*RunEngine, *Store, string, *keepingExecutor) {
	t.Helper()
	executor := &keepingExecutor{chainScriptExecutor: chainScriptExecutor{script: map[string][]string{"fmn_ship": {seatLost, "shipped"}}}}
	engine, store, runID := startKeepingRun(t, sessionChannel(s5HumanGateBoardFixture()), executor)
	if _, err := engine.RecordHumanGateVerdict(runID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Actor: "human:operator"}); err != nil {
		t.Fatal(err)
	}
	status, err := engine.ResumeRun(runID, RunResumeRequest{Actor: "coordinator", Mode: "reattach", Reason: "human verdict recorded"})
	if err != nil || status.Status != RunStatusBlocked {
		t.Fatalf("status = %+v, %v; want blocked by the lost ship seat", status, err)
	}
	if got := KeptSeats(mustEvents(t, store, runID)); len(got) != 1 {
		t.Fatalf("a blocked run keeps its seats: %+v", got)
	}
	// Planning while blocked records nothing.
	if plan, err := engine.PlanRunOnCall(context.Background(), runID); err != nil || len(plan.Gone)+len(plan.Answered)+len(plan.Deliveries) != 0 {
		t.Fatalf("blocked plan = %+v, %v", plan, err)
	}
	return engine, store, runID, executor
}

// crashedBeforeCancel returns a blocked run whose ledger ends in run_blocked and
// then its kept seat's cleanup, as a crash between that cleanup and the cancel
// or failure it precedes leaves it. It checks the cleanup changed nothing a
// reader of the block sees.
func crashedBeforeCancel(t *testing.T) (*RunEngine, *Store, string, RunEvent) {
	t.Helper()
	engine, store, runID, _ := blockedKeepingRun(t)
	before := mustEvents(t, store, runID)
	block := before[len(before)-1]
	if block.Type != RunEventBlocked {
		t.Fatalf("fixture ends in %s, want run_blocked", block.Type)
	}
	statusBefore, err := store.ProjectRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	asksBefore, err := ProjectSettledNeedsYouAsks(before)
	if err != nil || len(asksBefore) != 1 || asksBefore[0].Kind != NeedsYouKindBlocked || asksBefore[0].Seq != block.Seq {
		t.Fatalf("blocked asks = %+v, %v", asksBefore, err)
	}
	preservedBefore, err := engine.PreservePendingHumanGate(runID)
	if err != nil {
		t.Fatal(err)
	}

	if err := engine.EndKeptSeats(runID); err != nil {
		t.Fatal(err)
	}
	after := mustEvents(t, store, runID)
	if trail := seatTrail(t, store, runID); strings.Join(trail[len(trail)-2:], ", ") != "run_blocked, ended slot_work run_final" {
		t.Fatalf("crash ledger ends %v", trail)
	}
	statusAfter, err := store.ProjectRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if statusAfter.Status != RunStatusBlocked || statusAfter.ResumeAllowed != statusBefore.ResumeAllowed || statusAfter.Final || statusAfter.Epoch != statusBefore.Epoch {
		t.Fatalf("status after the cleanup %+v, before %+v", statusAfter, statusBefore)
	}
	if asksAfter, err := ProjectSettledNeedsYouAsks(after); err != nil || !reflect.DeepEqual(asksAfter, asksBefore) {
		t.Fatalf("asks after the cleanup %+v, before %+v (%v)", asksAfter, asksBefore, err)
	}
	if preserved, err := engine.PreservePendingHumanGate(runID); err != nil || preserved != preservedBefore || len(mustEvents(t, store, runID)) != len(after) {
		t.Fatalf("startup recovery after the cleanup = %v, %v; before %v", preserved, err, preservedBefore)
	}
	return engine, store, runID, block
}

func TestACrashBetweenAKeptSeatCleanupAndTheCancelLeavesTheRunBlocked(t *testing.T) {
	t.Run("only a cleanup, resume, cancel or failure may follow", func(t *testing.T) {
		_, store, runID, _ := crashedBeforeCancel(t)
		for _, event := range []RunEvent{
			{Type: RunEventNodeStarted, NodeID: "fmn_ship"},
			{Type: RunEventSeatCreated, NodeID: "fmn_ship", SlotID: "slot_ship"},
			{Type: RunEventHumanAskDelivered, NodeID: "fmn_work", SlotID: "slot_work"},
			{Type: RunEventHumanAskFallback, GateID: "gate_review"},
			{Type: RunEventError, Data: map[string]any{"code": "late"}},
			{Type: RunEventBlocked, Data: map[string]any{"resumeAllowed": true}},
			{Type: RunEventSucceeded, Data: map[string]any{"final": true}},
		} {
			if err := store.AppendRunEvent(runID, event); !errors.Is(err, ErrRunEpochBlocked) {
				t.Fatalf("%s after the block and cleanup: %v", event.Type, err)
			}
		}
		if err := store.AppendRunEvent(runID, RunEvent{Type: RunEventSeatCleanup, NodeID: "fmn_other", SlotID: "slot_other", Data: map[string]any{"outcome": SeatOutcomeEnded, "cause": SeatCauseRunFinal}}); err != nil {
			t.Fatalf("a second cleanup after the block: %v", err)
		}
		if status, err := store.ProjectRun(runID); err != nil || status.Status != RunStatusBlocked || !status.ResumeAllowed {
			t.Fatalf("status = %+v, %v", status, err)
		}
	})
	t.Run("resume", func(t *testing.T) {
		engine, store, runID, block := crashedBeforeCancel(t)
		status, err := engine.ResumeRun(runID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "retry ship after restart"})
		if err != nil || status.Status != RunStatusSucceeded {
			t.Fatalf("resume = %+v, %v", status, err)
		}
		for _, event := range mustEvents(t, store, runID) {
			if event.Type == RunEventResumed && event.Seq == block.Seq+2 {
				if event.Epoch != block.Epoch+1 || intFromRunEventData(event.Data["resumedFromSeq"]) != block.Seq {
					t.Fatalf("resume = epoch %d from %d, block epoch %d seq %d", event.Epoch, intFromRunEventData(event.Data["resumedFromSeq"]), block.Epoch, block.Seq)
				}
				return
			}
		}
		t.Fatalf("no resume right after the cleanup: %v", seatTrail(t, store, runID))
	})
	for _, final := range []string{RunEventCanceled, RunEventFailed} {
		t.Run(final, func(t *testing.T) {
			_, store, runID, _ := crashedBeforeCancel(t)
			if err := store.AppendRunEvent(runID, RunEvent{Type: final, Data: map[string]any{"final": true}}); err != nil {
				t.Fatal(err)
			}
			if trail := seatTrail(t, store, runID); strings.Join(trail[len(trail)-3:], ", ") != "run_blocked, ended slot_work run_final, "+final {
				t.Fatalf("trail = %v", trail)
			}
			// Nothing at all follows a final event, not even a cleanup.
			if err := store.AppendRunEvent(runID, RunEvent{Type: RunEventSeatCleanup, NodeID: "fmn_work", SlotID: "slot_work", Data: map[string]any{"outcome": SeatOutcomeEnded}}); !errors.Is(err, ErrRunFinal) {
				t.Fatalf("cleanup after %s: %v", final, err)
			}
			if _, err := store.ResumeRun(runID, RunResumeRequest{Actor: "agent:test"}); !errors.Is(err, ErrRunFinal) {
				t.Fatalf("resume after %s: %v", final, err)
			}
		})
	}
}

func TestABlockedRunRecordsASeatFoundGoneAfterItResumes(t *testing.T) {
	engine, store, runID, executor := blockedKeepingRun(t)
	executor.gone = map[string]bool{"$slot_work-1": true}
	if status, err := engine.ResumeRun(runID, RunResumeRequest{Actor: "agent:test", Mode: "reattach", Reason: "retry ship"}); err != nil || status.Status != RunStatusSucceeded {
		t.Fatalf("resume = %+v, %v", status, err)
	}
	trail := seatTrail(t, store, runID)
	joined := strings.Join(trail, ", ")
	if !strings.Contains(joined, "run_blocked, run_resumed, gone slot_work, created slot_ship") || !strings.HasSuffix(joined, "ended slot_ship, run_succeeded") {
		t.Fatalf("resume trail = %v", trail)
	}
	if len(executor.ended) != 0 {
		t.Fatalf("a gone seat was ended again: %v", executor.ended)
	}
}
