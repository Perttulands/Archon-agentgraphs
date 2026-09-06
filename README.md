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
