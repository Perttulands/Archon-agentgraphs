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
Use `--agents-dir "$FORMATIONS_AGENTS_DIR"` to read persona cards from an
absolute operator-selected directory. By default cards live in
`<state-dir>/agents`; built-in presets remain available in either case.
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

## Delivery template

Import the board and its operator notes into the definition workspace:

```sh
mkdir -p "$FORMATIONS_STATE_DIR/.formations/boards" "$FORMATIONS_STATE_DIR/.formations/notes"
cp examples/delivery.formation.toml "$FORMATIONS_STATE_DIR/.formations/boards/"
cp examples/delivery.notes.toml "$FORMATIONS_STATE_DIR/.formations/notes/"
archon --workspace "$FORMATIONS_STATE_DIR" board validate delivery
archon --workspace "$FORMATIONS_STATE_DIR" board arrange delivery
```

The `delivery-*` presets staff Plan, Beads, the Beads review judge,
Execution and Final review. The gate pushes a failed draft back to Beads;
Execution uses a Claude controller and three Codex workers. The final reviewer
uses `gpt-6-astra` and writes a report without another gate. Each handoff retains
the plan path. All delivery presets use medium effort; the other models use
their harness defaults.

The run must supply the target repository, brief and mission Bead as prompt
context. This example defines no host paths or run-input fields. Inspect the
personas, briefs and run limits before use, allowing at least two attempts for
gate pushback. Lab execution proves routing with simulated seats; it does not
perform a real delivery or a real Beads review.
>>>>>>> main
