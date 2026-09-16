package coordinator

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

type recordingNotifier struct {
	mu    sync.Mutex
	sent  []formations.NeedsYouNotification
	fails int
	tries int
	block bool
}

func (n *recordingNotifier) NotifyNeedsYou(ctx context.Context, notification formations.NeedsYouNotification) error {
	n.mu.Lock()
	n.tries++
	block := n.block
	if n.fails > 0 {
		n.fails--
		n.mu.Unlock()
		return errors.New("channel down")
	}
	n.mu.Unlock()
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, notification)
	return nil
}

func (n *recordingNotifier) snapshot() ([]formations.NeedsYouNotification, int) {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]formations.NeedsYouNotification(nil), n.sent...), n.tries
}

func awaitNotifications(t *testing.T, n *recordingNotifier, want int) []formations.NeedsYouNotification {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sent, _ := n.snapshot()
		if len(sent) >= want {
			return sent
		}
		if time.Now().After(deadline) {
			t.Fatalf("notifications = %+v, want %d", sent, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// settleQuietly lets the dispatcher finish any queued work before a count check.
func settleQuietly() { time.Sleep(300 * time.Millisecond) }

func reopen(t *testing.T, c *Coordinator, root string, executor formations.FormationExecutor) *Coordinator {
	t.Helper()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := Open(root, c.personas, func(*formations.Store) formations.FormationExecutor { return executor })
	if err != nil {
		t.Fatal(err)
	}
	if err := next.RecoverInterruptedRuns(); err != nil {
		t.Fatal(err)
	}
	return next
}

func TestNeedsYouMailsSettledAsksOnceAndSkipsTransientBlocks(t *testing.T) {
	c, executor, root := fixture(t)
	notifier := &recordingNotifier{}
	c.EnableNeedsYou(NeedsYouConfig{Notifier: notifier, CockpitURL: "https://cockpit.example/", ServerURL: "http://127.0.0.1:8091", RetryInterval: time.Hour})
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	seq := awaitState(t, c, id, "waiting_human").WaitingGates[0].RequestedSeq

	gate := awaitNotifications(t, notifier, 1)[0]
	if gate.Kind != formations.NeedsYouKindHumanGate || gate.Seq != seq || gate.RunStatus != "waiting_human" || gate.GateTitle != "Review" || gate.BoardTitle != "Proof" {
		t.Fatalf("gate notification = %+v", gate)
	}
	if gate.Subject != "Archon needs your answer: Review (Proof)" || gate.BoardURL != "https://cockpit.example/?board=proof&run="+id {
		t.Fatalf("subject %q link %q", gate.Subject, gate.BoardURL)
	}
	for _, want := range []string{
		"Gate criterion:\nPRIVATE-CRITERION",
		"The gate received this from Work:\n\nPRIVATE-OUTPUT\n",
		"Answer in the cockpit: https://cockpit.example/?board=proof&run=" + id,
		"archon --server http://127.0.0.1:8091 gate approve " + id + " gate_review --requested-seq " + strconv.Itoa(seq) + " --response 'your answer'",
		"archon --server http://127.0.0.1:8091 gate reject " + id + " gate_review --requested-seq " + strconv.Itoa(seq) + " --response 'what to change'",
	} {
		if !strings.Contains(gate.Body, want) {
			t.Fatalf("body lacks %q:\n%s", want, gate.Body)
		}
	}
	for _, private := range []string{"/private/", "PRIVATE-CAPTURE"} {
		if strings.Contains(gate.Body, private) {
			t.Fatalf("body leaked %q:\n%s", private, gate.Body)
		}
	}

	// The verdict appends a resume-required block and the coordinator resumes
	// at once. That block settles as success, so it must never be announced.
	if w := post(t, c, "/api/formations/runs/"+id+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"pass","reason":"use Postgres"}`); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "succeeded")
	sent := awaitNotifications(t, notifier, 2)
	settleQuietly()
	sent, _ = notifier.snapshot()
	events, err := c.store.ReadRunEvents(id)
	if err != nil {
		t.Fatal(err)
	}
	transient := false
	for _, event := range events {
		transient = transient || event.Type == formations.RunEventBlocked
	}
	final := sent[1]
	if !transient || len(sent) != 2 || final.Kind != formations.NeedsYouKindFinal || final.Seq != len(events) || final.Subject != "Archon run succeeded: Proof" {
		t.Fatalf("transient block %v, notifications %+v", transient, sent)
	}

	// Reopening the same state directory sends nothing again.
	c = reopen(t, c, root, executor)
	defer c.Close()
	again := &recordingNotifier{}
	c.EnableNeedsYou(NeedsYouConfig{Notifier: again, RetryInterval: 20 * time.Millisecond})
	settleQuietly()
	if resent, tries := again.snapshot(); len(resent) != 0 || tries != 0 {
		t.Fatalf("restart resent %+v (%d tries)", resent, tries)
	}
}

func TestNeedsYouStartupSkipsOldFinalRunsAndRetriesFailedSends(t *testing.T) {
	c, executor, root := fixture(t)
	finished := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	seq := awaitState(t, c, finished, "waiting_human").WaitingGates[0].RequestedSeq
	if w := post(t, c, "/api/formations/runs/"+finished+"/gates/gate_review/verdict", `{"requestedSeq":`+strconv.Itoa(seq)+`,"verdict":"pass"}`); w.Code != 202 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, finished, "succeeded")
	waiting := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	waitingSeq := awaitState(t, c, waiting, "waiting_human").WaitingGates[0].RequestedSeq

	// Notifications are first enabled after a restart, and the first send fails.
	c = reopen(t, c, root, executor)
	notifier := &recordingNotifier{fails: 1}
	c.EnableNeedsYou(NeedsYouConfig{Notifier: notifier, RetryInterval: 50 * time.Millisecond})
	sent := awaitNotifications(t, notifier, 1)
	settleQuietly()
	sent, tries := notifier.snapshot()
	if len(sent) != 1 || tries != 2 || sent[0].RunID != waiting || sent[0].Seq != waitingSeq || sent[0].Kind != formations.NeedsYouKindHumanGate {
		t.Fatalf("notifications %+v after %d tries, want one retried gate ask for %s", sent, tries, waiting)
	}
	if seqs, err := c.store.NeedsYouNotifiedSeqs(finished); err != nil || len(seqs) != 0 {
		t.Fatalf("old final run marked %v, %v", seqs, err)
	}
	if seqs, err := c.store.NeedsYouNotifiedSeqs(waiting); err != nil || !seqs[waitingSeq] || len(seqs) != 1 {
		t.Fatalf("waiting run marked %v, %v", seqs, err)
	}

	c = reopen(t, c, root, executor)
	defer c.Close()
	again := &recordingNotifier{}
	c.EnableNeedsYou(NeedsYouConfig{Notifier: again, RetryInterval: 20 * time.Millisecond})
	settleQuietly()
	if resent, tries := again.snapshot(); len(resent) != 0 || tries != 0 {
		t.Fatalf("restart resent %+v (%d tries)", resent, tries)
	}
}

type seatLostExecutor struct{}

func (seatLostExecutor) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	return formations.FormationExecutionResult{}, &formations.RunExecutionError{Code: "native_turn_failed", Message: "seat died", Boundary: "executor", NodeID: req.NodeID}
}

func TestNeedsYouMailsASettledBlockOnce(t *testing.T) {
	root := t.TempDir()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	c, err := Open(root, personas, func(*formations.Store) formations.FormationExecutor { return seatLostExecutor{} })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.store.BoardPath("proof"), []byte(testBoard), 0600); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	c.EnableNeedsYou(NeedsYouConfig{Notifier: notifier, ServerURL: "http://127.0.0.1:8091", RetryInterval: 20 * time.Millisecond})
	id := startRun(t, c)
	blocked := awaitState(t, c, id, "blocked")
	sent := awaitNotifications(t, notifier, 1)
	settleQuietly()
	sent, _ = notifier.snapshot()
	if len(sent) != 1 || sent[0].Kind != formations.NeedsYouKindBlocked || sent[0].Seq != blocked.EventCount || sent[0].Subject != "Archon run blocked: Proof" {
		t.Fatalf("notifications = %+v", sent)
	}
	for _, want := range []string{"Reason: seat died", "archon --server http://127.0.0.1:8091 run resume " + id} {
		if !strings.Contains(sent[0].Body, want) {
			t.Fatalf("body lacks %q:\n%s", want, sent[0].Body)
		}
	}
}

func TestNeedsYouSendNeverHoldsShutdown(t *testing.T) {
	c, executor, _ := fixture(t)
	notifier := &recordingNotifier{block: true}
	c.EnableNeedsYou(NeedsYouConfig{Notifier: notifier, RetryInterval: time.Hour})
	id := startRun(t, c)
	<-executor.entered
	executor.proceed <- struct{}{}
	awaitState(t, c, id, "waiting_human")
	deadline := time.Now().Add(5 * time.Second)
	for _, tries := notifier.snapshot(); tries == 0; _, tries = notifier.snapshot() {
		if time.Now().After(deadline) {
			t.Fatal("send never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	started := time.Now()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("shutdown waited %s on a hanging send", elapsed)
	}
	if seqs, err := c.store.NeedsYouNotifiedSeqs(id); err != nil || len(seqs) != 0 {
		t.Fatalf("unsent ask marked %v, %v", seqs, err)
	}
}

func TestRenderNeedsYouCapsInputAndUsesPlaceholders(t *testing.T) {
	long := strings.Repeat("q", pendingGateInputMaxBytes+10)
	events := []formations.RunEvent{
		{Seq: 1, RunID: "run_x", Type: formations.RunEventStarted, Data: map[string]any{"boardSlug": "proof"}},
		{Seq: 2, RunID: "run_x", Type: formations.RunEventHumanInputRequested, GateID: "gate_review", Data: map[string]any{"prompt": "Answer", "inputRef": map[string]any{"fromNodeId": "fmn_work", "text": long}}},
	}
	n := renderNeedsYou(NeedsYouConfig{}, formations.NeedsYouAsk{RunID: "run_x", Seq: 2, Kind: formations.NeedsYouKindHumanGate, GateID: "gate_review"}, events, "waiting_human", nil)
	if n.BoardURL != "" || strings.Contains(n.Body, "cockpit:") || !strings.Contains(n.Body, `archon --server "$FORM_SERVER" gate approve run_x gate_review --requested-seq 2`) {
		t.Fatalf("placeholders:\n%s", n.Body)
	}
	if strings.Contains(n.Body, long) || !strings.Contains(n.Body, strings.Repeat("q", pendingGateInputMaxBytes)+"\n\n[The input is longer; this is its first 64 KiB.]") {
		t.Fatal("input was not capped with a marker")
	}
	for kind, want := range map[string]string{
		formations.NeedsYouKindEscalation: "Archon needs you: fmn_work escalated (proof)",
		formations.NeedsYouKindFinal:      "Archon run failed: proof",
	} {
		ask := formations.NeedsYouAsk{RunID: "run_x", Seq: 2, Kind: kind, NodeID: "fmn_work", Ask: "stopped", Severity: "stop", Status: formations.RunStatusFailed}
		if got := renderNeedsYou(NeedsYouConfig{}, ask, events, "failed", nil); got.Subject != want || got.Text != want || !strings.Contains(got.Body, "stopped") {
			t.Fatalf("%s: %+v", kind, got)
		}
	}
	raw, err := json.Marshal(n)
	if err != nil || !strings.Contains(string(raw), `"subject":`) || !strings.Contains(string(raw), `"body":`) {
		t.Fatalf("notification JSON %s, %v", raw, err)
	}
}

func TestCommandNotifierWritesJSONAndReportsFailure(t *testing.T) {
	dir := t.TempDir()
	script := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	out := filepath.Join(dir, "received.json")
	notification := formations.NeedsYouNotification{RunID: "run_x", Seq: 4, Kind: "human_gate", Subject: "Archon needs your answer", Body: "line one\nline two"}
	ok := CommandNotifier{Path: script("ok", "cat > '"+out+"'\necho delivered >&2\n")}
	if err := ok.NotifyNeedsYou(context.Background(), notification); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var received formations.NeedsYouNotification
	if err := json.Unmarshal(raw, &received); err != nil || received != notification {
		t.Fatalf("received %+v, %v", received, err)
	}
	failing := CommandNotifier{Path: script("fail", "echo boom >&2\nexit 3\n")}
	if err := failing.NotifyNeedsYou(context.Background(), notification); err == nil || !strings.Contains(err.Error(), "exit status 3") {
		t.Fatalf("failing command error = %v", err)
	}
	hanging := CommandNotifier{Path: script("hang", "sleep 30 &\nwait\n"), Timeout: 200 * time.Millisecond}
	started := time.Now()
	if err := hanging.NotifyNeedsYou(context.Background(), notification); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("hanging command error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}
