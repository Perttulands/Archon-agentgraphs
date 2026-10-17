# Human gates on the session channel

Read with the main [Archon skill](../SKILL.md). This covers missions set to
`--human-channel session` and seats that relay an operator's decision.

Read the contract's [session-channel rules](../../../docs/CONTRACT.md#human-gates-on-the-session-channel)
and [human verdicts](../../../docs/CONTRACT.md#human-verdicts) before relaying a
decision (`docs/CONTRACT.md` in the Archon source or
`$ARCHON_SHARE/docs/CONTRACT.md` in an installed release).
Leave notification delivery to the daemon; never run the notify command or
resend a message yourself.

## Find the asking seat

Use `$ARCHON_TMUX_SOCKET` (the daemon's `--socket`) to find the tmux pane.

```bash
archon --server "$ARCHON_SERVER" run gates "$ARCHON_RUN_ID"
archon --server "$ARCHON_SERVER" gate request "$ARCHON_RUN_ID" "$ARCHON_GATE_ID"
archon --server "$ARCHON_SERVER" run seats "$ARCHON_RUN_ID"
tmux -S "$ARCHON_TMUX_SOCKET" list-panes -a -F '#{session_name} #{pane_id}'
```

Match a gate's `askedSeats[].createdSeq` to a seat's `createdSeq`; its
`sessionName` is the tmux session the operator talks to. The `archon` commands print text,
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

For a long answer supplied as a UTF-8 file, use
`--response-file FILE` in place of `--response`, never both. After a 503 while
the daemon restarts, run the same command again a few seconds later; after a
409 `human gate request is no longer pending`, tell the operator that another
seat or the cockpit decided first.
