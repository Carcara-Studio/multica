# ADR-016 — Require affirmative billing evidence for automatic failover

- Status: Accepted
- Date: 2026-10-18
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37
- Refines: ADR-008
- Supersedes: None

## Evidence

At fork `3aec44a5` and upstream `f896cc483`,
`server/pkg/taskfailure/classify.go` checks the words `credits` and `quota`
before generic capacity/rate-limit matching. Both `quota exceeded per minute`
and `rate limit: quota exceeded` receive
`agent_error.provider_quota_limit`. This is a useful history taxonomy, but
the label alone does not prove balance exhaustion.

`TaskService.FailTaskWithTransition` preserves non-empty reasons except for
the specific compatibility normalizations in `NormalizeDaemonReason`.
Consequently an older daemon's label alone is also insufficient evidence.

## Decision

Keep the upstream taxonomy, settlement and retry allowlist unchanged. Add a
fork-local evidence gate under `server/internal/failover`. Activation requires:

1. the canonical quota reason;
2. the raw diagnostic also classifies as quota under the current classifier;
3. an affirmative billing witness from the explicitly tested vocabulary;
4. no explicit transient rate/capacity witness.

Initial affirmative witnesses are `insufficient_balance`, `insufficient_quota`,
insufficient balance, low balance, monthly usage limit, exhausted credits,
out of credits, depleted/exhausted credit balance, and payment required.
The canonical classifier must accept the diagnostic too: a phrase not recognized
by it remains rejected even if it resembles a billing witness.

Generic quota/credits words, a bare HTTP number, and unknown provider wording
remain inconclusive. Mixed rate/billing diagnostics are conservatively rejected.
This can miss genuine exhaustion; it cannot be used to claim universal provider
coverage. Expand the gate only with provider diagnostic fixtures and negative
rate-limit tests. A future structured adapter signal should replace text evidence
without changing canonical history values.

This predicate is a necessary condition, not the transition itself. The eventual
transaction must additionally prove primary route, current target/configuration
generation, active Agent and valid compatible fallback. Fallback failures and
stale primary runs cannot change current state. No task replay is introduced.

## Alternatives

- Trust the canonical label alone: rejected because the counterexamples are
  executable and would violate the requirement to distinguish rate limiting.
- Reorder/rewrite the upstream classifier: rejected for this increment because
  it changes history and retry consumers beyond fork failover.
- Treat unknown diagnostics as exhaustion: rejected; retain upstream failure
  behavior until there is affirmative evidence.

## Impact and validation

No database, API, CLI, web/mobile, daemon or ordinary task behavior changes.
The gate has no call sites until the transition service is implemented.
Table tests cover affirmative/ambiguous diagnostics, all non-quota canonical
reasons, classifier precedence and the concrete broad-label counterexamples.

This adds two isolated Go files and no upstream production hotspot. No migration
or rollback is needed; removing the unused gate restores the prior source tree.
ADR-008's necessary canonical trigger remains intact, with a stricter sufficiency
rule derived from current code rather than a new failure taxonomy.
