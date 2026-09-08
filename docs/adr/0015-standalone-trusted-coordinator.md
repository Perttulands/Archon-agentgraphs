# Standalone trusted coordinator

Status: superseded in part by [ADR-0016](0016-daily-capability.md).
The single-run, solo/Codex-only, loopback-only and deferred abort/recovery
statements below describe the original proving mission. Read
[the current contract](../CONTRACT.md) for current behavior. Service ownership,
trusted authority, private evidence and immutable cleanup principles remain.

Accepted for the form-2fb proving mission on 2026-09-06. Formations now runs in
its own process, `formationsd`, with Archon as its authoring and command client.
CHROTE can consume this HTTP boundary in separately authorized integration work.
This repository does not change the CHROTE binary or its services.

The first service uses the existing trusted-operator execution model and
schema-1 ledger engine. It explicitly calls its projection
`standalone-trusted-v1`. The extensive `authoritySchema=2` target in the root
specs remains unimplemented. This decision changes service ownership and makes
the smaller current contract explicit; it does not claim certified same-UID
isolation, existing-session attachment, private artifact capabilities, durable
command receipts, or automatic crash recovery. The current code already allows
trusted runtime effects through `RequireRuntimeAuthority`; older root-spec
claims that all runtime calls are disabled need reconciliation in form-1jw.

One coordinator holds a kernel lock on its configured state directory. That
directory is outside the source checkout and contains the definition workspace,
private ledgers, binding snapshots, and dispatched brief files. Archon authors
definitions there through the shared package. The coordinator admits one
non-final mission at a time, snapshots definitions, and returns HTTP 202 with
the run ID only after the initial event is durable. The owning worker continues
after the HTTP request ends. Human gates retain that admission until verdict.
Closing the coordinator fences new work and waits for admitted execution before
releasing its lock.

The first adapter supports a chain of one-slot solo formations. Each slot gets
one new Codex session named `form-<mission-label>-<slot-id>` on the explicitly
configured existing socket. A collision fails; it never adopts another session.
The wrapper path, socket, cwd, transcript directory, model, effort, and limits
are host configuration. Creation returns immutable session and pane IDs. A
real terminal type is set for the daemon's tmux client environment and the new
session. The daemon resolves Codex to an absolute executable before launch. A
control client sizes the new session once with `refresh-client -C 160,48` and
supplies output events for readiness and prompt staging. A short file pointer is
pasted, inspected, and submitted once. Native transcript evidence must contain
that exact user message in the exact workspace, the configured model and effort,
and the same turn's final response followed by `task_complete`. Native
`phase: final_answer` and legacy `channel: final` both identify final responses;
native turn IDs must match completion. Filesystem
notifications drive observation. The admission deadline cancels the executor;
session cleanup finishes before the worker returns. Cleanup targets only the
immutable ID returned by creation and records its result.
The configured wrapper must authorize that cleanup. Supply the host's explicit
cleanup approval environment when launching the daemon if its wrapper requires
it; the service never changes the wrapper's allowlist or bypasses it. A failed
cleanup preserves diagnostic detail in the private ledger and exposes the
failure outcome in the public projection.

HTTP binds a literal loopback address and has no additional authentication.
This is a trusted local tool. Every runtime read, including run list and SSE,
uses the same closed projection. It includes event identities, node/slot/gate
progress, session display names, cleanup outcomes, and pending human request
sequences. It omits raw prompts, captures, runtime paths, native session IDs,
arbitrary event data, and output artifacts. Output inspection in this first
proof is the operator's local repository review. The coordinator exposes no
generic file access. SSE sends a complete projection after durable changes,
subscribing before reading to avoid lost notifications. Archon `--server` uses
HTTP for the whole selected command and has no local fallback.

A human verdict must name the exact pending gate and requested sequence. The
coordinator records the choice and continues the existing run. There is no
default verdict. An unwired FAIL remains a visible blocked run. A restart keeps
history inspectable. An operator may explicitly start the replacement daemon
with `--resume-run`, `--completed-transcript`, and `--completed-brief` to recover
one already completed unresolved dispatch. This requires the original brief
digest, exact native session previously recorded as consumed, matching workspace,
model, effort, final answer and completed turn. Evidence is validated before
resumption; rejected evidence leaves the blocked ledger unchanged. Recovery
derives the unresolved dispatch from the complete ledger, even if a legacy
timeout omitted it from the blocked event. It routes the recovered result and
continues the remaining graph without recreating that seat. It neither adopts
nor cleans up the old session; the operator must account for that recorded
session separately. No HTTP resume or live reattachment is exposed. Generalized
recovery and active cancellation are later work. Existing peer and leader
experiments remain research code outside this adapter's admission contract.

The tradeoff is deliberate: prove an owned team and a real human decision with
the existing engine before implementing the much larger private-authority and
session-reuse target. The [OpenAPI sketch](../openapi/formations.yaml) describes
the implemented subset. Host deployment and CHROTE consumption require their
own Beads.
