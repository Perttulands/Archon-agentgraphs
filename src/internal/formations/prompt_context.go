package formations

import (
	"fmt"
	"strings"
)

func renderBriefAndInputs(b *strings.Builder, req FormationExecution, card PersonaCard) {
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
	}
}
