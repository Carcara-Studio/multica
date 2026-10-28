# ADR-039 — Explicit native-login path configuration intent

- Status: Accepted
- Date: 2026-10-28
- Decision owners: Repository agent under standing R4 implementation authority
- Scope: R4 / #37 / #139
- Refines: ADR-018/024/038; does not approve an execution adapter

## Decision

Store `native_login_path BOOLEAN NOT NULL DEFAULT false` on the existing optional
`agent_fallback_config`. False retains strict credential policy; true records the
maintainer-selected ADR-038 native-login path policy. Do not infer the policy from
missing credential epochs, a daemon's quota report, catalog metadata or heartbeat.
There is no second configuration store or account-availability service.

Protected `ConfigManager.Save` explicitly replaces this intent with the rest of
the configuration. Inspect/redacted views include it; audit records the non-secret
choice. The existing membership/Agent ownership/runtime access fences still apply.
A worker daemon cannot impersonate a human owner to change it. Every edit, including
a mode switch or switch back, retains config/state CAS, rotates both generations
and invalidates old probe authority without declaring the primary healthy.

The strict `DescribeTargetBinding` boundary rejects native configurations, so
strict enqueue/queued resolution and evidence exchange cannot lend credential
evidence to this separate policy. Strict challenge issuance/consumption also
reject native rows at the persistence boundary. Legacy tasks without route proof keep their
primary behavior and cannot gain native routing authority retroactively. Shared
policy-snapshot/metadata and leaf SQL primitives remain consistency helpers;
they do not authorize either mode by themselves.

## Storage intent is not execution readiness

Saving true does not enable fallback, approve native model selection, supply an
observer or permit a probe. Unknown/default model metadata may still be saved as
pending configuration, as in the existing management contract. ADR-038 requires
explicit supported models/options, current owner/runtime/profile/configuration,
trusted run/turn evidence, all writers/claims and isolated real probes before
native execution can be authorized. The strict verifier and observer remain
closed, and credential epochs are never fabricated to bridge modes.

`DescribeNativePathBinding` reuses the existing target overlay and metadata
checks to describe one selected route's non-secret binding. Unlike strict
description it requires native intent and an explicit verified catalog selection;
an implicit/default or whitespace-padded model remains pending metadata. It does
not produce `ResolvedTarget`, route proof or execution approval. Its caller must
supply current server-owned rows/catalog and later enforce authenticated scope,
owner/configuration locks, native effective selection and required capabilities.
Catalog IDs and options alone do not prove actual CLI overrides, preserved tools/
MCP/hooks/context, safe session isolation or an isolated real recovery probe.

Migration900029 adds one column with strict defaults, without changing existing
generations, availability or routes. It adds no FK, index or backfill into native
mode. Applying it requires the existing base table; a missing table must fail
rather than record a skipped dependency as complete. Reapplication is idempotent.

Down migration refuses while native-mode rows exist, leaving the column and
migration ledger intact on failure. Before downgrade, use the current protected
management boundary and current CAS revision to explicitly revert/remove those
configurations, thereby invalidating their old generations. The migration must
not silently reinterpret native intent as strict or reset availability. Removing
the column is not certification of arbitrary older binaries/adapter policies.

## Alternatives and limits

Inferring mode from absent epochs would conflate unavailable strict evidence with
explicit user intent. A second config table/ledger is unnecessary. A boolean is
sufficient for the two accepted policies; additional policies need a separately
reviewed contract rather than a growing set of implicit combinations.

There is no public management endpoint/UI, production writer/claim resolution,
terminal availability hook or probe adapter in this increment. R4 stays disabled.
PostgreSQL/race tests must prove persistence/defaults, CAS/concurrent mode edits,
stale probe rejection, rollback and guarded down/up behavior. Pure tests prove
strict/native separation even with otherwise valid fixture credential evidence.
