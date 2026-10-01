# Human gates on the session channel

Read with the main [Archon skill](../SKILL.md). This covers missions set to
`--human-channel session` and seats that relay an operator's decision.

## Where the ask goes

A run keeps the channel its mission had at admission. On `notify` (the default)
every human-gate ask goes to the daemon's `--notify-command`, one message per
ask with the exact approve and reject commands. Never run the notify command or
resend a message yourself.

On `session` the ask goes to the **asking formation**: the nearest formation
behind the gate's input, followed back through gates. That formation's seats
stay alive after it finishes (`seat_cleanup` outcome `kept_on_call`): every seat
of a solo or peer formation, the controller of an orchestrated one. Each
receives a brief pointer at `<state-dir>/briefs/gate-<run>-<seq>-<slot>.md`.
Escalations, blocks and final outcomes still use the notify command. When no
kept seat can take the ask, it falls back once to the notify command
(`human_ask_fallback` with `lab_executor`, `no_asking_formation`,
`no_receivable_seat`, `asked_seats_gone` or `delivery_uncertain`).

## Find the asking seat

```bash
archon --server "$ARCHON_SERVER" run gates "$ARCHON_RUN_ID"
archon --server "$ARCHON_SERVER" gate request "$ARCHON_RUN_ID" "$ARCHON_GATE_ID"
archon --server "$ARCHON_SERVER" run seats "$ARCHON_RUN_ID"
```

Match a gate's `askedSeats[].createdSeq` to a seat's `createdSeq`; its
`sessionName` is the tmux session the operator talks to. These three print text,
or the daemon envelope with `--json`.

## Relay a decision from a seat

Only the operator decides. In the seat that received the ask:

- A complete, unambiguous operator verdict for this pending gate, with the exact
  response words, is confirmation. Record those words at once.
- When you drafted or paraphrased the response, or the verdict or gate is
  unclear, show the proposed verdict and exact response together and wait for
  the operator to confirm. Discussion alone is never a verdict.

Record it with your own slot ID as the relay, using the command in your gate
brief:

```bash
archon --server "$ARCHON_SERVER" gate approve "$RUN" "$GATE" --requested-seq "$SEQ" --relayed-by "$SLOT" --response "$RESPONSE"
archon --server "$ARCHON_SERVER" gate reject  "$RUN" "$GATE" --requested-seq "$SEQ" --relayed-by "$SLOT" --response "$RESPONSE"
```

The record keeps `decidedBy: human:operator` and stores your slot in
`relayedBy`. For a long answer the operator names a UTF-8 file; use
`--response-file FILE` in place of `--response`, never both. After a 409
`coordinator is executing` (the run stayed busy for the five seconds the daemon
waits), run the same command again a few seconds later;
after a 409 `human gate request is no longer pending`, tell the operator that
another seat or the cockpit decided first.

Kept seats end when their ask is answered, when their formation starts a new
attempt, or when the run finishes.
