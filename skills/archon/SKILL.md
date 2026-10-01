---
name: archon
description: Author Archon missions and drive their runs through the archond daemon. Use when building, editing, staffing or wiring a mission, starting or following a run, answering a human gate, or recovering a blocked run.
---

# Archon

This skill documents the Archon contract of VERSION 0.1.0 as of 2026-10-01:
the reusable unit is a mission, each slot owns its harness, model and effort,
run limits are optional, drivers pull with `run wait`, and every surface uses
current names only. It ships with that source.
`archon --version` names the build on PATH. When that build is older, a flag or
behaviour named here may differ: read the command's `-h` and trust the binary.

Archon makes chaining agents and gates easy and great, and that is all it
does. Seats are ordinary tmux agent sessions with full access, as in CHROTE;
Archon adds no sandbox or confinement, so safety comes from the agents' own
harness settings. Archon keeps only what is current.

## Vocabulary

- A **mission** is one reusable `<slug>.mission.toml` graph with a slug and a
  revision. Its **Input card** is the entry: a goal and an `out` port
  (`inputCard` in JSON and TOML). Each run supplies the input text, the brief.
- A **formation** is a step. `solo` has one seat, `peer` has two or more seats
  that converse, and `orchestrated` has one controller directing its bound
  workers. These three are the only types.
- A **slot** is a position in a formation. It owns its harness (`claude-code`
  or `openai-codex`), model and effort, and may name a **role**. Its runtime
  agent session is a **seat**.
- A **role** (persona card) is optional, generic role text such as a code
  reviewer; it carries no model or effort. A slot without one is a **vanilla**
  agent: `claude-code · opus · low`.
- A **gate** has a criterion and kinds `code`, `formation` and `human`, run in
  that order and stopping at the first failure. Its ports are `in`, `pass`,
  `fail` and `judge`.
- An **End node** ends a path on purpose, with outcome `done` or `rejected`.
  Its only port is `in`; any number of routes may lead into it.
- A **judge chain** is the formations wired from a gate's `judge` port back to it.
  A **pushback edge** is a gate's `fail` wired back to work; it carries the
  verdict as feedback and starts a bounded next attempt.
- A **run** snapshots the mission, its slots and any roles at admission. Later edits affect
  later runs only.

## Connect

Take `<archon-server>` and `<state-dir>` from the operator's runbook or
environment and set `ARCHON_SERVER` and `ARCHON_STATE`. The server is an HTTP URL
with a literal IP, with no trailing slash, credentials, query or fragment.
Keep runtime state outside any checkout.

- Runtime commands (`mission run`, `run ...`, `gate approve|reject`) always take
  `--server "$ARCHON_SERVER"`.
- Authoring takes `--server` too, so an open cockpit shows each edit live.
  `--workspace "$ARCHON_STATE"` authors the same files offline, for a state
  directory no daemon serves; runtime commands never run offline beside a
  daemon.
- A `--server` failure is final; nothing falls back to a local runtime.
- Read leaf help with `-h`, adding `--server` for the daemon's form.

## Author a mission

Read before you write: the mission, its notes and the role roster.

```bash
archon --server "$ARCHON_SERVER" mission list --json
archon --server "$ARCHON_SERVER" mission inspect "$M" --json
archon --server "$ARCHON_SERVER" mission notes "$M" --json
archon --server "$ARCHON_SERVER" agent list --json
```

Notes are the operator's intent, not seat instructions. Translate them into
staffing, briefs and wiring. Each note is a thread: answer by appending with
`mission note "$M" [--node <id>] --text "..."`, and change only your own
entries (`--entry <id> --text` or `--entry <id> --clear`). After you build or
change a step, explain it on its node thread in words the operator reads on the
canvas: what happens there and who does it. For example: "Here three reviewers
read the change in parallel and a lead merges their findings."

Take every ID from the JSON a command returns; never guess one. `mission
create` (which adds the Input card), `formation create` and `gate create`
return `{mission, layout, inputCard|formation|gate}`; `mission` there is the
mission's document. A new formation already has one input port, one
output port and its slots.

```bash
S="--server $ARCHON_SERVER"
archon $S mission new "$M" --title "Reviewed change" --json
INPUT=$(archon $S mission create "$M" --title "Change" --goal "Deliver a reviewed change" --json | jq -r .inputCard.id)
WORK_JSON=$(archon $S formation create "$M" solo --title "Build" --json)
WORK=$(jq -r .formation.id <<<"$WORK_JSON")
WORK_IN=$(jq -r '.formation.inputs[0].id' <<<"$WORK_JSON")
WORK_OUT=$(jq -r '.formation.outputs[0].id' <<<"$WORK_JSON")
WORK_SLOT=$(jq -r '.formation.slots[0].id' <<<"$WORK_JSON")
archon $S formation assign "$M" "$WORK" --slot "$WORK_SLOT" --harness openai-codex --effort medium --role codex-builder --json
archon $S formation set-brief "$M" "$WORK" --goal "Make the change the brief asks for." --json
archon $S mission wire "$M" "$WORK:$WORK_IN" --json
```

Edit nodes in place so their edges survive: `formation rename`, `formation
set-type <solo|peer|orchestrated>` (to solo with several staffed slots, add
`--keep-slot <slot>`), `mission update "$M" --title|--goal|--input-hint|--file` (the Input card),
`gate update`. An empty value clears a field; `gate update` changes only the
flags given. `--input-hint` tells the operator what a run brief should contain.

### Briefs belong to steps

Write what a step does in its formation brief (`formation set-brief --goal`,
`--file` for reference files). A role is reusable and generic; mission-specific
instructions stay in the step's brief, never in a role. Missions and gates take reference files too:
repeat `--file <path>` on `mission create|update` and `gate create|update`; on
update the list is replaced and `--file ''` clears it. Use absolute paths: the
cockpit opens any file by absolute path, and a relative one has no base there.

### Harness, model and effort

Each slot sets its own harness, model and effort, in the mission, where the
operator can see them: `formation assign "$M" <formation> --slot <slot>
--harness <h> --effort <e> [--model <m>] [--role <role>]`. `--effort` is
required; a blank `--model` means the harness default. `claude-code` takes
`low`, `medium`, `high`, `xhigh` or `max`; `openai-codex` also takes `ultra`.
Omit `--role` for a vanilla agent. A role adds only its text, so one step's
settings never change another's. `formation inspect` prints each slot's
staffing, for example `vanilla · claude-code · opus · low`.

Choose effort by the step's job, with purpose:

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
archon $S formation wire "$M" "$WORK:$WORK_OUT" "$REV1:$REV1_IN" --json
archon $S formation wire "$M" "$WORK:$WORK_OUT" "$REV2:$REV2_IN" --json
archon $S formation wire "$M" "$WORK:$WORK_OUT" "$REV3:$REV3_IN" --json
archon $S formation wire "$M" "$REV1:$REV1_OUT" "$LEAD:$LEAD_IN" --join --json
archon $S formation wire "$M" "$REV2:$REV2_OUT" "$LEAD:$LEAD_IN" --join --json
archon $S formation wire "$M" "$REV3:$REV3_OUT" "$LEAD:$LEAD_IN" --join --json
```

Gate and Tool inputs stay single-feed.

### Gates

`gate create` without `--kinds` makes a human gate. Pass `--kinds` for any mix of
`code`, `formation` and `human`.

- **Code** runs only `output_contains@1` or `output_absent@1`: `--kinds code
  --check output_contains --check-version 1 --check-value <text>`. It runs no
  shell commands; put real checks (tests, lint, review) in a judge formation.
- **Formation** needs a judge: create a solo judge formation, then `gate judge
  "$M" "$GATE" --chain "$JUDGE"` wires both ends. `--detach` removes it; a
  judge-only gate then becomes a human gate.
- **Human** waits for an explicit operator verdict. There is no default verdict.

Wire work into the gate and route both verdicts:

```bash
GATE=$(archon $S gate create "$M" --kinds formation --title "Review" --criterion "The change satisfies the brief" --json | jq -r .gate.id)
archon $S gate judge "$M" "$GATE" --chain "$JUDGE" --json
archon $S formation wire "$M" "$WORK:$WORK_OUT" "$GATE:in" --json
archon $S formation wire "$M" "$GATE:fail" "$WORK:$WORK_IN" --json
archon $S formation wire "$M" "$GATE:pass" "$NEXT:$NEXT_IN" --json
```

Every `pass` and `fail` must lead somewhere: to work, another gate or an End
node (next section).

A judge's brief must require exactly one fenced `archon-verdict` block with
exactly `verdict` (`pass` or `fail`), `reason` (string) and `evidence` (array of
strings), besides its normal `archon-outputs` block. A missing, duplicate or
malformed verdict blocks the run without resume.

Dropping a kind drops its configuration: `gate update "$M" "$GATE" --kinds
human` turns a code gate into a human gate; `--clear-check` clears the check and
keeps the code kind.

### Ending paths

Every route leads somewhere: each formation output and each gate `pass` and
`fail` goes to a step, a gate or an **End node**. End a path on purpose with an
End node, outcome `done` (the default) or `rejected`:

```bash
DONE=$(archon $S end create "$M" --json | jq -r .end.id)
REJECTED=$(archon $S end create "$M" --outcome rejected --json | jq -r .end.id)
archon $S formation wire "$M" "$NEXT:$NEXT_OUT" "$DONE:in" --json
archon $S formation wire "$M" "$SIGNOFF:pass" "$DONE:in" --json
archon $S formation wire "$M" "$SIGNOFF:fail" "$REJECTED:in" --json
```

Several routes may share one End node. `end update "$M" "$END" --title <t>
--outcome <o>` and `end delete "$M" "$END"` change or remove one.

A run finishes when every path has ended and nothing else can still run: every
formation has produced output since its last input, every gate has evaluated
its last input, and no human request is open. It succeeds unless a path ended
at a `rejected` End node; then it fails (`run_failed`, code `path_rejected`)
with the reason of the gate verdict that routed there. Other branches still
run to their own ends first; a join that rejection starved of an input does not
hold the run open. A route that leads nowhere is a validation error,
"Brief sign-off's pass route leads nowhere: wire it to a step or an End node",
and admission refuses the run.

### Step duration

A step has no time limit unless the mission gives it a duration; Archon adds no
default. A duration covers startup, the work and finalization, and expiry
blocks the run with `formation_timeout_exceeded`, keeping the partial evidence.
Give a step a duration only when it must stop by a known time, for example a
peer conversation, which has no fixed round count and talks until every seat
acknowledges one proposal.

`formation set-execution "$M" "$FORMATION" --timeout-seconds <n>` sets it;
`0` removes it. A run freezes the duration at admission.
When a step runs long, check `run seats` (a seat's `waiting` says what it waits
on) or open the seat; `run wait --until any-change` reports each `seat_state`.
A step's duration does not bound a send-back loop; see run limits below.

### Human channel

`mission create|update --human-channel session` sends a human gate's question
into the seats of the formation whose work it judges, instead of the notify
command. Set it only when the operator asks. Read
[references/human-gates.md](references/human-gates.md) when you drive or relay
a session-channel gate.

### Validate

Drafts save with blanks. Only `mission validate` and run admission reject gaps.

```bash
archon $S mission validate "$M" --json
archon $S mission arrange "$M" --json
```

Reach zero errors before running. Findings name node IDs: unstaffed slots,
incomplete gates, routes that lead nowhere (`route_leads_nowhere`), an unwired
Input card, a mission with several Input cards, `duplicate_slot_id`. Warnings
name nodes no path from the Input card reaches (`unreachable_node`); wire a
route into them or delete them. A rejected `mission run` prints the same findings (HTTP 422
`RUN_ADMISSION_FAILED`) and records no run.

To import an example mission or smoke-test routing on a lab daemon, read
[references/lab-and-examples.md](references/lab-and-examples.md).

## Run a mission

```bash
ARCHON_START=$(archon $S mission run "$M" \
  --brief "$ARCHON_BRIEF" --bead "$ARCHON_BEAD" \
  --context-path /abs/prior-art --context-path /abs/notes.md --json)
ARCHON_RUN_ID=$(jq -er .data.runId <<<"$ARCHON_START")
archon $S run status "$ARCHON_RUN_ID" --json
```

- `--brief` is a file path read locally, or literal text. It becomes the
  Input card's output; the mission stays reusable. `--bead` names the run's
  owning Bead; an Input card has none.
- Omit `--cwd` and the daemon allocates a private workspace for the run. Pass
  `--cwd /abs/existing/dir` to work in an existing project.
- `--context-path` names absolute existing files or directories the seats must
  inspect as prior art. They are read-only references, frozen as paths; repeat
  the flag for more.
- A run has no limits unless the launch sets them, and nothing supplies them
  for you. Without limits a send-back loop continues until its gate passes or
  you stop the run with `run abort`.
- Add a cap only when the mission needs one: `--max-attempts <n>` bounds
  revisits to any one node, `--max-dispatch <n>` bounds formation starts across
  the run (judges included; resume never refills them), and
  `--wall-clock-seconds <n>` bounds agent work from the run's start, excluding
  time waiting at human gates. Each must be positive; omitting it means none. A
  spent cap blocks the run naming the limit (for example the attempts used of
  the maximum), and that block is not resumable.
- A lost receipt: check `run list --json` before starting again.

### Watch

The driving agent pulls; Archon never pushes into your session. Start
`run wait` in the background right after the launch, and again after every
answer:

```bash
archon $S run wait "$ARCHON_RUN_ID" --until needs-you
```

It blocks until the run needs you, ends or changes, prints one paragraph
written for you, and exits. Act on the exit code:

| Exit | Meaning | What to do |
| --- | --- | --- |
| 3 | The run needs you | Do what the paragraph says. For a gate it names the gate, criterion, input and where each verdict leads, and prints the exact `gate approve`/`gate reject --requested-seq <n>` commands; for a block, the `run resume` or `run abort` command. Answer with the operator's authority, then wait again. |
| 0 | The run ended | Read how: its status, the step it stopped at, the reason and who ended it. No further wait. |
| 4 | Something changed (`any-change`) | Read the listed events, then wait again. |
| 5 | `--timeout` passed first | Wait again with the same command. |
| 6 | The daemon stayed unreachable (`--reconnect`, default one minute) | Wait again with the printed command once the daemon is back. |
| 1, 2 | Error or usage | Read stderr; an unknown run or a bad `--since` does not improve by waiting. |

- Every answer except a final one ends with `Wait for what comes next:` and
  the exact next command, carrying `--since <seq>`. Always run that command,
  never `--since 0` again in a loop: an ask counts as new only after `--since`,
  so you see each ask once and miss nothing between waits or across a daemon
  restart. The first wait, without `--since`, reports every open ask.
- `--until needs-you` (the default) is for driving: it returns for gates,
  blocking escalations, blocks and the end. `--until final` returns only at
  the end. `--until any-change` returns at every ledger event for step-by-step
  oversight, and still answers 3 when an event opens an ask.
- Answer the moment a wait returns: a verdict or resume sent while the run's
  command is still settling waits for it on the daemon.
- `--json` prints the same answer for machines: `outcome`, `seq`, `status`,
  `asks`, `end`, `changes` and `next` (the next command, with `--json`).
- `run status "$ARCHON_RUN_ID" --json` returns one projection at once, and
  `run follow "$ARCHON_RUN_ID" --json` streams a projection after each durable
  change until the run is final. Prefer `run wait` for driving; interrupting
  any of them stops watching, not the run. `run logs` is the same sanitized
  view as status.

Read `status` (`running`, `waiting_human`, `blocked`, `succeeded`, `failed`,
`canceled`), `final`, `resumeAllowed` and `waitingGates`. `waiting_human` means
the run needs the operator: each `waitingGates` entry has `gateId` and
`requestedSeq`. `blocked` is never a delivery. Runtime JSON is wrapped in
`.data`; authoring JSON is not.

Outputs: declared artifacts live under
`$ARCHON_STATE/.archon/artifacts/<runId>/`, and the cockpit's Produced list
opens them. Check each `seat_cleanup` outcome (`ended`, `left_socket_changed`,
`left_cleanup_failed`).

The runtime writes the brief each seat receives, including its output contract;
you author only the formation brief. When a seat's output did not route, or you
are working inside a seat, read [references/seat-output.md](references/seat-output.md).

## Decide a human gate

Decide only with the operator's authority. A `run wait` that exited 3 already
printed both commands with the current `--requested-seq`. Otherwise read fresh
status, then take
`ARCHON_GATE_ID` and `ARCHON_REQUESTED_SEQ` from the same `.data.waitingGates`
entry. `run gates "$ARCHON_RUN_ID"` lists them; `gate request "$ARCHON_RUN_ID"
"$ARCHON_GATE_ID"` shows the question, the input and where each verdict leads:
the targets (an End node target means "this path ends (done)" or "(rejected)",
`endsRun` says the run then ends, and `runFails` that it fails, now or once its other work ends), the
attempt each would start and, only when the run set a cap, that
cap (`maxAttempts`, `dispatches`) and a `limit` entry if taking the route would
exceed it.

```bash
archon $S gate approve "$ARCHON_RUN_ID" "$ARCHON_GATE_ID" --requested-seq "$ARCHON_REQUESTED_SEQ" --response "$ARCHON_RESPONSE" --json
archon $S gate reject  "$ARCHON_RUN_ID" "$ARCHON_GATE_ID" --requested-seq "$ARCHON_REQUESTED_SEQ" --response "$ARCHON_RESPONSE" --json
```

Run exactly one. On approve, a nonempty response travels with the gate's input
to the next step; on reject it becomes the feedback reason. `--response-file
<utf8-file>` records a long answer verbatim. A brief `resume_after_verdict`
block right after a verdict is the normal handoff; the coordinator resumes.

## Stop and recover

Abort a non-final run with `run abort "$ARCHON_RUN_ID" --reason "<why>" --json`;
it cancels that run only and ends its seats. For a blocked run, a 409, a daemon
restart or a lost seat, read [references/recovery.md](references/recovery.md).

## Seats belong to the runtime

The runtime creates, briefs and cleans up every seat. Leave seat tmux sessions
alone: only an orchestrated controller directs its own bound workers. The
operator may type into any seat at any time; a dispatch waits through their
turns and still completes on the run's sentinel. Inside a seat, treat what the
operator types as their instruction.
