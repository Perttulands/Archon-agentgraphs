package formations

import (
	"fmt"
	"strings"
)

func renderBriefAndInputs(b *strings.Builder, req FormationExecution, card PersonaCard) {
	switch {
	case req.JudgeGateID != "" && len(req.PickAttempts) > 0 && req.FinalJudge:
		b.WriteString(pickContract(req.PickAttempts))
	case req.JudgeGateID != "" && len(req.PickAttempts) > 0:
		b.WriteString(recommendContract(req.PickAttempts))
	case req.JudgeGateID != "":
		b.WriteString(judgeContract)
	}
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
	kept := false
	for _, input := range req.Inputs {
		// Work that reached the port with a send-back is shown with it, and
		// every feedback and response the port kept follows (archon-o7p.11).
		if input.Text != "" && (input.Feedback == nil || input.Text != input.Feedback.OriginalText) {
			b.WriteString("input: " + input.Text + "\n")
		}
		// A file or folder output arrives as the path of its kept copy.
		if keptKind(input.Kind) && input.Ref != "" {
			b.WriteString("input " + input.Kind + ": " + input.Ref + "\n")
			kept = true
		}
		for feedback := input.Feedback; feedback != nil; feedback = feedback.Earlier {
			fmt.Fprintf(b, "\ngate feedback from %s, attempt %d:\nverdict: %s\nreason: %s\n", feedback.GateID, feedback.GateAttempt, feedback.Verdict, feedback.Reason)
			for _, evidence := range feedback.Evidence {
				b.WriteString("evidence: " + evidence.Text + "\n")
			}
			b.WriteString("original input ref: " + feedback.OriginalRef + "\n")
			b.WriteString("original input: " + feedback.OriginalText + "\n\n")
			kept = kept || keptKind(feedback.OriginalKind)
		}
		for response := input.Response; response != nil; response = response.Earlier {
			fmt.Fprintf(b, "\nhuman response from %s, attempt %d:\nverdict: pass\ndecided by: %s\nresponse:\n%s\n", response.GateID, response.GateAttempt, response.DecidedBy, response.Text)
			b.WriteString("approved input ref: " + response.OriginalRef + "\n")
			b.WriteString("approved input: " + response.OriginalText + "\n\n")
			kept = kept || keptKind(response.OriginalKind)
		}
	}
	if kept {
		b.WriteString("Input paths are kept copies of earlier work and are read-only; write new versions in your own attempt directory.\n")
	}
	// Every seat of the attempt, an orchestrated worker included, writes in
	// the attempt's own directory (archon-5tg1).
	if req.ArtifactDir != "" {
		b.WriteString("attempt artifact directory: " + req.ArtifactDir + "\n")
		b.WriteString("Keep concise selected evidence there. Use the project workspace for scratch, renders, caches, builds and large deliverables. Ordinary runs have a 32 MiB total output snapshot budget; a larger file/folder output blocks acceptance until narrowed or this run is explicitly archived. A text output may return a project path without copying it. Create a file before naming it as a ref in archon-outputs.\n")
	}
}

// keptKind reports whether a delivery of this kind is the path of a kept
// file or folder output.
func keptKind(kind string) bool {
	return kind == PortKindFile || kind == PortKindFolder
}
