# Architecture Decision Records

This directory stores Architecture Decision Records (ADRs) for structural decisions specific to the Multica fork.

## Purpose

ADRs capture decisions whose rationale must survive individual chats, implementation agents, and future upstream changes.

An ADR is required when a decision materially affects one or more of:

- domain model;
- database invariants;
- API contracts;
- runtime/daemon behavior;
- concurrency or locking;
- delegation/escalation semantics;
- failover/recovery semantics;
- compatibility with upstream Multica;
- safety around side effects.

## Naming

Use sequential three-digit numbers:

```text
ADR-001-project-scoped-agent-hierarchy.md
ADR-002-squad-remains-workspace-scoped.md
ADR-003-hierarchy-integrity-and-cycle-prevention.md
```

Do not renumber accepted ADRs.

## Statuses

Each ADR must use one of:

- `Proposed` — under review; not yet implementation authority.
- `Accepted` — approved and authoritative for implementation.
- `Superseded` — replaced by a newer ADR; retain for history.
- `Deprecated` — no longer recommended but not necessarily replaced one-to-one.
- `Rejected` — considered and intentionally not adopted.

When superseding an ADR, the new ADR must link to the old one and the old ADR must identify its replacement.

## Current ADRs

| ADR | Decision | Status |
| --- | --- | --- |
| [ADR-001](ADR-001-project-scoped-agent-hierarchy.md) | Project-scoped Agent Hierarchy | Accepted |
| [ADR-002](ADR-002-squad-remains-workspace-scoped.md) | Squad remains workspace-scoped and separate from Project Hierarchy | Accepted |
| [ADR-003](ADR-003-hierarchy-integrity-and-cycle-prevention.md) | Hierarchy integrity and cycle prevention | Accepted |
| [ADR-004](ADR-004-hierarchical-delegation-semantics.md) | Hierarchical delegation semantics | Accepted |
| [ADR-005](ADR-005-escalation-semantics.md) | Escalation semantics | Accepted |
| [ADR-006](ADR-006-agent-archive-and-hierarchy-consistency.md) | Agent archive and hierarchy consistency | Accepted |
| [ADR-007](ADR-007-execution-target-abstraction.md) | Execution Target abstraction | Accepted |
| [ADR-008](ADR-008-canonical-quota-triggered-failover.md) | Canonical quota-triggered failover | Accepted |
| [ADR-009](ADR-009-persisted-failover-state-machine.md) | Persisted failover state machine | Accepted |
| [ADR-010](ADR-010-primary-recovery-probe.md) | 30-minute primary recovery probe | Accepted |
| [ADR-011](ADR-011-failover-concurrency-and-probe-leases.md) | Failover concurrency and probe leases | Accepted |
| [ADR-012](ADR-012-no-blind-retry-after-ambiguous-partial-execution.md) | No blind retry after ambiguous partial execution | Accepted |
| [ADR-013](ADR-013-cross-provider-compatibility-policy.md) | Cross-provider compatibility policy | Accepted |
| [ADR-014](ADR-014-upstream-compatibility-strategy.md) | Upstream compatibility strategy | Accepted |
| [ADR-015](ADR-015-fork-migration-namespace.md) | Reserved migration namespace for fork-owned schema changes | Accepted |

ADR-001 through ADR-006 were accepted by the human maintainer on 2026-10-14 and are implementation authority for Project Hierarchy work unless superseded or narrowed by a later accepted ADR.

ADR-007 through ADR-013 were accepted by the human maintainer on 2026-10-14 and are implementation authority for primary/fallback failover work unless superseded or narrowed by a later accepted ADR.

ADR-014 was accepted by the human maintainer on 2026-10-14 and is implementation authority for adapting fork mechanisms to current upstream repository rules. It takes precedence over FK/cascade-specific implementation proposals in earlier ADRs while preserving their accepted domain invariants.

ADR-015 was accepted on 2026-10-16 during MFK-010 and is implementation authority for allocating and renumbering fork-owned database migrations. Fork migrations use the reserved `900000+` band unless this ADR is explicitly amended.

## Required Structure

Use this template:

```markdown
# ADR-NNN — Title

- Status: Proposed
- Date: YYYY-MM-DD
- Decision owners: Human maintainer
- Related issues: #...
- Supersedes: None
- Superseded by: None

## Context

What confirmed upstream behavior and project requirement create the decision?
Separate confirmed facts from inference.

## Decision Drivers

- ...

## Considered Alternatives

### Alternative A
Pros, cons, upstream impact.

### Alternative B
Pros, cons, upstream impact.

## Decision

The chosen architecture and its invariants.

## Consequences

### Positive
- ...

### Negative / Trade-offs
- ...

## Data Impact

Tables, constraints, indexes, locking, migration, and rollback implications.

## Backend / Runtime Impact

Services, handlers, daemon/runtime hooks, and concurrency behavior.

## API / CLI / UI Impact

Backward-compatible contract implications.

## Failure and Edge Cases

Important invalid states, concurrency races, and recovery behavior.

## Testing Requirements

Unit, integration, database, runtime, concurrency, migration, and regression coverage.

## Upstream Compatibility

Hotspots touched, conflict risk, additive/isolation strategy, and future convergence path.

## Open Questions

Questions that must be resolved before implementation.
```

## Evidence Rules

Before accepting an ADR:

1. inspect current upstream code;
2. cite real upstream paths/types/contracts in the ADR when relevant;
3. distinguish confirmed facts from inference;
4. mark proposed paths/tables/APIs explicitly as proposals;
5. check whether upstream already provides a reusable mechanism;
6. assess future merge-conflict risk.

Do not invent upstream paths, types, tables, handlers, or behavior.

## Reserved Initial ADR Set

### Project hierarchy

```text
ADR-001 — Project-scoped Agent Hierarchy
ADR-002 — Squad remains workspace-scoped
ADR-003 — Hierarchy integrity and cycle prevention
ADR-004 — Hierarchical delegation semantics
ADR-005 — Escalation semantics
ADR-006 — Agent archive and hierarchy consistency
```

### Failover

```text
ADR-007 — Execution Target abstraction
ADR-008 — Canonical quota-triggered failover
ADR-009 — Persisted failover state machine
ADR-010 — 30-minute primary recovery probe
ADR-011 — Failover concurrency and probe leases
ADR-012 — No blind retry after ambiguous partial execution
ADR-013 — Cross-provider compatibility policy
ADR-016 — Require affirmative billing evidence for automatic failover
ADR-017 — Durable failover state and lease persistence boundary
ADR-018 — Opt-in task route proof and target persistence
ADR-019 — Target metadata assessment and atomic config invalidation
```

### Governance

```text
ADR-014 — Upstream compatibility strategy
ADR-015 — Reserved migration namespace for fork-owned schema changes
```

The numbering above records the accepted architecture set. Future ADR numbers must remain append-only; do not renumber accepted decisions.
