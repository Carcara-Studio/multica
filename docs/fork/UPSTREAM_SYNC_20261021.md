# Upstream synchronization — 2026-10-21

- R5 subtask: #104; policy: UPSTREAM_SYNC.md, ADR-014/015.
- Fork base: 33eef12241b6ad224bd9283359a62abab7470b9b.
- Prior upstream integration: 2b645691387e1535cc63d6ebf2ba4e996387c8b2.
- Upstream target: c9b6de8567d7bb57bf9a2a3220207cf2b3e5a038.
- Integration uses a merge commit with both ancestries; PR must not be squashed.

## Changes and resolution

Upstream adds shortcut/disabled-reason tooltips and dark tooltip surfaces,
GitHub linked-PR conflict wakeups, managed Qoder runtime support and setup docs.
All upstream changes are retained. One textual conflict occurs in
server/internal/handler/agent_env.go: upstream validates the effective merged
Qoder environment before writing; the fork already moved environment merging
under GetAgentForUpdate and made mutation, audit and failover invalidation atomic.
Retain that locked merge and apply upstream's Qoder rejection to the locked Agent
and merged environment before persistence. No values enter diagnostics. Rejected
writes roll back; accepted writes keep the existing audit/invalidation transaction.
Existing upstream Qoder effective-settings tests and fork environment lifecycle/
rollback tests jointly protect this resolution; no new validation subsystem.

Automatic merges retain ProjectHierarchySection, HierarchyEscalations, their
API/locale contracts and the fork's Agent/runtime lifecycle invalidation hooks.
Native initial-task registration (ADR-035), its due/early promotion guards and
receipt replay modules are unchanged. Qoder start/completion uses upstream's
provider-scoped Issue transitions; ordinary Codex/local transitions stay native.
Linked-PR conflicts extend only the pull_request wakeup branch; hierarchy child
completion and each-stage predicates retain their existing contracts.

Upstream migrations 586/587 remain byte-identical: an FK-free connection table
and separate concurrent unique index with the native retry-cleanup registration.
Fork migrations remain in 900000+; upstream has not approached that namespace.
SQLC generation is checked against the combined schema; no copied/renumbered
migration or historical schema_migrations rewrite is introduced.

Qoder's provider credentials are an upstream-managed runtime contract, not
request-bound effective-credential evidence for Codex failover. No failover
producer approval, automatic route, recovery probe or real account is activated.
The existing R4 blocker and R5 real-environment smoke remain explicit. This is
compatibility work within R5, not a new delivery item or executor choice.

## Verification

Local targeted Go tests/vet and selected core/UI/view regressions are run against
the merged tree. Local DB-backed tests skip without PostgreSQL; they are not
integration proof. PR CI must pass native PostgreSQL 17/race service/handler/
hierarchy/migrations, generated SQLC, agents, Windows, selected full client
quality/build/tests and mobile compatibility before merge. Exact run results
and any corrections are recorded on the synchronization PR. No installed CLI,
provider inference or real-account smoke is implied by those checks. Browser
screenshots are not claimed from component tests or build output.

Post-merge checks must verify the exact validated tree, upstream ancestry,
closed Issue and clean local main. Later upstream advances are reviewed on their
merits; they do not create another top-level completion condition indefinitely.
