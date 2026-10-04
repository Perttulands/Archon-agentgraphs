# Archon

Archon builds and runs agent work graphs through the `archond` coordinator,
`archon` CLI and Archon UI. Read [docs/CONTRACT.md](docs/CONTRACT.md) before changing
runtime semantics, authoring, gates or operator instructions. Read
[ADR-0016](docs/adr/0016-daily-capability.md) for daily-capability decisions;
`docs/adr/` holds earlier decisions and `examples/` holds runnable templates.
`docs/archive/` preserves historical targets, not the running contract.

## Product philosophy

Archon makes chaining agents and gates easy and great; that is all it does
([ADR-0021](docs/adr/0021-archon-only-chains-agents-and-gates.md)).

- No security rituals: sessions are tmux sessions with full access, as in
  CHROTE. Never add sandboxing, file confinement, blast-radius limits or
  authority checks; safety lives in the agents' own configuration.
- No legacy and no backwards compatibility: keep only what is current. Land
  renames and reshapes whole, migrate live data once, and delete the old path.
  Add no deprecated aliases or compatibility shims.
- The `archon` skill is a sensible current version, iterated later, with no
  eval harness, tests or CI around it.

## Project map

- `src/internal/formations/` owns the model, persistence and run engine.
- `src/internal/coordinator/` owns admission, runtime commands and projections.
- `src/internal/api/` owns authoring HTTP and local adapters.
- `src/internal/daemon/` owns `archond` flags, executor wiring and startup.
  `src/cmd/archond/` only calls it.
- `src/cmd/archon/` owns the CLI.
- `dashboard/` owns the Archon mission editor and Agents view.
- `Perttus_vision_for_agent_orchestration/` retains vision and canvas references.

## Reuse CHROTE's proven code

CHROTE has working, hard-won implementations for file browsing and viewing,
browser terminals, and tmux integration. When changing these areas, inspect the CHROTE repository's code and tests as
starting points. Adapt what fits Archon's contracts; justify a different design
when it is simpler or better suited to the intended outcome.

Start with these paths in the CHROTE repository:

- Files: `dashboard/src/components/FilesView/`,
  `dashboard/src/components/FileViewer.tsx`, and `src/internal/api/files*.go`.
- Browser terminals and tmux: `dashboard/src/terminal/` and `src/internal/proxy/`.

Preserve the relevant behavior and regression coverage while adapting to
Archon's contracts. Keep source attribution on ported code and explain any
necessary departure from the CHROTE implementation.

## Work state

Use this repository's `archon-` Beads store. Host deployment and forwarding live outside this
repository; CHROTE integration and SRV deployment require Beads in their owning
stores. Keep tracked files host-neutral. Supply host paths, sockets, users and
listen addresses through flags or environment, with placeholders in examples.

## Verification

For runtime and CLI changes, `go test ./...` from `src/` exercises Go behavior;
`go build ./cmd/archon` and `go build ./cmd/archond` check the executable targets.
For dashboard changes, its `package.json` exposes unit tests, build and lint.
Use the checks relevant to the affected contract. Current integration ownership
belongs to the owning Bead or lane brief.
