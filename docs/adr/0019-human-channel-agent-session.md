# Human gates reach the operator by mission channel

Proposed 2026-09-17, pending the operator's decision form-xex on which session
answers, and whether an agent may record a confirmed decision. Operator request
under form-3yd.10: a mission chooses how its human gates reach the operator, and
the operator wants to answer the first real Wayfinding run in an agent's tmux
session instead of by email. Extends the
needs-you notifications of the contract and the live terminals of
[ADR-0017](0017-run-evidence-api.md)'s trust boundary.

Before this decision one daemon flag, `--notify-command`, sent every run's asks
the same way, and every cockpit terminal was view-only.

## Decision

- A mission has `humanChannel`: `notify` (the default, stored as absent) or
  `session`. A run reads it from its frozen board, so a change applies to runs
  started afterwards. The Start mission dialog shows it.
- `notify` keeps today's behaviour: asks go to `--notify-command`, if set.
- `session` sends every needs-you ask of the run (human gate, escalation, block
  and final outcome) to the run's liaison, and never to the notify command. The
  same `.needs-you.json` record keeps each ask delivered once.

## The liaison

- One liaison per run: an interactive agent session the daemon starts on the
  first ask and reuses for later ones, so the conversation keeps its context.
  It runs on the executor's `--socket` as `form-<run>-liaison` (with any
  `--mission-label` prefix), in the run's cwd, launched and readied the way
  seats are. It has no turn to complete and no sentinel.
- Its persona is `operator-liaison`: a built-in Claude Code preset, replaced by
  a card of that ID in the agents directory.
- Each ask is written to `<state-dir>/briefs/liaison-<run>-<seq>.md` and pasted
  as a pointer only while the agent is idle and its input line is empty, so it
  never lands mid-turn or on top of the operator's unsent text. An undelivered
  ask stays unrecorded and is retried.
- The brief carries the ask as a notification would (gate title, criterion,
  the input capped at 64 KiB, the run artifact directory, the decisions already
  recorded in the run) and these rules:
  - Present the ask plainly and help the operator think it through, reading
    the run's artifacts to answer their questions. Offer a view only when
    asked, and label it as the liaison's.
  - Only the operator decides. Draft the response in the operator's words, show
    it with the verdict, and record it only after they confirm, with the exact
    `gate approve` or `gate reject` command given, `--requested-seq` and
    `--relayed-by liaison`.
  - Modify no files, and perform no Git, tmux, agent or service operations.
    Resume or abort the run only when the operator asks, with the command given.
- The daemon records the session in the run artifact `<runId>.liaison.json`:
  the immutable session, pane and socket identity, harness, model, effort,
  creation time, and end time and outcome. It is not a ledger event. The
  liaison is the operator's channel, not run execution, and replay ignores it.
- The liaison ends when the operator ends the conversation, or one hour after
  its run became final. Daemon shutdown leaves it running. Startup verifies
  the recorded identity; a liaison that is gone, or whose tmux server changed,
  is replaced at the next ask.

## Recording who relayed a decision

A verdict may carry `relayedBy`, a short name such as `liaison`. It is stored on
`human_verdict_recorded` and shown with the decision. `decidedBy` stays
`human:operator`: the operator decided, and the record says who typed the
command.

## Routes

- `GET /api/formations/runs/{runId}/liaison` returns the run's channel and the
  liaison's state (`none`, `live`, `ended` or `unavailable`), session name, times,
  end outcome and terminal link. The cockpit learns of changes without a reload.
- `GET /api/formations/runs/{runId}/liaison/terminal` is a `tty` WebSocket that
  accepts input and resize as well as pause and resume. It is interactive
  because the liaison exists to receive the operator's typing.
- `DELETE /api/formations/runs/{runId}/liaison` ends the conversation.

## Consequences

- Seats are unchanged: Peek stays view-only, and operators and agents still
  must not type into seats. The liaison is the one session meant for input.
- Anyone who reaches the cockpit can type to a run's liaison, which runs with
  the harness's permissions. That is the trust CHROTE's own terminals on the
  tailnet already carry, by the operator's decision that the tailnet needs no
  further authentication.
- The liaison sits in the shared tmux pool, so CHROTE shows it too and the
  operator may talk to it there.
- Choosing `session` means no email for that run. An operator away from the
  screen learns of the ask only when they return to the cockpit or CHROTE.
