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
  The same `.needs-you.json` record keeps each ask delivered once.

## Seats on call

- On a session-channel run, a formation whose output can reach a human gate
  through gates alone (for example work, then a judge gate, then a sign-off
  gate) keeps its seats when it finishes instead of ending them. The ledger
  records `seat_cleanup` with outcome `kept_on_call`. Other formations,
  including judge chains, end their seats as today.
- The asking formation of a human gate request is the nearest formation behind
  the gate's input, following that input back through gates. Its latest
  attempt's kept seats receive the ask.
- The ask is written to `<state-dir>/briefs/gate-<run>-<seq>.md` and pasted into
  each of those seats as a pointer, only while the agent is idle and its input
  line is empty. An undelivered ask stays unrecorded and is retried within
  seconds, not minutes. Every seat of a peer formation gets it; the operator
  may talk to any of them.
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
- A kept seat ends when its turn is over and one of these holds:
  - it has received at least one ask, and every ask delivered to it has been
    decided;
  - its formation starts a new attempt, which gets fresh seats as today;
  - the run is final or aborted.
  Before ending a seat that is mid-turn, the runtime waits up to 60 seconds for
  it to go idle, so the agent's closing reply stays readable. Cleanup ends only
  that seat, by immutable ID, and records `seat_cleanup`.
- Daemon shutdown leaves kept seats running. Startup verifies them from the
  ledger by recorded identity and keeps managing them. A kept seat that is gone,
  or whose tmux server changed, is recorded as such.
- If no kept seat can receive a human gate's ask, the ask goes to the notify
  command, and the cockpit says why. That happens when every seat has died, on
  the lab executor, or when no formation lies behind the gate.

## Typing into a seat

- The operator may type into any live seat at any time and interact with the
  agent normally, whether it is working a dispatch, on call, or idle. The seat
  terminal WebSocket, `GET /api/formations/runs/{runId}/seats/{createdSeq}/terminal`,
  accepts input and resize frames as CHROTE's terminals do, and CHROTE reaches
  the same sessions in the shared tmux pool.
- The runtime tolerates the operator's turns. It pastes a brief or an ask only
  while the agent is idle and its input line is empty. A turn the operator
  starts neither completes nor fails a dispatch by itself: completion still
  needs the exact run's completion evidence, which may arrive on a later turn.
  What the operator tells a working agent may change its work; that is the
  operator's call.
- The seats projection marks a seat `onCall` while an ask delivered to it waits,
  and the cockpit learns of changes without a reload.
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
