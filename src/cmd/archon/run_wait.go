package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/coordinator"
	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// `archon run wait` (archon-o7p.6) blocks until a run needs its driver, ends or
// changes, then prints one paragraph written for the agent that reads it. The
// daemon decides; this client long-polls it, reconnects across restarts with
// the same cursor, and turns the answer into text, JSON and an exit code.

const (
	waitExitFinal    = 0
	waitExitNeedsYou = 3
	waitExitChanged  = 4
	waitExitTimeout  = 5
	waitExitLost     = 6

	waitHold          = 30 * time.Second
	waitRetryInterval = 250 * time.Millisecond
	// waitExcerptBytes bounds the gate input quoted in the paragraph.
	waitExcerptBytes = 1500
)

// runWaitOffline refuses: only the daemon knows when a run has settled.
func runWaitOffline(stderr io.Writer) int {
	fmt.Fprintln(stderr, "run wait needs --server: only the Archon daemon knows when a run has settled")
	fmt.Fprintln(stderr, commandUsage("run wait"))
	return 2
}

// waitOutput is the --json document: the daemon's answer plus what the
// client adds, the next command and, on timeout or lost contact, why it
// returned.
type waitOutput struct {
	coordinator.RunWait
	// Next is the command that waits for what comes after this answer; a
	// final run has none.
	Next string `json:"next,omitempty"`
	// Error says why the client gave up on the daemon.
	Error string `json:"error,omitempty"`
}

func runWaitRemote(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run wait", stderr)
	until := fs.String("until", coordinator.WaitUntilNeedsYou, "needs-you, final or any-change")
	since := fs.Int("since", 0, "ledger sequence already seen; pass the seq the last wait printed")
	timeout := fs.Duration("timeout", 0, "give up after this long (0 waits until the run answers)")
	reconnect := fs.Duration("reconnect", time.Minute, "keep trying an unreachable daemon this long")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 || !coordinator.ValidWaitUntil(*until) || *since < 0 || *timeout < 0 || *reconnect < 0 {
		fmt.Fprintln(stderr, commandUsage("run wait"))
		return 2
	}
	runID := fs.Arg(0)
	w := newRunWaiter(c, runID, *until, *since, *reconnect)
	defer w.http.CloseIdleConnections()
	out, code, err := w.wait(*timeout)
	if err != nil {
		return fail(stderr, err)
	}
	if code != waitExitFinal {
		out.Next = waitCommand(c.server, runID, *until, out.Seq, *jsonOut)
	}
	if *jsonOut {
		if writeJSON(stdout, out) != 0 {
			return 1
		}
		return code
	}
	fmt.Fprint(stdout, renderWait(c.server, out, code))
	return code
}

type runWaiter struct {
	client *remoteClient
	// http is the one client every poll of this wait reuses, so a long wait
	// keeps a single connection to the daemon.
	http      *http.Client
	runID     string
	until     string
	since     int
	reconnect time.Duration
	now       func() time.Time
	sleep     func(time.Duration)
}

func newRunWaiter(c *remoteClient, runID, until string, since int, reconnect time.Duration) *runWaiter {
	return &runWaiter{
		client: c, runID: runID, until: until, since: since, reconnect: reconnect, now: time.Now, sleep: time.Sleep,
		http: &http.Client{
			// The daemon holds each poll at most waitHold before its headers.
			Transport:     &http.Transport{Proxy: nil, ResponseHeaderTimeout: waitHold + 15*time.Second, MaxIdleConnsPerHost: 1},
			CheckRedirect: c.http.CheckRedirect,
		},
	}
}

// wait polls until the daemon answers, the timeout passes or the daemon stays
// unreachable past the reconnect window. It returns an error only for answers
// that waiting again cannot fix, such as an unknown run.
func (w *runWaiter) wait(timeout time.Duration) (*waitOutput, int, error) {
	start := w.now()
	last := &waitOutput{RunWait: coordinator.RunWait{RunID: w.runID, Until: w.until, Since: w.since, Seq: w.since, Outcome: coordinator.WaitOutcomePending}}
	var lostAt time.Time
	for {
		hold := waitHold
		if timeout > 0 {
			remaining := timeout - w.now().Sub(start)
			if remaining <= 0 {
				if !lostAt.IsZero() {
					return last, waitExitLost, nil
				}
				last.Outcome = "timeout"
				return last, waitExitTimeout, nil
			}
			// Whole seconds, rounded up, so the last poll ends at the timeout.
			hold = min(hold, (remaining + time.Second - 1).Truncate(time.Second))
		}
		answer, retry, err := w.poll(hold)
		if err != nil && !retry {
			return nil, 1, err
		}
		if err != nil {
			if lostAt.IsZero() {
				lostAt = w.now()
			}
			last.Error = err.Error()
			if w.now().Sub(lostAt) >= w.reconnect {
				last.Outcome = "daemon-lost"
				return last, waitExitLost, nil
			}
			w.sleep(waitRetryInterval)
			continue
		}
		lostAt = time.Time{}
		last = &waitOutput{RunWait: *answer}
		switch answer.Outcome {
		case coordinator.WaitOutcomeFinal:
			return last, waitExitFinal, nil
		case coordinator.WaitOutcomeNeedsYou:
			return last, waitExitNeedsYou, nil
		case coordinator.WaitOutcomeChanged:
			return last, waitExitChanged, nil
		}
	}
}

// poll asks the daemon once. retry is true when the daemon could not answer,
// such as while it restarts.
func (w *runWaiter) poll(hold time.Duration) (*coordinator.RunWait, bool, error) {
	query := url.Values{"until": {w.until}, "since": {strconv.Itoa(w.since)}, "hold": {strconv.Itoa(int(hold / time.Second))}}
	response, err := w.http.Get(w.client.server + "/api/runs/" + url.PathEscape(w.runID) + "/wait?" + query.Encode())
	if err != nil {
		return nil, true, fmt.Errorf("daemon unreachable: %w", err)
	}
	defer response.Body.Close()
	// Reading the whole body lets the connection return to the pool.
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, true, fmt.Errorf("daemon answer cut off: %w", err)
	}
	if response.StatusCode == http.StatusServiceUnavailable || response.StatusCode == http.StatusBadGateway || response.StatusCode == http.StatusGatewayTimeout {
		return nil, true, fmt.Errorf("daemon unavailable: HTTP %d", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("coordinator HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	var envelope struct {
		Data *coordinator.RunWait `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Data == nil {
		return nil, false, errors.New("coordinator wait answer has no data")
	}
	return envelope.Data, false, nil
}

func waitCommand(server, runID, until string, since int, jsonOut bool) string {
	command := fmt.Sprintf("archon --server %s run wait %s --until %s --since %d", server, runID, until, since)
	if jsonOut {
		command += " --json"
	}
	return command
}

// waitStatus says a run status in words.
func waitStatus(status string) string {
	switch status {
	case "waiting_human":
		return "waiting for a human verdict"
	case "":
		return "in an unknown state"
	}
	return status
}

// renderWait writes the paragraph for the agent driving the run: what the run
// waits on and the exact command that answers it, how it ended and why, or
// what changed, and always how to wait for what comes next.
func renderWait(server string, out *waitOutput, code int) string {
	var b strings.Builder
	r := out.RunWait
	name := fmt.Sprintf("Run %s (mission %q)", r.RunID, r.Mission)
	switch code {
	case waitExitFinal:
		writeWaitEnd(&b, name, r)
	case waitExitNeedsYou:
		fmt.Fprintf(&b, "%s needs you.\n", name)
		writeWaitAsks(&b, server, r, true)
	case waitExitChanged:
		fmt.Fprintf(&b, "%s changed: %s. It is now %s.\n", name, waitSpan(r), waitStatus(r.Status))
		if r.ChangesOmitted > 0 {
			fmt.Fprintf(&b, "  (%d earlier events omitted: archon --server %s run logs %s)\n", r.ChangesOmitted, server, r.RunID)
		}
		for _, change := range r.Changes {
			fmt.Fprintf(&b, "  #%d %s\n", change.Seq, describeWaitChange(change))
		}
		writeWaitAsks(&b, server, r, false)
	case waitExitTimeout:
		fmt.Fprintf(&b, "Run %s: nothing the wait asked for (%s) happened before the timeout. ", r.RunID, r.Until)
		if r.Status != "" {
			fmt.Fprintf(&b, "It is %s.\n", waitStatus(r.Status))
		} else {
			b.WriteString("\n")
		}
		writeWaitAsks(&b, server, r, false)
	case waitExitLost:
		fmt.Fprintf(&b, "Run %s: lost contact with the Archon daemon at %s (%s). The run keeps its ledger, so nothing after #%d is missed; wait again once the daemon is back.\n", r.RunID, server, out.Error, r.Seq)
	}
	if out.Next != "" {
		fmt.Fprintf(&b, "Wait for what comes next: %s\n", out.Next)
	}
	return b.String()
}

func writeWaitEnd(b *strings.Builder, name string, r coordinator.RunWait) {
	end := r.End
	if end == nil {
		fmt.Fprintf(b, "%s ended: %s.\n", name, waitStatus(r.Status))
		return
	}
	fmt.Fprintf(b, "%s %s at #%d", name, end.Status, end.Seq)
	if len(end.Stopped) > 0 {
		titles := make([]string, 0, len(end.Stopped))
		for _, node := range end.Stopped {
			titles = append(titles, strconv.Quote(node.Title))
		}
		fmt.Fprintf(b, " while at %s", strings.Join(titles, ", "))
	}
	if end.Reason != "" {
		fmt.Fprintf(b, ": %s", end.Reason)
		if end.ReasonTruncated {
			b.WriteString(" [reason cut short; the full text is in the run evidence problems]")
		}
	}
	if end.Code != "" && end.Code != end.Reason {
		fmt.Fprintf(b, " (%s)", end.Code)
	}
	endSentence(b)
	if end.EndedBy != "" {
		fmt.Fprintf(b, " Ended by %s.", waitActor(end.EndedBy))
	}
	b.WriteString("\n")
}

// endSentence ends a sentence that may close on quoted text ending in its own
// punctuation.
func endSentence(b *strings.Builder) {
	if text := b.String(); !strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "!") && !strings.HasSuffix(text, "?") {
		b.WriteString(".")
	}
}

// waitActor names who ended a run the way the cockpit does.
func waitActor(actor string) string {
	switch {
	case actor == formations.RunFailureActor:
		return "Archon (archond)"
	case strings.HasPrefix(actor, "human:"), strings.HasPrefix(actor, "operator:"):
		return "the operator (" + actor + ")"
	}
	return actor
}

func writeWaitAsks(b *strings.Builder, server string, r coordinator.RunWait, onlyNew bool) {
	var older []coordinator.WaitAsk
	for _, ask := range r.Asks {
		if onlyNew && !ask.New {
			older = append(older, ask)
			continue
		}
		writeWaitAsk(b, server, r.RunID, ask)
	}
	if !onlyNew {
		return
	}
	for _, ask := range older {
		fmt.Fprintf(b, "It still waits on %s (asked at #%d, reported earlier).\n", describeWaitAskShort(ask), ask.Seq)
	}
}

func describeWaitAskShort(ask coordinator.WaitAsk) string {
	switch ask.Kind {
	case formations.NeedsYouKindHumanGate:
		return fmt.Sprintf("the gate %q", ask.Title)
	case formations.NeedsYouKindEscalation:
		return fmt.Sprintf("an escalation from %q", ask.Title)
	}
	return "a block"
}

func writeWaitAsk(b *strings.Builder, server, runID string, ask coordinator.WaitAsk) {
	switch ask.Kind {
	case formations.NeedsYouKindHumanGate:
		fmt.Fprintf(b, "The gate %q (%s) waits for a verdict, asked at #%d.\n", ask.Title, ask.GateID, ask.Seq)
		fmt.Fprintf(b, "Criterion: %s\n", ask.Criterion)
		if ask.Input != nil {
			from := ask.Input.FromTitle
			if from == "" {
				from = "the upstream step"
			}
			excerpt, cut := formations.CapEvidenceText(ask.Input.Text, waitExcerptBytes)
			fmt.Fprintf(b, "It received this from %q:\n", from)
			for _, line := range strings.Split(strings.TrimRight(excerpt, "\n"), "\n") {
				fmt.Fprintf(b, "  | %s\n", line)
			}
			if cut || ask.Input.Truncated {
				fmt.Fprintf(b, "  [first %d of %d bytes; read all of it: archon --server %s gate request %s %s]\n", len(excerpt), ask.Input.Bytes, server, runID, ask.GateID)
			}
		}
		for _, route := range ask.Routes {
			if where := describeGateRoute(route); where != "" {
				fmt.Fprintf(b, "On %s: %s.\n", route.Verdict, where)
			}
		}
		b.WriteString("Answer it with exactly one of:\n")
		fmt.Fprintf(b, "  archon --server %s gate approve %s %s --requested-seq %d --response 'your answer'\n", server, runID, ask.GateID, ask.Seq)
		fmt.Fprintf(b, "  archon --server %s gate reject %s %s --requested-seq %d --response 'what to change'\n", server, runID, ask.GateID, ask.Seq)
	case formations.NeedsYouKindEscalation:
		fmt.Fprintf(b, "%q escalated at #%d and the run stopped for it: %s\n", ask.Title, ask.Seq, ask.Reason)
		fmt.Fprintf(b, "The run stays blocked until you resolve it and resume:\n  archon --server %s run resume %s --reason 'how it was resolved'\n", server, runID)
	case formations.NeedsYouKindBlocked:
		reason := ask.Reason
		if reason == "" {
			reason = "no reason recorded"
		}
		where := ""
		if ask.Title != "" {
			where = fmt.Sprintf(" at %q", ask.Title)
		}
		fmt.Fprintf(b, "The run is blocked%s since #%d: %s", where, ask.Seq, reason)
		if ask.Code != "" && ask.Code != reason {
			fmt.Fprintf(b, " (%s)", ask.Code)
		}
		endSentence(b)
		b.WriteString("\n")
		switch {
		case ask.ResumeAllowed && ask.Limit != nil:
			fmt.Fprintf(b, "Give it %s if the work deserves it, or stop it:\n  archon --server %s run resume %s --grant --reason 'why more'\n  archon --server %s run abort %s --reason 'why'\n", formations.GrantWords(*ask.Limit), server, runID, server, runID)
		case ask.ResumeAllowed:
			fmt.Fprintf(b, "Resume it once the cause is resolved:\n  archon --server %s run resume %s --reason 'what you resolved'\n", server, runID)
		default:
			fmt.Fprintf(b, "It cannot resume. Stop it and start a new run:\n  archon --server %s run abort %s --reason 'why'\n", server, runID)
		}
	}
}

// describeGateRoute says where a verdict leads, as the cockpit's answer panel does.
func describeGateRoute(route formations.GateRoute) string {
	var parts, steps []string
	for _, target := range route.Targets {
		if target.Kind == "end" {
			continue
		}
		step := strconv.Quote(firstWaitNonEmpty(target.Title, target.NodeID))
		var notes []string
		if rounds := target.Rounds; rounds != nil && rounds.Used < rounds.Max {
			notes = append(notes, fmt.Sprintf("round %d of %d", rounds.Used+1, rounds.Max))
		}
		if clock := target.Time; clock != nil && clock.Used < clock.Max {
			notes = append(notes, formations.LeftWords(*clock))
		}
		if len(notes) > 0 {
			step += " (" + strings.Join(notes, ", ") + ")"
		}
		steps = append(steps, step)
	}
	if len(steps) > 0 {
		parts = append(parts, "goes to "+strings.Join(steps, ", "))
	}
	for _, target := range route.Targets {
		if target.Kind == "end" {
			parts = append(parts, "this path ends ("+target.Outcome+")")
		}
	}
	if mission := route.MissionRounds; mission != nil && route.Limit == nil && route.RoundsNeeded > 0 && mission.Max-mission.Used <= route.RoundsNeeded {
		// As the cockpit's answer panel says it, once the mission's rounds run short.
		parts = append(parts, fmt.Sprintf("the mission has %d of %d rounds left", mission.Max-mission.Used, mission.Max))
	}
	if clock := route.MissionTime; clock != nil && route.Limit == nil && len(steps) > 0 {
		parts = append(parts, "the mission has "+strings.Replace(formations.LeftWords(*clock), " left", " of working time left", 1))
	}
	where := strings.Join(parts, "; ")
	switch {
	case route.EndsRun && route.RunFails:
		where += ", and nothing else can run, so the run fails"
	case route.EndsRun:
		where += ", and nothing else can run, so the run succeeds"
	case route.RunFails:
		where += ", so the run fails once its other open work ends"
	}
	limit := route.Limit
	if where == "" || limit == nil {
		return where
	}
	who := "the mission"
	if mission := firstMissionCard(route); mission == nil || limit.LimitID != mission.LimitID {
		who = strconv.Quote(limit.NodeID)
		for _, target := range route.Targets {
			if target.NodeID == limit.NodeID && target.Title != "" {
				who = strconv.Quote(target.Title)
			}
		}
	}
	return fmt.Sprintf("%s, but %s has used %s, so the run blocks instead until you grant %s", where, who, formations.SpentWords(*limit), formations.GrantWords(*limit))
}

// firstMissionCard is the mission's Limit card as a route reports it.
func firstMissionCard(route formations.GateRoute) *formations.RunLimitReached {
	if route.MissionRounds != nil {
		return route.MissionRounds
	}
	return route.MissionTime
}

func firstWaitNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func waitSpan(r coordinator.RunWait) string {
	n := r.Seq - r.Since
	if n == 1 {
		return fmt.Sprintf("1 new event (#%d)", r.Seq)
	}
	return fmt.Sprintf("%d new events (#%d-#%d)", n, r.Since+1, r.Seq)
}

func describeWaitChange(change coordinator.WaitChange) string {
	var b strings.Builder
	b.WriteString(change.Type)
	if change.Title != "" {
		fmt.Fprintf(&b, " %q", change.Title)
	}
	if change.Attempt > 0 {
		fmt.Fprintf(&b, " attempt %d", change.Attempt)
	}
	if change.Status != "" {
		fmt.Fprintf(&b, " %s", change.Status)
	}
	if change.Verdict != "" {
		fmt.Fprintf(&b, " verdict %s", change.Verdict)
	}
	if change.State != "" {
		fmt.Fprintf(&b, " slot %s %s", change.SlotID, change.State)
		if change.Detail != "" {
			fmt.Fprintf(&b, ": %s", change.Detail)
		}
	}
	return b.String()
}
