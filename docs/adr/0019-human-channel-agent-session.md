# Human gates reach the operator by mission channel

Accepted 2026-09-17 by the operator's decision form-xex. Operator request under
form-3yd.10: a mission chooses how its human gates reach the operator, and the
operator wants to answer the first real Wayfinding run in an agent's tmux
session instead of by email. The operator chose to talk to the agents that asked
(form-xex option c), and allowed those agents to record a decision the operator
has confirmed. The operator then ruled that typing into agents must never be
blocked: "I want to be able to interact with agents normally in tmux even if
they are working on a mission."

Clarified 2026-09-17 in form-epn and form-n64.7: a complete, unambiguous
operator verdict for the pending gate, with the exact response to record,
is itself confirmation. The agent need not ask for the same confirmation again.

Before this decision one daemon flag, `--notify-command`, sent every run's asks
the same way. Seats ended when their formation finished, the cockpit's seat
terminal was view-only, and the contract told operators not to type into seats.

## Decision

- A mission has `humanChannel`: `notify` (the default, stored as absent) or
  `session`. A run reads it from its frozen board, so a change applies to runs
  started afterwards. The Start mission dialog shows it.
- `notify` keeps today's behaviour: every ask goes to `--notify-command`, if set.
- `session` changes human gates only. A human gate's ask goes to the seats of
  the formation whose work the gate is judging, and the operator talks to those
  agents. Escalations, blocks and final outcomes still go to the notify command.
  Session delivery works whenever the tmux executor runs, with or without
  `--notify-command`.

## Seats on call

- On a session-channel run, a formation whose output can reach a human gate
  through gates alone (following pass and fail routes, never a judge port) keeps
  its seats when it finishes instead of ending them. Examples are work, then a
  human gate; or work, then a judge gate, then a sign-off gate. The ledger
  records `seat_cleanup` with outcome `kept_on_call`. Judge chain members and
  formations with no such path end their seats as today.
- The asking formation of a human gate request is the nearest formation behind
  the gate's input, following that input back through gates. The kept seats of
  its latest attempt receive the ask. For solo and peer formations that is every
  seat, and the operator may talk to any of them. For an orchestrated formation
  it is the controller only.
- The ask is written per receiving seat to
  `<state-dir>/briefs/gate-<run>-<seq>-<slot>.md`, because each seat relays under
  its own slot ID, and pasted into that seat as a pointer, only while the agent is idle and its input
  line is empty. A seat not yet reached is retried within seconds.
- Delivery is recorded in the ledger. `human_ask_delivered` names the request,
  the gate, the asking formation, and the seat's slot and created sequence, one
  event per seat. `human_ask_fallback` names the request and the reason. They
  are appended under the run's command lock and do not affect execution replay.
  The ledger accepts nothing after a final event. After a block it accepts
  only a resume, cancel or failure, plus the `seat_cleanup` of a kept seat
  written just before that cancel or failure; readers look past such cleanups,
  so the run still reads as blocked. A delivery or fallback due while the run
  is blocked is recorded after it resumes.
  The projection shows them on the waiting gate (`askedSeats`, `fallbackReason`)
  and marks each receiving seat `onCall`, so the event stream updates the
  cockpit.
- The brief names the gate, its criterion, the exact pending request and the
  decisions already recorded in the run, and it sets these rules:
  - Present the gate's question plainly, from your own work, and help the
    operator think it through. Offer a view only when asked, and label it as
    yours.
  - Only the operator decides. A complete, unambiguous verdict for this pending
    gate, with the exact response to record, is itself confirmation. Record
    those words immediately, for either approval or send-back. If the agent
    drafts or paraphrases the response, or the verdict, response or intended
    gate is ambiguous, show the proposed verdict and exact response together
    and wait for the operator's confirmation. Never infer a verdict from
    discussion or invent missing response text. Use the exact
    `gate approve` or `gate reject` command given, with `--requested-seq` and
    `--relayed-by <slot-id>`. A 409 saying the coordinator is executing means
    the run is busy for a moment: wait a few seconds and run the same command
    again. A 409 saying the request is no longer pending means another seat or
    the cockpit decided first: say so.
  - The formation brief's limits still apply. Running the given gate command
    is the one exception to its bans.
- Kept seats are reconsidered only when the run settles or starts dispatching a
  formation, so no gate evaluation is in flight for their output. A kept seat
  ends when it is idle and one of these holds:
  - it has received at least one ask, and no open request names its formation
    as the asker;
  - its formation starts a new attempt, which gets fresh seats as today (the
    old seats end first, because the names collide);
  - the run is about to become final: succeed, fail, or be canceled.
  Before ending a seat that is mid-turn, the runtime waits up to 60 seconds for
  it to go idle, so the agent's closing reply stays readable. Cleanup ends only
  that seat, by immutable ID, and records `seat_cleanup`.
- Kept seats are ended, and their `seat_cleanup` recorded, before the event that
  makes a run final (`run_succeeded`, `run_failed` or `run_canceled`, including
  an abort of a waiting run), since the ledger accepts nothing after it. A
  blocked run keeps its kept seats. A cleanup decided while it is blocked,
  including a seat startup finds gone, is recorded after the run resumes, or
  just before the cancel or failure that ends it.
- Daemon shutdown leaves kept seats running. Startup verifies them from the
  ledger by recorded identity and keeps managing them. A kept seat that is gone,
  or whose tmux server changed, is recorded as such.
- A human gate's ask falls back to the notify command, once, with a recorded
  reason, in these cases:
  - no kept seat can receive it: every seat has died, the executor is lab, or
    no formation lies behind the gate;
  - every seat that received it is gone while the request still waits;
  - a paste may have changed a seat's input but its submission failed or could
    not be verified. Automatic delivery stops for that request, without
    clearing the input or pressing Enter again. The operator can answer in the
    cockpit or inspect the seat. The fallback records `seatCreatedSeq`, so later
    requests also skip that exact seat after restart. Healthy peers and new
    seats remain eligible; when none remain, the later request also falls back.
    A verdict racing the paste does not discard the affected seat identity.
    An agent that is merely busy, or an operator's
    unsent text found before a paste, still waits without a fallback.
  With no notify command, the recorded reason is all there is, and the cockpit
  shows it.

## Typing into a seat

- The operator may type into any live seat at any time and interact with the
  agent normally, whether it is working a dispatch, on call, or idle. The seat
  terminal WebSocket, `GET /api/formations/runs/{runId}/seats/{createdSeq}/terminal`,
  accepts input and resize frames as CHROTE's terminals do, and CHROTE reaches
  the same sessions in the shared tmux pool.
- The runtime tolerates the operator's turns:
  - It pastes anything into a seat only while the agent is idle and its input
    line is empty. That covers a formation brief, a peer's conversation
    brief and a gate ask, so the operator's unsent text never merges with a
    pointer.
  - A message the operator types during a dispatch neither completes nor fails
    it. Completion needs this run's exact completion sentinel on an agent turn
    after the pointer, together with the harness's native turn completion. That
    may come on a later turn than the one the pointer started.
  - A finished turn without the sentinel fails the dispatch at once only when
    the operator took no turn during it. Otherwise the dispatch keeps waiting,
    within the seat timeout.
  - The dispatch still fails loudly when the harness exits, the conversation
    is cleared or replaced (`/clear`, `/resume`), the model or effort changes,
    or the seat timeout expires.
- What the operator tells a working agent may change its work; that is the
  operator's call.
- Agents still must not type into seats, except an orchestrated controller
  directing its bound workers.

## Recording who relayed a decision

A verdict may carry `relayedBy`, the slot ID of the seat that ran the command.
It is stored on `human_verdict_recorded` and shown with the decision.
`decidedBy` stays `human:operator`: the operator decided, and the record says
which agent typed the command.

## Consequences

- An operator on the session channel works with the agents that did the work,
  in their own sessions and with their own context, rather than a relay.
- A send-back starts a new attempt with fresh seats, as before. The response
  the agent records must carry what the conversation settled, because the next
  attempt reads only that response.
- Idle agents stay running while a gate waits, one per slot of the asking
  formation.
- Anyone who reaches the cockpit can type to any live seat, which runs with its
  harness's permissions. That is the trust CHROTE's own terminals on the
  tailnet already carry, by the operator's decision that the tailnet needs no
  further authentication.
- Choosing `session` means no email for human gates on that run. An operator
  away from the screen learns of a gate only when they return to the cockpit or
  CHROTE.
