# Archon contract

## Scope

[PRODUCT.md](PRODUCT.md) states what Archon is for and its philosophy; this
contract defines the behaviour that implements it.

Archon builds and runs work graphs with agents and gates. `archond` owns
execution and serves the Archon UI. The `archon` CLI authors the same
definitions and sends runtime commands to the daemon. This is a trusted-operator
service with concurrent missions, two native harness adapters and durable run
history. Host deployment, forwarding and CHROTE integration live outside this
repository. [ADR-0016](adr/0016-daily-capability.md) records the daily-capability
decisions; [examples](../examples/) provide reusable missions.

## Definitions and storage

| Noun | Meaning |
| --- | --- |
| Mission | The reusable unit: one schema-1 TOML graph file, `<slug>.mission.toml`, with a stable ID, slug and revision. |
| Input card | The mission's entry node (`[[inputCard]]` in TOML, `inputCards` in JSON) with the mission's goal, input hint, declared inputs and outputs, and `out` port. A mission has one Input card. |
| Input | A named value each run of the mission supplies: a name, a description, a kind (`text`, `file` or `folder`) and whether it is required. Step briefs reference it as `{name}`. A mission that declares none has one implicit required text input, `brief`. |
| Output | A named result a successful run of the mission promises: a name, a description, a kind (`text`, `file` or `folder`), whether it is required, and `from`, the one step output port it comes from. A mission that declares none promises nothing. |
| Output port | What a step emits on one route. Its kind is `text` (the default), `file` or `folder`; a file or folder output's `ref` is the absolute path of what the step produced, and Archon keeps a copy of it as each attempt produced it. Input ports carry no kind. |
| Attempt directory | `<state-dir>/.archon/artifacts/<runId>/<nodeId>/<attempt>/`, created when each attempt of a step, judges included, is dispatched. The seat writes the files it produces there; it belongs to that attempt alone and is kept with the run. |
| Formation | A step: the team of agents that does it. `solo` has one seat; `peer` has peer seats; `orchestrated` has a controller directing its bound workers. |
| Slot | A position in a formation that owns what its seat runs: a harness (`claude-code` or `openai-codex`), a model (blank means the harness default) and an effort, plus an optional role (`agentId`, a persona). A slot without a role is a vanilla agent, such as `claude-code · opus · low`. A seat is the slot's runtime agent session. |
| Persona (role) | A TOML agent card with role text only: a display name, kind, summary, capabilities, status and notes. It carries no harness, model or effort; each slot that uses it states its own. Every Archon has seven built-in generic roles (`scout`, `planner`, `builder`, `judge`, `orchestrator`, `debugger`, `reviewer`); a card in the agents directory adds a role or overrides a built-in one. |
| Character | A saved staffing: a name, an optional description and picture, and a role, harness, model and effort. It is a stamp: stamping a slot writes those four values to the slot and keeps no link, so editing or deleting a character changes no mission. Characters live in `characters.json` beside the role cards. |
| Gate | A criterion with one or more kinds: `code`, `formation`, `human`. Its ports are `in`, `pass`, `fail`, `judge`. |
| End node | Ends a path on purpose (`[[end]]` in TOML: `id`, `title`, `outcome`). Its outcome is `done` or `rejected`. Its only port is `in`, which takes any number of routes; it leads nowhere. |
| Connection | A directed edge between `node-id:port-id` endpoints. Formation input and output ports have explicit IDs. |
| Judge chain | Formations wired from a gate's `judge` port and back to that same port: from the step the port feeds, each step returns to the port, which completes the chain, or hands on to the first step it feeds. A chain that leaves the steps (through a Tool or gate), loops or never returns is no chain, so validation names the gate incomplete; the engine and the cockpit share this rule. The final judge result decides the formation kind. |
| Limit card | Caps the rounds, time and tokens of the step it covers, or of the whole mission when it covers the Input card (`[[limit]]` in TOML, `limits` in JSON: `id`, `title`, `target`, `rounds`, `seconds`, `warnSeconds`, `tokens`, `whenSpent`). A run has no limits without one. A spent card defaults to blocking for a grant, fails the run with `whenSpent = "fail"`, or on a step, with `whenSpent = "best"`, picks the best attempt its gate judged and goes on. |
| Pushback edge | A gate's `fail` connection back to work, delivering feedback and starting the next attempt, capped only by a Limit card. There is no `retry_control` port. |
| Run | One admitted mission or isolated formation, with definition and persona snapshots and inputs. Later edits affect later runs. |
| Ledger | Private append-only NDJSON events, ordered by sequence. It records dispatch, results, routing and recovery evidence. |
| Projection | A sanitized view derived from the ledger, shared by HTTP, Archon and the cockpit. |

The private `<state-dir>` is also Archon's `--workspace`. Offline commands have
no default workspace: without `--workspace` or `--server` they say so and stop.
It contains `.archon/missions/*.mission.toml`, `.archon/notes/*.notes.toml`,
`.archon/layout/*.layout.toml`, `.archon/runs` ledgers and snapshots,
`.archon/artifacts`, and `briefs`. Any of these directories may be a symlink,
for example to another disk, and so may a mission, notes or layout file, for
example a mission kept in a repository: Archon reads and writes through the
link and keeps it, and deleting the mission removes the link, not the file it
points at. One file that cannot be read never hides the rest: a mission whose
link lost its target (or that does not parse) is listed with `broken`, "<link>
is a symlink to <target>, which does not exist", and every other mission still
lists, opens and runs; reading or starting the broken one answers the same
words (HTTP 422 `BROKEN_LINK`, CLI code `broken_link`), and the cockpit lists
it as "cannot be read", names it under the canvas and never opens it. A
mission or layout file is read only as strict TOML of its schema: one that is
not, such as a hand edit that repeats a key, is refused with
`invalid_definition_source` naming the fault, never read leniently. A role
card or run ledger that cannot be read is skipped and named: `agent list` and
`run list` print a `warning:` line, `GET /api/agents` lists it under
`unreadable`, and the daemon logs a skipped ledger at startup instead of
refusing to start. Role cards default to `<state-dir>/agents`, for the
daemon and offline alike: offline `archon` commands read
`<--workspace>/agents`. Daemon `--agents-dir` can select another absolute
directory; `ARCHON_AGENTS_DIR` points offline commands at the same one. An
offline `archon agent` command with neither a workspace nor
`ARCHON_AGENTS_DIR` is refused, as there is no default.

A slot owns its harness, model and effort. Staff it with `archon formation
assign <mission> <formation> --slot <slot> --harness <h> --effort <e>
[--model <m>] [--role <persona>]`,
or the `assignSlot` mission patch with `agentId`, `harness`, `model` and
`effort`. Staffing always states the slot in full: the CLI requires
`--harness` and `--effort`, and a patch without a harness or an effort,
including one naming only a role, is refused with `INVALID_SLOT_SETTINGS`
(HTTP 422, CLI code `invalid_slot_settings`). A role adds only its text. A
patch naming none of them empties the
slot, as does `formation unassign`. The harness must be one Archon starts and must accept the effort,
and a model is one name without spaces. The agent roster serves each
harness's known `models`: `claude-code` runs the aliases `opus`, `sonnet`,
`haiku` and `fable`.
`openai-codex` runs the models its CLI lists in
`$CODEX_HOME/models_cache.json` (default `~/.codex`), by priority, each with
the efforts it accepts; without that cache Codex lists none. A known Codex
model narrows the slot's efforts to its own.
The catalog is a picker, not an exhaustive list of accepted names. A model
outside it is accepted without a warning; the harness decides whether it can
run. Choose the effort by the policy the agent
roster serves as `effortPolicy`, each line with the role kinds it suits:
`low` for errands (`verifier`, `scout`), `medium` for making things
(`builder`, `debugger`, `operator`), `xhigh` for architecture and review
(`reviewer`, `judge`, `architect`, `designer`, `planner`, `orchestrator`), and
`max` for consequential reviews, chosen by hand and never suggested. One role may staff
several slots, each with its own settings.

The cockpit shows every slot as a row: its harness mark and label, then its
role (or `vanilla`) over `<Harness> · <model> · <effort>`, as in `Claude · opus
· low`. Cards name the harness in one short word (`Claude`, `Codex`) and a
blank model as `default`; sentences say `Claude Code` and `default model`. On a
card only a long model gives way, never the harness or the effort, and the
workers of an orchestrated step sit under `workers` with a solo slot's width;
an empty slot says `open slot`. Models outside the catalog receive no error
mark. The node window and Flow say the same, for example `Worker 1
is vanilla on Claude Code · opus · low.` Rosters list roles by name; roles
carry no harness. The Missions rail counts across every formation of the
mission: `25 roles · 6 in use`, and a role in use says `in 2 slots`. Clicking a role in the Missions rail opens it in a window beside the
rail, with its role text and the slots it staffs.

Staffing in the cockpit is that sentence, edited where the slot is. Clicking a
slot, or Enter on a focused one, opens a compact window reading `Worker 1 is
[vanilla] on [Claude Code] · [opus] · [low]` while the slot itself is
outlined. It opens like a dropdown on the clicked slot's row, or on the word
clicked in a node window (from the canvas or Flow): directly below it and left-aligned with it, held inside the view
across, or directly above it where the view cannot pan to make the room. One
rule holds on every surface. While open it may cover sibling rows, neighbouring
cards and the window it was opened from, never the clicked slot or word, and a
soft shadow puts what it covers behind it; nothing stays covered once it
closes. Where the room below a canvas slot is short, the canvas pans by exactly
the room the opening list needs, in 120 ms (none under reduced motion), and
the window fades in only after the pan. The window keeps one height from
opening to closing: the sentence with about eight lines of the list it opens
with (the role list for the whole sentence), or all of a shorter list such as
the six efforts. Lists show and scroll only whole rows, after a wheel, a
scrollbar drag or a press dragged inside them alike. vanilla stays above the
scrolling role grid. `on <harness> · <model> · <effort>` wraps as one group.
Each word opens its own list on the slot's current value, a model outside the
catalog included, so a reflex Enter changes nothing, and clicking one word of a
staffed slot opens only that list, where a pick lands at once. An empty slot opens as vanilla on the first harness and its
first model, at the effort the policy reads from the step's title, else `low`,
so Enter staffs it. The first word takes typed words in any order (`cri ast`,
`sonnet high`), echoing how each was read; a typo, an ambiguous role or an
effort the harness or model refuses is named and blocks Enter, with one-click
fixes. Digits 1-6 set the effort, in the window or on a focused slot. Esc or a
click away changes nothing. N on the canvas reaches the next empty slot in Flow
order, brings it into view and opens it. The role list shows each role's
suggested effort, from its kind by the policy (its name and summary are read
only when the policy names no effort for its kind), and the effort list tags
the suggestion with each effort's policy words. A reason cites the policy line,
as in `planning falls under architecture and review`. A role landing on an
empty slot takes the policy's effort. A role landing on a staffed slot keeps
that slot's harness, model and effort, and one landing with an effort chosen
in the open sentence (picked, typed as in `high cri`, or set by digit) keeps
that effort; when the policy suggests another, the window offers `use xhigh`
and the slot `use xhigh?`, one click each, until it is taken or the role or
effort changes. Nothing outside the open sentence remembers how an effort was
chosen. The slot takes its agent visibly, its mark dropping in with a ring
going out from it, and for as long as it reads a note on the slot's label
line, inside the slot, says why each landing came out as it did, or why it was
refused; its whole text is the slot's tooltip. Under reduced motion nothing
moves and no ring or highlight is left behind.
Dragging a role from the rail onto a slot lands it by the same rule, its ghost
waiting beside the slot so the slot's preview stays readable; dragging a
staffed slot onto another moves its staffing there and swaps a staffed
target's back, two slots running the same staffing are left alone, and a drop
that reaches no slot changes nothing. The slot's menu offers Staff and Empty.
Each staffing is one undo entry; a move is one entry for both slots. The node
window's staffing words open the same window.

A role is role text only. `archon agent new` and `archon agent edit` take no
harness, model or effort; `POST` and `PATCH /api/agents` refuse a `harness`,
a `model`, an `effort` or any field they do not take with `INVALID_AGENT_CARD`
(HTTP 422) naming it; persona reads carry none; and the Agents view edits a
role's text, never its settings. A role's own session is the tmux session named
after the role. `archon agent spawn <id> --harness <h> --effort <e> [--model
<m>]` starts it, and refuses one already running, which keeps what it started
with: it names what the spawn stated and says it changes none of it. It states its settings as a slot does: the effort must be one the harness accepts
(`claude-code` takes `low`, `medium`, `high`, `xhigh` or `max`; `openai-codex`
also takes `ultra`, though a Codex model may accept fewer), a blank model means
the harness default, and a harness Archon cannot start, such as `hermes`, is
refused. Cards hold no launch string.

The Agents view is the library of roles and characters and draws no mission:
slots are staffed on Missions. It opens on a role and shows, for the role
picked, every slot in every mission that uses it; a use opens its mission on
Missions. It offers Retire and Delete only after saying what each does to
those slots. `archon agent retire <id>` and `archon agent restore <id>` retire
a role and bring it back, naming the slots that use it; a retired role is no
longer offered for staffing, and a slot that still names it stops validation
and admission with `retired_persona` until it is restaffed or the role comes
back. `archon agent delete <id>` removes a role's card only while no slot
names it, so no slot ever points at a missing role. The inspector lists a
role's notes, oldest first, each with its author and time; a note written there
is the operator's (`human:ui`), sent as `updatedBy` on `PATCH /api/agents/{id}`,
and other writers' notes default to `agent:archon`. The cockpit calls the card
a role everywhere: New role, Edit role, Kind, and the terminal window is the
terminal.

Notes are operator intent, not
automatically executable briefs. Read mission and element notes, then translate
them into formation briefs, staffing and edges.

Each mission or element note is a thread of entries, oldest first. An entry has an
ID, an author (`human:<name>` or `agent:<name>`), a creation time, an optional
edit time and text. `archon mission note` appends an entry, by default as
`agent:archon` (`--author` names another). Only an entry's author can change it:
`--entry <id> --text` edits it and `--entry <id> --clear` deletes it. Reply
rather than rewriting someone else's note. The cockpit is the operator's own
surface, so everything it does names `human:ui` (archon-by4a): its mission
edits' `updatedBy`, its notes, and its run starts, verdicts, resumes and stops
(a verdict is recorded as decided by `human:operator`, whoever sent it); run
evidence reads that actor as "human:ui". It
shows notes on the canvas in their own layer above the cards, as a preview of
each thread's latest entry, the full thread, or hidden, with the operator's and
agents' entries styled apart. A card's note pin or sticky, or Mission notes, opens
a thread in a floating window to reply, and to edit or delete your own entries.
Notes files are schema 2: `missionId`, `rev`, and one `[[entry]]` per entry
with its `target` (`mission` for the mission's own thread, or a node ID).

A minimal mission file, `hello.mission.toml`, has an Input card and one staffed
formation:

```toml
schema = 1
id = "brd_hello"
slug = "hello"
title = "Hello"
rev = 1

[[inputCard]]
id = "inp_hello"
title = "Hello"
goal = "Return the supplied input"

[[formation]]
id = "fmn_work"
type = "solo"
title = "Work"
[formation.brief]
goal = "Return a short result using the supplied output contract."
[[formation.input]]
id = "port_in"
label = "Input"
[[formation.output]]
id = "port_out"
label = "Result"
[[formation.slot]]
id = "slot_work"
label = "Worker"
agentId = "builder"
harness = "openai-codex"
model = "" # harness default
effort = "medium"
controller = true

[[connection]]
id = "edge_start"
from = "inp_hello:out"
to = "fmn_work:port_in"
```

Authoring accepts drafts through the cockpit, HTTP and Archon. Mission, Input
card, formation, gate, note and persona fields may be blank or partial: a title,
goal, criterion or code check value can be left out. Blank
values take defaults where a node needs one: a formation becomes `solo`, a
persona kind becomes `specialist`, an Input card without a title becomes
`Input`, and a mission named in neither title nor slug becomes `Untitled
mission`. A supplied value must still be well formed, so an unsafe Bead ID, an
unknown formation type or an unknown code check profile is rejected on write.
Only run admission and `mission validate` reject incomplete work.

Each formation input port takes one ordinary feed, and a formation waits for all
of its input ports before starting. Joining work means one input port per
upstream. Dropping a wire (or reconnecting its target) onto a fed formation input
on the canvas adds a new input port and its connection in one mission revision;
one undo removes that join. Free inputs use their existing port. Gate and Tool
inputs remain single-feed. Gate-fail pushback retains its feedback exception.

For CLI authoring, `archon formation wire <mission> <from-node:port>
<to-node:port> --join` enables the same join, both offline and with `--server`.
Without `--join`, an occupied exact input returns an occupied-input error.
HTTP `wireConnection` and `rewireConnection` accept `joinIfOccupied: true`.
`wireConnection` may name an unused `id` to restore a deleted wire's identity;
without it a new identity is allocated. A target rewire retains the wire's
identity, so its routing stays attached.
To undo a target join atomically, `rewireConnection` also accepts
`removePreviousInput: true`: it removes the old formation input only if no
other connection uses it. Wiring errors use `INPUT_OCCUPIED`, `SELF_WIRE`,
`DUPLICATE_CONNECTION` or `INCOMPATIBLE_TOOL_CONNECTION`; `CONFLICT` remains
reserved for stale mission revisions or ETags in these edits.

The [delivery mission](../examples/delivery.mission.toml) and its
[notes](../examples/delivery.notes.toml) add Plan, Beads, a Beads-review judge,
orchestrated Execution and Final review. A run supplies one input, `change`,
which every step's brief references as `{change}`; the target repository is the
run's cwd and the owning Bead its Bead. Generic roles staff it at the effort
the policy gives them: Plan is `planner` at `xhigh`, Beads a vanilla agent at
`medium`, the Beads reviewer `judge` at `xhigh`, the Execution controller
`orchestrator` at `xhigh` with three `builder` workers at `medium`, and Final
review `reviewer` at `max`, a consequential review. The briefs carry everything
specific to delivery. Execution uses a Claude controller and three Codex
workers; Final review uses Astra. Failed Beads review returns directly to Beads. Final review produces a
report, with no following gate. This graph has no human gate.

## Execution

Archon checks no runtime authority: whoever reaches the daemon or runs the CLI
starts, resumes, aborts and decides runs. Listeners have no authentication;
configure only trusted interfaces and the host's network perimeter.

A run ledger is read only when it is a valid event sequence: one JSON event per
line, numbered from 1 without gaps, each with a timestamp, type, actor and the
run's own ID.

One coordinator locks a state directory. Many runs execute concurrently within
it. Admission validates the graph and inputs, snapshots definitions, durably
appends `run_started`, then returns HTTP 202.

New runs freeze each staffed slot's harness, model and effort, and its complete
role card when it has one, in a private schema-3 bindings snapshot. Tmux, lab
execution and completed-turn recovery use that snapshot after edits, retries
and restarts, and refuse a snapshot whose settings disagree with the frozen
slot. An omitted model freezes the choice to use the harness default, which
remains unpinned; give the slot a model to retain an exact model setting.
Bindings are schema 3; a snapshot in any other schema, or one whose settings
disagree with its frozen slot, blocks seat execution with
`persona_snapshot_invalid`. Start a new run to use current staffing.

Admission first checks that `expectedRev` and any `If-Match` name the current
mission revision (HTTP 409 otherwise). It then builds one report of every
problem the run would hit: mission validation plus supported formation types,
staffed slots (`unstaffed_slot`) whose harness, model and effort can start a
seat (`invalid_slot_settings`) and whose role, if named, is readable
(`unavailable_persona`) and not retired (`retired_persona`), one controller and a worker in each orchestrated
formation, complete code checks, judge chains, runnable Tools and a wired Input
card, and the run's inputs (see [Mission inputs](#mission-inputs)). Findings
cover the nodes the run reaches from its Input card, or the selected formation.
Formation types, slot counts and slot staffing are checked across the whole
mission, because the run snapshot binds every formation.
Any finding rejects the start with HTTP 422, error code `RUN_ADMISSION_FAILED`
and `error.findings` as `{code,nodeId,message}` entries; no run is recorded.
The engine's own fail-fast checks remain behind this report. The worker continues after the
client disconnects. A missing receipt requires checking the run list before
starting again. A mission run and a single step's run take the same run fields:
`cwd`, `contextPaths`, `beadId`, `inputs` and `actor`. Omit `cwd` (or send an empty string) to create a private workspace
for this run under `<state-dir>/workspaces/<runId>`. The daemon's
`--run-workspace-root` flag can select another absolute root. Admission records
the resolved directory in `run_started` and run status; every seat uses it.
Optional `contextPaths` names absolute existing files or directories to inspect as
mission context, independently of the workspace. The CLI accepts repeated
`--context-path`; the cockpit accepts one path per line. Admission validates
these references before allocating a workspace, records their ordered list in
`run_started`, and projects it in run status. Every seat, including the first
formation and later attempts, is instructed to inspect this context before
claiming no prior art. The paths are frozen for the run; their contents are not
snapshotted. They are reference inputs, not permission to modify the
referenced projects.

Rejected admission removes any newly allocated empty workspace. Admitted runs
retain their workspace and outputs after completion, cancellation or failure.
For an existing project, supply `cwd` as an absolute existing directory.
The run's `beadId` is optional but should identify the owning task. An Input
card has no Bead ID; a formation brief may name its own.

### Mission inputs

A mission declares on its Input card the named values its runs supply
(archon-o7p.3), so an agent can start it without reading its steps:

```toml
[[inputCard]]
id = "inp_delivery"
title = "Deliver"
goal = "Deliver a reviewed change"
inputs = [
  { name = "change", kind = "text", required = true, description = "What to deliver and why" },
  { name = "spec", kind = "file", description = "An existing spec to follow" },
]
```

A name is a lowercase letter followed by lowercase letters, digits or
underscores, unique in the mission. The kind is `text` (the default), `file` or
`folder`; an input is optional unless `required = true`. Authoring refuses
anything else with `INVALID_MISSION_INPUT` (CLI code `invalid_mission_input`),
and `mission validate` reports a hand-written mistake as
`invalid_mission_input`. A mission that declares no inputs has one implicit
required text input named `brief`, described by the Input card's input hint.

A run supplies values by name: HTTP `inputs` (`{"change":"..."}`), and on the
CLI `--input name=value` or `--input-file name=path` (the file's UTF-8 text),
each repeated per input. `archon mission input <mission>` lists what a run
supplies. Admission reports, on the Input card, a required input missing or
blank (`missing_input`: "input change is required: What to deliver and why"),
a name the mission does not declare (`unknown_input`), and a `file` or
`folder` value that is not the absolute path of an existing regular file or
directory (`invalid_input`). Text is kept verbatim; a path is trimmed.

The Input card hands its first step the supplied values: the implicit `brief`
unchanged, or one `name: value` line per supplied input in declared order. A
step brief references an input as `{name}`, and each seat's brief carries the
value in its place, or `(not supplied)` for an optional input the run left out.
Unless an input named `bead` is declared, `{bead}` takes the run's `beadId`
(`--bead`), or an empty string when none was supplied. A declared input named
`bead` takes precedence, including the optional-input rule when omitted. `{{bead}}` remains literal.
Braces around anything else stay as written. The one escape is `{{name}}`: it
reaches the seat as a literal `{name}` and is never a reference, so a brief can
show an agent a placeholder. Other references to names the mission does not declare
are the validation error `unknown_input_reference`: "Plan's brief references
{topic}, but the mission has no input named topic; its inputs are change,
spec". `run_started` records the supplied values as `inputs`
(`name`, `kind`, `value`), and run status and the run list project them, so
resume and restart substitute the same values. A single step's run takes the
mission's inputs and the same checks, and its step receives them as the first
step does; its objective, the step's brief, has its references resolved too.
The mission goal remains prompt context.

In the cockpit, Start mission and a formation's ▶ open one dialog
(archon-o7p.4). It asks for each input, labelled by its name with its
description as help: a text box for `text`, an absolute path for `file` and
`folder`. A required input left blank blocks the start with a message under
that field, and a refused start lists the daemon's findings in the dialog. It
also takes the workspace, context paths and Bead. ▶ runs that step on its own
(Run step), so it offers no human gate choice; a formation never starts without
the mission's inputs. The Input card's window reads the declared inputs as the
`{name}` references briefs use and edits them in place, one undo entry per
save.

### Mission outputs

A mission declares on its Input card the named results a successful run
promises (archon-5tg1), so an agent can chain it without reading its steps.
Each output is bound to one step output port by `from = "<step-id>:<port-id>"`:

```toml
[[inputCard]]
id = "inp_delivery"
title = "Deliver"
goal = "Deliver a reviewed change"
outputs = [
  { name = "report", kind = "file", from = "fmn_write:port_report", required = true, description = "The reviewed report" },
  { name = "summary", from = "fmn_write:port_out" },
]
```

The binding is explicit because generated port IDs (`port_` and uppercase
letters) cannot follow the output naming rule, and a gated step reaches its End
only through its gate. Names follow the input rule; the kind is `text` (the
default), `file` or `folder`; an output is optional unless `required = true`.
Authoring refuses a bad name, a bad kind, a repeated name or a `from` that is
not `<step-id>:<port-id>` with `INVALID_MISSION_OUTPUT` (CLI code
`invalid_mission_output`). A blank `from` saves as a draft. Validation reports,
on the Input card, `invalid_mission_output` for a hand-written mistake,
`unbound_mission_output` for an output with no `from` or a `from` that names no
step output port, and `mission_output_kind_mismatch` for an output whose kind is
not its port's kind, such as a `file` output bound to a text port. Admission
refuses a mission run with any of them; a single step's run ignores the
mission's outputs.

Output ports have a kind: `[[formation.output]]` takes `kind = "file"` or
`kind = "folder"`, and text is written as no kind. A kind on an input port, or
any other kind, is the validation error `invalid_port_kind`. The authoring
surfaces are `addPort` with `kind`, the `updatePort` mission patch
(`formationId`, `portId`, and `label` or `kind`, each optional; a text kind
clears it), `archon formation add-output --kind`, `archon formation set-port
<mission> <formation> <port> [--label] [--kind]`, and the output row's menu in
the cockpit. A port edit that names an input port's kind, an unknown kind or a
blank label is refused with `INVALID_PORT` (CLI code `invalid_port`).

`archon mission output <mission>` lists the declared outputs (name, kind,
required or optional, `from` or `unbound`, description); `archon mission output
<mission> <name> [--kind] [--from <step-id>:<port-id>] [--required |
--optional] [--description] [--delete]` declares, changes or removes one, and
only the flags given change an existing one. `updateInputCard` takes `outputs`,
which replaces the declared outputs; an empty list clears them. The Input
card's window edits them beside the inputs, each bound by choosing a step
output port.

When a run finishes at a done End, each declared output is a payload of its
bound port that went down the path to that End, and the attempt that produced
it: the text for a text output, the kept snapshot path for a file or folder.
Archon follows the routed payloads back from each delivery to a done End,
through gate pass routes and the inputs of the attempts that ran, by the
`origin` each routed input carries; when the bound port's output reached the
End more than once, the latest attempt wins. A gate's fail route is never
followed, so an attempt a gate rejected is never the run's output: when a
fixing step's output went on to the End, that is the output. An earlier
attempt a gate routes on as its pass is the one returned. A required output
whose port delivered nothing to the End (its step did not run on the path
taken, only rejected attempts of it ran, or its text was blank) fails the run
with `run_failed` code `missing_output`, actor `archond`, and a reason naming
the output and its `from`. So does a file or
folder output, required or not, whose path no longer exists or is no longer of
its kind when the run ends. An optional output the run did not produce is left
out. A run that fails at a rejected End promises nothing: it fails with
`path_rejected` and lists no outputs.

`run_succeeded`, or `run_picked` for a run whose work came through a pick,
records the outputs as `outputs` (`name`, `kind`, `value`, `nodeId`, `portId`,
`attempt`, and `picks` where a pick carried the value), and run status, the
run list, `run wait`'s `end` and the cockpit project them. Plain `run status`
prints one line per output after the status line,
`output<TAB><name><TAB><kind><TAB><value>`, and the final `run wait` paragraph
does the same; a text value of several lines shows its first line and
` [...]`, and `--json` holds every value whole. A driver passes a value
straight to the next mission: `archon mission run next --input
report=<value>`. The cockpit lists a succeeded or picked run's outputs; a path
opens in the file viewer.

An output whose value came through a pick is failed work ([ADR-0022](adr/0022-a-picked-run-is-not-a-success.md)), and
carries `picks`, oldest first: one entry per pick its value came through,
whether the pick carried the value itself or carried an output that the
attempt producing the value took in, however far back. Each entry has the
picked step as `nodeId`, its `attempt`, the selecting gate's `gateId`, the
`seq` of the pick's `gate_verdict`, who picked as `decidedBy` (the final
judge's ID, the operator's actor or `archon`), and as `verdict` the picked
attempt's own verdict as that gate recorded it: its `seq`, the `verdict`
(`fail`, unless a pass route led back to the step), the judge's or operator's
`reason`, and the judge's `findings`, the evidence lines that differ from the
reason. Plain `run status` and the final `run wait` paragraph follow such an
output's line with `picked<TAB><name><TAB><step> attempt <n> through <gate> by
<picker>; its verdict: <verdict>: <reason>` and one
`finding<TAB><name><TAB><text>` line per finding, each text on one line as
values are. An output no pick carried has no `picks`. The cockpit marks a
picked output's chip "picked" and gives the failed verdict and findings in its
title.

### Limit cards

Runs have no limits unless the mission holds a Limit card (archon-o7p.8). A
run without one loops through send-backs until a gate passes or its driver
stops it; neither a launch nor the daemon sets a limit. A Limit card covers one
target: a step (a formation), or the Input card for the whole mission. Each
knob it sets is enforced; all are optional. Its `rounds` knob counts, from
the ledger:

- on a step, how many times the step may run, send-backs and resumed re-runs
  included;
- on a peer step, how many journal messages its conversation may hold after
  the openings, proposals, acknowledgements and dissent included, summed over
  its attempts;
- on the Input card, how many step runs the whole mission may make, judges
  included.

A start the coordinator cut short before its output, by a crash or by a
shutdown such as a deploy (whose block has `code` `coordinator_shutdown`), is
not a round; the step's re-run is, so a run reaches the same limits wherever a
restart falls.

Its `seconds` knob (the time knob) counts wall time, in whole seconds, from the
ledger's timestamps, never time spent waiting:

- on a step, while one of the step's attempts runs: from its `node_started`
  to its output, its own error, its abandonment or the run's end. It does not
  count while the step's path waits on a human gate, so a send-back resumes
  the step with the time it has left;
- on the Input card, while any step of the run is running, judges included.
  It keeps counting while another path works during a gate's wait and pauses
  while every open path only waits on a human gate.

Neither counts while the run is blocked, nor while the daemon was down after
a crash: the time from the last event before the restart's interruption to the
interruption counts nothing. An attempt still open when the run resumes counts
on. The whole attempt counts: seat startup, preparation,
collaboration and finalization. When a step is dispatched, the time its cards
have left becomes its deadline, recomputed from the ledger, so a restart never
gives an attempt more time. At the deadline the step's seats stop, keeping
partial evidence. With `warnSeconds` set, the covered seats get a warning
pasted once when that much time is left, like an operator's message: "Archon:
5 min left of this step's working time (Limit card Clock). When it runs out
the step stops and the run waits for the operator. Finish your output now."
The ending depends on `whenSpent`: `stop` says "waits for the operator";
`fail` says "fails, keeping the attempts as evidence"; `best` says "picks
the best judged attempt, or waits if none is eligible". A step's card warns
each seat Archon dispatched in the attempt (a solo seat,
every peer, an orchestrated step's controller) once per attempt, at once when
the attempt starts with less time left; the mission's card warns only the
seats working when it fires, once in the run. Each warning is recorded as
`limit_warning` (`limitId`, `nodeId`, `slotId`, `attempt`, `text`); the lab
executor records them without seats.

Its `tokens` knob (archon-o7p.9) counts what the covered seats spend: on a
step, every dispatch of its attempts, and on the Input card, every dispatch of
the run, judges included. The count is approximate and the same for both
harnesses: input the model did not read from its cache, writing to the cache
included, plus every output token, reasoning included, subagents included,
counted for each dispatch from its pointer. For Claude Code that is
`input_tokens + cache_creation_input_tokens + output_tokens` of each assistant
message after the pointer, once per message id, in the session file and the
subagent files beside it; for Codex it is how much the session's
`total_token_usage` grew from its last count before the pointer,
`input_tokens - cached_input_tokens + output_tokens`. An orchestrated step's
workers count from the pointers its controller pastes. When a dispatch ends
or is stopped the executor records `token_usage` (`nodeId`, `slotId`,
`attempt`, `dispatchId`, `tokens`, `usage` with `input`, `cacheWrite`,
`cacheRead` and `output`), and the knob counts those records, so replay and
later attempts read the ledger, not transcripts; only a run whose step a
tokens knob covers records them. While a step works, its seats' transcripts
are counted every two seconds, and when the attempt reaches what its cards
have left the step's seats stop, so a count can pass the budget by what the
seats spent in between. A lab step whose brief holds `archon-lab-tokens: N`
spends N tokens.

A step covered by its own card and the mission's card stops at whichever is
spent first. Before a step starts, the engine checks its card and then the
mission's, rounds before time before tokens; at a spent limit the step does
not start, and when time or tokens run out the running step stops. With
`whenSpent = "fail"`, on a step or Input card, the run immediately ends
`run_failed` with `code` `limit_reached`, the plain usage reason naming the
covered step (or mission), the spent `limit`, and `endedBy` `archond`. It opens
no block or grant request and keeps every attempt's verdicts and outputs as
evidence. With `stop` (the default), the run
blocks with `code` `limit_reached`, the plain reason ("Review used 3 of 3
rounds", "The mission used 20 of 20 rounds, 1 of them granted", "Review used
30 min of 30 min", "Review used 51,230 of 50,000 tokens"), `resumePolicy`
`grant`, or `grant_or_pick` where the step can also pick its best attempt (see
[Picking the best attempt](#picking-the-best-attempt)), and the limit as `limit` (`kind` `rounds`, `time` or `tokens`,
`limitId`, `nodeId` the card's target, `used`, `max` counting grants,
`granted`; time in seconds). A peer conversation that reaches
its message count without an agreed result is stopped: its seats are told the
cap in their brief, posts past it are refused with "the peers have used every
round their Limit card allows", and the run blocks the same way, keeping the
journal.

`archon run resume <run> --grant` (API `grant: true`) gives the stopped limit
one more allowance and resumes: one more round, or the card's time or tokens
again. On a peer step's own card, whose rounds are journal messages, one
round is room for a proposal and every peer's acknowledgement, one message
more than the step has peers, so the peers can agree in it. The `run_resumed`
records `grant` (`limitId`, `kind`, `amount`: 1 round or a peer step's round
of messages, or the card's seconds or tokens) with the actor, and the run status's `resumePolicy` says
`grant` or `grant_or_pick` while such a block waits. A resume without `--grant`
at a spent limit (or without `--grant` or `--pick-best` under `grant_or_pick`),
with `--grant` at any other block, with `--pick-best` anywhere but a
`grant_or_pick` block, or with both, is refused with 409 and nothing is
recorded. There is no automatic loop detection.

A step has no time limit unless a Limit card's time covers it; the daemon
imposes no default. A downstream human gate waits after the formation finishes
and spends no step time.

The projection reports `running`, `waiting_human`, `blocked`, `succeeded`,
`picked`, `failed` or `canceled`. `waiting_human` means at least one human gate waits,
even while other steps still run; the events say what runs. Always check
`final` and `resumeAllowed`; a blocked run is not a completed delivery. `picked`
is final: the run reached a done End, but its work got there through a pick,
so it delivered failed work, not a pass (see [Picking the best
attempt](#picking-the-best-attempt)). A failed or canceled run names who ended it in
`endedBy`; why is in its run evidence problems. Events expose node, slot and gate identities,
attempt, status/verdict, session display name and cleanup outcome where
applicable. `run_succeeded`, `run_picked` and `run_failed` list in `endIds` the End nodes the
run's paths reached; a `run_failed` with code `path_rejected` names the
rejected End node as its `nodeId` and the routing gate as its `gateId`.
Typical sequences include `run_started`, `node_started`, `slot_dispatch`,
`seat_created`, `seat_prompt_consumed`, `slot_result`, `seat_cleanup`,
`gate_kind_result`, `gate_verdict`, `pick_started` (see [Picking the best
attempt](#picking-the-best-attempt)), and a run outcome. `node_output` carries
the `attempt` whose output it is, and every routed input, in `node_started`'s
`inputRefs` and `gate_evaluating`'s `inputRef`, carries `origin` (`nodeId`, `portId`,
`attempt`): the step attempt whose output it is, kept through gate pass and
fail routes. Malformed judges emit
`judge_attempt_failed`. Session-channel runs add `human_ask_delivered` and
`human_ask_fallback` (see [Human gates on the session
channel](#human-gates-on-the-session-channel)). Pending human requests appear in
`waitingGates` with `gateId` and `requestedSeq`; several can wait at once.

The public projection includes cwd, context paths, inputs and Bead ID but excludes prompt text,
artifact contents, brief paths, native session IDs and arbitrary private event
data. Projections and SSE stay sanitized; the run evidence routes under
[HTTP contract](#http-contract) serve a run's outputs, gate results, human
responses, briefs and artifacts to the operator.
Run status and lists carry `startedBy`, the run's driver, recorded as the
start event's actor. Every CLI command taking `--actor <name>` defaults it
to `ARCHON_DRIVER`, then `agent:archon`. The CLI alone resolves that
environment default and sends `actor` to the API. Historical runs keep their
recorded actor, displayed verbatim as the driver in the cockpit and CLI.
Plain status lists each input's name, kind and first line, up to 100 characters;
JSON keeps the full value. Status also carries
`startedAt` and `updatedAt`, the times of its first and latest ledger events; a final run ended at
`updatedAt`. CLI status and attempts render timestamps in local time and
elapsed time as durations; JSON retains the stored timestamps. Status names
the mission revision and stopped step (ID plus its frozen title), and attempts
include their stop reason. A stopped attempt uses the run's terminal reason
when that end stopped it, ahead of intermediate cancellation errors. A run
ending while blocked names the last blocked step, even when it has no open
dispatch; otherwise its active steps identify where it stopped.
Each waiting gate carries `requestedAt`, when it asked.
`run logs` is the same sanitized projection as `run status`. `run follow` prints
complete enveloped projections from SSE after durable changes, waits through
human gates and closes only at finality, exiting 0, or 7 for a `picked` run as
`run wait` does. Interrupting that client stops viewing,
not execution. Abort cancels only the selected run and waits for owned-seat
cleanup, including seats kept on call, before returning `canceled`. An abort
records the cancellation and seat cleanups only, never an executor failure or
a block.

### Picking the best attempt

A step's Limit card says with `whenSpent` what happens when it is spent
(archon-gihe): `stop`, the default, which TOML and JSON leave out, blocks the
run for a grant as above; `best` picks the best attempt the step's gate has
already judged and sends it on as if the gate had passed it. Best of N is a
step, its gate's fail route back to it, and a best card with N rounds.

Only a step's card can pick, and only where the step has a selecting gate:
exactly one gate whose `in` port that step alone feeds and whose `fail` route
returns to the step, decided by a judge (`formation`) or a `human`, since a
code check cannot compare attempts. Validation reports, as `invalid_limit`, a
best card on the mission ("Limit Cap picks the best attempt when spent, but it
covers the whole mission: only a step's card can pick; set it to stop or fail"), on a
step without a selecting gate ("Limit Cap picks the best attempt of Draft when
spent, but no gate judges it and sends it back: wire a judge or human gate that
only it feeds and whose fail returns to it, or set it to stop or fail"), two such gates
or a code-only gate, and any value other than `stop`, `best` or `fail`; admission
refuses the run with the same words. `limit create|update --when-spent
stop|best|fail` and the patch operations' `whenSpent` set it; another value is
refused with `INVALID_LIMIT` (HTTP 400). In the cockpit the card's window sets
it (When spent: Stop for a grant, Pick the best attempt, or Fail the run).
For `best`, the card reads "then picks the best", and Flow and the step's
window say ", then the best attempt goes on". For `fail`, the card reads
"then fails the run", and Flow and the step's window say ", then fails the run".

When any knob of a best card is spent, the step runs no more and picks among
its candidates: the attempts whose output the selecting gate judged with a
verdict of its own, each credited to the attempt named by the `origin` of the
input that verdict judged, so a verdict that arrives while a later attempt
runs still joins the attempt it judged. An attempt the limit cut short, or a
crash ended, has no output and is never a candidate; the time a crashed
attempt ran still counts on a time card. With no candidate the run blocks as a stop card's does; with one,
Archon takes it without asking anyone (`decidedBy` `archon`). A card picks by
itself only when the gate judged a candidate since the step's last pick, so a
pass route that leads back to the step cannot pick the same attempts forever;
such a step blocks instead.

The pick is recorded as `pick_started` (`nodeId` the step, `gateId`, `trigger`
`whenSpent` or `resume`, the spent `limit`, `candidates` with each
`attempt`, `outputSeq` and `verdictSeq`, and for a judge gate its
`judgeChain`); it settles the step's pending send-back as its output would.
Then the gate decides:

- A judge gate runs its judge chain once more, anchored at `pick_started`,
  on one input that shows every candidate side by side: the criterion, and for
  each attempt its verdict, reason and output (text, or the kept file or
  folder path). Every judge of the chain reads that input. A judge before the
  last recommends instead of picking: its prompt carries the recommend
  contract, and its output (the attempt it recommends, why, and what decided
  it) reaches each later judge below the candidates as "recommendation from
  <judge title>:". Judges number their attempts on, as for any judging, and
  each judge's start spends a round of a mission card that sets rounds, so a
  spent mission card blocks the pick at the judge it would start. The final
  judge decides with exactly one fenced block, instead of a verdict:

  ```archon-pick
  {"attempt":2,"reason":"Why this attempt is the best","evidence":["what decided it"]}
  ```

  `attempt` must be a candidate, `reason` a string and `evidence` an array of
  strings, and no other key is allowed. A malformed pick is asked for once
  more, as a malformed verdict is; a second blocks the run resumably with
  `code` `invalid_judge_pick` (`judge_attempt_failed` records the same code),
  and a plain resume asks the final judge again.
- A human gate asks the operator: `human_input_requested` carries `pick`
  (`nodeId`, `pickSeq`, `candidates`) with the side-by-side input. A gate with
  a judge and a human asks the judge chain first and offers its pick as
  `recommendation` (`attempt`, `reason`, `evidence`, `decidedBy`); the operator
  decides. The pending request appears in `waitingGates` with `pick`
  (`nodeId`, `candidates`, `recommendation` with `attempt`, `reason` and
  `decidedBy`), in `gate request` and its route, and in `run wait`, which
  prints the `gate pick` command. `gate request` and `run wait` add the
  judge's title on the frozen board as `recommendation.judgeTitle`, and `run
  wait` names the judge by it. `archon gate pick <run> <gate> --attempt <n>
  [--reason <text>] [--requested-seq <seq>] [--relayed-by <slot-id>]`, or `POST
  /api/runs/{runId}/gates/{gateId}/pick` with `requestedSeq`, `attempt`,
  `reason` and optional `relayedBy`, records it as a `human_verdict_recorded`
  pass with `pick.attempt`, `decidedBy` `human:operator`. A pick of an attempt
  that is not a candidate answers 400 ("not a candidate attempt: attempt 4;
  pick one of 1, 2 or 3"), a verdict on a pending pick or a pick on a pending
  verdict 409, and a stale request 409 as for verdicts. Like a verdict, the pick
  is recorded at once and routed between steps.

The pick ends in a `gate_verdict` pass whose `pick` names the step
(`nodeId`), the picked `attempt`, `pickSeq`, `candidates`, `decidedBy` (the
final judge's ID, the operator's actor or `archon`), `trigger`, and any
`recommendation`, with the picker's `reason` and `evidence`. It carries the
picked attempt's gate input, its `origin` included, down the gate's pass
routes exactly as the gate's own pass of that attempt would have, without the
picker's reason, so downstream steps and the run's mission
[outputs](#mission-outputs) receive that attempt's output. A mission output
bound to another port of the step, one that reaches a done End without
passing the selecting gate, is not part of the pick: it keeps the latest
attempt whose output on that port reached a done End, which can differ from
the picked attempt. Replay
finishes a pick the ledger started and did not end, from the judge outputs it
holds.

A run stopped at a spent limit whose step can pick (a selecting gate that
judged at least one attempt) carries `resumePolicy` `grant_or_pick`, whatever
the card's `whenSpent`: `archon run resume <run> --pick-best` (API `pickBest:
true`) picks instead of granting, and the `run_resumed` records `pickBest`.
`--pick-best` elsewhere is refused with 409 ("this block is not a spent limit
of a step whose gate judged its attempts, so there is nothing to pick"), and
so are `--grant` and `--pick-best` together. The cockpit's run bar offers Pick
the best attempt beside the grant at such a block.

A pick happens only when a step's limit is spent with no attempt passed, so a
picked attempt is failed work retained, never a pass ([ADR-0022](adr/0022-a-picked-run-is-not-a-success.md)). A run whose
work reached its done End through a pick ends with `run_picked`, not
`run_succeeded`, and its status is `picked`: following what reached each done
End back as [Mission outputs](#mission-outputs) does, through the inputs of the
attempts that produced it, the walk reached an output a pick carried on. The
event carries what `run_succeeded` would (`endIds`, `outputs`, `final`, and a
`reason` on resume) and `picks`, every pick that work came through in the
shape outputs carry it; each output a pick carried names it in its own
`picks`. A run that crossed a pick but whose work reached no done End through
it, such as a pick whose pass leads to a rejected End, ends as it would
without the pick: a rejected path still fails it with `path_rejected`, and a
required output never delivered with `missing_output`. `run wait` and `run
follow` exit 7 for a picked run, the final `run wait` paragraph says its
outputs are failed work, a final notification says the same, and the cockpit
shows the run as "Picked, not passed".

Run status lists the run's picks in `picks` (`seq`, `gateId`, `nodeId`,
`attempt`, `candidates`, `decidedBy`, `trigger`; the reason stays in the
evidence), and `run status` prints one line for each ("pick fmn_draft attempt
2, picked from 1, 2 or 3 by fmn_judge, went on through gate_review"). The
gate's node evidence lists `picks` (`seq`, `nodeId`, `trigger`, `candidates`,
`limit`, `judgeChain`, `judgeFailures`, `humanRequests`, `recommendation`, and
once decided `picked` with `seq`, `attempt`, `reason`, `evidence` and
`decidedBy`). The attempts route marks the picked attempt with `picked`
(`gateId`, `gateTitle`, `seq`, `reason`, `decidedBy`, `candidates`,
`trigger`), and `run attempts` prints "picked as the best by ... through ...".
A lab judge picks when its input holds one `archon-pick` block, as it judges
with an `archon-verdict` block.

### Waiting on a run

The agent that launched a run drives it by pulling; Archon pushes nothing into
its session. `archon --server <server> run wait <run>` blocks until the run
needs the driver, ends or changes, then prints one paragraph for the agent and
exits. Leave it running in the background and react when it returns:

```bash
archon --server "$ARCHON_SERVER" run wait "$ARCHON_RUN_ID" --until needs-you
```

`--until` takes `needs-you` (the default), `final` or `any-change`:

- `needs-you` returns when a human gate asks for a verdict or a pick, a blocking
  escalation is raised, or the run blocks with no blocking escalation to
  explain it. A waiting gate explains no block: it holds up only its own path. The paragraph names the gate or step, the gate's criterion, the start of
  its input (the whole input is `gate request`), where each verdict leads, and
  the exact `gate approve`/`gate reject` commands with `--requested-seq`; for a
  pick, the candidates, any judge's recommendation and the `gate pick` command;
  or the `run resume` or `run abort` command a block needs, with both `--grant`
  and `--pick-best` at a `grant_or_pick` block.
- `final` returns when the run succeeds, is picked, fails or is canceled.
- `any-change` returns at the next ledger event and lists the new events. When
  that event opens a new ask it answers as `needs-you` (exit 3) instead.

`run status` also shows a final run's outcome, terminal reason and `endedBy`.
Every wait mode returns at once for a final run, and says how it ended: its status,
the step it stopped at, the terminal reason and code from its evidence
problems, and who ended it. Every other answer ends with the command that
waits for what comes next, carrying `--since <seq>`, the ledger sequence the
answer covers, and `--json` when the wait used it. Pass it to the next wait:
an ask counts as new only after `since`, so a driver that loops sees each ask
once and misses nothing between calls. Without `--since` every open ask is new.
Other open asks are still listed as reported earlier.

A bare block counts only once the daemon has settled the run, since the
command that recorded it can still end the run, as a stop does. Until then the
run reads as `running`, and in every mode the cursor stops below the block and
an answer reports only the events before it; with nothing else new the wait
keeps holding. Once the run settles, a real block is a new ask in every mode.

A verdict never waits: the daemon records it at once, even while other steps
run (see [Human verdicts](#human-verdicts)). A resume sent while the run's
command is still settling waits for that command, up to five seconds, instead
of answering 409, so a driver can answer the moment a wait returns.

`--json` prints the daemon's answer (`runId`, `missionSlug`, `missionTitle`, `until`, `outcome`,
`since`, `seq`, `status`, `final`, `resumeAllowed`, `settled`, `end`, `asks`,
`changes`) with `next`, the next wait command (absent for a final run), and on
a lost daemon `error`. `outcome` is `final`, `needs-you` or `changed` from the
daemon, or from the client `timeout` (the last answer, with the cursor
unchanged) or `daemon-lost` (with `error` and the cursor unchanged). Exit
codes:

| Code | Meaning |
| --- | --- |
| 0 | The run is final (succeeded, failed or canceled). |
| 7 | The run is final as `picked`: its outputs are failed work a pick carried on. |
| 1 | Error, such as an unknown run or a `--since` past the run's last event. |
| 2 | Usage, or run without `--server`. |
| 3 | The run needs you, in `needs-you` or `any-change`. |
| 4 | The run changed, with no new ask (`any-change`). |
| 5 | `--timeout` passed first; the cursor is unchanged. |
| 6 | The daemon stayed unreachable for `--reconnect` (default one minute). |

A daemon that stops answers waits with 503. The client retries the same
`--since` every quarter second until the daemon is back, so a restart mid-wait
loses nothing; only a daemon still unreachable after `--reconnect` ends the
wait with code 6, printing the command that waits again from the same cursor.
One wait keeps one connection to the daemon across its polls, and the daemon
closes connections idle for two minutes. The daemon answers within a second of
the ledger event.

`run gates <run>` lists pending gate IDs, request sequences and asking seats.
`gate request <run> <gate>` reads the question and routed input. `run seats <run>`
lists the public seat projection, including session names, on-call requests and
what a seat is waiting on.
These three commands print readable text by default and the daemon envelope
with `--json`. Match a gate's `askedSeats[].createdSeq` to a seat's `createdSeq`,
then match its `sessionName` to `tmux list-panes` on the daemon host's configured
socket. This locates the asking pane without exposing private terminal IDs in
the API. In session mode the operator talks to that seat; it records the
confirmed answer with `gate approve|reject`, the current `--requested-seq`,
its own `--relayed-by` slot ID and the exact `--response` or `--response-file`.

The `tmux` executor uses one implementation with Claude Code and OpenAI Codex
adapters. It resolves authenticated harness executables from its environment.
Each fresh seat gets a pointer to a file under `<state-dir>/briefs`. The file
contains run/node/slot identity, the role (`agent: vanilla (no role)` for a slot without one), cwd, mission goal, Bead, role summary,
formation brief, file/link references, routed inputs, gate feedback, human
responses, output ports and their kinds, the attempt directory and completion instructions.
Orchestrated controllers also get their bound workers and may direct only those
workers; the workers' briefs name the same attempt directory. The operator may type into any live seat at any time, through the seat
terminal or in CHROTE, and talk to the agent normally, whether it is working a
dispatch or idle. The runtime pastes a brief only while the agent is idle and its
input line is empty, waiting within the step's time limit if it has one (a long wait is
recorded as `waiting_for_idle_input`, below), so a brief never lands
mid-turn or on the operator's unsent text. It submits the brief once its pointer
shows in the input line, however the harness wraps it, and confirms the harness
took it: the input line no longer holds the pointer. While the input line still
holds exactly the pointer, Enter is pressed again after a moment, up to five
presses; Enter is never pressed while the input line holds anything else, such
as text the operator added (recorded as `brief_not_taken`, below). A Codex
seat starts with its folder trusted for its launch only (`-c
'projects={"<cwd>"={trust_level="trusted"}}'`), so it shows no trust dialog
and Archon never writes the operator's `~/.codex/config.toml`. Should a
first-use trust dialog show anyway, it is answered while the seat has taken no
brief and its cursor is in no input line, as on the dialog; words on screen
that only quote it never are. Archon agents must not type into seats or manage
their sessions.

Sessions are named `archon-<run>-<slot>`, with the `--mission-label` value
before the run ID when it is set.
The runtime creates and cleans up seats by immutable session ID through the
configured guarded wrapper. The daemon's launch environment must include any
host-required cleanup override and reason. Wrapper path alone is insufficient.
Check every `seat_cleanup` outcome: `ended`, `left_socket_changed` or
`left_cleanup_failed`. Shutdown records `left_shutdown` when it detaches from
an owned seat without ending it, so the restarted daemon can reattach. The run
still owns that seat: its step's next attempt (cause `new_attempt`) or the
run's end, cancel included (cause `run_final`), ends it at once. Success does not erase a cleanup failure.
On a session-channel run `kept_on_call` leaves a finished formation's seat
running for its human gate. That seat's later cleanup records `ended`, `gone`,
`left_socket_changed` or `left_cleanup_failed`, and the ledger keeps the
`cause` when the runtime ended it.

Native completion must match the exact pointer, cwd, the slot's model/effort,
native session, and a natively finished agent turn carrying this run's
completion sentinel: Codex task completion, or Claude's end_turn assistant
message. A marker alone is insufficient, and another run's sentinel never
completes a dispatch. The operator's turns after the pointer, typed or queued
messages and interrupts, neither complete nor fail the dispatch, and the
completing turn may come after them. A turn that finishes without the sentinel waits for recovery.
For both harnesses, once a dispatched seat has an idle, empty input prompt
and no native transcript activity for five minutes, Archon sends one continue
instruction in that same seat naming the missing completion sentinel. Busy
panes, Claude background tasks awaiting their native completion notification,
and unsent operator input reset this quiet window; idle prompt animations
do not. This also covers API errors or lost responses with no native turn-end
record. Five minutes leaves room for delayed writes and normal pauses without
leaving overnight runs stuck indefinitely. A second five-minute idle window
without completion blocks the run with `seat_idle_without_sentinel` and a plain
reason; `run wait --until needs-you` returns it. Inspect the seat and resume
with `--mode redispatch` to run the step again. This is stalled-seat recovery,
not a time limit on a working step.
The dispatch
still fails loudly when the seat ends, when the model or effort changes, or when
the harness moves to another conversation (`/clear`, `/new` or `/resume`).

A step without a time limit never times out, so a seat that waits on something
Archon cannot end on its own is recorded instead. The idle recovery above
handles seats that have stopped working without completing.
Once such a wait has lasted a minute, the ledger records `seat_state` (`nodeId`,
`slotId`, `data.state`, `data.detail`, `data.since` and `data.dispatchId` once
dispatched), and records it again with state `working` when the wait ends:

- `seat_not_ready`: the harness has not reached its ready prompt;
- `waiting_for_idle_input`: the brief waits to be pasted because the agent is
  busy or the input line holds text the operator has not sent;
- `brief_not_taken`: the brief is pasted but not sent, because Enter did not
  send it or the input line also holds text Archon does not send;
- `turn_ended_without_sentinel`: the agent ended a turn without this run's
  sentinel;
- `background_work_pending`: Claude ended its turn with background work that
  has not resumed the conversation.

`run seats` shows a seat's current wait as `waiting` (`state`, `detail`,
`since`, `seq`) until the seat records anything else, and `run wait --until
any-change` reports each `seat_state` as a change with its `slotId`, `state`
and `detail`. Each tmux command is bounded at 30 seconds, so a wedged tmux
server fails the dispatch with that reason instead of waiting forever.
The `lab` executor creates no tmux sessions and echoes deterministic inputs.
It writes each rendered brief to `<state-dir>/briefs/lab-*.md`, as a seat would
receive it. It proves routing, not agent work or the truth of a review.

The cockpit lists a mission's runs from the daemon, so runs started by the CLI,
an agent or another browser appear. A mission lists only its own runs, by its
identity: a mission created again under a deleted one's slug starts with none,
and a link to a run of the deleted mission is reported and not shown
(archon-n7u.15). It shows the open run that most needs the operator: waiting
for a human, then running, then blocked, newest first. The navigator on the
left lists every mission with its runs (archon-o7p.2, archon-fh5c.4), open runs
by attention and then finished runs newest first, the first five shown and the
rest behind "N earlier runs". Every run row shows its local start time and
state, followed by its Bead or the start of its first input. Choosing one shows
it; a mission's Edit mission row puts the shown run
away and returns to the current revision for authoring. The run bar has a
reserved, fixed-height row even before selection and during loading, so
Canvas and Flow never shift when it appears or changes. The canvas creation
palette is absent from Flow. Gate verdicts read PASS and FAIL on Canvas, in Flow
and in attempts; run and formation statuses keep their own words. Canvas or Flow
is remembered once per browser across missions and runs. The old per-mission
preference migrates once from the first opened mission, then is removed. The run bar says when
the shown run started, how long it took and who drives it; a run waiting at a
gate says since when; and a run that ran an
earlier revision draws its frozen graph and run states on Canvas and Flow,
read only, with its cards where the current mission has them, and the dock
names the revision shown and offers Edit current mission. Every run that needs
the operator, waiting at a human gate or blocked, is counted (archon-n7u.29):
in the navigator's Needs you group, above the missions and across all of them,
and in the page title ("(3) Scouting · Archon"). The navigator's Sessions group
lists the sessions of open runs, and only those; choosing one shows its run and
opens that step's terminal. Waiting is the
most visible state: the run bar's badge is filled gold, and the gate waiting
for the answer has a gold ring and a "waiting for you" chip.
`/?mission=<slug>&run=<runId>`, the link that notifications carry,
opens that mission and keeps that run shown. The address bar keeps a chosen run
across reloads, and an unknown linked mission or run is reported rather than
silently replaced.

The cockpit shows what a run produced, read from the evidence routes. Each step's
card lists its latest output as chips: artifact files its ports name, the text
of ports that name no file, and the seat's report when it adds something. The
run bar's Produced list leads with a finished run's final steps (those whose
outputs feed nothing), or a running run's latest step, and a menu holds the
rest, grouped in run order by step and output with counts, outputs before other files.
A kept output copy replaces its working-path twin in the list; distinct paths
and attempts remain distinct. A formation that recorded output reserves its
produced-file row while evidence loads, keeping its card and open windows still.
Declared output chips open the producing step's attempts; other files open in a
floating file window, rendered by kind, with Open raw and Copy path (the
artifact's absolute path on the daemon host); several can be open side by side.
Node, note, file and attempts windows float in the free space nearest what
opened them, over the canvas or over Flow, shrinking to half their remembered size at most to
fit. A window leaves its own card or Flow row, its neighbours, its next links and
the title bars of open windows visible and clickable where space allows, and
cascades when no free space is left. Opening, closing or resizing a window never
moves Flow's rows. Menus opened from the run bar render above every window.

A step that ran at least once shows "N attempts" among its card's run tools and
in its Flow row, and its node window always shows the count. One click opens the
step's attempts window (archon-7963), read from the attempts route below. It
lays every attempt out oldest first, one column each, so the loop reads left to
right: the attempt's outcome and time, with a stopped or failed attempt's reason
in its column header, each gate's verdict and evidence (image evidence such as a judge's crops as thumbnails), then what each
output produced: images large enough to judge, text and Markdown rendered, and
other files as chips that open the file window. Run evidence uses the shared
floating window, with Escape closing an open document before the window.
Cited images appear as thumbnails in run evidence too; a relative citation
uses the same resolver in attempts, run evidence and gate evidence. It links
only to a known artifact, resolving against the producing attempt directory
and output folders, with cited reports disambiguating other matches.
Scheme-bearing URLs are not file citations. Evidence
windows keep a readable minimum of 640×600, even when that covers other cards.
A folder searches its nested files and previews videos first, then twelve images, preferring verdict-cited images and names
containing contact sheet or sheet, then at most three final images. It shows
each model once as a tile with its name, size and Open file action before
eight other files, and counts the rest. Folder model tiles load only when clicked; opening one
model file of at most 64 MiB loads it immediately. Videos fit the file window
with their native controls visible. Opening another model
unmounts the previous viewer and releases its graphics resources. Missing
textures use a neutral lit material with a note; an FBX export with black
untextured materials uses the same fallback. The note overlays the preview
without moving its frame. Attempt media fits its column, including side by
side, and long chip names wrap. Files above 64 MiB offer a download
instead of parsing in the browser; geometry-free FBX says "No mesh in this file".
Cited images, contact sheets and previewable media are preferred before filling a folder's file-list cap. The attempt the loop
settled on, the picked one or else the latest that passed, is marked green.
When the latest attempt stopped without output and none settled, the window
opens on the latest recorded output. A running attempt still opens on itself.
The arrow keys, Home, End or an attempt's number step between attempts; C puts
two of them side by side in frames of one size: the focused attempt and the one
before it, or the pair C left while the focus stayed on it. Any two can be
compared: from the strip, a shift-click on an attempt (its number or its
column) or shift with an arrow, Home or End puts that attempt beside the
focused one. Side by side, each side has its own row of attempt numbers above
its column; a click there chooses that side's attempt, the keys move the side
last chosen, and with shift they move the other. A click on an image zooms every
attempt's image of that output to the same spot at the image's own pixels, a
drag pans them together, and Z or another click fits them again. While the run
is live the window reads the attempts again as the run records them and follows
a new attempt, unless the operator stepped back to an older one.

A mission opens at a scale its card text can be read at: the whole graph when
that fits at 80% or more, else 80% from the graph's leading edge, and a run
opens centred on the card it has reached. Fit always shows the whole graph.

A step's window (archon-fh5c.6) opens on the first click of its card with the
step's staffing in one row a slot (role, harness, model, effort, each opening
its own list) and, once the run has seated the step, the seat's terminal filling
the rest of the window, one tab a seat; the brief line between them opens the
step's other fields. A gate's window states first what the run did there: the
latest verdict with its reason, or why there is none and the verdict the judge
returned. The terminal attaches to an owned live seat and sends typing
through the seat terminal WebSocket below. Its resizes change only the viewer's
own view; the seat keeps its pinned size. The
operator types to the agent whether it is working a dispatch, on call or idle.
Seat terminals are re-ported from CHROTE's terminal (archon-o7p.13.1). Each holds
the seat's native grid and fits its font to its window: the largest font up to
14px at which every row and column fits, down to an 11px floor. Below the floor
the grid scrolls, starting at its newest rows, with Start of line and End of
line controls when it is wider than the window. Resizing a window changes only
its font; no size reaches the seat. A seated step's window opens over the canvas at up to 90% of the workspace, keeping other
windows' title bars clear. It keeps the
terminal, connection and frame of every seat it has shown while it is open, so
switching seats preserves a healthy connection, and reads the step's seats again
every few seconds, so a new attempt's seat gets its terminal. A connection lost with the seat still live,
such as across a daemon restart, dials again once on Reconnect (Refresh in a
Talk window) or when the page becomes visible again; nothing retries on its
own. Every refused attach, including a stale or unknown seat, prints its reason in the terminal;
attach failure details are logged by the daemon. Painting a selection
copies it and the window says whether it reached the clipboard. A seat kept on call
for a human gate is marked on call, and
waiting for you while it holds a pending ask. On a session-channel run the
waiting gate's answer panel offers Talk with the asking formation, which opens
each asked seat's terminal in its own window beside the panel; a window says
when the decision is recorded. The terminal does not enter tmux copy mode, resize the
seat window, or end sessions. Closing the window disconnects only its own clients. Labels and controller/worker roles come from the run's frozen
graph; a new attempt has a new terminal URL. Scroll and selection happen in the
browser; the stream shows the native current screen and subsequent output,
without an API for reading old transcripts or entering historical tmux copy
mode.

Each new `seat_created` records its immutable tmux session/pane IDs and a private
socket/server identity. Linux socket peer credentials, process start time and
boot identity distinguish the original server from a replacement, even when
session IDs are reused. A seat without that proof is unavailable for
a terminal. A renamed session is still identified by its recorded immutable ID.
A step whose seats have all ended, gone missing or become unavailable shows its fields without a terminal.
A seat whose tmux server has exited is unavailable, because the original server
can no longer be proven.
Terminal support uses the daemon's existing `--socket` and `--tmux-bin`; the lab
executor exposes no live terminals. No generic session browser is provided.

## Gates and output contracts

Kinds run in order: code, formation, human, stopping on failure. Code supports
only `output_contains@1` and `output_absent@1`, configured through `check`,
`checkVersion` and `checkValue`. A draft gate may leave these blank; admission
reports the gap.

A gate's kinds are any non-empty combination of `code`, `formation` and
`human`. A new gate is `human` unless kinds are given: the cockpit editor
preselects Human, and `createGate` or `archon gate create` without kinds saves
`["human"]`, which runs as soon as it is wired. A code check on a new gate needs
the `code` kind. The cockpit editor, the `updateGate` mission patch and `archon gate
update` change a gate through one store path. They set only the fields given,
and an empty value clears one. A gate keeps only the configuration its kinds
use: dropping `formation` detaches the judge chain, as detaching the judge does,
and dropping `code` clears the check. Detaching the judge from a gate whose only
kind is `formation` leaves a `human` gate. Adding `formation` without a chain
leaves a draft finding until a judge is attached. Converting a code gate to a human
gate is `archon gate update <mission> <gate> --kinds human`; `--clear-check`
clears the check while keeping the code kind. It does not run arbitrary shell commands.
Use a judge formation to execute checks such as Beads lint or code review.

The runtime appends the verdict contract to every judge seat's prompt alongside
the output contract. A judge must emit exactly one fenced block with exactly these keys:

```archon-verdict
{"verdict":"pass","reason":"Required checks passed","evidence":["check report reference"]}
```

`verdict` is `pass` or `fail`, `reason` a string, and `evidence` an array of
strings. On a missing, duplicate, extra or malformed field/block, the runtime
re-prompts the same live judge seat once with the parse error to correct its
response. If the correction is still invalid, the run blocks with
`resumeAllowed: true`; neither branch runs. The parse error names the missing,
extra or repeated key, and the gate's evidence shows each rejected verdict
block as the judge wrote it. Resume reruns the failed final
judge in a new attempt, retaining earlier judges' completed reviews. `run resume
--take-fail` (`takeFail` on the resume request) settles the gate as a fail
instead, with no further judge run: the verdict's reason names who took it and
what was rejected, and the fail routes as any other. It is refused at any other
block. A blocked run's projection carries the block's `blockCode`, here
`invalid_judge_result`, and the gate's window offers both as Ask the judge
again and Take it as a fail. A judge also emits its ordinary
output block, without embedding a second verdict block in it. A chain's judges
run in order, each judging the one before; resuming a chain a crash or a lost
seat interrupted runs only the judges that have not yet answered the gate's
current evaluation.

A fail edge delivers typed feedback containing gate ID, gate attempt, verdict,
reason, evidence and original input text/reference. The next prompt renders
this as a gate-feedback section.
A pushback also holds when the failing gate was fed by another gate's pass:
resume never re-delivers an input a gate has already evaluated, so the pushback
target runs first.

### End nodes and how a run finishes

Every route leads somewhere (archon-o7p.10). Each formation output and each
gate's `pass` and `fail` lead to a step, a gate or an End node; an End node
ends that path on purpose with outcome `done` or `rejected`. Validation reports
a route that leads nowhere as the error `route_leads_nowhere`, worded "Brief
sign-off's pass route leads nowhere: wire it to a step or an End node" (or
"Draft's output leads nowhere", naming the output's label when the step has
several). Drafts with such routes save and stay editable; admission refuses a
run whose path holds one, with the same words. A single step's run ignores that
step's routes. Validation also warns, as `unreachable_node`, about each step,
gate or End node no path from the Input card reaches: "No path from the Input
card reaches step Orphan, so no run will get there; wire a route into it or
delete it".

A run finishes when every path has ended and nothing else can still run: every
formation has produced output since the last delivery it received (a node's
output on a wired port, or a verdict's route, including a send-back), every
gate has evaluated the last input it received, and no formation, gate or human
request is still open. A delivery to an End node is not work; it ends that
path. A formation reached only through a route no verdict took is not pending
work. Resume first runs whatever is still owed, including a send-back a gate
routes during that resume, and finishes the run when nothing is; the same rule
applies on first execution and after a restart. A formation that received
some of its inputs but can never receive the rest is starved, not work that can
still run: once nothing else can run, a rejected path fails the run as below
(the rejection is why the join never ran), and without one the run blocks
non-resumably with `reachable_node_starved`. Any other work that remains blocks,
resumably, with `run_work_unfinished` naming those nodes.

A finished run succeeds unless a path ended at a rejected End node, or its
work reached a done End through a pick, when it ends `picked` (see [Picking
the best attempt](#picking-the-best-attempt)). At a rejected End it
records `run_failed` with `code` `path_rejected`, the End node's `endId`, the
routing gate's `gateId`, and as `reason` that gate verdict's reason (a human
gate's response, a judge's or code check's reason); the operator who rejected
is who ended it. A rejected path does not stop other branches: they run to
their own ends first, and only then does the run fail. A rejected End reached
from a step's output, with no gate, fails the run with the reason "the path
ended at <title> (rejected)". A human gate's answer panel says whether a
verdict ends its path, and whether the run then succeeds or fails, by the same
rule.

### Human verdicts

A human kind waits for an explicit verdict naming the exact pending sequence;
stale or duplicate decisions return HTTP 409. There is no default verdict. A
gate that evaluates a newer input while its request waits replaces that
request, and a verdict naming the old one answers 409.
The verdict's `reason` is the operator's response, preserved verbatim including
leading/trailing whitespace and newlines. On pass, a nonempty response
travels on every pass route together with the gate's original input. It is typed
with gate ID, gate attempt, requested sequence, deciding actor and text,
along with the approved input's reference and text. The next prompt renders
these as a human-response section naming the approved input, even when newer
unread work has reached the same port or earlier responses travel with it
(archon-qtq1). An empty response routes the input unchanged. On fail the response
becomes the feedback reason. Resume rebuilds the response from the verdict recorded for
that exact request, so it survives restart.

A human gate blocks only the work it gates (archon-o7p.11). While its request
waits, the run keeps dispatching every step not behind it, one step at a time,
and several requests can wait at once, each answered on its own and in any
order. The daemon records a verdict at once, while other seats work, and the
run routes it between steps: once the step running when it arrived has
recorded its output and that output has moved on. A step's output moves on
along its connections one at a time, and a code or judge gate it reaches is
evaluated right there, its verdict routed before the output's later
connections. Replay after a restart makes every delivery in that same order
and writes nothing to the ledger, except that it finishes a gate evaluation
the restart cut short, at the point the run would have; a verdict recorded
while a step worked waits for that step's output even when a restart cut the
step short and it runs again, so a run reaches the same end, each step on the
same inputs and the same steps in the same order, wherever a restart falls. A send-back to a step still
working its first attempt runs that step again once the attempt ends. When two
deliveries reach one port before its step runs, nothing is lost: the newest
work is the input, a send-back that meets unread work keeps that work as the
input, and every gate feedback and human response the step has not run on
travels with it, newest first (`feedback.earlier`, `response.earlier`); the
prompt shows the work and each of them. A run
whose only open work is waiting gates waits, with no block, and a verdict that
ends its path ends the run only once every other path has ended. A verdict is
also accepted on a blocked run, which routes it when it resumes, and a blocked
run resumes while its gates keep waiting.

The cockpit shows a pending human gate's input with a response box, Approve and
Send back. A pending best-of-N pick (archon-gihe) is answered in the
step's attempts window instead: it opens by itself on the judge's
recommendation with its reason, sets apart the attempts that are not
candidates, and its one button, Pick attempt N, records the focused candidate
with an optional reason. A recorded pick closes the window and shows on the
attempt as picked, by you or the judge, with the reason. The cockpit names a
judge by its step's title, never its node ID. The run bar names every gate that waits and every step that runs
("waiting for you at Review · running Draft"); choosing a waiting gate there
opens its answer.
A verdict may carry `relayedBy`, the slot ID of the seat that typed the
operator's confirmed decision (the slot ID rule: a letter or digit, then up to
63 letters, digits, underscores or hyphens): `archon gate approve|reject ... --relayed-by <slot-id>`.
It is stored on `human_verdict_recorded` and served beside `decidedBy` on the
gate's recorded decision in the run evidence route; `decidedBy` stays
`human:operator`. Any other value returns HTTP 400 and records nothing, and a
verdict without it is unchanged. The cockpit's run evidence shows a relayed
decision "via" the slot's agent.

Real formations must emit all and only their declared output IDs in one block:

```archon-outputs
{"port_out":{"text":"Short result"}}
```

A text payload can also include `ref` naming a text file anywhere on disk,
preferably under the prompt's attempt directory. Use its full absolute
filesystem path; a relative `ref` is refused with `invalid_output_ref`. A
missing, unreadable, non-text or oversized reference rejects the output. To
route only a file or folder path on a text port, put its absolute path in
`text` and omit `ref`. A file or folder port is the way to keep the artifact.

A missing or invalid output block, unknown or omitted port, invalid or oversized text ref
gets one correction turn on the same live seat before
blocking routing. The prompt names the rejected contract and declared ports,
the text-ref byte cap, and how to return a path as text. The fresh complete
response replaces the rejected one; a judge preserves its verdict or pick.
A second invalid output blocks the run. `run status` and `run logs` in text
mode show the block/error code and message.

The prompt names the attempt directory, `attempt artifact directory:
<state-dir>/.archon/artifacts/<runId>/<nodeId>/<attempt>`, which Archon
creates before dispatch for every attempt, judges included, and tells the seat
to write the files it produces there. It also says what each output port
carries. On a `file` or `folder` port, `ref` is required and is the absolute
path of the file (any type) or directory the output is, and `text` is a one-line
summary. A blank, relative or missing path, or one that is not a regular file
or a directory as the kind says, blocks the step with `invalid_output_ref` or
`unavailable_output_ref`, and the output is not recorded. When the output is
accepted, Archon keeps a copy of it in the attempt directory, at
`outputs/<port-id>/<base name>`, wherever the seat wrote it, the attempt
directory included: a copy-on-write clone (`FICLONE`) where the file system
supports one, otherwise an ordinary copy, never a hard link. Only a path the
seat itself wrote under `outputs/` is kept where it is. In a folder, a symlink
to something inside the folder stays a link (to the copy), a symlink that
leads out of the folder is replaced by a copy of what it points at, a dangling
one is kept as it is, special files are left out, and the run's artifact
directory inside it is not copied. The kept copy is read-only, its files and
directories alike. Its path is what the ledger records, the next step
receives and run evidence shows, so neither a later attempt that rewrites the
same workspace file in place nor a step handed the path can change an earlier
attempt's output. A brief whose inputs include a kept file or folder says so:
"Input paths are kept copies of earlier work and are read-only; write new
versions in your own attempt directory." Deleting a run's artifacts takes
`chmod -R u+w` first. Snapshots are always taken and live with the run's
artifacts; a large folder output is copied in full. The copy runs after the
step's seats finished, outside its time limit, and a run cancel or daemon
shutdown stops it. A copy that fails or is stopped does not lose the
attempt: the run blocks (code `output_not_kept`, or `coordinator_shutdown`
for a shutdown) with the attempt's output in the block, and `run resume`
copies it again and routes it without running the step again; resume in
`redispatch` mode runs the step again instead. The resumed copy reads the
paths as they are then. Routed
data is never redacted: inline payload text, a ref file's text, controller
plans and peer openings reach the next step exactly as the seat wrote them.
Redaction applies only to text Archon displays to a person (run evidence, file
windows). Free-form answer text is not
routed. Finish with the exact run ID substituted in the sentinel:

```text
<<<ARCHON-DONE run-id=<run-id> status=ok artifact=<path-or-ref>>>
```

For a lab judge smoke test, put one synthetic `archon-verdict` block in the run
brief. The lab echo carries it to the judge parser. Peer and orchestrated
formations echo it once per seat; the lab keeps one copy of identical blocks,
so the fixture reaches a judge downstream of them, while differing blocks
still block. Label that evidence as simulated; a plain brief without a verdict
will block at a formation gate.

### Peer conversations

A peer formation collects one independent opening from every configured seat
before sharing them. Those same seats then converse concurrently through an
append-only journal scoped to the run, formation and attempt. There is no fixed
turn order or facilitator, and no message count unless a Limit card covers the
step (see Execution). The seats use the supplied local
`archon --workspace <state> peer` commands to read, post, wait, propose and
acknowledge; direct file writes are outside the protocol. The commands lock
the attempt's directory while they read or write, so the journal is the only
file there and the run's produced artifacts list no coordination file.

Any peer can propose the full output. Every peer, including its author, must
acknowledge that proposal; a contested proposal needs revision and fresh
acknowledgements. A result may accurately preserve unresolved tensions and ask
the operator to decide. The acknowledged result still must satisfy the normal
declared output ports. Acknowledgement of that text does not decide a human gate.

A peer step under a time card spends it on startup, openings, discussion and
finalization. Participants receive the deadline and must leave time to finish;
without a time card the conversation runs until it agrees. Running out of time
without a completed valid result blocks visibly at the card and retains the
journal and completed openings. A restart does not replenish the time;
unresolved multi-seat execution requires inspection. See
[ADR-0020](adr/0020-peer-conversations.md) for boundaries and lifecycle.

### Human gates on the session channel

A run freezes its Input card's `humanChannel`
([ADR-0019](adr/0019-human-channel-agent-session.md)). On `notify` every ask
goes to the notify command. On `session` a human gate's ask goes to the agents
whose work the gate judges, while escalations, blocks and final outcomes still
go to the notify command. Session delivery works with or without
`--notify-command`.

On a session-channel run, a formation whose output reaches a human gate through
gates alone (pass and fail routes, never a judge port) keeps its seats when it
finishes, recorded as `seat_cleanup` outcome `kept_on_call`. Solo and peer
formations keep every seat; an orchestrated formation keeps its controller and
ends its workers. Judge chain members, and formations with no such path, end
their seats as on `notify`.

The asking formation is the nearest formation behind the request's gate input,
followed back through gates. The kept seats of its latest attempt receive the
ask: every seat of a solo or peer formation, the controller of an orchestrated
one. Each receiving seat gets its own brief at
`<state-dir>/briefs/gate-<run>-<seq>-<slot>.md`, pasted as a one-line pointer
only while the agent is idle and its input line is empty, and submitted once
it shows in the input line, however the harness wraps it. A seat not yet
reached is tried again within seconds. The brief names the run, gate, criterion,
pending request, asking formation and the decisions already recorded, with
these commands for the seat's own slot. `<archon>` is the `archon` installed
beside the daemon, so the command matches it, or `archon` on the seat's PATH
when none is:

```text
<archon> --server <server> gate approve <run> <gate> --requested-seq <seq> --relayed-by <slot> --response RESPONSE
<archon> --server <server> gate reject <run> <gate> --requested-seq <seq> --relayed-by <slot> --response RESPONSE
```

It tells the agent that only the operator decides. A complete, unambiguous
operator verdict for this pending gate, with the exact response to record,
is itself confirmation. The agent records those words immediately, for either
approval or send-back. If the agent drafts or paraphrases the response, or the
verdict, response or intended gate is ambiguous, it shows the proposed verdict
and exact response together and waits for confirmation. It must not infer a
verdict from discussion or invent missing response text. The instructions also
say to run the same command again a few seconds after a 503 while the daemon
restarts, and to tell the operator that another seat or the cockpit decided
first after a 409 `human gate request is no longer pending`. The formation brief's limits still apply, except for that command.

Each pasted ask is recorded as `human_ask_delivered` with the request sequence,
gate, asking formation, slot, the seat's created sequence, session name and
brief path. An ask falls back once, recorded as `human_ask_fallback` with a code
and reason, when no kept seat can receive it (`lab_executor`,
`no_asking_formation`, `no_receivable_seat`) or when every seat that received it
is gone while the request waits (`asked_seats_gone`). An ask is sent as a
brief is: Enter again while the input line holds only the ask, never while it
holds anything else. If a paste may have changed a seat's input but the ask is
not seen leaving the input line in time, or a tmux command fails, the ask
falls back with `delivery_uncertain`. Automatic delivery then stops for that
request without clearing the input or pressing Enter again. The operator can
answer in the cockpit or inspect the seat. The fallback records the affected
seat's immutable `seatCreatedSeq`. Later asks also skip that seat, including
after restart, and fall back if no healthy receiver remains. Other peer seats
and replacement seats remain eligible. A verdict arriving during the failed
paste does not discard that seat identity. An agent that is merely busy, or
unsent operator text found before a paste, still waits without a fallback.
Only after a fallback does the notify command, if configured, get its
`human_gate` notification. Asks are delivered as soon as the gate asks, while
other steps still run. Each delivery or fallback is recorded only while its
request still waits and its seat is still kept, checked as it is appended, so
it never lands after the verdict or seat end it raced with, and replay ignores
them.

Kept seats are reconsidered when the run settles and before a formation is
dispatched. A kept seat ends when it has received an ask and neither an open
request nor a verdict the run has yet to route names its formation (cause
`ask_answered`), when its formation starts a new
attempt (`new_attempt`), or when the run is about to succeed, fail or be
canceled, including an abort of a waiting run (`run_final`). Ending waits up to
60 seconds for the agent to go idle with an empty input line, the check every
paste waits on, and kills only that session, by immutable ID, through the
guarded wrapper. Those cleanups are recorded before the final event. A blocked
run's kept seats end just before the cancel or failure that ends it; the ledger
accepts that `seat_cleanup` after `run_blocked` (see
[Shutdown and recovery](#shutdown-and-recovery)). A blocked run otherwise keeps
its seats: a seat found gone while it is blocked is recorded after it resumes, and so are asks due while it was blocked.

Daemon shutdown leaves kept seats running. At startup, and every 15 seconds
while a run keeps seats or has open asks, the daemon checks each kept seat's
recorded pane, session ID and tmux server identity. A seat that is gone, or
whose server changed, is recorded as `gone` or `left_socket_changed`; a check
that cannot tell leaves the seat on call.

The projection carries `humanChannel` (`notify` or `session`). Each waiting gate
lists `askedSeats` (`nodeId`, `slotId`, `createdSeq`, `deliveredSeq`) and, after
a fallback, `fallbackReason`. `onCallSeats` lists the seats kept on call and not
yet ended (`nodeId`, `slotId`, `createdSeq`, `keptSeq`, and `waitingOn`, the
pending `{gateId,requestedSeq}` asks the seat received). Ask events carry
`requestedSeq`, and a fallback event's `outcome` is its code. The event stream
sends the same projection.

For long answers, the operator may give an explicit verdict and name a UTF-8
file as the exact response. The seat uses `--response-file FILE` instead of
`--response`, recording the complete file text verbatim and unabridged. A file
alone does not imply a verdict. The CLI rejects invalid UTF-8, unreadable files
and combinations with `--response` before any request.
The cockpit can load a local UTF-8 file into its editable answer box; import
failures leave the current draft intact. An unedited import retains all file
text. Editing follows the browser textarea's normal newline behavior.

## Operation

For authoring, running and following missions, use the [Archon skill](../skills/archon/SKILL.md).

### Daemon configuration

`<archon-server>` must be an HTTP URL with a literal IP, no trailing slash,
credentials, query or fragment. Source, binary, UI, state, socket and transcript
paths are host values, never mission constants.
For build and launch commands, see [lab setup](../skills/archon/references/lab-and-examples.md#build-and-launch).

For real seats select `--executor tmux` and supply `--socket`, `--tmux-bin`,
`--codex-transcripts`, `--claude-transcripts` from host configuration.
The daemon has no default working directory: every run, a mission's or a
single step's, works in the cwd its start names or an automatic workspace, as
described above, and records it in `run_started`; completed-turn recovery
checks the native turn against that recorded cwd. `--mission-label` is
optional. The daemon sets no step time limit; a Limit card
does.
Repeat `--listen` for each trusted interface. `--agents-dir` overrides cards;
installed daemons find `../share/archon/ui` beside their `bin` directory.
Set `--ui-dir ''` to disable the cockpit, or an absolute path to select another
build. These are daemon flags, not model settings. Set harness, model and effort on each slot.
`--notify-command` and `--cockpit-url` configure needs-you notifications,
described in [Needs-you notifications](#needs-you-notifications). The cockpit opens any file a mission,
brief or gate references by absolute path (see Referenced files).

`GET /healthz` answers `{status, version, commit}`: `ok` and the running
build's version and source commit, which a deploy checks against the commit it
admitted (archon-1ea). `archon --server <server> version` prints the CLI's build
and the daemon's, and the cockpit's top bar shows the daemon's.

Import copies or symlinks mission and notes TOML; there is no import command.
For the walkthrough, see [import an example mission](../skills/archon/references/lab-and-examples.md#import-an-example-mission).

### Authoring commands and editor behaviour

Author through the cockpit, or with Archon's mission, formation, gate, tool and
agent nouns offline (`--workspace`) or through the daemon (`--server`).
Every noun and command explains itself (archon-n7u.33): `archon -h` lists the
nouns, `archon <noun>`, `archon <noun> -h` and `archon <noun> help` list a
noun's commands, and `archon <noun> <command> -h` prints the command's usage,
what it does and its flags, offline and with `--server`. A noun's unknown
command is named and its commands listed; a command that needs the daemon says
so offline, and one that works only offline says so with `--server`.
The skill ships under `share/archon/skills/archon/` in a release
(`$ARCHON_SHARE/skills/archon`).
For skill discovery setup, see [install the skill](../README.md#let-agents-drive-archon).

Arrange (`mission arrange`, the cockpit's Arrange) rewrites only the layout. It
lays columns along the run from the Input card (its out port, formation and
Tool outputs, gate pass), ignores fail edges back to earlier steps and judge
wiring, places judge formations below their gate, and puts nodes the Input card
does not reach after the main path. The same mission always arranges the same
way. The canvas computes this same arrangement for nodes without saved
positions, preserving saved positions verbatim. Opening or fitting a mission
does not save that arrangement or change its revision.
`mission new <slug>` creates an empty mission and `mission create <mission>`
adds its Input card. `mission create`, `formation create`, `gate create` and
`end create` print `created <id>`, or with `--json` `{mission, layout,
inputCard|formation|gate|end}` naming the new node. `archon end create <mission>
[--outcome done|rejected] [--title <title>]` adds an End node (outcome `done`
and title Done or Rejected by default; any other outcome is refused); wire a
route into it with `archon formation wire <mission> <node:port> <end-id>:in`.
`archon end update <mission> <end> [--title] [--outcome]` and `archon end
delete <mission> <end>` change or remove one. The mission patch operations are
`createEnd` (`title`, `outcome`, `x`, `y`), `updateEnd` (`id`, `title`,
`outcome`) and `deleteEnd` (`id`). On the canvas, drag the End token from the
canvas dock or right-click the canvas (End node · done or rejected); an End card
takes any number of wires into its one port, its window and right-click menu
change its outcome, and a finished run lights the End nodes its paths reached.
`archon limit create <mission> --target <step|input> [--rounds <n>] [--time
<duration>] [--warn <duration>] [--tokens <n>] [--when-spent stop|best|fail] [--title <title>]` adds a Limit card covering a step, named by ID or title, or the Input
card (`input`, or its ID) for the whole mission; it prints `created <id>`, or
with `--json` `{mission, layout, limit}`. `archon limit update <mission>
<limit> [--target] [--rounds] [--time] [--warn] [--tokens] [--when-spent] [--title]` changes only what
it names (an empty `--rounds`, `--time`, `--warn` or `--tokens` clears that knob, `--target
''` unwires the card) and `archon limit delete <mission> <limit>` removes it. The patch
operations are `createLimit` (`title`, `target`, `rounds`, `seconds`,
`warnSeconds`, `tokens`, `whenSpent`, `x`, `y`), `updateLimit` (`id`, and any of `title`, `target`,
`rounds`, `seconds`, `warnSeconds`, `tokens`, `whenSpent`; an empty target unwires the card, a knob
of 0 clears it and `whenSpent` `stop` or empty stores the default) and `deleteLimit` (`id`). `--time` and `--warn` take whole
seconds as a duration such as `45s`, `30m` or `1h30m`. A write naming a target
that is not a step or the Input card, or a negative knob, is refused with
`INVALID_LIMIT` (HTTP 400); an unwired card saves as a draft.
Validation reports, as the error `invalid_limit`, a card wired to nothing
("Limit Cap is wired to nothing: wire it to a step, or to the Input card for
the whole mission"), a target that is not a step or the Input card, a second
card on one target ("Review has two Limit cards, Cap and Guard: keep one"),
a rounds, seconds or tokens value that is not a positive whole number, and a warning
without time or not shorter than the time ("Limit Clock warns with 5 min left
of 5 min, before any work: warn with less time left"); it warns, as
`empty_limit`, about a card that sets no knob. Admission refuses a run whose
mission holds an invalid card, with the same words. On the canvas, drag the
Limit token from the canvas dock onto a step or the Input card, or onto empty
canvas, or right-click the canvas (Limit card); a new card sets no limit and
opens its window. The card states its knobs ("at most 3 rounds", "at most 40
journal messages" on a peer step, "at most 20 step runs" for the whole mission,
"at most 30 min of work", "at most 3 rounds · 30 min", "at most 50,000
tokens") and its warning ("warns at 5 min left"), says what it covers, and has
a dashed tether to its target; drag its handle onto a step or the Input card to
rewire it. Its window edits the title, target, rounds, time (such as 45s, 30m
or 1h30m), warning and tokens, and says how tokens count and that the count is
approximate; its
right-click menu covers the whole mission, unwires it or deletes it; and every
edit has its own undo entry. Flow and the covered node's window state the
limit in words ("the whole mission may work at most 2 h"), and a step's window
lists the time warnings its seats were given. A run stopped at a spent card
offers a grant in place of Resume that says what it gives: Grant one more
round, or the card's time or tokens again, Grant 30 min more; where the step's
gate judged attempts, it also offers Pick the best attempt (see [Picking the
best attempt](#picking-the-best-attempt)). `mission list` lists missions; `formation list <mission>`
lists its formations with their slots and staffing. `mission inspect <mission>`
prints the whole mission, `mission inspect <mission> <input>` prints the Input
card with its reachable chain, and `formation inspect <mission> <formation>`
one formation with its slots, ports, brief and the connections at its ports,
then one line per slot with its staffing (`vanilla · claude-code · opus · low`).
Nodes keep their IDs when edited: `archon formation rename <mission> <formation>
<title>`, `archon mission update <mission> [<input>]` with `--title`, `--goal`,
`--file`, `--human-channel` or `--input-hint`, and `archon gate update --title` change only what
they name, and an empty value clears a field. `<input>` names the Input card
and may be left out when the mission has one. The Input card's input hint
describes the implicit `brief` input of a mission that declares none; Start
mission shows it. `archon mission input <mission> <name> [--kind
text|file|folder] [--required|--optional] [--description <text>]` declares an
input or changes only the flags given, `--delete` removes it, and without a
name the command lists the run's inputs; the patch is `updateInputCard` with
`inputs`, which replaces the list. Its `humanChannel`
(`--human-channel` on `mission create|update`) records how its runs' human gates
reach the operator ([ADR-0019](adr/0019-human-channel-agent-session.md)):
`notify`, the default, or `session`. `notify` and an empty value store no
channel, any other value is refused with the allowed values and nothing is
saved, and a run keeps the channel of its frozen mission. On `session`, human
asks reach the asking formation's kept seats; only a recorded delivery fallback
sends them to `--notify-command`. Escalations, blocks and final outcomes use
the notify command on either channel. Input cards and gates carry
reference files, such as a gate's rubric, the way formation briefs do: `--file
<path>` on `mission create|update` and `gate create|update` (API `files`),
repeated for more. On update the given files replace the list, and `--file ''`
clears it. Name each file by its absolute path: a relative path has no base, so
authoring refuses it, on these and on `formation set-brief --file`, with
`RELATIVE_FILE_REFERENCE` (HTTP 400, CLI code `relative_file_reference`),
worded `file "rubric.md" is relative: use an absolute path`; a node window's
Files field says the same before it saves. Clicking an
Input card, formation or gate card opens its node window, where every field is
read in full and edited in place: titles, the Input card's goal, inputs, input hint
and files, a formation's type, brief and staffing, and a gate's kinds,
check, criterion, judge and files. Each save is one mission edit with undo. Ports, edges, layout and notes are
unchanged.
Undo and Redo are visible toolbar words with Ctrl+Z and Ctrl+Shift+Z.
Every canvas edit that changes the mission is one undo entry. Undo reverses
the newest; Redo restores the fields and identities Undo reversed. A new edit
clears Redo, and switching missions clears both histories. A card or wire click
selects it; Delete or Backspace on the focused canvas removes the selection
through the same undoable delete, never from a text field.
A wire dropped on a card body reaches its free input; a role reaches its first
empty slot. A card without a matching target explains at the drop point.
Creation dialogs focus their title field with the default selected. Formation
creation opens its title editor before the create response, retaining the same
draft and input while the persisted identity arrives. End and Limit cards keep
their instant defaults; an opened title editor selects the default for typing. Deleting an Input card, formation, gate or End node is undone by HTTP
`restoreNode` (with `inputCard`, `formation`, `gate`, `end` or `limit`), which puts the node back with its IDs, fields, staffing, ports,
connections and position in one revision; its notes and wire lanes, kept by
ID, apply again. Removing a port is undone by `restorePort`, which puts it back
in its place with its connections. Undo waits for edits still being saved. When
another editor changed the mission first, undo reloads it and tries once more.
An undo the mission no longer allows is reported once and dropped from the
history, so older entries stay reachable.
`archon formation set-type <mission> <formation> <solo|peer|orchestrated>` and the
type chip on a formation card change its type in place. Solo keeps one slot,
peer has at least two slots with no controller, and orchestrated has one
controller (the existing one, else the first slot) and a worker. Added slots
are empty and bound agents stay on the slots that remain. Changing to solo with
more than one staffed slot is refused until `--keep-slot` (API `keepSlotId`)
names the slot to keep; the cockpit offers one choice per staffed slot. Undo
restores the previous slots exactly when they fit the type. Supplied slot
snapshots and restored nodes must have exactly one solo slot, at least two peer
slots with no controller, or an orchestrated controller and at least one worker.
A solo snapshot's controller flag is allowed. A snapshot may remove empty slots;
omitting a staffed slot requires `keepSlotId` naming the sole retained current
slot for solo, or emptying the omitted slots first. Validation and admission
report imported shapes that break these rules, including formations outside the
selected run path.

Solo, peer and orchestrated are the only formation types. Creating or changing
to any other type fails with `UNSUPPORTED_FORMATION_TYPE`, listing the three.
Every slot ID in a mission is
unique, because a seat's session is named after its run and slot and a relayed
verdict names its slot. Authoring generates a fresh ID for each new slot, and
restoring slots cannot take another formation's ID. A hand-written or imported
mission that repeats one gets `duplicate_slot_id` on each formation holding it,
from mission validation and run admission. A slot ID is a letter or digit, then
up to 63 letters, digits, underscores or hyphens, wherever it is written or
named: generated IDs fit, restoring slots refuses any other, and a hand-written
one that breaks the rule gets the error `invalid_slot_id` on its formation from
validation and admission, so every slot a run admits can relay its own gate
decision (archon-1ds).
`mission validate` lists every finding for the whole mission, admission checks
included, as `ERROR`/`WARN` lines naming each node by title and ID (`Draft
(fmn_draft)`, or a connection with its ends) or `--json`, and exits 1 on any
error.
`mission run` and `formation run` print every admission finding when a start is
rejected. The cockpit tags incomplete nodes as drafts and highlights the nodes
a rejected start names.

### Run inspection

`run list` prints one line per run, newest first: its ID, mission, status,
Bead (`-` when none), start, last change and driver, tab-separated; `--json` prints
each run's full projection. Starting a run without `--cwd` allocates a workspace
automatically; `--cwd` selects an existing project.

For launch, follow, output chaining, abort and verdict commands, see [run a mission](../skills/archon/SKILL.md#run-a-mission).

### Shutdown and recovery

For restart, resume and redispatch steps, see [recover a run](../skills/archon/references/recovery.md).

On restart with the same state directory and configuration, before
listening the daemon scans non-final ledgers, recovers eligible completed native evidence
or records a block naming unresolved dispatches. It never adopts or cleans up
old seats, except that it verifies seats kept on call and keeps managing them
(see [Human gates on the session
channel](#human-gates-on-the-session-channel)). Preserve those identities for
host-owner inspection. A seat shutdown left working remains associated with
the run: aborting the run, or resuming so the step runs again, ends it.
SIGTERM fences new commands and downstream dispatch
immediately. Active turns have five
seconds to finish, then the daemon cancels observation and detaches from its
seats without ending them. HTTP draining and execution share a ten-second total
shutdown budget. Open dispatch identities remain in a resumable block for
startup recovery; a waiting human request remains answerable, on the blocked run
too. Abort runs explicitly
when seat cancellation is intended.

The ledger accepts nothing after a final event. After `run_blocked` it accepts
only a resume, a cancel, a failure, a human verdict (routed once the run
resumes), or the `seat_cleanup` of seats kept on call, which the runtime
records just before the cancel or failure that ends the run. Those verdicts and
cleanups leave the run blocked. A ledger ending in them, as a crash
between the cleanup and the cancel leaves it, projects the block's status,
`resumeAllowed` and needs-you asks, is recovered at startup as that block, and
still accepts a resume, cancel or failure.

If final seat cleanup was recorded but no final outcome could be written, the
settled run is blocked resumably with `run_finalization_interrupted` when ledger
writes work again. This also covers a failed coordinator failure record.
The existing needs-you retries reconcile it under the run's command reservation;
startup applies the same rule, including a run with a human request still open.
An active finalization is left alone, and an existing block or final outcome is
preserved. Cleanup does not establish success, rejection or cancellation: an
explicit resume follows the recorded inputs and verdicts to decide what runs or
ends next.

The writer lock is retained until all admitted execution and authoring writes
settle. If a worker ignores cancellation, shutdown returns an error at the
deadline and retains that lock until the worker or process exits. The host's
service watchdog is an outer limit, not the normal shutdown mechanism; keep
deployment and live restart verification in the owning host repository.

A run that only waits on its gates survives restart waiting, with each
request's original `requestedSeq`; verdicts after restart still name that exact
request sequence. A run that was between steps, with work still
owed, a recorded verdict not yet routed, or a gate that received a newer input
than the one it asked about, is blocked at startup ("coordinator restarted
between steps; resume to continue"), and resuming continues it while its gates
keep waiting.

`run resume --mode redispatch` abandons an open dispatch without completed
evidence and runs the node again as a new attempt.
The abandoned dispatch is recorded as a `slot_result` with status `abandoned`;
the node's next attempt is one more round for a Limit card that covers it. A failed reattach never
finishes the run: it records `dispatch_reattach_failed` with the reason and
leaves the run blocked and resumable.

A single step's run resumes as a mission run does, a Limit card grant
included: once its step's latest attempt has its output the run succeeds,
otherwise the step runs again as its next attempt on the run's inputs.

Resume does not manufacture missing completion or resend an uncertain task.
An unresolved dispatch can block again. For explicitly selected completed native
evidence, restart with `--resume-run`, `--completed-transcript` and
`--completed-brief` together, naming the blocked resumable run, absolute native
transcript and original brief. Validation checks the original digest, pointer,
cwd, session, model/effort and completed turn before continuation.
An offline `mission run`, `formation run`, `run resume`, `run abort` and
`gate approve|reject` start, continue or stop a run as the daemon's worker does, so
each takes the state directory's coordinator lock and refuses while a daemon
owns it ("a daemon owns this state directory: run this through it with
--server").

### Needs-you notifications

`--notify-command <absolute-executable>` turns on operator notifications; empty
leaves them off. `--cockpit-url` supplies the base for links in messages. The
daemon runs the command directly, with no shell or arguments, writes one
notification as JSON on stdin, and treats exit status 0 as delivered. Each send
has a 30-second timeout. A timed-out command's process group is killed, and the
first 4 KiB of its stderr go to the daemon log. The command owns the channel,
recipient and credentials; none of them are daemon flags. On a session-channel
run a `human_gate` ask goes to the asking formation's seats instead, and reaches
the command only after its recorded fallback (see [Human gates on the session
channel](#human-gates-on-the-session-channel)); the daemon delivers those asks
with or without a notify command.

A notification carries `runId`, `missionSlug`, `missionTitle`, `seq`, `kind`,
`runStatus`, `nodeId`, `gateId`, `gateTitle`, `ask`, `severity`, `blocks`,
`missionUrl`, a one-line `text`, and a complete plain-text `subject` and `body`.
`missionUrl` is the mission's cockpit link,
`/?mission=<slug>` with `&run=<runId>` when the run is known. The kinds are:

- `human_gate`, keyed by the request sequence. The body carries the criterion,
  the gate's input text capped at 64 KiB, the cockpit link, and exact
  `gate approve` and `gate reject` commands with `--requested-seq`.
- `escalation`, keyed by a blocking escalation's sequence.
- `blocked`, keyed by the `run_blocked` sequence when no blocking escalation
  already explains the block. A waiting gate explains none. The body gives the reason and, when resumable,
  the `run resume` command.
- `final`, keyed by the terminal event sequence. A `picked` run's body says
  its work went on through a pick, so its outputs are failed work.

A human gate's ask and a blocking escalation are sent as soon as the ledger
records them, while other steps still run. Blocks and final outcomes are sent
only once the run has settled: its command worker has exited, so a block that
same command follows with a cancel is never sent. Each ask is sent once and recorded in the run's
`.needs-you.json` artifact, so restarts send no duplicates. A failed send stays
unrecorded and is retried at the run's next settle, at startup and every five
minutes. Sends never delay runs or shutdown. At startup the daemon reconciles
only non-final runs. A final outcome is sent only for a run that finished while
this daemon process was running, so enabling notifications never mails old
results.

For a host command example, see [configure notifications](../skills/archon/references/lab-and-examples.md#configure-notifications).

## HTTP contract

[OpenAPI](openapi/archon.yaml) lists the served routes. Except for the raw
theme document described below, JSON responses use
`{success,timestamp,data}`; errors carry an error object. Mission authoring
under `/api/missions` includes list/create/read/patch/delete, notes, layout,
validation and change polling. A mission read or edit answers `data.mission`
(with `layout` and the created or changed node, such as `inputCard`, where it
applies). A mission always carries its lists as arrays, empty or not:
`inputCards`, `formations`, `gates`, `tools`, `ends` and `connections`, each
formation's `inputs`, `outputs` and `slots`, each gate's `kinds` and each
Tool's ports; a layout carries `nodes` and `edges` the same way; a run starts with `mission` and `inputCardId` (or `formationId`), its
`inputs` and the other run fields. The
Input card patch actions are `createInputCard`, `updateInputCard` and
`deleteInputCard`. A mission patch carries one operation: one naming two or
more is refused whole with HTTP 400, "a mission patch names one operation, and
this one names deleteInputCard and title: send them one at a time", and nothing
changes. Agent routes
list/create/read/patch persona cards; `GET /api/agents/{id}/usage` lists every
slot, in every mission, that names the role; `PATCH` with `retire` true or
false retires the role or brings it back; and `DELETE` (with `If-Match`)
removes a role's card, refusing with `ROLE_IN_USE` (HTTP 409) and the slots
while any slot names the role, and with `BUILTIN_ROLE` for a built-in role,
which has no card; deleting a card that overrides a built-in role brings the
built-in back; the roster also serves `harnesses` (each
with the efforts it accepts and its known `models`, `{id,efforts?}`) and
`effortPolicy` (`{effort,use}` lines). Gate
profiles expose the two code checks.
With the tmux executor the agent roster marks a persona live when the session
named after it runs on `--socket`, and lists the socket's other sessions as
unbound. The lab executor reports every agent offline.
Revision and ETag checks protect edits. A mission edit that leaves the mission
as it was (the same slot assignment, title, brief, type, controller, gate or
Input card fields, judge chain, or a Tool's title and parameters) saves
nothing: it answers 200 with the current mission, its revision and ETag
unchanged, and a stale ETag still conflicts. A stale revision or ETag answers
409 `CONFLICT`, "The mission changed since it was read; reload it and retry".
The cockpit's Rename mission conflicts with no other edit, so it renames the
mission as it is when saved, reading it again once if another edit lands in
between, and otherwise says to press Save again (archon-n7u.16). A note edit to the text its entry
already has saves nothing in the same way (archon-62h). Runtime routes start/list/read runs
(`GET /api/runs?mission=<slug>` lists the runs of the mission now under that
slug or ID, and none for a mission that does not exist; `needs=you` keeps the
open runs waiting at a human gate or blocked), read projected
events/escalations, stream SSE, abort, resume, read run evidence and record
exact human verdicts and picks. They all use the coordinator; no request-local executor
exists.
There is no generic file reader, transcript endpoint, mission import endpoint or
authentication layer. Run evidence reads only one run's ledger, its artifact
directory and the briefs its own dispatches recorded.

`GET /api/characters` lists the saved characters; `POST /api/characters` saves
a new one (`name`, `harness` and `effort` required; `id` defaults to the name
in lowercase with dashes; optional `description`, `agentId`, `model`, and
`picture` as a PNG, JPEG or WebP data URL of at most 96 KB) and refuses an id
that exists with HTTP 409 `CHARACTER_EXISTS`; `PUT /api/characters/{id}` states
one in full; `DELETE /api/characters/{id}` removes it. A harness, model or
effort a slot could not take is refused with HTTP 422 `INVALID_CHARACTER`. No
route changes a mission: the dashboard stamps a slot with the ordinary
`assignSlot` edit, from the slot's row in the step window or by dragging the
character from the roster. Characters are made and edited in the Agents view. On
the CLI, `agent characters` lists them and `formation assign --character <id>`
stamps one, each of `--role`, `--harness`, `--model` and `--effort` given
beside it replacing that value.

With `--server`, Archon runs these authoring and read commands through the
daemon, so an open cockpit sees the edits through its change polling: `mission
new|list|inspect|notes|note|validate|arrange|create|update|delete|input|output|wire`, `formation
list|inspect|create|rename|set-type|assign|unassign|set-brief|add-input|add-output|set-port|wire|unwire|delete`,
`gate create|update|judge|delete`, `end create|update|delete`, `tool create|update|delete|inspect` and `agent
list|inspect|new|edit|retire|restore|delete`. They take the offline flags and print the offline
output: unwrapped JSON without TOML, or the same text. Each command reads the
document it changes, resolves formation, gate, mission and Tool selectors from
that read, and writes with its ETag and mission revision; Tool writes also carry
the layout's state and ETag. A write that loses to another editor is read and
retried up to three times. Differences from offline use:

- Error messages come from the daemon (`coordinator HTTP <status>: ...`). JSON
  error codes, boundaries and selectors match.
- Agent cards are the daemon's `--agents-dir`, with the liveness the daemon
  reports. `mission note --file` reads locally.
- Runtime commands (`mission run`, `formation run`, `run`, `gate approve|reject|pick`) print the
  daemon's `{success,timestamp,data}` envelope, except `run status`, `run
  gates`, `run seats`, `run attempts` and `gate request`, which require
  `--json` for that format and otherwise print the offline text, and `run wait`,
  which prints its paragraph or its own JSON (see [Waiting on a run](#waiting-on-a-run)); `mission list` and
  `mission inspect` print offline JSON like the other reads. `run ask` and
  `agent spawn|attach` remain offline only.

`GET /api/missions/{mission}/validation` returns
`{missionRev,missionEtag,errors,warnings}` for the whole mission, the same report as
`mission validate`.

Plain `mission inspect <mission>` is the agent's read-only hand-over: it shows
the graph's goals, inputs, complete node configuration, briefs, staffing and
routes with their action IDs, chronological attributed notes beside their
objects, orphan threads, and the current validation findings. It keeps all note
words, including partial answers and contradictions; it infers no question or
answer state. Drafts with findings remain readable, and larger reads are never
truncated. The graph outline is not a run trace. Its captured mission and notes
revisions describe separately collected documents, not an atomic snapshot.
The server read checks notes' mission identity and the validation report's
mission revision/ETag against the displayed graph; a failed component or identity
mismatch reports an error before printing the hand-over. `--json` still returns
the graph alone, and `mission inspect <mission> <input>` retains its selected
Input-card inspection.

### CLI deletion

`archon mission delete <mission>` removes the mission and its layout; run
history remains. `archon formation delete <mission> <formation>` removes the
step, every connection touching it, its layout node and its Limit cards.
`archon gate delete <mission> <gate>` removes the gate, its layout node and
every route touching it, including its judge attachment. Judge formations
remain. These commands work offline and through `--server`, with the same
revision checks as other authoring edits.


One mission per file, with one Input card. A file holding several Input cards
loads and stays editable, but validation reports one `several_input_cards`
error that names each Input card and says how to split the file: copy it beside
itself under a new slug with a new id, slug and title, then delete from each
file the Input cards, and the steps only they reach, that belong to the other.
Admission refuses every run from such a file, mission or single formation, with
the same message.

`GET /api/theme` returns the raw CHROTE schema-1 theme document with no response
envelope. Optional `--theme-file <absolute-path>` selects a host-owned file,
read and validated on each request. With no flag the daemon serves bundled
`chrote-dark`; `src/internal/api/theme_default.json` is the canonical default
for the server and dashboard first paint. A configured missing/unreadable file
returns HTTP 500 `THEME_UNREADABLE`; malformed JSON or schema returns HTTP 500
`INVALID_THEME` naming the offending field. These errors do not disable mission
execution. The dashboard keeps its complete default palette and reports a failed
theme request; it applies the theme once per page load. No theme-art routes
or polling are provided. Host paths and deployment belong to the host.

The app bar's Style control chooses what the dashboard draws in this browser:
Console is the host theme above; Paper is a built-in light palette. The choice
is kept per browser and never changes what `GET /api/theme` serves.

`GET /api/runs/{runId}/seats` returns an enveloped
`{runId,available,reason?,seats}`. Each latest seat per node/slot includes
`runId`, `nodeId`, `nodeTitle`, `slotId`, `slotLabel`, `harness`, the `model` (absent for the harness default) and `effort` the seat was started with, `controller`,
`createdSeq`, `sessionName` and `state` (`live`, `ended`, `missing`, `unavailable`).
A live seat also includes native `columns`, `rows` and a relative `terminalUrl`;
other states include a reason and no terminal URL. A seat kept on call also
includes `onCall` with `keptSeq` and `waitingOn`, the pending
`{gateId,requestedSeq}` asks it received. No private session IDs,
socket paths or socket identities are exposed in this projection.

`GET /api/runs/{runId}/seats/{createdSeq}/terminal` upgrades to a
WebSocket with subprotocol `tty`, after resolving that exact run and attempt.
Unknown seats return 404, replaced attempts or non-live seats 409, and unavailable
configuration or shutdown 503. The frames are CHROTE's. The opening JSON frame
gives `columns` and `rows`, and the terminal attaches at the seat's native grid.
Afterwards the client sends binary frames: ASCII `0` followed by input bytes,
which reach the pane; `1` followed by JSON `columns` and `rows`, which sizes this
terminal's view; and `2` and `3` to pause and resume output. CHROTE's `4`
claim frame, which asks to size the window, is declined, and other frames are
ignored. The cockpit's handshake is CHROTE's, `{AuthToken:"",columns,rows}`,
with the seat's native grid; the token is not checked. The seat window keeps its own size: the executor sizes a new seat's
window to 160x48 and pins it (tmux `window-size manual`), so no viewer resizes
it, including the only viewer of a seat kept on call. A CHROTE tile watches
the pinned window at that size.
Output frames are binary ASCII `0` followed by terminal bytes. A terminal ending
closes with 1000, and so does a refused attach, after an output frame reading
`Archon: <reason>`; daemon shutdown closes terminals with 1001. WebSocket origins must
match the request host. Terminal bytes are the actual seat display, not the
sanitized ledger projection. The same trusted-network access boundary applies.

### Run evidence

Absolute paths in terminals, file text, briefs, outputs and notes open read-only
file windows beside their source. Bead IDs copy on click (tooltip: "Bead ID,
copied on click"); Archon has no Beads catalog or card API. File and folder
inputs in the start dialog may browse the agent host through
`GET /api/files/directory?path=<absolute-directory>` or accept a typed path.


[ADR-0017](adr/0017-run-evidence-api.md) records this API. Every route is a
`GET` for one run; an unknown run returns 404. Served text is an object
`{text,bytes,truncated}`: `bytes` is the full size, `truncated` marks a cut on
a UTF-8 boundary, and the ledger's secret patterns are redacted.

- `/api/runs/{runId}/evidence/nodes/{nodeId}` returns
  `data.evidence` for a node of the run's frozen mission, with `kind` `inputCard`,
  `formation`, `gate` or `tool`. Its `definition` contains the frozen `title`,
  `outputs` (`id`, `label`) and `outgoing` connections (`id`, `from`, `to`).
  Produced-output names and ordering use this run metadata even after the
  editable mission changes. Input cards and formations list `attempts` with
  routed `inputs`, `dispatches` (`seq`, slot, agent, harness, result status and
  whether a brief exists), `seatCleanups` (slot and outcome) and `output` (text,
  status, reason and sorted `ports`).
  Gates list `evaluations` with the criterion, input, `kindResults` with judge
  `evidence`, `judgeFailures`, `humanRequests` with each decision's `response`,
  and the final `verdict` with `perKind` and `routePort`, and `picks`, each pick
  of a step's best attempt (see [Picking the best
  attempt](#picking-the-best-attempt)). `problems` lists the
  blocks and errors recorded against the node, including a block whose open
  dispatches name it. Each text is capped at 64 KiB;
  a kind result or verdict lists at most 100 evidence items and counts the rest
  in `evidenceOmitted`; one response carries at most 2 MiB of text, after which
  texts are empty and truncated. An unknown node returns 404.
- `/api/runs/{runId}/evidence/problems` returns `data.problems`,
  every block and error of the run and the `run_failed` or `run_canceled` that
  ended it, oldest first: `seq`, `type`, `code`, `reason`, `resumeAllowed` and
  `nodeIds`, the nodes it names (its node or gate, the blocked node or gate,
  and nodes with open dispatches). A block that names no node, such as a
  coordinator restart between steps, has empty `nodeIds` and its reason. A run end that names
  no node lists the nodes it stopped: formations without output and gates
  without a verdict. A run end carries `actor`, who ended it (`archond` for a
  coordinator failure, whose cause is its `reason` and code its `code`). A
  block the run later resumed carries `resumedSeq`; a spent Limit card's
  block carries `limit`. Node evidence `problems` include the same run end for the nodes it
  stopped. The 2 MiB budget is spent on the latest first.
- `/api/runs/{runId}/evidence/briefs/{dispatchSeq}` returns
  `data.brief` (`dispatchSeq`, `nodeId`, `slotId`, `attempt`, `text` capped at
  256 KiB): the brief that this run's `slot_dispatch` at that sequence sent to
  its seat, read from the path that event recorded. Any other sequence
  returns 404.
- `/api/runs/{runId}/evidence/artifacts` returns `data.artifacts`
  (`name`, `size`, `modifiedAt`), relative to
  `<state-dir>/.archon/artifacts/<runId>`. Among files reached by the bounded
  walk, accepted output files and folders come first, with at least one file per nonempty output folder, then other
  files in name order. The list holds 500 files (or more when needed for
  one representative per output), with `data.omitted` a lower bound on unlisted files
  and `data.truncated` when files are omitted or exceed 16 directory levels.
  Directory cycles are skipped. A run without artifacts lists none.
- `/api/runs/{runId}/evidence/attempts` returns `data.attempts`, `{runId, steps}`:
  every step that ran, in the order it first ran, or with `?node=<id>` only
  that step (an unknown step returns 404). A step has `nodeId`, `title`,
  `judgeOf` (the gates of the frozen mission it judges for, when it is a
  judge), `outputs` (its
  output ports from the frozen mission: `id`, `label`, `kind`) and `attempts`,
  oldest first. An attempt has `attempt`, `status` (`running`, `done` or
  `stopped`), `startedSeq`, `startedAt`, `endedAt`, optional `stopReason`
  (bounded evidence text explaining a stopped attempt), `outputSeq`, `directory`
  (`name` relative to the run's artifact directory and absolute `path`, when
  it exists), `outputs` (`portId`, `label`, `kind`, `text`, and for a path
  `path`, `artifact` for the artifact routes, `size` and `fileKind`: `image`,
  `pdf`, `video`, `model`, `markdown`, `json`, `text`, `binary` or `folder`), `verdicts`, each
  gate evaluation of that attempt's output, by the `origin` of the input the
  gate judged, so a gate on another gate's fail route or after a human pass
  judges the attempt whose output it carries (`gateId`, `gateTitle`,
  `evaluationSeq`, and once decided `seq`, `verdict`, `reason`, `evidence`,
  `evidenceOmitted` and `decidedBy`; `pending` while it waits; `superseded`
  when the gate was evaluated again before its human request was answered, so
  that request no longer waits), `picked` when a spent Limit card picked it as
  the step's best attempt (see [Picking the best
  attempt](#picking-the-best-attempt); the pick's own verdict is not one of its
  `verdicts`), and `files`,
  the regular files in its directory (`name`, `path`, `size`, `modifiedAt`,
  `kind`), ordered like the run artifact list with declared outputs first,
  `filesOmitted` a lower bound on unlisted files and `filesTruncated` past its limits.
  Artifact walks stop at 50,000 files per listing; omitted counts are lower bounds
  displayed as "at least N more".
  Output folders open complete directory listings, with navigable subfolders.
  Bytes come
  from `/api/runs/{runId}/artifacts/{name...}` by a file's `name` or an
  output's `artifact`. Texts share the node evidence caps, spent on the newest
  attempts first, so a long run cuts its oldest attempts' texts. `archon run attempts
  <runId> [--node <id>] [--files] [--json]` defaults to each step and
  attempt with its number, status, verdict and reason one-liner, and declared
  outputs. `--files` adds directory files; `--json` keeps the complete evidence,
  offline or through `--server`.
- `/api/runs/{runId}/evidence/artifacts/{name...}` returns
  `data.artifact` (`name`, `size`, `modifiedAt`, `kind` `markdown`, `json`,
  `text`, `image`, `pdf`, `video`, `model` or `binary`, and `path`, its absolute path on the
  daemon host), with `text` capped at 256 KiB for textual kinds. A `.pdf` file
  that starts with `%PDF-` is `pdf`.
- `/api/runs/{runId}/evidence/mission` returns `data.mission`
  (`missionRev`, `graph`, `text` capped at 256 KiB): the structured graph and
  mission's TOML as the run froze
  it at admission, which later edits never change.
- `/api/runs/{runId}/artifacts/{name...}` returns the artifact's bytes
  with MP4, WebM, GLB, glTF and binary glTF sidecars (`.bin`) streamed
  directly from disk without a size cap. Other files are buffered up to
  16 MiB; larger files return 413. Text is `text/plain; charset=utf-8`
  and redacted, PNG, JPEG, GIF and WebP keep their image type, a `pdf` is an
  inline `application/pdf`. MP4 and WebM are inline `video/mp4` and
  `video/webm`; GLB and glTF are inline `model/gltf-binary` and
  `model/gltf+json`. Models, including FBX, and their binary sidecars stream byte-exact without the buffered read cap.
  Other binary content is an `application/octet-stream`
  attachment. Responses carry
  `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox` and
  `Cache-Control: no-store`, and support ranges. The file window is a reader,
  so HTML is served as text and nothing a file contains runs in the cockpit.
  Video responses replace `img-src 'self'` with `media-src 'self'` in their CSP.
  Native video controls open in file windows and attempt evidence. GLB/glTF/FBX previews load the 3D
  viewer on demand; relative glTF buffers and textures resolve beside the model.
  FBX uses the same viewer controls; unsupported formats remain downloadable.
- `/api/runs/{runId}/gates/{gateId}/request` is the evidence API's
  view of a human request still waiting for an answer. For the gate's latest
  request, while it is pending, it returns `gateId`, `requestedSeq`, the frozen
  `criterion` and the routed input: `fromNodeId`, `fromPortId`, `text` capped at
  64 KiB, and `truncated`. `routes` says where each verdict leads on the
  run's frozen mission: `verdict` (`pass`, `fail`), `targets` (`nodeId`,
  `title`, `kind`, and for a formation the `attempt` it would start, its
  Limit card's `rounds` and `time` so far (`used`, `max`; omitted without a
  card, by the engine's own rule) and
  `waitsForInputs` for a join still missing another input; for an End node
  kind `end` and its `outcome`), `endsRun` when every route of the verdict
  ends its path at an End node and nothing else in the run can still run,
  `runFails` when the run fails once it ends, at once with `endsRun` or else
  after its other open work (a rejected End node on this route, or a path
  already rejected), `missionRounds` (the mission card's use) and
  `roundsNeeded` (the step runs the route starts, judges included, and for a
  step that picks the judges its pick starts instead of the step) when a
  Limit card covers the mission's rounds, `missionTime` when it sets time, and
  `limit` when a card the route needs is already spent, so taking it blocks
  the run until a grant, or, with `picksBest`, the card's `whenSpent` is
  `best` and the step's best judged attempt is picked instead and goes on as
  its gate's pass. A pick whose judges would start under a spent mission card
  blocks there, so such a route names the mission card without `picksBest`.
  When the frozen mission cannot be read, `routes` is omitted. A
  pending pick adds `pick` (`nodeId`, `candidates`, `recommendation`), its
  input shows the candidates side by side, and `routes` holds only `pass`,
  which the picked attempt takes. An
  unknown run or gate returns 404; a decided request
  returns 409. After the verdict, the gate's node evidence holds the same input
  with the response.
- `/api/runs/{runId}/wait?until=&since=&hold=` holds the request
  until the run is final, has an ask after `since` (`needs-you`,
  `any-change`) or has any event after `since` (`any-change`), for at most
  `hold` seconds (0 to 60, default 30). It returns `data` as `run wait --json`
  describes, with `outcome` `final`, `needs-you`, `changed`, or `pending` when
  the hold ended first; a pending answer keeps `seq` at `since`. Asks carry
  their gate or step `title`, a human gate's `criterion`, `input` (the start of
  the text, capped at 4 KiB, with `bytes` and `truncated`), `routes` and, for a
  pick, `pick`, and a block's `reason`, `code`, `resumeAllowed`, and at a spent
  limit `limit` and `resumePolicy`. A driver agent consumes this
  text, so it is served verbatim, as `gate request` serves it. `end` carries `status`, `seq`, `code`, `reason`, `endedBy`, the
  `stopped` steps and a succeeded or picked run's mission `outputs`, with their `picks`. A bad `until`, `since` or `hold`, or a `since` past the
  run's last event, returns 400, an unknown run 404, and a stopping daemon 503.
  Waits and streams read the daemon's parsed ledger, which parses only what
  each append added, so fifty waiters on a large ledger answer within a
  second of an event without holding up the run's appends; an unknown run
  answers 404 before anything is kept for it.

Artifact names are relative to the run's artifact directory, and a name with
`..` or an empty component returns 404. Symlinks and hard links an agent left
there are followed, and only regular files are read. Output and input references appear as `ref.artifact` inside that
directory or `ref.external` (its absolute path, which opens in a file window) elsewhere; engine references
such as `ledger://` name no file and are omitted. Structured fields
never carry native session IDs, tmux session or pane IDs, `sessionRef`, socket or
prompt digests, brief or prompt paths or seat report pointers, and worker pane
captures are not served; an artifact's preview names its absolute path. Text is served as
recorded apart from redaction, so it can mention host paths such as the cwd.

### Referenced files

Archon confines no files ([ADR-0021](adr/0021-archon-only-chains-agents-and-gates.md)).
The daemon opens any file a reference names by absolute path, following
symlinks, as CHROTE's file viewer does. The `path` query is the reference as
authored. A relative path has no base: authoring refuses one, and a
hand-written one returns 400, "use an absolute path".

- `GET /api/files/preview?path=<ref>` returns `data.file` (`path`
  read, `name`, `size`, `modifiedAt`, `kind` as for artifacts), with `text`
  capped at 256 KiB for textual kinds.
- `GET /api/files/raw?path=<ref>` uses the raw artifact route's streaming,
  size limits, content types, range support and headers.

A file the daemon's user may not read returns 403. A missing path, a
directory or another non-regular file returns 404, because reading a FIFO or
device would never finish. Served text is redacted like run evidence.

The cockpit shows each referenced file as a chip on its card: a mission's and a
gate's files and a formation's brief files. A gate's card also shows the brief
files of the formations judging it. A card shows the first few chips and lists
the rest under +N. A chip opens the file in a floating file window, and a file
the daemon cannot read opens with the daemon's reason and its path, offering
Copy path but no Open raw or Download. Arrange reserves a chip row under a card
that has referenced files.

A reference that names nothing the daemon can open is flagged where it is
authored and on its chip (archon-n7u.26). Mission validation warns, with the
file as `path`, `missing_file` for a file that does not exist ("Review's file
/srv/rubrics/quality.md does not exist") and `relative_file` for a
hand-written relative path ("... is relative: use an absolute path"). A file may
still appear before a run reads it, so neither blocks a run. The CLI's `--file`
writes print `warning: file <path> does not exist` once saved; the cockpit
flags the chip, its line in the node window and its Flow chip, as of the
mission's latest validation.
