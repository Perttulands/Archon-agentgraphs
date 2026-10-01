package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/coordinator"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

const waitTestBoard = `schema = 1
id = "brd_proof"
slug = "proof"
title = "Proof"
rev = 1
[[inputCard]]
id = "mis_proof"
title = "Proof"
goal = "Prove the wait"
[[formation]]
id = "fmn_work"
type = "solo"
title = "Draft"
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Output"
[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
controller = true
effort = "medium"
[[gate]]
id = "gate_review"
title = "Review"
kinds = ["human"]
criterion = "Is the draft ready to ship?"
[[formation]]
id = "fmn_after"
type = "solo"
title = "Ship"
[[formation.input]]
id = "port_after_in"
label = "Input"
[[formation.output]]
id = "port_after_out"
label = "Output"
[[formation.slot]]
id = "slot_after"
label = "Worker"
agentId = "codex-builder"
harness = "openai-codex"
controller = true
effort = "medium"
[[connection]]
id = "edge_start"
from = "mis_proof:out"
to = "fmn_work:port_in"
[[connection]]
id = "edge_gate"
from = "fmn_work:port_out"
to = "gate_review:in"
[[connection]]
id = "edge_pass"
from = "gate_review:pass"
to = "fmn_after:port_after_in"
[[end]]
id = "end_done"
title = "Done"
outcome = "done"
[[end]]
id = "end_rejected"
title = "Rejected"
outcome = "rejected"
[[connection]]
id = "edge_fail"
from = "gate_review:fail"
to = "end_rejected:in"
[[connection]]
id = "edge_after_done"
from = "fmn_after:port_after_out"
to = "end_done:in"
`

// waitExecutor finishes each formation when the test lets it.
type waitExecutor struct{ proceed chan struct{} }

func (e *waitExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	<-e.proceed
	outputs := map[string]formations.FormationOutputPayload{}
	for _, port := range req.Formation.Outputs {
		outputs[port.ID] = formations.FormationOutputPayload{Text: "Draft v1: ship the wait command.\nIt prints one paragraph."}
	}
	return formations.FormationExecutionResult{Status: "done", Outputs: outputs}, nil
}

func startWaitDaemon(t *testing.T) (*httptest.Server, *waitExecutor, string) {
	t.Helper()
	root := t.TempDir()
	e := &waitExecutor{proceed: make(chan struct{})}
	c, err := coordinator.Open(root, formations.NewPersonaStore(filepath.Join(root, "agents")), func(*formations.Store) formations.FormationExecutor { return e })
	if err != nil {
		t.Fatal(err)
	}
	store := formations.NewStore(root)
	if err := os.MkdirAll(filepath.Dir(store.BoardPath("proof")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.BoardPath("proof"), []byte(waitTestBoard), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(c.Handler())
	t.Cleanup(func() { close(e.proceed); server.Close(); c.Close() })
	out, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "mission", "run", "proof", "--input", "mis_proof", "--brief", "prove it", "--cwd", root)
	if code != 0 {
		t.Fatalf("start %d %s %s", code, out, stderr)
	}
	var receipt struct {
		Data struct {
			RunID string `json:"runId"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &receipt); err != nil {
		t.Fatal(err)
	}
	return server, e, receipt.Data.RunID
}

var waitApprovePattern = regexp.MustCompile(`archon --server \S+ gate approve (\S+) (\S+) --requested-seq (\d+) --response 'your answer'`)

func TestRunWaitTellsTheDriverWhatTheGateAsksAndHowToAnswer(t *testing.T) {
	server, e, runID := startWaitDaemon(t)
	e.proceed <- struct{}{}
	out, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", runID, "--until", "needs-you", "--timeout", "10s")
	if code != waitExitNeedsYou {
		t.Fatalf("code %d stderr %s out %s", code, stderr, out)
	}
	for _, want := range []string{
		fmt.Sprintf("Run %s (mission \"Proof\") needs you.", runID),
		`The gate "Review" (gate_review) waits for a verdict`,
		"Criterion: Is the draft ready to ship?",
		`It received this from "Draft":`,
		"  | Draft v1: ship the wait command.\n  | It prints one paragraph.\n",
		`On pass: goes to "Ship".`,
		fmt.Sprintf("gate reject %s gate_review --requested-seq", runID),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	match := waitApprovePattern.FindStringSubmatch(out)
	if match == nil || match[1] != runID || match[2] != "gate_review" {
		t.Fatalf("no exact approve command in:\n%s", out)
	}
	seq, _ := strconv.Atoi(match[3])
	next := regexp.MustCompile(`Wait for what comes next: archon --server \S+ run wait \S+ --until needs-you --since (\d+)`).FindStringSubmatch(out)
	if next == nil {
		t.Fatalf("no next wait in:\n%s", out)
	}
	cursor, _ := strconv.Atoi(next[1])
	if cursor < seq {
		t.Fatalf("cursor %d before the ask %d", cursor, seq)
	}

	// The same answer as JSON, for machines.
	raw, _, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", runID, "--json")
	var answer waitOutput
	if err := json.Unmarshal([]byte(raw), &answer); err != nil || code != waitExitNeedsYou {
		t.Fatalf("code %d err %v json %s", code, err, raw)
	}
	if answer.Outcome != "needs-you" || answer.Seq != cursor || len(answer.Asks) != 1 || answer.Asks[0].Seq != seq || answer.Asks[0].Input == nil || !strings.HasPrefix(answer.Next, "archon --server ") || !strings.HasSuffix(answer.Next, " --json") {
		t.Fatalf("answer = %+v", answer)
	}

	// From the cursor nothing new happens before the timeout.
	out, _, code = runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", runID, "--since", strconv.Itoa(cursor), "--timeout", "1s")
	if code != waitExitTimeout || !strings.Contains(out, "happened before the timeout") || !strings.Contains(out, fmt.Sprintf("--since %d", cursor)) {
		t.Fatalf("timeout code %d:\n%s", code, out)
	}

	// Answering with the printed command lets the run finish.
	if out, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "gate", "approve", runID, "gate_review", "--requested-seq", match[3], "--response", "ship it"); code != 0 {
		t.Fatalf("approve %d %s %s", code, out, stderr)
	}
	go func() { e.proceed <- struct{}{} }()
	out, stderr, code = runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", runID, "--until", "final", "--since", strconv.Itoa(cursor), "--timeout", "10s")
	if code != waitExitFinal || !strings.Contains(out, fmt.Sprintf("Run %s (mission \"Proof\") succeeded at #", runID)) {
		t.Fatalf("final code %d stderr %s:\n%s", code, stderr, out)
	}
}

func TestRunWaitSaysWhyACanceledRunEnded(t *testing.T) {
	server, e, runID := startWaitDaemon(t)
	e.proceed <- struct{}{}
	if _, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", runID, "--timeout", "10s"); code != waitExitNeedsYou {
		t.Fatalf("code %d %s", code, stderr)
	}
	if out, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "abort", runID, "--reason", "wrong brief"); code != 0 {
		t.Fatalf("abort %d %s %s", code, out, stderr)
	}
	out, _, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", runID, "--until", "any-change")
	want := fmt.Sprintf("Run %s (mission \"Proof\") canceled at #", runID)
	if code != waitExitFinal || !strings.Contains(out, want) || !strings.Contains(out, `while at "Review": wrong brief. Ended by the operator (operator:archon).`) {
		t.Fatalf("code %d:\n%s", code, out)
	}
}

func TestRunWaitReportsChangesInOrder(t *testing.T) {
	server, e, runID := startWaitDaemon(t)
	out, _, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", runID, "--until", "any-change")
	if code != waitExitChanged || !strings.Contains(out, "#1 run_started") || !strings.Contains(out, "It is now running.") {
		t.Fatalf("code %d:\n%s", code, out)
	}
	e.proceed <- struct{}{}
}

// A daemon that restarts answers 503 or refuses connections for a while; the
// wait keeps its cursor and reconnects.
func TestRunWaitReconnectsAcrossARestart(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var sinces []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sinces = append(sinces, r.URL.Query().Get("since"))
		mu.Unlock()
		if calls.Add(1) < 3 {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"success":false,"error":{"message":"coordinator is stopping"}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"data":{"runId":"run_x","missionTitle":"Proof","until":"final","outcome":"final","since":7,"seq":9,"status":"failed","final":true,"settled":true,"asks":[],"changes":[],"end":{"status":"failed","seq":9,"code":"coordinator_execution_failed","reason":"completed recovery requires a single-slot formation","endedBy":"archond","stopped":[{"id":"fmn_exec","title":"Execution"}]}}}`)
	}))
	defer server.Close()
	out, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", "run_x", "--until", "final", "--since", "7")
	if code != waitExitFinal {
		t.Fatalf("code %d %s", code, stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sinces) != 3 {
		t.Fatalf("polls = %v", sinces)
	}
	for _, since := range sinces {
		if since != "7" {
			t.Fatalf("reconnect lost the cursor: %v", sinces)
		}
	}
	want := `Run run_x (mission "Proof") failed at #9 while at "Execution": completed recovery requires a single-slot formation (coordinator_execution_failed). Ended by Archon (archond).`
	if !strings.Contains(out, want) || strings.Contains(out, "Wait for what comes next") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestRunWaitGivesUpOnAnUnreachableDaemonWithAClearCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	start := time.Now()
	out, _, code := runArchon(t, &fakeTmux{}, "--server", url, "run", "wait", "run_x", "--since", "4", "--reconnect", "1s")
	if code != waitExitLost || time.Since(start) > 10*time.Second {
		t.Fatalf("code %d after %s:\n%s", code, time.Since(start), out)
	}
	if !strings.Contains(out, "lost contact with the Archon daemon at "+url) || !strings.Contains(out, "--since 4") {
		t.Fatalf("out:\n%s", out)
	}
	raw, _, code := runArchon(t, &fakeTmux{}, "--server", url, "run", "wait", "run_x", "--reconnect", "0s", "--json")
	var answer waitOutput
	if err := json.Unmarshal([]byte(raw), &answer); err != nil || code != waitExitLost || answer.Outcome != "daemon-lost" || answer.Error == "" {
		t.Fatalf("code %d json %s", code, raw)
	}
}

func TestRunWaitRefusesUnknownRunsAndBadFlags(t *testing.T) {
	server, e, _ := startWaitDaemon(t)
	e.proceed <- struct{}{}
	if _, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", "run_missing"); code != 1 || !strings.Contains(stderr, "HTTP 404") {
		t.Fatalf("missing run: %d %s", code, stderr)
	}
	for _, args := range [][]string{{"run", "wait"}, {"run", "wait", "run_x", "--until", "soon"}, {"run", "wait", "run_x", "--since", "-1"}} {
		if _, _, code := runArchon(t, &fakeTmux{}, append([]string{"--server", server.URL}, args...)...); code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
	}
	if _, stderr, code := runArchon(t, &fakeTmux{}, "run", "wait", "run_x"); code != 2 || !strings.Contains(stderr, "run wait needs --server") {
		t.Fatalf("offline: %d %s", code, stderr)
	}
}

func TestDescribeGateRouteSaysWhereEachVerdictLeads(t *testing.T) {
	for _, tt := range []struct {
		route formations.GateRoute
		want  string
	}{
		{formations.GateRoute{Verdict: "pass", Targets: []formations.GateRouteTarget{{NodeID: "end_done", Title: "Done", Kind: "end", Outcome: "done"}}, EndsRun: true}, "this path ends (done), and nothing else can run, so the run succeeds"},
		{formations.GateRoute{Verdict: "fail", Targets: []formations.GateRouteTarget{{NodeID: "end_rejected", Title: "Rejected", Kind: "end", Outcome: "rejected"}}, EndsRun: true, RunFails: true}, "this path ends (rejected), and nothing else can run, so the run fails"},
		{formations.GateRoute{Verdict: "pass", Targets: []formations.GateRouteTarget{{NodeID: "end_done", Title: "Done", Kind: "end", Outcome: "done"}}}, "this path ends (done)"},
		{formations.GateRoute{Verdict: "fail", Targets: []formations.GateRouteTarget{{NodeID: "end_rejected", Title: "Rejected", Kind: "end", Outcome: "rejected"}}, RunFails: true}, "this path ends (rejected), so the run fails once its other open work ends"},
		{formations.GateRoute{Verdict: "pass", Targets: []formations.GateRouteTarget{{NodeID: "fmn_ship", Title: "Ship"}, {NodeID: "end_done", Title: "Done", Kind: "end", Outcome: "done"}}}, `goes to "Ship"; this path ends (done)`},
		{formations.GateRoute{Verdict: "pass", Targets: []formations.GateRouteTarget{{NodeID: "fmn_ship", Title: "Ship"}}, MissionRounds: &formations.RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "inp", Used: 3, Max: 3}, Limit: &formations.RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "inp", Used: 3, Max: 3}}, `goes to "Ship", but the mission has used all 3 of its rounds, so the run blocks instead until you grant one more`},
		{formations.GateRoute{Verdict: "fail", Targets: []formations.GateRouteTarget{{NodeID: "fmn_build", Title: "Build", Rounds: &formations.RunLimitReached{Kind: "rounds", LimitID: "lim_build", NodeID: "fmn_build", Used: 2, Max: 2}}}, Limit: &formations.RunLimitReached{Kind: "rounds", LimitID: "lim_build", NodeID: "fmn_build", Used: 2, Max: 2}}, `goes to "Build", but "Build" has used all 2 of its rounds, so the run blocks instead until you grant one more`},
		{formations.GateRoute{Verdict: "fail", Targets: []formations.GateRouteTarget{{NodeID: "fmn_build", Title: "Build", Rounds: &formations.RunLimitReached{Kind: "rounds", LimitID: "lim_build", NodeID: "fmn_build", Used: 1, Max: 3}}}}, `goes to "Build" (round 2 of 3)`},
		{formations.GateRoute{Verdict: "pass", Targets: []formations.GateRouteTarget{{NodeID: "fmn_ship", Title: "Ship"}}, MissionRounds: &formations.RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "inp", Used: 1, Max: 1}, Limit: &formations.RunLimitReached{Kind: "rounds", LimitID: "lim_mission", NodeID: "inp", Used: 1, Max: 1}}, `goes to "Ship", but the mission has used its only round, so the run blocks instead until you grant one more`},
	} {
		if got := describeGateRoute(tt.route); got != tt.want {
			t.Errorf("got %q want %q", got, tt.want)
		}
	}
}

// Every poll of one wait reuses one connection to the daemon.
func TestRunWaitKeepsOneConnectionAcrossPolls(t *testing.T) {
	var polls atomic.Int32
	var conns atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outcome, final := "pending", "false"
		if polls.Add(1) >= 5 {
			outcome, final = "final", "true"
		}
		fmt.Fprintf(w, `{"success":true,"data":{"runId":"run_x","missionTitle":"Proof","until":"final","outcome":%q,"since":3,"seq":3,"status":"succeeded","final":%s,"settled":true,"asks":[],"changes":[]}}`, outcome, final)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	if _, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "run", "wait", "run_x", "--until", "final", "--since", "3"); code != waitExitFinal {
		t.Fatalf("code %d %s", code, stderr)
	}
	if polls.Load() != 5 || conns.Load() != 1 {
		t.Fatalf("%d polls opened %d connections", polls.Load(), conns.Load())
	}
}

// archon-o7p.8.1: a run stopped at a spent Limit card offers the grant.
func TestRunWaitOffersAGrantAtASpentLimit(t *testing.T) {
	ask := coordinator.WaitAsk{Kind: formations.NeedsYouKindBlocked, Seq: 9, New: true, Title: "Review", Reason: "Review used 3 of 3 rounds", Code: formations.RunBlockLimitReached, ResumeAllowed: true}
	out := renderWait("http://127.0.0.1:1", &waitOutput{RunWait: coordinator.RunWait{RunID: "run_x", Mission: "Proof", Since: 3, Seq: 9, Status: "blocked", Asks: []coordinator.WaitAsk{ask}}, Next: "archon next"}, waitExitNeedsYou)
	for _, want := range []string{
		`The run is blocked at "Review" since #9: Review used 3 of 3 rounds (limit_reached).`,
		"Give it one more round if the work deserves it, or stop it:\n  archon --server http://127.0.0.1:1 run resume run_x --grant --reason 'why one more'\n  archon --server http://127.0.0.1:1 run abort run_x --reason 'why'\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out lacks %q:\n%s", want, out)
		}
	}
}

func TestRunWaitWordsStatusesAndEscalationsPlainly(t *testing.T) {
	out := renderWait("http://127.0.0.1:1", &waitOutput{RunWait: coordinator.RunWait{RunID: "run_x", Mission: "Proof", Since: 3, Seq: 4, Status: "waiting_human", Asks: []coordinator.WaitAsk{{Kind: formations.NeedsYouKindEscalation, Seq: 4, New: true, Title: "Work", Severity: "stop", Reason: "credentials missing"}}}, Next: "archon next"}, waitExitNeedsYou)
	for _, raw := range []string{"waiting_human", "stop escalation", "blocking"} {
		if strings.Contains(out, raw) {
			t.Fatalf("raw %q in:\n%s", raw, out)
		}
	}
	if !strings.Contains(out, `"Work" escalated at #4 and the run stopped for it: credentials missing`) {
		t.Fatalf("out:\n%s", out)
	}
	changed := renderWait("s", &waitOutput{RunWait: coordinator.RunWait{RunID: "run_x", Mission: "Proof", Since: 3, Seq: 4, Status: "waiting_human", Changes: []coordinator.WaitChange{{Seq: 4, Type: "node_output"}}}, Next: "n"}, waitExitChanged)
	if !strings.Contains(changed, "It is now waiting for a human verdict.") {
		t.Fatalf("changed:\n%s", changed)
	}
	// A seat's recorded wait reads as what it waits on.
	stalled := describeWaitChange(coordinator.WaitChange{Seq: 5, Type: formations.RunEventSeatState, Title: "Work", SlotID: "slot_work", State: formations.SeatStateNotReady, Detail: "the seat has not reached its ready prompt"})
	if stalled != `seat_state "Work" slot slot_work seat_not_ready: the seat has not reached its ready prompt` {
		t.Fatalf("seat_state change = %q", stalled)
	}
}
