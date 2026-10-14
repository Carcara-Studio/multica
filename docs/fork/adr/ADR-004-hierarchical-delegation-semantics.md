# ADR-004 — Hierarchical delegation semantics

- Status: Proposed
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #5
- Supersedes: None
- Superseded by: None

## Context

Multica already supports decomposing work into child Issues and waking the parent assignee when child work completes. Current Issue wakeup documentation makes clear that the platform records facts and re-runs the target Agent; business completion remains Agent-managed.

Current Squad behavior also already uses the leader as coordinator rather than automatically fanning work out to every Squad member.

The fork needs Project hierarchy to support downward delegation without creating a second task system, bypassing existing Issue semantics, or introducing uncontrolled recursive execution.

## Decision Drivers

- Reuse existing Issue/sub-issue work representation.
- Keep delegation visible and auditable as normal Multica work.
- Enforce Project hierarchy authority server-side.
- Preserve existing `@mention` collaboration behavior.
- Avoid automatic fan-out or recursive server orchestration.
- Prevent duplicate delegation and re-trigger loops.
- Preserve parent status as Agent-managed, consistent with upstream.

## Considered Alternatives

### Alternative A — Treat every `@mention` of a subordinate as hierarchical delegation

**Pros**

- Reuses an existing interaction.

**Cons**

- Mentions are general collaboration primitives.
- Existing mentions can cross roles and do not imply manager authority.
- Hard to distinguish casual consultation from accountable delegated work.
- Would change existing mention semantics.

**Decision:** rejected.

### Alternative B — Add a completely separate hierarchy task table/runtime

**Pros**

- Could encode hierarchy-specific lifecycle precisely.

**Cons**

- Duplicates Issue/task behavior.
- Creates a second completion/wakeup model.
- High implementation and upstream maintenance cost.

**Decision:** rejected.

### Alternative C — Hierarchical delegation creates/assigns child Issues through a validated hierarchy action

**Pros**

- Reuses Issue, assignment, comments, status and wakeup infrastructure.
- Work remains visible in existing product surfaces.
- Authority can be validated explicitly.
- Completion can reuse parent wakeups.

**Cons**

- Requires an additive hierarchy-aware delegation API/CLI action.
- Idempotency and lineage must be made explicit.

**Decision:** accepted design, pending ADR approval.

## Decision

Hierarchical delegation is an explicit, server-validated action that delegates work **down one direct-report edge at a time** and represents the delegated work as a child Issue using existing Multica Issue semantics.

A hierarchy delegation is valid only when:

1. the source/parent Issue has a non-null `project_id`;
2. the delegating Agent has an active membership in that Project;
3. the target Agent has an active membership in that Project;
4. the target membership's direct manager is the delegator's membership;
5. the target is currently eligible for normal Multica assignment/invocation under existing access/runtime rules;
6. the operation can be settled idempotently.

The child Issue remains a normal Multica Issue. Existing assignee behavior remains authoritative after creation/assignment.

### One-hop rule

A manager can hierarchically delegate only to a direct report.

If work needs to reach a deeper descendant:

```text
Manager
  -> Direct Report / Lead
      -> that Lead's Direct Report
```

This preserves organizational responsibility and avoids the server silently skipping hierarchy levels.

### Mentions remain collaboration

Existing Agent/Squad mentions keep upstream semantics.

A mention of a descendant, peer, manager or external collaborator is not automatically recorded as hierarchical delegation.

The fork may expose both:

- ordinary collaboration (`@mention`, ordinary assignment where upstream permits it);
- governed hierarchical delegation.

Only the latter receives hierarchy authority/lineage semantics.

### Parent completion

The server must not auto-complete the parent Issue because a child finishes.

Existing child/stage wakeup behavior should wake the responsible manager, who reads current state and decides whether to:

- accept result;
- delegate follow-up;
- continue own work;
- escalate;
- move the parent toward review/completion.

### Idempotency and lineage

A delegation request must have a stable idempotency identity so retried API calls or repeated Agent turns do not create duplicate child Issues for the same delegation operation.

The exact storage mechanism is intentionally deferred until implementation-time inspection of current Issue creation/idempotency facilities. Do **not** invent a parallel ledger if upstream already provides an appropriate idempotency primitive.

At minimum, implementation must be able to distinguish:

- a retry of the same delegation operation;
- a deliberate new delegation to the same subordinate;
- a later follow-up child Issue.

### No automatic recursive delegation

The server validates and records one delegation action. It does not automatically ask the subordinate to further delegate because deeper hierarchy exists.

Any further delegation is a new Agent decision and a new validated action.

## Consequences

### Positive

- Reuses existing Issue/task/wakeup product model.
- Delegation remains human-visible and auditable.
- Organization authority is explicit rather than inferred from prompts.
- One-hop delegation naturally preserves manager accountability.
- Existing mentions and ordinary assignments remain backward compatible.

### Negative / Trade-offs

- Deep delegation requires multiple Agent turns/actions.
- Requires explicit idempotency handling.
- Hierarchy-only delegation can fail even where an ordinary collaboration path would be possible.

## Data Impact

No separate hierarchy-task table is introduced by this ADR.

Potential additive persistence for delegation lineage/idempotency may be required, but the exact representation is not decided until existing upstream Issue creation and metadata/idempotency mechanisms are inspected at implementation time.

Do not overload `squad_member.role` or hierarchy membership rows with per-delegation execution state.

## Backend / Runtime Impact

Add a hierarchy service action conceptually equivalent to:

```text
Delegate(parentIssue, delegatorAgent, directReport, workSpec, idempotencyKey)
```

The service validates hierarchy membership/authority, then delegates through existing Issue creation/assignment services rather than directly enqueueing an ad hoc runtime task.

Normal Multica assignment/readiness/access checks remain in force.

Runtime/daemon behavior should not need a separate execution protocol for hierarchy delegation.

## API / CLI / UI Impact

Expose an additive hierarchy-aware delegation command/action rather than changing the meaning of generic Issue create/assign or mention APIs.

The UI should distinguish:

- hierarchical delegation;
- ordinary collaboration/assignment.

A future CLI may expose a command under Project/Issue hierarchy conventions, but exact syntax is deferred until implementation.

## Failure and Edge Cases

- Target is a descendant but not direct report: hierarchical delegation rejected; manager should delegate through the chain.
- Target is a peer or manager: rejected as downward hierarchical delegation.
- Target belongs to another Project: rejected.
- Parent Issue has no Project: hierarchy delegation unavailable.
- Delegator is not in Project hierarchy: unavailable.
- Target Agent is archived/unroutable: fail closed using existing eligibility semantics.
- Same network request is retried: must not create duplicate child work.
- Child completes while manager run is active: rely on existing wakeup/coalescing behavior rather than starting an uncontrolled parallel manager loop.
- Manager repeatedly receives completion and tries the same delegation again: idempotency identity prevents accidental duplicate replay, while a deliberate follow-up can use a new identity.
- Delegation to Squad: may still use normal upstream assignment/collaboration, but it is not a direct-report hierarchical delegation action.

## Testing Requirements

### Authority

- direct-report delegation succeeds;
- descendant-but-not-direct-report delegation fails;
- peer/upward/cross-Project delegation fails;
- missing Project membership fails;
- Issue without Project cannot use hierarchy delegation.

### Integration

- child Issue is created/assigned through existing Issue services;
- normal assignment readiness/access rules still apply;
- child completion wakes manager using existing mechanisms;
- parent is not auto-completed by server;
- Squad/mention behavior is unchanged.

### Idempotency/concurrency

- repeated identical request creates one logical delegation;
- two concurrent retries settle to one logical child;
- explicit later follow-up can create separate child work.

## Upstream Compatibility

This decision deliberately builds on existing Issue/sub-issue/wakeup behavior and avoids a new execution subsystem.

Implementation should prefer a small hierarchy validation wrapper around existing Issue service calls.

If upstream later introduces native delegation/idempotency metadata, the fork should use it rather than preserve a fork-specific ledger.

## Open Questions

- Which current Issue creation path provides the best stable idempotency hook must be re-inspected immediately before implementation.
- Exact metadata presented in manager/subordinate briefings should be designed in HIER-006/HIER-007 without altering the one-hop authority rule.
