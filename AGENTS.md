# Archon

Archon builds and runs agent work graphs through the `archond` coordinator,
`archon` CLI and Archon UI. Its area docs are the one statement of each area's
vision and goals; read the one you are changing and link to it rather than
restating it:

- [docs/PRODUCT.md](docs/PRODUCT.md): what Archon is for, its philosophy
  and its goals.
- [docs/CONTRACT.md](docs/CONTRACT.md): the running contract. Read it before
  changing runtime semantics, authoring, gates or operator instructions.
- [docs/DESIGN.md](docs/DESIGN.md): the cockpit's design language.

Three rules hold for every change; PRODUCT.md gives the reasons:

- Add no sandboxing, file confinement, blast-radius limits or authority checks.
- Land renames and reshapes whole: migrate live data once, delete the old path,
  and add no deprecated aliases or compatibility shims.
- Put no eval harness, tests or CI around the `archon` skill.

`docs/adr/` records the rationale of past decisions, `examples/` holds runnable
missions, and `docs/archive/` and `Perttus_vision_for_agent_orchestration/`
are history, not instructions.

Do not settle product direction (scope, the trust or visibility model,
supported formation types, UX direction) inside the work; raise it as a
`--type decision` Bead labelled `product-decision`, with options, evidence and a recommendation. Record
platform shortcomings found during work as Beads labelled `platform-gap`,
linked `discovered-from` the work that revealed them.

## Project map

- `src/internal/formations/` owns the model, persistence and run engine.
- `src/internal/coordinator/` owns admission, runtime commands and projections.
- `src/internal/api/` owns authoring HTTP and local adapters.
- `src/internal/daemon/` owns `archond` flags, executor wiring and startup.
  `src/cmd/archond/` only calls it.
- `src/cmd/archon/` owns the CLI.
- `dashboard/` owns the Archon mission editor and Agents view.

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
Changes to dashboard UI, or to API shapes that `dashboard/tests/cockpit-fixture.ts`
mimics, also run the browser suite (`npm run test:browser` with
`ARCHON_BROWSER_PORT` set to a free loopback port) and update that fixture.
Type-check through `npm run build`; `npx tsc -b` writes build output into
`dashboard/` and breaks lint. CI has no `claude` or `codex` binary, so tests
that resolve a harness can pass here and fail there; before pushing Go changes
also run `env PATH=/usr/bin:/bin:$(dirname "$(command -v go)") go test ./...`
from `src/`.
Use the checks relevant to the affected contract. Current integration ownership
belongs to the owning Bead or lane brief.
