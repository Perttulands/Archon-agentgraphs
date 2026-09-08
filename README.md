# Formations

Formations builds and runs work graphs with agents and gates. Operators draft
boards in the cockpit, agents author them with ARCHON, and `formationsd` runs
missions through Claude Code and OpenAI Codex seats. Many missions can run
concurrently with separate inputs, cancellation and durable recovery evidence.

Read the [current contract](docs/CONTRACT.md) for definitions, output formats,
operator commands and recovery. [ADR-0016](docs/adr/0016-daily-capability.md)
records the daily-capability decisions. [OpenAPI](docs/openapi/formations.yaml)
lists the HTTP routes. Historical target specs live in [docs/archive](docs/archive/).
The vision interview, locked decisions and canvas prototypes remain in
`Perttus_vision_for_agent_orchestration/`.

## Build and verify

```bash
cd src
go test ./...
go build ./cmd/archon
go build ./cmd/formationsd
cd ../dashboard
npm ci
npm run test:unit
npm run build
npm run lint
```

## Run the cockpit

Set host values from the operator runbook and use the built daemon:

```bash
formationsd --executor lab --state-dir "$FORM_STATE" \
  --listen "$FORM_LISTEN" --ui-dir "$FORM_UI_DIR"
```

Repeat `--listen` for each trusted interface. Omit `--ui-dir` to disable static
serving. The default executor is `tmux`, with required `--socket`, `--tmux-bin`,
`--codex-transcripts` and `--claude-transcripts`. `--cwd` is an optional daemon
default; missions supply their own cwd. `--agents-dir` selects an absolute persona
directory, defaulting to `<state-dir>/agents`, while retaining built-in presets.
Set model and effort on persona variants, not daemon flags. Lab creates no real
seats and does not perform agent work.

The service has no authentication. Host deployment, guarded cleanup environment,
network forwarding and CHROTE integration live outside this repository. Configure
trusted listeners. For Vite development, set `FORMATIONS_API_URL` to the daemon.

## Author and deliver

The cockpit and `archon --workspace "$FORM_STATE"` share definitions. Runtime
commands use `archon --server "$FORM_SERVER"`; they do not fall back locally.
See the [operator procedure](docs/CONTRACT.md#operator-procedure) to import, inspect,
validate and arrange a board, run with cwd/brief/Bead and limits, follow progress,
answer a human gate, abort or recover.

[delivery.formation.toml](examples/delivery.formation.toml) and its
[notes](examples/delivery.notes.toml) define Plan -> Beads -> Beads review gate ->
orchestrated Execution -> Final review. Six delivery presets staff the graph.
The review gate pushes failed drafts back to Beads. Execution uses a Claude
controller with three Codex workers, and Astra writes the final review report.
There is no human gate in this template. Inspect its briefs and owning task
before running it; lab acceptance is simulated routing evidence.

## Source map

- `src/internal/formations/`: model, persistence, gates and execution.
- `src/internal/coordinator/`: service ownership, admission and projection.
- `src/internal/api/`: authoring handlers and local adapters.
- `src/cmd/archon/` and `src/cmd/formationsd/`: command entrypoints.
- `dashboard/`: Formations and Agents cockpit.

The transcript files under `src/internal/formations/testdata` are synthetic
fixtures. The repository preserves the history of the extracted experiments.
