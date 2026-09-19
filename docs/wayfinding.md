# Run Wayfinding

Use the installed `archon` matching the daemon, with `jq` for the selections
below. Replace the example settings with your daemon address and input paths.
Context paths must be absolute existing files or directories on the daemon
host. They supply reference material; the run gets a separate workspace.

```bash
SERVER='http://127.0.0.1:8080'
BOARD='wayfinding'
BRIEF='/absolute/path/to/brief.txt'
CONTEXT_REPO='/absolute/path/to/prior-art'
CONTEXT_NOTES='/absolute/path/to/context.md'
BEAD='your-task-id'
archon --server "$SERVER" board list
archon --server "$SERVER" board inspect "$BOARD" --json
archon --server "$SERVER" mission list "$BOARD"
MISSION='mis_replace_with_the_listed_mission_id'
archon --server "$SERVER" mission inspect "$BOARD" "$MISSION" --json
```

Check the board revision, selected mission, staffing and `humanChannel: session`
before starting. Wayfinding uses 30 dispatches, 30 attempts and 7200 seconds per
run; its peer formation has an authored 600-second timeout. Inspect that setting
in the board JSON. Gate waits do not consume the run wall clock. The CLI fetches
the current board revision for admission; a revision conflict requires inspection
before trying again.

Launch once. `BRIEF` is read by the local CLI. Omit `--cwd` for an automatic
workspace; use `--cwd /absolute/existing/project` when work belongs there.
Repeat `--context-path` for each input, or omit it when none is needed.

```bash
archon --server "$SERVER" mission run "$BOARD" --mission "$MISSION" \
  --brief "$BRIEF" --bead "$BEAD" \
  --context-path "$CONTEXT_REPO" --context-path "$CONTEXT_NOTES" \
  --max-dispatch 30 --max-attempts 30 --wall-clock-seconds 7200 \
  --json > launch.json
RUN=$(jq -er '.data.runId' launch.json)
archon --server "$SERVER" run status "$RUN" --json
archon --server "$SERVER" run follow "$RUN" --json
```

`follow` waits through human gates. Ctrl-C stops viewing, not the run. If launch
returns no receipt, use `archon --server "$SERVER" run list --json` to find an
admitted run before launching again. Status reports the actual `cwd` and
`contextPaths`; the cockpit also shows the workspace path.

Find the pending question and the seats that received it. Choose the intended
`GATE` and its exact `SEQ` from `run gates`; refresh them for each decision.

```bash
archon --server "$SERVER" run gates "$RUN"
archon --server "$SERVER" run gates "$RUN" --json > gates.json
GATE='gate_replace_with_pending_id'
SEQ=$(jq -er --arg gate "$GATE" '.data.waitingGates[] | select(.gateId == $gate) | .requestedSeq' gates.json)
archon --server "$SERVER" gate request "$RUN" "$GATE"
archon --server "$SERVER" run seats "$RUN" --json > seats.json
```

Match `askedSeats[].createdSeq` in `gates.json` to `seats[].createdSeq` in
`seats.json`. Its `sessionName` identifies the asking session. On the daemon
host, use the configured socket to find its pane, then open that session in
CHROTE or your terminal:

```bash
TMUX_SOCKET='/absolute/path/to/configured/tmux.sock'
tmux -S "$TMUX_SOCKET" list-panes -a -F '#{session_name} #{pane_id}'
```

Tell the asking seat a complete verdict and the exact response, for example:
`Approve this pending framing gate. Record exactly: Focus on individual editors.`
For send-back, say `Reject this pending gate` and supply the complete correction.
Discussion alone is not a decision. A complete explicit verdict and exact response
are confirmation; the seat records them without asking again.

For a long answer, save the complete UTF-8 text to a file the asking seat can
read. Tell it: `Approve this pending gate. Record the complete verbatim text of
/absolute/path/to/answer.txt as my response, including whitespace and final
newlines. Use --response-file.` Use `Reject` for send-back. A file path alone does
not decide the gate. Avoid pasting a long answer into the agent input or passing
it through shell command substitution.

If the asking seat is unavailable, record your decision directly. The response
file is read on the machine running this command. Use exactly one command for
the current request; substitute `reject` for `approve` to send work back:

```bash
archon --server "$SERVER" gate approve "$RUN" "$GATE" \
  --requested-seq "$SEQ" --response 'Focus on individual editors.'
# Alternative for a long response:
ANSWER='/absolute/path/to/answer.txt'
archon --server "$SERVER" gate approve "$RUN" "$GATE" \
  --requested-seq "$SEQ" --response-file "$ANSWER"
```

The asking seat adds its own `--relayed-by <slot-id>` when relaying your verdict.
Direct operator commands omit that flag. The cockpit can also load a local UTF-8
response file into its editable answer box, then Approve or Send back. Do not
combine `--response-file` with `--response` or `--reason`.

A brief `run_blocked` with reason/code `resume_after_verdict`, followed by
`run_resumed`, is the expected durable handoff after a human verdict. The
coordinator resumes automatically. A persistent block or a different reason
needs inspection; it is not proof of completion. A busy-coordinator 409 permits
retrying the same decision. A request-no-longer-pending 409 means someone already
answered: refresh the gate list.

At completion, check `final` and `status` and inspect `run seats`. Outputs in the
reported workspace are retained. Declared artifacts live under the daemon state
directory at `.formations/artifacts/<runId>/`; use the cockpit Produced list or
`GET /api/formations/runs/<runId>/evidence/artifacts` to locate them. A send-back
starts a fresh attempt, so the recorded correction must carry the decisions the
next drafting seat needs. Do not rename or freeze the board when finishing a run.

Verification of these steps on a disposable run is pending; this guide does not
claim a completed live proof.
