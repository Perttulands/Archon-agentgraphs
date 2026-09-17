package formations

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestMaxDispatchIncludesJudges(t *testing.T) {
	for limit := 1; limit <= 4; limit++ {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			fixture := strings.Replace(s4JudgeChainRunBoardFixture(), `kinds = ["code", "formation"]`, `kinds = ["formation"]`, 1)
			writeFixture(t, store.BoardPath("session-search"), fixture)
			executor := &fakeRunExecutor{outputs: map[string]string{"fmn_j2": judgeBlock("pass", "review passed")}}
			engine := NewRunEngine(store, personas, executor)
			status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", Limits: RunLimits{MaxDispatch: limit, MaxAttempts: 3}})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"fmn_work", "fmn_j1", "fmn_j2", "fmn_ship"}[:limit]
			if got := executor.nodeIDs(); !reflect.DeepEqual(got, want) {
				t.Fatalf("executions = %v, want %v", got, want)
			}
			wantStatus := RunStatusBlocked
			if limit == 4 {
				wantStatus = RunStatusSucceeded
			}
			if status.Status != wantStatus {
				t.Fatalf("status = %+v", status)
			}
			if limit < 4 {
				events, err := store.ReadRunEvents(status.RunID)
				if err != nil {
					t.Fatal(err)
				}
				if got := lastEventOfType(t, events, RunEventError).Data["code"]; got != "max_dispatch_exceeded" {
					t.Fatalf("error = %v", got)
				}
			}
		})
	}
}

func TestMaxDispatchSurvivesHumanResume(t *testing.T) {
	store, personas := s4RunFixture(t)
	createS4Persona(t, personas, "scout")
	writeFixture(t, store.BoardPath("session-search"), s5HumanGateBoardFixture())
	board, err := store.ReadBoard("session-search")
	if err != nil {
		t.Fatal(err)
	}
	executor := &fakeRunExecutor{}
	engine := NewRunEngine(store, personas, executor)
	status, err := engine.RunMission("session-search", RunStartRequest{MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev, Limits: RunLimits{MaxDispatch: 1, MaxAttempts: 3}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.RecordHumanGateVerdict(status.RunID, HumanGateVerdictRequest{GateID: "gate_review", Verdict: "pass", Reason: "approved", Actor: "human:operator"})
	if err != nil {
		t.Fatal(err)
	}
	status, err = NewRunEngine(store, personas, executor).ResumeRun(status.RunID, RunResumeRequest{Mode: "reattach", Reason: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("maxDispatch=1; executed=%v; status=%s", executor.nodeIDs(), status.Status)
	if status.Status != RunStatusBlocked || len(executor.calls) != 1 {
		t.Fatalf("dispatch limit reset after human verdict: %v", executor.nodeIDs())
	}
}
