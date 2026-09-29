package main

import (
	"encoding/json"
	"fmt"
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
[[mission]]
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
`

// waitExecutor finishes each formation when the test lets it.
type waitExecutor struct{ proceed chan struct{} }

func (e *waitExecutor) DefaultFormationTimeoutSeconds() int { return 0 }

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
	out, stderr, code := runArchon(t, &fakeTmux{}, "--server", server.URL, "mission", "run", "proof", "--mission", "mis_proof", "--brief", "prove it", "--cwd", root)
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
	if answer.Outcome != "needs-you" || answer.Seq != cursor || len(answer.Asks) != 1 || answer.Asks[0].Seq != seq || answer.Asks[0].Input == nil || !strings.HasPrefix(answer.Next, "archon --server ") {
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
		fmt.Fprint(w, `{"success":true,"data":{"runId":"run_x","mission":"Proof","until":"final","outcome":"final","since":7,"seq":9,"status":"failed","final":true,"settled":true,"asks":[],"changes":[],"end":{"status":"failed","seq":9,"code":"coordinator_execution_failed","reason":"completed recovery requires a single-slot formation","endedBy":"archond","stopped":[{"id":"fmn_exec","title":"Execution"}]}}}`)
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
	if !strings.Contains(out, want) || !strings.Contains(out, "--until final --since 9") {
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
