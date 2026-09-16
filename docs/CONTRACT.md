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
limits bound formation execution steps, including judge steps; attempts bound
revisits to a node. Seat timeout separately bounds real agent execution.
Limit exhaustion and unresolved execution leave visible blocks rather than
claiming success.

The projection reports `running`, `waiting_human`, `blocked`, `succeeded`,
`failed` or `canceled`. Always check `final` and `resumeAllowed`; a blocked run
is not a completed delivery. Events expose node, slot and gate identities,
status/verdict, session display name and cleanup outcome where applicable.
Typical sequences include `run_started`, `node_started`, `slot_dispatch`,
`seat_created`, `seat_prompt_consumed`, `slot_result`, `seat_cleanup`,
`gate_kind_result`, `gate_verdict`, and a run outcome. Malformed judges emit
`judge_attempt_failed`. Pending human requests appear in `waitingGates` with
`gateId` and `requestedSeq`.

The public projection includes cwd and Bead ID but excludes prompt text,
artifact contents, brief paths, native session IDs and arbitrary private event
data. Read detailed reasons and artifacts locally in the private evidence.
The pending-gate read route
`GET /api/formations/runs/{runId}/gates/{gateId}/request` lets the operator
read what a human gate received before answering it, because the projection
carries no content. For the gate's latest request, while it is pending, the
route returns `gateId`, `requestedSeq`, the frozen `criterion` and the routed
input: `fromNodeId`, `fromPortId`, `text` capped at 64 KiB, and `truncated`.
It never returns refs, paths, prompts or session identities. An unknown run or
gate returns 404; a decided request returns 409. The operator-approved run
evidence API (form-3rq) will absorb this route.
`run logs` is the same sanitized projection as `run status`. `run follow` prints
complete enveloped projections from SSE after durable changes, waits through
human gates and closes only at finality. Interrupting that client stops viewing,
not execution. Abort cancels only the selected run and waits for owned-seat
cleanup before returning `canceled`.

The `tmux` executor uses one implementation with Claude Code and OpenAI Codex
adapters. It resolves authenticated harness executables from its environment.
Each fresh seat gets a pointer to a file under `<state-dir>/briefs`. The file
contains run/node/slot identity, cwd, mission goal, Bead, persona summary,
formation brief, file/link references, routed inputs, gate feedback, human
responses, output ports, artifact directory and completion instructions.
Orchestrated controllers also get their bound workers and may direct only those
workers. External operators and Archon agents must not type into seats or
manage their sessions.

Sessions are named `form-<run>-<slot>`, optionally prefixed by `--mission-label`.
The runtime creates and cleans up seats by immutable session ID through the
configured guarded wrapper. The daemon's launch environment must include any
host-required cleanup override and reason. Wrapper path alone is insufficient.
Check every `seat_cleanup` outcome: `ended`, `left_socket_changed` or
`left_cleanup_failed`. Shutdown records `left_shutdown` when it detaches from
an owned seat without ending it. Success does not erase a cleanup failure.

Native completion must match the exact pointer, cwd, persona model/effort,
native session and completed turn. Codex uses native task completion; Claude
uses its completed assistant turn and sentinel. A marker alone is insufficient.
The `lab` executor creates no tmux sessions and echoes deterministic inputs.
It writes each rendered brief to `<state-dir>/briefs/lab-*.md`, as a seat would
receive it. It proves routing, not agent work or the truth of a review.

The cockpit's floating Peek observes an owned live seat. It does not send input,
enter tmux copy mode, claim pane size, or end sessions. Switching seats and
closing Peek disconnect only the observer client. Labels and controller/worker
roles come from the run's frozen graph; a new attempt has a new terminal URL.
The native grid includes tmux status rows, so an observer also preserves size
when no other client is attached. Scroll and selection happen in the browser;
the stream shows the native current screen and subsequent output, without an
API for reading old transcripts or entering historical tmux copy mode.

Each new `seat_created` records its immutable tmux session/pane IDs and a private
socket/server identity. Linux socket peer credentials, process start time and
boot identity distinguish the original server from a replacement, even when
session IDs are reused. A legacy seat without that proof is unavailable for
Peek. A renamed session is still identified by its recorded immutable ID.
Missing, ended and unavailable seats remain visible with their actual state.
Terminal support uses the daemon's existing `--socket` and `--tmux-bin`; the lab
executor exposes no live terminals. No generic session browser is provided.

## Gates and output contracts

Kinds run in order: code, formation, human, stopping on failure. Code supports
only `output_contains@1` and `output_absent@1`, configured through `check`,
`checkVersion` and `checkValue`. A draft gate may leave these blank; admission
reports the gap.

A gate's kinds are any non-empty combination of `code`, `formation` and
`human`. The cockpit editor, the `updateGate` board patch and `archon gate
update` change a gate through one store path. They set only the fields given,
and an empty value clears one. A gate keeps only the configuration its kinds
use: dropping `formation` detaches the judge chain, as detaching the judge does,
and dropping `code` clears the check. Adding `formation` without a chain leaves
a draft finding until a judge is attached. Converting a code gate to a human
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
A human kind waits for an explicit verdict naming the exact pending sequence;
stale or duplicate decisions return HTTP 409. There is no default verdict.
The verdict's `reason` is the operator's response. On pass, a nonempty response
travels on every pass route together with the gate's original input. It is typed
with gate ID, gate attempt, requested sequence, deciding actor and text, and
the next prompt renders it as a human-response section after that input. An
empty response routes the input unchanged. On fail the response becomes the
feedback reason. Resume rebuilds the response from the verdict recorded for
that exact request, so it survives restart. The cockpit shows a pending human
gate's input with a response box, Approve and Send back.

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
brief. The lab echo carries it to the judge parser. Label that evidence as
simulated; a plain brief without a verdict will block at a formation gate.

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
described at the end of this section.

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

Author through the cockpit or offline Archon's board, mission, formation,
gate, tool and agent nouns. Read command-specific help with `-h`; for runtime
flags include `--server` in the help invocation. Preserve an operator's draft
and notes, staff its slots, write executable briefs, wire exact port IDs, then
validate and arrange. The shared `archon` skill gives an authoring recipe.
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
old seats. Preserve those identities for host-owner inspection. SIGTERM fences
new commands and downstream dispatch immediately. Active turns have five
seconds to finish, then the daemon cancels observation and detaches from its
seats without ending them. HTTP draining and execution share a ten-second total
shutdown budget. Open dispatch identities remain in a resumable block for
startup recovery; an idle human request remains answerable. Abort runs explicitly
when seat cancellation is intended.

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
recipient and credentials; none of them are daemon flags.

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
Revision and ETag checks protect edits. Runtime routes start/list/read runs,
read projected events/escalations, stream SSE, abort, resume, read a pending
human gate's input and record exact human verdicts. They all use the
coordinator; no request-local executor exists.
There is no generic file reader, transcript endpoint, board import endpoint or
authentication layer. Remote Archon supports board list/inspect/validate and
runtime commands; author definitions with `--workspace` or the cockpit.
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
other states include a reason and no terminal URL. No private session IDs,
socket paths or socket identities are exposed in this projection.

`GET /api/formations/runs/{runId}/seats/{createdSeq}/terminal` upgrades to a
WebSocket with subprotocol `tty`, after resolving that exact run and attempt.
Unknown seats return 404, replaced attempts or non-live seats 409, and unavailable
configuration or shutdown 503. The opening JSON frame accepts `columns` and
`rows` for CHROTE protocol compatibility; the observer retains the native grid.
Output frames are binary ASCII `0` followed by terminal bytes. Only one-byte
ASCII `2` (pause output) and `3` (resume) are accepted afterward. Input `0`, resize
`1`, claim `4` and other client frames close with 1008. A terminal ending closes
with 1000; daemon shutdown closes observers with 1001. WebSocket origins must
match the request host. Terminal bytes are the actual seat display, not the
sanitized ledger projection. The same trusted-network access boundary applies.
