# ADR-005 — Escalation semantics

- Status: Accepted
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #5
- Supersedes: None
- Superseded by: None

## Context

The Project hierarchy must support escalation in the inverse direction of delegation.

Delegation moves responsibility downward through direct reports. Escalation is needed when an Agent cannot safely continue, lacks authority/capability, encounters a blocking decision, or determines that a higher organizational level must decide.

Without explicit semantics, escalation could become an uncontrolled reassignment mechanism, skip managers, create loops, or silently route around unavailable managers.

The existing Project model already has an optional `lead_type` / `lead_id`, but that relationship is not the same as the operational hierarchy root. ADR-001 keeps these concepts separate.

## Decision Drivers

- Escalation should preserve the reporting chain.
- Intermediate managers should retain responsibility and context.
- The server must not silently skip unavailable managers.
- Root-level escalation must terminate at a human/project intervention boundary rather than loop back into the hierarchy.
- Existing Issue/task semantics should be reused where practical.
- Escalation must not imply automatic replay or reassignment of partially executed work.

## Considered Alternatives

### Alternative A — Escalate directly to Project root from any node

**Pros**

- Fast path to the highest authority.

**Cons**

- Skips intermediate managers.
- Breaks organizational accountability.
- Root becomes a bottleneck.
- Loses the value of multi-level hierarchy.

**Decision:** rejected.

### Alternative B — Escalate to the first available ancestor, skipping unavailable managers

**Pros**

- Keeps work moving.

**Cons**

- Availability becomes an implicit organization rewrite.
- Can hide structural breakage.
- Different runtime health states could produce inconsistent authority chains.

**Decision:** rejected.

### Alternative C — Escalate one direct-manager edge at a time, fail closed on structural breakage

**Pros**

- Mirrors downward one-hop delegation.
- Preserves accountability.
- Makes escalation visible at each level.
- Structural problems are surfaced rather than bypassed.

**Cons**

- May require multiple turns before reaching the root.
- A broken manager can block organizational flow until repaired.

**Decision:** accepted.

## Decision

Escalation moves **one direct-manager edge at a time**.

For an Agent membership `A` with manager membership `M`:

```text
A --escalate--> M
```

The server validates that both memberships are active and belong to the same Project before recording/routing the escalation.

A manager who receives an escalation may:

- resolve it directly;
- request more information;
- delegate related work downward;
- escalate one additional level to their own manager.

The server does not automatically cascade an escalation through multiple ancestors in one operation.

### Root boundary

The hierarchy root has no Agent manager. Root escalation therefore exits the Agent hierarchy.

Root escalation must create a clear Project-level human/intervention condition rather than:

- wrapping back to a subordinate;
- assigning the root to itself;
- silently discarding the escalation;
- automatically treating the Project lead as another hierarchy edge.

The existing Project `lead_type` / `lead_id` may be used as part of the human-facing notification/ownership surface where appropriate, but it remains a separate Project relationship. Exact notification UI/inbox mechanics are an implementation detail and must follow current upstream capabilities at implementation time.

If `lead_type = agent`, implementation must avoid creating a loop where root escalation simply re-enters the same Agent hierarchy. A human intervention surface remains the terminal fallback.

### Manager unavailable

Do not silently skip an unavailable manager.

If the direct manager membership exists but its Agent is archived, invalid, or otherwise unusable for the requested escalation, the hierarchy is considered structurally/operationally blocked for that path.

The operation must fail closed and surface the need for hierarchy repair/intervention.

This is distinct from transient provider/runtime execution failures; escalation topology must not change based on temporary execution availability.

### Work continuity

Escalation does not automatically replay a partially executed task.

It transfers a decision/request for intervention upward. Existing work artifacts, Issue state and comments remain the source of context. Any subsequent manager execution is a new governed action using current state.

## Consequences

### Positive

- Predictable inverse of one-hop delegation.
- Intermediate managers remain accountable.
- No hidden topology changes based on runtime health.
- Root escalation has an explicit human boundary.
- Easier to audit and reason about escalation paths.

### Negative / Trade-offs

- Escalation can take multiple turns through deep hierarchies.
- An unavailable manager blocks the path until repaired/intervened.
- Root escalation needs a user-facing intervention surface.

## Data Impact

This ADR does not require a separate escalation queue by itself.

Escalation may require additive durable metadata/event records so that:

- source membership;
- target manager/root boundary;
- reason;
- related Issue;
- current state

are auditable and idempotent.

Exact storage must be selected after inspecting current Issue comment/event/wakeup facilities. Prefer existing durable Multica primitives when they meet the requirement.

Do not store escalation state on global Agent rows.

## Backend / Runtime Impact

Hierarchy service should provide a validated escalation operation conceptually equivalent to:

```text
Escalate(issue, sourceMembership, reason, idempotencyIdentity)
```

For non-root membership, target is the direct manager membership.

For root membership, target is a Project-level intervention boundary.

The runtime/daemon should not need a new recursive orchestration protocol. Escalation should route through ordinary server task/Issue/wakeup mechanisms once authority is validated.

## API / CLI / UI Impact

Expose escalation as an explicit hierarchy action.

UI should show:

- who escalated;
- to whom / whether it reached the root boundary;
- reason/state;
- whether hierarchy repair is required.

Do not overload generic assignment controls to imply hierarchy escalation silently.

## Failure and Edge Cases

- Source has no active Project membership: escalation unavailable.
- Source is root: create Project-level intervention condition.
- Direct manager membership missing: hierarchy invalid; fail closed.
- Manager Agent archived: fail closed; do not skip to grand-manager.
- Temporary provider/runtime outage on manager: topology remains unchanged; normal execution failure handling applies separately.
- Project lead is the same Agent as root: root escalation must not self-loop.
- Project lead is another Agent: do not implicitly add a reporting edge; human/project intervention semantics still terminate hierarchy recursion.
- Repeated identical escalation request: must be idempotent.
- Escalation arrives while manager already has an active run: rely on existing queue/coalescing/execution-fence behavior rather than spawning uncontrolled parallel runs.

## Testing Requirements

- worker escalates to direct manager only;
- manager can escalate one additional edge;
- direct jump to grand-manager is rejected as hierarchy escalation;
- unavailable manager is not skipped;
- root escalation reaches intervention boundary;
- root/project-lead self-loop is prevented;
- repeated request is idempotent;
- Issue without Project cannot use Project hierarchy escalation;
- ordinary assignment/mention behavior remains unchanged.

## Upstream Compatibility

The decision keeps escalation in an additive hierarchy layer and avoids changing the global Agent model or ordinary Issue assignment semantics.

Implementation should reuse existing Issue/task/inbox/wakeup mechanisms where they fit rather than create a second messaging system.

If upstream later adds native Project escalation semantics, the fork should converge where their authority and no-skip invariants match this ADR.

## Open Questions

- Exact human intervention surface for root escalation (Project lead notification, inbox item, dedicated Project state, or combination) must be chosen from current upstream primitives immediately before implementation.
- Exact idempotency persistence should reuse an existing upstream facility if available.
