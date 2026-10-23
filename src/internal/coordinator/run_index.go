package coordinator

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// RunSummary is the discovery index. Full inputs, outputs, pending requests and
// event history belong to a selected run's detail projection, never this list.
type RunSummary struct {
	RunID       string `json:"runId"`
	Status      string `json:"status"`
	Final       bool   `json:"final"`
	MissionSlug string `json:"missionSlug"`
	MissionID   string `json:"missionId"`
	MissionRev  int    `json:"missionRev"`
	// Number counts the mission's runs in start order, from 1 (formations.NumberRuns).
	Number        int                 `json:"number"`
	InputCardID   string              `json:"inputCardId"`
	BeadID        string              `json:"beadId"`
	EventCount    int                 `json:"eventCount"`
	StartedBy     string              `json:"startedBy,omitempty"`
	StartedAt     string              `json:"startedAt,omitempty"`
	UpdatedAt     string              `json:"updatedAt,omitempty"`
	ResumeAllowed bool                `json:"resumeAllowed"`
	ResumePolicy  string              `json:"resumePolicy,omitempty"`
	BlockCode     string              `json:"blockCode,omitempty"`
	InputExcerpt  string              `json:"inputExcerpt"`
	Sessions      []RunSessionSummary `json:"sessions"`
}

type RunSessionSummary struct {
	SessionName string `json:"sessionName"`
	NodeID      string `json:"nodeId"`
	SlotID      string `json:"slotId"`
}

func (c *Coordinator) listRuns(w http.ResponseWriter, r *http.Request) {
	filter := formations.RunListFilter{}
	if mission := r.URL.Query().Get("mission"); mission != "" {
		// Mission identity, rather than a reused slug, owns its run history.
		filter.MissionID = "-"
		if slug, err := c.store.ResolveBoardSelector(mission); err == nil {
			if board, err := c.store.ReadBoard(slug); err == nil {
				filter.MissionID = board.ID
			}
		}
	}
	views, _, err := c.store.ListRunLedgerViews(filter)
	if err != nil {
		failure(w, err)
		return
	}
	summaries := make([]RunSummary, 0, len(views))
	for _, view := range views {
		summary := summarizeRun(view.Status, view.Events)
		if r.URL.Query().Get("needs") == "you" && (summary.Final || summary.Status != formations.RunStatusBlocked && summary.Status != "waiting_human") {
			continue
		}
		summaries = append(summaries, summary)
	}
	reply(w, http.StatusOK, summaries)
}

func summarizeRun(status *formations.RunStatusProjection, events []formations.RunEvent) RunSummary {
	summary := RunSummary{
		RunID: status.RunID, Status: status.Status, Final: status.Final,
		MissionSlug: status.BoardSlug, MissionID: status.BoardID, MissionRev: status.BoardRev, Number: status.Number,
		InputCardID: status.MissionID, BeadID: status.BeadID, EventCount: status.EventCount,
		StartedBy: status.StartedBy, StartedAt: status.StartedAt, UpdatedAt: status.UpdatedAt,
		ResumeAllowed: status.ResumeAllowed, ResumePolicy: status.ResumePolicy, BlockCode: status.BlockCode,
		InputExcerpt: runInputExcerpt(status.Inputs), Sessions: []RunSessionSummary{},
	}
	if status.Final {
		return summary
	}
	if status.Status != formations.RunStatusBlocked && len(formations.OpenHumanRequests(events)) > 0 {
		summary.Status = "waiting_human"
	}
	// Preserve the navigator's creation order. A kept-on-call cleanup keeps the
	// session; another cleanup removes it, and a later creation adds it anew.
	sessions := map[string]RunSessionSummary{}
	order := []string{}
	for _, event := range events {
		name, _ := event.Data["sessionName"].(string)
		switch event.Type {
		case formations.RunEventSeatCreated:
			if name == "" || event.NodeID == "" {
				continue
			}
			if _, exists := sessions[name]; !exists {
				order = append(order, name)
			}
			sessions[name] = RunSessionSummary{SessionName: name, NodeID: event.NodeID, SlotID: event.SlotID}
		case formations.RunEventSeatCleanup:
			if outcome, _ := event.Data["outcome"].(string); outcome != "kept_on_call" {
				delete(sessions, name)
				order = slices.DeleteFunc(order, func(value string) bool { return value == name })
			}
		}
	}
	for _, name := range order {
		summary.Sessions = append(summary.Sessions, sessions[name])
	}
	return summary
}

func runInputExcerpt(inputs []formations.RunInput) string {
	if len(inputs) == 0 {
		return ""
	}
	first := inputs[0]
	for _, input := range inputs {
		if input.Kind == formations.PortKindText {
			first = input
			break
		}
	}
	words := strings.Join(strings.Fields(first.Value), " ")
	chars := []rune(words)
	if len(chars) > 90 {
		return string(chars[:89]) + "…"
	}
	return words
}
