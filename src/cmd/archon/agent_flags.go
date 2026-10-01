package main

import (
	"flag"
	"fmt"
	"io"
)

// agent new and agent edit take the same flags offline and with --server.

const (
	agentNewUsage  = "usage: archon agent new <id> [--kind <kind>] [--capable a,b] [--personality p] [--json]\nA role is role text; each slot that uses it states its own harness, model and effort (archon formation assign)."
	agentEditUsage = "usage: archon agent edit <id> [--display-name n] [--kind k] [--summary s] [--capable a,b] [--add-capability t|--remove-capability t|--note text] [--json]"
)

type agentNewFlags struct {
	kind, capable, personality *string
	jsonOut                    *bool
}

func newAgentNewFlags(fs *flag.FlagSet, stderr io.Writer) agentNewFlags {
	flags := agentNewFlags{
		kind:        fs.String("kind", "", "role kind (default specialist)"),
		capable:     fs.String("capable", "", "comma-separated bare capabilities"),
		personality: fs.String("personality", "", "personality facet"),
		jsonOut:     fs.Bool("json", false, "write JSON"),
	}
	agentUsage(fs, stderr, agentNewUsage)
	return flags
}

type agentEditFlags struct {
	addCapability, removeCapability           *string
	displayName, kind, summary, capable, note *string
	jsonOut                                   *bool
}

func newAgentEditFlags(fs *flag.FlagSet, stderr io.Writer) agentEditFlags {
	flags := agentEditFlags{
		addCapability:    fs.String("add-capability", "", "add bare capability"),
		removeCapability: fs.String("remove-capability", "", "remove bare capability"),
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
