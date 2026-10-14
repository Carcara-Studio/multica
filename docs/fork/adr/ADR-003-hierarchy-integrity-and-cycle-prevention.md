# ADR-003 — Hierarchy integrity and cycle prevention

- Status: Proposed
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #5
- Supersedes: None
- Superseded by: None

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
- Database constraints should enforce simple invariants where practical.
- More contextual invariants may be enforced by one authoritative hierarchy service inside a transaction.
- Avoid triggers or broad modifications to upstream central tables unless evidence shows they are necessary.

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

### Alternative D — DB constraints for local invariants plus Project-level transactional serialization for structural writes

**Pros**

- Strong correctness.
- Works across server replicas.
- Simple concurrency model.
- Complex checks remain explicit in service code.
- Limited fork footprint.

**Cons**

- Only one structural mutation per Project proceeds at a time.
- Requires disciplined lock ordering for every hierarchy mutation.

**Decision:** accepted design, pending ADR approval.

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

### Proposed database constraints

Names are proposals and must be adapted to current migration conventions.

```text
UNIQUE(project_id, agent_id)
UNIQUE(project_id, id)
CHECK (manager_membership_id IS NULL OR manager_membership_id <> id)
```

Use a same-Project composite self-reference conceptually equivalent to:

```text
FOREIGN KEY (project_id, manager_membership_id)
REFERENCES project_agent_membership(project_id, id)
```

Use a partial unique index for the active root conceptually equivalent to:

```text
UNIQUE(project_id)
WHERE manager_membership_id IS NULL
  AND archived_at IS NULL
```

This DB index provides **at most one** active root. The service transaction enforces the stronger rule that a non-empty active hierarchy has **exactly one** root before commit.

Project/Agent Workspace equality is validated transactionally through the authoritative service in v1. We intentionally avoid adding new composite unique keys to upstream `project` or `agent` solely to support a composite Workspace FK unless implementation-time inspection shows a clean additive way to do so.

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

## Consequences

### Positive

- Deterministic integrity under concurrency.
- No distributed lock subsystem needed.
- Most simple invalid states are blocked by DB constraints.
- More complex rules stay readable/testable in service code.
- Structural changes remain isolated from task runtime paths.

### Negative / Trade-offs

- Structural writes within one Project are serialized.
- The Project row becomes the hierarchy mutation lock point.
- Workspace equality relies on service enforcement in v1 rather than a single DB FK.
- Bulk reorganization APIs must be transactional rather than a sequence of independent edits.

## Data Impact

Expected indexes:

- Project membership lookup;
- Agent membership lookup;
- direct reports by `manager_membership_id`;
- partial unique active root;
- composite uniqueness required by same-Project manager FK.

FK delete behavior should avoid silent structural mutation. In particular, manager membership references should not use `ON DELETE SET NULL`, because that could accidentally create a second root or orphaned node.

Physical deletion of hierarchy rows should be rare; archival/explicit cleanup is preferred while references exist.

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
- Manager from another Project: rejected by composite manager relation/service validation.
- Agent from another Workspace: rejected before membership mutation.
- Remove only root from a non-empty hierarchy: rejected unless the same transaction empties the hierarchy intentionally through an explicit operation.
- Reparent manager while descendants exist: allowed only if resulting graph remains connected and acyclic.
- Archived membership selected as manager: rejected.
- Concurrent root creation: partial unique root index plus Project lock prevents two active roots.

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

- concurrent `A -> B` / `B -> A`, exactly one succeeds;
- concurrent root creation, at most one succeeds;
- same-Project composite manager FK;
- root partial uniqueness;
- rollback leaves prior tree unchanged;
- failed bulk reorganization is atomic.

## Upstream Compatibility

The lock uses the existing Project row rather than adding a new global synchronization subsystem.

The design adds constraints/indexes only to the new fork table where possible. It avoids triggers and avoids modifying central upstream Agent/Project schema merely to support hierarchy.

If upstream later introduces a Project membership transaction/locking convention, the hierarchy service should converge toward it.

## Open Questions

- Exact error codes/names should follow current Multica domain-error conventions at implementation time.
- Whether v1 needs a dedicated bulk-reorganization endpoint or only explicit root-replacement plus reparent operations should be decided from the first UI/API design, without weakening the atomicity rule.
