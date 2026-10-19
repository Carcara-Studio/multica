# ADR-020 — Atomic Agent mutation and failover invalidation

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #47
- Implements in part: ADR-011, ADR-017, ADR-018, ADR-019

## Decision

Connect existing primary-setting, protected environment, runtime-skill and archive
mutations to the optional durable policy epoch. Keep sqlc Agent method signatures
and public response schemas unchanged; use short existing/new transactions and a
small Go helper rather than database triggers or wrapping every Agent query in
fork-owned SQL machinery.

`InvalidateAgentChange` compares DB round-trips of runtime identity/mode/config,
owner, model, thinking, tier, custom env/args, MCP, disabled runtime skills and
Composio allowlist. Changed values rotate the opaque primary policy generation.
The helper compares secret-bearing bytes only in memory; no credential hash,
secret copy, prompt, external discovery or provider inference is introduced.

Cosmetic metadata, instructions/shared identity, idle/working status and unchanged
stored settings do not rotate availability. Instructions/ordinary skill bindings
remain live shared Agent context; later compatibility checks must still validate
current required tools at launch. This helper is not a full capability proof.

Absent state is a no-op, never initialization or opt-in. Existing primary-active
state stays primary-active. Fallback/probing stays fallback, claims are cleared
and the next probe is scheduled 30 minutes after rotation. Existing suspension
and failure/recovery history are preserved. Archive or unbind additionally
suspends policy; restoring an Agent does not silently resume it or prove health.
Queued route rows are retained with their old generations for fail-closed launch
validation, not rewritten to a new target or replayed.

## Transaction and lock placement

Generic Agent update now locks the Agent only after pre-write validation, applies
updates, nullable clears and invocation targets within one transaction, then
invalidates state last and commits before responses/events. The protected env
endpoint merges mask sentinels against the locked current row, writes its existing
redacted audit, then invalidates before the same commit. A failing audit or policy
rotation rolls back the environment and audit together. Runtime-skill toggles
reuse their existing Agent lock/transaction and invalidate before commit.

Single-Agent archive locks the current Agent, rechecks archive eligibility,
archives and invalidates in one transaction. It commits **before** the existing
TaskService cancellation path, avoiding a new state-to-chat/task lock edge.
Member revocation already archives/cancels/settles work; batch invalidation is its
last write after member/subscription/owner operations. Runtime teardown already
locks runtime then user Agents and drains tasks/prunes system dependencies;
its callers invalidate user-Agent policy after deleting runtimes/profile and
before commit. Profile deletion batches all runtime teardowns before state locks,
so the first runtime's state is not held while draining tasks on the next. Batch state writes use stable Agent-ID order without
reordering response/event lists. No network work is performed under state locks.

Rotation locks state before sampling the database wall clock, matching the
existing dispatch/settlement rule. A concurrent policy-generation change fails
CAS and must roll back the caller mutation; no latest-version substitution is
used to grant recovery. Probe state/version changes alone do not claim health or
rewrite queued proof. Production config writers must hold the Agent/owner locks
before config -> state, as required by ADR-018/019.

These placements close the inspected mutation edges only. They do **not** declare
the complete future enqueue/config/claim/probe lock graph closed. Existing upstream
runtime/Agent/task owner ordering remains an integration constraint, not a reason
to hold state while acquiring those locks. Default Agent JSON/execution behavior
is unchanged, though multi-write Agent settings now commit/roll back atomically.
No fallback config endpoint, routing, quota activation or real probe is enabled.

## Current production coverage and remaining boundaries

Covered: `UpdateAgent` and all its clear branches, `UpdateAgentEnv`,
`SetAgentRuntimeSkillEnabled`, single `ArchiveAgent`, member revocation's
`ArchiveAgentsByRuntime`, and runtime/profile deletion plus retention GC after `TeardownRuntime` user-Agent
unbind.
`RestoreAgent` deliberately preserves suspended state. No new migrations.

Not covered by this increment: legacy runtime consolidation/reassignment,
runtime/profile/default-model edits, runtime-local account/credential epochs,
terminal system-Agent cleanup of optional fork rows, fallback-runtime references
on otherwise bound Agents, ordinary skill/permission table changes, all task
writers/retry/claim ownership, complete protected fallback target configuration
and capability transport, API/CLI/UI and safe inference adapters. These remain
required before feature enablement. The unused `ArchiveAgentsByIDs` SQL query has
no production caller in this snapshot; a new caller must include the same hook.
Direct SQL writes bypass application hooks and are not product configuration APIs.

## Verification

DB-backed HTTP regressions cover changed/no-op/cosmetic edits, nullable clears,
runtime move, env, archive/restore, runtime-skill toggle, concurrent updates,
member revocation and runtime teardown. A valid maximum bigint state version
forces invalidation failure after writes, without shared test triggers: model,
archive, env and its audit must all roll back. Ordinary Agents are not initialized.
Old dispatched success cannot restore primary after mutation. Existing endpoint
permissions, secret redaction/audit, runtime teardown, migration and failover
regressions must pass on exact-head PostgreSQL/race CI. Local DB skips are not
verification of transactional behavior.
