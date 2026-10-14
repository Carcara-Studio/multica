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

If implementation evidence contradicts an accepted ADR, the structural change must stop and the decision must be reviewed before proceeding.

## Protected Branch

`main` is the protected integration branch.

The active repository ruleset requires:

- changes through pull requests;
- conversation resolution before merge;
- protection against branch deletion;
- protection against non-fast-forward / force-push updates.

The repository currently requires zero approving reviews because there is one primary human maintainer. This may be increased when additional maintainers or a governed review workflow are established.

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

Structural changes should not be merged only because tests pass. They must also remain consistent with accepted architecture.

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

- inspect the repository;
- edit files on a feature branch;
- run local commands;
- run tests;
- inspect diffs;
- create commits;
- open pull requests.

Implementation agents must not:

- redefine accepted architecture silently;
- merge their own structural changes without human governance;
- bypass the protected-branch workflow;
- perform broad unrelated refactors;
- create parallel mechanisms when Multica already has an appropriate one;
- retry potentially side-effecting partial executions without an approved safety policy.

When implementation evidence invalidates an assumption, the agent should document the evidence and stop the affected structural change.

## Compatibility Review

Before merging a structural change, review:

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
