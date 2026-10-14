# Upstream Synchronization Policy

> Fork: `Carcara-Studio/multica`
>
> Upstream: `multica-ai/multica`

## Purpose

This document defines how the fork incorporates changes from the official Multica repository while preserving fork-specific work and a traceable Git history.

## Remote Convention

Local clones must use:

```text
origin   → https://github.com/Carcara-Studio/multica.git
upstream → https://github.com/multica-ai/multica.git
```

Verify with:

```bash
git remote -v
```

## Main Branch

`main` is the protected integration branch for the fork.

Once fork-specific development begins, `main` is expected to contain both:

- upstream Multica ancestry;
- accepted fork-specific commits.

Therefore `main` will no longer always have the same HEAD SHA as `upstream/main`.

Compatibility is maintained through regular synchronization, not by force-resetting fork `main` to upstream.

## Synchronization Strategy

Use a dedicated synchronization branch and a pull request.

Recommended flow:

```bash
git switch main
git pull --ff-only origin main
git fetch upstream

git switch -c chore/upstream-sync-YYYYMMDD
git merge --no-ff upstream/main
```

If there are no conflicts, continue with validation.

If there are conflicts, resolve them on the synchronization branch.

Then:

```bash
git status
git add <resolved-files>
git commit
git push -u origin chore/upstream-sync-YYYYMMDD
```

Open a pull request into fork `main`.

## Why Merge Instead of Rebase

Do not rebase published fork `main` onto upstream.

Do not force-push rewritten history.

A merge-based upstream synchronization policy:

- preserves original upstream commit ancestry;
- keeps merge bases meaningful;
- makes repeated synchronization easier;
- avoids rewriting published fork history;
- makes upstream-vs-fork provenance easier to audit.

## Why Upstream Sync Must Not Be Squashed

Do not squash an upstream synchronization merge.

Squashing would collapse upstream ancestry into a fork-specific commit and make future merge-base and synchronization analysis harder.

Upstream sync PRs should preserve the merge commit created from `upstream/main`.

## Conflict Resolution Principle

When a conflict occurs, do not automatically prefer the fork version.

Use this priority:

1. understand the upstream change;
2. preserve upstream behavior where possible;
3. reapply the smallest fork-specific adaptation;
4. keep fork-specific logic isolated;
5. add or update regression tests.

The objective is not to make the conflict disappear. The objective is to preserve both the new upstream behavior and the minimum required fork behavior.

## High-risk Conflict Areas

Pay special attention to:

```text
server/internal/service/task.go
server/internal/handler/agent.go
server/internal/handler/daemon.go
server/pkg/protocol/messages.go
server/internal/daemon/
server/internal/handler/squad_briefing.go
```

These are current known or expected hotspots and must be revalidated over time.

## Before Resolving a Structural Conflict

For architecture-sensitive conflicts:

1. inspect the upstream commit/PR that changed the area;
2. determine whether upstream semantics changed;
3. compare the new semantics with the relevant fork ADR;
4. update the fork approach if upstream now provides the needed mechanism;
5. avoid blindly restoring the previous fork implementation.

If the upstream change invalidates an ADR assumption, pause the sync decision and update the ADR before completing the structural resolution.

## Validation After Sync

At minimum:

```bash
git status
git log --oneline --decorate -n 20
git merge-base main upstream/main
```

Run the relevant upstream and fork regression suites.

As the project matures, the required validation should include:

- build;
- unit tests;
- database tests;
- API tests;
- daemon/runtime tests;
- fork regression tests;
- migration tests;
- concurrency tests for affected subsystems.

## Sync Pull Request

Every upstream-sync PR should record:

```text
Upstream repository:
multica-ai/multica

Upstream target:
<upstream/main SHA>

Fork base before sync:
<origin/main SHA>

Conflicts:
<none or list>

Fork-specific files re-applied:
<list>

Tests:
<commands/results>

Architecture impact:
<none or explanation>
```

## Frequency

Do not wait for a large divergence.

Prefer small, regular upstream synchronizations.

A sync should be considered whenever:

- upstream releases a new version;
- upstream changes a known fork hotspot;
- a fork feature is about to modify an area that upstream recently changed;
- meaningful time has passed since the previous sync.

Exact cadence may be automated later.

## Emergency Exceptions

Avoid cherry-picking upstream commits as the normal sync mechanism.

A surgical cherry-pick may be used for a documented urgent fix only when taking all of current `upstream/main` is not viable.

Such an exception must be recorded because later full synchronization may encounter equivalent ancestry or semantic conflicts.

## Never Do

Do not:

```text
force-reset fork main to upstream/main
force-push fork main
rebase published fork main
squash full upstream synchronization
copy upstream files manually instead of merging Git history
resolve conflicts without understanding the upstream semantic change
```

## Long-term Goal

The fork should minimize permanent divergence.

Every synchronization is also an opportunity to ask:

> Can any fork-specific behavior now be replaced by upstream functionality?

If yes, prefer convergence and delete redundant fork code.
