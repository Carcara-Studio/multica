# ADR-021 — Runtime lifecycle invalidation and terminal cleanup

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #49
- Extends: ADR-011, ADR-018, ADR-020

## Decision

Execution-affecting profile changes (command, fixed arguments, enabled status,
visibility) rotate existing optional policy epochs in the same transaction as
profile mutation. Lock profile, registered runtimes and affected user Agents
before the leaf state phase. Lock the entire primary/fallback Agent union in
Agent-ID order before any primary-only dependency scans, including profile-wide
deletes; cross-referenced runtimes must not lock primary subsets first. Cosmetic/no-op edits preserve availability. Disable
also suspends; enable does not silently resume or establish primary health.
Runtime visibility mutation similarly locks its current row and affected Agents,
rechecks permission and commits visibility with policy invalidation. Cosmetic
runtime name changes preserve upstream machine-fanout behavior and do not rotate.
As upstream already did, a combined visibility/name PATCH applies the name after
the visibility operation; this is not an atomic machine-wide rename API.

Affected Agents include fallback-runtime references when their primary is on
another runtime. Runtime/profile deletion suspends those policies and retains
configuration plus surviving immutable task proofs for explicit revalidation;
it never silently chooses a replacement fallback. Batch invalidation deduplicates
Agent IDs because primary and fallback may belong to two instances of one profile.

Legacy consolidation preserves upstream reassignment of existing Agent/task
runtime IDs. It rotates and conservatively suspends affected optional policies
before commit; old route proofs are not rewritten and old probe success cannot
restore health. A failed rotation rolls back reassignment and runtime deletion.
Consolidation does not certify equivalent credentials or migrate fallback config.

## Terminal cleanup and lock order

Runtime teardown captures affected user Agents and terminal system-Agent
identities while runtime owner locks are held. It performs no failover-state or
config mutation itself: every runtime/task/Agent/profile write must finish before
the outer transaction calls the failover leaf phase. Manual deletion, confirmed
delete, profile delete and retention GC all use this boundary.

Only terminal owner deletion removes optional config/state/route rows. Cleanup
requires the Agent to be absent, filters workspace, orders config before state
before route, and runs in the same transaction that deleted the owner. Deleting
an ordinary chat leaves its surviving user Agent and failover data untouched;
deleting a builder chat cleans its deleted system Agent. No FK, cascade, trigger,
migration, new public response field or external provider access is introduced.

## Remaining boundaries

Daemon registration/heartbeat metadata is not an account epoch, capability proof,
model-selection proof or recovery signal. Runtime-local credential/default-model
changes still need an explicit transport contract before failover is enabled.
All protected configuration, enqueue/retry/claim gates and credential-faithful,
tool-free real-inference probes remain required. This increment enables none of
those workers or automatic fallback routing.
