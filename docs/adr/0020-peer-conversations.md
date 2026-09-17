# Peer conversations and authored execution duration

Accepted 2026-09-17. Implements `form-0wz`. Supersedes the fixed peer-turn and
first-peer facilitator schedule in ADR-0007. The running contract is
[CONTRACT.md](../CONTRACT.md).

## Decision

A peer formation prepares independent openings, then lets the same participants
converse. Archon provisions one seat per configured slot. Every opening finishes
before any opening is published to the conversation. Completed openings are
retained individually if another participant fails during preparation.

After publication, all seats work concurrently. They choose when to contribute,
respond or propose a result. There is no fixed turn order, round count, leader,
or two-participant restriction. Local `archon peer` commands append to the same
run-, formation- and attempt-specific journal. Writes are locked, attributed,
ordered and bounded; readers wait on filesystem events rather than polling.
Agents must use those commands instead of editing the file directly.

Any participant can propose the full result. Every participant, including its
author, must acknowledge that proposal. Dissent names the proposal it contests;
a revision needs fresh acknowledgements. Acknowledgement can mean agreement
that the result accurately states unresolved tensions and the operator's
decision, rather than agreement on the substantive answer. The acknowledged
result follows the existing output-port and gate contracts.

The peer module owns journal state, completion and wake-up. The run engine owns
admission, graph routing and execution allocation. The existing seat transport
owns harness startup, prompt delivery, native completion and cleanup. A whole
peer formation consumes one graph dispatch, regardless of its message count.

## Time and finality

An optional `[formation.execution] timeoutSeconds` authors the allocation.
Omission inherits the executor default captured when the run is admitted.
The board snapshot freezes explicit overrides. The formation allocation begins
at `node_started` and includes startup, openings, conversation and finalization.
The effective deadline is bounded by the run's remaining execution allowance;
an explicit formation value is not capped by the executor's default.

Agents receive the absolute deadline and instructions to leave time for a result
and their completion receipts. There is no silent extension or fixed extra
finalization interval. An acknowledged, valid result can include disagreement.
Missing acknowledgements, invalid output or an unfinished turn at expiry blocks
visibly with evidence retained. Completed formation time does not include a
downstream human gate's wait.

The deadline and journal survive interruption. This change does not add general
multi-seat recovery: interrupted peer execution blocks for inspection rather
than receiving another full budget. A deliberate retry is a separate attempt
with separate evidence and remains subject to the run's dispatch/attempt limits.

On the session channel, every successful peer seat stays on call when its output
reaches a human gate. Only the operator makes that gate's decision.

## Configuration and verification

Durations are mission authoring, not source-level policy. Different formations
and runs may use different allocations. Model and effort remain persona
settings frozen by run admission; the peer mechanism does not select models.

The CLI, board API and cockpit share the same duration field. Zero in the
authoring mutation removes an override; a stored override must be positive.
Tests cover concurrent conversation, independent openings, arbitrary participant
counts, proposal acknowledgement, preserved disagreements, deadline and
cancellation behavior, safe artifacts, different durations and frozen defaults.
Real-seat evidence complements these tests before deployment.
