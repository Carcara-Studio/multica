# ADR-031 — Native delegation operation identity

- Status: Accepted
- Date: 2026-10-20
- Decision owners: Human maintainer (standing governed execution authority)
- Related issues: #77; follows #75
- Supersedes: None; resolves ADR-004's deferred storage choice
- Superseded by: None

## Context

At fork main `1529aec2db660aad6417ac3842dce91e768ec181`, upstream baseline
`47120a4131564175da044d63d897ed9b5d76979a`, `IssueService.Create` owns its
transaction. Its title duplicate guard only covers active Issues.
`IssueCreateParams`, the Issue schema and source-context capture have no
general stable operation identity. Autopilot quota reservations and maintenance
job identities belong to other domains. These are inspected code facts.

## Decision Drivers

Preserve native Issues, assignment, wakeups and one-hop authority; settle retries
across later Agent turns and child edits/completion/deletion without new work.

## Considered Alternatives

- Active-title/origin matching: cannot distinguish explicit follow-up from retry,
  and ceases to protect completed/deleted work.
- Outer transaction around ordinary Create: cannot cover its independent commit.
- Separate hierarchy executor: rejected by ADR-004.
- An additive identity record inside native Create: selected.

## Decision

Add an optional internal hierarchy admission point at the beginning of native
Create's transaction. It calls CheckDelegationRelation before other locks.
The native hook also verifies that the supplied source task belongs to the
parent/delegator/workspace and is running. This read is not a task-liveness fence.
A trusted mandatory callback must independently reauthorize the current
authenticated actor/task and target invocation/runtime using transaction-bound
queries, on new creation and replay. No transport, production caller or public
capability is enabled by this increment.

Persist a workspace/parent/delegator/request-UUID unique identity with Project,
target, original source task, child Issue and SHA-256 canonical work intent.
Origin task is lineage, not intent: a later authorized Agent turn may retry.
Supported work is title, description and priority, starting on todo; extensions,
attachments, labels, properties, dates, stages and channel deferral are rejected
until their replay semantics are specified. Hash Project/target as well.
A new identity permits an intentional same-title follow-up.

Commit identity and native child together. Exact replay returns the existing
workspace-scoped child before counters, events, attachments and enqueue.
Changed intent conflicts. A deleted child leaves a tombstone and cannot be
recreated by retry. Child edits/completion/moves do not reopen the identity.
Authorization/relation changes can deny replay; replay is not an auth receipt.

## Consequences

The fork gains durable creation idempotency and lineage without a new work
lifecycle. Workspace-wide creation serialization is inherited from the relation
gate and increases contention. The native post-commit scheduling crash gap
remains: this does not promise exactly-once dispatch or automatic repair.
Retries never enqueue again, even if the initial post-commit enqueue failed.

## Data Impact

FK-free project_hierarchy_delegation, reserved migrations 900024/900025, one
concurrent unique index (no surrogate ID/primary key needed).
Workspace teardown explicitly purges identities under its workspace lock.
Parent/Agent/Project/child deletion alone preserves tombstones until workspace
deletion. Rollback removes identities; migration down removes replay history,
so do not downgrade and re-enable delegation expecting previous keys to survive.

## Backend / Runtime Impact

Small opt-in hook in IssueService.Create; validation/persistence live in an
isolated service module. Native Issue row, origin, numbering and scheduling
remain authoritative. Callback implementations must not acquire locks in an
order that reverses native workspace/Project/Issue/Agent/task protocols, perform
side effects, or treat a caller's advertised eligibility as authorization.

## API / CLI / UI Impact

None. No endpoint/command is registered. Public actor/task/invocation admission
and execution integration remain pending. Mentions and Squads are unchanged;
the server never completes the parent.

## Failure and Edge Cases

Concurrent retries serialize before identity lookup. Infrastructure errors
propagate; a unique index protects against writers violating the protocol.
Missing authorization fails closed. Authorization failures and failed commits
leave no child/identity/counter/event. An unreadable child produces an error
without retry creation. Deleted workspace blocks new admission.

## Testing Requirements

PostgreSQL concurrent retry, changed-intent conflict, deliberate follow-up,
completed/edited/deleted child, fresh authorization on replay, invalid/reassigned
parent, missing admission callback, commit rollback, counter/event suppression
and workspace teardown. Require exact-head build/vet/race, migrations and sqlc
freshness checks. Fixtures only; no real CLI/account/inference.

## Upstream Compatibility

Only native IssueService opt-in hooks and workspace teardown are altered, plus
generated schema output/teardown manifest/migrator registration. Zero options
preserve existing callers. Prefer future upstream stable Issue operation
identities if their invariants match; migrate rather than preserve redundancy.

## Open Questions

The future public caller must prove current task actor, normal invocation and
runtime eligibility and complete the execution lock-order review before
registration. Durable dispatch recovery is separate from creation replay.
