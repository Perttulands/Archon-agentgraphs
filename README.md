# CHROTE Agent Formations

This repository is a history-preserving extraction of CHROTE's unreleased
Formations, Archon, Agents, and Oracle experiments. Formations now has an
experimental standalone coordinator for trusted local missions. It is not a
supported CHROTE feature or deployed production service.

The coordinator exposes a loopback HTTP contract. See
[ADR-0015](docs/adr/0015-standalone-trusted-coordinator.md) and the
[OpenAPI sketch](docs/openapi/formations.yaml). Deployment and CHROTE integration
belong to separately authorized work.

## Contents

- `src/internal/formations`: the Formations model, persistence, and run engine.
- `src/internal/api`: the extracted HTTP handlers and small local adapters.
- `src/cmd/archon`: the experimental Archon CLI.
- `dashboard`: the Formations and Agents cockpit with its unit tests.
- `Perttus_vision_for_agent_orchestration`: the approved vision packet.

## Verify

```sh
cd src
go test ./...
go build ./cmd/archon

cd ../dashboard
npm ci
npm run test:unit
npm run build
npm run lint
```

The transcript files under `src/internal/formations/testdata` are synthetic
fixtures. The public history was filtered and scanned before publication.

## Serve the cockpit

Build the dashboard, then start the lab daemon:

```sh
formationsd --state-dir "$FORMATIONS_STATE_DIR" \
  --listen "$FORMATIONS_LISTEN" --ui-dir "$FORMATIONS_UI_DIR" --executor lab
```
The state and UI directories are supplied by the operator. Repeat `--listen`
for each trusted interface. An empty `--ui-dir` disables static serving.
For Vite development, set `FORMATIONS_API_URL` to this daemon's HTTP URL.

The default executor is `tmux`; it also requires `--socket`, `--tmux-bin`,
`--codex-transcripts`, and `--claude-transcripts`. Lab execution uses no terminal
sessions. Mission runs supply their own working directory. `--cwd` on the daemon
is an optional fallback for isolated formation runs.
This service has no authentication; each configured listener must be inside the
operator's trusted network boundary.

The cockpit and `archon --workspace "$FORMATIONS_STATE_DIR"` share definitions.
Runtime HTTP commands use `archon --server "$FORMATIONS_URL"`. Both HTTP clients
consume `{success,timestamp,data}` responses. Mission and isolated formation
starts require explicit positive limits and return a durable HTTP 202 receipt.
The coordinator owns dispatch, continuation, cancellation and the run projection.

Many runs can execute concurrently in one daemon. Start a mission with:

```sh
archon --server "$FORMATIONS_URL" mission run "$BOARD" --mission "$MISSION_ID" \
  --cwd "$TARGET_DIRECTORY" --brief "$BRIEF_FILE_OR_TEXT" --bead "$BEAD_ID"
```

The cwd must be an absolute existing directory. The brief supplies the mission
output; the board's goal remains context. Bead is optional. The cockpit collects
these inputs and execution limits in its Start mission dialog. Each seat name
includes its run ID; `--mission-label` adds an optional prefix.

`archon --server "$FORMATIONS_URL" run abort "$RUN_ID" --reason "$REASON"`
cancels only that run and waits for owned-seat cleanup before reporting canceled.
A new daemon inspects non-final ledgers before listening. It recovers a completed
single-slot dispatch only when its private brief and recorded native session
match; otherwise it leaves a visible resumable block naming open dispatches in
the private ledger. Existing non-resumable blocks stay blocked. Recovery never
adopts or cleans up sessions created by the previous daemon.
