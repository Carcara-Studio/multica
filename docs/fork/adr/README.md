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
```

### Governance

```text
ADR-014 — Upstream compatibility strategy
```

The numbering above is reserved for the initial architecture set. If investigation materially changes grouping before acceptance, document the change rather than silently reusing an accepted ADR number.
