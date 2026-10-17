---
name: archon
description: Author Archon missions and drive their runs through the archond daemon. Use when building, editing, staffing or wiring a mission, starting or following a run, answering a human gate, or recovering a blocked run.
---

# Archon

What we already learned lives in the brain: `brain show agent-completion` (agents that say done before the work is real), `brain show workflow-encoding` (encoding work as Beads graphs), `brain show agent-quality` (iterating agents against a quality bar, with measured costs).

This skill documents the Archon contract of VERSION 0.1.0 as of 2026-10-01:
the reusable unit is a mission, each slot owns its harness, model and effort,
a Limit card is the only run limit, drivers pull with `run wait`, and every surface uses
current names only. It ships with that source.
`archon --version` names the build on PATH, and `archon --server "$ARCHON_SERVER"
version` the daemon's too. When a build is older, a flag or behaviour named
here may differ: read the command's `-h` and trust the binary.

Seats are ordinary tmux agent sessions with full access, as in CHROTE; Archon
adds no sandbox or confinement, so safety comes from the agents' own harness
settings. `docs/PRODUCT.md` in the Archon source or `$ARCHON_SHARE/docs/`
states the product's philosophy and goals.

## Vocabulary

- A **mission** is one reusable `<slug>.mission.toml` graph with a slug and a
  revision. Its **Input card** is the entry: a goal, the mission's declared
  **inputs** and an `out` port (`inputCard` in JSON and TOML).
- An **input** is a named value each run supplies (`text`, `file` or
  `folder`, required or optional). Step briefs reference it as `{name}`. A
  mission that declares none takes one required text input, `brief`.
- A **formation** is a step. `solo` has one seat, `peer` has two or more seats
  that converse, and `orchestrated` has one controller directing its bound
  workers. These three are the only types.
- A **slot** is a position in a formation. It owns its harness (`claude-code`
  or `openai-codex`), model and effort, and may name a **role**. Its runtime
  agent session is a **seat**.
- A **role** (persona card) is optional, generic role text such as a code
  reviewer; it carries no harness, model or effort. Every Archon has the
  built-in roles `scout`, `planner`, `builder`, `judge`, `orchestrator`,
  `debugger` and `reviewer`. A slot without a role is a **vanilla** agent:
  `claude-code · opus · low`. Put what is specific to a mission in its step
  briefs, never in a role. `agent retire <id>` and `agent delete <id>` name
  the slots that use a role; a slot with a retired role does not run, and a
  role a slot names is not deleted.
- A **gate** has a criterion and kinds `code`, `formation` and `human`, run in
  that order and stopping at the first failure. Its ports are `in`, `pass`,
  `fail` and `judge`.
- An **End node** ends a path on purpose, with outcome `done` or `rejected`.
  Its only port is `in`; any number of routes may lead into it.
- A **Limit card** caps the rounds, time and tokens of the step it covers, or
  of the whole mission when it covers the Input card. It is the only run limit.
- A **judge chain** is the formations wired from a gate's `judge` port back to it,
  step to step; a Tool or gate inside it, or no way back, makes it no chain.
  A **pushback edge** is a gate's `fail` wired back to work; it carries the
  verdict as feedback and starts the next attempt, capped only by a Limit card.
- A **run** snapshots the mission, its slots and any roles at admission. Later edits affect
  later runs only.

## Connect

Take `<archon-server>` and `<state-dir>` from the operator's runbook or
environment and set `ARCHON_SERVER` and `ARCHON_STATE`. The server is an HTTP URL
with a literal IP, with no trailing slash, credentials, query or fragment.
Keep runtime state outside any checkout.

- Runtime commands (`mission run`, `formation run`, `run ...`, `gate approve|reject`) always take
  `--server "$ARCHON_SERVER"`.
- Authoring takes `--server` too, so an open cockpit shows each edit live.
  `--workspace "$ARCHON_STATE"` authors the same files offline, for a state
  directory no daemon serves; runtime commands never run offline beside a
  daemon. There is no default: a command with neither flag says so and stops.
- A `--server` failure is final; nothing falls back to a local runtime.
- `archon -h` lists the nouns, `archon <noun> -h` a noun's commands and
  `archon <noun> <command> -h` a command's usage and flags; add `--server` for
  the daemon's form.

## Author a mission

Read before you write: the mission, its notes and the role roster. Preserve
the existing draft and notes as you staff slots, write executable briefs and
wire exact port IDs.

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
archon $S formation assign "$M" "$WORK" --slot "$WORK_SLOT" --harness openai-codex --effort medium --role builder --json
archon $S formation set-brief "$M" "$WORK" --goal "Make the change the brief asks for." --json
archon $S mission wire "$M" "$WORK:$WORK_IN" --json
```

Edit nodes in place so their edges survive: `formation rename`, `formation
set-type <solo|peer|orchestrated>` (to solo with several staffed slots, add
`--keep-slot <slot>`), `mission update "$M" --title|--goal|--input-hint|--file` (the Input card),
`gate update`. An empty value clears a field; `gate update` changes only the
flags given. `--input-hint` describes the implicit `brief` input of a mission
that declares no inputs.

`mission delete "$M"` removes the mission and its layout, retaining run history.
`formation delete "$M" "$WORK"` removes the step, its connections and Limit
cards. `gate delete "$M" "$GATE"` removes the gate and its routes, including
its judge attachment; the judge formations remain.

### Inputs

Declare what a run must supply instead of baking run specifics into briefs, and
reference each input from the briefs that need it:

```bash
archon $S mission input "$M" change --required --description "What to deliver and why" --json
archon $S mission input "$M" spec --kind file --description "An existing spec to follow" --json
archon $S formation set-brief "$M" "$WORK" --goal "Deliver {change}. Follow {spec}." --json
archon $S mission input "$M"
```

`mission input "$M" <name>` declares an input or changes only the flags given
(`--kind text|file|folder`, `--required` or `--optional`, `--description`);
`--delete` removes it. Without a name it lists what a run supplies, one line
each: name, kind, required or optional, description. Names are a lowercase
letter then lowercase letters, digits or underscores. Each seat's brief gets the
value in place of `{name}`, or `(not supplied)` for an optional input left out.
A declared input named `bead` wins for `{bead}`, following the same input
rules as other names. Otherwise `{bead}` takes the run's `--bead` value,
or an empty string when none was supplied; it needs no declaration.
Other references to undeclared names are the validation error
`unknown_input_reference`. To show a seat a literal `{name}`, write `{{name}}`;
it is never a reference. The first step also receives the inputs from the
Input card: the implicit `brief` unchanged, or one `name: value` line per
input.

### Outputs

A step output port carries `text` (the default), a `file` or a `folder`. A
file or folder port's payload is the absolute path the seat made; when Archon
accepts it, it keeps a copy of that attempt's version and routes the copy's
path, so a send-back loop that rewrites one project file keeps every version.
Set the kind when you add the port or later, by port ID or label:

```bash
archon $S formation add-output "$M" "$WORK" --label site --kind folder --json
archon $S formation set-port "$M" "$WORK" "$WORK_OUT" --kind file --json
```

Declare what a successful run promises its driver as **mission outputs** on
the Input card, each bound with `--from <step-id>:<port-id>` to one step output
port of the same kind:

```bash
archon $S mission output "$M" report --kind file --from "$WORK:$WORK_OUT" --required --description "The final report" --json
archon $S mission output "$M"
```

`mission output "$M" <name>` declares an output or changes only the flags
given (`--kind`, `--from`, `--required` or `--optional`, `--description`);
`--delete` removes it, and without a name it lists them. Names follow the input
rule. When the run ends at a `done` End, each output is the payload of its
port that went down the path to that End, never one a gate rejected. A
required output that path never delivered, or a file or
folder no longer there, fails the run (`run_failed`, code `missing_output`); a
run that ends at a `rejected` End promises nothing. Validation reports
`unbound_mission_output` (no `--from`, or no such step port),
`mission_output_kind_mismatch` and `invalid_port_kind`, and admission refuses
the run on them.

### Briefs belong to steps

Write what a step does in its formation brief (`formation set-brief --goal`,
`--file` for reference files). A role is reusable and generic; mission-specific
instructions stay in the step's brief, never in a role. Missions and gates take reference files too:
repeat `--file <path>` on `mission create|update` and `gate create|update`; on
update the list is replaced and `--file ''` clears it. Use absolute paths:
authoring refuses a relative one (`relative_file_reference`), since it has no
base for the cockpit or a seat. A file that does not exist yet is saved with a
`warning:` line, and `mission validate` warns `missing_file` until it exists.

### Harness, model and effort

Each slot sets its own harness, model and effort, in the mission, where the
operator can see them: `formation assign "$M" <formation> --slot <slot>
--harness <h> --effort <e> [--model <m>] [--role <role>]`. `--effort` is
required; a blank `--model` means the harness default. Saved characters are
named staffings listed by `agent characters`:
`--character <id>` stamps one's role, harness, model and effort onto the slot,
and any of those flags given beside it wins. `claude-code` takes
`low`, `medium`, `high`, `xhigh` or `max`; `openai-codex` also takes `ultra`,
though a known Codex model may take fewer. The
known models are in `GET /api/agents` `harnesses[].models` and the `formation
assign` usage: `claude-code` offers `opus`, `sonnet`, `haiku` and `fable`,
and Codex the models its CLI lists on the daemon host. The catalog is not exhaustive; other model
names are accepted without warnings and the harness decides whether they run.
Omit `--role` for a vanilla agent. The
operator sees and edits the same settings in the cockpit as a sentence on each
slot (`Worker 1 is vanilla on Claude Code · opus · low`), so state each slot in
full rather than leaving a choice to them. A role adds only its text, so one step's
settings never change another's. `formation inspect` prints each slot's
staffing, for example `vanilla · claude-code · opus · low`.

Choose effort by the step's job, with purpose. The role's kind names it:

| Job | Role kinds | Effort |
| --- | --- | --- |
| Architecture, design and review | `reviewer`, `judge`, `architect`, `designer`, `planner`, `orchestrator` | `xhigh` |
| Consequential review (release gate, irreversible change) | chosen by hand | `max` |
| Making things | `builder`, `debugger`, `operator` | `medium` |
| Errands | `verifier`, `scout` | `low` |

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
- **Human** waits for an explicit operator verdict, with no default verdict,
  and holds up only its own path: other branches keep running and several human
  gates can wait at once. See [Decide a human gate](#decide-a-human-gate).

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

The runtime supplies every judge seat with the `archon-verdict` contract:
exactly `verdict` (`pass` or `fail`), `reason` (string) and `evidence` (array of
strings), in one fenced block separate from `archon-outputs`. For a missing or
malformed verdict it re-prompts the same live seat once with the parse error.
If that correction fails, the run blocks resumably; `run resume` reruns the
failed final judge while retaining earlier completed reviews. `run resume
--take-fail` settles the gate as a fail instead, without asking the judge again.

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

### Limit cards

A run has no limits unless the mission holds a **Limit card**, and nothing adds
one for you: without a card a send-back loop continues until its gate passes or
you stop the run, and a step works as long as it takes. Add a card only when a
loop or a step needs a guard. It covers one target, a step or the Input card
(the whole mission), with three optional knobs:

- `--rounds` counts how many times the step may run, send-backs and resumed
  re-runs included (for a peer step, how many journal messages its
  conversation may hold), or how many step runs the whole mission may make,
  judges included.
- `--time` (whole seconds as `45s`, `30m`, `1h30m`) counts wall time while the
  step works, or while any step of the mission works. Waiting on a human gate
  or a blocked run counts nothing, so a send-back resumes a step with the time
  it has left. `--warn 5m` pastes a warning into the covered seats once when
  that much time is left.
- `--tokens` counts what the covered seats spend, approximately: input not
  read from the cache, cache writes included, plus output, subagents
  included, per dispatch. At the budget the working step stops; a count can
  pass it by what the seats spent in the last two seconds.

```bash
archon $S limit create "$M" --target "$WORK" --rounds 3 --json
archon $S limit create "$M" --target input --rounds 20 --time 2h --warn 10m --title "Mission cap" --json
```

`limit update "$M" "$LIMIT" --rounds <n>|--time <d>|--warn <d>|--tokens
<n>|--when-spent stop|best|fail|--target <t>|--title <t>` changes a card (an empty `--rounds`, `--time`,
`--warn` or `--tokens` clears that knob) and `limit delete "$M" "$LIMIT"` removes it. Validation
rejects a card wired to nothing, a second card on one target, a value that is
not a positive whole number and a warning without time or not shorter than it
(`invalid_limit`). At a spent limit the step does not start, or stops when its
time runs out. By default the run blocks with `limit_reached` and a plain reason,
"Review used 3 of 3 rounds", "Review used 30 min of 30 min" or "Review used
51,230 of 50,000 tokens"; see Recover.

`--when-spent fail` on a step or mission card ends the run as failed at the
spent limit, with its usage reason and every attempt's verdicts and outputs
kept as evidence. It needs no operator grant. `stop` is the default.

**Best of N.** `--when-spent best` on a step's card picks the best attempt
instead of blocking: when the card is spent, the step's gate compares the
attempts it judged side by side and the one it picks goes on through the
gate's pass route, as if the gate had passed it (to the next step, an End and
the mission's outputs). The step needs a selecting gate: one judge or human
gate that only it feeds and whose fail returns to it; validation names a best
card without one. The final judge picks with one `archon-pick` block
(`{"attempt":n,"reason":"...","evidence":[...]}`); every judge of the chain
sees the attempts, and a judge before it only recommends one in its output,
which the later judges read. The runtime gives each its contract. A human gate
asks the operator (see Decide a human gate), with the judges' pick as a
recommendation when the gate has both. An attempt the limit cut short or a
crash ended is never picked, and with only one judged attempt Archon takes it.
The pick's judges spend mission-card rounds like any step, so leave the
mission card room for them. A mission output bound to another port of the
step, one that skips the selecting gate, keeps its own latest attempt. A pick
happens only when no attempt passed, so the picked attempt is failed work: a
run whose work reached its done End through a pick ends `picked`, not
`succeeded` (see Results).

```bash
archon $S limit create "$M" --target "$WORK" --rounds 3 --when-spent best --json
```
When a step runs long, check `run seats` (a seat's `waiting` says what it waits
on) or open the seat; `run wait --until any-change` reports each `seat_state`.

### Human channel

Set `mission create|update --human-channel session` only when the operator asks to
answer gates through the asking seats. Read
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

To build and launch a lab daemon, import examples, configure
notifications or smoke-test routing, read
[references/lab-and-examples.md](references/lab-and-examples.md).

## Manage a mission for the operator

Clarify the mission's inputs in the conversation, and ask when an input or the
authority to run is missing. Start only on an explicit go, passing the owning
Bead with `--bead`, and follow the run with `run wait`.

For a mission meant to run unattended:

- Give each Limit card `--when-spent fail`, so exhaustion ends the run. `best`
  still blocks when no attempt was judged, and asks the operator when its gate
  is human.
- Leave the admitted run as it is. A grant or a mission edit needs the
  operator's authority; improvements land between runs, through a retro
  recorded on the owning Bead.
- Ask the operator only what the mission cannot settle: a missing input or
  authority, or a taste or direction call, as one question with options and a
  recommendation.

Give the rundown from `run attempts` and the artifacts: what ran, what passed
or failed, what it produced and where the evidence is.

## Run a mission

```bash
archon $S mission input "$M"
ARCHON_START=$(archon $S mission run "$M" \
  --input change="Add CSV export to the report page" --input-file spec=/abs/spec.md \
  --actor "agent:<your session>" --bead "$ARCHON_BEAD" --context-path /abs/prior-art --json)
ARCHON_RUN_ID=$(jq -er .data.runId <<<"$ARCHON_START")
archon $S run status "$ARCHON_RUN_ID" --json
```

- Supply every input by name: `--input name=value` sends the value as written,
  and `--input-file name=path` sends a local UTF-8 file's text. A `file` or
  `folder` input takes an absolute path to an existing file or directory on the
  daemon's host. A mission without declared inputs takes `--input brief=...`.
  Admission names each input that is missing, unknown or not a usable path
  (`missing_input`, `unknown_input`, `invalid_input`) and records no run. Run
  status prints each input's name, kind and first line, up to 100 characters.
  JSON keeps full values in `inputs`.
- `--bead` names the run's owning Bead; an Input card has none.
- `formation run "$M" <formation>` runs one step on its own with the same
  inputs and run flags; its step receives the inputs as a first step does.
- Omit `--cwd` and the daemon allocates a private workspace for the run. Pass
  `--cwd /abs/existing/dir` to work in an existing project.
- `--context-path` names absolute existing files or directories the seats must
  inspect as prior art. They are read-only references, frozen as paths; repeat
  the flag for more.
- A run's only limits are the mission's Limit cards (above); a launch sets
  none. Without a card a send-back loop continues until its gate passes or you
  stop the run with `run abort`.
- `--actor <name>` names the acting agent. Every CLI command taking it
  defaults to `ARCHON_DRIVER`, then `agent:archon`, resolved only by the CLI
  and sent as `actor` to the API. Pass your own session name when starting
  a run. Run status, lists and the run bar show that recorded ID verbatim
  (`startedBy` in JSON), including the default `agent:archon`.
- A lost receipt: check `run list --mission "$M" --json` before starting again.
  It lists only that mission's runs, each with `status`, `startedBy`,
  `startedAt`, `updatedAt` (a final run ended then) and `inputs`.

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
| 3 | The run needs you | Do what the paragraph says. For a gate it names the gate, criterion, input and where each verdict leads, and prints the exact `gate approve`/`gate reject --requested-seq <n>` commands, or for a pick the candidates and the `gate pick` command; for a block, the `run resume` or `run abort` command. Answer with the operator's authority, then wait again. |
| 0 | The run ended | Read how: its status, the step it stopped at, the reason and who ended it. No further wait. |
| 7 | The run ended `picked` | Its outputs are failed work a pick carried on; see Results before chaining them. No further wait. |
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
- Answer the moment a wait returns: the daemon records a verdict at once, even
  while other steps run, and a resume sent while the run's command is still
  settling waits for it on the daemon.
- `--json` prints the same answer for machines: `outcome`, `seq`, `status`,
  `asks`, `end`, `changes` and `next` (the next command, with `--json`).
- `run status "$ARCHON_RUN_ID" --json` returns one projection at once, and
  `run follow "$ARCHON_RUN_ID" --json` streams a projection after each durable
  change until the run is final. Prefer `run wait` for driving; interrupting
  any of them stops watching, not the run. `run logs` is the same sanitized
  view as status.

Read `status` (`running`, `waiting_human`, `blocked`, `succeeded`, `picked`,
`failed`, `canceled`), `final`, `resumeAllowed` and `waitingGates`. `waiting_human` means
the run needs the operator, even while other steps still run: each
`waitingGates` entry has `gateId` and `requestedSeq`, and several gates can
wait at once; answer each. `blocked` is never a delivery. Runtime JSON is wrapped in
`.data`; authoring JSON is not.

Results: a succeeded or picked run's `run status` lists its mission outputs, one `output`
line each with name, kind and value (`outputs` in JSON, each with `name`,
`kind`, `value`, `nodeId`, `portId` and `attempt`); the final `run wait`
answer carries them too. Chain a mission by passing a value on:

```bash
REPORT=$(archon $S run status "$ARCHON_RUN_ID" --json | jq -er '.data.outputs[] | select(.name == "report") | .value')
archon $S mission run "$NEXT_MISSION" --input spec="$REPORT" --json
```

A `picked` run's outputs are failed work, not results. Each output that came
through a pick carries `picks` (the step, `attempt`, `gateId`, `decidedBy` and
the `verdict` that failed it, with `reason` and `findings`); in text, `picked`
and `finding` lines follow its `output` line. Feed a picked output only into a
mission that opens with a repair step whose gate checks those findings, or stop
the chain and report the run and its findings. Never pass it on as if it had
succeeded.

Every attempt of every step, judges included, has its own directory under
`$ARCHON_STATE/.archon/artifacts/<runId>/<nodeId>/<attempt>/`.
`run attempts "$ARCHON_RUN_ID" [--node <id>] [--files] [--json]` lists each
step and attempt, its status, declared outputs and verdict with a reason
one-liner, and whether it was picked. A stopped attempt carries the run's
terminal reason when that end stopped it; status also names the last blocked
or active step. `--files` adds its directory files; `--json` keeps the complete
evidence. `run status` prints a `pick` line for each pick (`picks` in JSON).
The cockpit's output chips open the producing step's attempts, including nested
videos and contact sheets; cited images open from their thumbnails. Cite files
relative to the producing attempt directory or an output folder, or use their
absolute paths. The cockpit resolves the same citations in attempts, run
evidence and gate evidence; ambiguous relative names stay text. The Produced
menu groups files in run order and lists kept copies once. Check each
`seat_cleanup` outcome (`ended`, `left_socket_changed`, `left_cleanup_failed`).

The runtime writes the brief each seat receives, including its output contract;
you author only the formation brief. When a seat's output did not route, or you
are working inside a seat, read [references/seat-output.md](references/seat-output.md).

## Decide a human gate

Read [Human verdicts](../../docs/CONTRACT.md#human-verdicts) for the rules and
[Run evidence](../../docs/CONTRACT.md#run-evidence) for the request and
route-preview fields (`docs/CONTRACT.md` in the Archon source or
`$ARCHON_SHARE/docs/CONTRACT.md` in an installed release).
Decide only with the operator's authority. A `run wait` that exited 3 already
printed both commands with the current `--requested-seq`. Otherwise read fresh
status and take `ARCHON_GATE_ID` and `ARCHON_REQUESTED_SEQ` from the same
`.data.waitingGates` entry. Use `run gates "$ARCHON_RUN_ID"` to list them and
`gate request "$ARCHON_RUN_ID" "$ARCHON_GATE_ID"` to inspect the question,
input and routes before answering.

```bash
archon $S gate approve "$ARCHON_RUN_ID" "$ARCHON_GATE_ID" --requested-seq "$ARCHON_REQUESTED_SEQ" --response "$ARCHON_RESPONSE" --json
archon $S gate reject  "$ARCHON_RUN_ID" "$ARCHON_GATE_ID" --requested-seq "$ARCHON_REQUESTED_SEQ" --response "$ARCHON_RESPONSE" --json
```

Run exactly one. On approve, a nonempty response travels with the gate's input
to the next step; on reject it becomes the feedback reason.
Use `--response-file <utf8-file>` for a long answer supplied
as a file. For a session-channel relay, follow
[human-gates.md](references/human-gates.md).

When `gate request` asks for a best-attempt pick, inspect its candidates and
any judge recommendation. Use `run attempts "$ARCHON_RUN_ID" --node <step>`
to read each in full, then pick one candidate:

```bash
archon $S gate pick "$ARCHON_RUN_ID" "$ARCHON_GATE_ID" --attempt 2 --requested-seq "$ARCHON_REQUESTED_SEQ" --reason "Tightest draft" --json
```

For pick rules, read [Picking the best attempt](../../docs/CONTRACT.md#picking-the-best-attempt).

## Stop and recover

Abort a non-final run with `run abort "$ARCHON_RUN_ID" --reason "<why>" --json`;
it cancels that run only and ends its seats. For a blocked run, a 409, a daemon
restart or a lost seat, read [references/recovery.md](references/recovery.md).

## Seats belong to the runtime

The runtime creates, briefs and cleans up every seat. A dispatched seat idle
at an empty prompt without native transcript activity for five minutes gets
one continue instruction for its missing sentinel. Another five-minute idle
window blocks with `seat_idle_without_sentinel`; inspect it and use
`run resume --mode redispatch` to run the step again. Busy agents, pending Claude background tasks and unsent
operator input reset the quiet window. Leave seat tmux sessions
alone: only an orchestrated controller directs its own bound workers. The
operator may type into any seat at any time; a dispatch waits through their
turns and still completes on the run's sentinel. Inside a seat, treat what the
operator types as their instruction.
