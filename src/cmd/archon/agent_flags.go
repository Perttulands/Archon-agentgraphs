package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Perttulands/Archon-agentgraphs/internal/formations"
)

// agent new and agent edit take the same flags offline and with --server.

const (
	agentNewUsage  = "usage: archon agent new <id> [--kind <kind>] [--harness <h>] [--capable a,b] [--personality p] [--from <path>] [--json]\nA role carries no model or effort; each slot that uses it sets them (archon formation assign)."
	agentEditUsage = "usage: archon agent edit <id> [--display-name n] [--kind k] [--summary s] [--capable a,b] [--session-stem s] [--harness h] [--model m] [--effort e] [--launch command] [--add-capability t|--remove-capability t|--add-harness h|--note text] [--json]"
)

type agentNewFlags struct {
	kind, harness, model, effort, capable, personality, from *string
	jsonOut                                                  *bool
}

func newAgentNewFlags(fs *flag.FlagSet, stderr io.Writer) agentNewFlags {
	flags := agentNewFlags{
		kind:        fs.String("kind", "", "role kind (default specialist)"),
		harness:     fs.String("harness", "", "default harness: claude-code, openai-codex or hermes (default claude-code, or inferred from --from); hermes takes no model or effort"),
		capable:     fs.String("capable", "", "comma-separated bare capabilities"),
		personality: fs.String("personality", "", "personality facet"),
		from:        fs.String("from", "", "source config path"),
		jsonOut:     fs.Bool("json", false, "write JSON"),
	}
	// A new role carries no model or effort; the flags remain only to refuse
	// them with directions to the slot.
	flags.model = fs.String("model", "", "refused: set the model on each slot (archon formation assign --model)")
	flags.effort = fs.String("effort", "", "refused: set the effort on each slot (archon formation assign --effort)")
	agentUsage(fs, stderr, agentNewUsage)
	return flags
}

// refusedSettings reports, and refuses, a model or effort given to a new role.
func (f agentNewFlags) refusedSettings(stderr io.Writer) bool {
	if err := formations.RefuseRoleSettings(*f.model, *f.effort); err != nil {
		fmt.Fprintln(stderr, err)
		return true
	}
	return false
}

type agentEditFlags struct {
	addCapability, removeCapability, addHarness, sessionStem, launch  *string
	displayName, kind, summary, capable, note, harness, model, effort *string
	jsonOut                                                           *bool
}

func newAgentEditFlags(fs *flag.FlagSet, stderr io.Writer) agentEditFlags {
	flags := agentEditFlags{
		addCapability:    fs.String("add-capability", "", "add bare capability"),
		removeCapability: fs.String("remove-capability", "", "remove bare capability"),
		addHarness:       fs.String("add-harness", "", "add a harness variant; --model, --effort, --session-stem and --launch then set it"),
		sessionStem:      fs.String("session-stem", "", "default or added-harness session stem"),
		launch:           fs.String("launch", "", "legacy launch command, kept only for harnesses Archon cannot start (such as hermes); claude-code and openai-codex seats ignore it"),
		displayName:      fs.String("display-name", "", "replace display name"),
		kind:             fs.String("kind", "", "replace role kind"),
		summary:          fs.String("summary", "", "replace summary"),
		capable:          fs.String("capable", "", "replace comma-separated bare capabilities"),
		note:             fs.String("note", "", "append note"),
		harness:          fs.String("harness", "", "harness variant that --model and --effort change (default the card's default harness)"),
		jsonOut:          fs.Bool("json", false, "write JSON"),
	}
	flags.model, flags.effort = agentSettingsFlags(fs)
	agentUsage(fs, stderr, agentEditUsage)
	return flags
}

func agentSettingsFlags(fs *flag.FlagSet) (model, effort *string) {
	model = fs.String("model", "", "model the harness runs; blank means the harness default model")
	efforts := []string{}
	for _, harness := range formations.LaunchableHarnesses() {
		efforts = append(efforts, harness.ID+": "+strings.Join(harness.Efforts, ", "))
	}
	effort = fs.String("effort", "", "reasoning effort; blank means "+formations.DefaultHarnessEffort+" ("+strings.Join(efforts, "; ")+")")
	return model, effort
}

func agentUsage(fs *flag.FlagSet, stderr io.Writer, usage string) {
	fs.Usage = func() {
		fmt.Fprintln(stderr, usage)
		fmt.Fprintln(stderr, "Seats start from the harness, model and effort; a launch string is not used for claude-code or openai-codex.")
		fs.PrintDefaults()
	}
}

// checkEditHarness refuses an --harness that would otherwise be ignored: it
// only picks the variant --model and --effort change.
func checkEditHarness(fs *flag.FlagSet, f agentEditFlags, stderr io.Writer) bool {
	given := givenFlags(fs)
	if !given["harness"] {
		return true
	}
	switch {
	case *f.addHarness != "":
		fmt.Fprintln(stderr, "--harness picks the variant --model and --effort change; with --add-harness they already set the added variant, so drop --harness")
	case !given["model"] && !given["effort"]:
		fmt.Fprintln(stderr, "--harness picks the variant --model and --effort change; give --model or --effort with it")
	default:
		return true
	}
	return false
}
