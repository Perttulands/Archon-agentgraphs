package coordinator

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Run wait (archon-o7p.6) is how the agent driving a run learns that the run
// needs it, ended or changed. Archon pushes nothing into outside sessions: the
// driver long-polls this route, usually through a background `archon run wait`.
// Every answer carries the ledger sequence to pass as `since` next, so a
// driver that loops never misses an event between calls or across restarts.

const (
	WaitUntilNeedsYou  = "needs-you"
	WaitUntilFinal     = "final"
	WaitUntilAnyChange = "any-change"

	WaitOutcomeFinal    = "final"
	WaitOutcomeNeedsYou = "needs-you"
	WaitOutcomeChanged  = "changed"
	// WaitOutcomePending means the hold ended first; ask again with the same since.
	WaitOutcomePending = "pending"

	waitDefaultHold = 30 * time.Second
	waitMaxHold     = 60 * time.Second
	// waitInputExcerptBytes bounds the gate input a wait carries; the whole
	// input is the gate request's.
	waitInputExcerptBytes = 4096
	waitChangesMax        = 50
)

// RunWait is one answer to a wait: the outcome, the cursor and the run as the
// driver must see it.
type RunWait struct {
	RunID     string `json:"runId"`
	BoardSlug string `json:"missionSlug"`
	// Mission is the run's mission title on its frozen board, or the board's.
	Mission string `json:"missionTitle"`
	Until   string `json:"until"`
	Outcome string `json:"outcome"`
	Since   int    `json:"since"`
	// Seq is the cursor: pass it as since to wait for what comes next.
	Seq           int    `json:"seq"`
	Status        string `json:"status"`
	Final         bool   `json:"final"`
	ResumeAllowed bool   `json:"resumeAllowed"`
	// Settled is false while a run command is still executing.
	Settled bool     `json:"settled"`
	End     *WaitEnd `json:"end,omitempty"`
	// Asks lists everything the run is waiting on the driver for, oldest first.
	Asks []WaitAsk `json:"asks"`
	// Changes are the events after since, up to Seq; the oldest are omitted
	// beyond fifty.
	Changes        []WaitChange `json:"changes"`
	ChangesOmitted int          `json:"changesOmitted,omitempty"`
}

// WaitEnd says how a final run ended and why.
type WaitEnd struct {
	Status          string     `json:"status"`
	Seq             int        `json:"seq"`
	Code            string     `json:"code,omitempty"`
	Reason          string     `json:"reason,omitempty"`
	ReasonTruncated bool       `json:"reasonTruncated,omitempty"`
	EndedBy         string     `json:"endedBy,omitempty"`
	Stopped         []WaitNode `json:"stopped,omitempty"`
}

type WaitNode struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// WaitAsk is one open ask: a human gate's verdict, a blocking escalation, or a
// block no gate or escalation explains.
type WaitAsk struct {
	Kind string `json:"kind"`
	Seq  int    `json:"seq"`
	// New is true when the ask opened after since.
	New       bool   `json:"new"`
	GateID    string `json:"gateId,omitempty"`
	NodeID    string `json:"nodeId,omitempty"`
	Title     string `json:"title"`
	Criterion string `json:"criterion,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Code      string `json:"code,omitempty"`
	Severity  string `json:"severity,omitempty"`
	// ResumeAllowed says whether run resume can continue a blocked ask.
	ResumeAllowed bool `json:"resumeAllowed,omitempty"`
	// Limit is the spent Limit card a blocked ask resumes past with a grant.
	Limit  *formations.RunLimitReached `json:"limit,omitempty"`
	Input  *WaitInput                  `json:"input,omitempty"`
	Routes []formations.GateRoute      `json:"routes,omitempty"`
}

// WaitInput is the start of what a human gate received.
type WaitInput struct {
	FromNodeID string `json:"fromNodeId,omitempty"`
	FromTitle  string `json:"fromTitle,omitempty"`
	FromPortID string `json:"fromPortId,omitempty"`
	Text       string `json:"text"`
	Bytes      int    `json:"bytes"`
	Truncated  bool   `json:"truncated"`
}

type WaitChange struct {
	Seq    int    `json:"seq"`
	Type   string `json:"type"`
	NodeID string `json:"nodeId,omitempty"`
	SlotID string `json:"slotId,omitempty"`
	// State and Detail describe a seat_state change: what the seat waits on.
	State   string `json:"state,omitempty"`
	Detail  string `json:"detail,omitempty"`
	GateID  string `json:"gateId,omitempty"`
	Title   string `json:"title,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
	Status  string `json:"status,omitempty"`
	Verdict string `json:"verdict,omitempty"`
}

// ValidWaitUntil reports whether until names a wait mode.
func ValidWaitUntil(until string) bool {
	return until == WaitUntilNeedsYou || until == WaitUntilFinal || until == WaitUntilAnyChange
}

// wait holds the request until the run satisfies until after since, the
// hold ends, or the daemon stops. Stopping answers 503 so the client
// reconnects to the next daemon with the same since.
func (c *Coordinator) wait(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("runId")
	query := r.URL.Query()
	until := query.Get("until")
	if until == "" {
		until = WaitUntilNeedsYou
	}
	if !ValidWaitUntil(until) {
		reply(w, 400, map[string]string{"error": "until must be needs-you, final or any-change"})
		return
	}
	since := 0
	if raw := query.Get("since"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			reply(w, 400, map[string]string{"error": "since must be a non-negative ledger sequence"})
			return
		}
		since = n
	}
	hold := waitDefaultHold
	if raw := query.Get("hold"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || time.Duration(n)*time.Second > waitMaxHold {
			reply(w, 400, map[string]string{"error": "hold must be 0 to 60 seconds"})
			return
		}
		hold = time.Duration(n) * time.Second
	}
	// An unknown run answers before it is subscribed to, so it allocates nothing.
	if _, err := c.store.ReadRunEvents(runID); err != nil {
		failure(w, err)
		return
	}
	timer := time.NewTimer(hold)
	defer timer.Stop()
	var board *formations.BoardDocument
	boardRead := false
	for {
		changed := c.nextChange(runID) // subscribe before reading, so no append is missed
		events, err := c.store.ReadRunEvents(runID)
		if err != nil {
			failure(w, err)
			return
		}
		if since > len(events) {
			reply(w, 400, map[string]string{"error": "since is past the run's last event " + strconv.Itoa(len(events))})
			return
		}
		if !boardRead {
			// Titles are a courtesy: an unreadable frozen board shows identifiers.
			board, _ = c.store.ReadRunBoard(runID)
			boardRead = true
		}
		c.mu.Lock()
		settled := !c.state(runID).busy
		c.mu.Unlock()
		result, err := projectWait(runID, events, board, until, since, settled, c.store.CurrentTime())
		if err != nil {
			failure(w, err)
			return
		}
		if result.Outcome != WaitOutcomePending {
			reply(w, 200, result)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-c.stopping:
			reply(w, 503, map[string]string{"error": "coordinator is stopping; wait again with the same since"})
			return
		case <-timer.C:
			reply(w, 200, result)
			return
		case <-changed:
		}
	}
}

// projectWait decides one wait from the ledger. A final run always answers.
// Human gates and blocking escalations are asks as soon as the ledger records
// them; a bare block is an ask only once the run has settled, since a verdict
// records one on its way to the automatic resume.
func projectWait(runID string, events []formations.RunEvent, board *formations.BoardDocument, until string, since int, settled bool, now time.Time) (*RunWait, error) {
	status, err := formations.ProjectRunEvents(runID, events)
	if err != nil {
		return nil, err
	}
	result := &RunWait{
		RunID: runID, BoardSlug: status.BoardSlug, Mission: waitMissionTitle(board, status), Until: until, Since: since,
		Seq: len(events), Status: project(status, events).Status, Final: status.Final, ResumeAllowed: status.ResumeAllowed,
		Settled: settled, Asks: []WaitAsk{}, Changes: []WaitChange{},
	}
	if status.Final {
		result.End = waitEnd(events, board, status)
	} else {
		asks, err := formations.ProjectSettledNeedsYouAsks(events)
		if err != nil {
			return nil, err
		}
		for _, ask := range asks {
			if ask.Kind == formations.NeedsYouKindBlocked && !settled {
				continue
			}
			result.Asks = append(result.Asks, waitAsk(ask, events, board, since, now))
		}
	}
	newAsk := false
	for _, ask := range result.Asks {
		newAsk = newAsk || ask.New
	}
	if !status.Final && !settled && status.Status == formations.RunStatusBlocked && len(result.Asks) == 0 {
		// A block recorded inside a command is either a verdict on its way to
		// the automatic resume or a real block the settle will announce. Until
		// the run settles the run is still executing, and in every mode the
		// cursor stops below the block, so the next wait still reports it as
		// new if it becomes an ask.
		result.Status = formations.RunStatusRunning
		for i := len(events) - 1; i >= 0 && events[i].Seq > since; i-- {
			if events[i].Type == formations.RunEventBlocked {
				result.Seq = events[i].Seq - 1
				break
			}
		}
	}
	switch {
	case status.Final:
		result.Outcome = WaitOutcomeFinal
	case newAsk && until != WaitUntilFinal:
		result.Outcome = WaitOutcomeNeedsYou
	case until == WaitUntilAnyChange && result.Seq > since:
		result.Outcome = WaitOutcomeChanged
	default:
		// Nothing was reported, so the cursor stays where the driver put it.
		result.Outcome = WaitOutcomePending
		result.Seq = since
		return result, nil
	}
	for _, event := range events {
		if event.Seq <= since || event.Seq > result.Seq {
			continue
		}
		change := WaitChange{Seq: event.Seq, Type: event.Type, NodeID: event.NodeID, SlotID: event.SlotID, GateID: event.GateID, Attempt: event.Attempt}
		if event.Type == formations.RunEventSeatState {
			change.State, _ = event.Data["state"].(string)
			change.Detail, _ = event.Data["detail"].(string)
		}
		change.Title = waitTitle(board, firstNonEmpty(event.NodeID, event.GateID))
		change.Status, _ = event.Data["status"].(string)
		change.Verdict, _ = event.Data["verdict"].(string)
		result.Changes = append(result.Changes, change)
	}
	if extra := len(result.Changes) - waitChangesMax; extra > 0 {
		result.Changes = result.Changes[extra:]
		result.ChangesOmitted = extra
	}
	return result, nil
}

func waitAsk(ask formations.NeedsYouAsk, events []formations.RunEvent, board *formations.BoardDocument, since int, now time.Time) WaitAsk {
	out := WaitAsk{Kind: ask.Kind, Seq: ask.Seq, New: ask.Seq > since, GateID: ask.GateID, NodeID: ask.NodeID, Severity: ask.Severity}
	out.Title = waitTitle(board, firstNonEmpty(ask.GateID, ask.NodeID))
	var event formations.RunEvent
	if ask.Seq >= 1 && ask.Seq <= len(events) {
		event = events[ask.Seq-1]
	}
	switch ask.Kind {
	case formations.NeedsYouKindHumanGate:
		out.Criterion = ask.Ask
		out.Severity = ""
		input, _ := event.Data["inputRef"].(map[string]any)
		from, _ := input["fromNodeId"].(string)
		port, _ := input["fromPortId"].(string)
		text, _ := input["text"].(string)
		excerpt, truncated := formations.CapEvidenceText(text, waitInputExcerptBytes)
		out.Input = &WaitInput{FromNodeID: from, FromTitle: waitTitle(board, from), FromPortID: port, Text: excerpt, Bytes: len(text), Truncated: truncated}
		if board != nil {
			out.Routes = formations.HumanGateRoutes(board, events, ask.GateID, now)
		}
	default:
		out.Reason = ask.Ask
		out.Code, _ = event.Data["code"].(string)
		out.ResumeAllowed = ask.Kind == formations.NeedsYouKindBlocked && ask.ResumeAllowed
		out.Limit = ask.Limit
	}
	return out
}

func waitEnd(events []formations.RunEvent, board *formations.BoardDocument, status *formations.RunStatusProjection) *WaitEnd {
	end := &WaitEnd{Status: status.Status, Seq: len(events), EndedBy: status.EndedBy}
	problem := formations.RunEndProblem(events)
	if problem == nil {
		return end
	}
	end.Seq, end.Code, end.Reason, end.ReasonTruncated = problem.Seq, problem.Code, problem.Reason.Text, problem.Reason.Truncated
	if problem.Actor != "" {
		end.EndedBy = problem.Actor
	}
	for _, id := range problem.NodeIDs {
		end.Stopped = append(end.Stopped, WaitNode{ID: id, Title: waitTitle(board, id)})
	}
	return end
}

func waitMissionTitle(board *formations.BoardDocument, status *formations.RunStatusProjection) string {
	if board != nil && board.Title != "" {
		return board.Title
	}
	return status.BoardSlug
}

// waitTitle is a node's title on the run's frozen board, or its ID.
func waitTitle(board *formations.BoardDocument, id string) string {
	if id == "" {
		return ""
	}
	return nodeTitle(board, id)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
