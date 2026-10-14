# ADR-006 — Agent archive and hierarchy consistency

- Status: Proposed
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #5
- Supersedes: None
- Superseded by: None

## Context

Project hierarchy creates structural dependencies on Agents. Archiving an Agent therefore cannot be treated as an unrelated lifecycle action when that Agent is an active root, manager or leaf in one or more Project hierarchies.

Current Multica Agent archival is represented by `agent.archived_at`. `server/pkg/db/queries/agent.sql` contains `ArchiveAgent`, which sets `archived_at`, `archived_by` and `updated_at`. Current code also contains more than one archival path, including runtime/workspace-management flows.

The hierarchy must never appear structurally valid while referencing an archived Agent as an active organizational node.

## Decision Drivers

- Active hierarchy nodes must always refer to usable, non-archived organizational Agents.
- Removing a manager/root must not silently reparent descendants.
- Agent archive can affect multiple Projects at once.
- Archive/restore behavior must be deterministic and auditable.
- Existing Multica Agent archival semantics should remain unchanged for Agents that do not participate in active hierarchy.
- The implementation must account for every upstream path capable of archiving user Agents, not only the primary HTTP handler.

## Considered Alternatives

### Alternative A — Allow Agent archive and leave active memberships unchanged

**Pros**

- No archive-path integration required.

**Cons**

- Active hierarchy can reference an unusable Agent.
- Delegation/escalation chains become ambiguous.
- Root/manager invariants become only nominally true.

**Decision:** rejected.

### Alternative B — Automatically reparent descendants to the archived Agent's manager

**Pros**

- Archive usually succeeds without user intervention.

**Cons**

- Changes organizational authority silently.
- Root archive has no obvious parent.
- Can create unexpected reporting structures across many Projects.
- Hard to audit intent.

**Decision:** rejected.

### Alternative C — Fail closed for structural nodes; automatically deactivate leaf memberships

**Pros**

- Preserves explicit organization changes.
- Leaf archival remains ergonomic.
- No silent manager replacement.
- Consistent with hierarchy integrity rules.

**Cons**

- Archiving a manager/root requires prior hierarchy repair.
- Every Agent archival path that can affect hierarchy must use the guard.

**Decision:** accepted design, pending ADR approval.

## Decision

No archive operation may commit if it would leave an active Project hierarchy referencing an archived Agent.

### Root or manager Agent

If the Agent is an active hierarchy root or has active direct/indirect descendants in any Project, archival is rejected unless the same explicit transaction first performs an approved hierarchy reorganization that removes the structural dependency.

No silent reparenting is permitted.

Examples of valid explicit preparation:

- reparent all direct reports and then archive the former manager membership;
- replace the Project root transactionally;
- intentionally remove the entire Project hierarchy if product policy permits it.

### Leaf Agent

If the Agent participates only as active non-root leaf memberships, Agent archival may proceed and those active memberships are archived/deactivated atomically with the Agent archive.

If the same Agent is a leaf in one Project but a manager/root in another, the whole Agent archive fails until all blocking Project structures are repaired.

### Restore behavior

Restoring an Agent does **not** automatically reactivate prior Project memberships.

Reason:

- the Project may have been reorganized while the Agent was archived;
- old manager relationships may no longer be valid;
- silent restoration could create cycles or a second root.

Re-entry into a Project hierarchy is an explicit membership reactivation/reassignment operation validated under ADR-003.

### Membership removal independent of Agent archive

Removing/archiving a hierarchy membership follows the same structural rule:

- leaf can be removed/deactivated;
- manager/root cannot be removed while active descendants depend on it unless an explicit same-transaction reorganization is supplied.

### Multiple archival paths

Implementation must audit every current upstream path that can set `agent.archived_at` for user Agents.

All such paths must either:

1. use the hierarchy archive guard/transaction; or
2. be proven to occur only as part of deleting the containing Project/Workspace in an order that removes hierarchy state safely.

A fork implementation is incomplete if only the primary `ArchiveAgent` handler is protected while a batch/runtime-revocation path can bypass the invariant.

## Consequences

### Positive

- No active hierarchy points to archived organizational Agents.
- No silent reparenting.
- Cross-Project impact of Agent archive is handled consistently.
- Restore cannot resurrect stale structure.
- Existing archive behavior stays unchanged for non-hierarchy Agents.

### Negative / Trade-offs

- Manager/root archive can return a conflict and require organization repair first.
- Archive implementation may need integration with several upstream lifecycle paths.
- Leaf membership deactivation and Agent archive must be atomic.

## Data Impact

Hierarchy membership needs an explicit active/archived concept; the current proposal uses `archived_at` on `project_agent_membership`.

For leaf Agent archival:

```text
BEGIN
  lock Agent / affected hierarchy Projects in deterministic order
  validate Agent is not active root/manager anywhere
  archive active leaf memberships
  archive Agent
COMMIT
```

Exact lock ordering must be designed to avoid deadlocks with existing Agent lifecycle transactions. Project locks should follow a deterministic Project-ID ordering when one Agent belongs to several Projects.

No `ON DELETE SET NULL` manager behavior is allowed as a substitute for lifecycle handling.

## Backend / Runtime Impact

A hierarchy-aware archive guard must be callable from all relevant Agent archival services.

The guard should return a domain conflict containing affected Projects/memberships so UI/CLI can explain required reorganization.

Runtime/daemon behavior should not attempt to compensate by skipping an archived manager or routing to a different hierarchy node.

## API / CLI / UI Impact

Existing archive API can remain backward compatible for Agents with no blocking hierarchy role.

When blocked, API/CLI/UI should explain that the Agent is an active manager/root and list the affected Project(s) sufficiently for the user to repair them.

Do not silently perform reparenting from the archive confirmation dialog.

Restore UI should not imply that prior Project positions were automatically restored.

## Failure and Edge Cases

- Agent is root of a one-node hierarchy: archive rejected unless the hierarchy is explicitly removed/replaced.
- Agent is manager in Project A and leaf in Project B: archive rejected because Project A blocks it.
- Agent is leaf in several Projects: all active leaf memberships deactivate atomically, then Agent archives.
- Concurrent reparent and archive: Project/Agent locking and revalidation must produce one valid outcome; no orphaned active membership.
- Batch archive includes one blocking manager: behavior must be atomic or explicitly per-Agent according to the upstream batch contract, but no blocking Agent may commit archived with active hierarchy references.
- Workspace/Project deletion: hierarchy cleanup may proceed with containing deletion if ordering makes the hierarchy irrelevant before Agent archival; implementation must verify the real deletion transaction.
- Agent restored later: previous memberships stay inactive.

## Testing Requirements

### Unit/service

- non-hierarchy Agent archives normally;
- leaf Agent archives and memberships deactivate;
- manager archive rejected;
- root archive rejected;
- multi-Project Agent blocked if any Project contains structural dependency;
- restore does not reactivate memberships.

### Database/integration

- leaf membership updates and Agent archive are atomic;
- failed archive rolls back membership changes;
- concurrent archive/reparent cannot leave orphaned structure;
- deterministic locking across multiple Projects avoids deadlock in concurrency tests;
- every audited Agent archival path enforces equivalent hierarchy consistency.

### Regression

- existing Agent archive/restore behavior remains unchanged for Agents outside hierarchy;
- existing Squad archived-leader fail-closed behavior is not replaced by hierarchy rerouting.

## Upstream Compatibility

This decision does not change the Agent schema solely for hierarchy. It adds an integration guard around lifecycle writes and stores Project-specific state in the new hierarchy domain.

The main merge risk is touching upstream Agent archival flows. Integration should therefore be centralized behind a small hierarchy service hook instead of duplicating hierarchy checks in handlers.

Before implementation, re-inspect all current `archived_at` writers because upstream evolves rapidly.

If upstream introduces a lifecycle-hook mechanism, prefer registering hierarchy consistency there rather than keeping several fork-specific call sites.

## Open Questions

- Exact current transaction boundaries and lock order of all Agent archival writers must be mapped during HIER-010 before code changes.
- Batch archive error UX should follow the existing upstream batch contract while preserving the no-invalid-commit invariant.
