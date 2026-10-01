![Archon](docs/images/archon-header.png)

# Archon

A workbench for teams of coding agents. Draw the work, assign Claude Code and
Codex agents, and decide where a review or human approval must happen before
the next step runs.

Archon combines a visual mission editor, a CLI and a local coordinator. Agents
work in tmux sessions on your machine. The UI shows their assignments and
terminal output; the coordinator routes results through the graph and keeps
the run history on disk.

## What Archon is

Archon makes chaining agents and gates easy and great. That is all it does.

- Agents run in tmux sessions, just like CHROTE's, with full access to
  everything. Archon adds no sandboxing, no file confinement and no security
  rituals; safety lives in the configuration of the agents it runs.
- Archon keeps only what is current: no legacy paths, no backwards
  compatibility, no deprecated aliases.

See [ADR-0021](docs/adr/0021-archon-only-chains-agents-and-gates.md).

![Delivery mission with agent assignments, a review gate and attached notes](docs/images/workflow.png)

The included delivery mission plans a change, drafts Beads, reviews the proposed
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
- Click a mission, formation or gate to open it in a floating window. Read its
  full text, staffing and routes, and edit any field in place, with undo.
- Open a floating terminal Peek to talk with a seat's agent, even while it
  works, and to select, copy and scroll its output. The terminal follows the
  window's size. Open one per formation, then move, resize and stack the
  windows over the canvas.

![Floating terminal Peek with controller and worker tabs](docs/images/terminal-peek.png)

This capture uses a real tmux shell running CLI help. It is labelled as a demo;
no model is running in it.

The Agents view keeps reusable personas beside the mission's staffing. Inspect
and edit a persona and see which slots use it. It shows the mission's current
run read-only and links to Missions, where runs start and gates are answered.

![Agents view with delivery personas, staffed execution slots and the controller inspector](docs/images/agents.png)

## Install

The Linux release includes the CLI, daemon, UI, examples and documentation in
one archive. Choose `amd64` for x86-64 machines or `arm64` for ARM64, then
download the archive and `SHA256SUMS` from the
[latest release](https://github.com/Perttulands/Archon-agentgraphs/releases/latest).
No Go, Node or frontend server is needed to use the release.

For the x86-64 release:

```bash
sha256sum --ignore-missing --check SHA256SUMS
tar -xzf archon-0.1.0-linux-amd64.tar.gz
cd archon-0.1.0-linux-amd64
./install.sh --prefix "$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
archon --version
archond --version
```

The installer defaults to `$HOME/.local`. Use `--prefix /absolute/path` to
choose another location. It installs `bin/archon` and `bin/archond`, with complete releases under
`lib/archon/releases/` and a `lib/archon/current` link.
The daemon finds the installed UI automatically. Keep runtime state in a
separate directory so replacing a release leaves missions and history intact.

| Part | Role |
| --- | --- |
| `archon` | Author missions locally and send runtime commands to the daemon. |
| `archond` | Run missions, manage agent seats, persist events and serve the UI. |
| `share/archon/ui/` | Browser UI included in the release. |

A formation is a step: the team of agents that does it. CHROTE is not required.

## Try the UI

After installing to `$HOME/.local`, import the delivery example and start a
lab daemon. Lab lets you explore missions without launching agents.

```bash
export ARCHON_STATE="${XDG_DATA_HOME:-$HOME/.local/share}/archon/state"
export ARCHON_SHARE="$HOME/.local/lib/archon/current/share/archon"
umask 077
mkdir -p "$ARCHON_STATE/.archon/missions" "$ARCHON_STATE/.archon/notes"
cp "$ARCHON_SHARE/examples/delivery.mission.toml" "$ARCHON_STATE/.archon/missions/"
cp "$ARCHON_SHARE/examples/delivery.notes.toml" "$ARCHON_STATE/.archon/notes/"
archon --workspace "$ARCHON_STATE" mission validate delivery --json
archon --workspace "$ARCHON_STATE" mission arrange delivery --json
archond --executor lab --state-dir "$ARCHON_STATE" --listen 127.0.0.1:8091
```

For a custom install prefix, set `ARCHON_SHARE` to its
`lib/archon/current/share/archon` directory
and add its `bin` directory to `PATH`.

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
briefs. Those tools and skills are not bundled here; only Archon's own skill is. Read and adapt the
[mission](examples/delivery.mission.toml) and its
[notes](examples/delivery.notes.toml) before running it against a repository.
The [minimal mission](docs/CONTRACT.md#definitions-and-storage) is a smaller
starting point for your own workflow.

With a configured daemon and a prepared delivery mission, start a run from
another terminal. `archon --server http://127.0.0.1:8091 mission input delivery`
lists the inputs a run supplies; Delivery takes one, `change`. Replace the
working directory, change and Bead below with your task's values.

```bash
archon --server http://127.0.0.1:8091 mission run delivery \
  --cwd /absolute/path/to/your/repository \
  --input-file change=/absolute/path/to/your/change.md --bead your-project-123 --json
```

Use the returned run ID with `run status`, `run logs`, `run follow`,
`run wait` or `run abort`. An agent driving the run leaves `run wait` running
in the background: it returns when the run needs an answer, ends or changes,
and prints the command that answers it. Runtime commands always use `--server`; local authoring uses
`--workspace`, and there is no default workspace. A run keeps a snapshot of its mission and personas, so later
edits apply to later runs. Recovery records unresolved work explicitly;
inspect a blocked run before deciding how to continue it.

## Let agents drive Archon

The release ships the `archon` agent skill, which teaches Claude Code and Codex
agents to author missions, run them and answer gates. Link it where each harness
discovers user-level skills. The loop skips a directory that already provides
an `archon` skill, such as a shared skills catalog, and never replaces an
existing path:

```bash
export ARCHON_SHARE="$HOME/.local/lib/archon/current/share/archon"
for dir in "$HOME/.claude/skills" "$HOME/.agents/skills"; do
  mkdir -p "$dir"
  if [ -e "$dir/archon/SKILL.md" ]; then
    echo "$dir/archon already provides the archon skill; left unchanged"
  else
    ln -sn "$ARCHON_SHARE/skills/archon" "$dir/archon"
  fi
done
```

The links follow `current`, so an upgrade updates the skill with the binaries.
In a source checkout the skill is [skills/archon](skills/archon/SKILL.md).

## Build from source

Build on Linux with Go 1.26.6 or newer and Node 20.19+ in the 20.x line, or
Node 22.12+. The build script creates both Linux architectures by default;
select one with `--arch amd64` or `--arch arm64`.

```bash
git clone https://github.com/Perttulands/Archon-agentgraphs.git archon
cd archon
./scripts/build-release.sh --out "$PWD/release" --arch amd64
```

Install the resulting archive as above. See [release packaging](docs/releasing.md)
for the archive layout, checks and publication procedure.

## Develop

```bash
(cd src && go test ./...)
(cd dashboard && npm run test:unit && npm run build && npm run lint)
```

For Vite development, set `ARCHON_API_URL` to the daemon URL and run
`npm run dev` in `dashboard/`.

| Source | Owns |
| --- | --- |
| `src/internal/formations/` | Model, persistence, gates and execution. |
| `src/internal/coordinator/` | Admission, runtime commands and projections. |
| `src/internal/api/` | Authoring HTTP and local adapters. |
| `src/internal/daemon/` | `archond` flags, executor wiring and startup. |
| `src/cmd/archon/`, `src/cmd/archond/` | CLI and daemon entrypoints. |
| `dashboard/` | Mission editor, agent staffing and terminal Peek. |

Read the [runtime contract](docs/CONTRACT.md),
[daily-capability decisions](docs/adr/0016-daily-capability.md) and
[OpenAPI specification](docs/openapi/archon.yaml) before changing runtime
behavior. Host deployment and CHROTE integration live outside this repository.
Historical designs remain in [the archive](docs/archive/).

[MIT license](LICENSE). [Image sources and capture notes](docs/images/README.md).
