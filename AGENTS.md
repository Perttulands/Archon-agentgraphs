# CHROTE Agent Formations

This repository preserves the extracted Formations, ARCHON, Agents, and Oracle
experiments. Formations now has an experimental standalone coordinator for trusted local
missions, described in docs/adr/0015-standalone-trusted-coordinator.md. It is not a
supported CHROTE feature or deployed production service. New product or
integration work requires an explicit Bead here; do not silently wire it back
into CHROTE.

## Project map

- `src/internal/formations/` owns the model, persistence, and run engine.
- `src/internal/api/` owns the extracted HTTP surface and local adapters.
- `src/cmd/archon/` owns the ARCHON CLI.
- `dashboard/` owns the experimental Formations and Agents cockpit.
- `Perttus_vision_for_agent_orchestration/` is the durable vision and design packet.

## Work state

This project owns the `form-` Beads store. Use it for Formations and ARCHON
work. CHROTE integration work belongs to CHROTE; SRV deployment work belongs to
SRV. Execute only the active Bead and record unrelated findings separately.

## Validation

```bash
cd src && go test ./... && go build ./cmd/archon
cd ../dashboard && npm ci && npm run test:unit && npm run build && npm run lint
```

Keep `main` clean and current. Do ordinary verified work directly there; use a
branch only when isolation materially helps, then merge it back and remove it.
