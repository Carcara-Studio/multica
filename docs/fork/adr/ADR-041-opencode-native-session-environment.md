# ADR-041 — Opt-in native OpenCode shell environment

- Status: Accepted within the existing R5 compatibility scope
- Date: 2026-10-30
- Governing decisions: ADR-012/014/021/036; task authority and completion rules remain unchanged

## Evidence

On Windows, the installed built-in OpenCode 2.2.3 completed requests through
its managed service while private servers returned `provider.free-limit`.
Local comparisons found equal authentication, organization, client metadata
and request bodies; session headers differed. The gateway's internal reason
was not established. This is not proof of account-wide quota exhaustion.

The installed API exposes `PUT /api/session/{sessionID}/environment`. The
native CLI calls it on the managed connection before prompting. Source uses
an in-memory map keyed by session ID, and local tests verified separate A/B
values, no value in a third session, and removal on replacement. A real
backend smoke observed each sentinel through a shell tool.

Sources: OpenCode `packages/cli/src/run/run.ts`, `session-target.ts`, `env.ts`,
and `packages/core/src/session/environment.ts`; the installed 2.2.3 contract
was tested rather than inferred solely from upstream development sources.

## Decision

Task-scoped built-in OpenCode 2.x clients verified from 2.2.3 may use the
managed connection only when the owner explicitly sets
`MULTICA_OPENCODE_NATIVE_SESSION_ENV=1` in the agent environment. The default,
older/unknown versions and unknown major contracts retain the private server.
Custom runtime profiles are not newly mapped to this capability.

This opt-in uses the managed service's provider authentication/configuration.
Only local shell environment variables are replaced per session. It must not
be presented as support for per-task provider credentials or config isolation.
Owners requiring those overrides must retain the default private mode.

Task `custom_args` cannot override `--server` or `--standalone` in either mode:
an explicit server connection bypasses the CLI's native environment forwarding.
No token is put in Git, instructions or a new credential store. Managed MCP
remains refused by the existing guard; shell environment support does not
establish per-task MCP credential delivery.

ADR-036 completion evidence remains unchanged: a missing terminal event is not
accepted merely because the CLI exited zero, and proof must not start the
default service. Cancellation still interrupts the exact session before
terminating the client, preserving other managed-service sessions.

## Validation and scope

Version/connection/identity and cancellation regressions passed, as did the
agent package suite in Linux/WSL (without race detection). A gated Windows
smoke verified two native shell sentinels. Native dispatcher runs completed
with DeepSeek Free and GLM Free; build and four existing Unreal regressions
passed. A controlled cancellation stopped its shell process and preserved the
resident service. These are scoped observations, not model-quality, quota or
memory-economy benchmarks. Default tests use fake executables; real account
access remains behind `agentintegration` plus the explicit smoke opt-in.

Fork base reviewed: `0ff09a12b71ea3b83959431f7a0cedbfc114c8b7`.
Upstream reviewed: `e8ae72c9dfc1b4474b61a9087f51b33cbee6a048`, including ACP
transport `7d02596fc`. ACP is a broader transport change and is not imported
here. A later sync must re-evaluate environment delivery, cancellation and the
fork's stronger ADR-036 completion predicate; redundant run-transport code can
then be removed when native behavior meets those invariants.

No schema migration, new executor, account rotation, quota retry, R4 activation,
or change to the installed global Multica is part of this compatibility fix.
