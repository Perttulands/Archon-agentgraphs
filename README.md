# CHROTE Agent Formations

This repository is a history-preserving extraction of CHROTE's unreleased
Formations, Archon, Agents, and Oracle experiments. It is **experimental and
frozen**: the code is retained for research and possible future work, not as a
supported CHROTE feature or production service.

The extraction deliberately has no runtime integration with CHROTE. Designing
a future HTTP contract, deployment model, or supported release belongs to new
work in this repository.

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
