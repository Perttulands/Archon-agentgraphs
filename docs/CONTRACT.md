# Formations contract

Formations builds and runs work graphs with agents and gates. `formationsd` owns
execution and serves the Formations and Agents cockpit. ARCHON authors the same
definitions and sends runtime commands to the daemon. This is a trusted-operator
service with concurrent missions, two native harness adapters and durable run
history. Host deployment, forwarding and CHROTE integration live outside this
repository. [ADR-0016](adr/0016-daily-capability.md) records the daily-capability
decisions; [examples](../examples/) provide reusable boards.

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
| Projection | A sanitized view derived from the ledger, shared by HTTP, ARCHON and the cockpit. |

The private `<state-dir>` is also ARCHON's `--workspace`. It contains
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
appends `run_started`, then returns HTTP 202. The worker continues after the
client disconnects. A missing receipt requires checking the run list before
starting again. `cwd` must be an absolute existing directory and `brief` must be
nonempty. ARCHON reads an existing brief file or sends the argument as literal
text. The brief becomes mission output; the board goal remains prompt context.
`beadId` is optional in the API but should identify the owning task.

HTTP admission requires positive `maxDispatch`, `maxAttempts` and
`wallClockSeconds`, with `redact` false. Remote ARCHON defaults to 3 dispatches,
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
`run logs` is the same sanitized projection as `run status`. `run follow` prints
complete enveloped projections from SSE after durable changes, waits through
human gates and closes only at finality. Interrupting that client stops viewing,
not execution. Abort cancels only the selected run and waits for owned-seat
cleanup before returning `canceled`.

The `tmux` executor uses one implementation with Claude Code and OpenAI Codex
adapters. It resolves authenticated harness executables from its environment.
Each fresh seat gets a pointer to a file under `<state-dir>/briefs`. The file
contains run/node/slot identity, cwd, mission goal, Bead, persona summary,
formation brief, file/link references, routed inputs, gate feedback, output
ports, artifact directory and completion instructions. Orchestrated controllers
also get their bound workers and may direct only those workers. External
operators and ARCHON agents must not type into seats or manage their sessions.

Sessions are named `form-<run>-<slot>`, optionally prefixed by `--mission-label`.
The runtime creates and cleans up seats by immutable session ID through the
configured guarded wrapper. The daemon's launch environment must include any
host-required cleanup override and reason. Wrapper path alone is insufficient.
Check every `seat_cleanup` outcome: `ended`, `left_socket_changed` or
`left_cleanup_failed`. Success does not erase a cleanup failure.

Native completion must match the exact pointer, cwd, persona model/effort,
native session and completed turn. Codex uses native task completion; Claude
uses its completed assistant turn and sentinel. A marker alone is insufficient.
The `lab` executor creates no tmux sessions and echoes deterministic inputs.
It proves routing, not agent work or the truth of a review.

## Gates and output contracts

Kinds run in order: code, formation, human, stopping on failure. Code supports
only `output_contains@1` and `output_absent@1`, configured through `check`,
`checkVersion` and `checkValue`. It does not run arbitrary shell commands.
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
go build -o "$FORM_BIN/formationsd" ./cmd/formationsd
cd "$FORM_SOURCE/dashboard"
npm ci
npm run build
"$FORM_BIN/formationsd" --executor lab --state-dir "$FORM_STATE" \
  --listen "$FORM_LISTEN" --ui-dir "$FORM_SOURCE/dashboard/dist"
```

For real seats select `--executor tmux` and supply `--socket`, `--tmux-bin`,
`--codex-transcripts`, `--claude-transcripts` from host configuration.
`--cwd` is an optional daemon default; missions supply their own cwd.
`--mission-label` is optional and `--seat-timeout` defaults to `30m`.
Repeat `--listen` for each trusted interface. `--agents-dir` overrides cards;
omitting `--ui-dir` disables the cockpit. These are daemon flags, not model
settings. Set model and effort on persona harness variants.

In another terminal use the compiled ARCHON. Import means copying board and
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

Author through the cockpit or offline ARCHON's board, mission, formation,
gate, tool and agent nouns. Read command-specific help with `-h`; for runtime
flags include `--server` in the help invocation. Preserve an operator's draft
and notes, staff its slots, write executable briefs, wire exact port IDs, then
validate and arrange. The shared `archon` skill gives an authoring recipe.

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
pending gate and `FORM_REASON` to the operator's decision. Choose exactly one
verdict command, only when authorized to decide that gate:

```bash
FORM_STATUS=$(archon --server "$FORM_SERVER" run status "$FORM_RUN_ID" --json)
FORM_REQUESTED_SEQ=$(printf '%s\n' "$FORM_STATUS" | jq -er --arg gate "$FORM_GATE_ID" \
  '.data.waitingGates[] | select(.gateId == $gate) | .requestedSeq')
archon --server "$FORM_SERVER" gate approve "$FORM_RUN_ID" "$FORM_GATE_ID" \
  --requested-seq "$FORM_REQUESTED_SEQ" --reason "$FORM_REASON" --json
```

For rejection, substitute this command for approval:

```bash
archon --server "$FORM_SERVER" gate reject "$FORM_RUN_ID" "$FORM_GATE_ID" \
  --requested-seq "$FORM_REQUESTED_SEQ" --reason "$FORM_REASON" --json
```

Restart the daemon with the same state directory and configuration. Before
listening it scans non-final ledgers, recovers eligible completed native evidence
or records a block naming unresolved dispatches. It never adopts or cleans up
old seats. Preserve those identities for host-owner inspection. Graceful shutdown
waits for execution; abort runs explicitly when cancellation is intended.

Inspect list/status after restart. For a resumable block whose cause is resolved:

```bash
archon --server "$FORM_SERVER" run resume "$FORM_RUN_ID" --reason "Recovery evidence inspected" --json
```

Current limitation, form-do9: restarting while a human gate is pending appends
a block even with no open dispatch. Its projection still says `waiting_human`,
but an exact verdict returns 422 and resume returns 409. Preserve the ledger
and report this condition; the current commands cannot continue that run.
Resolve human decisions before a planned restart. The delivery graph has no
human gate and does not enter this case.

Resume does not manufacture missing completion or resend an uncertain task.
An unresolved dispatch can block again. For explicitly selected completed native
evidence, restart with `--resume-run`, `--completed-transcript` and
`--completed-brief` together, naming the blocked resumable run, absolute native
transcript and original brief. Validation checks the original digest, pointer,
cwd, session, model/effort and completed turn before continuation. Keep original
artifacts intact. Do not run offline runtime mutations alongside the daemon.

## HTTP contract

[OpenAPI](openapi/formations.yaml) lists the served routes. JSON responses use
`{success,timestamp,data}`; errors carry an error object. Board authoring includes
list/create/read/patch/delete, notes, layout and change polling. Agent routes
list/create/read/patch persona cards; gate profiles expose the two code checks.
Revision and ETag checks protect edits. Runtime routes start/list/read runs,
read projected events/escalations, stream SSE, abort, resume and record exact
human verdicts. They all use the coordinator; no request-local executor exists.
There is no generic file reader, transcript endpoint, board import endpoint or
authentication layer. Remote ARCHON supports board list/inspect and runtime
commands; author definitions with `--workspace` or the cockpit.
