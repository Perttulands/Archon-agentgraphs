# Archon contract

Archon builds and runs work graphs with agents and gates. `archond` owns
execution and serves the Archon UI. The `archon` CLI authors the same
definitions and sends runtime commands to the daemon. This is a trusted-operator
service with concurrent missions, two native harness adapters and durable run
history. Host deployment, forwarding and CHROTE integration live outside this
repository. [ADR-0016](adr/0016-daily-capability.md) records the daily-capability
decisions; [examples](../examples/) provide reusable boards.

## Names and compatibility

Archon ships the `archon` CLI, `archond` daemon and browser UI together.
`formationsd` remains a compatibility entrypoint for the same daemon. Existing
`.formations` storage, `/api/formations` routes, `form-` session names, browser
preferences and `CHROTE_*` configuration/protocol identifiers retain their exact
spelling. Existing boards and run history need no migration. A formation remains
the domain name for an execution node. Historical designs use the earlier
product names; they do not define current behavior.

## Definitions and storage

| Noun | Meaning |
| --- | --- |
| Board | A schema-1 TOML graph with a stable ID, slug and revision. |
| Mission | An entry node with a goal and `out` port. A run supplies its input text. |
| Formation | An execution node. `solo` has one seat; `peer` has peer seats; `orchestrated` has a controller directing its bound workers. |
| Slot | A position in a formation, bound to a persona and harness variant. A seat is the slot's runtime agent session. |
| Persona | A TOML agent card with a summary, capabilities and harness variants. Presets remain available; local cards can override them. |
| Harness variant | The persona's `openai-codex` or `claude-code` settings, including session stem, model and effort. Omitted effort resolves to `medium`; omitted model uses the harness default. |
| Gate | A criterion with one or more kinds: `code`, `formation`, `human`. Its ports are `in`, `pass`, `fail`, `judge`. |
| Connection | A directed edge between `node-id:port-id` endpoints. Formation input and output ports have explicit IDs. |
| Judge chain | Formations wired from a gate's `judge` port and back to that same port. The final judge result decides the formation kind. |
| Pushback edge | A gate's `fail` connection back to work, delivering feedback and starting a bounded next attempt. There is no `retry_control` port. |
| Run | One admitted mission or isolated formation, with definition and persona snapshots, inputs and limits. Later edits affect later runs. |
| Ledger | Private append-only NDJSON events, ordered by sequence. It records dispatch, results, routing and recovery evidence. |
| Projection | A sanitized view derived from the ledger, shared by HTTP, Archon and the cockpit. |

The private `<state-dir>` is also Archon's `--workspace`. It contains
`.formations/boards/*.formation.toml`, `.formations/notes/*.notes.toml`, layouts,
`.formations/runs` ledgers and snapshots, `.formations/artifacts`, and `briefs`.
Persona cards default to `<state-dir>/agents`; daemon `--agents-dir` can select
another absolute directory. Match offline persona authoring to that directory
with `CHROTE_AGENTS_DIR` when using an override. Notes are operator intent, not
automatically executable briefs. Read board and element notes, then translate
them into formation briefs, staffing and edges.

Each board or element note is a thread of entries, oldest first. An entry has an
ID, an author (`human:<name>` or `agent:<name>`), a creation time, an optional
edit time and text. `archon board note` appends an entry, by default as
`agent:archon` (`--author` names another). Only an entry's author can change it:
`--entry <id> --text` edits it and `--entry <id> --clear` deletes it. Reply
rather than rewriting someone else's note. The cockpit writes as `human:ui`. It
shows notes on the canvas in their own layer above the cards, as a preview of
each thread's latest entry, the full thread, or hidden, with the operator's and
agents' entries styled apart. A card's note pin or sticky, or Board notes, opens
a thread in a floating window to reply, and to edit or delete your own entries. Notes files written before threads (schema 1) still load, each text as
one human entry, and are saved as schema 2 on the next write.

A minimal board file, `hello.formation.toml`, has one staffed formation:

```toml
schema = 1
id = "brd_hello"
slug = "hello"
title = "Hello"
rev = 1

[[mission]]
id = "mis_hello"
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
controller = true

[[connection]]
id = "edge_start"
from = "mis_hello:out"
to = "fmn_work:port_in"
```

Authoring accepts drafts through the cockpit, HTTP and Archon. Board, mission,
formation, gate, note and persona fields may be blank or partial: a title, goal,
mission Bead ID, criterion or code check value can be left out. Blank values
take defaults where a node needs one: a formation becomes `solo`, a persona kind
becomes `specialist`, a board named in neither title nor slug becomes
`Untitled board`. A supplied value must still be well formed, so an unsafe
Bead ID, an unknown formation type or an unknown code check profile is rejected
on write. Only run admission and `board validate` reject incomplete work.

The [delivery board](../examples/delivery.formation.toml) and its
[notes](../examples/delivery.notes.toml) add Plan, Beads, a Beads-review judge,
orchestrated Execution and Final review. Six `delivery-*` presets staff it.
Execution uses a Claude controller and three Codex workers; Final review uses
Astra. Failed Beads review returns directly to Beads. Final review produces a
report, with no following gate. This graph has no human gate.

## Execution and authority

Runtime authority enforcement is deliberately disabled.
`RequireRuntimeAuthority` returns nil, so the trusted operator's runtime effects
proceed. The seam remains, but schema-2 certification and same-UID isolation are
not implemented. Do not interpret the archived authority target as a runtime
restriction or security guarantee. Listeners have no authentication; configure
only trusted interfaces and the host's network perimeter.

One coordinator locks a state directory. Many runs execute concurrently within
it. Admission validates the graph and inputs, snapshots definitions, durably
appends `run_started`, then returns HTTP 202.

New runs freeze each complete persona card, its selected model setting and its
resolved effort in a private schema-2 bindings snapshot. Tmux, lab execution and
completed-turn recovery use that snapshot after edits, retries and restarts.
An omitted model freezes the choice to use the harness default, which remains
unpinned; specify a model in the persona to retain an exact model setting.
Older schema-1 bindings have only paths and hashes. Their history remains
readable, but seat execution and recovery block with
`persona_snapshot_incomplete` instead of substituting today's persona. Start a
new run to use current personas.

Admission first checks that `expectedRev` and any `If-Match` name the current
board (HTTP 409 otherwise). It then builds one report of every problem the run
would hit: board validation plus supported formation types, slots with readable
personas and harness variants, one controller and a worker in each orchestrated
formation, complete code checks, judge chains, runnable Tools and a wired
mission. Findings cover the nodes the run reaches from its mission, or the
selected formation. Formation types, slot counts and persona bindings are
checked across the whole board, because the run snapshot binds every formation.
Any finding rejects the start with HTTP 422, error code `RUN_ADMISSION_FAILED`
and `error.findings` as `{code,nodeId,message}` entries; no run is recorded.
The engine's own fail-fast checks remain behind this report. The worker continues after the
client disconnects. A missing receipt requires checking the run list before
starting again. `cwd` must be an absolute existing directory and `brief` must be
nonempty. Archon reads an existing brief file or sends the argument as literal
text. The brief becomes mission output; the board goal remains prompt context.
`beadId` is optional in the API but should identify the owning task.

HTTP admission requires positive `maxDispatch`, `maxAttempts` and
`wallClockSeconds`, with `redact` false. Remote Archon defaults to 3 dispatches,
3 attempts and 7200 seconds. Set limits explicitly for larger graphs. Dispatch
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
`wall_clock_exceeded`. Seat timeout separately bounds real agent execution.
Limit exhaustion and unresolved execution leave visible blocks rather than
claiming success.

The projection reports `running`, `waiting_human`, `blocked`, `succeeded`,
`failed` or `canceled`. Always check `final` and `resumeAllowed`; a blocked run
is not a completed delivery. Events expose node, slot and gate identities,
attempt, status/verdict, session display name and cleanup outcome where
applicable.
Typical sequences include `run_started`, `node_started`, `slot_dispatch`,
`seat_created`, `seat_prompt_consumed`, `slot_result`, `seat_cleanup`,
`gate_kind_result`, `gate_verdict`, and a run outcome. Malformed judges emit
`judge_attempt_failed`. Session-channel runs add `human_ask_delivered` and
`human_ask_fallback` (see [Human gates on the session
channel](#human-gates-on-the-session-channel)). Pending human requests appear in
`waitingGates` with `gateId` and `requestedSeq`.

The public projection includes cwd and Bead ID but excludes prompt text,
artifact contents, brief paths, native session IDs and arbitrary private event
data. Projections and SSE stay sanitized; the run evidence routes under
[HTTP contract](#http-contract) serve a run's outputs, gate results, human
responses, briefs and artifacts to the operator.
`run logs` is the same sanitized projection as `run status`. `run follow` prints
complete enveloped projections from SSE after durable changes, waits through
human gates and closes only at finality. Interrupting that client stops viewing,
not execution. Abort cancels only the selected run and waits for owned-seat
cleanup, including seats kept on call, before returning `canceled`.

The `tmux` executor uses one implementation with Claude Code and OpenAI Codex
adapters. It resolves authenticated harness executables from its environment.
Each fresh seat gets a pointer to a file under `<state-dir>/briefs`. The file
contains run/node/slot identity, cwd, mission goal, Bead, persona summary,
formation brief, file/link references, routed inputs, gate feedback, human
responses, output ports, artifact directory and completion instructions.
Orchestrated controllers also get their bound workers and may direct only those
workers. The operator may type into any live seat at any time, through the seat
terminal or in CHROTE, and talk to the agent normally, whether it is working a
dispatch or idle. The runtime pastes a brief only while the agent is idle and its
input line is empty, waiting within the seat timeout, so a brief never lands
mid-turn or on the operator's unsent text. It submits the brief once its pointer
shows in the input line, however the harness wraps it. Archon agents must not
type into seats or manage their sessions.

Sessions are named `form-<run>-<slot>`, optionally prefixed by `--mission-label`.
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

Native completion must match the exact pointer, cwd, persona model/effort,
native session, and a natively finished agent turn carrying this run's
completion sentinel: Codex task completion, or Claude's end_turn assistant
message. A marker alone is insufficient, and another run's sentinel never
completes a dispatch. The operator's turns after the pointer, typed or queued
messages and interrupts, neither complete nor fail the dispatch, and the
completing turn may come after them. A turn that finishes without the sentinel
fails the dispatch at once only when nobody else took a turn during it, and for
Claude only when the agent left no background work that resumes the
conversation; otherwise the dispatch waits within the seat timeout. The dispatch
still fails loudly when the seat ends, when the model or effort changes, or when
the harness moves to another conversation (`/clear`, `/new` or `/resume`).
The `lab` executor creates no tmux sessions and echoes deterministic inputs.
It writes each rendered brief to `<state-dir>/briefs/lab-*.md`, as a seat would
receive it. It proves routing, not agent work or the truth of a review.

The cockpit lists a board's runs from the daemon, so runs started by the CLI,
an agent or another browser appear. It shows the open run that most needs the
operator: waiting for a human, then running, then blocked, newest first. A
picker switches between open runs and the board's ten most recent finished
runs; when no run is open, the run bar still offers the finished ones, and a
reopened finished run can be put away again. `/?board=<slug>&run=<runId>`, the link that
notifications carry, opens that board and keeps that run shown. The address bar
keeps a chosen run across reloads, and an unknown linked board or run is
reported rather than silently replaced.

The cockpit shows what a run produced, read from the evidence routes. Each step's
card lists its latest output as chips: artifact files its ports name, the text
of ports that name no file, and the seat's report when it adds something. The
run bar's Produced list leads with a finished run's final steps (those whose
outputs feed nothing), or a running run's latest step, and a menu holds the
rest, including artifact files no output names. A chip opens the file in a
floating file window, rendered by kind, with Open raw and Copy path (relative to
the state directory) for artifacts; several can be open side by side.

The cockpit's floating Peek attaches to an owned live seat and sends typing and
resize as CHROTE's terminals do, through the seat terminal WebSocket below. The
operator types to the agent whether it is working a dispatch, on call or idle,
and the terminal fits its window and sends that size, which sizes only its own
view. A seat kept on call for a human gate is marked on call, and
waiting for you while it holds a pending ask. On a session-channel run the
waiting gate's answer panel offers Talk with the asking formation, which opens
each asked seat's terminal in its own window beside the panel; a window says
when the decision is recorded. Peek does not enter tmux copy mode, resize the
seat window, or end sessions. Switching seats and closing Peek disconnect only
its own client. Labels and controller/worker roles come from the run's frozen
graph; a new attempt has a new terminal URL. Scroll and selection happen in the
browser; the stream shows the native current screen and subsequent output,
without an API for reading old transcripts or entering historical tmux copy
mode.

Each new `seat_created` records its immutable tmux session/pane IDs and a private
socket/server identity. Linux socket peer credentials, process start time and
boot identity distinguish the original server from a replacement, even when
session IDs are reused. A legacy seat without that proof is unavailable for
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
the `code` kind. The cockpit editor, the `updateGate` board patch and `archon gate
update` change a gate through one store path. They set only the fields given,
and an empty value clears one. A gate keeps only the configuration its kinds
use: dropping `formation` detaches the judge chain, as detaching the judge does,
and dropping `code` clears the check. Detaching the judge from a gate whose only
kind is `formation` leaves a `human` gate. Adding `formation` without a chain
leaves a draft finding until a judge is attached. Converting a code gate to a human
gate is `archon gate update <board> <gate> --kinds human`; `--clear-check`
clears the check while keeping the code kind. It does not run arbitrary shell commands.
Use a judge formation to execute checks such as Beads lint or code review.

A judge must emit exactly one fenced block with exactly these keys:

```chrote-verdict
{"verdict":"pass","reason":"Required checks passed","evidence":["check report reference"]}
```

`verdict` is `pass` or `fail`, `reason` a string, and `evidence` an array of
strings. Missing, duplicate, extra or malformed fields/blocks block the run
with `resumeAllowed: false`; neither branch runs. A judge also emits its ordinary
output block, without embedding a second verdict block in it.

A fail edge delivers typed feedback containing gate ID, gate attempt, verdict,
reason, evidence and original input text/reference. The next prompt renders
this as a gate-feedback section. An unwired fail leaves a visible block.
A pushback also holds when the failing gate was fed by another gate's pass:
resume never re-delivers an input a gate has already evaluated, so the pushback
target runs first. A pass cannot finish a run while a fail verdict's target has
not yet acted on its feedback.
A human kind waits for an explicit verdict naming the exact pending sequence;
stale or duplicate decisions return HTTP 409. There is no default verdict.
The verdict's `reason` is the operator's response. On pass, a nonempty response
travels on every pass route together with the gate's original input. It is typed
with gate ID, gate attempt, requested sequence, deciding actor and text, and
the next prompt renders it as a human-response section after that input. An
empty response routes the input unchanged. On fail the response becomes the
feedback reason. Resume rebuilds the response from the verdict recorded for
that exact request, so it survives restart. Recording the verdict blocks the run
with code `resume_after_verdict` until the coordinator resumes it; that block is
a pause, not a failure. The cockpit shows a pending human gate's input with a
response box, Approve and Send back.
A verdict may carry `relayedBy`, the slot ID of the seat that typed the
operator's confirmed decision (a letter or digit, then up to 63 letters, digits,
underscores or hyphens): `archon gate approve|reject ... --relayed-by <slot-id>`.
It is stored on `human_verdict_recorded` and served beside `decidedBy` on the
gate's recorded decision in the run evidence route; `decidedBy` stays
`human:operator`. Any other value returns HTTP 400 and records nothing, and a
verdict without it is unchanged. The cockpit's run evidence shows a relayed
decision "via" the slot's agent.

Real formations must emit all and only their declared output IDs in one block:

```chrote-outputs
{"port_out":{"text":"Short result"}}
```

A payload can also include `ref` naming a text artifact created under the
prompt's artifact directory or another configured root. Invalid, missing,
oversized or escaped references block routing. Free-form answer text is not
routed. Finish with the exact run ID substituted in the sentinel:

```text
<<<CHROTE-DONE run-id=<run-id> status=ok artifact=<path-or-ref>>>
```

For a lab judge smoke test, put one synthetic `chrote-verdict` block in the run
brief. The lab echo carries it to the judge parser. Peer and orchestrated
formations echo it once per seat; the lab keeps one copy of identical blocks,
so the fixture reaches a judge downstream of them, while differing blocks
still block. Label that evidence as simulated; a plain brief without a verdict
will block at a formation gate.

### Human gates on the session channel

A run freezes its mission's `humanChannel`
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
answer in the cockpit or inspect the seat. An agent that is merely busy, or
unsent operator text found before a paste, still waits without a fallback.
Only after a fallback does the notify command, if configured, get its
`human_gate` notification. Both events are
appended under the run's command reservation, so a verdict sent in that moment
gets the busy 409, and replay ignores them.

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

## Operator procedure

Read server URL and state directory from the host runbook or environment.
`<formations-server>` must be an HTTP URL with a literal IP, no trailing slash,
credentials, query or fragment. Host forwarding reaches the cockpit over the
tailnet. Source, binary, UI, state, socket and transcript paths are host values,
never board constants. Set `FORM_SOURCE`, `FORM_BIN`, `FORM_STATE`, `FORM_LISTEN`,
`FORM_SERVER`, `FORM_CWD`, `FORM_BRIEF` and `FORM_BEAD` accordingly. Keep runtime
state outside the source checkout. Use Go, Node/npm, Bash, curl and jq.

Build and launch a scratch lab daemon, leaving its terminal open:

```bash
umask 077
mkdir -p "$FORM_BIN"
cd "$FORM_SOURCE/src"
go build -o "$FORM_BIN/archon" ./cmd/archon
go build -o "$FORM_BIN/archond" ./cmd/archond
cd "$FORM_SOURCE/dashboard"
npm ci
npm run build
"$FORM_BIN/archond" --executor lab --state-dir "$FORM_STATE" \
  --listen "$FORM_LISTEN" --ui-dir "$FORM_SOURCE/dashboard/dist"
```

For real seats select `--executor tmux` and supply `--socket`, `--tmux-bin`,
`--codex-transcripts`, `--claude-transcripts` from host configuration.
`--cwd` is an optional daemon default; missions supply their own cwd.
`--mission-label` is optional and `--seat-timeout` defaults to `30m`.
Repeat `--listen` for each trusted interface. `--agents-dir` overrides cards;
installed daemons find `../share/archon/ui` beside their `bin` directory.
Set `--ui-dir ''` to disable the cockpit, or an absolute path to select another
build. These are daemon flags, not model settings. Set model and effort on persona harness variants.
`--notify-command` and `--cockpit-url` configure needs-you notifications,
described at the end of this section. Repeat `--file-root <absolute-dir>` for
each directory whose files missions, briefs and gates may reference; the
cockpit reads referenced files only under those roots (see Referenced files).

In another terminal use the compiled Archon. Import means copying board and
notes TOML; there is no import command:

```bash
export PATH="$FORM_BIN:$PATH"
curl --noproxy '*' --fail --silent --show-error "$FORM_SERVER/healthz"
mkdir -p "$FORM_STATE/.formations/boards" "$FORM_STATE/.formations/notes"
cp "$FORM_SOURCE/examples/delivery.formation.toml" "$FORM_STATE/.formations/boards/"
cp "$FORM_SOURCE/examples/delivery.notes.toml" "$FORM_STATE/.formations/notes/"
archon --workspace "$FORM_STATE" board list --json
archon --workspace "$FORM_STATE" board inspect delivery --json
archon --workspace "$FORM_STATE" board notes delivery --json
archon --workspace "$FORM_STATE" board validate delivery --json
archon --workspace "$FORM_STATE" board arrange delivery --json
```

Author through the cockpit, or with Archon's board, mission, formation, gate,
tool and agent nouns offline (`--workspace`) or through the daemon (`--server`).
Read command-specific help with `-h`, including `--server` when using the
daemon. Preserve an operator's draft
and notes, staff its slots, write executable briefs, wire exact port IDs, then
validate and arrange. The shared `archon` skill gives an authoring recipe.
Arrange (`board arrange`, the cockpit's Arrange) rewrites only the layout. It
lays columns along the run from each mission (mission out, formation and Tool
outputs, gate pass), ignores fail edges back to earlier steps and judge wiring,
places judge formations below their gate, and puts nodes no mission reaches
after the main path. The same board always arranges the same way.
`mission create`, `formation create` and `gate create` print `created <id>`, or
with `--json` `{board, layout, mission|formation|gate}` naming the new node.
`board list` lists boards; `mission list <board>` and `formation list <board>`
list that board's missions and formations, each formation with its slots and
staffing. `mission inspect <board> <mission>` prints one mission with its
reachable chain, and `formation inspect <board> <formation>` one formation with
its slots, ports, brief and the connections at its ports; `board inspect` prints
the whole board.
Nodes keep their IDs when edited: `archon formation rename <board> <formation>
<title>`, `archon mission update <board> <mission>` with `--title`, `--goal`,
`--bead` or `--input-hint`, and `archon gate update --title` change only what
they name, and an empty value clears a field. A mission's input hint says what a
run brief should contain; Start mission shows it. A mission's `humanChannel`
(`--human-channel` on `mission create|update`) records how its runs' human gates
reach the operator ([ADR-0019](adr/0019-human-channel-agent-session.md)):
`notify`, the default, or `session`. `notify` and an empty value store no
channel, any other value is refused with the allowed values and nothing is
saved, and a run keeps the channel of its frozen board. On `session`, human
asks reach the asking formation's kept seats; only a recorded delivery fallback
sends them to `--notify-command`. Escalations, blocks and final outcomes use
the notify command on either channel. Missions and gates carry
reference files, such as a gate's rubric, the way formation briefs do: `--file
<path>` on `mission create|update` and `gate create|update` (API `files`),
repeated for more. On update the given files replace the list, and `--file ''`
clears it. A path is absolute or relative to a daemon file root. Clicking a
mission, formation or gate card opens its node window, where every field is read
in full and edited in place: titles, a mission's goal, input hint, Bead ID and
files, a formation's type, brief and staffing, and a gate's kinds, check,
criterion, judge and files. Each save is one board edit with undo. Ports, edges, layout and notes are
unchanged.
`archon formation set-type <board> <formation> <solo|peer|orchestrated>` and the
type chip on a formation card change its type in place. Solo keeps one slot,
peer has at least two slots with no controller, and orchestrated has one
controller (the existing one, else the first slot) and a worker. Added slots
are empty and bound agents stay on the slots that remain. Changing to solo with
more than one staffed slot is refused until `--keep-slot` (API `keepSlotId`)
names the slot to keep; the cockpit offers one choice per staffed slot. Undo
restores the previous slots exactly.

Solo, peer and orchestrated are the only formation types. Creating or changing
to any other type fails with `UNSUPPORTED_FORMATION_TYPE`, listing the three.
A board saved with a retired type, such as the former `flow`, still loads and
shows every slot. Board validation and run admission report
`invalid_formation_type` for that node until `formation set-type` converts it
or it is deleted; nothing rewrites it silently. Every slot ID on a board is
unique, because a seat's session is named after its run and slot and a relayed
verdict names its slot. Authoring generates a fresh ID for each new slot, and
restoring slots cannot take another formation's ID. A hand-written or imported
board that repeats one gets `duplicate_slot_id` on each formation holding it,
from board validation and run admission.
`board validate` lists every finding for the whole board, admission checks
included, as `ERROR`/`WARN` lines or `--json`, and exits 1 on any error.
`mission run` and `formation run` print every admission finding when a start is
rejected. The cockpit tags incomplete nodes as drafts and highlights the nodes
a rejected start names.

For the delivery template use the following limits as a bounded smoke example,
and allow enough wall time for the actual task. Lab briefs need the synthetic
verdict described above. Real briefs describe the work to deliver.

```bash
FORM_START=$(archon --server "$FORM_SERVER" mission run delivery \
  --mission mis_delivery --cwd "$FORM_CWD" --brief "$FORM_BRIEF" --bead "$FORM_BEAD" \
  --max-dispatch 30 --max-attempts 3 --wall-clock-seconds 7200 --json)
FORM_RUN_ID=$(printf '%s\n' "$FORM_START" | jq -er '.data.runId')
archon --server "$FORM_SERVER" run status "$FORM_RUN_ID" --json
archon --server "$FORM_SERVER" run logs "$FORM_RUN_ID" --json
archon --server "$FORM_SERVER" run follow "$FORM_RUN_ID" --json
archon --server "$FORM_SERVER" run list --json
```

To stop a selected non-final run, set `FORM_RUN_ID` to its ID:

```bash
archon --server "$FORM_SERVER" run abort "$FORM_RUN_ID" --reason "Operator stopped this run" --json
```

For a graph with a human gate, inspect fresh status. Set `FORM_GATE_ID` to the
pending gate and `FORM_RESPONSE` to the operator's answer. `--response` is an
alias of `--reason`. Choose exactly one verdict command, only when authorized to
decide that gate:

```bash
FORM_STATUS=$(archon --server "$FORM_SERVER" run status "$FORM_RUN_ID" --json)
FORM_REQUESTED_SEQ=$(printf '%s\n' "$FORM_STATUS" | jq -er --arg gate "$FORM_GATE_ID" \
  '.data.waitingGates[] | select(.gateId == $gate) | .requestedSeq')
archon --server "$FORM_SERVER" gate approve "$FORM_RUN_ID" "$FORM_GATE_ID" \
  --requested-seq "$FORM_REQUESTED_SEQ" --response "$FORM_RESPONSE" --json
```

For rejection, substitute this command for approval:

```bash
archon --server "$FORM_SERVER" gate reject "$FORM_RUN_ID" "$FORM_GATE_ID" \
  --requested-seq "$FORM_REQUESTED_SEQ" --response "$FORM_RESPONSE" --json
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
archon --server "$FORM_SERVER" run resume "$FORM_RUN_ID" --reason "Recovery evidence inspected" --json
```

An idle human request with no unresolved dispatch survives restart with its
original `requestedSeq`. Startup also repairs the older interruption block
immediately following that request, by appending an audited resume event;
it retains the original request and evidence. Other blocks are not repaired.
Approve or reject using the same exact request sequence after restart.

When a seat died mid-turn and its completed evidence cannot be found, abandon
the open dispatch and run the node again as a fresh bounded attempt:

```bash
archon --server "$FORM_SERVER" run resume "$FORM_RUN_ID" --mode redispatch --reason "Seat lost; run the node again" --json
```

The abandoned dispatch is recorded as a `slot_result` with status `abandoned`;
the node's next attempt counts against `maxAttempts`. A failed reattach never
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

A notification carries `runId`, `boardSlug`, `boardTitle`, `seq`, `kind`,
`runStatus`, `nodeId`, `gateId`, `gateTitle`, `ask`, `severity`, `blocks`,
`boardUrl`, a one-line `text`, and a complete plain-text `subject` and `body`.
The kinds are:

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

[OpenAPI](openapi/formations.yaml) lists the served routes. Except for the raw
theme document described below, JSON responses use
`{success,timestamp,data}`; errors carry an error object. Board authoring includes
list/create/read/patch/delete, notes, layout and change polling. Agent routes
list/create/read/patch persona cards; gate profiles expose the two code checks.
With the tmux executor the agent roster marks a persona live when a session named
by its default session stem runs on `--socket`, and lists the socket's other
sessions as unbound. The lab executor reports every agent offline.
Revision and ETag checks protect edits. A board edit that leaves the board as
it was (the same slot assignment, title, brief, type, controller, gate or
mission fields, or judge chain) saves nothing: it answers 200 with the current
board, its revision and ETag unchanged, and a stale ETag still conflicts. Tool
and note edits still save a revision. Runtime routes start/list/read runs
(`GET /api/formations/runs?board=<slug>` lists one board's runs), read projected
events/escalations, stream SSE, abort, resume, read run evidence and record
exact human verdicts. They all use the coordinator; no request-local executor
exists.
There is no generic file reader, transcript endpoint, board import endpoint or
authentication layer. Run evidence reads only one run's ledger, its artifact
directory and the briefs its own dispatches recorded.

With `--server`, Archon runs these authoring and read commands through the
daemon, so an open cockpit sees the edits through its change polling: `board
list|inspect|new|notes|note|validate|arrange`, `mission
list|inspect|create|update|wire`, `formation
list|inspect|create|rename|set-type|assign|unassign|set-brief|add-input|add-output|wire|unwire`,
`gate create|update|judge`, `tool create|update|delete|inspect` and `agent
list|inspect|new|edit`. They take the offline flags and print the offline
output: unwrapped JSON without TOML, or the same text. Each command reads the
document it changes, resolves formation, gate, mission and Tool selectors from
that read, and writes with its ETag and board revision; Tool writes also carry
the layout's state and ETag. A write that loses to another editor is read and
retried up to three times. Differences from offline use:

- Error messages come from the daemon (`coordinator HTTP <status>: ...`). JSON
  error codes, boundaries and selectors match.
- Agent cards are the daemon's `--agents-dir`, with the liveness the daemon
  reports, and `agent new --from` names a path on the daemon host. `board note
  --file` reads locally.
- Runtime commands (`mission run`, `run`, `gate approve|reject`) print the
  daemon's `{success,timestamp,data}` envelope; `board list` and `board
  inspect` print offline JSON like the other reads. `formation
  remove-verification|run`, `run ask` and `agent spawn|attach|retire` remain
  offline only.

`GET /api/formations/boards/{board}/validation` returns
`{boardRev,boardEtag,errors,warnings}` for the whole board, the same report as
`board validate`.

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

`GET /api/formations/runs/{runId}/seats` returns an enveloped
`{runId,available,reason?,seats}`. Each latest seat per node/slot includes
`runId`, `nodeId`, `nodeTitle`, `slotId`, `slotLabel`, `harness`, `controller`,
`createdSeq`, `sessionName` and `state` (`live`, `ended`, `missing`, `unavailable`).
A live seat also includes native `columns`, `rows` and a relative `terminalUrl`;
other states include a reason and no terminal URL. A seat kept on call also
includes `onCall` with `keptSeq` and `waitingOn`, the pending
`{gateId,requestedSeq}` asks it received. No private session IDs,
socket paths or socket identities are exposed in this projection.

`GET /api/formations/runs/{runId}/seats/{createdSeq}/terminal` upgrades to a
WebSocket with subprotocol `tty`, after resolving that exact run and attempt.
Unknown seats return 404, replaced attempts or non-live seats 409, and unavailable
configuration or shutdown 503. The frames are CHROTE's. The opening JSON frame
gives `columns` and `rows`, and the terminal attaches at the seat's native grid.
Afterwards the client sends binary frames: ASCII `0` followed by input bytes,
which reach the pane; `1` followed by JSON `columns` and `rows`, which sizes this
terminal's view; and `2` and `3` to pause and resume output. Other frames are
ignored. The seat window keeps its own size: the executor sizes a new seat's
window to 160x48 and pins it (tmux `window-size manual`), so no viewer resizes
it, including the only viewer of a seat kept on call. A CHROTE tile watches
the pinned window at that size.
Output frames are binary ASCII `0` followed by terminal bytes. A terminal ending
closes with 1000; daemon shutdown closes terminals with 1001. WebSocket origins must
match the request host. Terminal bytes are the actual seat display, not the
sanitized ledger projection. The same trusted-network access boundary applies.

### Run evidence

[ADR-0017](adr/0017-run-evidence-api.md) records this API. Every route is a
`GET` for one run; an unknown run returns 404. Served text is an object
`{text,bytes,truncated}`: `bytes` is the full size, `truncated` marks a cut on
a UTF-8 boundary, and the ledger's secret patterns are redacted.

- `/api/formations/runs/{runId}/evidence/nodes/{nodeId}` returns
  `data.evidence` for a node of the run's frozen board, with `kind` `mission`,
  `formation`, `gate` or `tool`. Its `definition` contains the frozen `title`,
  `outputs` (`id`, `label`) and `outgoing` connections (`id`, `from`, `to`).
  Produced-output names and ordering use this run metadata even after the
  editable board changes. Missions and formations list `attempts` with
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
- `/api/formations/runs/{runId}/evidence/problems` returns `data.problems`,
  every block and error of the run oldest first: `seq`, `type`, `code`,
  `reason`, `resumeAllowed` and `nodeIds`, the nodes it names (its node or
  gate, the blocked node or gate, and nodes with open dispatches). A block that
  names no node, such as an exceeded wall clock, has empty `nodeIds` and its
  reason. The 2 MiB budget is spent on the latest first.
- `/api/formations/runs/{runId}/evidence/briefs/{dispatchSeq}` returns
  `data.brief` (`dispatchSeq`, `nodeId`, `slotId`, `attempt`, `text` capped at
  256 KiB): the brief that this run's `slot_dispatch` at that sequence sent to
  its seat. Only a path recorded by that event, naming a direct child of
  `<state-dir>/briefs`, is read. Any other sequence returns 404.
- `/api/formations/runs/{runId}/evidence/artifacts` returns `data.artifacts`
  (`name`, `size`, `modifiedAt`), sorted by name relative to
  `<state-dir>/.formations/artifacts/<runId>`, and `data.truncated` past 500
  entries or 8 directory levels. A run without artifacts lists none.
- `/api/formations/runs/{runId}/evidence/artifacts/{name...}` returns
  `data.artifact` (`name`, `size`, `modifiedAt`, `kind` `markdown`, `json`,
  `text`, `image`, `pdf` or `binary`), with `text` capped at 256 KiB for textual
  kinds. A `.pdf` file that starts with `%PDF-` is `pdf`.
- `/api/formations/runs/{runId}/artifacts/{name...}` returns the artifact's bytes
  up to 16 MiB; larger files return 413. Text is `text/plain; charset=utf-8`
  and redacted, PNG, JPEG, GIF and WebP keep their image type, a `pdf` is an
  inline `application/pdf`, and anything else is an `application/octet-stream`
  attachment. Responses carry
  `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox` and
  `Cache-Control: no-store`, and support ranges.
- `/api/formations/runs/{runId}/gates/{gateId}/request` is the evidence API's
  view of a human request still waiting for an answer. For the gate's latest
  request, while it is pending, it returns `gateId`, `requestedSeq`, the frozen
  `criterion` and the routed input: `fromNodeId`, `fromPortId`, `text` capped at
  64 KiB, and `truncated`. An unknown run or gate returns 404; a decided request
  returns 409. After the verdict, the gate's node evidence holds the same input
  with the response.

Artifact names are relative; every component is opened from the state
directory without following symlinks, and only regular files with one link are
read, so names with `..`, symlinks and hard links cannot leave the run's
directory. Output and input references appear as `ref.artifact` inside that
directory or `ref.external` (a base name only) elsewhere; engine references
such as `ledger://` name no file and are omitted. Structured fields
never carry native session IDs, tmux session or pane IDs, `sessionRef`, socket or
prompt digests, brief or prompt paths, seat report pointers or absolute
artifact paths, and worker pane captures are not served. Text is served as
recorded apart from redaction, so it can mention host paths such as the cwd.

### Referenced files

[ADR-0018](adr/0018-referenced-file-roots.md) records these routes. The daemon
reads referenced files only under its `--file-root` directories; with none, every
reference is outside them. The `path` query is a reference as authored: an
absolute clean path is read under the deepest root containing it, and a relative
path is tried under each root in order.

- `GET /api/formations/files/preview?path=<ref>` returns `data.file` (`path`
  read, `name`, `size`, `modifiedAt`, `kind` as for artifacts), with `text`
  capped at 256 KiB for textual kinds.
- `GET /api/formations/files/raw?path=<ref>` returns the bytes up to 16 MiB with
  the raw artifact route's content types and headers; larger files return 413.

A path outside every root, or one the daemon may not read, returns 403 and is
not readable here. Every component below the root opens without following
symlinks, and only regular files with one link are read, so `..`, symlinks,
hard links and non-regular files return 404. Served text is redacted like run
evidence.

The cockpit shows each referenced file as a chip on its card: a mission's and a
gate's files and a formation's brief files. A gate's card also shows the brief
files of the formations judging it. A card shows the first few chips and lists
the rest under +N. A chip opens the file in a floating file window, and a file
the daemon will not read opens as not readable here, with its path. Arrange
reserves a chip row under a card that has referenced files.
