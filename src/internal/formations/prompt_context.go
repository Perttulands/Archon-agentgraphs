package formations

import (
	"fmt"
	"strings"
)

func renderBriefAndInputs(b *strings.Builder, req FormationExecution, card PersonaCard) {
	if req.Cwd != "" {
		b.WriteString("run cwd: " + req.Cwd + "\n")
	}
	if len(req.ContextPaths) > 0 {
		b.WriteString("\nLaunch context: inspect these files or directories before describing existing work or claiming there is no prior art. Treat them as reference inputs; write outputs in the run workspace unless the mission explicitly authorizes changes elsewhere. Report any unreadable context.\n")
		for _, path := range req.ContextPaths {
			fmt.Fprintf(b, "context path: %q\n", path)
		}
		b.WriteString("\n")
	}
	if req.MissionGoal != "" {
		b.WriteString("mission goal: " + req.MissionGoal + "\n")
	}
	if card.Summary != "" {
		b.WriteString("persona summary: " + card.Summary + "\n")
	}
	if req.MissionBeadID != "" {
		b.WriteString("mission bead: " + req.MissionBeadID + "\n")
	}
	b.WriteString("brief: " + req.Brief.Goal + "\n")
	if req.Brief.BeadID != "" {
		b.WriteString("brief bead: " + req.Brief.BeadID + "\n")
	}
	for _, file := range req.Brief.Files {
		b.WriteString("brief file: " + file + "\n")
	}
	for _, link := range req.Brief.Links {
		b.WriteString("brief link: " + link + "\n")
	}
	for _, input := range req.Inputs {
		if feedback := input.Feedback; feedback != nil {
			fmt.Fprintf(b, "\ngate feedback from %s, attempt %d:\nverdict: %s\nreason: %s\n", feedback.GateID, feedback.GateAttempt, feedback.Verdict, feedback.Reason)
			for _, evidence := range feedback.Evidence {
				b.WriteString("evidence: " + evidence.Text + "\n")
			}
			b.WriteString("original input ref: " + feedback.OriginalRef + "\n")
			b.WriteString("original input: " + feedback.OriginalText + "\n\n")
		} else if input.Text != "" {
			b.WriteString("input: " + input.Text + "\n")
		}
		if response := input.Response; response != nil {
			fmt.Fprintf(b, "\nhuman response from %s, attempt %d:\nverdict: pass\ndecided by: %s\nresponse:\n%s\n\n", response.GateID, response.GateAttempt, response.DecidedBy, response.Text)
		}
	}
}
