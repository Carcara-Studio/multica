# Multica Fork Documentation

This directory contains documentation specific to the `Carcara-Studio/multica` fork.

The official upstream repository is `multica-ai/multica`. Upstream-owned root documentation remains authoritative for standard Multica behavior unless an accepted fork ADR explicitly defines an additive deviation.

## Documents

- [`PROJECT_BLUEPRINT.md`](./PROJECT_BLUEPRINT.md) — project mission, confirmed upstream architecture, fork architecture, roadmap, risks, and current decisions.
- [`GOVERNANCE.md`](./GOVERNANCE.md) — repository governance, protected-branch workflow, and implementation-agent rules.
- [`UPSTREAM_SYNC.md`](./UPSTREAM_SYNC.md) — policy for continuously incorporating upstream changes while preserving Git ancestry.
- [`REGRESSION_BASELINE.md`](./REGRESSION_BASELINE.md) — upstream CI/test baseline, protected behavior map, confirmed coverage gaps, and pre-implementation gates.
- [`adr/`](./adr/) — Architecture Decision Records for structural fork decisions.

## Authority

For fork-specific structural decisions, use the following order:

1. current code and tests;
2. accepted ADRs;
3. approved GitHub Issues;
4. `PROJECT_BLUEPRINT.md`;
5. implementation notes.

If current upstream behavior contradicts an assumption in the blueprint or an ADR, investigate the upstream change before implementation. Accepted ADRs may need to be superseded or narrowed when new evidence invalidates an implementation mechanism while leaving the underlying domain decision intact.

## Current Phase

Foundation architecture and the upstream regression baseline are established. Project Hierarchy implementation activation is in final review, but no fork-specific product behavior has been implemented yet.

Current sequence:

```text
MFK-003 — architecture documentation                 DONE
MFK-004 — hierarchy ADRs                             DONE
MFK-005 — failover ADRs                              DONE
MFK-006 — upstream regression baseline               DONE
MFK-007 — activate Project Hierarchy implementation  READY FOR MERGE
HIER-001 — Project hierarchy schema                  BLOCKED UNTIL MFK-007 MERGES
```

`REGRESSION_BASELINE.md` remains the standing compatibility gate for implementation PRs and must be revalidated when upstream changes relevant behavior.

Implementation-time revalidation found current upstream database rules that prohibit new foreign keys/cascades and require every new index to use its own concurrent-index migration. [`ADR-014`](./adr/ADR-014-upstream-compatibility-strategy.md) is now accepted and makes those current upstream engineering contracts authoritative for fork implementation while preserving the semantic invariants of earlier hierarchy ADRs.

HIER-001 may begin only after MFK-007 is merged. Its implementation must recheck upstream HEAD and migration head before writing or integrating product schema.

## Upstream Compatibility Principle

Prefer additive, isolated, optional, and testable changes. Minimize edits to upstream hotspots and continuously converge toward native upstream mechanisms whenever they satisfy fork requirements.
