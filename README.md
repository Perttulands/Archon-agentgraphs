![Archon](docs/images/archon-header.png)

# Archon

A workbench for teams of coding agents. Draw the work, assign Claude Code and
Codex agents, and decide where a review or human approval must happen before
the next step runs.

Archon combines a visual board editor, a CLI and a local coordinator. Agents
work in tmux sessions on your machine. The UI shows their assignments and
terminal output; the coordinator routes results through the graph and keeps
the run history on disk.

![Delivery board with agent assignments, a review gate and attached notes](docs/images/workflow.png)

The included delivery board plans a change, drafts Beads, reviews the proposed
work, then hands execution to a Claude controller with three Codex workers.
A final reviewer checks the result. A failed Beads review sends feedback back
to the drafting step.

## Work you can inspect

- Use a solo agent, peer agents, or a controller with assigned workers.
- Give each step its own brief, input and output ports, and agent settings.
- Add code checks, agent reviews and human approval gates. Route failures back
  for another attempt, within the run's limits.
- Keep several missions running with separate briefs, working directories,
  cancellation and histories.
- Open a floating terminal Peek to select, copy and scroll a seat's output.
  Peek is view-only and preserves the agent's terminal size.

![Floating terminal Peek with controller and worker tabs](docs/images/terminal-peek.png)

This capture uses a real tmux shell running CLI help. It is labelled as a demo;
no model is running in it.

The Agents view keeps reusable personas beside the mission's staffing. Inspect
a persona's settings and see which slots use it before starting work.

![Agents view with delivery personas, staffed execution slots and the controller inspector](docs/images/agents.png)

## What ships together

The CLI, backend and UI live in this repository. The current build produces
two Go binaries and a static UI directory. The daemon serves the UI directly,
so production use does not need a Node server. CHROTE is not required.

| Part | Role |
| --- | --- |
| `archon` | Author boards locally and send runtime commands to the daemon. |
| `formationsd` | Run missions, manage agent seats, persist events and serve HTTP. |
| `dashboard/dist/` | The browser UI served by the daemon. |

Archon was previously called Formations. The daemon name, some UI labels,
storage paths and API names still use that name. This is currently a source
build, with no unified release installer.

## Try the UI

Build on Linux with Go 1.26.6 or newer and Node 20.19+ in the 20.x line, or
Node 22.12+. The lab executor lets you explore boards without launching agents.

```bash
git clone https://github.com/Perttulands/chrote-agent-formations.git archon
cd archon

export ARCHON_HOME="${XDG_DATA_HOME:-$HOME/.local/share}/archon"
umask 077
mkdir -p "$ARCHON_HOME/bin"
(cd src && go build -o "$ARCHON_HOME/bin/archon" ./cmd/archon \
  && go build -o "$ARCHON_HOME/bin/formationsd" ./cmd/formationsd)
(cd dashboard && npm ci && npm run build)

mkdir -p "$ARCHON_HOME/state/.formations/boards" \
  "$ARCHON_HOME/state/.formations/notes"
cp examples/delivery.formation.toml "$ARCHON_HOME/state/.formations/boards/"
cp examples/delivery.notes.toml "$ARCHON_HOME/state/.formations/notes/"
"$ARCHON_HOME/bin/archon" --workspace "$ARCHON_HOME/state" board validate delivery --json
"$ARCHON_HOME/bin/archon" --workspace "$ARCHON_HOME/state" board arrange delivery --json

"$ARCHON_HOME/bin/formationsd" --executor lab \
  --state-dir "$ARCHON_HOME/state" --ui-dir "$PWD/dashboard/dist" \
  --listen 127.0.0.1:8091
```

Open **http://127.0.0.1:8091**. Leave the daemon running in this terminal and
stop it with Ctrl+C when finished. Lab simulates execution; it does not perform
the work in a brief. The service has no authentication, so keep its listeners
on trusted interfaces.

## Run real agents

Install tmux and the Claude Code or Codex CLI required by your chosen personas,
and authenticate those CLIs. Start the daemon with `--executor tmux` and your
absolute paths for `--socket`, `--tmux-bin`, `--codex-transcripts` and
`--claude-transcripts`. The daemon creates seats on demand when a formation
runs. See the [operator procedure](docs/CONTRACT.md#operator-procedure) for
configuration, execution limits, approvals and recovery.

The delivery example also expects Beads and the shared skills named in its
briefs. Those tools and skills are not bundled here. Read and adapt the
[board](examples/delivery.formation.toml) and its
[notes](examples/delivery.notes.toml) before running it against a repository.
The [minimal board](docs/CONTRACT.md#definitions-and-storage) is a smaller
starting point for your own workflow.

With a configured daemon and a prepared delivery board, submit a mission from
another terminal. Replace the working directory, brief and Bead below with
your task's values.

```bash
"$ARCHON_HOME/bin/archon" --server http://127.0.0.1:8091 mission run delivery \
  --mission mis_delivery --cwd /absolute/path/to/your/repository \
  --brief /absolute/path/to/your/brief.md --bead your-project-123 \
  --max-dispatch 30 --max-attempts 3 --wall-clock-seconds 7200 --json
```

Use the returned run ID with `run status`, `run logs`, `run follow` or
`run abort`. Runtime commands always use `--server`; local authoring uses
`--workspace`. A run keeps a snapshot of its board and personas, so later
edits apply to later runs. Recovery records unresolved work explicitly;
inspect a blocked run before deciding how to continue it.

## Develop

```bash
(cd src && go test ./...)
(cd dashboard && npm run test:unit && npm run build && npm run lint)
```

For Vite development, set `FORMATIONS_API_URL` to the daemon URL and run
`npm run dev` in `dashboard/`.

| Source | Owns |
| --- | --- |
| `src/internal/formations/` | Model, persistence, gates and execution. |
| `src/internal/coordinator/` | Admission, runtime commands and projections. |
| `src/internal/api/` | Authoring HTTP and local adapters. |
| `src/cmd/archon/`, `src/cmd/formationsd/` | CLI and daemon entrypoints. |
| `dashboard/` | Board editor, agent staffing and terminal Peek. |

Read the [runtime contract](docs/CONTRACT.md),
[daily-capability decisions](docs/adr/0016-daily-capability.md) and
[OpenAPI specification](docs/openapi/formations.yaml) before changing runtime
behavior. Host deployment and CHROTE integration live outside this repository.
Historical designs remain in [the archive](docs/archive/).

[MIT license](LICENSE). [Image sources and capture notes](docs/images/README.md).
