# Multica Fork Documentation

This directory contains documentation specific to the `Carcara-Studio/multica` fork.

The official upstream repository is `multica-ai/multica`. Upstream-owned root documentation remains authoritative for standard Multica behavior unless an accepted fork ADR explicitly defines an additive deviation.

## Documents

- [`PROJECT_BLUEPRINT.md`](./PROJECT_BLUEPRINT.md) — project mission, confirmed upstream architecture, fork architecture, roadmap, risks, and current decisions.
- [`GOVERNANCE.md`](./GOVERNANCE.md) — repository governance, protected-branch workflow, and implementation-agent rules.
- [`UPSTREAM_SYNC.md`](./UPSTREAM_SYNC.md) — policy for continuously incorporating upstream changes while preserving Git ancestry.
- [`REGRESSION_BASELINE.md`](./REGRESSION_BASELINE.md) — upstream CI/test baseline, protected behavior map, confirmed coverage gaps, and pre-implementation gates.
- [`FAIL-001_INVESTIGATION.md`](./FAIL-001_INVESTIGATION.md) — current failover call-site inventory, evidence corrections and unclosed integration gates.
- [`CODEX_CREDENTIAL_BOUNDARY.md`](./CODEX_CREDENTIAL_BOUNDARY.md) — pinned Codex source audit, credential-observation dependency and dispatch-bound acceptance requirements.
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

Foundation architecture and the upstream regression baseline are established. Project Hierarchy persistence and the authoritative transactional mutation domain are now implemented.

Current sequence:

```text
MFK-003 — architecture documentation                 DONE
MFK-004 — hierarchy ADRs                             DONE
MFK-005 — failover ADRs                              DONE
MFK-006 — upstream regression baseline               DONE
MFK-007 — activate Project Hierarchy implementation  DONE
HIER-001 — Project hierarchy schema                  DONE
HIER-002 — ProjectHierarchyService                   DONE
HIER-003 — cycle-safe concurrent mutations           DONE / SUBSUMED BY HIER-002
MFK-008 — reconcile roadmap/governance                IN PROGRESS
HIER-004 — Project hierarchy API                     NEXT PRODUCT WORK
```

HIER-003 is not a separate implementation branch. HIER-002 already delivered Project-row serialization, complete final-tree validation, cycle prevention, concurrent cycle/root mutation coverage, and rollback/lock-release validation. Reimplementing that scope would duplicate fork logic without adding capability.

`REGRESSION_BASELINE.md` remains the standing compatibility gate for implementation PRs and must be revalidated when upstream changes relevant behavior.

Implementation-time revalidation found upstream database rules that prohibit new foreign keys/cascades and require every new index to use its own concurrent-index migration. [`ADR-014`](./adr/ADR-014-upstream-compatibility-strategy.md) remains authoritative for fork implementation while preserving the semantic invariants of earlier hierarchy ADRs.

The next product implementation milestone is HIER-004. Before adding a public hierarchy API, inspect the current upstream handler/router/types/error conventions and design the adapter around `server/internal/projecthierarchy` rather than duplicating mutation logic in handlers.

## Repository Operating Model

Repository-capable work proceeds autonomously through the governed GitHub workflow when it remains within accepted architecture and the defined Issue scope. The human maintainer is not a per-PR execution dependency.

Human/local handoff is reserved for tests or executions that cannot be performed through available repository tooling, and for external product input that cannot be inferred from accepted requirements or repository evidence.

See [`GOVERNANCE.md`](./GOVERNANCE.md) for the full merge and implementation-agent policy.

## Upstream Compatibility Principle

Prefer additive, isolated, optional, and testable changes. Minimize edits to upstream hotspots and continuously converge toward native upstream mechanisms whenever they satisfy fork requirements.
