# Archon only chains agents and gates

Accepted 2026-10-01 by Perttu. This is Archon's core product philosophy. It
supersedes [ADR-0018](0018-referenced-file-roots.md) (file-root confinement),
the runtime-authority and certification parts of
[ADR-0007](0007-formations-execution-authority.md) and
[ADR-0015](0015-standalone-trusted-coordinator.md), and every one-release
compatibility promise made elsewhere. The running contract is
[CONTRACT.md](../CONTRACT.md).

## Decision

**Archon makes chaining agents and gates easy and great. That is all it does.**

- **No security rituals.** Archon sessions are tmux sessions, just like
  CHROTE's, with full access to everything. Archon adds no sandboxing, no
  file-root confinement, no blast-radius limits and no authority checks. Safety
  lives in the configuration of the agents Archon runs (their harness
  permissions and settings), not in Archon. The cockpit opens any file a path
  names, as CHROTE does.
- **No legacy and no backwards compatibility.** Archon keeps only what is
  current: no deprecated aliases, old routes, old flag names, compatibility
  readers or migration shims, and no mess left behind. When something changes,
  the live data is migrated once and the old path is deleted.
- **No rituals around the skill.** The `archon` skill is a version that makes
  sense today, iterated on later. It has no eval harness, tests or CI.

## Consequences

- Code that exists only to confine, certify or authorize is removed: the
  referenced-file roots and their refusals, and the dormant runtime-authority
  seam. Checks that keep Archon correct stay, such as attaching a viewer to the
  right seat.
- Renames and reshapes land whole. The mission rename's `board` aliases,
  `/boards` routes and `?board=` links, older flag names, legacy persona launch
  strings, and the slot migration code after live data is migrated, are
  removed.
- The skill's eval harness is deleted.
- Work that adds a guard, a shim or a ritual needs a reason that serves chaining
  agents and gates, or it does not belong in Archon.
