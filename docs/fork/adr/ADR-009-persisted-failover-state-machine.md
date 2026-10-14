# ADR-009 — Persisted failover state machine

- Status: Proposed
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #7
- Supersedes: None
- Superseded by: None

## Context

ADR-007 defines primary and fallback execution targets. ADR-008 limits automatic activation to a canonical primary quota failure.

A route switch cannot safely live only in process memory:

- Multica may run multiple server replicas;
- servers and daemons restart;
- work may remain queued across restarts;
- a 30-minute recovery probe must survive process failure;
- concurrent quota failures may arrive from multiple tasks;
- operators need to observe why and when fallback is active.

Configuration and operational state have different lifecycles. Fallback configuration changes rarely, while failover state changes as quota fails, probes run and recovery succeeds.

## Decision Drivers

- State must survive server/daemon restarts.
- Multiple server replicas must observe one shared truth.
- The state machine must distinguish normal fallback operation from an in-flight primary probe.
- Normal tasks must keep using fallback while a primary probe is running.
- Configuration must not be mutated to represent temporary availability.
- Agents without fallback must preserve upstream behavior without mandatory backfill.
- Transitions must be idempotent and auditable.

## Considered Alternatives

### Alternative A — Keep failover state only in server memory

**Pros**

- No database state.

**Cons**

- Lost on restart.
- Inconsistent across replicas.
- Cannot reliably schedule 30-minute recovery.

**Decision:** rejected.

### Alternative B — Encode state by rewriting Agent primary configuration

**Pros**

- Existing rows already durable.

**Cons**

- Configuration no longer describes configuration.
- Recovery must reconstruct primary settings.
- Creates races with user edits and queued tasks.
- Violates ADR-007.

**Decision:** rejected.

### Alternative C — Store a single boolean `use_fallback`

**Pros**

- Minimal schema.

**Cons**

- Cannot represent an in-flight recovery probe, lease, next schedule or reason.
- Hard to recover safely after crashes.
- Insufficient observability.

**Decision:** rejected.

### Alternative D — Separate fallback configuration from a durable state-machine row

**Pros**

- Clear semantics.
- Supports distributed coordination and recovery.
- Additive and optional.
- State can evolve without changing primary Agent fields.

**Cons**

- Adds a state table and transition service.

**Decision:** accepted design, pending ADR approval.

## Decision

Model automatic failover as a durable per-Agent state machine.

Conceptual states:

```text
PRIMARY_ACTIVE
FALLBACK_ACTIVE
PRIMARY_PROBING
```

Names are part of this fork's architecture but final persisted wire strings may be adapted to existing database naming conventions during implementation.

### State meaning

#### PRIMARY_ACTIVE

Future ordinary tasks resolve to the primary target.

No automatic recovery probe is due because primary is considered available.

#### FALLBACK_ACTIVE

Future ordinary tasks resolve to the fallback target.

Primary is considered unavailable due to the accepted quota transition or a failed/inconclusive recovery probe.

`next_probe_at` determines when primary recovery may next be attempted.

#### PRIMARY_PROBING

A recovery probe claim for the primary target is currently outstanding.

**Ordinary user work still routes to fallback.**

This state does not mean primary is trusted again.

### Proposed durable state

A conceptual additive structure:

```text
agent_failover_state
--------------------
agent_id PK
status
failure_reason
primary_failed_at
last_primary_failure_task_id
next_probe_at
last_probe_at
recovered_at
probe_claim_token
probe_lease_until
probe_dispatched_at nullable
last_probe_reason nullable
intervention_reason nullable
version
updated_at
```

Exact columns may be reduced or renamed if implementation evidence shows equivalent existing primitives, but these concepts must remain representable.

### State transitions

```text
PRIMARY_ACTIVE
    |
    | primary canonical provider_quota_limit
    v
FALLBACK_ACTIVE
    |
    | next_probe_at <= now and probe successfully claimed
    v
PRIMARY_PROBING
    |
    +-- successful minimal primary probe ------------> PRIMARY_ACTIVE
    |
    +-- quota / transient / inconclusive probe ------> FALLBACK_ACTIVE
    |
    +-- auth/config/model intervention-class failure -> FALLBACK_ACTIVE
```

For every transition from `PRIMARY_PROBING` back to `FALLBACK_ACTIVE`, schedule the next primary probe for 30 minutes after the settled probe attempt, subject to ADR-010/011.

### Initial state and optionality

Agents without enabled fallback configuration do not require state rows and remain on ordinary upstream primary behavior.

When fallback configuration is enabled, the implementation may create a `PRIMARY_ACTIVE` row or lazily create state on first transition. Absence must have an unambiguous backward-compatible meaning equivalent to primary-active behavior.

No migration should backfill every existing Agent merely to create idle state.

### Routing by state

```text
state absent / PRIMARY_ACTIVE -> PRIMARY
FALLBACK_ACTIVE              -> FALLBACK
PRIMARY_PROBING              -> FALLBACK for normal work
```

Probe work itself explicitly targets PRIMARY and is not ordinary task routing.

### Explicit operator changes

Automatic state changes must be separated from intentional configuration changes.

An operator must not be able to accidentally remove/disable the only active fallback target while the state still claims that normal work is fallback-routable.

Implementation must provide a transactional policy such as:

- update compatible fallback configuration while preserving active state; or
- explicitly reset/disable automatic failover and return routing to primary.

The exact API verb is deferred, but silent configuration/state inconsistency is forbidden.

### Archival

An archived Agent does not execute normal work or probes.

Archival should disable/cancel future probe scheduling for that Agent without deleting historical state needed for audit. Restore does not imply that an expired probe claim is still valid; normal state validation resumes from durable current configuration.

## Consequences

### Positive

- Restart-safe and multi-server safe source of truth.
- Operational state is independent from configuration.
- Probe-in-flight state is explicit.
- Normal work remains deterministically on fallback until recovery is proven.
- Useful observability fields exist without overloading task history.

### Negative / Trade-offs

- Requires a transition service and database state.
- Configuration mutations must coordinate with state.
- Operational cleanup/archival needs explicit policy.

## Data Impact

Additive table keyed by `agent_id` with:

- FK to Agent;
- constrained status values;
- indexes supporting due-probe scans, likely on state + `next_probe_at`;
- timestamps/token/lease fields needed by ADR-011.

No legacy task/Agent data is rewritten.

State deletion should not be used as a normal transition because absence has primary-compatible semantics and could accidentally reactivate primary if performed while quota remains exhausted.

## Backend / Runtime Impact

Create one authoritative failover service owning transitions and target-state reads.

No daemon owns the state machine. Daemon is an executor of probe requests and ordinary tasks selected by server-side routing.

State transition methods should be narrow and idempotent, conceptually:

```text
ActivateFallbackFromQuota(...)
ClaimDuePrimaryProbe(...)
SettlePrimaryProbe(...)
ResetFailoverExplicitly(...)
```

## API / CLI / UI Impact

Expose operational status separately from fallback configuration.

Useful read-only information includes:

- state;
- primary failure reason/time;
- next probe time;
- whether a probe is in flight;
- last probe outcome;
- intervention warning;
- last recovery time.

Do not present `PRIMARY_PROBING` as primary-active.

## Failure and Edge Cases

- Server restarts in `FALLBACK_ACTIVE`: next due time remains durable.
- Server/daemon crashes in `PRIMARY_PROBING`: lease recovery defined by ADR-011.
- Two quota failures race: one logical transition.
- Probe succeeds while new normal task routes: concurrency rule in ADR-011 determines a serial outcome.
- Fallback config removed without explicit reset: reject configuration change or preserve a consistent state; never leave an unroutable fallback-active state silently.
- Agent archived while probe outstanding: probe result must not reactivate an archived Agent or resurrect an obsolete claim.
- Agent restored later: stale pre-archive claim token cannot settle current state.

## Testing Requirements

- state absent resolves primary;
- primary quota transitions to fallback once;
- fallback/probing normal work resolves fallback;
- successful probe returns primary-active;
- failed probe returns fallback-active with a future probe time;
- restart preserves state and due schedule;
- archive invalidates outstanding probe settlement;
- incompatible config deletion cannot strand fallback-active routing;
- transitions are idempotent under duplicate calls;
- legacy Agents without fallback remain unchanged.

## Upstream Compatibility

All state is additive and external to the central Agent table. Existing Agent execution fields remain untouched.

The design follows Multica's general preference for durable database coordination where process memory would be insufficient, while keeping normal primary execution on the upstream path.

If upstream later adds native provider availability/target-state infrastructure, this row should converge toward it rather than remain a parallel state machine.

## Open Questions

- Final state/status persistence type (`TEXT + CHECK`, enum-like constraint, etc.) should match migration conventions at implementation time.
- Exact archival cleanup behavior must be reconciled with the current Agent archive transactions when FAIL implementation begins.
