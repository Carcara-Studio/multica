# ADR-008 — Canonical quota-triggered failover

- Status: Accepted
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #7
- Supersedes: None
- Superseded by: None

## Context

Multica already owns a canonical task-failure taxonomy in:

```text
server/pkg/taskfailure/failure.go
server/pkg/taskfailure/classify.go
```

The taxonomy persists refined reasons into task history and distinguishes provider quota exhaustion from several superficially similar failures.

Confirmed current meanings include:

```text
agent_error.provider_quota_limit
  -> 402 / insufficient balance / monthly usage limit / credits exhausted
  -> not retryable until quota/balance is restored

agent_error.provider_capacity_or_rate_limit
  -> 429 / 529 / overloaded / no capacity
  -> transient class

agent_error.provider_auth_or_access
  -> authentication/access problem

agent_error.provider_server_error
  -> provider 5xx

agent_error.provider_network
  -> transport/network failure
```

The classifier also deliberately orders more specific rules before quota matching, for example context-window overflow before generic limit/quota wording.

Current TaskService retry behavior is a separate policy. At the investigated baseline, comments in the classifier explicitly note that `provider_network` is the only `agent_error.*` reason on the current retry allowlist. Failover must not implicitly widen that retry policy.

## Decision Drivers

- Failover must activate only on high-confidence evidence that waiting for the same primary account/model will not immediately solve the problem.
- Existing canonical classification should remain the source of truth.
- Rate limiting, temporary capacity, network and 5xx errors must not cause a long-lived switch to fallback.
- Auth/config/model errors require intervention, not quota failover.
- A failure from the fallback itself must not cause ping-pong back to primary.
- The task that exposed the quota problem must remain governed by the no-blind-replay policy in ADR-012.

## Considered Alternatives

### Alternative A — Reclassify raw error text in a new failover module

**Pros**

- Failover rules could evolve independently.

**Cons**

- Duplicates canonical Multica classification.
- Can disagree with persisted failure history and UI/metrics.
- Requires provider-specific maintenance in two places.

**Decision:** rejected.

### Alternative B — Trigger on all provider failures

**Pros**

- Maximizes apparent availability.

**Cons**

- Converts transient outages/rate limits into unnecessary target switches.
- Can hide authentication/configuration problems.
- Creates instability and provider ping-pong.

**Decision:** rejected.

### Alternative C — Trigger from a broad dashboard/UI failure class such as `rate_limit`

**Pros**

- Reuses an existing presentation grouping.

**Cons**

- Current presentation code may group quota and transient capacity together for operator display.
- UI grouping is not a safe execution-policy boundary.

**Decision:** rejected.

### Alternative D — Trigger only from the final canonical primary `provider_quota_limit` reason

**Pros**

- Reuses Multica's canonical source of truth.
- Precisely matches the user requirement.
- Avoids transient and unrelated failure classes.
- Easy to test exhaustively against the known taxonomy.

**Cons**

- A provider whose quota error is not recognized by upstream classification will not fail over until the canonical classifier is improved.

**Decision:** accepted.

## Decision

V1 automatic failover activates only when all of the following are true:

```text
execution route == PRIMARY
AND
final canonical failure_reason == agent_error.provider_quota_limit
AND
an enabled, structurally valid fallback configuration exists
```

The decision consumes the **final normalized canonical reason** used by task settlement, not raw provider text and not a presentation-layer grouping.

### Non-triggering failures

The following do not activate automatic fallback in v1:

```text
provider auth/access
provider capacity/rate limit
provider 5xx/server error
provider network error
context overflow
agent/process timeout
missing configuration
model unavailable/not found
runtime executable/version error
platform/runtime failures
unknown/internal failures
```

They continue through ordinary upstream failure/retry/recovery semantics.

### Relationship with existing retry policy

This ADR does not add `provider_quota_limit` to Multica's retry allowlist.

The quota-triggering task settles as failed according to ordinary task semantics. The failover transition affects subsequent routing only, subject to ADR-012.

Likewise, existing network retry behavior remains unchanged.

### Fallback-routed quota failure

A fallback task that itself fails with `agent_error.provider_quota_limit`:

- does not switch back to primary;
- does not reset the primary recovery schedule;
- does not create a target ping-pong loop;
- fails according to ordinary task semantics.

The Agent remains in fallback operational state while scheduled primary probes continue.

### Repeated primary quota signals

Concurrent or late primary quota failures after the first logical transition are idempotent.

They must not repeatedly postpone recovery by moving `next_probe_at` later on every duplicate signal.

The first successful transition out of primary establishes the recovery schedule. Later evidence may update observability fields such as a last-seen failure timestamp only if doing so cannot alter the recovery deadline unexpectedly.

### Missing/invalid fallback

If no enabled/valid fallback exists when the primary quota failure settles:

- the triggering task fails normally;
- the Agent does not enter a state that claims normal work can execute on fallback;
- configuration/intervention may be surfaced to the user.

Automatic failover requires a usable fallback target; it is not an excuse to manufacture one.

## Consequences

### Positive

- Failover policy is deterministic and auditable.
- No fork-specific provider string classifier is required.
- Temporary rate limits/capacity issues remain temporary.
- Current upstream retry policy is preserved.
- Fallback quota cannot cause oscillation.

### Negative / Trade-offs

- Classification misses become failover misses until upstream/fork classifier evidence is improved.
- Some providers may surface quota problems through ambiguous errors that intentionally remain outside automatic failover.
- Availability is sacrificed in ambiguous cases in favor of correctness.

## Data Impact

No new failure taxonomy table or raw-error matching store is introduced.

Failover state may persist:

- the canonical triggering reason;
- primary failure timestamp;
- triggering primary task ID;

as defined by ADR-009.

The canonical reason remains owned by Multica's existing task-failure package.

## Backend / Runtime Impact

The failover transition hook must run only after failure classification/normalization has produced the durable canonical reason.

Do not hook before classification in a provider adapter.

The hook should be narrow and idempotent, conceptually:

```text
OnSettledTaskFailure(task, canonicalReason)
```

It may request a failover transition only when the task is known to have used the primary route.

No daemon-side independent failover state machine is introduced.

## API / CLI / UI Impact

UI may display that automatic fallback was activated because primary quota was exhausted.

Do not show capacity/rate-limit failures as automatic quota failover simply because a dashboard presentation groups them together.

Operators should be able to inspect:

- current route/state;
- triggering canonical reason;
- triggering task;
- primary-failed timestamp;
- next probe time.

Exact API shape is deferred.

## Failure and Edge Cases

- Raw error says `quota` but canonical reason is another type: no failover; canonical result wins.
- Canonical primary quota with fallback disabled: task fails; no automatic fallback route.
- Two primary tasks fail with quota concurrently: one logical state transition.
- A primary task queued before failover fails with quota after state is already fallback: no schedule reset.
- A fallback task fails with quota: no primary switch.
- A transient rate limit immediately after a primary quota failure but on fallback: ordinary fallback task failure semantics; primary probing remains scheduled.
- Canonical reason is unknown: fail closed; do not infer quota from UI copy.

## Testing Requirements

Create an exhaustive transition matrix covering every current canonical failure reason.

At minimum:

- primary quota + valid fallback -> one failover transition;
- primary quota + no fallback -> no fallback-active state;
- primary rate-limit/capacity -> no failover;
- primary 5xx/network/auth/context/config/model errors -> no failover;
- fallback quota -> no ping-pong;
- repeated/concurrent primary quota failures -> one logical transition and stable first probe deadline;
- raw error/canonical reason disagreement -> canonical reason governs;
- current retryable reason behavior remains unchanged.

## Upstream Compatibility

This decision explicitly reuses upstream's canonical `taskfailure` package rather than adding a competing classifier.

The only required integration is a small post-classification failover hook. If upstream adds more precise quota reasons later, this ADR should be reviewed before broadening the trigger set.

## Open Questions

- The exact task-settlement hook must be re-inspected at implementation time because `TaskService` is an upstream hotspot.
- If upstream later introduces structured provider error metadata stronger than the canonical reason, any trigger expansion requires a new accepted ADR rather than an implicit heuristic.
