package coordinator

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// A restart at any point reproduces the same outcome (archon-n7u.53,
// archon-o7p.11). Each case runs a mission to its end, driving it as an
// operator would: answering each gate by a script, resuming each resumable
// block. Then, for every prefix of that ledger, it opens a new daemon on a
// state directory holding only the prefix, as a crash right after that event
// leaves it, recovers, drives the run to its end the same way and compares the
// outcome: how it ended, the End nodes it reached and how often each step
// produced output.

// scriptedJudgeLab runs every step on the lab executor, except that a judge's
// verdict follows its script by the judged gate's attempt, so a restart
// replays the same verdicts.
type scriptedJudgeLab struct {
	lab    formations.FormationExecutor
	judges map[string][]string
}

func (e *scriptedJudgeLab) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	result, err := e.lab.ExecuteFormation(req)
	script, judge := e.judges[req.NodeID]
	if err != nil || !judge {
		return result, err
	}
	verdict := "pass"
	if req.Attempt-1 < len(script) {
		verdict = script[req.Attempt-1]
	}
	result.Text = "```archon-verdict\n{\"verdict\":\"" + verdict + "\",\"reason\":\"judged " + verdict + "\",\"evidence\":[]}\n```"
	for port := range result.Outputs {
		result.Outputs[port] = formations.FormationOutputPayload{Text: result.Text}
	}
	return result, nil
}

type restartCase struct {
	name  string
	board string
	// verdicts answer each gate's requests in turn; past the script, pass.
	verdicts map[string][]string
	// judges script each judge formation's verdicts by gate attempt.
	judges map[string][]string
	// session runs the gates on the session channel, with seats kept on call.
	session bool
	want    runOutcome
}

type runOutcome struct {
	Status  string
	EndIDs  []string
	Outputs map[string]int
}

func (o runOutcome) String() string {
	nodes := make([]string, 0, len(o.Outputs))
	for node, count := range o.Outputs {
		nodes = append(nodes, fmt.Sprintf("%s×%d", node, count))
	}
	sort.Strings(nodes)
	return fmt.Sprintf("%s at %v after %s", o.Status, o.EndIDs, strings.Join(nodes, " "))
}

func outcomeOf(events []formations.RunEvent) runOutcome {
	outcome := runOutcome{Outputs: map[string]int{}}
	for _, event := range events {
		switch event.Type {
		case formations.RunEventNodeOutput:
			if strings.HasPrefix(event.NodeID, "fmn_") {
				outcome.Outputs[event.NodeID]++
			}
		case formations.RunEventSucceeded, formations.RunEventFailed, formations.RunEventCanceled:
			outcome.Status = strings.TrimPrefix(event.Type, "run_")
			if ends, ok := event.Data["endIds"].([]any); ok {
				for _, end := range ends {
					outcome.EndIDs = append(outcome.EndIDs, end.(string))
				}
			}
			sort.Strings(outcome.EndIDs)
		}
	}
	return outcome
}

func openRestartLab(t *testing.T, root string, tc restartCase) *Coordinator {
	t.Helper()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
		if tc.session {
			return &keeperExecutor{store: store}
		}
		return &scriptedJudgeLab{lab: formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: root}), judges: tc.judges}
	})
	if err != nil {
		t.Fatal(err)
	}
	if tc.session {
		c.EnableNeedsYou(NeedsYouConfig{ServerURL: "http://127.0.0.1:18400", RetryInterval: time.Hour, SessionRetryInterval: 20 * time.Millisecond, SessionProbeInterval: 50 * time.Millisecond})
	}
	return c
}

// awaitSettled waits until the run's worker has exited.
func awaitSettled(t *testing.T, c *Coordinator, id string) *Projection {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		change := c.nextChange(id)
		p, err := c.Project(id)
		if err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		busy := c.state(id).busy
		c.mu.Unlock()
		if !busy {
			return p
		}
		select {
		case <-change:
		case <-time.After(time.Until(deadline)):
			t.Fatalf("run never settled: %s", ledgerTrail(eventsOf(t, c, id)))
		}
	}
}

// driveToEnd answers waiting gates by the script and resumes resumable blocks,
// redispatching steps whose seats a restart left, until the run ends.
func driveToEnd(t *testing.T, c *Coordinator, id string, tc restartCase) *Projection {
	t.Helper()
	for range 100 {
		p := awaitSettled(t, c, id)
		events := eventsOf(t, c, id)
		switch {
		case p.Final:
			return p
		case p.Status == formations.RunStatusBlocked:
			if !p.ResumeAllowed {
				t.Fatalf("run blocked for good: %s", ledgerTrail(events))
			}
			mode := "reattach"
			for i := len(events) - 1; i >= 0; i-- {
				if events[i].Type == formations.RunEventBlocked {
					if open, _ := events[i].Data["openDispatches"].([]any); len(open) > 0 {
						mode = "redispatch"
					}
					break
				}
			}
			if w := post(t, c, "/api/runs/"+id+"/resume", `{"mode":"`+mode+`","reason":"continue after a restart"}`); w.Code != 202 {
				t.Fatalf("resume %d %s: %s", w.Code, w.Body.String(), ledgerTrail(events))
			}
		case len(p.WaitingGates) > 0:
			for _, gate := range p.WaitingGates {
				ordinal := 0
				for _, event := range events {
					if event.Type == formations.RunEventHumanInputRequested && event.GateID == gate.GateID && event.Seq <= gate.RequestedSeq {
						ordinal++
					}
				}
				verdict := "pass"
				if script := tc.verdicts[gate.GateID]; ordinal-1 < len(script) {
					verdict = script[ordinal-1]
				}
				w := post(t, c, "/api/runs/"+id+"/gates/"+gate.GateID+"/verdict", `{"requestedSeq":`+strconv.Itoa(gate.RequestedSeq)+`,"verdict":"`+verdict+`","reason":"scripted `+verdict+`"}`)
				if w.Code != 202 {
					t.Fatalf("verdict %d %s: %s", w.Code, w.Body.String(), ledgerTrail(events))
				}
			}
		default:
			// A verdict recorded as the worker settled is routed by the next
			// worker, which may not have started yet.
			if stuck := time.Now().Add(2 * time.Second); !awaitProgress(c, id, len(events), stuck) {
				t.Fatalf("run settled %s with nothing to answer:\n%s", p.Status, fullTrail(events))
			}
		}
	}
	t.Fatalf("run never ended: %s", ledgerTrail(eventsOf(t, c, id)))
	return nil
}

// awaitProgress reports whether the run records another event before until.
func awaitProgress(c *Coordinator, id string, seen int, until time.Time) bool {
	for time.Now().Before(until) {
		if events, err := c.store.ReadRunEvents(id); err == nil && len(events) > seen {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// requireOnlyRestartErrors fails on any error a restart does not explain.
func requireOnlyRestartErrors(t *testing.T, events []formations.RunEvent) {
	t.Helper()
	for _, event := range events {
		if event.Type != formations.RunEventError {
			continue
		}
		if code, _ := event.Data["code"].(string); code != "coordinator_interrupted" && code != "dispatch_reattach_failed" {
			t.Fatalf("unexpected error %s: %s", code, ledgerTrail(events))
		}
	}
}

func restartCases() []restartCase {
	formation := gateBoardFormation
	gate := gateBoardHumanGate
	wire := gateBoardConnection
	return []restartCase{
		{
			name:  "a gate beside a two-step branch",
			board: branchingProofBoard(),
			want:  runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_a": 1, "fmn_b": 1, "fmn_c": 1}},
		},
		{
			name: "a gate beside a two-step branch, asking its kept seat",
			board: strings.Replace(branchingProofBoard(), `beadId = "archon-n7u.53"`, `beadId = "archon-n7u.53"
humanChannel = "session"`, 1),
			session: true,
			want:    runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_a": 1, "fmn_b": 1, "fmn_c": 1}},
		},
		{
			name:     "two gates at once, one rejected",
			board:    twoGatesBesideABranch(),
			verdicts: map[string][]string{"gate_two": {"fail"}},
			want:     runOutcome{Status: "failed", EndIDs: []string{"end_done", "end_rejected"}, Outputs: map[string]int{"fmn_a": 1, "fmn_b": 1, "fmn_c": 1}},
		},
		{
			name: "a gate sending its step back beside a branch",
			board: gateBoard(formation("fmn_a") + formation("fmn_b") + gate("gate_review") + endNodes +
				wire("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
				wire("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
				endWire("edge_pass", "gate_review:pass", "end_done") +
				wire("edge_back", "gate_review:fail", "fmn_a:port_in") +
				wire("edge_m_b", "mis_proof:out", "fmn_b:port_in") +
				endWire("edge_b_done", "fmn_b:port_out", "end_done")),
			verdicts: map[string][]string{"gate_review": {"fail", "pass"}},
			want:     runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_a": 2, "fmn_b": 1}},
		},
	}
}

func TestARestartAfterAnyEventReachesTheSameOutcome(t *testing.T) {
	for _, tc := range restartCases() {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			c := openRestartLab(t, root, tc)
			if err := os.MkdirAll(filepath.Dir(c.store.BoardPath("proof")), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(c.store.BoardPath("proof"), []byte(tc.board), 0o600); err != nil {
				t.Fatal(err)
			}
			id := startProof(t, c)
			driveToEnd(t, c, id, tc)
			events := eventsOf(t, c, id)
			requireOnlyRestartErrors(t, events)
			if got := outcomeOf(events); got.String() != tc.want.String() {
				t.Fatalf("outcome = %s, want %s: %s", got, tc.want, ledgerTrail(events))
			}
			runs := filepath.Join(root, ".archon", "runs", "proof")
			ledger, err := os.ReadFile(filepath.Join(runs, id+".ndjson"))
			if err != nil {
				t.Fatal(err)
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			lines := bytes.SplitAfter(ledger, []byte("\n"))
			for cut := 1; cut < len(events); cut++ {
				t.Run("after "+strconv.Itoa(cut)+" "+events[cut-1].Type, func(t *testing.T) {
					next := t.TempDir()
					for _, file := range []string{id + ".snapshot.toml", id + ".bindings.toml"} {
						raw, err := os.ReadFile(filepath.Join(runs, file))
						if err != nil {
							t.Fatal(err)
						}
						writeState(t, filepath.Join(next, ".archon", "runs", "proof", file), raw)
					}
					writeState(t, filepath.Join(next, ".archon", "runs", "proof", id+".ndjson"), bytes.Join(lines[:cut], nil))
					writeState(t, filepath.Join(next, ".archon", "missions", "proof.mission.toml"), []byte(tc.board))
					restarted := openRestartLab(t, next, tc)
					t.Cleanup(func() { restarted.Close() })
					if err := restarted.RecoverInterruptedRuns(); err != nil {
						t.Fatal(err)
					}
					driveToEnd(t, restarted, id, tc)
					events := eventsOf(t, restarted, id)
					requireOnlyRestartErrors(t, events)
					if got := outcomeOf(events); got.String() != tc.want.String() {
						t.Fatalf("outcome = %s, want %s:\n%s", got, tc.want, fullTrail(events))
					}
				})
			}
		})
	}
}

// fullTrail lists every event with its node and any code, reason or detail.
func fullTrail(events []formations.RunEvent) string {
	var trail []string
	for _, event := range events {
		entry := strconv.Itoa(event.Seq) + " " + event.Type + " " + firstNonEmpty(event.NodeID, event.GateID)
		for _, key := range []string{"code", "reason", "detail", "verdict"} {
			if value, _ := event.Data[key].(string); value != "" {
				entry += " " + key + "=" + value
			}
		}
		trail = append(trail, entry)
	}
	return strings.Join(trail, "\n")
}

func writeState(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
