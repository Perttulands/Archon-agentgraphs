---
name: archon
description: Author Archon missions and drive their runs through the archond daemon. Use when building, editing, staffing or wiring a mission, starting or following a run, answering a human gate, or recovering a blocked run.
---

# Archon

This skill documents the Archon contract at commit 7adfe4c (VERSION 0.1.0,
2026-09-29). It ships with that source. `archon --version` names the build on
PATH. When that build is older, a flag named here may be missing: read the
command's `-h` and trust the binary.

## Vocabulary

Today's CLI and files still say "board" for the reusable graph. Read "board" as
"mission graph".

- A **board** is one TOML graph with a slug and a revision. Its **mission** node
  is the entry: a goal and an `out` port. Each run supplies the mission's input
  text, the brief.
- A **formation** is a step. `solo` has one seat, `peer` has two or more seats
  that converse, and `orchestrated` has one controller directing its bound
  workers. These three are the only types.
- A **slot** is a position in a formation, bound to a persona and a harness
  (`claude-code` or `openai-codex`). Its runtime agent session is a **seat**.
- A **persona** is an agent card. Model and effort live on its harness variant.
- A **gate** has a criterion and kinds `code`, `formation` and `human`, run in
  that order and stopping at the first failure. Its ports are `in`, `pass`,
  `fail` and `judge`.
- A **judge chain** is the formations wired from a gate's `judge` port back to it.
  A **pushback edge** is a gate's `fail` wired back to work; it carries the
  verdict as feedback and starts a bounded next attempt.
- A **run** snapshots the board and personas at admission. Later edits affect
  later runs only.

## Connect

Take `<archon-server>` and `<state-dir>` from the operator's runbook or
environment and set `FORM_SERVER` and `FORM_STATE`. The server is an HTTP URL
with a literal IP, with no trailing slash, credentials, query or fragment.
Keep runtime state outside any checkout.

- Runtime commands (`mission run`, `run ...`, `gate approve|reject`) always take
  `--server "$FORM_SERVER"`.
- Authoring takes `--server` too, so an open cockpit shows each edit live.
  `--workspace "$FORM_STATE"` authors the same files offline, for a state
  directory no daemon serves; runtime commands never run offline beside a
  daemon.
- A `--server` failure is final; nothing falls back to a local runtime.
- Read leaf help with `-h`, adding `--server` for the daemon's form.

## Author a mission

Read before you write: the board, its notes and the persona roster.

```bash
archon --server "$FORM_SERVER" board list --json
archon --server "$FORM_SERVER" board inspect "$BOARD" --json
archon --server "$FORM_SERVER" board notes "$BOARD" --json
archon --server "$FORM_SERVER" agent list --json
```

Notes are the operator's intent, not seat instructions. Translate them into
staffing, briefs and wiring. Each note is a thread: answer by appending with
`board note "$BOARD" [--node <id>] --text "..."`, and change only your own
entries (`--entry <id> --text` or `--entry <id> --clear`). After you build or
change a step, explain it on its node thread in words the operator reads on the
canvas: what happens there and who does it. For example: "Here three reviewers
read the change in parallel and a lead merges their findings."

Take every ID from the JSON a command returns; never guess one. `mission
create`, `formation create` and `gate create` return `{board, layout,
mission|formation|gate}`. A new formation already has one input port, one
output port and its slots.

```bash
S="--server $FORM_SERVER"
archon $S board new "$BOARD" --title "Reviewed change" --json
MISSION=$(archon $S mission create "$BOARD" --title "Change" --goal "Deliver a reviewed change" --json | jq -r .mission.id)
WORK_JSON=$(archon $S formation create "$BOARD" solo --title "Build" --json)
WORK=$(jq -r .formation.id <<<"$WORK_JSON")
WORK_IN=$(jq -r '.formation.inputs[0].id' <<<"$WORK_JSON")
WORK_OUT=$(jq -r '.formation.outputs[0].id' <<<"$WORK_JSON")
WORK_SLOT=$(jq -r '.formation.slots[0].id' <<<"$WORK_JSON")
archon $S formation assign "$BOARD" "$WORK" --slot "$WORK_SLOT" --agent codex-builder --harness openai-codex --json
archon $S formation set-brief "$BOARD" "$WORK" --goal "Make the change the brief asks for." --json
archon $S mission wire "$BOARD" "$MISSION" "$WORK:$WORK_IN" --json
```

Edit nodes in place so their edges survive: `formation rename`, `formation
set-type <solo|peer|orchestrated>` (to solo with several staffed slots, add
`--keep-slot <slot>`), `mission update --title|--goal|--bead|--input-hint`,
`gate update`. An empty value clears a field; `gate update` changes only the
flags given. `--input-hint` tells the operator what a run brief should contain.

### Briefs belong to steps

Write what a step does in its formation brief (`formation set-brief --goal`,
`--file` for reference files). A persona is a reusable role; mission-specific
instructions stay in the brief. Missions and gates take reference files too:
repeat `--file <path>` on `mission create|update` and `gate create|update`; on
update the list is replaced and `--file ''` clears it. The daemon serves a file
only under its `--file-root` directories.

### Model and effort

Model and effort live on the persona's harness variant, never on the board.
Set them with `agent new <id> --harness <h> --model <m> --effort <e>` or `agent
edit <id> --harness <h> --effort <e>`. Blank effort means `medium`; blank model
means the harness default. `claude-code` takes `low`, `medium`, `high`, `xhigh`
or `max`; `openai-codex` also takes `ultra`.

A persona is shared: `agent edit` changes every slot staffed with it, on every
board, from the next run on. When one step needs different settings, create a
dedicated persona (`agent new`) and assign it to that slot instead.

Choose effort by the step's job:

| Job | Effort |
| --- | --- |
| Architecture, design and review | `xhigh` |
| Consequential review (release gate, irreversible change) | `max` |
| Making things | `medium` |
| Errands | `low` |

### Fan out and join

An output port feeds any number of inputs. Each formation input takes one feed,
and a formation starts only when every input port has received its input. To
join parallel work, wire every upstream into the joining formation's input with
`--join`: a free input takes the wire, and an occupied one gains a new input
port and its connection in one edit. Without `--join` an occupied input is
refused (`input_occupied`). Three reviewers in parallel, merged by a lead (IDs from each
formation's create JSON):

```bash
archon $S formation wire "$BOARD" "$WORK:$WORK_OUT" "$REV1:$REV1_IN" --json
archon $S formation wire "$BOARD" "$WORK:$WORK_OUT" "$REV2:$REV2_IN" --json
archon $S formation wire "$BOARD" "$WORK:$WORK_OUT" "$REV3:$REV3_IN" --json
archon $S formation wire "$BOARD" "$REV1:$REV1_OUT" "$LEAD:$LEAD_IN" --join --json
archon $S formation wire "$BOARD" "$REV2:$REV2_OUT" "$LEAD:$LEAD_IN" --join --json
archon $S formation wire "$BOARD" "$REV3:$REV3_OUT" "$LEAD:$LEAD_IN" --join --json
```

Gate and Tool inputs stay single-feed.

### Gates

`gate create` without `--kinds` makes a human gate. Pass `--kinds` for any mix of
`code`, `formation` and `human`.

- **Code** runs only `output_contains@1` or `output_absent@1`: `--kinds code
  --check output_contains --check-version 1 --check-value <text>`. It runs no
  shell commands; put real checks (tests, lint, review) in a judge formation.
- **Formation** needs a judge: create a solo judge formation, then `gate judge
  "$BOARD" "$GATE" --chain "$JUDGE"` wires both ends. `--detach` removes it; a
  judge-only gate then becomes a human gate.
- **Human** waits for an explicit operator verdict. There is no default verdict.

Wire work into the gate and route both verdicts:

```bash
GATE=$(archon $S gate create "$BOARD" --kinds formation --title "Review" --criterion "The change satisfies the brief" --json | jq -r .gate.id)
archon $S gate judge "$BOARD" "$GATE" --chain "$JUDGE" --json
archon $S formation wire "$BOARD" "$WORK:$WORK_OUT" "$GATE:in" --json
archon $S formation wire "$BOARD" "$GATE:fail" "$WORK:$WORK_IN" --json
archon $S formation wire "$BOARD" "$GATE:pass" "$NEXT:$NEXT_IN" --json
```

A judge's brief must require exactly one fenced `chrote-verdict` block with
exactly `verdict` (`pass` or `fail`), `reason` (string) and `evidence` (array of
strings), besides its normal `chrote-outputs` block. A missing, duplicate or
malformed verdict blocks the run without resume.

Dropping a kind drops its configuration: `gate update "$BOARD" "$GATE" --kinds
human` turns a code gate into a human gate; `--clear-check` clears the check and
keeps the code kind.

### Ending paths

A path ends where an output or a gate's `pass` has no outgoing wire. A run
succeeds only when nothing else can still run: every formation has produced
output since its last input, every gate has evaluated its last input, and no
human request is open. So a pass with no route finishes only once every other
reachable branch has run, and a fail with no route leaves a visible block. Wire
every `fail` somewhere.

### Step duration

Archon adds no guardrails a mission did not ask for, but every step already has
a time budget: a step without its own duration inherits the daemon's
`--seat-timeout` (30 minutes unless the host configured another value). That
budget covers startup, the work and finalization, and expiry blocks the run with
`formation_timeout_exceeded`, keeping the partial evidence. So set a duration
when a step's work differs from that default:

- A long step (a large build, a deep review) needs more time, or the inherited
  default cuts it short.
- A peer conversation has no fixed round count; its seats talk until every seat
  acknowledges one proposal. Give it a duration when it must stop converging by
  a known time.

`formation set-execution "$BOARD" "$FORMATION" --timeout-seconds <n>` sets it;
`0` returns to the daemon default. A run freezes the duration at admission. A
pushback loop is bounded separately, by the run's `--max-attempts`.

### Human channel

`mission create|update --human-channel session` sends a human gate's question
into the seats of the formation whose work it judges, instead of the notify
command. Set it only when the operator asks. Read
[references/human-gates.md](references/human-gates.md) when you drive or relay
a session-channel gate.

### Validate

Drafts save with blanks. Only `board validate` and run admission reject gaps.

```bash
archon $S board validate "$BOARD" --json
archon $S board arrange "$BOARD" --json
```

Reach zero errors before running. Findings name node IDs: unstaffed slots,
incomplete gates, an unwired mission, `invalid_formation_type`,
`duplicate_slot_id`. A rejected `mission run` prints the same findings (HTTP 422
`RUN_ADMISSION_FAILED`) and records no run.

To import an example board or smoke-test routing on a lab daemon, read
[references/lab-and-examples.md](references/lab-and-examples.md).

## Run a mission

```bash
FORM_START=$(archon $S mission run "$BOARD" --mission "$MISSION" \
  --brief "$FORM_BRIEF" --bead "$FORM_BEAD" \
  --context-path /abs/prior-art --context-path /abs/notes.md \
  --max-dispatch 30 --max-attempts 3 --wall-clock-seconds 7200 --json)
FORM_RUN_ID=$(jq -er .data.runId <<<"$FORM_START")
archon $S run status "$FORM_RUN_ID" --json
```

- `--brief` is a file path read locally, or literal text. It becomes the
  mission's output; the board stays reusable.
- Omit `--cwd` and the daemon allocates a private workspace for the run. Pass
  `--cwd /abs/existing/dir` to work in an existing project.
- `--context-path` names absolute existing files or directories the seats must
  inspect as prior art. They are read-only references, frozen as paths; repeat
  the flag for more.
- Set limits for the graph. Remote defaults are 3 dispatches, 3 attempts and
  7200 seconds. Every formation start, judges included, spends one dispatch;
  resume never refills them. Attempts bound revisits to one node. The wall clock
  excludes time waiting at human gates.
- A lost receipt: check `run list --json` before starting again.

### Watch

The driving agent pulls; Archon never pushes into your session.

- `run status "$FORM_RUN_ID" --json` returns one projection at once. Poll it to
  watch a run from your own turn.
- `run follow "$FORM_RUN_ID" --json` prints a projection after each durable
  change and does not return until the run is final. It keeps waiting while a
  human gate waits, so run it in the background (and read its output) or it
  holds your turn for the whole run. Interrupting it stops watching, not the run.
- `run logs` is the same sanitized view as status.

Read `status` (`running`, `waiting_human`, `blocked`, `succeeded`, `failed`,
`canceled`), `final`, `resumeAllowed` and `waitingGates`. `waiting_human` means
the run needs the operator: each `waitingGates` entry has `gateId` and
`requestedSeq`. `blocked` is never a delivery. Runtime JSON is wrapped in
`.data`; authoring JSON is not.

Outputs: declared artifacts live under
`$FORM_STATE/.formations/artifacts/<runId>/`, and the cockpit's Produced list
opens them. Check each `seat_cleanup` outcome (`ended`, `left_socket_changed`,
`left_cleanup_failed`).

The runtime writes the brief each seat receives, including its output contract;
you author only the formation brief. When a seat's output did not route, or you
are working inside a seat, read [references/seat-output.md](references/seat-output.md).

## Decide a human gate

Decide only with the operator's authority. Read fresh status, then take
`FORM_GATE_ID` and `FORM_REQUESTED_SEQ` from the same `.data.waitingGates`
entry. `run gates "$FORM_RUN_ID"` lists them; `gate request "$FORM_RUN_ID"
"$FORM_GATE_ID"` shows the question, the input and where each verdict leads.

```bash
archon $S gate approve "$FORM_RUN_ID" "$FORM_GATE_ID" --requested-seq "$FORM_REQUESTED_SEQ" --response "$FORM_RESPONSE" --json
archon $S gate reject  "$FORM_RUN_ID" "$FORM_GATE_ID" --requested-seq "$FORM_REQUESTED_SEQ" --response "$FORM_RESPONSE" --json
```

Run exactly one. On approve, a nonempty response travels with the gate's input
to the next step; on reject it becomes the feedback reason. `--response-file
<utf8-file>` records a long answer verbatim. A brief `resume_after_verdict`
block right after a verdict is the normal handoff; the coordinator resumes.

## Stop and recover

Abort a non-final run with `run abort "$FORM_RUN_ID" --reason "<why>" --json`;
it cancels that run only and ends its seats. For a blocked run, a 409, a daemon
restart or a lost seat, read [references/recovery.md](references/recovery.md).

## Seats belong to the runtime

The runtime creates, briefs and cleans up every seat. Leave seat tmux sessions
alone: only an orchestrated controller directs its own bound workers. The
operator may type into any seat at any time; a dispatch waits through their
turns and still completes on the run's sentinel. Inside a seat, treat what the
operator types as their instruction.
