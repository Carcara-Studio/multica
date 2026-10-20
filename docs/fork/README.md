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

Reconciled on 2026-10-20 against merged code and closed Issues #19, #21, #27,
#28 and #69. The API and management clients are implemented; hierarchy-aware
execution and end-to-end automatic failover are not complete.

| Area | Implemented | Remaining boundary |
| --- | --- | --- |
| Hierarchy persistence/domain | Schema, Project/Workspace cleanup, serialized full-tree validation and structural mutations (#16/#18) | Agent lifecycle rules under ADR-006 |
| Hierarchy API | Five Project-scoped routes around the authoritative service (#22) | No duplicate API implementation needed |
| Web/desktop management | Shared Project hierarchy UI (#32) | Runtime/briefing/delegation semantics are separate |
| Native mobile management | Mobile data layer and Project hierarchy screens (#33, Issue #28) | Independent mobile checks remain required for mobile changes |
| Failover foundations | Durable state/config, lifecycle invalidation, scoped evidence/connection/challenge/exchange boundaries, internal Codex config/thread observations and cancellable owned stdin (ADR-016–030) | Production evidence approvals, all writer/retry/claim integration, quota activation and isolated recovery inference |
| Effective Codex credentials | Source-pinned investigation (#70) | Producer does not expose the required request-bound credential receipt through the inspected account interfaces |

HIER-003 was satisfied by HIER-002; it is not another mutation implementation.
The original candidate labels HIER-005 (CLI) and HIER-006 (briefing) were
subsequently reused in delivered UI work. Actual Issue/PR scope is authoritative:
HIER-005 is shared web/desktop management; Issue #28 calls mobile parity HIER-005M
and its merged PR #33 uses HIER-006. Do not reuse these labels for unimplemented
CLI or briefing work.

The first runtime-context slice (#73) adds an informational one-hop Project
hierarchy snapshot to directly assigned Issue claims. See
[HIERARCHY_BRIEFING.md](./HIERARCHY_BRIEFING.md) for exclusions and bounds. It grants
no delegation authority and does not cover chat/autopilot/quick-create or Squad
tasks. The accepted direct-report delegation/wakeup/escalation and Agent lifecycle
rules remain pending. Before each next slice, create a focused Issue, reinspect
current hooks and preserve no-Project/direct-Agent/Squad regressions. CLI
management remains a separate pending adapter. This roadmap does not authorize a
different delegation architecture.

The next delegation prerequisite (#75) is a transaction-owned direct-report
relation gate. See [HIERARCHY_DELEGATION.md](./HIERARCHY_DELEGATION.md). It freezes
the relevant structural rows but does not authenticate a caller, approve target
invocation/runtime eligibility or create a child Issue. Native Issue-create
transaction integration and durable operation idempotency remain open; no
public delegation API/CLI is enabled.

Automatic fallback and 30-minute primary recovery probes remain disabled.
The [credential boundary audit](./CODEX_CREDENTIAL_BOUNDARY.md) records an external
producer-observability dependency, not permission to replace Codex, inspect user
secrets or require paid API keys. Continue independent work while that dependency
is unresolved; do not claim a complete fallback feature from control-plane tests.

`REGRESSION_BASELINE.md` remains the standing compatibility gate. Revalidate
current upstream behavior before each affected product change. ADR-014/015 keep
new relationships application-owned, forbid new FKs/cascades and reserve fork
migration numbering with separate concurrent-index migrations.

## Repository Operating Model

Repository-capable work proceeds autonomously through the governed GitHub workflow when it remains within accepted architecture and the defined Issue scope. The human maintainer is not a per-PR execution dependency.

Human/local handoff is reserved for tests or executions that cannot be performed through available repository tooling, and for external product input that cannot be inferred from accepted requirements or repository evidence.

See [`GOVERNANCE.md`](./GOVERNANCE.md) for the full merge and implementation-agent policy.

## Upstream Compatibility Principle

Prefer additive, isolated, optional, and testable changes. Minimize edits to upstream hotspots and continuously converge toward native upstream mechanisms whenever they satisfy fork requirements.
