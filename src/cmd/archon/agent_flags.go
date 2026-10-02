package main

import (
	"flag"
)

// agent new and agent edit take the same flags offline and with --server; their
// usage is the CLI help table's, as every other command's is.

type agentNewFlags struct {
	kind, capable, personality *string
	jsonOut                    *bool
}

func newAgentNewFlags(fs *flag.FlagSet) agentNewFlags {
	flags := agentNewFlags{
		kind:        fs.String("kind", "", "role kind (default specialist)"),
		capable:     fs.String("capable", "", "comma-separated bare capabilities"),
		personality: fs.String("personality", "", "personality facet"),
		jsonOut:     fs.Bool("json", false, "write JSON"),
	}
	return flags
}

type agentEditFlags struct {
	addCapability, removeCapability           *string
	displayName, kind, summary, capable, note *string
	jsonOut                                   *bool
}

func newAgentEditFlags(fs *flag.FlagSet) agentEditFlags {
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
	return flags
}
