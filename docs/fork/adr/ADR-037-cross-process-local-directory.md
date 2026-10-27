# ADR-037 — Cross-process local-directory exclusion

- Status: Accepted
- Date: 2026-10-27
- Scope: R5 / #122; correct existing local-directory exclusion

## Evidence

Desktop can automatically run its own daemon alongside a manually started
development profile. LocalPathLocker keeps a mutex map per daemon, so two
instances do not exclude each other. The synthetic independent-instance
regression acquires the same path twice on the baseline. The real Windows
test also observed overlapping profiles and mixed task activity; that observation
does not independently prove the complete cause of the earlier identity issue.

## Decision

Retain the current local queue, holder hints and cancellation interface. Extend
its acquired section with an advisory OS file lock using existing execenv
LockFileEx/flock primitives. Key stable coordination files by the canonical
directory path in the user's shared cache, independent of CLI profile, backend,
task temp directories and Codex configuration. Fold Windows path case for the
case-insensitive supported environment. Do not delete coordination files as
stale markers: the OS releases ownership on handle close or process death.

Lock failures fail closed; cancellation closes the waiting handle and releases
the local queue. Announce waiting at most once per acquisition, including
contention with another process whose task ID is unknown. Keep distinct paths
independent. Existing call sites determine the protected lifetime; no new task
executor, dispatch fence, global single-daemon restriction or retry is added.
This is same-user local-filesystem coordination, not a security boundary against
processes that deliberately bypass it or differently configured cache roots.

## Compatibility and validation

Baseline 58770f863; upstream d7d1831fc inspected. Preserve API/schema/protocol,
worktree execution and existing native lifecycle. Reuse OS lock primitives;
isolate file-lock implementation in execenv with a small LocalPathLocker hook.
Independent-instance and separate-process tests must cover contention,
cancellation, release/crash recovery and different-path progress without real
agents or credentials. R4 remains disabled; this does not close R5.
