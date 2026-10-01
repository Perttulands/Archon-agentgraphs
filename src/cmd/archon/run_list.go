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
// full projection.

// runListLine is one run as run list prints it.
type runListLine struct {
	RunID     string `json:"runId"`
	Mission   string `json:"missionSlug"`
	Status    string `json:"status"`
	BeadID    string `json:"beadId"`
	StartedAt string `json:"startedAt"`
	UpdatedAt string `json:"updatedAt"`
}

func runListLines(runs []formations.RunStatusProjection) []runListLine {
	lines := make([]runListLine, 0, len(runs))
	for _, run := range runs {
		lines = append(lines, runListLine{RunID: run.RunID, Mission: run.BoardSlug, Status: run.Status, BeadID: run.BeadID, StartedAt: run.StartedAt, UpdatedAt: run.UpdatedAt})
	}
	return lines
}

// writeRunList prints the runs, newest first.
func writeRunList(w io.Writer, runs []runListLine) {
	sort.SliceStable(runs, func(i, j int) bool { return listTime(runs[i].StartedAt).After(listTime(runs[j].StartedAt)) })
	for _, run := range runs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", run.RunID, run.Mission, run.Status, orDash(run.BeadID), listTimeWords(run.StartedAt), listTimeWords(run.UpdatedAt))
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

// listTimeWords is a ledger time to the second, in UTC.
func listTimeWords(value string) string {
	at := listTime(value)
	if at.IsZero() {
		return "-"
	}
	return at.UTC().Format(time.RFC3339)
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
