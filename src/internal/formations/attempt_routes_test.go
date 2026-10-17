package formations

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Attempt identity on the ledger (archon-5tg1): node_output names its
// attempt and every delivery names the output it carries, so mission outputs
// and the attempts view follow the routed payload rather than guess.

func numbered(events ...RunEvent) []RunEvent {
	for i := range events {
		events[i].Seq = i + 1
	}
	return events
}

func originData(nodeID, portID string, attempt int) map[string]any {
	return map[string]any{"nodeId": nodeID, "portId": portID, "attempt": attempt}
}

// carried is a delivery from a node's port carrying the given output.
func carried(fromNodeID, fromPortID string, origin map[string]any) map[string]any {
	return map[string]any{"fromNodeId": fromNodeID, "fromPortId": fromPortID, "origin": origin}
}

func outputData(portID, text string) map[string]any {
	return map[string]any{"status": "done", "text": text, "outputs": map[string]any{portID: map[string]any{"text": text}}}
}

func parsedBoard(t *testing.T, fixture string) *BoardDocument {
	t.Helper()
	board, err := parseBoard([]byte(fixture))
	if err != nil {
		t.Fatalf("parse board: %v", err)
	}
	return board
}

// A pass that routes an earlier attempt's output to the done End, as best of
// N does when it picks that attempt after later ones were judged, makes that
// attempt the mission's output; a pass of the latest makes the latest. The
// sent-back attempts reached nothing through the fail route.
func TestMissionOutputComesFromTheAttemptRoutedToTheEnd(t *testing.T) {
	board := parsedBoard(t, strings.Replace(draftLoopBoardFixture(), `kind = "file", from`, `kind = "text", from`, 1))
	board.Formations[0].Outputs[0].Kind = PortKindText
	for _, picked := range []int{1, 2} {
		draft := func(attempt int) map[string]any {
			return carried("fmn_work", "port_work_out", originData("fmn_work", "port_work_out", attempt))
		}
		events := numbered(
			RunEvent{Type: RunEventStarted},
			RunEvent{Type: RunEventNodeStarted, NodeID: "mis_showcase"},
			RunEvent{Type: RunEventNodeOutput, NodeID: "mis_showcase", Data: outputData("out", "brief")},
			RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: 1, Data: map[string]any{"inputRefs": []any{carried("mis_showcase", "out", originData("mis_showcase", "out", 0))}}},
			RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_work", Attempt: 1, Data: outputData("port_work_out", "draft 1")},
			RunEvent{Type: RunEventGateEvaluating, GateID: "gate_lint", Data: map[string]any{"inputRef": draft(1)}},
			RunEvent{Type: RunEventGateVerdict, GateID: "gate_lint", Data: map[string]any{"verdict": "fail", "routePort": "fail", "inputRef": draft(1)}},
			RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: 2, Data: map[string]any{"inputRefs": []any{carried("gate_lint", "fail", originData("fmn_work", "port_work_out", 1))}}},
			RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_work", Attempt: 2, Data: outputData("port_work_out", "draft 2")},
			RunEvent{Type: RunEventGateEvaluating, GateID: "gate_lint", Data: map[string]any{"inputRef": draft(2)}},
			RunEvent{Type: RunEventGateVerdict, GateID: "gate_lint", Data: map[string]any{"verdict": "pass", "routePort": "pass", "inputRef": draft(picked)}},
		)
		outputs, _, missing := resolveMissionOutputs(board, events)
		want := []RunOutput{{Name: "report", Kind: PortKindText, Value: fmt.Sprintf("draft %d", picked), NodeID: "fmn_work", PortID: "port_work_out", Attempt: picked}}
		if missing != "" || !reflect.DeepEqual(outputs, want) {
			t.Fatalf("picked %d: outputs = %+v, %q; want %+v", picked, outputs, missing, want)
		}
	}
}

func attemptVerdicts(evidence *RunAttemptsEvidence, nodeID string) map[int][]string {
	verdicts := map[int][]string{}
	for _, step := range evidence.Steps {
		if step.NodeID != nodeID {
			continue
		}
		for _, attempt := range step.Attempts {
			verdicts[attempt.Attempt] = []string{}
			for _, verdict := range attempt.Verdicts {
				state := verdict.Verdict
				switch {
				case verdict.Superseded:
					state = "superseded"
				case verdict.Pending:
					state = "pending"
				}
				if verdict.DecidedBy != "" {
					state += " by " + verdict.DecidedBy
				}
				verdicts[attempt.Attempt] = append(verdicts[attempt.Attempt], verdict.GateID+":"+state)
			}
		}
	}
	return verdicts
}

// A verdict joins the attempt whose output the gate judged, as the delivery
// names it: through another gate's fail route, and through a human gate's
// pass. A human request a later evaluation replaced is superseded, not
// pending; a verdict recorded before the gate evaluated again still answers
// its own request.
func TestAttemptVerdictsJoinTheOutputTheGateJudged(t *testing.T) {
	work := func(attempt int) map[string]any {
		return carried("fmn_work", "port_out", originData("fmn_work", "port_out", attempt))
	}
	started := func(attempt int) RunEvent {
		return RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: attempt}
	}
	output := func(attempt int) RunEvent {
		return RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_work", Attempt: attempt, Data: outputData("port_out", fmt.Sprintf("draft %d", attempt))}
	}
	evaluating := func(gateID string, input map[string]any) RunEvent {
		return RunEvent{Type: RunEventGateEvaluating, GateID: gateID, Data: map[string]any{"inputRef": input}}
	}
	verdict := func(gateID, verdict string, requestedSeq int) RunEvent {
		return RunEvent{Type: RunEventGateVerdict, GateID: gateID, Data: map[string]any{"verdict": verdict, "routePort": verdict, "requestedSeq": requestedSeq}}
	}
	recorded := func(gateID string, requestedSeq int) RunEvent {
		return RunEvent{Type: RunEventHumanVerdictRecorded, GateID: gateID, Data: map[string]any{"verdict": "pass", "requestedSeq": requestedSeq, "decidedBy": "operator"}}
	}
	asked := func(gateID string) RunEvent { return RunEvent{Type: RunEventHumanInputRequested, GateID: gateID} }
	approved := work(3)
	approved["response"] = map[string]any{"gateId": "gate_h", "text": "ship it"}
	events := numbered(
		RunEvent{Type: RunEventStarted}, // 1
		started(1), output(1),           // 2, 3
		evaluating("gate_a", work(1)), verdict("gate_a", "fail", 0), // 4, 5
		// Gate B takes A's fail route: it judges attempt 1 again.
		evaluating("gate_b", carried("gate_a", "fail", originData("fmn_work", "port_out", 1))), verdict("gate_b", "pass", 0), // 6, 7
		started(2), output(2), // 8, 9
		evaluating("gate_h", work(2)), asked("gate_h"), // 10, 11
		started(3), output(3), // 12, 13
		// Attempt 3 reaches the human gate before anyone answered: request 11 is replaced.
		evaluating("gate_h", work(3)), asked("gate_h"), recorded("gate_h", 15), // 14, 15, 16
		started(4), output(4), // 17, 18
		// Attempt 4 reaches it after the verdict was recorded, before it was routed.
		evaluating("gate_h", work(4)), asked("gate_h"), // 19, 20
		verdict("gate_h", "pass", 15), // 21
		// The approved attempt 3 goes on with the operator's response.
		evaluating("gate_c", approved), verdict("gate_c", "fail", 0), // 22, 23
	)
	board := &BoardDocument{Formations: []FormationNode{{ID: "fmn_work", Outputs: []FormationPort{{ID: "port_out"}}}}}
	got := attemptVerdicts(projectRunAttempts("run_x", "", board, events, &RunStatusProjection{}, nil), "fmn_work")
	want := map[int][]string{
		1: {"gate_a:fail", "gate_b:pass"},
		2: {"gate_h:superseded"},
		3: {"gate_h:pass by operator", "gate_c:fail"},
		4: {"gate_h:pending"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("verdicts = %v, want %v", got, want)
	}
}

// A judge's gates come from the run's board, so they are known whether the
// gate was evaluated once or again, and when the attempts view is asked for
// the judge alone.
func TestAttemptsNameTheGatesAJudgeJudges(t *testing.T) {
	for _, loop := range []bool{false, true} {
		t.Run(fmt.Sprintf("send back %v", loop), func(t *testing.T) {
			store, personas := s4RunFixture(t)
			createS4Persona(t, personas, "scout")
			fixture := strings.Replace(s4JudgeChainRunBoardFixture(), `kinds = ["code", "formation"]`, `kinds = ["formation"]`, 1)
			fixture = strings.Replace(fixture, "id = \"edge_gate_fail_rejected\"\nfrom = \"gate_review:fail\"\nto = \"end_rejected:in\"", "id = \"edge_gate_fail_work\"\nfrom = \"gate_review:fail\"\nto = \"fmn_work:port_work_in\"", 1)
			writeFixture(t, store.BoardPath("session-search"), fixture)
			board, err := store.ReadBoard("session-search")
			if err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			verdicts := map[int]string{1: judgeBlock("pass", "fine")}
			if loop {
				verdicts = map[int]string{1: judgeBlock("fail", "redo"), 2: judgeBlock("pass", "fine")}
			}
			executor := &scriptedLabExecutor{lab: NewLabFormationExecutor(store, personas, LabExecutorConfig{Cwd: cwd, Harnesses: []string{"openai-codex"}}), responses: map[string]map[int]string{
				"fmn_work": {1: "draft one", 2: "draft two"},
				"fmn_j1":   {1: "first review", 2: "second review"},
				"fmn_j2":   verdicts,
				"fmn_ship": {1: "shipped"},
			}}
			engine := NewRunEngine(store, personas, executor)
			status, err := engine.RunMission("session-search", RunStartRequest{Cwd: cwd, MissionID: "mis_showcase", ExpectedBoardETag: board.ETag, ExpectedBoardRev: board.Rev})
			if err != nil || status.Status != RunStatusSucceeded {
				t.Fatalf("status = %+v, %v", status, err)
			}
			evidence, err := store.ProjectRunAttempts(status.RunID, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, step := range evidence.Steps {
				want := []string(nil)
				if step.NodeID == "fmn_j1" || step.NodeID == "fmn_j2" {
					want = []string{"gate_review"}
				}
				if !reflect.DeepEqual(step.JudgeOf, want) {
					t.Errorf("%s judges %v, want %v", step.NodeID, step.JudgeOf, want)
				}
			}
			wantWork := map[int][]string{1: {"gate_review:pass"}}
			if loop {
				wantWork = map[int][]string{1: {"gate_review:fail"}, 2: {"gate_review:pass"}}
			}
			if got := attemptVerdicts(evidence, "fmn_work"); !reflect.DeepEqual(got, wantWork) {
				t.Errorf("work verdicts = %v, want %v", got, wantWork)
			}
			only, err := store.ProjectRunAttempts(status.RunID, "fmn_j1")
			if err != nil || len(only.Steps) != 1 || !reflect.DeepEqual(only.Steps[0].JudgeOf, []string{"gate_review"}) {
				t.Fatalf("judge alone = %+v, %v", only, err)
			}
		})
	}
}

// A resume carries on only the attempt it reattaches; a stopped attempt the
// resume runs again stays stopped.
func TestResumeRelabelsOnlyTheReattachedAttempt(t *testing.T) {
	board := &BoardDocument{Formations: []FormationNode{{ID: "fmn_work"}, {ID: "fmn_other"}}}
	for _, tc := range []struct {
		name string
		data map[string]any
		want map[string]string
	}{
		{"reattach", map[string]any{"resumeMode": "reattach", "openDispatches": []any{map[string]any{"dispatchId": "dsp_1", "nodeId": "fmn_work"}}}, map[string]string{"fmn_work": AttemptRunning, "fmn_other": AttemptStopped}},
		{"nothing open", map[string]any{"resumeMode": "reattach"}, map[string]string{"fmn_work": AttemptStopped, "fmn_other": AttemptStopped}},
		{"redispatch", map[string]any{"resumeMode": "redispatch", "openDispatches": []any{map[string]any{"dispatchId": "dsp_1", "nodeId": "fmn_work"}}}, map[string]string{"fmn_work": AttemptStopped, "fmn_other": AttemptStopped}},
	} {
		events := numbered(
			RunEvent{Type: RunEventStarted},
			RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_other", Attempt: 1},
			RunEvent{Type: RunEventBlocked, NodeID: "fmn_other", Data: map[string]any{"blockedNodeId": "fmn_other"}},
			RunEvent{Type: RunEventResumed, Data: map[string]any{"resumeMode": "redispatch"}},
			RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: 1},
			RunEvent{Type: RunEventBlocked, NodeID: "fmn_work", Data: map[string]any{"blockedNodeId": "fmn_work"}},
			RunEvent{Type: RunEventResumed, Data: tc.data},
		)
		got := map[string]string{}
		for _, step := range projectRunAttempts("run_x", "", board, events, &RunStatusProjection{}, nil).Steps {
			got[step.NodeID] = step.Attempts[0].Status
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: statuses = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The text budget goes to the newest attempts first: a long run's latest
// outputs are whole and its oldest give way.
func TestAttemptsBudgetKeepsTheNewestAttempts(t *testing.T) {
	big := strings.Repeat("x", EvidenceTextMaxBytes)
	events := []RunEvent{{Type: RunEventStarted}}
	attempts := EvidenceNodeBudgetBytes/EvidenceTextMaxBytes + 4
	for attempt := 1; attempt <= attempts; attempt++ {
		events = append(events,
			RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: attempt},
			RunEvent{Type: RunEventNodeOutput, NodeID: "fmn_work", Attempt: attempt, Data: outputData("port_out", big)})
	}
	board := &BoardDocument{Formations: []FormationNode{{ID: "fmn_work", Outputs: []FormationPort{{ID: "port_out"}}}}}
	steps := projectRunAttempts("run_x", "", board, numbered(events...), &RunStatusProjection{}, nil).Steps
	all := steps[0].Attempts
	newest, oldest := all[len(all)-1].Outputs[0].Text, all[0].Outputs[0].Text
	if newest.Text != big || newest.Truncated || oldest.Text != "" || !oldest.Truncated || oldest.Bytes != len(big) {
		t.Fatalf("newest = %d bytes (truncated %v), oldest = %d bytes (truncated %v)", len(newest.Text), newest.Truncated, len(oldest.Text), oldest.Truncated)
	}
}

func TestStoppedAttemptKeepsCauseAndReattachClearsIt(t *testing.T) {
	board := &BoardDocument{Formations: []FormationNode{{ID: "fmn_work"}}}
	events := numbered(RunEvent{Type: RunEventStarted}, RunEvent{Type: RunEventNodeStarted, NodeID: "fmn_work", Attempt: 1}, RunEvent{Type: RunEventError, NodeID: "fmn_work", Data: map[string]any{"message": "harness disconnected"}}, RunEvent{Type: RunEventBlocked, NodeID: "fmn_work", Data: map[string]any{"reason": "executor_failed"}})
	attempt := projectRunAttempts("run_x", "", board, events, &RunStatusProjection{}, nil).Steps[0].Attempts[0]
	if attempt.Status != AttemptStopped || attempt.StopReason == nil || attempt.StopReason.Text != "harness disconnected" {
		t.Fatalf("stopped = %+v", attempt)
	}
	events = append(events, RunEvent{Type: RunEventResumed, Data: map[string]any{"resumeMode": "reattach", "openDispatches": []any{map[string]any{"dispatchId": "dsp_1", "nodeId": "fmn_work"}}}})
	attempt = projectRunAttempts("run_x", "", board, numbered(events...), &RunStatusProjection{}, nil).Steps[0].Attempts[0]
	if attempt.Status != AttemptRunning || attempt.StopReason != nil {
		t.Fatalf("reattached = %+v", attempt)
	}
}
