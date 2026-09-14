# Release packaging

Archon releases target Linux on `amd64` and `arm64`. Each archive contains the
CLI, daemon and UI built from the same Git commit. The daemon compatibility
command `formationsd` contains the same binary as `archond`.

## Build and verify

Use Linux with Go 1.26.6 or newer, Node 20.19+ in the 20.x line or Node 22.12+,
Bash, Git, GNU tar and coreutils. Set the release number in the root `VERSION`
file and commit the source before building. Python 3 runs the release smoke
checks.

```bash
./scripts/build-release.sh --out "$PWD/dist"
```

The script rebuilds the UI with `npm ci` and `npm run build`, then compiles
both Linux architectures with CGO disabled. `--arch amd64` or `--arch arm64`
builds only that architecture. Uncommitted source stops a release build;
Beads state is excluded from that check. `--allow-dirty` is for local
verification and stamps the commit with `-dirty`.

For version `0.1.0`, the output is:

```text
archon-0.1.0-linux-amd64.tar.gz
archon-0.1.0-linux-arm64.tar.gz
SHA256SUMS
```

Each archive has one top-level directory matching its filename without
`.tar.gz`:

```text
bin/archon
bin/archond
bin/formationsd
share/archon/ui/
share/archon/examples/
share/archon/docs/
docs/
examples/
install.sh
README.md
LICENSE
VERSION
COMMIT
PLATFORM
MANIFEST.sha256
```

The root `docs/` and `examples/` copies keep the bundled README links usable.

`SHA256SUMS` verifies downloaded archives. `MANIFEST.sha256` verifies the files
inside each archive. `archon --version` and `archond --version` report the
release version and source commit. Run the source validation in
[AGENTS.md](https://github.com/Perttulands/Archon-agentgraphs/blob/main/AGENTS.md) and the release smoke checks before publication:

```bash
python3 scripts/test-release.py "dist/archon-$(cat VERSION)-linux-amd64.tar.gz"
```

## Install and upgrade

Extract an archive for the current machine, then run its `install.sh`.
The default prefix is `$HOME/.local`; `--prefix` accepts an absolute directory.
The installer checks the platform and manifest before installing.

For a chosen prefix, the installed layout is:

```text
bin/archon -> ../lib/archon/current/bin/archon
bin/archond -> ../lib/archon/current/bin/archond
bin/formationsd -> ../lib/archon/current/bin/formationsd
lib/archon/current -> releases/<version>-<commit>-<platform>
lib/archon/releases/<version>-<commit>-<platform>/
```

The release directory contains the full archive. Examples are available under
`lib/archon/current/share/archon/examples/` and documentation beside them.
The daemon resolves its executable and finds the bundled UI automatically.
Use `--ui-dir /absolute/path` to serve another UI build, or `--ui-dir ''` to
run without the UI.

An upgrade installs a separate release directory and switches the `current`
link atomically. The installer refuses to replace unmanaged command paths or
an unmanaged `current` path. It also refuses a changed installation with the
same release identity. Older releases remain on disk. A running daemon keeps
using its loaded binary until the operator restarts it.

The installer creates no service and does not copy, migrate or remove runtime
state. Supply the same `--state-dir` when restarting to retain boards and run
history. Follow the [shutdown and recovery procedure](CONTRACT.md#operator-procedure)
before restarting a daemon that has active work.

## Publish

The release workflow builds version tags. The tag must match `v` followed by
`VERSION`, for example `v0.1.0`. Publish both archives and `SHA256SUMS` together
on the existing
[GitHub repository](https://github.com/Perttulands/Archon-agentgraphs/releases).
Download the published archive, check its checksum, and verify installation,
version identity, UI serving and lab execution in a disposable state directory.
The release workflow runs the install, upgrade, UI and persisted-run checks on
native x86-64 and ARM64 runners before publishing either archive.
