# ADR-018 — Opt-in task route proof and target persistence

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #42
- Refines: ADR-007, ADR-017

## Decision and reason

ADR-017 requires enqueue-time generation/version proof before a primary failure
may activate fallback. A marker only on fallback tasks (ADR-007) cannot provide
that proof for primary tasks. Reading the latest state when an old Task fails
would allow it to reactivate fallback after successful primary recovery.

Store an optional immutable route row for **both primary and fallback work when
fallback is configured**. Agents without fallback retain the upstream path,
without rows or backfill. Existing Agent fields remain the primary target and
`agent_task_queue.runtime_id` remains the selected runtime authority. This
refines ADR-007's fallback-only row rule; it does not migrate primary config.

Each proof binds Task, Agent, workspace, selected runtime, route, primary policy
generation, enqueue state version and fallback config generation. No prompt,
credentials, session ID, environment, custom arguments or MCP payload is stored.
The fallback generation binds primary work too: deleting/recreating or editing
the fallback must not authorize a failure from an older policy.

`agent_fallback_config` is optional user-Agent metadata: runtime, model, thinking
level, service tier, opaque generation and timestamps. Generation changes on
**every** edit, including same-runtime option edits. These initial columns are
not the complete execution contract. Required provider-specific overrides and
secret handling must be integrated with upstream protected mechanisms before
any combination can be advertised as ready. Never silently inherit incompatible
primary environment/arguments/MCP, or drop required tools to permit a launch.

## Persistence boundary

New migrations 900013–900021 are additive, no-FK and no-backfill. Each index is
concurrent in its own migration. Unique indexes attach as primary keys. Existing
interrupted-index hooks and primary-key ledger recovery cover both tables.
Workspace deletion removes both tables in its existing transaction and manifest.
Terminal Agent/runtime/task deletion hooks remain required before integration.

Config insert/update check active same-workspace user Agent, existing runtime,
and the upstream private-runtime binding rule: a private runtime's owner must
match the Agent's owner, not merely the editing administrator. Public runtimes
remain structurally eligible. Offline status is not a reason to return to the
quota-exhausted primary. These are relationship checks, **not authorization** or
capability/model validation. Config CAS rejects stale generation and requires a
new generation. Removal never changes availability or erases queued proof.

Route insertion requires a real queued/deferred Task whose Agent/runtime match,
current unsuspended state, exact supplied generations/version and route/state
agreement. Primary requires primary-active; fallback permits fallback-active or
primary-probing. Actual runtime visibility/ownership is rechecked. Invalid input
writes no row. Duplicate insert never rewrites an existing proof.

The caller must pass its existing transaction (`WithTx`) and roll back task
creation when required proof cannot be written. Configuration locks precede
state locks; both are share-locked through route insertion. Upstream owner/chat/
task locks must be acquired before entering these primitives. These SQL helpers
do not acquire those owner locks and their MVCC checks alone do not fence
concurrent archive, rebind or teardown. No complete integration lock graph is
claimed here. Do not enable until lifecycle writes and task writers compose
these locks and have concurrent integration tests.

Automatic retry copies the actual parent route and original epochs/version in
the child transaction, checking persisted parent/child Agent/runtime lineage.
Current config/state is deliberately not resolved again. A stale proof remains
stale; persistence is not launch permission. No parent marker means upstream
behavior; a routed parent's failed copy must roll back the child. Human reruns
use upstream `rerun_of_task_id` instead of automatic retry's `parent_task_id`;
that policy/caller integration is deferred and must preserve the selected runtime
and explicitly define which original target proof it inherits before enabling.

## Later integration requirements

- Bind primary quota activation to the stored proof and current fallback config,
  then apply ADR-016 reliable raw evidence. The existing low-level activation
  primitive alone is still insufficient. Old tasks without proof cannot activate.
- Revalidate route/config epochs at claim. Config edits invalidate queued opt-in
  work before launch; recovery alone does not reroute already queued fallback.
- Preserve existing private-runtime/workspace/task authorization and session
  fences; same-runtime primary/fallback must not reuse incompatible sessions.
- Config edit/removal and primary-critical changes must atomically invalidate
  probe claims/rotate policy as appropriate while preserving availability.
- Complete compatible target payloads, owner cleanup, API redaction/authorization,
  enqueue/retry/claim integration and capability-gated tool-free inference.

No production enqueue/claim/failure/config endpoint calls these primitives in
this increment. No Agent is opted in and no fallback is enabled. DB tests cover
ownership/CAS, immutable proofs, stale versions, route/state/runtime agreement,
probe/recovery preservation, retry inheritance after config removal, and atomic
Task/proof rollback. Migration tests cover up/down/up, constraints, no backfill,
no FKs and interrupted PK ledger recovery; workspace tests cover atomic cleanup
and neighbour preservation. CI PostgreSQL execution is required before merge;
local skips are not database validation.
