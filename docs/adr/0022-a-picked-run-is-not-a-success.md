# A picked run is not a success

Accepted 2026-10-16. Implements `archon-ofnp`. Supersedes the choice
made while grilling best of N (`archon-gihe`, question 30) that a pick leaves
no mark downstream: a picked attempt went on exactly as a gate's pass would,
and the run succeeded. The running contract is [CONTRACT.md](../CONTRACT.md).

## Context

A pick happens only when a step's Limit card is spent and no attempt passed
its gate, so the picked attempt is always failed work kept because nothing
better exists. Best of N carried that work on as a pass and said nothing.

The Ahti character runs showed what that costs. E1's picked plate, failed by
its judge for the underarm hair, went into E2; E2's picked meshes, also failed,
went into E3. Each run reported `succeeded` and its outputs looked like any
other, so the driving agent chained failed work into the next mission twice and
nothing stopped or warned it. Korpi's character contract (Stage handovers)
already states the rule the runs lacked: best-at-limit goes forward only as
explicit repair input that the receiving stage must close before acceptance.

## Decision

- A run whose work reached its done End through a pick ends with its own
  final outcome, `run_picked` with status `picked`, never `succeeded`. It is
  visible as such in run status and the run list, `run wait` and `run follow`
  (exit code 7), the API and OpenAPI, final notifications and the cockpit
  ("Picked, not passed").
- A run that crossed a pick but ended at a rejected End stays failed with
  `path_rejected`; a missing required output still fails it with
  `missing_output`.
- Every mission output whose value came through a pick carries `picks`: the
  picked step, attempt and gate, who picked, and the picked attempt's own
  verdict as recorded, with the judge's or operator's reason and findings. An
  output can come through more than one pick (a picked draft feeding a later
  best-of-N step), so it is a list. Outputs no pick carried carry nothing new.
- The `archon` skill tells a driving agent that a picked output is failed
  work: feed it only into a mission that opens with a repair step whose gate
  checks the listed findings, or stop the chain and report.

Archon still only chains agents and gates (ADR-0021): it does not refuse to
start a mission on picked input. It states the truth about the work, and the
driver decides with the findings in hand.

## Consequences

- Within a run nothing changes: a pick still routes the picked attempt down
  the gate's pass route, so a mission can repair picked work itself. The run
  is still `picked`, since its work descends from failed work.
- Drivers that treated exit 0 or `succeeded` as "use the outputs" now stop at
  a picked run instead of chaining it.
- No compatibility path: ledgers that ended `run_succeeded` through a pick
  before this change keep that outcome; nothing is migrated, since every
  ledger still loads.
