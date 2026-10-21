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
- [`HIERARCHY_ESCALATION.md`](./HIERARCHY_ESCALATION.md) — one-hop/root-boundary validation and remaining escalation delivery work (#94).
- [`HIERARCHY_AGENT_LIFECYCLE.md`](./HIERARCHY_AGENT_LIFECYCLE.md) — archive-writer/lock audit and atomic lifecycle enforcement (#90/#92).
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

Reconciled on 2026-10-21. The API, management clients, direct-Issue hierarchy
briefing and task-scoped delegation API/CLI are implemented. Server integration
coverage connects delegation to native child completion and manager resumption
(#88). Escalation has an internal one-hop/root-boundary relation gate (#94);
durable routing/API/CLI/UI delivery and end-to-end automatic failover remain
incomplete; no real installed-agent/account/inference smoke is claimed.

| Area | Implemented | Remaining boundary |
| --- | --- | --- |
| Hierarchy persistence/domain | Schema, Project/Workspace cleanup, serialized full-tree mutations and atomic Agent archive/leaf deactivation (#16/#18/#92) | Restore requires explicit hierarchy re-entry; no automatic reparenting |
| Hierarchy API | Five Project-scoped routes around the authoritative service (#22) | No duplicate API implementation needed |
| Web/desktop management | Shared Project hierarchy UI (#32) | Runtime/briefing/delegation semantics are separate |
| Native mobile management | Mobile data layer and Project hierarchy screens (#33, Issue #28) | Independent mobile checks remain required for mobile changes |
| Direct-Issue delegation | Bounded Project briefing, one-hop native child creation, durable replay, task-token API/CLI and server completion/resumption regression (#73/#75/#77/#80/#82/#88) | Real CLI/daemon/provider smoke and escalation; native post-commit dispatch gap remains |
| Upward escalation | Internal direct-manager/root-boundary relation validation (#94) | Durable idempotent operation, current-task/runtime authorization, manager routing, human intervention surface and API/CLI/UI |
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
tasks. Direct-report delegation and native completion/resumption are described
below; escalation remains pending. Before each
next slice, create a focused Issue, reinspect
current hooks and preserve no-Project/direct-Agent/Squad regressions. CLI
management remains a separate pending adapter. This roadmap does not authorize a
different delegation architecture.

The delegation core (#75/#77) provides transactional one-hop validation and
durable native child creation. The task-token-only API (#80) composes current
MAT/task bindings, originator-based invocation and native runtime readiness.
The task-scoped CLI and bounded direct-Issue briefing (#82) expose
`multica issue delegate` with an explicit persisted request UUID, no automatic
retry and no generic-create fallback. Hierarchy management CLI remains separate.
See [HIERARCHY_DELEGATION.md](./HIERARCHY_DELEGATION.md). The relation gate freezes
the relevant structural rows; the authenticated adapter supplies the independent
invocation/runtime gate. Native child creation and replay reuse existing
Issue/task/wakeup semantics, without a separate executor or recursive fan-out.

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
