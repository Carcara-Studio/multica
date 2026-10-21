# Multica Fork Governance

> Scope: Carcara-Studio/multica
>
> Upstream: https://github.com/multica-ai/multica

## Purpose

This repository is an upstream-compatible fork of Multica.

The fork adds capabilities that are intentionally absent or incomplete upstream while preserving the ability to continuously incorporate upstream changes.

The primary engineering constraint is:

> Prefer the smallest additive change that satisfies the requirement while preserving upstream compatibility.

## Sources of Truth

The repository uses the following hierarchy of authority:

1. current repository code and tests;
2. accepted Architecture Decision Records (ADRs);
3. approved GitHub Issues;
4. fork architecture documentation;
5. implementation-agent output.

Agent-generated implementation notes are not architectural authority by themselves.

If implementation evidence contradicts an accepted ADR, the affected structural implementation must stop until the contradiction is resolved through repository evidence and, when the structural decision changes, an ADR update or superseding ADR.

## Remaining Delivery Scope

`docs/fork/DELIVERY_CHECKLIST.md` is the single current execution-scope checklist
for the five agreed remaining deliverables (R1-R5). Every new Issue/PR must map
to an existing item and its acceptance criterion. Necessary implementation,
verification and compatibility substeps remain autonomously authorized; no new
per-PR approval gate is introduced. New top-level deliverables or functional
expansion require explicit maintainer agreement and a recorded scope change
before implementation. Historical blueprint candidate lists do not independently
authorize more work. Code/tests and accepted ADRs retain architectural authority.

## Protected Branch

`main` is the protected integration branch.

The active repository ruleset requires:

- changes through pull requests;
- conversation resolution before merge;
- protection against branch deletion;
- protection against non-fast-forward / force-push updates.

The repository currently requires zero approving reviews because there is one primary human maintainer and the repository workflow has standing authorization for governed autonomous execution.

No normal development work is performed directly on `main`.

## Branching

Work must use focused branches.

Recommended naming:

```text
chore/MFK-<id>-<description>
docs/MFK-<id>-<description>
feat/HIER-<id>-<description>
feat/FAIL-<id>-<description>
fix/<id>-<description>
```

A branch should address one cohesive concern.

Avoid mixing:

- unrelated refactors;
- formatting-only changes;
- upstream synchronization;
- multiple architectural features.

## Pull Requests

Every fork-specific change must be submitted through a pull request.

A pull request should identify:

- the governing Issue;
- relevant ADRs;
- the upstream baseline used during implementation;
- affected upstream hotspots;
- tests executed;
- backward-compatibility impact;
- migration impact, when applicable;
- known upstream-sync risk.

Structural changes should not be merged only because tests pass. They must also remain consistent with accepted architecture and complete the compatibility review described below.

## Merge Authority and Human Handoff

The maintainer has granted standing authority for repository-capable work to proceed autonomously through the normal governed workflow.

When work remains inside accepted architecture and an approved Issue scope, the repository agent may independently:

```text
investigate
  ↓
create/refine Issue
  ↓
create branch
  ↓
implement
  ↓
run available validation
  ↓
open/update PR
  ↓
inspect CI and review findings
  ↓
perform final upstream/compatibility review
  ↓
merge
  ↓
verify post-merge state
```

A separate per-PR human approval is not required solely because the change is structural. Passing CI alone is still insufficient: accepted ADRs, Issue scope, regression gates, upstream revalidation and compatibility review remain mandatory.

Human/local handoff is required only when the next necessary action cannot be completed through available repository tooling or when the requirement itself needs external product input that is not established by accepted project decisions. Typical examples include:

- local Docker/PostgreSQL validation unavailable in CI;
- host-specific runtime or daemon behavior that cannot be reproduced remotely;
- manual application/game/UI behavior that requires human observation;
- credentials, hardware or external environments not available to the repository agent;
- a genuinely new product choice for which repository evidence and accepted requirements do not establish the intended behavior.

When a local handoff is necessary, request only the minimum command/test and evidence needed to unblock the governed workflow.

## Merge Policy

### Normal fork changes

Prefer **squash merge** for feature, fix, documentation, and chore pull requests unless preserving individual commits has a concrete engineering benefit.

The goal is to keep fork-specific history compact and easy to distinguish from upstream history.

### Upstream synchronization

Upstream synchronization must use a **merge commit**, not squash or rebase.

This preserves the ancestry of `multica-ai/multica` inside the fork and makes future synchronization and merge-base analysis easier.

See `docs/fork/UPSTREAM_SYNC.md`.

## Upstream Compatibility Rules

Prefer:

1. additive modules;
2. additive tables and migrations;
3. optional fields;
4. existing Multica contracts;
5. existing Multica failure classifications;
6. existing Multica runtime and daemon patterns;
7. small hooks in upstream hotspots;
8. fork-specific logic outside upstream hotspots.

Avoid:

- rewriting central services;
- duplicating upstream subsystems;
- replacing existing contracts unnecessarily;
- large cross-cutting refactors;
- renaming or moving upstream files for fork convenience;
- modifying upstream documentation when a fork-specific document is sufficient.

## Fork-specific Documentation

Fork governance, architecture, ADRs, and compatibility notes should live under:

```text
docs/fork/
```

This location is intentionally isolated from upstream-owned root documentation.

Do not modify upstream `AGENTS.md`, `CONTRIBUTING.md`, issue templates, or pull-request templates solely to describe fork governance unless a future requirement makes that necessary.

## Architecture Governance

Structural decisions must follow:

```text
evidence
  ↓
architecture analysis
  ↓
ADR
  ↓
GitHub Issue
  ↓
branch
  ↓
implementation
  ↓
tests
  ↓
pull request
  ↓
review
  ↓
merge
```

Important architecture decisions require ADRs.

At minimum, ADRs are expected for:

- project-scoped hierarchy;
- relationship between Project Hierarchy and Squad;
- hierarchy invariants and cycle prevention;
- delegation;
- escalation;
- Agent archive behavior;
- execution-target abstraction;
- quota-triggered failover;
- failover state machine;
- 30-minute primary recovery probe;
- failover concurrency;
- ambiguous partial-execution retry policy;
- cross-provider fallback;
- upstream compatibility strategy.

## Implementation Agents

Implementation agents may:

- inspect the repository and upstream;
- create/refine governed Issues;
- create focused branches;
- edit files on a feature branch;
- run available local/remote commands and tests;
- inspect diffs and CI logs;
- create commits;
- open and maintain pull requests;
- merge their governed work after all required gates pass;
- perform post-merge verification and documentation/state reconciliation.

Implementation agents must not:

- redefine accepted architecture silently;
- bypass the Issue/ADR/protected-branch workflow;
- merge while required CI, regression or compatibility gates are unresolved;
- perform broad unrelated refactors;
- create parallel mechanisms when Multica already has an appropriate one;
- retry potentially side-effecting partial executions without an approved safety policy;
- conceal implementation evidence that invalidates an accepted assumption.

When implementation evidence invalidates an assumption, document the evidence before continuing. If the existing ADR no longer supports the required structural mechanism, update or supersede the ADR before implementing the changed architecture.

## Compatibility Review

Before merging a structural change, review:

- current upstream HEAD and relevant upstream behavior;
- number of existing upstream files modified;
- whether a new isolated module could reduce those modifications;
- database backward compatibility;
- API backward compatibility;
- daemon compatibility;
- migration rollback characteristics;
- test coverage;
- known conflict likelihood with upstream.

High-conflict files should receive especially small, explicit changes.

## Current High-risk Upstream Hotspots

Current architecture investigation identified these areas as likely merge-conflict hotspots:

```text
server/internal/service/task.go
server/internal/handler/agent.go
server/internal/handler/daemon.go
server/pkg/protocol/messages.go
server/internal/daemon/
server/internal/handler/squad_briefing.go
```

This list is informational and must be revalidated as upstream evolves.

## Change Principle

If upstream later implements functionality that overlaps with this fork:

1. inspect the upstream implementation;
2. compare its invariants with fork requirements;
3. prefer upstream behavior when it satisfies the requirement;
4. migrate fork behavior toward upstream;
5. remove redundant fork code;
6. document meaningful architectural changes through an ADR.

The fork should continuously converge toward upstream wherever possible.
