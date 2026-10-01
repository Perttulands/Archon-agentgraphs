package coordinator

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	// clock, when set, is the run's only clock: each step takes work[node]
	// on it, stopping at its deadline as a real seat is stopped.
	clock *workClock
	work  map[string]time.Duration
	// onStart, when set, runs as each step starts, as an operator acting
	// while the step's seat works.
	onStart func(req formations.FormationExecution)
}

// workClock moves only while a step works, so a run's counted time is the
// same wherever a restart falls.
type workClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *workClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *workClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at
}

func (e *scriptedJudgeLab) ExecuteFormation(req formations.FormationExecution) (formations.FormationExecutionResult, error) {
	if e.onStart != nil {
		e.onStart(req)
	}
	if e.clock != nil {
		end := e.clock.now().Add(e.work[req.NodeID])
		if !req.Deadline.IsZero() && !end.Before(req.Deadline) {
			e.clock.set(req.Deadline)
			return formations.FormationExecutionResult{}, formations.ErrFormationTimeoutExceeded
		}
		e.clock.set(end)
	}
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
	// work, when set, runs the case on a work clock: each step takes its
	// node's work, for the Limit cards' time.
	work map[string]time.Duration
	// answerWhile answers a gate's waiting request, by the verdict script,
	// as the named step starts: an operator deciding while a seat works.
	answerWhile map[string][]string
	// midStepOptional stops answerWhile from requiring a verdict mid-step.
	midStepOptional bool
	// wantInputs names text each step's runs must have received.
	wantInputs map[string][]string
	want       runOutcome
}

// requireAnsweredMidStep fails unless each step answerWhile names had a
// verdict recorded while it ran.
func requireAnsweredMidStep(t *testing.T, tc restartCase, events []formations.RunEvent) {
	t.Helper()
	if tc.midStepOptional {
		return
	}
	for node := range tc.answerWhile {
		running, answered := false, false
		for _, event := range events {
			switch {
			case event.Type == formations.RunEventNodeStarted && event.NodeID == node:
				running = true
			case event.Type == formations.RunEventNodeOutput && event.NodeID == node:
				running = false
			case event.Type == formations.RunEventHumanVerdictRecorded && running:
				answered = true
			}
		}
		if !answered {
			t.Fatalf("no verdict was recorded while %s worked:\n%s", node, fullTrail(events))
		}
	}
}

// scriptedVerdict is the verdict the case's script gives the gate's request.
func scriptedVerdict(tc restartCase, events []formations.RunEvent, gateID string, requestedSeq int) string {
	ordinal := 0
	for _, event := range events {
		if event.Type == formations.RunEventHumanInputRequested && event.GateID == gateID && event.Seq <= requestedSeq {
			ordinal++
		}
	}
	if script := tc.verdicts[gateID]; ordinal-1 < len(script) {
		return script[ordinal-1]
	}
	return "pass"
}

type runOutcome struct {
	Status  string
	EndIDs  []string
	Outputs map[string]int
	// Grants counts the Limit card grants the run needed.
	Grants int
	// Inputs lists, per step, what each run of it that produced output ran
	// on: every input's source, text, feedback and responses, in order.
	Inputs map[string][]string
}

func (o runOutcome) String() string {
	nodes := make([]string, 0, len(o.Outputs))
	for node, count := range o.Outputs {
		nodes = append(nodes, fmt.Sprintf("%s×%d", node, count))
	}
	sort.Strings(nodes)
	return fmt.Sprintf("%s at %v after %s with %d grants", o.Status, o.EndIDs, strings.Join(nodes, " "), o.Grants)
}

// inputsString lists what each step ran on, for comparing two runs.
func (o runOutcome) inputsString() string {
	nodes := make([]string, 0, len(o.Inputs))
	for node := range o.Inputs {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	var b strings.Builder
	for _, node := range nodes {
		for i, inputs := range o.Inputs[node] {
			fmt.Fprintf(&b, "%s run %d: %s\n", node, i+1, inputs)
		}
	}
	return b.String()
}

// inputSummary is one run's inputs as its node_started recorded them.
func inputSummary(started formations.RunEvent) string {
	raw, _ := started.Data["inputRefs"].([]any)
	parts := make([]string, 0, len(raw))
	for _, item := range raw {
		fields, _ := item.(map[string]any)
		part := fmt.Sprintf("[from %v to %v: %q", fields["fromNodeId"], fields["toPortId"], fields["text"])
		for feedback, _ := fields["feedback"].(map[string]any); feedback != nil; feedback, _ = feedback["earlier"].(map[string]any) {
			part += fmt.Sprintf(" feedback %v %v %q", feedback["gateId"], feedback["verdict"], feedback["reason"])
		}
		for response, _ := fields["response"].(map[string]any); response != nil; response, _ = response["earlier"].(map[string]any) {
			part += fmt.Sprintf(" response %v %q", response["gateId"], response["text"])
		}
		parts = append(parts, part+"]")
	}
	return strings.Join(parts, " ")
}

func outcomeOf(events []formations.RunEvent) runOutcome {
	outcome := runOutcome{Outputs: map[string]int{}, Inputs: map[string][]string{}}
	started := map[string]formations.RunEvent{}
	for _, event := range events {
		switch event.Type {
		case formations.RunEventNodeStarted:
			started[event.NodeID] = event
		case formations.RunEventNodeOutput:
			if strings.HasPrefix(event.NodeID, "fmn_") {
				outcome.Outputs[event.NodeID]++
				outcome.Inputs[event.NodeID] = append(outcome.Inputs[event.NodeID], inputSummary(started[event.NodeID]))
			}
		case formations.RunEventResumed:
			if _, granted := event.Data["grant"]; granted {
				outcome.Grants++
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

// openRestartLab opens a daemon on root; a work-clock case's clock resumes at
// start, the time of the last event the state holds.
func openRestartLab(t *testing.T, root string, tc restartCase, start time.Time) *Coordinator {
	t.Helper()
	personas := formations.NewPersonaStore(filepath.Join(root, "agents"))
	var clock *workClock
	if tc.work != nil {
		clock = &workClock{at: start}
	}
	var c *Coordinator
	onStart := func(req formations.FormationExecution) {
		if c == nil {
			return
		}
		for _, gateID := range tc.answerWhile[req.NodeID] {
			events, err := c.store.ReadRunEvents(req.RunID)
			if err != nil {
				return
			}
			for _, request := range formations.OpenHumanRequests(events) {
				if request.GateID == gateID {
					verdict := scriptedVerdict(tc, events, gateID, request.Seq)
					post(t, c, "/api/runs/"+req.RunID+"/gates/"+gateID+"/verdict", `{"requestedSeq":`+strconv.Itoa(request.Seq)+`,"verdict":"`+verdict+`","reason":"scripted `+verdict+`"}`)
				}
			}
		}
	}
	c, err := Open(root, personas, func(store *formations.Store) formations.FormationExecutor {
		if clock != nil {
			store.Now = clock.now
		}
		if tc.session {
			return &keeperExecutor{store: store}
		}
		return &scriptedJudgeLab{lab: formations.NewLabFormationExecutor(store, personas, formations.LabExecutorConfig{Harnesses: []string{"openai-codex"}, Cwd: root}), judges: tc.judges, clock: clock, work: tc.work, onStart: onStart}
	})
	if err != nil {
		t.Fatal(err)
	}
	if tc.session {
		c.EnableNeedsYou(NeedsYouConfig{ServerURL: "http://127.0.0.1:18400", RetryInterval: time.Hour, SessionRetryInterval: 20 * time.Millisecond, SessionProbeInterval: 50 * time.Millisecond})
	}
	return c
}

// awaitSettled waits until the run's worker has exited; generously, since
// under host load each durable append can take seconds.
func awaitSettled(t *testing.T, c *Coordinator, id string) *Projection {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		change := c.nextChange(id)
		c.mu.Lock()
		busy := c.state(id).busy
		c.mu.Unlock()
		if !busy {
			// Projected after the worker is seen gone, so it shows all the
			// worker recorded.
			p, err := c.Project(id)
			if err != nil {
				t.Fatal(err)
			}
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
			// A spent Limit card resumes only with a grant (archon-o7p.8).
			grant := strconv.FormatBool(p.ResumePolicy == formations.ResumePolicyGrant)
			if w := post(t, c, "/api/runs/"+id+"/resume", `{"mode":"`+mode+`","reason":"continue after a restart","grant":`+grant+`}`); w.Code != 202 {
				t.Fatalf("resume %d %s: %s", w.Code, w.Body.String(), ledgerTrail(events))
			}
		case len(p.WaitingGates) > 0:
			// One answer per settle, oldest request first: the run routes it
			// before the next is given, so a step that answers a gate as it
			// starts never races the driver.
			gate := p.WaitingGates[0]
			for _, waiting := range p.WaitingGates {
				if waiting.RequestedSeq < gate.RequestedSeq {
					gate = waiting
				}
			}
			verdict := scriptedVerdict(tc, events, gate.GateID, gate.RequestedSeq)
			w := post(t, c, "/api/runs/"+id+"/gates/"+gate.GateID+"/verdict", `{"requestedSeq":`+strconv.Itoa(gate.RequestedSeq)+`,"verdict":"`+verdict+`","reason":"scripted `+verdict+`"}`)
			if w.Code != 202 {
				t.Fatalf("verdict %d %s: %s", w.Code, w.Body.String(), ledgerTrail(events))
			}
			if !awaitProgress(c, id, len(events), time.Now().Add(10*time.Second)) {
				t.Fatalf("the verdict recorded nothing:\n%s", fullTrail(events))
			}
		default:
			// A verdict recorded as the worker settled is routed by the next
			// worker, which may not have started yet.
			if stuck := time.Now().Add(10 * time.Second); !awaitProgress(c, id, len(events), stuck) {
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
		if code, _ := event.Data["code"].(string); code != "coordinator_interrupted" && code != "dispatch_reattach_failed" && code != formations.RunBlockLimitReached {
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
		{
			// The operator sends A back while B works; A runs again after B.
			name: "a gate sending its step back while its branch works",
			board: gateBoard(formation("fmn_a") + formation("fmn_b") + gate("gate_review") + endNodes +
				wire("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
				wire("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
				endWire("edge_pass", "gate_review:pass", "end_done") +
				wire("edge_back", "gate_review:fail", "fmn_a:port_in") +
				wire("edge_m_b", "mis_proof:out", "fmn_b:port_in") +
				endWire("edge_b_done", "fmn_b:port_out", "end_done")),
			verdicts:    map[string][]string{"gate_review": {"fail", "pass"}},
			answerWhile: map[string][]string{"fmn_b": {"gate_review"}},
			want:        runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_a": 2, "fmn_b": 1}},
		},
		{
			// The review's probe: Review rejects A into F's port while P, which
			// also feeds that port, works. F runs once on P's work with the
			// rejection, live and after any restart.
			name:        "a rejection into a port the working step also feeds",
			board:       rejectionIntoAFedPortBoard(),
			verdicts:    map[string][]string{"gate_review": {"fail"}},
			answerWhile: map[string][]string{"fmn_p": {"gate_review"}},
			wantInputs:  map[string][]string{"fmn_f": {"[from fmn_p to port_in", "feedback gate_review fail"}},
			want:        runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_a": 1, "fmn_p": 1, "fmn_f": 1}},
		},
		{
			// Both gates are answered while C works: One passes, Two rejects.
			name:        "two gates answered while another step works",
			board:       twoGatesBesideABranch(),
			verdicts:    map[string][]string{"gate_two": {"fail"}},
			answerWhile: map[string][]string{"fmn_c": {"gate_one", "gate_two"}},
			want:        runOutcome{Status: "failed", EndIDs: []string{"end_done", "end_rejected"}, Outputs: map[string]int{"fmn_a": 1, "fmn_b": 1, "fmn_c": 1}},
		},
		{
			name: "a judge sending work back while a gate waits",
			board: gateBoard(formation("fmn_a") + formation("fmn_work") + formation("fmn_judge") + formation("fmn_ship") + gate("gate_review") + `
[[gate]]
id = "gate_judge"
title = "Judge"
kinds = ["formation"]
criterion = "Judge the work"
` + endNodes +
				wire("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
				wire("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
				endWire("edge_review_pass", "gate_review:pass", "end_done") +
				endWire("edge_review_fail", "gate_review:fail", "end_rejected") +
				wire("edge_m_work", "mis_proof:out", "fmn_work:port_in") +
				wire("edge_work_judge", "fmn_work:port_out", "gate_judge:in") +
				wire("edge_gate_judge", "gate_judge:judge", "fmn_judge:port_in") +
				wire("edge_judge_verdict", "fmn_judge:port_out", "gate_judge:judge") +
				wire("edge_judge_fail", "gate_judge:fail", "fmn_work:port_in") +
				wire("edge_judge_pass", "gate_judge:pass", "fmn_ship:port_in") +
				endWire("edge_ship_done", "fmn_ship:port_out", "end_done")),
			judges: map[string][]string{"fmn_judge": {"fail", "pass"}},
			want:   runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_a": 1, "fmn_work": 2, "fmn_judge": 2, "fmn_ship": 1}},
		},
		{
			name: "a two-judge chain sending work back",
			board: gateBoard(formation("fmn_work") + formation("fmn_first") + formation("fmn_last") + `
[[gate]]
id = "gate_judge"
title = "Judge"
kinds = ["formation"]
criterion = "Judge the work"
` + endNodes +
				wire("edge_m_work", "mis_proof:out", "fmn_work:port_in") +
				wire("edge_work_judge", "fmn_work:port_out", "gate_judge:in") +
				wire("edge_gate_first", "gate_judge:judge", "fmn_first:port_in") +
				wire("edge_first_last", "fmn_first:port_out", "fmn_last:port_in") +
				wire("edge_last_verdict", "fmn_last:port_out", "gate_judge:judge") +
				wire("edge_judge_fail", "gate_judge:fail", "fmn_work:port_in") +
				endWire("edge_judge_pass", "gate_judge:pass", "end_done")),
			judges: map[string][]string{"fmn_last": {"fail", "pass"}},
			want:   runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_work": 2, "fmn_first": 2, "fmn_last": 2}},
		},
		{
			// Work may run twice; the third try waits for a grant.
			name:   "a step's Limit card stopping a judge loop until granted",
			board:  judgeLoopBoard(`target = "fmn_work"`),
			judges: map[string][]string{"fmn_judge": {"fail", "fail", "pass"}},
			want:   runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_work": 3, "fmn_judge": 3}, Grants: 1},
		},
		{
			// The mission may run four steps: Work and its judge twice. Work's
			// third try and its judge each wait for a grant.
			name:   "the mission's Limit card stopping a judge loop until granted",
			board:  judgeLoopBoard(`target = "mis_proof"`),
			judges: map[string][]string{"fmn_judge": {"fail", "fail", "pass"}},
			want:   runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_work": 3, "fmn_judge": 3}, Grants: 2},
		},
		{
			// Work may work 70 s: its third try runs out after 10 s and waits
			// for a grant of 70 s more, which its fourth try uses 30 of.
			name:   "a step's time card stopping a judge loop until granted",
			board:  strings.Replace(judgeLoopBoard(`target = "fmn_work"`), "rounds = 2", "seconds = 70", 1),
			judges: map[string][]string{"fmn_judge": {"fail", "fail", "pass"}},
			work:   map[string]time.Duration{"fmn_work": 30 * time.Second, "fmn_judge": 10 * time.Second},
			want:   runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_work": 3, "fmn_judge": 3}, Grants: 1},
		},
		{
			// The mission may work 95 s: A and B use 70 while the gate waits,
			// the send-back's A runs out after 25 s, and the grant's A finishes.
			name: "the mission's time card pausing at a gate and stopping a send-back",
			board: gateBoard(gateBoardFormation("fmn_a") + gateBoardFormation("fmn_b") + gateBoardHumanGate("gate_review") + endNodes + `
[[limit]]
id = "lim_clock"
title = "Clock"
target = "mis_proof"
seconds = 95
` +
				gateBoardConnection("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
				gateBoardConnection("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
				endWire("edge_pass", "gate_review:pass", "end_done") +
				gateBoardConnection("edge_back", "gate_review:fail", "fmn_a:port_in") +
				gateBoardConnection("edge_m_b", "mis_proof:out", "fmn_b:port_in") +
				endWire("edge_b_done", "fmn_b:port_out", "end_done")),
			verdicts: map[string][]string{"gate_review": {"fail", "pass"}},
			work:     map[string]time.Duration{"fmn_a": 30 * time.Second, "fmn_b": 40 * time.Second},
			want:     runOutcome{Status: "succeeded", EndIDs: []string{"end_done"}, Outputs: map[string]int{"fmn_a": 2, "fmn_b": 1}, Grants: 1},
		},
	}
}

// rejectionIntoAFedPortBoard is Input -> A -> Review (pass Done, fail into
// F's input) beside Input -> P -> F -> Done, so a rejection and P's work can
// both reach F's one port before F runs (the o7p.11 review's probe).
func rejectionIntoAFedPortBoard() string {
	return gateBoard(gateBoardFormation("fmn_a") + gateBoardFormation("fmn_p") + gateBoardFormation("fmn_f") + gateBoardHumanGate("gate_review") + endNodes +
		gateBoardConnection("edge_m_a", "mis_proof:out", "fmn_a:port_in") +
		gateBoardConnection("edge_a_gate", "fmn_a:port_out", "gate_review:in") +
		endWire("edge_pass", "gate_review:pass", "end_done") +
		gateBoardConnection("edge_fail_f", "gate_review:fail", "fmn_f:port_in") +
		gateBoardConnection("edge_m_p", "mis_proof:out", "fmn_p:port_in") +
		gateBoardConnection("edge_p_f", "fmn_p:port_out", "fmn_f:port_in") +
		endWire("edge_f_done", "fmn_f:port_out", "end_done"))
}

// judgeLoopBoard is Work judged by a formation that sends it back on fail,
// with a two-round Limit card on the given target, or four for the mission.
func judgeLoopBoard(target string) string {
	rounds := "2"
	if strings.Contains(target, "mis_proof") {
		rounds = "4"
	}
	return gateBoard(gateBoardFormation("fmn_work") + gateBoardFormation("fmn_judge") + `
[[gate]]
id = "gate_judge"
title = "Judge"
kinds = ["formation"]
criterion = "Judge the work"

[[limit]]
id = "lim_cap"
title = "Cap"
` + target + `
rounds = ` + rounds + `
` + endNodes +
		gateBoardConnection("edge_m_work", "mis_proof:out", "fmn_work:port_in") +
		gateBoardConnection("edge_work_judge", "fmn_work:port_out", "gate_judge:in") +
		gateBoardConnection("edge_gate_judge", "gate_judge:judge", "fmn_judge:port_in") +
		gateBoardConnection("edge_judge_verdict", "fmn_judge:port_out", "gate_judge:judge") +
		gateBoardConnection("edge_judge_fail", "gate_judge:fail", "fmn_work:port_in") +
		endWire("edge_judge_pass", "gate_judge:pass", "end_done"))
}

func TestARestartAfterAnyEventReachesTheSameOutcome(t *testing.T) {
	for _, tc := range restartCases() {
		t.Run(tc.name, func(t *testing.T) { checkRestartCuts(t, tc) })
	}
}

// checkRestartCuts runs tc's mission to its end, then restarts it after every
// event and requires the same outcome, each step having run on the same
// inputs. A case without a want only compares the restarts with the run.
func checkRestartCuts(t *testing.T, tc restartCase) {
	t.Helper()
	root := t.TempDir()
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	c := openRestartLab(t, root, tc, start)
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
	reference := outcomeOf(events)
	if tc.want.Status != "" && reference.String() != tc.want.String() {
		t.Fatalf("outcome = %s, want %s: %s", reference, tc.want, ledgerTrail(events))
	}
	requireAnsweredMidStep(t, tc, events)
	for node, texts := range tc.wantInputs {
		runs := strings.Join(reference.Inputs[node], "\n")
		for _, text := range texts {
			if !strings.Contains(runs, text) {
				t.Fatalf("%s ran on %s, missing %q", node, runs, text)
			}
		}
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
			resumeAt, _ := time.Parse(time.RFC3339Nano, events[cut-1].Timestamp)
			restarted := openRestartLab(t, next, tc, resumeAt)
			t.Cleanup(func() { restarted.Close() })
			if err := restarted.RecoverInterruptedRuns(); err != nil {
				t.Fatal(err)
			}
			driveToEnd(t, restarted, id, tc)
			events := eventsOf(t, restarted, id)
			requireOnlyRestartErrors(t, events)
			got := outcomeOf(events)
			if got.String() != reference.String() {
				t.Fatalf("outcome = %s, without the restart %s:\n%s", got, reference, fullTrail(events))
			}
			// Each step ran on what it ran on without the restart.
			if got.inputsString() != reference.inputsString() {
				t.Fatalf("inputs after the restart:\n%s\nwithout it:\n%s\n%s", got.inputsString(), reference.inputsString(), fullTrail(events))
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
