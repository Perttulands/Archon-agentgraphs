# Archon product

Archon makes chaining agents and gates easy and great, and that is all it does
([ADR-0021](adr/0021-archon-only-chains-agents-and-gates.md)).

- **No security rituals.** Seats are ordinary tmux sessions with full access,
  as in CHROTE. Archon adds no sandboxing, file confinement, blast-radius
  limits or authority checks; safety lives in the configuration of the agents
  it runs.
- **Only what is current.** No legacy paths and no backwards compatibility:
  renames and reshapes land whole, live data is migrated once, and the old
  path is deleted. There are no deprecated aliases or compatibility shims.
- **The `archon` skill** is a sensible current version, iterated later, with
  no eval harness, tests or CI around it.

## Goals

- **Agent-facing first.** Agents author, run and chain missions. The UI exists
  so the operator and the agent share an understanding of what a mission is
  for and iterate on it together.
- **The operator steers through the managing agent.** In that agent's own
  conversation it clarifies inputs, prepares or reuses the mission, runs it,
  follows progress and asks consequential questions. The cockpit is where both
  inspect the mission and its sessions, work, artifacts and reasons.
- **A mission works like a skill.** It carries everything a run needs (agents,
  chaining, gates, what a failed gate does, any limits), can be sent off inside
  other work and can be chained by agents.
- **The driving agent pulls.** It oversees its run directly with `run wait` and
  `run follow`; Archon pushes nothing into outside sessions.
- **Limits are opt-in.** A run has no limits unless its mission holds a Limit
  card capping rounds, time or tokens; nothing adds one by default.
- **Evidence stays small by default.** Keep compact history and selected evidence;
  expire completed working files. Large archives are explicit. Project
  deliverables belong to their project, not an unbounded run archive.
- **Roles are generic.** Mission-specific instructions go in the step brief;
  model and effort stay visible on the mission.
- **Work is ranked** by whether it helps an agent build, understand, invoke or
  chain a mission without human configuration, and whether it helps the
  operator understand and steer it.

The [contract](CONTRACT.md) defines the running behaviour and
[DESIGN.md](DESIGN.md) the cockpit's design language.
