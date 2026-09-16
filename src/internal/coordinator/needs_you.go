package coordinator

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Needs-you notifications tell the operator when a settled run needs them. The
// coordinator decides when a run has settled: its command worker has exited,
// so a block recorded mid-command (a human verdict awaiting its automatic
// resume) is never announced. Each ask is sent once and marked in the run's
// .needs-you.json artifact; a failed send stays unmarked and is retried.

const defaultNeedsYouRetryInterval = 5 * time.Minute

// NeedsYouConfig enables notifications for the runs this coordinator owns.
type NeedsYouConfig struct {
	Notifier formations.NeedsYouNotifier
	// CockpitURL is the optional base for links back to the cockpit.
	CockpitURL string
	// ServerURL is the coordinator address used in the CLI commands a message
	// suggests.
	ServerURL string
	// RetryInterval paces the periodic retry; it defaults to five minutes.
	RetryInterval time.Duration
}

type needsYouDispatcher struct {
	c       *Coordinator
	config  NeedsYouConfig
	mu      sync.Mutex
	pending map[string]bool
	// live names runs that settled in this process. Only they may announce a
	// final outcome, so enabling notifications never mails old finished runs.
	live map[string]bool
	wake chan struct{}
	// done closes when the dispatcher has stopped, before shutdown releases
	// the writer lock.
	done chan struct{}
}

// EnableNeedsYou starts notifications. Call it once, after startup recovery:
// it first reconciles every non-final run, then follows settled commands and
// retries undelivered asks periodically. It never blocks runs or shutdown.
func (c *Coordinator) EnableNeedsYou(config NeedsYouConfig) {
	if config.Notifier == nil {
		return
	}
	if config.RetryInterval <= 0 {
		config.RetryInterval = defaultNeedsYouRetryInterval
	}
	d := &needsYouDispatcher{c: c, config: config, pending: map[string]bool{}, live: map[string]bool{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	c.mu.Lock()
	if c.closed || c.needsYou != nil {
		c.mu.Unlock()
		return
	}
	c.needsYou = d
	c.mu.Unlock()
	go d.run()
}

// settled queues a run whose command worker has just exited.
func (d *needsYouDispatcher) settled(runID string) {
	d.mu.Lock()
	d.pending[runID] = true
	d.live[runID] = true
	d.mu.Unlock()
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *needsYouDispatcher) run() {
	defer close(d.done)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-d.c.stopping:
			cancel()
		case <-ctx.Done():
		}
	}()
	d.queueNonFinal()
	ticker := time.NewTicker(d.config.RetryInterval)
	defer ticker.Stop()
	for {
		d.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case <-ticker.C:
			d.queueNonFinal()
		}
	}
}

// queueNonFinal queues every non-final run and every live run still owing a
// message. Final runs from before this process are never queued.
func (d *needsYouDispatcher) queueNonFinal() {
	runs, err := d.c.store.ListRuns(formations.RunListFilter{})
	if err != nil {
		log.Printf("needs-you: list runs: %v", err)
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, run := range runs {
		if !run.Final {
			d.pending[run.RunID] = true
		}
	}
	for runID := range d.live {
		d.pending[runID] = true
	}
}

func (d *needsYouDispatcher) drain(ctx context.Context) {
	for ctx.Err() == nil {
		d.mu.Lock()
		runID := ""
		for candidate := range d.pending {
			runID = candidate
			break
		}
		if runID == "" {
			d.mu.Unlock()
			return
		}
		delete(d.pending, runID)
		live := d.live[runID]
		d.mu.Unlock()
		if !d.deliver(ctx, runID, live) {
			d.mu.Lock()
			delete(d.live, runID)
			d.mu.Unlock()
		}
	}
}

// deliver sends the settled run's undelivered asks. It reports whether the run
// still owes a message, so a live run stays eligible for retry.
func (d *needsYouDispatcher) deliver(ctx context.Context, runID string, live bool) bool {
	c := d.c
	c.mu.Lock()
	state := c.state(runID)
	busy, changed := state.busy, state.changed
	c.mu.Unlock()
	if busy {
		return false // the command's release queues the run again
	}
	events, err := c.store.ReadRunEvents(runID)
	if err != nil {
		log.Printf("needs-you: run %s: %v", runID, err)
		return live
	}
	asks, err := formations.ProjectSettledNeedsYouAsks(events)
	if err != nil {
		log.Printf("needs-you: run %s: %v", runID, err)
		return live
	}
	status, err := formations.ProjectRunEvents(runID, events)
	if err != nil {
		log.Printf("needs-you: run %s: %v", runID, err)
		return live
	}
	notified, err := c.store.NeedsYouNotifiedSeqs(runID)
	if err != nil {
		log.Printf("needs-you: run %s: %v", runID, err)
		return live
	}
	board, err := c.store.ReadRunBoard(runID)
	if err != nil {
		board = nil // titles fall back to identifiers
	}
	runStatus := project(status, events).Status
	owing := false
	for _, ask := range asks {
		if notified[ask.Seq] || ask.Kind == formations.NeedsYouKindFinal && !live {
			continue
		}
		select {
		case <-changed:
			return true // the run moved on; its next settle or retry decides again
		default:
		}
		notification := renderNeedsYou(d.config, ask, events, runStatus, board)
		if err := d.config.Notifier.NotifyNeedsYou(ctx, notification); err != nil {
			log.Printf("needs-you: run %s ask %d (%s) not delivered: %v", runID, ask.Seq, ask.Kind, err)
			owing = true
			continue
		}
		if err := c.store.MarkNeedsYouNotified(runID, ask.Seq); err != nil {
			log.Printf("needs-you: run %s ask %d delivered but not recorded: %v", runID, ask.Seq, err)
			owing = true
		}
	}
	return owing
}

// renderNeedsYou writes the complete plain-text message for one ask.
func renderNeedsYou(config NeedsYouConfig, ask formations.NeedsYouAsk, events []formations.RunEvent, runStatus string, board *formations.BoardDocument) formations.NeedsYouNotification {
	slug, _ := events[0].Data["boardSlug"].(string)
	n := formations.NeedsYouNotification{
		RunID: ask.RunID, BoardSlug: slug, BoardTitle: slug, Seq: ask.Seq, Kind: ask.Kind, RunStatus: runStatus,
		NodeID: ask.NodeID, GateID: ask.GateID, Ask: ask.Ask, Severity: ask.Severity, Blocks: ask.Blocks,
		BoardURL: cockpitLink(config.CockpitURL, slug, ask.RunID),
	}
	if board != nil && board.Title != "" {
		n.BoardTitle = board.Title
	}
	server := config.ServerURL
	if server == "" {
		server = `"$FORM_SERVER"`
	}
	var body strings.Builder
	switch ask.Kind {
	case formations.NeedsYouKindHumanGate:
		n.GateTitle = nodeTitle(board, ask.GateID)
		n.Subject = fmt.Sprintf("Archon needs your answer: %s (%s)", n.GateTitle, n.BoardTitle)
		fmt.Fprintf(&body, "%s is waiting for your answer at the gate %q.\n\n", n.BoardTitle, n.GateTitle)
		var request formations.RunEvent
		if ask.Seq >= 1 && ask.Seq <= len(events) {
			request = events[ask.Seq-1]
		}
		if criterion, _ := request.Data["prompt"].(string); criterion != "" {
			fmt.Fprintf(&body, "Gate criterion:\n%s\n\n", criterion)
		}
		input, _ := request.Data["inputRef"].(map[string]any)
		from, _ := input["fromNodeId"].(string)
		text, _ := input["text"].(string)
		text, truncated := capPendingGateText(text)
		fmt.Fprintf(&body, "The gate received this from %s:\n\n%s\n", nodeTitle(board, from), text)
		if truncated {
			body.WriteString("\n[The input is longer; this is its first 64 KiB.]\n")
		}
		body.WriteString("\n")
		if n.BoardURL != "" {
			fmt.Fprintf(&body, "Answer in the cockpit: %s\n\n", n.BoardURL)
		}
		body.WriteString("Or answer from the Archon host. Approve sends your response to the next step; reject sends it back as feedback:\n")
		fmt.Fprintf(&body, "archon --server %s gate approve %s %s --requested-seq %d --response 'your answer'\n", server, ask.RunID, ask.GateID, ask.Seq)
		fmt.Fprintf(&body, "archon --server %s gate reject %s %s --requested-seq %d --response 'what to change'\n", server, ask.RunID, ask.GateID, ask.Seq)
	case formations.NeedsYouKindEscalation:
		where := nodeTitle(board, ask.NodeID)
		n.Subject = fmt.Sprintf("Archon needs you: %s escalated (%s)", where, n.BoardTitle)
		fmt.Fprintf(&body, "%s on %s raised a %s escalation:\n%s\n\n", where, n.BoardTitle, ask.Severity, ask.Ask)
		body.WriteString("The run stays blocked until you resolve it and resume:\n")
		fmt.Fprintf(&body, "archon --server %s run resume %s --reason 'how it was resolved'\n", server, ask.RunID)
		writeCockpitLink(&body, n.BoardURL)
	case formations.NeedsYouKindBlocked:
		n.Subject = fmt.Sprintf("Archon run blocked: %s", n.BoardTitle)
		reason := ask.Ask
		if reason == "" {
			reason = "not recorded"
		}
		fmt.Fprintf(&body, "Run %s on %s is blocked.\n\nReason: %s\n\n", ask.RunID, n.BoardTitle, reason)
		if ask.ResumeAllowed {
			body.WriteString("Resume it after resolving the cause:\n")
			fmt.Fprintf(&body, "archon --server %s run resume %s --reason 'what you resolved'\n", server, ask.RunID)
		} else {
			body.WriteString("It cannot resume. Inspect the run and start a new one.\n")
		}
		writeCockpitLink(&body, n.BoardURL)
	case formations.NeedsYouKindFinal:
		n.Subject = fmt.Sprintf("Archon run %s: %s", ask.Status, n.BoardTitle)
		fmt.Fprintf(&body, "Run %s on %s finished: %s.\n", ask.RunID, n.BoardTitle, ask.Status)
		if ask.Status != formations.RunStatusSucceeded && ask.Ask != "" {
			fmt.Fprintf(&body, "Reason: %s\n", ask.Ask)
		}
		writeCockpitLink(&body, n.BoardURL)
	}
	fmt.Fprintf(&body, "\nRun: %s\nBoard: %s (%s)\nStatus: %s\n", ask.RunID, n.BoardTitle, slug, runStatus)
	n.Body = body.String()
	n.Text = n.Subject
	return n
}

func writeCockpitLink(body *strings.Builder, link string) {
	if link != "" {
		fmt.Fprintf(body, "\nOpen the cockpit: %s\n", link)
	}
}

func cockpitLink(base, slug, runID string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	query := url.Values{}
	if slug != "" {
		query.Set("board", slug)
	}
	query.Set("run", runID)
	return base + "/?" + query.Encode()
}

func nodeTitle(board *formations.BoardDocument, id string) string {
	if board != nil {
		for _, node := range board.Formations {
			if node.ID == id && node.Title != "" {
				return node.Title
			}
		}
		for _, node := range board.Gates {
			if node.ID == id && node.Title != "" {
				return node.Title
			}
		}
		for _, node := range board.Missions {
			if node.ID == id && node.Title != "" {
				return node.Title
			}
		}
	}
	if id == "" {
		return "the upstream step"
	}
	return id
}
