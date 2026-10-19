# ADR-019 — Target metadata assessment and atomic config invalidation

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #44
- Implements in part: ADR-013, ADR-017, ADR-018

## Scope and evidence

Base fork `f98c0f7eb`, upstream `e752b971b` unchanged. Current upstream provides
`SupportedTypes`, `BuiltinRuntimeByID`, `ModelSelectionSupported`,
`ModelKnownIncompatibleWithProvider`, `QualifyModelID`, literal thinking/tier
checks and pure `ValidateThinkingLevelWith`/`ValidateServiceTierWith` loaders.
Reuse these instead of maintaining another provider option vocabulary.

The catalog explicitly distinguishes static fallback lists from verified runtime
reports. A model absent from a catalog may be a valid manual alias/custom model;
absence alone does not prove invalidity. A catalog Default badge is a display
hint, not proof of which model a CLI's local config resolves. `agent.ExecOptions`
still has no universal safe tool-free probe contract. None is invented here.

## Metadata assessment

`AssessFallbackMetadata` is a pure necessary gate over stored Agent/config/runtime
and a caller-supplied catalog for that exact runtime. It checks nonzero identities,
active user Agent, workspace/binding, private-runtime Agent-owner equality,
recognized provider identity/family and explicit model/thinking/tier options.
Runtime offline status does not make primary healthy or select another target.

The result is one of `metadata_compatible`, `incompatible`, or `unknown`, with a
stable reason code and no raw provider diagnostics, environment or credentials.
It never logs input, launches a CLI, reads an account or changes saved options.

Known literal/ownership/provider-family/unsupported-control mismatches and
runtime-reported unavailable models are incompatible. Unknown providers, empty or
static model reports, ambiguous qualification and unlisted manual model selectors
remain unknown. Both incompatible and unknown block automatic readiness. Verified
qualification uses the upstream helper; it does not guess from slash placement.
Thinking and service-tier checks use one supplied snapshot, not discovery.
Codex/OMP effort requires a pinned model, as upstream execution already requires;
there is no legacy fallback configuration to grandfather. For other providers,
effort without a pinned model stays unknown: a catalog Default badge is not
proof of the effective runtime-local model. Known CLI effort vocabulary can still
reject an unsupported flag independently. Explicit standard Codex
routing reuses upstream's installed-binary capability exception even when the
model list is a static fallback; missing binary evidence stays unknown.

Empty options retain upstream runtime-default semantics, not a fabricated model
selection based on a catalog badge. `metadata_compatible` is **not** complete
execution compatibility or permission to enqueue, claim or probe. Required
MCP/tools/skills/apps, context limits, runtime/profile overrides, protected secrets,
session isolation and credential-faithful inference must still be established.
No static table claiming every installed adapter is automatically ready is added.
Catalog provenance/freshness must be authenticated and checked by later transport
and caller integration; a cached/list response is not durable lease authority.

## Configuration mutation

The existing optional config update/removal primitives now require expected
config generation, primary generation and state version, plus fresh policy epoch
(and fresh config epoch for update). Their single SQL statement locks **config
then state**, samples wall clock after locking, and changes both atomically.
A failed condition changes neither. Missing state is not silently initialized.

Editing invalidates all old probe claim fields, rotates both generations and
increments state version. Primary-active stays primary-active; fallback-active
or probing stays fallback-active and schedules 30 minutes after the edit. Prior
failure/recovery history is retained. An existing suspension is never resumed.
Editing the fallback does not prove recovery of an exhausted primary.

Removal rotates policy, increments version, suspends state and invalidates claims
while deleting config. Probing becomes fallback-active with a coherent schedule;
other availability states are retained. Duplicate/stale removal is a no-op.
Suspended state plus missing config is not authorization for a later resolver to
silently route a known-exhausted Agent to primary. Initialization/re-enablement
and explicit intervention policy still require later domain integration.

Queued route proofs are retained unchanged; execution-critical config epochs no
longer match them. They must fail before launch rather than silently receive new
settings. Merely recording metadata is not an executed task or partial replay.

These remain caller-transaction-bound sqlc primitives, not owner authorization or
complete lifecycle fencing. Existing owner/chat/task locks must precede entry.
They acquire no upstream owner locks and perform no network I/O while holding
state. Agent edit/archive, runtime/profile/default/credential changes and terminal
cleanup hooks, the complete integration lock graph, public API/CLI/UI, all task
writers and safe probe adapters remain unimplemented. No config endpoint or
production task flow calls them, and automatic failover remains disabled.

## Verification

Pure tests cover binding/ownership, offline/default behavior, provider identity,
model selection/discovery/qualification ambiguity, supported/unsupported/unknown
thinking/tier metadata and no option rewriting. Real DB tests cover edit/removal
of a dispatched probe, stale success rejection, preserved availability/history,
queued proof retention, transaction rollback, state/config CAS mismatches,
suspension and missing-state fail-closed behavior. Prior concurrent config CAS
and task route regressions still apply. sqlc is regenerated. DB skips locally are
not validation; exact-head PostgreSQL/race CI is required before merge.
