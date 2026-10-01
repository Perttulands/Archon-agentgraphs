# Recover a run

Read with the main [Archon skill](../SKILL.md). All commands take `--server
"$ARCHON_SERVER"`. Preserve briefs, transcripts and artifacts while you inspect.

## Read the block

Start from fresh `run status "$ARCHON_RUN_ID" --json`: `status`, `final`,
`resumeAllowed` and `resumePolicy`. The run evidence route
`GET $ARCHON_SERVER/api/runs/<runId>/evidence/problems` lists every
block with `code`, `reason`, `resumeAllowed` and the `nodeIds` it names.

| Code | Meaning | Next move |
| --- | --- | --- |
| `resume_after_verdict` | A human verdict was just recorded. | Nothing; the coordinator resumes. |
| `run_work_unfinished` | Work remains that the run still owes. | Resume. |
| `reachable_node_starved` | A formation can never receive a missing input. | Not resumable. Fix the wiring and start a new run. |
| `resume_attempts_exhausted`, `revise_loop_exhausted`, `max_dispatch_exceeded` | A limit is spent (`resumePolicy: limit_exhausted`, `limit` names it). | Not resumable. Start a new run with a larger cap, or none. |
| `wall_clock_exceeded` | A dispatch ran past the run's wall clock. | The clock counts from the run's start, so start a new run with more time. |
| `formation_timeout_exceeded` | A step ran past its execution duration. | Inspect the partial evidence. `--mode redispatch` starts a fresh attempt with a fresh duration; `set-execution` changes later runs only. |
| Malformed `archon-verdict` | The judge broke the verdict contract. | Not resumable. Fix the judge brief and start a new run. |
| `persona_snapshot_invalid` | The run's frozen staffing cannot start a seat. | Start a new run. |

A failed or canceled run names who ended it in `endedBy`.

## Resume

For a resumable block whose cause is resolved:

```bash
archon --server "$ARCHON_SERVER" run resume "$ARCHON_RUN_ID" --reason "Recovery evidence inspected" --json
```

Resume runs whatever is still owed and never re-delivers an input a gate has
already evaluated. It cannot invent a missing completion.

When a seat died mid-turn and no completed evidence exists, abandon the open
dispatch and run the node again as a new counted attempt:

```bash
archon --server "$ARCHON_SERVER" run resume "$ARCHON_RUN_ID" --mode redispatch --reason "Seat lost; run the node again" --json
```

## HTTP 409

A 409 means your view is stale. Read fresh status and use the current
`requestedSeq`; never reuse an old one or derive it from `eventCount`.
`coordinator is executing` means a command kept running for the five seconds
the daemon waits for it: retry the same command after a few seconds.
`human gate request is no longer pending` means someone else decided first.

## After a daemon restart

The daemon restarts with the same state directory. At startup it recovers
eligible completed native evidence and records a block naming any unresolved
dispatch. It never adopts or cleans up old seats, except seats kept on call for
a human gate. An idle human request survives with its original `requestedSeq`:
read fresh status and decide that exact request. Then inspect `run list` and
`run status` and resume as above.

Explicit recovery from a chosen transcript is a daemon restart with
`--resume-run`, `--completed-transcript` and `--completed-brief` together. Before
using it, read the Operator procedure in `docs/CONTRACT.md` of the Archon source,
or `$ARCHON_SHARE/docs/CONTRACT.md` of an installed release
(`<prefix>/lib/archon/current/share/archon`).
