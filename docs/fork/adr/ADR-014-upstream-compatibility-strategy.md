# ADR-014 — Upstream compatibility strategy

- Status: Accepted
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #13, #14
- Supersedes: None
- Superseded by: None

## Context

The fork exists under a stronger constraint than ordinary feature development: it must continuously absorb changes from `multica-ai/multica` without turning fork-only behavior into a parallel architecture.

Implementation-time revalidation against upstream `main` at `52b26900c51f759b87707b4ea35fa9f141b83b52` found repository rules that materially affect how accepted hierarchy/failover designs must be implemented.

Current upstream `AGENTS.md` states under **Database and Migration Rules**:

- do not add foreign keys, cascading deletes, or cascading updates;
- validate relationships and clean up dependents in application code, using a transaction when atomicity is required;
- every migration-created index, including indexes on new tables, uses `CREATE [UNIQUE] INDEX CONCURRENTLY`;
- each concurrent index build has its own single-statement migration;
- SQL changes regenerate sqlc.

Current upstream code follows that policy in newer subsystems. For example:

- `server/migrations/558_issue_child_event.up.sql` explicitly stores application-owned relations without FKs;
- `server/internal/handler/project.go` deletes a Project inside a transaction after `LockProjectForDelete` and explicit Project-owned cleanup;
- `server/internal/handler/workspace.go` maintains an application-owned Workspace teardown graph under `LockWorkspaceForDelete`;
- `server/cmd/migrate/main.go` tracks concurrent-index invalid-build cleanup so interrupted index migrations are retry-safe.

Earlier accepted fork ADRs were written from domain requirements before this current repository policy was revalidated. ADR-001 and ADR-003 correctly require Project-scoped membership, same-Project manager semantics, root uniqueness, workspace equality, cycle prevention and Project-level transactional serialization. However, their text also describes foreign keys — especially a same-Project composite self-reference — as a possible/expected implementation mechanism.

The semantic invariants remain required. The FK mechanism does not.

## Decision Drivers

- Future upstream synchronization is a first-class architectural constraint.
- Fork features should follow current upstream repository invariants unless doing so makes a required feature impossible.
- Domain integrity must not weaken merely because a database FK is not used.
- Relationship ownership and cleanup must remain explicit and transactional.
- New migrations must obey upstream migration-runner behavior rather than introducing a fork-only migration style.
- Older accepted ADRs should retain their domain decisions while implementation details adapt to newer evidence.

## Considered Alternatives

### Alternative A — Keep the FK design from earlier hierarchy ADRs

**Pros**

- Database directly rejects missing/cross-table relationships.
- Familiar relational model.

**Cons**

- Directly violates current upstream repository instructions.
- Reintroduces cascade/FK coupling that newer upstream subsystems deliberately moved into application code.
- Increases migration and future-sync divergence.
- Would require special exceptions for the fork before implementation has shown they are necessary.

**Decision:** rejected.

### Alternative B — Remove all database constraints and rely entirely on handlers

**Pros**

- Minimal DDL.

**Cons**

- Handler-only validation can be bypassed by internal/CLI/future write paths.
- Simple local invariants would no longer be enforced atomically.
- Concurrency correctness would depend on every caller independently reproducing rules.

**Decision:** rejected.

### Alternative C — Follow current upstream application-owned relationship policy while retaining DB-local constraints

Use:

- `CHECK` constraints for row-local invariants;
- concurrent unique/partial indexes for uniqueness/root constraints;
- authoritative transactional services for cross-row/cross-table relationships;
- explicit Project/Workspace cleanup in existing deletion transactions;
- existing upstream lock/fencing conventions for concurrent lifecycle operations.

**Pros**

- Complies with current upstream rules.
- Preserves domain correctness.
- Keeps fork behavior additive and legible.
- Reuses upstream migration and teardown patterns.
- Reduces future merge/sync risk.

**Cons**

- More correctness responsibility lives in service/query code.
- Every create/update/delete path must participate in the same transaction/lock protocol.
- Tests must prove relationship and cleanup invariants that an FK would otherwise provide automatically.

**Decision:** accepted.

## Decision

### 1. Current upstream repository rules are implementation constraints

Fork implementation must obey current upstream `AGENTS.md` and repository conventions unless a later accepted ADR documents why a specific exception is required.

This includes database/migration rules, generated-code workflows, test placement and compatibility rules.

### 2. Do not add new hierarchy/failover foreign keys or cascades under the current upstream policy

New fork tables may store relationship identifiers such as:

```text
workspace_id
project_id
agent_id
manager_membership_id
```

but those relations are application-owned.

The authoritative service/query transaction validates existence, scope and lifecycle state before committing structural mutations.

### 3. Preserve database-local invariants

The no-FK policy does not mean "no database integrity".

Use row-local checks and unique/partial indexes where they encode invariants without contradicting upstream rules.

For Project Hierarchy this includes, conceptually:

```text
CHECK (manager_membership_id IS NULL OR manager_membership_id <> id)
UNIQUE INDEX (project_id, agent_id)
UNIQUE INDEX (project_id)
  WHERE manager_membership_id IS NULL AND archived_at IS NULL
```

The authoritative hierarchy service remains responsible for the stronger contextual rules:

- Project/Agent Workspace equality;
- manager exists and is active;
- manager belongs to the same Project;
- exactly one root for any non-empty active hierarchy;
- cycle prevention;
- manager/root removal/reparenting semantics.

### 4. Structural hierarchy writes serialize on the Project row

ADR-003 remains authoritative for concurrency semantics.

Conceptual flow remains:

```text
BEGIN
  lock Project row
  validate Project/Workspace/Agent/membership state
  validate manager/root/cycle invariants
  mutate hierarchy rows
  revalidate resulting tree
COMMIT
```

The lack of a self-FK does not change this transaction boundary.

### 5. Relationship cleanup joins existing upstream deletion graphs

A fork-owned row must not outlive its owner merely because no FK exists.

Project-scoped hierarchy rows are deleted inside the existing Project deletion transaction after the Project row has been locked and before the Project is removed.

Workspace-scoped hierarchy rows are deleted inside the existing Workspace teardown graph while the Workspace deletion lock is held.

Do not create a second fork-only lifecycle/deletion subsystem.

### 6. New indexes follow upstream concurrent-index migrations

Every new migration-created index:

- uses `CREATE [UNIQUE] INDEX CONCURRENTLY`;
- is the only statement in its migration file;
- has the corresponding down migration behavior required by the migration runner;
- participates in upstream invalid-index cleanup registration/tests where required.

Do not hide new indexes inside `CREATE TABLE ... UNIQUE (...)` or non-concurrent `CREATE INDEX` clauses.

### 7. Earlier ADR implementation mechanisms adapt to this ADR

This ADR does **not** supersede the domain decisions of ADR-001 or ADR-003.

It takes precedence only where those ADRs describe new FK/cascade mechanisms that conflict with current upstream repository rules.

In particular, after ADR-014 is accepted:

- ADR-001's expected `project`/`agent`/manager FK bullets are historical implementation proposals, not implementation authority;
- ADR-003's conceptual same-Project composite manager FK and its FK-specific test/index expectations are replaced by transactional service validation plus DB-local checks/indexes;
- all required hierarchy invariants remain mandatory.

The same principle applies to later failover implementation: relation identifiers may be persisted, but new FKs/cascades are not introduced while upstream policy forbids them.

### 8. Migration numbers are provisional until implementation-time revalidation

A migration number may be proposed only after inspecting the current upstream migration head.

It must be checked again immediately before the product PR is integrated. If upstream occupies the number meanwhile, fork migrations are renumbered rather than creating a permanent collision.

## Consequences

### Positive

- Fork schema follows the same modern relationship ownership model as upstream.
- Domain invariants remain explicit and testable.
- Project/Workspace lifecycle behavior stays in existing transactions.
- Migration conflicts with upstream are reduced.
- Future upstream contribution is more plausible because the fork does not introduce a conflicting FK/cascade style.

### Negative / Trade-offs

- Cross-table integrity is not automatically enforced by PostgreSQL foreign keys.
- Service/write-path discipline becomes critical.
- Project/Workspace cleanup tests become mandatory for every new owner relationship.
- New concurrent indexes require more migration files and migration-runner bookkeeping.

## Data Impact

For HIER-001, the preferred minimal table is currently:

```text
project_agent_membership
------------------------
id UUID PK
workspace_id UUID NOT NULL
project_id UUID NOT NULL
agent_id UUID NOT NULL
manager_membership_id UUID NULL
title TEXT NOT NULL DEFAULT ''
archived_at TIMESTAMPTZ NULL
created_at TIMESTAMPTZ NOT NULL
updated_at TIMESTAMPTZ NOT NULL
```

No FK/backfill is required.

`operational_role`, `sort_order` and stored hierarchy depth are deferred unless concrete consumers justify them.

Required uniqueness/root/direct-report/workspace indexes are separate concurrent migrations.

## Backend / Runtime Impact

Backend services own relationship validation and lifecycle cleanup.

The hierarchy service will be the only authority for hierarchy structural writes once HIER-002 begins.

MFK-007/ADR-014 itself changes no runtime/daemon/task behavior.

## API / CLI / UI Impact

None for this ADR.

Future APIs/CLI/UI must call the authoritative backend hierarchy service rather than writing membership rows through parallel logic.

Existing clients remain unaffected until additive hierarchy surfaces are introduced.

## Failure and Edge Cases

- cross-Workspace Agent inserted into a Project hierarchy: service rejects before commit;
- manager from another Project: service rejects before commit;
- concurrent cycle attempts: Project-row serialization + revalidation resolves the race;
- concurrent root creation: Project-row serialization plus partial unique root index protects the invariant;
- Project delete racing a hierarchy write: both participate in Project locking and the delete cleanup transaction;
- Workspace delete racing a hierarchy write: the future write path must participate in the existing Workspace lifecycle fence/lock protocol as required by current upstream deletion rules;
- interrupted concurrent index build: upstream migration cleanup convention must make retry safe;
- upstream claims a fork migration number before merge: renumber fork migration files and registrations before integration.

## Testing Requirements

### Migration/database

- every concurrent index is created in its own single-statement migration;
- invalid-index cleanup registration remains complete;
- row-local self-reference check works;
- unique Project/Agent membership works;
- active-root partial uniqueness works;
- migration up/down behavior follows current repository harness.

### Backend/integration

- cross-Workspace Agent rejected;
- cross-Project manager rejected;
- missing/archived manager rejected;
- Project deletion removes hierarchy rows in the same transaction;
- Workspace deletion removes hierarchy rows;
- cleanup cannot cross tenant/Workspace boundaries;
- rollback leaves prior hierarchy intact.

### Concurrency

- concurrent reparent cycle attempt: at most one valid structural result;
- concurrent root creation: at most one active root;
- deletion/write races do not produce orphan hierarchy rows.

### Regression

- Projects without hierarchy preserve upstream behavior;
- Squad routing remains unchanged;
- Issue/task/daemon behavior is untouched by schema-only work.

## Upstream Compatibility

This ADR formalizes the rule that upstream compatibility is not only a Git strategy; current upstream engineering contracts also constrain fork implementation.

When upstream changes a convention materially:

1. inspect the new behavior and rationale;
2. determine whether an accepted fork ADR depends on the old mechanism;
3. preserve the fork's semantic requirement where still needed;
4. adapt the implementation toward upstream rather than freezing stale mechanics;
5. record a new/superseding ADR when the architectural consequence is material.

If upstream later reintroduces/standardizes relational FKs for comparable domains, the fork may reconsider this mechanism through a new ADR instead of retaining this policy dogmatically.

## Open Questions

- The exact Workspace write-fence call that hierarchy mutations should join must be chosen during HIER-002 after inspecting the then-current Project/Workspace lifecycle queries.
- Exact migration names/numbers remain provisional until HIER-001 starts from the then-current upstream migration head.
