package main

import (
	"flag"
	"fmt"
	"io"
)

// agent new and agent edit take the same flags offline and with --server.

const (
	agentNewUsage  = "usage: archon agent new <id> [--kind <kind>] [--harness <h>] [--capable a,b] [--personality p] [--from <path>] [--json]\nA role is role text; each slot that uses it states its own harness, model and effort (archon formation assign)."
	agentEditUsage = "usage: archon agent edit <id> [--display-name n] [--kind k] [--summary s] [--capable a,b] [--session-stem s] [--add-capability t|--remove-capability t|--add-harness h|--note text] [--json]"
)

type agentNewFlags struct {
	kind, harness, capable, personality, from *string
	jsonOut                                   *bool
}

func newAgentNewFlags(fs *flag.FlagSet, stderr io.Writer) agentNewFlags {
	flags := agentNewFlags{
		kind:        fs.String("kind", "", "role kind (default specialist)"),
		harness:     fs.String("harness", "", "harness of the role's own session (agent spawn): claude-code or openai-codex (default claude-code, or inferred from --from)"),
		capable:     fs.String("capable", "", "comma-separated bare capabilities"),
		personality: fs.String("personality", "", "personality facet"),
		from:        fs.String("from", "", "source config path"),
		jsonOut:     fs.Bool("json", false, "write JSON"),
	}
	agentUsage(fs, stderr, agentNewUsage)
	return flags
}

type agentEditFlags struct {
	addCapability, removeCapability, addHarness, sessionStem *string
	displayName, kind, summary, capable, note                *string
	jsonOut                                                  *bool
}

func newAgentEditFlags(fs *flag.FlagSet, stderr io.Writer) agentEditFlags {
	flags := agentEditFlags{
		addCapability:    fs.String("add-capability", "", "add bare capability"),
		removeCapability: fs.String("remove-capability", "", "remove bare capability"),
		addHarness:       fs.String("add-harness", "", "add a harness for the role's own session; --session-stem names it"),
		sessionStem:      fs.String("session-stem", "", "default or added-harness session stem"),
		displayName:      fs.String("display-name", "", "replace display name"),
		kind:             fs.String("kind", "", "replace role kind"),
		summary:          fs.String("summary", "", "replace summary"),
		capable:          fs.String("capable", "", "replace comma-separated bare capabilities"),
		note:             fs.String("note", "", "append note"),
		jsonOut:          fs.Bool("json", false, "write JSON"),
	}
	agentUsage(fs, stderr, agentEditUsage)
	return flags
}

func agentUsage(fs *flag.FlagSet, stderr io.Writer, usage string) {
	fs.Usage = func() {
		fmt.Fprintln(stderr, usage)
		fs.PrintDefaults()
	}
}
