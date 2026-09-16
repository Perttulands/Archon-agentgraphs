# Referenced file roots

Accepted 2026-09-16. Implements the file-reference part of form-ged.5 under the
epic form-ged, whose operator decisions ask for a gate's rubric and other
referenced files to open from the cockpit. Extends
[ADR-0017](0017-run-evidence-api.md), whose trust boundary and confinement it
reuses.

Missions, formation briefs and gates name reference files by path. Before this
decision the daemon served no file outside a run's own artifacts, so a rubric
could be named but not read.

## Decision

- `archond` takes repeatable `--file-root <absolute-dir>` flags. It reads a
  referenced file only under one of these roots. With no roots, nothing is
  readable and every reference reports as outside them.
- Two `GET` routes serve a reference: `/api/formations/files/preview?path=` (a
  capped, classified, redacted preview) and `/api/formations/files/raw?path=`
  (bytes, for images and opening in a tab).
- An absolute reference must be a clean path and is read under the deepest root
  that contains it. A relative reference is tried under each root in the order
  the flags give; the first readable file wins, and the response names the
  absolute path it read.
- References are data on the board. Authoring does not check that a file
  exists or sits under a root, so a board stays portable between hosts.

## Confinement

The run evidence rules apply, with the configured root in place of the state
directory:

- The root is opened once; it may itself be a symlink the operator chose. Every
  component below it opens with `O_NOFOLLOW`, so `..`, a symlink at any level,
  a hard-linked file (link count above one) and non-regular files are refused
  as not found.
- Previews carry at most 256 KiB of text; raw reads stop at 16 MiB and return
  413 above it.
- Text passes the ledger's secret patterns. The raw route never serves active
  content: text is `text/plain`, only PNG, JPEG, GIF and WebP keep their image
  type, anything else is an attachment, and every response carries `nosniff`,
  `Content-Security-Policy: sandbox` and `no-store`.
- A path outside every root, or one the daemon user may not read, is 403, which
  the cockpit shows as not readable here with the path.

## Consequences

The operator chooses which host directories the cockpit may read, per daemon.
Adding roots to a deployed unit is host configuration owned outside this
repository. Anything under a root is readable by whoever reaches the cockpit,
the same trust as run evidence, so roots should be project checkouts and
documentation, never state, credential or home directories.
