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
| `coordinator_interrupted` | The daemon restarted with the run in flight: open dispatches it names, or between steps. | Resume; with open dispatches and no completed evidence, `--mode redispatch`. |
| `run_work_unfinished` | Work remains that the run still owes. | Resume. |
| `reachable_node_starved` | A formation can never receive a missing input. | Not resumable. Fix the wiring and start a new run. |
| `limit_reached` | A Limit card is spent: "Review used 3 of 3 rounds" or "Review used 30 min of 30 min" (`resumePolicy: grant`, `limit` names the card and its `kind`). | With the operator's authority, `run resume "$ARCHON_RUN_ID" --grant --reason '<why more>'` gives one more round, or the card's time again; or `run abort`. A plain resume is refused. |
| Malformed `archon-verdict` | The judge broke the verdict contract. | Not resumable. Fix the judge brief and start a new run. |
| `persona_snapshot_invalid` | The run's frozen staffing cannot start a seat. | Start a new run. |

A failed or canceled run names who ended it in `endedBy`.

## Resume

For a resumable block whose cause is resolved:

```bash
archon --server "$ARCHON_SERVER" run resume "$ARCHON_RUN_ID" --reason "Recovery evidence inspected" --json
```

Resume runs whatever is still owed and never re-delivers an input a gate has
already evaluated. It cannot invent a missing completion. Gates still waiting
keep waiting through it, and a verdict given while the run was blocked is
routed by it.

When a seat died mid-turn and no completed evidence exists, abandon the open
dispatch and run the node again as a new counted attempt:

```bash
archon --server "$ARCHON_SERVER" run resume "$ARCHON_RUN_ID" --mode redispatch --reason "Seat lost; run the node again" --json
```

## HTTP 409

A 409 means your view is stale. Read fresh status and use the current
`requestedSeq`; never reuse an old one or derive it from `eventCount`.
`coordinator is executing` answers a resume when the run's command kept running
for the five seconds the daemon waits for it: retry after a few seconds. A
verdict never gets it. `human gate request is no longer pending` means someone
else decided first, or the gate has since evaluated a newer input.

## After a daemon restart

The daemon restarts with the same state directory. At startup it recovers
eligible completed native evidence and records a block naming any unresolved
dispatch. At startup it neither adopts nor cleans up old seats. A seat the
shutdown left working stays the run's: aborting the run, or resuming so the
step runs again, ends it; seats kept on call for a human gate stay on call. A run that only waited on its gates survives waiting, each
request with its original `requestedSeq`: read fresh status and decide that
exact request. A run that was between steps is blocked; its gates still take
verdicts. Then inspect `run list` and `run status` and resume as above.

Explicit recovery from a chosen transcript is a daemon restart with
`--resume-run`, `--completed-transcript` and `--completed-brief` together. Before
using it, read the Operator procedure in `docs/CONTRACT.md` of the Archon source,
or `$ARCHON_SHARE/docs/CONTRACT.md` of an installed release
(`<prefix>/lib/archon/current/share/archon`).
