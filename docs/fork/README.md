# Multica Fork Documentation

This directory contains documentation specific to the `Carcara-Studio/multica` fork.

The official upstream repository is `multica-ai/multica`. Upstream-owned root documentation remains authoritative for standard Multica behavior unless an accepted fork ADR explicitly defines an additive deviation.

## Documents

- [`PROJECT_BLUEPRINT.md`](./PROJECT_BLUEPRINT.md) — project mission, confirmed upstream architecture, fork architecture, roadmap, risks, and current decisions.
- [`GOVERNANCE.md`](./GOVERNANCE.md) — repository governance, protected-branch workflow, and implementation-agent rules.
- [`UPSTREAM_SYNC.md`](./UPSTREAM_SYNC.md) — policy for continuously incorporating upstream changes while preserving Git ancestry.
- [`adr/`](./adr/) — Architecture Decision Records for structural fork decisions.

## Authority

For fork-specific structural decisions, use the following order:

1. current code and tests;
2. accepted ADRs;
3. approved GitHub Issues;
4. `PROJECT_BLUEPRINT.md`;
5. implementation notes.

If current upstream behavior contradicts an assumption in the blueprint or an ADR, investigate the upstream change before implementation. Accepted ADRs may need to be superseded when new evidence invalidates them.

## Current Phase

The project is still in foundation/architecture work. No fork-specific product behavior has been implemented yet.

Current sequence:

```text
MFK-003 — architecture documentation
MFK-004 — hierarchy ADRs
MFK-005 — failover ADRs
MFK-006 — upstream regression baseline
MFK-007 — activate Project Hierarchy implementation
```

## Upstream Compatibility Principle

Prefer additive, isolated, optional, and testable changes. Minimize edits to upstream hotspots and continuously converge toward native upstream mechanisms whenever they satisfy fork requirements.
