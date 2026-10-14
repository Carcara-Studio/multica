# ADR-003 — Hierarchy integrity and cycle prevention

- Status: Accepted
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #5, #13, #14
- Supersedes: None
- Superseded by: None

> **Implementation note (2026-10-14):** ADR-014 is authoritative where this ADR's earlier implementation proposals mentioned new foreign keys or cascades. The integrity model remains unchanged: Project-row serialization, row-local constraints/indexes and authoritative transactional service validation together enforce the hierarchy invariants under current upstream policy.

## Context

ADR-001 chooses a Project-scoped adjacency-list model for Agent hierarchy.

That model is only useful if structural invariants remain true under normal writes, concurrent writes, reparenting, removals and archival.

The fork must prevent:

- self-management;
- direct cycles;
- transitive cycles;
- cross-Project manager relationships;
- multiple active roots;
- non-root active nodes without a manager;
- concurrent writes that independently look valid but commit a cycle together.

Current Multica already uses PostgreSQL transactional locking patterns, including `FOR UPDATE` / `FOR UPDATE SKIP LOCKED`, for durable coordination in several subsystems. The hierarchy should use ordinary database transactions rather than an in-memory lock that would fail across server replicas.

## Decision Drivers

- Integrity must survive multiple server instances.
- Invalid hierarchy states should be prevented at write time, not repaired later.
- Structural writes are rare relative to hierarchy reads, so correctness is more important than maximizing concurrent hierarchy mutations in one Project.
- Database-local constraints/indexes should enforce simple row/uniqueness invariants where practical.
- Cross-row/cross-table invariants must be enforced by one authoritative hierarchy service inside a transaction.
- Avoid triggers or broad modifications to upstream central tables unless evidence shows they are necessary.
- Follow ADR-014 and current upstream migration/database conventions.

## Considered Alternatives

### Alternative A — Validate only in UI/API handlers

**Pros**

- Simple implementation.

**Cons**

- Different write paths can diverge.
- Races remain possible.
- CLI/internal callers could bypass assumptions.
- Harder to reason about correctness.

**Decision:** rejected.

### Alternative B — Recursive validation without serializing structural mutations

**Pros**

- Higher write concurrency.

**Cons**

Two concurrent transactions can both observe a valid tree and create a cycle. Example:

```text
Tx A: set A.manager = B
Tx B: set B.manager = A
```

Without a shared serialization point both validations may pass before either commit is visible.

**Decision:** rejected.

### Alternative C — Database trigger implementing all hierarchy rules

**Pros**

- Central DB enforcement.

**Cons**

- Recursive/business rules become hidden in trigger code.
- Harder migrations, testing and upstream maintenance.
- Increased coupling to PostgreSQL-specific implementation details.

**Decision:** rejected for v1.

### Alternative D — DB-local constraints/indexes plus Project-level transactional serialization for structural writes

**Pros**

- Strong correctness.
- Works across server replicas.
- Simple concurrency model.
- Complex checks remain explicit in service code.
- Limited fork footprint.
- Conforms to current upstream no-new-FK relationship ownership policy.

**Cons**

- Only one structural mutation per Project proceeds at a time.
- Requires disciplined lock ordering for every hierarchy mutation.
- Application code must prove relationship existence/scope/cleanup invariants that an FK would otherwise help enforce.

**Decision:** accepted.

## Decision

All structural hierarchy mutations for a Project must execute through one authoritative Project-hierarchy service and serialize on the Project row.

Conceptual write flow:

```text
BEGIN
  lock project row FOR UPDATE
  load affected memberships
  validate workspace/project/agent state
  validate root/manager invariants
  run recursive cycle check
  apply mutation
  revalidate resulting active tree invariants
COMMIT
```

### Required active-tree invariants

For each Project:

1. Zero active memberships means hierarchy is not configured.
2. One or more active memberships means exactly one active root.
3. The active root has `manager_membership_id = NULL`.
4. Every active non-root membership has one active direct manager.
5. The manager membership belongs to the same Project.
6. A membership cannot manage itself.
7. The graph is acyclic.
8. An Agent has at most one membership row for a Project; reactivation reuses the row rather than creating duplicate historical rows.
9. Agent and Project must belong to the same Workspace.
10. Only eligible user Agents may be active hierarchy members.

### Database-local enforcement under ADR-014

Names remain implementation proposals and must follow the then-current migration conventions.

The database should enforce invariants that are row-local or uniqueness-based without adding new relationship FKs/cascades:

```text
CHECK (manager_membership_id IS NULL OR manager_membership_id <> id)
UNIQUE INDEX (project_id, agent_id)
UNIQUE INDEX (project_id)
  WHERE manager_membership_id IS NULL
    AND archived_at IS NULL
```

Additional lookup indexes should support:

- membership by Project;
- membership by Agent/Project;
- active direct reports by `(project_id, manager_membership_id)`;
- Workspace cleanup by `workspace_id`.

Under current upstream policy every migration-created index is built with `CREATE [UNIQUE] INDEX CONCURRENTLY` in its own single-statement migration, with the migration runner's invalid-index cleanup convention applied.

The following contextual relationships are **not** delegated to new FKs:

- Project exists and is in the expected Workspace;
- Agent exists, is eligible and belongs to the same Workspace;
- manager membership exists, is active and belongs to the same Project;
- hierarchy rows are removed explicitly during Project/Workspace teardown.

Those rules are owned by the authoritative hierarchy service/query transaction and lifecycle cleanup paths.

The partial unique root index provides **at most one** active root. The service transaction enforces the stronger rule that a non-empty active hierarchy has **exactly one** root before commit.

### Cycle check

When changing membership `A` to report to proposed manager `B`:

- reject if `A == B`;
- recursively traverse descendants of `A` within the same Project;
- reject if `B` is in that descendant set.

Because all structural mutations for the Project first lock the same Project row, concurrent reparenting cannot both validate against a stale pre-mutation tree and commit a cycle.

### Reparenting and removal

No silent reparenting.

A manager/root with active descendants cannot be removed, archived as a membership, or assigned a new structural meaning that would orphan descendants unless the request explicitly supplies a valid replacement/reparent operation executed in the same transaction.

Changing the root is an explicit transactional operation that leaves exactly one root at commit.

Project deletion removes Project hierarchy rows explicitly in the existing locked Project deletion transaction. Workspace deletion removes hierarchy rows through the existing Workspace teardown graph. No cascade may silently restructure the hierarchy.

## Consequences

### Positive

- Deterministic integrity under concurrency.
- No distributed lock subsystem needed.
- Simple local invalid states are blocked by DB checks/unique indexes.
- More complex rules stay readable/testable in service code.
- Structural changes remain isolated from task runtime paths.
- Schema follows current upstream relationship/migration policy.

### Negative / Trade-offs

- Structural writes within one Project are serialized.
- The Project row becomes the hierarchy mutation lock point.
- Cross-table relationship integrity relies on authoritative service/query transactions rather than new FKs.
- Project/Workspace cleanup tests become mandatory.
- Bulk reorganization APIs must be transactional rather than a sequence of independent edits.

## Data Impact

Expected data/index behavior under ADR-014:

- one additive `project_agent_membership` table;
- no new Project/Agent/self foreign keys or cascades;
- row-local self-reference `CHECK`;
- concurrent unique Project/Agent membership index;
- concurrent partial unique active-root index;
- concurrent direct-report lookup index;
- concurrent Workspace cleanup index;
- explicit Project/Workspace cleanup queries integrated into existing deletion transactions.

Physical deletion of individual hierarchy rows should remain rare once structural behavior exists; archival/explicit cleanup is preferred while references exist. Project/Workspace teardown is explicit owner cleanup rather than cascade behavior.

## Backend / Runtime Impact

All hierarchy-changing handlers/commands call the same hierarchy service.

No runtime/daemon lock is involved. The transaction is server/database-side.

Read-only traversal can use recursive SQL without taking the Project mutation lock unless a consistent structural snapshot is required for a write decision.

## API / CLI / UI Impact

Mutation endpoints should expose conflicts explicitly, preferably as stable 409-style domain conflicts for:

- cycle detected;
- cross-Project manager;
- multiple-root attempt;
- orphaning removal;
- stale/invalid reparent target.

Bulk root replacement/reparent should be an explicit operation rather than a UI sequence that temporarily violates invariants.

## Failure and Edge Cases

- `A -> B` and concurrent `B -> A`: one transaction waits; after the first commits, the second revalidates and fails.
- Transitive cycle `A -> B -> C`, then `A.manager = C`: rejected.
- Manager from another Project: rejected by authoritative service validation before commit.
- Agent from another Workspace: rejected before membership mutation.
- Missing/archived manager: rejected before membership mutation.
- Remove only root from a non-empty hierarchy: rejected unless the same transaction empties the hierarchy intentionally through an explicit operation.
- Reparent manager while descendants exist: allowed only if resulting graph remains connected and acyclic.
- Concurrent root creation: Project lock plus partial unique root index prevents two active roots.
- Project/Workspace delete racing hierarchy writes: lifecycle lock protocol and explicit cleanup must prevent orphan rows.

## Testing Requirements

### Unit/service

- self-reference;
- direct cycle;
- transitive cycle;
- cross-Project manager;
- cross-Workspace Agent;
- missing manager;
- archived manager;
- root replacement;
- removal with descendants;
- explicit reparent.

### Database/concurrency

- self-reference check;
- unique `(project_id, agent_id)`;
- active-root partial uniqueness;
- expected concurrent index shapes;
- concurrent `A -> B` / `B -> A`, exactly one succeeds;
- concurrent root creation, at most one succeeds;
- rollback leaves prior tree unchanged;
- failed bulk reorganization is atomic;
- Project deletion removes hierarchy rows atomically;
- Workspace deletion removes hierarchy rows;
- cleanup cannot cross Workspace boundaries;
- deletion/write races do not leave orphan hierarchy rows.

## Upstream Compatibility

The lock uses the existing Project row rather than adding a new global synchronization subsystem.

The design adds only fork-owned table/check/index/query surfaces where possible, follows current upstream application-owned relationship policy, uses concurrent-index migration conventions, avoids triggers and avoids modifying central upstream Agent/Project schema merely to support hierarchy.

ADR-014 governs implementation mechanics when this ADR and current upstream repository rules differ.

If upstream later introduces a Project membership transaction/locking convention, the hierarchy service should converge toward it.

## Open Questions

- Exact error codes/names should follow current Multica domain-error conventions at implementation time.
- Whether v1 needs a dedicated bulk-reorganization endpoint or only explicit root-replacement plus reparent operations should be decided from the first UI/API design, without weakening the atomicity rule.
