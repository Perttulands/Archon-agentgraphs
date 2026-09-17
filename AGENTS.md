# Archon

Archon builds and runs agent work graphs through the `archond` coordinator,
`archon` CLI and Archon UI. Read [docs/CONTRACT.md](docs/CONTRACT.md) before changing
runtime semantics, authoring, gates or operator instructions. Read
[ADR-0016](docs/adr/0016-daily-capability.md) for daily-capability decisions;
`docs/adr/` holds earlier decisions and `examples/` holds runnable templates.
`docs/archive/` preserves historical targets, not the running contract.

## Project map

- `src/internal/formations/` owns the model, persistence and run engine.
- `src/internal/coordinator/` owns admission, runtime commands and projections.
- `src/internal/api/` owns authoring HTTP and local adapters.
- `src/internal/daemon/` owns `archond` flags, executor wiring and startup.
  `src/cmd/archond/` and the compatibility entrypoint `src/cmd/formationsd/`
  only call it.
- `src/cmd/archon/` owns the CLI.
- `dashboard/` owns the Archon board editor and Agents view.
- `Perttus_vision_for_agent_orchestration/` retains vision and canvas references.

## Reuse CHROTE's proven code

CHROTE has working, hard-won implementations for file browsing and viewing,
browser terminals, and tmux integration. Before implementing or changing these
areas, inspect the CHROTE repository's code and tests and repurpose them for
Archon. Prefer adapting that code over building a parallel implementation.

Start with these paths in the CHROTE repository:

- Files: `dashboard/src/components/FilesView/`,
  `dashboard/src/components/FileViewer.tsx`, and `src/internal/api/files*.go`.
- Browser terminals and tmux: `dashboard/src/terminal/` and `src/internal/proxy/`.

Preserve the relevant behavior and regression coverage while adapting to
Archon's contracts. Keep source attribution on ported code and explain any
necessary departure from the CHROTE implementation.

## Work state

Use this repository's `form-` Beads store. Execute the active Bead and record
unrelated findings separately. Host deployment and forwarding live outside this
repository; CHROTE integration and SRV deployment require Beads in their owning
stores. Keep tracked files host-neutral. Supply host paths, sockets, users and
listen addresses through flags or environment, with placeholders in examples.

## Validation

```bash
cd src && go test ./... && go build ./cmd/archon && go build ./cmd/archond && go build ./cmd/formationsd
cd ../dashboard && npm ci && npm run test:unit && npm run build && npm run lint
```

Keep main clean and current. Do ordinary verified work directly there with small
commits. Use a branch when isolation helps, then merge and remove it within the
assigned integration authority. Lane briefs may reserve integration to an owner.
