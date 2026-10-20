package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// run list prints one line per run, newest first: run ID, mission, status,
// Bead, started and updated, tab-separated (archon-lmg2). --json prints the
// list payload (compact summaries when reading through the daemon).

// runListLine is one run as run list prints it.
type runListLine struct {
	RunID     string `json:"runId"`
	Mission   string `json:"missionSlug"`
	Status    string `json:"status"`
	StartedBy string `json:"startedBy"`
	BeadID    string `json:"beadId"`
	StartedAt string `json:"startedAt"`
	UpdatedAt string `json:"updatedAt"`
}

func runListLines(runs []formations.RunStatusProjection) []runListLine {
	lines := make([]runListLine, 0, len(runs))
	for _, run := range runs {
		lines = append(lines, runListLine{RunID: run.RunID, Mission: run.BoardSlug, Status: run.Status, StartedBy: run.StartedBy, BeadID: run.BeadID, StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt})
	}
	return lines
}

// writeRunList prints the runs, newest first.
func writeRunList(w io.Writer, runs []runListLine) {
	sort.SliceStable(runs, func(i, j int) bool { return listTime(runs[i].StartedAt).After(listTime(runs[j].StartedAt)) })
	for _, run := range runs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", run.RunID, run.Mission, run.Status, orDash(run.BeadID), listTimeWords(run.StartedAt), listTimeWords(run.UpdatedAt), orDash(run.StartedBy))
	}
}

// writeRemoteRunList prints the daemon's run list response as run list lines.
func writeRemoteRunList(raw []byte, stdout, stderr io.Writer) int {
	var body struct {
		Data []runListLine `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return fail(stderr, err)
	}
	writeRunList(stdout, body.Data)
	return 0
}

func listTime(value string) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, value)
	return at
}

// listTimeWords is a ledger time to the second in the operator’s local time.
func listTimeWords(value string) string {
	at := listTime(value)
	if at.IsZero() {
		return "-"
	}
	return at.Local().Format("2006-01-02 15:04:05 MST")
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// runDuration formats elapsed run or attempt time without ledger precision.
func runDuration(start, end string, finished bool) string {
	from := listTime(start)
	if from.IsZero() {
		return "-"
	}
	to := listTime(end)
	if !finished {
		to = time.Now()
	}
	if to.IsZero() || to.Before(from) {
		return "-"
	}
	return to.Sub(from).Round(time.Second).String()
}
