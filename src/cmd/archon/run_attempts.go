package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// Run attempts (archon-5tg1): every attempt of every step, in order, with its
// outputs, the verdicts that judged them and its directory's files.

func runAttempts(store *formations.Store, args []string, stdout, stderr io.Writer) int {
	fs := commandFlags("run attempts", stderr)
	nodeID := fs.String("node", "", "only this step's attempts")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("run attempts"))
		return 2
	}
	evidence, err := store.ProjectRunAttempts(fs.Arg(0), *nodeID)
	if err != nil {
		return failJSON(stderr, err, *jsonOut, "run", fs.Arg(0))
	}
	if *jsonOut {
		return writeJSON(stdout, evidence)
	}
	writeRunAttempts(stdout, evidence)
	return 0
}

func remoteRunAttempts(c *remoteClient, args []string, stdout, stderr io.Writer) int {
	fs := remoteFlags("run attempts", stderr)
	nodeID := fs.String("node", "", "only this step's attempts")
	jsonOut := fs.Bool("json", false, "write JSON")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"json": true})); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, commandUsage("run attempts"))
		return 2
	}
	path := "/api/runs/" + url.PathEscape(fs.Arg(0)) + "/evidence/attempts"
	if *nodeID != "" {
		path += "?node=" + url.QueryEscape(*nodeID)
	}
	raw, err := c.raw("GET", path, nil)
	if err != nil {
		return fail(stderr, err)
	}
	if *jsonOut {
		fmt.Fprint(stdout, string(raw))
		return 0
	}
	var envelope struct {
		Data formations.RunAttemptsEvidence `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fail(stderr, err)
	}
	writeRunAttempts(stdout, &envelope.Data)
	return 0
}

// writeRunAttempts prints each step, then each of its attempts with what it
// output, how gates judged it and the files it left.
func writeRunAttempts(stdout io.Writer, evidence *formations.RunAttemptsEvidence) {
	if len(evidence.Steps) == 0 {
		fmt.Fprintln(stdout, "No attempts.")
		return
	}
	for _, step := range evidence.Steps {
		line := fmt.Sprintf("step %s", step.NodeID)
		if step.Title != "" {
			line += fmt.Sprintf(" %q", step.Title)
		}
		if len(step.JudgeOf) > 0 {
			line += " judging " + strings.Join(step.JudgeOf, ", ")
		}
		fmt.Fprintln(stdout, line)
		for _, attempt := range step.Attempts {
			line := fmt.Sprintf("  attempt %d %s", attempt.Attempt, attempt.Status)
			if attempt.StartedAt != "" {
				line += " from " + attempt.StartedAt
			}
			if attempt.EndedAt != "" {
				line += " to " + attempt.EndedAt
			}
			fmt.Fprintln(stdout, line)
			if attempt.Directory != nil {
				fmt.Fprintf(stdout, "    directory %s\n", attempt.Directory.Path)
			}
			for _, output := range attempt.Outputs {
				fmt.Fprintf(stdout, "    output %s %s %s\n", attemptPortName(output), output.Kind, attemptOutputValue(output))
			}
			for _, verdict := range attempt.Verdicts {
				gate := verdict.GateID
				if verdict.GateTitle != "" {
					gate = fmt.Sprintf("%q", verdict.GateTitle)
				}
				outcome := verdict.Verdict
				if verdict.Pending {
					outcome = "pending"
				}
				line := fmt.Sprintf("    verdict %s %s", gate, outcome)
				if reason := firstLine(verdict.Reason.Text); reason != "" {
					line += ": " + reason
				}
				fmt.Fprintln(stdout, line)
			}
			for _, file := range attempt.Files {
				fmt.Fprintf(stdout, "    file %s %d bytes %s\n", file.Name, file.Size, file.Kind)
			}
			if attempt.FilesTruncated {
				fmt.Fprintln(stdout, "    more files are not listed")
			}
		}
	}
}

func attemptPortName(output formations.AttemptOutput) string {
	if output.Label != "" {
		return fmt.Sprintf("%q", output.Label)
	}
	return output.PortID
}

// attemptOutputValue is a file or folder output's path, or the first line of
// a text output.
func attemptOutputValue(output formations.AttemptOutput) string {
	if output.Kind != formations.PortKindText && output.Path != "" {
		return output.Path
	}
	text := firstLine(output.Text.Text)
	if text != strings.TrimSpace(output.Text.Text) || output.Text.Truncated {
		text += " [...]"
	}
	return text
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(line)
}
