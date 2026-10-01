# Archon contract

## Philosophy

Archon makes chaining agents and gates easy and great, and that is all it does
([ADR-0021](adr/0021-archon-only-chains-agents-and-gates.md)). Its sessions are
tmux sessions with full access to everything, as in CHROTE: Archon adds no
sandboxing, file confinement, blast-radius limits or authority checks, and
safety lives in the configuration of the agents it runs. Archon keeps only what
is current, with no backwards compatibility: when a name or format changes, the
deploy migrates live data once and the old path is deleted.

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
| Input card | The mission's entry node (`[[inputCard]]` in TOML, `inputCards` in JSON) with the mission's goal, input hint, declared inputs and `out` port. A mission has one Input card. |
| Input | A named value each run of the mission supplies: a name, a description, a kind (`text`, `file` or `folder`) and whether it is required. Step briefs reference it as `{name}`. A mission that declares none has one implicit required text input, `brief`. |
| Formation | A step: the team of agents that does it. `solo` has one seat; `peer` has peer seats; `orchestrated` has a controller directing its bound workers. |
| Slot | A position in a formation that owns what its seat runs: a harness (`claude-code` or `openai-codex`), a model (blank means the harness default) and an effort, plus an optional role (`agentId`, a persona). A slot without a role is a vanilla agent, such as `claude-code · opus · low`. A seat is the slot's runtime agent session. |
| Persona (role) | A TOML agent card with generic role text: a summary, capabilities and kind. A new card carries no model or effort; an existing card's harness variant settings are read by the cockpit's role drag and `archon agent spawn`. Presets remain available; local cards can override them. |
| Harness variant | A persona card's `openai-codex` or `claude-code` settings: session stem, model and effort. Seats start from slot settings. |
| Gate | A criterion with one or more kinds: `code`, `formation`, `human`. Its ports are `in`, `pass`, `fail`, `judge`. |
| End node | Ends a path on purpose (`[[end]]` in TOML: `id`, `title`, `outcome`). Its outcome is `done` or `rejected`. Its only port is `in`, which takes any number of routes; it leads nowhere. |
| Connection | A directed edge between `node-id:port-id` endpoints. Formation input and output ports have explicit IDs. |
| Judge chain | Formations wired from a gate's `judge` port and back to that same port. The final judge result decides the formation kind. |
| Pushback edge | A gate's `fail` connection back to work, delivering feedback and starting the next attempt, capped only when the run set `maxAttempts`. There is no `retry_control` port. |
| Run | One admitted mission or isolated formation, with definition and persona snapshots, inputs and any limits the launch set. Later edits affect later runs. |
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
points at. Persona cards default to `<state-dir>/agents`;
daemon `--agents-dir` can select another absolute directory. Match offline
persona authoring to that directory with `ARCHON_AGENTS_DIR` when using an
override.

A slot owns its harness, model and effort. Staff it with `archon formation
assign <mission> <formation> --slot <slot> --harness <h> --effort <e>
[--model <m>] [--role <persona>]`,
or the `assignSlot` mission patch with `agentId`, `harness`, `model` and
`effort`. Staffing always states its effort: the CLI requires `--harness` and
`--effort`, and a patch without an effort is refused with
`INVALID_SLOT_SETTINGS` (HTTP 422, CLI code `invalid_slot_settings`), except a
patch naming only a role (and perhaps a harness), which is the cockpit's role
drag: it writes that role's current effective harness, model and effort onto
the slot. A patch naming none of them empties the slot, as does `formation
unassign`. The harness must be one Archon starts and must accept the effort,
and a model is one name without spaces; a model outside any catalog is
accepted and the harness decides. Choose the effort by the policy the agent
roster serves as `effortPolicy`: `low` for errands, `medium` for making
things, `xhigh` for architecture and review, `max` for consequential reviews.
One role may staff several slots, each with its own settings.

A new role carries no model or effort: `POST /api/agents` and `archon agent
new` refuse them with `INVALID_AGENT_CARD`, and the New agent form no longer
asks for them. An existing card's variant settings are edited per
harness variant in the Agents view inspector and persona editor, with `model`,
`effort` and `variant` (or a `variants` list) on `PATCH /api/agents`, or with
`archon agent edit --model --effort` (`edit --harness` picks the variant); the
role drag and `archon agent spawn` read them. Effort must be one the
harness accepts: `claude-code` takes `low`, `medium`, `high`, `xhigh` or `max`;
`openai-codex` also takes `ultra`, though a Codex model may accept fewer. A
blank model or effort clears it to the harness default model or `medium`.
Persona reads carry each variant's `effectiveEffort` and `seatLaunch`, the
command a seat with those settings runs, rendered by the seat launcher from the harness CLI on the
reader's PATH (the daemon's for HTTP); `seatLaunchError` says why a variant
cannot start. `archon agent spawn` runs the same command, and refuses a harness
Archon cannot start, such as `hermes`. Cards hold no launch string. One edit
names each variant once.

Notes are operator intent, not
automatically executable briefs. Read mission and element notes, then translate
them into formation briefs, staffing and edges.

Each mission or element note is a thread of entries, oldest first. An entry has an
ID, an author (`human:<name>` or `agent:<name>`), a creation time, an optional
edit time and text. `archon mission note` appends an entry, by default as
`agent:archon` (`--author` names another). Only an entry's author can change it:
`--entry <id> --text` edits it and `--entry <id> --clear` deletes it. Reply
rather than rewriting someone else's note. The cockpit writes as `human:ui`. It
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
agentId = "codex-builder"
harness = "openai-codex"
model = "gpt-6-astra"
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
To undo a target join atomically, `rewireConnection` also accepts
`removePreviousInput: true`: it removes the old formation input only if no
other connection uses it. Wiring errors use `INPUT_OCCUPIED`, `SELF_WIRE`,
`DUPLICATE_CONNECTION` or `INCOMPATIBLE_TOOL_CONNECTION`; `CONFLICT` remains
reserved for stale mission revisions or ETags in these edits.

The [delivery mission](../examples/delivery.mission.toml) and its
[notes](../examples/delivery.notes.toml) add Plan, Beads, a Beads-review judge,
orchestrated Execution and Final review. A run supplies one input, `change`,
which every step's brief references as `{change}`; the target repository is the
run's cwd and the owning Bead its Bead. Six `delivery-*` preset roles staff it, each slot at `medium` effort.
Execution uses a Claude controller and three Codex workers; Final review uses
Astra. Failed Beads review returns directly to Beads. Final review produces a
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
(`unavailable_persona`), one controller and a worker in each orchestrated
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
`cwd`, `contextPaths`, `beadId` and `inputs`. Omit `cwd` (or send an empty string) to create a private workspace
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
Braces around anything else stay as written. The one escape is `{{name}}`: it
reaches the seat as a literal `{name}` and is never a reference, so a brief can
show an agent a placeholder. A reference to a name the mission does not declare
is the validation error `unknown_input_reference`: "Plan's brief references
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

Runs have no limits unless the launch sets them. `maxDispatch`, `maxAttempts`
and `wallClockSeconds` are optional; an absent or zero limit means none, in
admission and in the engine. A negative limit is rejected.
Neither `archon mission run` nor the cockpit's Start mission dialog supplies a
limit: a run started without one loops through send-backs until a gate passes
or its driver stops it. Set `--max-dispatch`, `--max-attempts` or
`--wall-clock-seconds` to cap a run; each set limit is enforced and named when
it blocks the run, as below. Dispatch
limits bound formation execution steps, including judge steps. Each durable
formation start consumes one dispatch before any seat launches, including failed
or interrupted execution. Human approval, resume, restart and redispatch do not
replenish that run-wide allowance; reattaching completed evidence consumes no
new dispatch. Attempts bound revisits to a node. The wall clock bounds agent
work. Every formation dispatch must finish within `wallClockSeconds` of the run's start, not counting time the
run spent waiting for the operator. A wait runs from a human gate's
`human_input_requested` to the `human_verdict_recorded` for that gate, and time
while several requests wait counts once. The waits come from the ledger's
timestamps, so they survive restarts, and a request still waiting never runs
the clock out. A dispatch that exceeds it blocks the run with
`wall_clock_exceeded`. Formation allocations also bound real agent execution.
Limit exhaustion and unresolved execution leave visible blocks rather than
claiming success. A block that exhausts attempts or dispatches
(`resume_attempts_exhausted`, `revise_loop_exhausted`,
`max_dispatch_exceeded`) records `resumeAllowed: false` and `resumePolicy:
limit_exhausted`, because resuming could only block again; its run evidence
names the limit as `limit` (`kind` `attempts` or `dispatches`, `nodeId`,
`used`, `max`).

A step has no time limit unless its formation authors `[formation.execution]`
with a positive `timeoutSeconds`; the daemon imposes no default. That
allocation covers the whole attempt: seat startup, preparation, collaboration
and finalization. The run's mission snapshot freezes it, so later mission edits
affect later runs. Each `node_started` of a step with a duration records the
duration and absolute `executionDeadline`; restarting does not give the same
attempt more time. Explicit redispatch starts a new counted attempt. The earlier
of that deadline and the run's remaining wall clock governs execution. Formation
expiry blocks with `formation_timeout_exceeded`, retaining partial evidence. A
downstream human gate waits after the formation finishes and spends no
formation time.

The projection reports `running`, `waiting_human`, `blocked`, `succeeded`,
`failed` or `canceled`. Always check `final` and `resumeAllowed`; a blocked run
is not a completed delivery. A failed or canceled run names who ended it in
`endedBy`; why is in its run evidence problems. Events expose node, slot and gate identities,
attempt, status/verdict, session display name and cleanup outcome where
applicable. `run_succeeded` and `run_failed` list in `endIds` the End nodes the
run's paths reached; a `run_failed` with code `path_rejected` names the
rejected End node as its `nodeId` and the routing gate as its `gateId`.
Typical sequences include `run_started`, `node_started`, `slot_dispatch`,
`seat_created`, `seat_prompt_consumed`, `slot_result`, `seat_cleanup`,
`gate_kind_result`, `gate_verdict`, and a run outcome. Malformed judges emit
`judge_attempt_failed`. Session-channel runs add `human_ask_delivered` and
`human_ask_fallback` (see [Human gates on the session
channel](#human-gates-on-the-session-channel)). Pending human requests appear in
`waitingGates` with `gateId` and `requestedSeq`.

The public projection includes cwd, context paths, inputs and Bead ID but excludes prompt text,
artifact contents, brief paths, native session IDs and arbitrary private event
data. Projections and SSE stay sanitized; the run evidence routes under
[HTTP contract](#http-contract) serve a run's outputs, gate results, human
responses, briefs and artifacts to the operator.
Run status also carries `startedBy`, the run's driver (the `actor` the start
named, `operator:standalone` when it named none; the cockpit's starts name
`human:ui`, and the CLI's run starts take `--actor`, default `agent:archon`), and `startedAt` and `updatedAt`, the
times of its first and latest ledger events; a final run ended at
`updatedAt`. Each waiting gate carries `requestedAt`, when it asked.
`run logs` is the same sanitized projection as `run status`. `run follow` prints
complete enveloped projections from SSE after durable changes, waits through
human gates and closes only at finality. Interrupting that client stops viewing,
not execution. Abort cancels only the selected run and waits for owned-seat
cleanup, including seats kept on call, before returning `canceled`.

### Waiting on a run

The agent that launched a run drives it by pulling; Archon pushes nothing into
its session. `archon --server <server> run wait <run>` blocks until the run
needs the driver, ends or changes, then prints one paragraph for the agent and
exits. Leave it running in the background and react when it returns:

```bash
archon --server "$ARCHON_SERVER" run wait "$ARCHON_RUN_ID" --until needs-you
```

`--until` takes `needs-you` (the default), `final` or `any-change`:

- `needs-you` returns when a human gate asks for a verdict, a blocking
  escalation is raised, or the run blocks with no gate or escalation to explain
  it. The paragraph names the gate or step, the gate's criterion, the start of
  its input (the whole input is `gate request`), where each verdict leads, and
  the exact `gate approve`/`gate reject` commands with `--requested-seq`, or
  the `run resume` or `run abort` command a block needs.
- `final` returns when the run succeeds, fails or is canceled.
- `any-change` returns at the next ledger event and lists the new events. When
  that event opens a new ask it answers as `needs-you` (exit 3) instead.

Every mode returns at once for a final run, and says how it ended: its status,
the step it stopped at, the terminal reason and code from its evidence
problems, and who ended it. Every other answer ends with the command that
waits for what comes next, carrying `--since <seq>`, the ledger sequence the
answer covers, and `--json` when the wait used it. Pass it to the next wait:
an ask counts as new only after `since`, so a driver that loops sees each ask
once and misses nothing between calls. Without `--since` every open ask is new.
Other open asks are still listed as reported earlier.

A bare block counts only once the daemon has settled the run, since a verdict
records one on its way to the automatic resume. Until then the run reads as
`running`, and in every mode the cursor stops below the block and an answer
reports only the events before it; with nothing else new the wait keeps
holding. Once the run settles, a real block is a new ask in every mode.

A verdict or resume sent while the command that recorded the ask is still
settling the run waits for that command, up to five seconds, instead of
answering 409, so a driver can answer the moment a wait returns.

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
responses, output ports, artifact directory and completion instructions.
Orchestrated controllers also get their bound workers and may direct only those
workers. The operator may type into any live seat at any time, through the seat
terminal or in CHROTE, and talk to the agent normally, whether it is working a
dispatch or idle. The runtime pastes a brief only while the agent is idle and its
input line is empty, waiting within the step's duration if it has one (a long wait is
recorded as `waiting_for_idle_input`, below), so a brief never lands
mid-turn or on the operator's unsent text. It submits the brief once its pointer
shows in the input line, however the harness wraps it. Archon agents must not
type into seats or manage their sessions.

Sessions are named `archon-<run>-<slot>`, with the `--mission-label` value
before the run ID when it is set.
The runtime creates and cleans up seats by immutable session ID through the
configured guarded wrapper. The daemon's launch environment must include any
host-required cleanup override and reason. Wrapper path alone is insufficient.
Check every `seat_cleanup` outcome: `ended`, `left_socket_changed` or
`left_cleanup_failed`. Shutdown records `left_shutdown` when it detaches from
an owned seat without ending it. Success does not erase a cleanup failure.
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
completing turn may come after them. A turn that finishes without the sentinel
fails the dispatch at once only when nobody else took a turn during it, and for
Claude only when the agent left no background work that resumes the
conversation; otherwise the dispatch waits, within the step's duration if it has one. The dispatch
still fails loudly when the seat ends, when the model or effort changes, or when
the harness moves to another conversation (`/clear`, `/new` or `/resume`).

A step without a duration never times out, so a seat that waits on something
Archon cannot end on its own is recorded instead, never blocked or failed.
Once such a wait has lasted a minute, the ledger records `seat_state` (`nodeId`,
`slotId`, `data.state`, `data.detail`, `data.since` and `data.dispatchId` once
dispatched), and records it again with state `working` when the wait ends:

- `seat_not_ready`: the harness has not reached its ready prompt;
- `waiting_for_idle_input`: the brief waits to be pasted because the agent is
  busy or the input line holds text the operator has not sent;
- `turn_ended_without_sentinel`: after an operator turn, the agent ended a
  turn without this run's sentinel;
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
for a human, then running, then blocked, newest first. The run bar's Runs
button lists every run of the mission (archon-o7p.2), open runs by attention
and then finished runs newest first, each identified without its ID: its status,
when it started, how long it took or has run, the start of its first text
input, and its driver. Choosing one shows it, and a finished run shown can be
put away. The run bar says when the shown run started, how long it took and who
drives it; a run waiting at a gate says since when; and a run that ran an
earlier revision of the mission says so and opens the mission as it ran in a
file window. Every run that needs the operator, waiting at a human gate or
blocked, is counted (archon-n7u.29): on the mission picker beside each
mission ("Scouting · 2 need you"), and in the page title for all missions
("(3) Scouting · Archon"). When another run of any mission needs the operator,
the run bar offers it ("1 more needs you"), the one waiting longest at a gate
first, so after an answer or a stop the next is a click away. Waiting is the
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
rest, including artifact files no output names. A chip opens the file in a
floating file window, rendered by kind, with Open raw and Copy path (the
artifact's absolute path on the daemon host); several can be open side by side.
Node, note and file windows open in the free space nearest what opened them:
Flow's gutters, or the canvas above and below the graph, shrinking to half
their remembered size at most to fit. A window leaves its own card or Flow row,
its neighbours, its next links and the title bars of open windows visible and
clickable, and cascades when no free space is left. Menus opened from the run
bar render above every window.

The cockpit's floating Peek attaches to an owned live seat and sends typing
through the seat terminal WebSocket below. Its resizes change only the viewer's
own view; the seat keeps its pinned size. The
operator types to the agent whether it is working a dispatch, on call or idle.
Seat terminals are re-ported from CHROTE's terminal (archon-o7p.13.1). Each holds
the seat's native grid and fits its font to its window: the largest font up to
14px at which every row and column fits, down to an 11px floor. Below the floor
the grid scrolls, starting at its newest rows, with Start of line and End of
line controls when it is wider than the window. Resizing a window changes only
its font; no size reaches the seat. Peek opens where window placement finds room
and shrinks to the grid it drew until the operator sizes it. Peek keeps the
terminal, connection and frame of every seat it has shown while it is open, so
switching seats does not reconnect. A connection lost with the seat still live,
such as across a daemon restart, dials again once on Refresh seats (Refresh in a
Talk window) or when the page becomes visible again; nothing retries on its
own. A refused attach prints its reason in the terminal. Painting a selection
copies it and the footer says whether it reached the clipboard. A seat kept on call
for a human gate is marked on call, and
waiting for you while it holds a pending ask. On a session-channel run the
waiting gate's answer panel offers Talk with the asking formation, which opens
each asked seat's terminal in its own window beside the panel; a window says
when the decision is recorded. Peek does not enter tmux copy mode, resize the
seat window, or end sessions. Closing Peek disconnects only its own clients. Labels and controller/worker roles come from the run's frozen
graph; a new attempt has a new terminal URL. Scroll and selection happen in the
browser; the stream shows the native current screen and subsequent output,
without an API for reading old transcripts or entering historical tmux copy
mode.

Each new `seat_created` records its immutable tmux session/pane IDs and a private
socket/server identity. Linux socket peer credentials, process start time and
boot identity distinguish the original server from a replacement, even when
session IDs are reused. A seat without that proof is unavailable for
Peek. A renamed session is still identified by its recorded immutable ID.
Missing, ended and unavailable seats remain visible with their actual state.
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

A judge must emit exactly one fenced block with exactly these keys:

```archon-verdict
{"verdict":"pass","reason":"Required checks passed","evidence":["check report reference"]}
```

`verdict` is `pass` or `fail`, `reason` a string, and `evidence` an array of
strings. Missing, duplicate, extra or malformed fields/blocks block the run
with `resumeAllowed: false`; neither branch runs. A judge also emits its ordinary
output block, without embedding a second verdict block in it.

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

A finished run succeeds unless a path ended at a rejected End node. Then it
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
stale or duplicate decisions return HTTP 409. There is no default verdict.
The verdict's `reason` is the operator's response, preserved verbatim including
leading/trailing whitespace and newlines. On pass, a nonempty response
travels on every pass route together with the gate's original input. It is typed
with gate ID, gate attempt, requested sequence, deciding actor and text, and
the next prompt renders it as a human-response section after that input. An
empty response routes the input unchanged. On fail the response becomes the
feedback reason. Resume rebuilds the response from the verdict recorded for
that exact request, so it survives restart. Recording the verdict blocks the run
with code `resume_after_verdict` until the coordinator resumes it; that block is
a pause, not a failure. Currently, while a human request waits, no other work
is dispatched; branches not behind the gate run after the verdict, and a
verdict that ends its path ends the run only once they have run. The
cockpit shows a pending human gate's input with a response box, Approve and Send back.
A verdict may carry `relayedBy`, the slot ID of the seat that typed the
operator's confirmed decision (a letter or digit, then up to 63 letters, digits,
underscores or hyphens): `archon gate approve|reject ... --relayed-by <slot-id>`.
It is stored on `human_verdict_recorded` and served beside `decidedBy` on the
gate's recorded decision in the run evidence route; `decidedBy` stays
`human:operator`. Any other value returns HTTP 400 and records nothing, and a
verdict without it is unchanged. The cockpit's run evidence shows a relayed
decision "via" the slot's agent.

Real formations must emit all and only their declared output IDs in one block:

```archon-outputs
{"port_out":{"text":"Short result"}}
```

A payload can also include `ref` naming a text file anywhere on disk,
preferably under the prompt's artifact directory. Use its full absolute
filesystem path; a relative `ref` is refused with `invalid_output_ref`. A
missing, unreadable, non-text or oversized reference blocks routing. Routed
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
turn order, round count or facilitator. The seats use the supplied local
`archon --workspace <state> peer` commands to read, post, wait, propose and
acknowledge; direct file writes are outside the protocol.

Any peer can propose the full output. Every peer, including its author, must
acknowledge that proposal; a contested proposal needs revision and fresh
acknowledgements. A result may accurately preserve unresolved tensions and ask
the operator to decide. The acknowledged result still must satisfy the normal
declared output ports. Acknowledgement of that text does not decide a human gate.

A peer formation with an authored duration spends it on startup, openings,
discussion and finalization. Participants receive the deadline and must leave
time to finish; without a duration the conversation runs until it agrees.
Expiry without a completed valid result blocks visibly and retains the journal
and completed openings. A restart does not replenish the allocation; unresolved
multi-seat execution requires inspection. See
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
say to run the same command again a few seconds after a 409
`coordinator is executing`, and to tell the operator that another
seat or the cockpit decided first after a 409 `human gate request is no longer
pending`. The formation brief's limits still apply, except for that command.

Each pasted ask is recorded as `human_ask_delivered` with the request sequence,
gate, asking formation, slot, the seat's created sequence, session name and
brief path. An ask falls back once, recorded as `human_ask_fallback` with a code
and reason, when no kept seat can receive it (`lab_executor`,
`no_asking_formation`, `no_receivable_seat`) or when every seat that received it
is gone while the request waits (`asked_seats_gone`). If a paste may have
changed a seat's input but submission fails or cannot be verified, the ask
falls back with `delivery_uncertain`. Automatic delivery stops for that
request without clearing the input or pressing Enter again. The operator can
answer in the cockpit or inspect the seat. The fallback records the affected
seat's immutable `seatCreatedSeq`. Later asks also skip that seat, including
after restart, and fall back if no healthy receiver remains. Other peer seats
and replacement seats remain eligible. A verdict arriving during the failed
paste does not discard that seat identity. An agent that is merely busy, or
unsent operator text found before a paste, still waits without a fallback.
Only after a fallback does the notify command, if configured, get its
`human_gate` notification. Both events are
appended under the run's command reservation, so a verdict sent in that moment
waits for it (the busy 409 comes only after five seconds), and replay ignores
them.

Kept seats are reconsidered when the run settles and before a formation is
dispatched. A kept seat ends when it has received an ask and no open request
names its formation (cause `ask_answered`), when its formation starts a new
attempt (`new_attempt`), or when the run is about to succeed, fail or be
canceled, including an abort of a waiting run (`run_final`). Ending waits up to
60 seconds for the agent to go idle with an empty input line, the check every
paste waits on, and kills only that session, by immutable ID, through the
guarded wrapper. Those cleanups are recorded before the final event. A blocked
run's kept seats end just before the cancel or failure that ends it; the ledger
accepts that `seat_cleanup` after `run_blocked` (see the restart procedure). A
blocked run otherwise keeps its seats: a seat found gone while it is blocked is
recorded after it resumes, and so are asks due while it was blocked.

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

## Operator procedure

Read server URL and state directory from the host runbook or environment.
`<archon-server>` must be an HTTP URL with a literal IP, no trailing slash,
credentials, query or fragment. Host forwarding reaches the cockpit over the
tailnet. Source, binary, UI, state, socket and transcript paths are host values,
never mission constants. Set `ARCHON_SOURCE`, `ARCHON_BIN`, `ARCHON_STATE`, `ARCHON_LISTEN`,
`ARCHON_SERVER`, `ARCHON_CWD`, `ARCHON_CHANGE` and `ARCHON_BEAD` accordingly. Keep runtime
state outside the source checkout. Use Go, Node/npm, Bash, curl and jq.

Build and launch a scratch lab daemon, leaving its terminal open:

```bash
umask 077
mkdir -p "$ARCHON_BIN"
cd "$ARCHON_SOURCE/src"
go build -o "$ARCHON_BIN/archon" ./cmd/archon
go build -o "$ARCHON_BIN/archond" ./cmd/archond
cd "$ARCHON_SOURCE/dashboard"
npm ci
npm run build
"$ARCHON_BIN/archond" --executor lab --state-dir "$ARCHON_STATE" \
  --listen "$ARCHON_LISTEN" --ui-dir "$ARCHON_SOURCE/dashboard/dist"
```

For real seats select `--executor tmux` and supply `--socket`, `--tmux-bin`,
`--codex-transcripts`, `--claude-transcripts` from host configuration.
`--cwd` is an optional daemon default for standalone formations. Missions use
their explicit cwd or allocate an automatic workspace as described above.
`--mission-label` is optional. The daemon sets no step time limit (see the
Execution duration field below).
Repeat `--listen` for each trusted interface. `--agents-dir` overrides cards;
installed daemons find `../share/archon/ui` beside their `bin` directory.
Set `--ui-dir ''` to disable the cockpit, or an absolute path to select another
build. These are daemon flags, not model settings. Set harness, model and effort on each slot.
`--notify-command` and `--cockpit-url` configure needs-you notifications,
described at the end of this section. The cockpit opens any file a mission,
brief or gate references by absolute path (see Referenced files).

In another terminal use the compiled Archon. Import means copying mission and
notes TOML, or symlinking them; there is no import command:

```bash
export PATH="$ARCHON_BIN:$PATH"
curl --noproxy '*' --fail --silent --show-error "$ARCHON_SERVER/healthz"
mkdir -p "$ARCHON_STATE/.archon/missions" "$ARCHON_STATE/.archon/notes"
cp "$ARCHON_SOURCE/examples/delivery.mission.toml" "$ARCHON_STATE/.archon/missions/"
cp "$ARCHON_SOURCE/examples/delivery.notes.toml" "$ARCHON_STATE/.archon/notes/"
archon --workspace "$ARCHON_STATE" mission list --json
archon --workspace "$ARCHON_STATE" mission inspect delivery --json
archon --workspace "$ARCHON_STATE" mission notes delivery --json
archon --workspace "$ARCHON_STATE" mission validate delivery --json
archon --workspace "$ARCHON_STATE" mission arrange delivery --json
```

Author through the cockpit, or with Archon's mission, formation, gate, tool and
agent nouns offline (`--workspace`) or through the daemon (`--server`).
`archon mission` lists the mission commands; read command-specific help with
`-h`, including `--server` when using the daemon. Preserve an operator's draft
and notes, staff its slots, write executable briefs, wire exact port IDs, then
validate and arrange. The `archon` skill gives agents the authoring, run and
recovery recipe for this contract. Its source is `skills/archon/` in this
repository, and a release installs it under `lib/archon/current/share/archon`
(`ARCHON_SHARE`). Link it where Claude Code and Codex discover user-level skills,
unless that directory already provides an `archon` skill (a shared catalog, for
example), which stays unchanged: for each of `~/.claude/skills` and
`~/.agents/skills`, `[ -e "$dir/archon/SKILL.md" ] || ln -sn
"$ARCHON_SHARE/skills/archon" "$dir/archon"`. The README gives the full loop.
Arrange (`mission arrange`, the cockpit's Arrange) rewrites only the layout. It
lays columns along the run from the Input card (its out port, formation and
Tool outputs, gate pass), ignores fail edges back to earlier steps and judge
wiring, places judge formations below their gate, and puts nodes the Input card
does not reach after the main path. The same mission always arranges the same
way.
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
top bar or right-click the canvas (End node · done or rejected); an End card
takes any number of wires into its one port, its window and right-click menu
change its outcome, and a finished run lights the End nodes its paths reached. `mission list` lists missions; `formation list <mission>`
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
worded `file "rubric.md" is relative: use an absolute path`. Clicking an
Input card, formation or gate card opens its node window, where every field is
read in full and edited in place: titles, the Input card's goal, inputs, input hint
and files, a formation's type, brief and staffing, and a gate's kinds,
check, criterion, judge and files. Each save is one mission edit with undo. Ports, edges, layout and notes are
unchanged.
Every canvas edit that changes the mission is one undo entry, and Ctrl+Z undoes
the newest. Deleting an Input card, formation, gate or End node is undone by HTTP
`restoreNode` (with `inputCard`, `formation`, `gate` or `end`), which puts the node back with its IDs, fields, staffing, ports,
connections and position in one revision; its notes and wire lanes, kept by
ID, apply again. Removing a port is undone by `restorePort`, which puts it back
in its place with its connections. Undo waits for edits still being saved. When
another editor changed the mission first, undo reloads it and tries once more.
An undo the mission no longer allows is reported once and dropped from the
history, so older entries stay reachable.
The formation window's Execution duration field sets the total seconds for one
formation invocation, including preparation and finalization. Leave it blank
for no time limit. The authored field is
`execution.timeoutSeconds`, set with `setExecution` or `archon formation
set-execution <mission> <formation> --timeout-seconds <n>`; zero clears it. The admitted
run freezes the effective duration, so later edits apply to new runs. Saving or
clearing a duration has its own undo entry.
`archon formation set-type <mission> <formation> <solo|peer|orchestrated>` and the
type chip on a formation card change its type in place. Solo keeps one slot,
peer has at least two slots with no controller, and orchestrated has one
controller (the existing one, else the first slot) and a worker. Added slots
are empty and bound agents stay on the slots that remain. Changing to solo with
more than one staffed slot is refused until `--keep-slot` (API `keepSlotId`)
names the slot to keep; the cockpit offers one choice per staffed slot. Undo
restores the previous slots exactly.

Solo, peer and orchestrated are the only formation types. Creating or changing
to any other type fails with `UNSUPPORTED_FORMATION_TYPE`, listing the three.
Every slot ID in a mission is
unique, because a seat's session is named after its run and slot and a relayed
verdict names its slot. Authoring generates a fresh ID for each new slot, and
restoring slots cannot take another formation's ID. A hand-written or imported
mission that repeats one gets `duplicate_slot_id` on each formation holding it,
from mission validation and run admission.
`mission validate` lists every finding for the whole mission, admission checks
included, as `ERROR`/`WARN` lines or `--json`, and exits 1 on any error.
`mission run` and `formation run` print every admission finding when a start is
rejected. The cockpit tags incomplete nodes as drafts and highlights the nodes
a rejected start names.

For the delivery template the following starts a run without limits; add
`--max-*` flags only when the run needs a cap. Its one input, `change`,
describes the work to deliver; in a lab run it carries the synthetic verdict
described above.

```bash
ARCHON_START=$(archon --server "$ARCHON_SERVER" mission run delivery \
  --input change="$ARCHON_CHANGE" --bead "$ARCHON_BEAD" --json)
ARCHON_RUN_ID=$(printf '%s\n' "$ARCHON_START" | jq -er '.data.runId')
archon --server "$ARCHON_SERVER" run status "$ARCHON_RUN_ID" --json
archon --server "$ARCHON_SERVER" run logs "$ARCHON_RUN_ID" --json
archon --server "$ARCHON_SERVER" run follow "$ARCHON_RUN_ID" --json
archon --server "$ARCHON_SERVER" run wait "$ARCHON_RUN_ID" --until needs-you
archon --server "$ARCHON_SERVER" run list --json
```

This allocates a workspace automatically. Add `--cwd "$ARCHON_CWD"` to work in
an existing project. For a session gate, inspect the asking seats before typing:

```bash
archon --server "$ARCHON_SERVER" run gates "$ARCHON_RUN_ID"
archon --server "$ARCHON_SERVER" gate request "$ARCHON_RUN_ID" "$ARCHON_GATE_ID"
archon --server "$ARCHON_SERVER" run seats "$ARCHON_RUN_ID"
tmux -S "$ARCHON_TMUX_SOCKET" list-panes -a -F '#{session_name} #{pane_id}'
```

To stop a selected non-final run, set `ARCHON_RUN_ID` to its ID:

```bash
archon --server "$ARCHON_SERVER" run abort "$ARCHON_RUN_ID" --reason "Operator stopped this run" --json
```

For a graph with a human gate, inspect fresh status. Set `ARCHON_GATE_ID` to the
pending gate and `ARCHON_RESPONSE` to the operator's answer. Choose exactly one verdict command, only when authorized to
decide that gate:

```bash
ARCHON_STATUS=$(archon --server "$ARCHON_SERVER" run status "$ARCHON_RUN_ID" --json)
ARCHON_REQUESTED_SEQ=$(printf '%s\n' "$ARCHON_STATUS" | jq -er --arg gate "$ARCHON_GATE_ID" \
  '.data.waitingGates[] | select(.gateId == $gate) | .requestedSeq')
archon --server "$ARCHON_SERVER" gate approve "$ARCHON_RUN_ID" "$ARCHON_GATE_ID" \
  --requested-seq "$ARCHON_REQUESTED_SEQ" --response "$ARCHON_RESPONSE" --json
```

For rejection, substitute this command for approval:

```bash
archon --server "$ARCHON_SERVER" gate reject "$ARCHON_RUN_ID" "$ARCHON_GATE_ID" \
  --requested-seq "$ARCHON_REQUESTED_SEQ" --response "$ARCHON_RESPONSE" --json
```

Restart the daemon with the same state directory and configuration. Before
listening it scans non-final ledgers, recovers eligible completed native evidence
or records a block naming unresolved dispatches. It never adopts or cleans up
old seats, except that it verifies seats kept on call and keeps managing them
(see [Human gates on the session
channel](#human-gates-on-the-session-channel)). Preserve those identities for
host-owner inspection. SIGTERM fences new commands and downstream dispatch
immediately. Active turns have five
seconds to finish, then the daemon cancels observation and detaches from its
seats without ending them. HTTP draining and execution share a ten-second total
shutdown budget. Open dispatch identities remain in a resumable block for
startup recovery; an idle human request remains answerable. Abort runs explicitly
when seat cancellation is intended.

The ledger accepts nothing after a final event. After `run_blocked` it accepts
only a resume, a cancel, a failure, or the `seat_cleanup` of seats kept on call,
which the runtime records just before the cancel or failure that ends the run.
Those cleanups leave the run blocked. A ledger ending in them, as a crash
between the cleanup and the cancel leaves it, projects the block's status,
`resumeAllowed` and needs-you asks, is recovered at startup as that block, and
still accepts a resume, cancel or failure.

The writer lock is retained until all admitted execution and authoring writes
settle. If a worker ignores cancellation, shutdown returns an error at the
deadline and retains that lock until the worker or process exits. The host's
service watchdog is an outer limit, not the normal shutdown mechanism; keep
deployment and live restart verification in the owning host repository.

Inspect list/status after restart. For a resumable block whose cause is resolved:

```bash
archon --server "$ARCHON_SERVER" run resume "$ARCHON_RUN_ID" --reason "Recovery evidence inspected" --json
```

An idle human request with no unresolved dispatch survives restart with its
original `requestedSeq`. Approve or reject using the same exact request sequence after restart.

When a seat died mid-turn and its completed evidence cannot be found, abandon
the open dispatch and run the node again as a fresh bounded attempt:

```bash
archon --server "$ARCHON_SERVER" run resume "$ARCHON_RUN_ID" --mode redispatch --reason "Seat lost; run the node again" --json
```

The abandoned dispatch is recorded as a `slot_result` with status `abandoned`;
the node's next attempt counts against `maxAttempts` when the run set one. A failed reattach never
finishes the run: it records `dispatch_reattach_failed` with the reason and
leaves the run blocked and resumable.

Resume does not manufacture missing completion or resend an uncertain task.
An unresolved dispatch can block again. For explicitly selected completed native
evidence, restart with `--resume-run`, `--completed-transcript` and
`--completed-brief` together, naming the blocked resumable run, absolute native
transcript and original brief. Validation checks the original digest, pointer,
cwd, session, model/effort and completed turn before continuation. Keep original
artifacts intact. Do not run offline runtime mutations alongside the daemon.

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
- `blocked`, keyed by the `run_blocked` sequence when no open gate or escalation
  already explains the block. The body gives the reason and, when resumable,
  the `run resume` command.
- `final`, keyed by the terminal event sequence.

Only settled runs are announced: the run's command worker has exited. A block
recorded inside a command, such as a human verdict awaiting its automatic
resume, is never sent. Each ask is sent once and recorded in the run's
`.needs-you.json` artifact, so restarts send no duplicates. A failed send stays
unrecorded and is retried at the run's next settle, at startup and every five
minutes. Sends never delay runs or shutdown. At startup the daemon reconciles
only non-final runs. A final outcome is sent only for a run that finished while
this daemon process was running, so enabling notifications never mails old
results.

A placeholder host command that reads the JSON and hands the message to a
host sender:

```sh
#!/bin/sh
set -eu
notification=$(cat)
subject=$(printf '%s' "$notification" | jq -r '.subject')
printf '%s' "$notification" | jq -r '.body' |
  "$NOTIFY_SENDER" --to "$NOTIFY_TO" --subject "$subject"
```

`NOTIFY_SENDER` and `NOTIFY_TO` stand for host configuration. Keep the real
script, sender and address with the host deployment.

## HTTP contract

[OpenAPI](openapi/archon.yaml) lists the served routes. Except for the raw
theme document described below, JSON responses use
`{success,timestamp,data}`; errors carry an error object. Mission authoring
under `/api/missions` includes list/create/read/patch/delete, notes, layout,
validation and change polling. A mission read or edit answers `data.mission`
(with `layout` and the created or changed node, such as `inputCard`, where it
applies); a run starts with `mission` and `inputCardId` (or `formationId`), its
`inputs` and the other run fields. The
Input card patch actions are `createInputCard`, `updateInputCard` and
`deleteInputCard`. Agent routes
list/create/read/patch persona cards; the roster also serves `harnesses` (each
with the efforts it accepts) and `effortPolicy` (`{effort,use}` lines). Gate
profiles expose the two code checks.
With the tmux executor the agent roster marks a persona live when a session named
by its default session stem runs on `--socket`, and lists the socket's other
sessions as unbound. The lab executor reports every agent offline.
Revision and ETag checks protect edits. A mission edit that leaves the mission
as it was (the same slot assignment, title, brief, type, controller, gate or
Input card fields, or judge chain) saves nothing: it answers 200 with the
current mission, its revision and ETag unchanged, and a stale ETag still conflicts. Tool
and note edits still save a revision. Runtime routes start/list/read runs
(`GET /api/runs?mission=<slug>` lists the runs of the mission now under that
slug or ID, and none for a mission that does not exist; `needs=you` keeps the
open runs waiting at a human gate or blocked), read projected
events/escalations, stream SSE, abort, resume, read run evidence and record
exact human verdicts. They all use the coordinator; no request-local executor
exists.
There is no generic file reader, transcript endpoint, mission import endpoint or
authentication layer. Run evidence reads only one run's ledger, its artifact
directory and the briefs its own dispatches recorded.

With `--server`, Archon runs these authoring and read commands through the
daemon, so an open cockpit sees the edits through its change polling: `mission
new|list|inspect|notes|note|validate|arrange|create|update|input|wire`, `formation
list|inspect|create|rename|set-type|assign|unassign|set-brief|add-input|add-output|wire|unwire`,
`gate create|update|judge`, `end create|update|delete`, `tool create|update|delete|inspect` and `agent
list|inspect|new|edit`. They take the offline flags and print the offline
output: unwrapped JSON without TOML, or the same text. Each command reads the
document it changes, resolves formation, gate, mission and Tool selectors from
that read, and writes with its ETag and mission revision; Tool writes also carry
the layout's state and ETag. A write that loses to another editor is read and
retried up to three times. Differences from offline use:

- Error messages come from the daemon (`coordinator HTTP <status>: ...`). JSON
  error codes, boundaries and selectors match.
- Agent cards are the daemon's `--agents-dir`, with the liveness the daemon
  reports, and `agent new --from` names a path on the daemon host. `mission note
  --file` reads locally.
- Runtime commands (`mission run`, `formation run`, `run`, `gate approve|reject`) print the
  daemon's `{success,timestamp,data}` envelope, except `run gates`, `run seats`
  and `gate request`, which require `--json` for that format, and `run wait`,
  which prints its paragraph or its own JSON (see [Waiting on a run](#waiting-on-a-run)); `mission list` and
  `mission inspect` print offline JSON like the other reads. `run ask` and
  `agent spawn|attach|retire` remain offline only.

`GET /api/missions/{mission}/validation` returns
`{missionRev,missionEtag,errors,warnings}` for the whole mission, the same report as
`mission validate`.

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
theme request; it applies the theme once per page load. No theme-art routes,
picker or polling are provided. Host paths and deployment belong to the host.

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
  and the final `verdict` with `perKind` and `routePort`. `problems` lists the
  blocks and errors recorded against the node, including a block whose open
  dispatches name it. Each text is capped at 64 KiB;
  a kind result or verdict lists at most 100 evidence items and counts the rest
  in `evidenceOmitted`; one response carries at most 2 MiB of text, after which
  texts are empty and truncated. An unknown node returns 404.
- `/api/runs/{runId}/evidence/problems` returns `data.problems`,
  every block and error of the run and the `run_failed` or `run_canceled` that
  ended it, oldest first: `seq`, `type`, `code`, `reason`, `resumeAllowed` and
  `nodeIds`, the nodes it names (its node or gate, the blocked node or gate,
  and nodes with open dispatches). A block that names no node, such as an
  exceeded wall clock, has empty `nodeIds` and its reason. A run end that names
  no node lists the nodes it stopped: formations without output and gates
  without a verdict. A run end carries `actor`, who ended it (`archond` for a
  coordinator failure, whose cause is its `reason` and code its `code`). A
  block the run later resumed carries `resumedSeq`; a limit block carries
  `limit`. Node evidence `problems` include the same run end for the nodes it
  stopped. The 2 MiB budget is spent on the latest first.
- `/api/runs/{runId}/evidence/briefs/{dispatchSeq}` returns
  `data.brief` (`dispatchSeq`, `nodeId`, `slotId`, `attempt`, `text` capped at
  256 KiB): the brief that this run's `slot_dispatch` at that sequence sent to
  its seat, read from the path that event recorded. Any other sequence
  returns 404.
- `/api/runs/{runId}/evidence/artifacts` returns `data.artifacts`
  (`name`, `size`, `modifiedAt`), sorted by name relative to
  `<state-dir>/.archon/artifacts/<runId>`, and `data.truncated` past 500
  entries or 8 directory levels. A run without artifacts lists none.
- `/api/runs/{runId}/evidence/artifacts/{name...}` returns
  `data.artifact` (`name`, `size`, `modifiedAt`, `kind` `markdown`, `json`,
  `text`, `image`, `pdf` or `binary`, and `path`, its absolute path on the
  daemon host), with `text` capped at 256 KiB for textual kinds. A `.pdf` file
  that starts with `%PDF-` is `pdf`.
- `/api/runs/{runId}/evidence/mission` returns `data.mission`
  (`missionRev`, `text` capped at 256 KiB): the mission's TOML as the run froze
  it at admission, which later edits never change.
- `/api/runs/{runId}/artifacts/{name...}` returns the artifact's bytes
  up to 16 MiB; larger files return 413. Text is `text/plain; charset=utf-8`
  and redacted, PNG, JPEG, GIF and WebP keep their image type, a `pdf` is an
  inline `application/pdf`, and anything else is an `application/octet-stream`
  attachment. Responses carry
  `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox` and
  `Cache-Control: no-store`, and support ranges. The file window is a reader,
  so HTML is served as text and nothing a file contains runs in the cockpit.
- `/api/runs/{runId}/gates/{gateId}/request` is the evidence API's
  view of a human request still waiting for an answer. For the gate's latest
  request, while it is pending, it returns `gateId`, `requestedSeq`, the frozen
  `criterion` and the routed input: `fromNodeId`, `fromPortId`, `text` capped at
  64 KiB, and `truncated`. `routes` says where each verdict leads on the
  run's frozen mission: `verdict` (`pass`, `fail`), `targets` (`nodeId`,
  `title`, `kind`, and for a formation the `attempt` it would start, the
  run's `maxAttempts` (omitted when the run set none, so attempts are
  unlimited; the engine applies the same rule) and
  `waitsForInputs` for a join still missing another input; for an End node
  kind `end` and its `outcome`), `endsRun` when every route of the verdict
  ends its path at an End node and nothing else in the run can still run,
  `runFails` when the run fails once it ends, at once with `endsRun` or else
  after its other open work (a rejected End node on this route, or a path
  already rejected), `dispatches` (`used`, `max`) and `dispatchesNeeded` (judges
  included) when the route starts formations under a dispatch limit, and
  `limit` when a limit the route needs is already spent, so taking it blocks
  the run. When the frozen mission cannot be read, `routes` is omitted. An
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
  the text, capped at 4 KiB, with `bytes` and `truncated`) and `routes`, and a
  block's `reason`, `code` and `resumeAllowed`. A driver agent consumes this
  text, so it is served verbatim, as `gate request` serves it. `end` carries `status`, `seq`, `code`, `reason`, `endedBy` and the
  `stopped` steps. A bad `until`, `since` or `hold`, or a `since` past the
  run's last event, returns 400, an unknown run 404, and a stopping daemon 503.

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
- `GET /api/files/raw?path=<ref>` returns the bytes up to 16 MiB with
  the raw artifact route's content types and headers; larger files return 413.

A file the daemon's user may not read returns 403. A missing path, a
directory or another non-regular file returns 404, because reading a FIFO or
device would never finish. Served text is redacted like run evidence.

The cockpit shows each referenced file as a chip on its card: a mission's and a
gate's files and a formation's brief files. A gate's card also shows the brief
files of the formations judging it. A card shows the first few chips and lists
the rest under +N. A chip opens the file in a floating file window, and a file
the daemon cannot read opens with the daemon's reason and its path. Arrange
reserves a chip row under a card that has referenced files.
