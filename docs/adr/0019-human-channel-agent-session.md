# Human gates reach the operator by mission channel

Accepted 2026-09-17 by the operator's decision form-xex. Operator request under
form-3yd.10: a mission chooses how its human gates reach the operator, and the
operator wants to answer the first real Wayfinding run in an agent's tmux
session instead of by email. The operator chose to talk to the agents that asked
(form-xex option c), and allowed those agents to record a decision the operator
has confirmed. The operator then ruled that typing into agents must never be
blocked: "I want to be able to interact with agents normally in tmux even if
they are working on a mission."

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
- The ask is written to `<state-dir>/briefs/gate-<run>-<seq>.md` and pasted into
  each receiving seat as a pointer, only while the agent is idle and its input
  line is empty. A seat not yet reached is retried within seconds.
- Delivery is recorded in the ledger. `human_ask_delivered` names the request,
  the gate, the asking formation, and the seat's slot and created sequence, one
  event per seat. `human_ask_fallback` names the request and the reason. They
  are appended under the run's command lock and do not affect execution replay.
  The projection shows them on the waiting gate (`askedSeats`, `fallbackReason`)
  and marks each receiving seat `onCall`, so the event stream updates the
  cockpit.
- The brief names the gate, its criterion, the exact pending request and the
  decisions already recorded in the run, and it sets these rules:
  - Present the gate's question plainly, from your own work, and help the
    operator think it through. Offer a view only when asked, and label it as
    yours.
  - Only the operator decides. Draft the response in the operator's words, show
    it with the verdict, and record it only after they confirm. Use the exact
    `gate approve` or `gate reject` command given, with `--requested-seq` and
    `--relayed-by <slot-id>`. If another seat or the cockpit decided first, the
    command returns 409; say so.
  - The formation brief's limits still apply. Running the given gate command
    is the one exception to its bans.
- Kept seats are reconsidered only when the run settles or starts dispatching a
  formation, so no gate evaluation is in flight for their output. A kept seat
  ends when it is idle and one of these holds:
  - it has received at least one ask, and no open request names its formation
    as the asker;
  - its formation starts a new attempt, which gets fresh seats as today (the
    old seats end first, because the names collide);
  - the run is final or aborted.
  Before ending a seat that is mid-turn, the runtime waits up to 60 seconds for
  it to go idle, so the agent's closing reply stays readable. Cleanup ends only
  that seat, by immutable ID, and records `seat_cleanup`.
- Daemon shutdown leaves kept seats running. Startup verifies them from the
  ledger by recorded identity and keeps managing them. A kept seat that is gone,
  or whose tmux server changed, is recorded as such.
- A human gate's ask falls back to the notify command, once, with a recorded
  reason, in these cases:
  - no kept seat can receive it: every seat has died, the executor is lab, or
    no formation lies behind the gate;
  - every seat that received it is gone while the request still waits.
  With no notify command, the recorded reason is all there is, and the cockpit
  shows it.

## Typing into a seat

- The operator may type into any live seat at any time and interact with the
  agent normally, whether it is working a dispatch, on call, or idle. The seat
  terminal WebSocket, `GET /api/formations/runs/{runId}/seats/{createdSeq}/terminal`,
  accepts input and resize frames as CHROTE's terminals do, and CHROTE reaches
  the same sessions in the shared tmux pool.
- The runtime tolerates the operator's turns:
  - It pastes a brief or an ask only while the agent is idle and its input line
    is empty.
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
