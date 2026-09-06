# Standalone operations

`formationsd` is the experimental trusted local coordinator described in
[ADR-0015](adr/0015-standalone-trusted-coordinator.md). Its HTTP contract is
[standalone-trusted-v1](openapi/formations.yaml), using the schema-1 engine.
Deployment and CHROTE integration require separate work. The listener has no
authentication beyond requiring a literal loopback IP. Seats run Codex with
approvals and sandboxing bypassed, so use only trusted missions and operators.

Use Linux, the Go toolchain required by `src/go.mod`, Bash, `curl`, and `jq`.
The existing tmux server must have access to an installed, authenticated `codex`
and the requested model. Set these shell variables from host configuration in
each terminal used below. Keep their actual values out of source documents.

| Variable | Required value |
| --- | --- |
| `FORM_SOURCE` | Absolute path to this checkout, also the agents' working directory. |
| `FORM_STATE` | New private absolute directory outside the checkout for this coordinator. |
| `FORM_BIN` | Absolute directory for the two compiled binaries. |
| `FORM_SOCKET` | Absolute path to the operator's existing tmux socket. |
| `FORM_TMUX_BIN` | Absolute path to the guarded tmux wrapper. |
| `FORM_TRANSCRIPTS` | Existing absolute native sessions directory receiving these Codex transcripts. |
| `FORM_LISTEN` | Available literal loopback IP and port, in `IP:port` form. |
| `FORM_MISSION_LABEL` | Unique simple session-name component for this mission. |

`FORM_STATE` is both the offline Archon definition workspace and the private
runtime store. It holds `.formations/boards`, ledgers and snapshots under
`.formations/runs`, routed artifacts under `.formations/artifacts`, and dispatched
files under `briefs`. Optional persona overrides belong in `FORM_STATE/agents`.
The example's `codex-builder` and `codex-reviewer` personas are built in.
Agents edit the source checkout through `--cwd`; `--workspace` points Archon at
the private definitions. Do not put runtime state in the checkout.

Build both programs, then import the
[operations example](../examples/operations-proof.formation.toml) into a new
state directory. Archon has no `board import` command. Stop on any command error.

```bash
umask 077
mkdir -p "$FORM_BIN"
cd "$FORM_SOURCE/src"
go build -o "$FORM_BIN/archon" ./cmd/archon
go build -o "$FORM_BIN/formationsd" ./cmd/formationsd

mkdir -m 700 "$FORM_STATE"
mkdir -p "$FORM_STATE/.formations/boards"
install -m 600 "$FORM_SOURCE/examples/operations-proof.formation.toml" \
  "$FORM_STATE/.formations/boards/operations-proof.formation.toml"
"$FORM_BIN/archon" --workspace "$FORM_STATE" \
  board validate operations-proof --json
```

This example owns `form-2fb` work. It runs producer, reviewer, and integrator in
sequence to write and check this guide, then waits at `gate_operator`. Inspect
the example and the owning Bead before running it. Importing or validating it
does not start seats. Definition changes after admission do not change the run's
snapshots.

Launch the coordinator in a terminal kept open for its lifetime. The configured
socket must already exist and have the expected operator ownership. The adapter
will not start a tmux server. This configuration selects Astra at xhigh for
every seat, with a 30-minute limit per seat.

```bash
"$FORM_BIN/formationsd" \
  --listen "$FORM_LISTEN" \
  --state-dir "$FORM_STATE" \
  --cwd "$FORM_SOURCE" \
  --socket "$FORM_SOCKET" \
  --tmux-bin "$FORM_TMUX_BIN" \
  --transcripts "$FORM_TRANSCRIPTS" \
  --mission-label "$FORM_MISSION_LABEL" \
  --model gpt-6-astra --effort xhigh --seat-timeout 30m
```

In another terminal, use HTTP for all run operations. `--server` must precede
the command and contain an HTTP URL with a literal loopback IP, without a
trailing slash, credentials, query, or fragment. A failed remote command never
falls back to the local engine.

```bash
FORM_SERVER="http://$FORM_LISTEN"
curl --noproxy '*' --fail --silent --show-error "$FORM_SERVER/healthz"
"$FORM_BIN/archon" --server "$FORM_SERVER" board inspect operations-proof --json
"$FORM_BIN/archon" --server "$FORM_SERVER" run list --json
```

One coordinator locks each state directory and admits only one non-final run.
If the list already contains the intended mission, take its `runId` as
`FORM_RUN_ID` and go straight to status. Otherwise, start the mission once.
Use the board slug `operations-proof` and mission ID `mis_operations`.

```bash
FORM_START=$("$FORM_BIN/archon" --server "$FORM_SERVER" \
  mission run operations-proof --mission mis_operations \
  --max-dispatch 3 --max-attempts 1 --wall-clock-seconds 7200 --json)
FORM_RUN_ID=$(printf '%s\n' "$FORM_START" | jq -er '.runId')
"$FORM_BIN/archon" --server "$FORM_SERVER" run status "$FORM_RUN_ID" --json
"$FORM_BIN/archon" --server "$FORM_SERVER" run follow "$FORM_RUN_ID"
```

The start command reads the board revision and submits it for admission. HTTP
202 means the initial event is durable and the worker owns execution. Closing
the client does not cancel the run. If the receipt is lost, inspect `run list`
before attempting another start. HTTP 409 can mean another run owns admission,
the worker is busy, or the board revision changed.

`run follow` prints complete JSON projections on durable changes through SSE.
It stays open at a human gate and closes at finality. Use another terminal for
the verdict, or interrupt only the follow client. `run logs` returns the same
sanitized projection as `run status`, not raw transcripts. Runtime projections
include progress and cleanup events, but omit prompts, artifact contents,
runtime paths, and native session IDs. Inspect the guide in the local checkout
and any private mission evidence locally; HTTP offers no generic file reader.

At the operator gate, first read a fresh status and list its pending requests.
Do not infer a request sequence from `eventCount` or copy one from an older run.

```bash
FORM_STATUS=$("$FORM_BIN/archon" --server "$FORM_SERVER" \
  run status "$FORM_RUN_ID" --json)
printf '%s\n' "$FORM_STATUS" | jq '{runId, status, final, waitingGates}'
FORM_GATE_ID=gate_operator
FORM_REQUESTED_SEQ=$(printf '%s\n' "$FORM_STATUS" | jq -er \
  --arg gate "$FORM_GATE_ID" \
  '.waitingGates[] | select(.gateId == $gate) | .requestedSeq')
```

Proceed only if status is `waiting_human`, `final` is false, and the selected
gate has a positive `requestedSeq`. An empty result means this gate is not
pending. Read `docs/standalone-operations.md` and the producer-reviewer-integrator
evidence. The gate asks whether the guide is usable and this handoff is worth
keeping. The human operator chooses the verdict and sets `FORM_REASON` to their
reason. Run exactly one of the following commands.

Approve with `pass`:

```bash
"$FORM_BIN/archon" --server "$FORM_SERVER" \
  gate approve "$FORM_RUN_ID" "$FORM_GATE_ID" \
  --requested-seq "$FORM_REQUESTED_SEQ" --reason "$FORM_REASON" --json
```

Reject with `fail`:

```bash
"$FORM_BIN/archon" --server "$FORM_SERVER" \
  gate reject "$FORM_RUN_ID" "$FORM_GATE_ID" \
  --requested-seq "$FORM_REQUESTED_SEQ" --reason "$FORM_REASON" --json
```

There is no default verdict. The server matches the run, gate, and exact pending
request. Duplicate or stale decisions return HTTP 409. On a conflict or lost
response, read status again before deciding whether a request is still pending.
HTTP 202 records the verdict and lets the coordinator continue the same run;
follow or read status to see the outcome. Approval should finish this example
as `succeeded` with `final: true`. Rejection has no wired FAIL route, so it leaves
a visible `blocked`, non-final run with no pending human request. It still holds
admission; submitting another verdict cannot reverse the recorded choice.

Each seat creates a new session named `form-<mission-label>-<slot-id>` on the
configured socket. Names are sanitized; a collision fails instead of adopting
an existing session. Completion requires the exact brief-pointer user message
in the configured workspace, matching model and effort, and that turn's final
response followed by native `task_complete` evidence. A completion marker alone
is insufficient. Control events and filesystem notifications drive observation.

Cleanup targets only the immutable session ID returned by creation. Inspect
`seat_created` and `seat_cleanup` events for every seat. Cleanup reports `ended`,
`left_socket_changed`, or `left_cleanup_failed`; a leftover requires operator
inspection. Never kill another session or use `kill-server`. If creation did
not return a usable identity, inspect the reported session before cleanup.

SIGINT or SIGTERM stops HTTP admission; coordinator shutdown waits for admitted
execution and its cleanup before releasing the state lock. The HTTP shutdown
timeout is not a promise that the whole process exits in five seconds. Stopping
the daemon is not an active-run cancel command. Keep it running while collecting
the human verdict.

After a restart, the same state directory preserves inspectable history and
pending gates can still receive an exact verdict. The coordinator never
automatically resends an unresolved dispatch or reattaches an old seat. There
is no remote `run resume`, `run abort`, or general recovery API, even when a
projection says `resumeAllowed`. A blocked or interrupted non-final run needs
owner-directed recovery and continues to prevent new admission. Preserve its
state and evidence; do not bypass the coordinator with local runtime commands.
