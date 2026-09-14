> Decision record, reviewed 2026-09-14. Earlier product names and host assumptions
> below record their original context. Archon is the standalone product;
> [the current contract](../CONTRACT.md) defines its implemented behavior.

# Daily Formations capability

Accepted 2026-09-08. Implements the decisions in form-bxm. Supersedes
[ADR-0015](0015-standalone-trusted-coordinator.md) on admission, harnesses,
network listeners, cockpit ownership, cancellation and recovery.

Formations builds and runs reusable agent work graphs. Operators draft boards
and notes in the cockpit, agents build them out with ARCHON, and the daemon
executes missions. [CONTRACT.md](../CONTRACT.md) is the running contract.

Use one seat executor with `claude-code` and `openai-codex` adapters. Admit solo,
peer and orchestrated formations. Orchestrated execution stays leader-agentic
under ADR-0011: the controller directs only its bound workers; the runtime owns
session creation and cleanup. Model and effort belong to persona harness
variants. Missing effort means medium; model may use the harness default.

Execute substantive gate checks through judge formations. A judge seat performs
the checks and emits a structured `chrote-verdict` with verdict, reason and
evidence. Malformed output blocks. Add no code-gate profile for shell commands
or Beads checks. Existing substring profiles remain. A failed gate delivers
reason, evidence and original input along its fail edge for bounded rework.

Supply target cwd, brief and Bead per run. Keep boards reusable. Admit many runs
concurrently; abort cancels one run and waits for its seats to stop. On restart,
inspect durable evidence and recover eligible completed dispatches; block loudly
when completion cannot be proved. Never adopt or clean up an old daemon's seats.
Explicit completed-turn recovery remains available.

Serve the cockpit and authoring API from formationsd. Configure listeners on
trusted host interfaces and reach the cockpit over the tailnet through host
forwarding. No application authentication is added. Host deployment, network
configuration and CHROTE integration belong to their owning repositories and
Beads, not this source tree.

The first delivery graph is Plan -> Beads -> Beads review gate -> orchestrated
Execution -> Final review. Failed review returns to Beads. Execution uses a
Claude controller and Codex workers; final review uses Astra. Final review is a
report, with no following gate. There is no human gate in this delivery graph.
Human gates remain available for other graphs. Telegram notification is deferred.

Retire the three large root specs and the vision spec folder to an unchanged
archive. Keep the vision interview, locked decisions and referenced prototypes.
The current contract, examples, CLI help and OpenAPI describe the implemented
system. Runtime authority remains the trusted-operator seam from ADR-0015,
with enforcement deliberately disabled; this decision does not implement the
archived schema-2 certification or isolation target.

The tradeoff is to make owned agent delivery usable with observable outcomes
before implementing a broader authority model or session reuse. Lab runs prove
routing only. The real delivery acceptance remains a separate Bead.
