package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// Help for every noun and command (archon-n7u.33). This table is the one place
// that says what each command takes and does: "archon <noun>" and
// "archon <noun> -h" list a noun's commands, "archon <noun> <command> -h"
// prints that command's usage and flags, and a command given the wrong
// arguments prints the same usage.

type commandHelp struct {
	verb string
	// args follows "archon <noun> <verb>": positional arguments, then flags.
	args    string
	summary string
}

type nounHelp struct {
	noun string
	// summary is the noun's line in archon -h; about opens its own help.
	summary  string
	about    string
	commands []commandHelp
	// note follows the command list.
	note string
}

var nounHelps = []nounHelp{
	{noun: "mission", summary: "missions: the reusable unit", about: "A mission is the reusable unit: one .mission.toml file whose Input card starts each run.", commands: []commandHelp{
		{"new", "<slug> [--title <title>] [--json]", "create an empty mission"},
		{"list", "[--json]", "list the missions"},
		{"inspect", "<mission> [<input>] [--json]", "print the whole mission, or an Input card and the steps it reaches"},
		{"notes", "<mission> [--json]", "print the mission's note threads"},
		{"note", "<mission> [--node <element-id>] (--text <text> | --file <path>) [--entry <id> [--clear]] [--author <human|agent>:<name>] [--json]", "add to a note thread. With --entry, edit your own entry, or delete it with --clear"},
		{"validate", "<mission> [--json]", "list every finding that would stop a run, naming each node by title and ID. Exits 1 on any error"},
		{"arrange", "<mission> [--json]", "lay the canvas out along the run"},
		{"create", "<mission> [--title <title>] [--goal <goal>] [--file <path>]... [--human-channel notify|session] [--x n] [--y n] [--json]", "add the Input card to a mission that has none. Create the mission itself with archon mission new <slug>"},
		{"update", "<mission> [<input>] [--title text] [--goal text] [--file path]... [--input-hint text] [--human-channel notify|session] [--json]", "change the Input card. Only the flags you give change it, and an empty value clears that field"},
		{"input", "<mission> [<name>] [--kind text|file|folder] [--required | --optional] [--description <text>] [--delete] [--json]", "list the inputs a run supplies, or with a name declare, change or --delete that input. A mission that declares none takes one required text input, brief. A new input is optional text unless the flags say otherwise, and only the flags you give change an existing one; step briefs reference it as {name}"},
		{"wire", "<mission> [<input>] <to-node:port> [--json]", "wire the Input card to a step"},
		{"run", "<mission> [--input name=value]... [--input-file name=path]... [--cwd dir] [--context-path path]... [--bead id] [--actor actor] [--json]", "start a run. Required: <mission>, and an --input or --input-file for each input the mission requires (archon mission input <mission> lists them); every other flag is optional. The mission's Limit cards set its limits; it has none without one"},
	}, note: "<input> may be left out when the mission has one Input card."},
	{noun: "formation", summary: "steps: the agents that do the work", about: "A formation is a step: the team of agents that does it, solo, peer or orchestrated.", commands: []commandHelp{
		{"create", "<mission> [solo|peer|orchestrated] [--title <title>] [--x n] [--y n] [--json]", "add a step. It is solo unless you name another type"},
		{"list", "<mission> [--json]", "list the steps with their slots and staffing"},
		{"inspect", "<mission> <formation> [--json]", "print one step with its slots, ports, brief and connections"},
		{"assign", "<mission> <formation> --slot <slot> --harness <claude-code|openai-codex> --effort <effort> [--model <model>] [--role <persona>] [--json]", "staff a slot. A slot owns its harness, model and effort; --role adds optional role text, and a slot without one is a vanilla agent"},
		{"unassign", "<mission> <formation> --slot <slot> [--json]", "empty a slot"},
		{"set-brief", "<mission> <formation> --goal <goal> [--bead <beads-id>] [--file <path>]... [--link <url>]... [--json]", "write the step's brief"},
		{"rename", "<mission> <formation> <title> [--json]", "change the step's title"},
		{"set-type", "<mission> <formation> <solo|peer|orchestrated> [--keep-slot <slot>] [--json]", "change the step's type. --keep-slot names the slot a change to solo keeps"},
		{"add-input", "<mission> <formation> --label <label> [--json]", "add an input port"},
		{"add-output", "<mission> <formation> --label <label> [--json]", "add an output port"},
		{"wire", "<mission> <from-node:port> <to-node:port> [--join] [--json]", "connect two ports. --join adds an input when the target is already fed"},
		{"unwire", "<mission> <from-node:port> <to-node:port> [--json]", "remove a connection"},
		{"run", "<mission> <formation> [--input name=value]... [--input-file name=path]... [--cwd dir] [--context-path path]... [--bead id] [--actor actor] [--json]", "run one step on its own. It takes the mission's inputs, as a mission run does"},
	}},
	{noun: "gate", summary: "checks between steps, and human verdicts", about: "A gate checks work between steps with code, a judge formation or a human, and routes it on pass or fail.", commands: []commandHelp{
		{"create", "<mission> [--kinds code,formation,human] [--title text] [--criterion text] [--check id --check-version version --check-value value] [--file path]... [--x n] [--y n] [--json]", "add a gate. It is human unless --kinds says otherwise"},
		{"update", "<mission> <gate> [--title text] [--kinds code,formation,human] [--criterion text] [--check id] [--check-version version] [--check-value value | --clear-check] [--file path]... [--json]", "change a gate. Only the fields given change, and an empty value clears one"},
		{"judge", "<mission> <gate> --chain f1,f2 | --detach [--json]", "attach a judge chain of formations, or detach it"},
		{"request", "<runId> <gateId> [--json]", "read a waiting human gate's question, routed input and where each verdict leads. Needs --server"},
		{"approve", "<runId> <gateId> --requested-seq <seq> [--response text | --response-file path] [--relayed-by slot-id] [--json]", "pass a waiting human gate. The response travels with the gate's input"},
		{"reject", "<runId> <gateId> --requested-seq <seq> [--response text | --response-file path] [--relayed-by slot-id] [--json]", "send a waiting human gate's work back. The response is the feedback"},
	}},
	{noun: "end", summary: "End nodes, which end a path on purpose", about: "An End node ends a path on purpose, done or rejected.", commands: []commandHelp{
		{"create", "<mission> [--outcome done|rejected] [--title text] [--x n] [--y n] [--json]", "add an End node. Wire a route into it with archon formation wire <mission> <node:port> <end-id>:in"},
		{"update", "<mission> <end> [--outcome done|rejected] [--title text] [--json]", "change an End node's title or outcome"},
		{"delete", "<mission> <end> [--json]", "remove an End node"},
	}},
	{noun: "limit", summary: "Limit cards, which cap a step or the whole mission", about: "A Limit card caps the step it covers, or the whole mission when it covers the Input card. At the limit the run blocks until run resume --grant.", commands: []commandHelp{
		{"create", "<mission> --target <step|input> [--rounds n] [--time 30m] [--warn 5m] [--tokens n] [--title text] [--x n] [--y n] [--json]", "add a Limit card. At the limit the run blocks; run resume --grant gives one more round, or the card's time or tokens again. Tokens are approximate: input not read from the cache, cache writes included, plus output, subagents included"},
		{"update", "<mission> <limit> [--target <step|input>] [--rounds n] [--time 30m] [--warn 5m] [--tokens n] [--title text] [--json]", "change a Limit card. Only the flags you give change; an empty --rounds, --time, --warn or --tokens clears that knob and an empty --target unwires the card"},
		{"delete", "<mission> <limit> [--json]", "remove a Limit card"},
	}},
	{noun: "tool", summary: "Tool nodes", about: "A Tool node runs a registered profile on its input.", commands: []commandHelp{
		{"create", "<mission> --profile-id <id> --profile-version <version> --title <title> --params-json <object> [--x n --y n | --predecessor-node-id <id> | --successor-node-id <id>] [--json]", "add a Tool"},
		{"update", "<mission> <tool> [--title <title>] [--params-json <object>] [--json]", "change a Tool's title or parameters"},
		{"delete", "<mission> <tool> [--json]", "remove a Tool"},
		{"inspect", "<mission> <tool> [--json]", "print a Tool"},
	}},
	{noun: "agent", summary: "role cards", about: "A role card is generic role text a slot may use; the slot owns its harness, model and effort.", commands: []commandHelp{
		{"list", "[--capable <capability>] [--assignable] [--json]", "list the roles and their liveness"},
		{"inspect", "<id> [--json]", "print a role card"},
		{"new", "<id> [--kind <kind>] [--harness <h>] [--capable a,b] [--personality p] [--from <path>] [--json]", "create a role card. A role carries no model or effort; each slot that uses it sets them (archon formation assign)"},
		{"edit", "<id> [--display-name n] [--kind k] [--summary s] [--capable a,b] [--session-stem s] [--harness h] [--model m] [--effort e] [--add-capability t | --remove-capability t | --add-harness h | --note text] [--json]", "change a role card"},
		{"spawn", "<id> [--harness <h>]", "start the role's own session"},
		{"attach", "<id> [--harness <h>]", "attach to the role's live session"},
		{"retire", "<id> [--force]", "retire a role card"},
	}},
	{noun: "run", summary: "runs, through the daemon", about: "A run is one start of a mission or a single step. Read and drive runs with --server.", commands: []commandHelp{
		{"list", "[--mission <mission>] [--json]", "list runs, or one mission's, newest first: one line per run with its ID, mission, status, Bead, start and last change. --json prints each run's full projection"},
		{"status", "<runId> [--json]", "print a run's status"},
		{"logs", "<runId> [--node <id>] [--follow] [--json]", "print a run's projection"},
		{"follow", "<runId> [--since <seq>] [--node <id>] [--json]", "print the run as it changes until it is final"},
		{"wait", "<runId> [--until needs-you|final|any-change] [--since <seq>] [--timeout <duration>] [--reconnect <duration>] [--json]", "block until the run needs you, ends or changes. Needs --server"},
		{"gates", "<runId> [--json]", "list a run's waiting gates and the seats they asked. Needs --server"},
		{"seats", "<runId> [--json]", "list a run's seats, their session names and what each waits on. Needs --server"},
		{"resume", "<runId> [--mode reattach|redispatch] [--grant] [--reason text] [--actor actor] [--json]", "resume a blocked run. --grant gives a step its spent Limit card stopped one more round, or the card's time or tokens again"},
		{"abort", "<runId> [--reason <reason>] [--requested-by <actor>] [--json]", "stop a run"},
		{"ask", "<runId> [question] [--json]", "summarize what a run has done, offline"},
	}},
	{noun: "peer", summary: "a peer formation's conversation, for its seats", about: "A peer formation's seats converse through these commands, run with --workspace <state-dir>.", commands: []commandHelp{
		{"read", "--run <id> --node <id> --attempt <n>", "print the conversation"},
		{"wait", "--run <id> --node <id> --attempt <n> --after <seq>", "wait for a later entry or a final state"},
		{"post", "--run <id> --node <id> --attempt <n> --slot <id> (--text <text> | --text-file <path>) [--dissent --proposal <seq>]", "post a message. With --dissent and --proposal, object to a proposal"},
		{"propose", "--run <id> --node <id> --attempt <n> --slot <id> (--text <text> | --text-file <path>)", "propose the full output"},
		{"ack", "--run <id> --node <id> --attempt <n> --slot <id> --proposal <seq>", "acknowledge a proposal"},
	}},
}

const archonHelp = `usage: archon [--workspace <state-dir> | --server <url>] <mission|formation|gate|end|limit|tool|agent|run|peer> <command> [arguments]
       archon [--server <url>] version

Archon chains agents and gates. Name the state directory with --workspace to
work on its files offline, or a daemon with --server; there is no default.
Runtime commands (mission run, formation run, run, gate approve|reject) go
through --server.

`

// helpForNoun finds a noun's help.
func helpForNoun(noun string) (nounHelp, bool) {
	for _, help := range nounHelps {
		if help.noun == noun {
			return help, true
		}
	}
	return nounHelp{}, false
}

// helpForCommand finds a command's help by "noun verb".
func helpForCommand(name string) (commandHelp, bool) {
	noun, verb, _ := strings.Cut(name, " ")
	help, ok := helpForNoun(noun)
	if !ok {
		return commandHelp{}, false
	}
	for _, command := range help.commands {
		if command.verb == verb {
			return command, true
		}
	}
	return commandHelp{}, false
}

// commandUsage is a command's usage line and what it does.
func commandUsage(name string) string {
	command, ok := helpForCommand(name)
	if !ok {
		return "usage: archon " + name
	}
	return fmt.Sprintf("usage: archon %s %s\n%s.", name, command.args, upperFirst(command.summary))
}

func upperFirst(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func writeArchonHelp(w io.Writer) {
	fmt.Fprint(w, archonHelp)
	for _, help := range nounHelps {
		fmt.Fprintf(w, "  %-10s %s\n", help.noun, help.summary)
	}
	fmt.Fprintln(w, "\nRun \"archon <noun> -h\" for its commands and \"archon <noun> <command> -h\" for a command's flags.")
}

func writeNounHelp(w io.Writer, help nounHelp) {
	fmt.Fprintf(w, "usage: archon %s <command> [arguments]\n\n%s\n\n", help.noun, help.about)
	uses := make([]string, len(help.commands))
	width := 0
	for index, command := range help.commands {
		uses[index] = strings.TrimSpace(command.verb + " " + positionals(command.args))
		width = max(width, len(uses[index]))
	}
	for index, command := range help.commands {
		fmt.Fprintf(w, "  %-*s  %s\n", width, uses[index], firstSentence(command.summary))
	}
	if help.note != "" {
		fmt.Fprintf(w, "\n%s\n", help.note)
	}
	fmt.Fprintf(w, "\nRun \"archon %s <command> -h\" for a command's arguments and flags.\n", help.noun)
}

// commandFlags is a command's flag set: -h prints its usage, then its flags.
func commandFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, commandUsage(name))
		flags := false
		fs.VisitAll(func(*flag.Flag) { flags = true })
		if flags {
			fmt.Fprintln(stderr, "\nflags:")
			fs.PrintDefaults()
		}
	}
	return fs
}

// offlineUnavailable answers a command that runs only through the daemon, or
// one the noun lacks.
func offlineUnavailable(stderr io.Writer, noun, verb string) int {
	if _, ok := helpForCommand(noun + " " + verb); ok {
		fmt.Fprintf(stderr, "archon %s %s needs --server <url>: it reads the daemon\n%s\n", noun, verb, commandUsage(noun+" "+verb))
		return 2
	}
	return unknownCommand(stderr, noun, verb)
}

// unknownCommand names a command the noun lacks, then lists the noun's
// commands.
func unknownCommand(stderr io.Writer, noun, verb string) int {
	fmt.Fprintf(stderr, "unknown %s command %q\n\n", noun, verb)
	if help, ok := helpForNoun(noun); ok {
		writeNounHelp(stderr, help)
	}
	return 2
}

// positionals is the part of a command's arguments before its flags.
func positionals(args string) string {
	end := len(args)
	for _, flagStart := range []string{"[--", "--", "(--"} {
		if strings.HasPrefix(args, flagStart) {
			return ""
		}
		if at := strings.Index(args, " "+flagStart); at >= 0 && at < end {
			end = at
		}
	}
	return args[:end]
}

// firstSentence is a summary's first sentence, for a noun's command list.
func firstSentence(summary string) string {
	if at := strings.Index(summary, ". "); at >= 0 {
		return summary[:at]
	}
	return summary
}
