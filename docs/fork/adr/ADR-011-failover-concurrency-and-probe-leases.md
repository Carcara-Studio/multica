# ADR-011 — Failover concurrency and probe leases

- Status: Proposed
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #7
- Supersedes: None
- Superseded by: None

## Context

Automatic failover is a distributed coordination problem, not only a routing boolean.

Relevant races include:

- several primary tasks fail with quota at the same time;
- multiple server replicas scan for the same due probe;
- multiple heartbeats from the same or replacement daemon race to receive control work;
- daemon crashes after receiving a probe but before reporting its result;
- a late probe result arrives after the claim was reaped;
- primary recovers while a normal task is being routed;
- fallback configuration changes while fallback work is queued.

Current Multica already uses PostgreSQL locks and leases for distributed work, including `FOR UPDATE SKIP LOCKED` patterns and runtime/task ownership fencing. Failover should use the database as the coordination authority rather than process-local mutexes.

## Decision Drivers

- At most one logical failover transition per Agent for a burst of primary quota failures.
- At most one valid in-flight primary probe claim per Agent.
- No probe storms across server replicas or daemon reconnects.
- Crash recovery must not depend on a specific process returning.
- Stale/duplicate probe results must be harmless no-ops.
- A normal task must get a deterministic route when primary recovery races with enqueue.
- Existing task/runtime ownership fences must not be bypassed.
- Lock ordering must be designed to avoid introducing deadlocks into upstream task settlement/enqueue paths.

## Considered Alternatives

### Alternative A — Process-local mutex per Agent

**Pros**

- Simple within one server.

**Cons**

- Does not coordinate multiple replicas.
- Lost on restart.
- Cannot fence late daemon results.

**Decision:** rejected.

### Alternative B — Distributed cache lock as sole authority

**Pros**

- Fast and familiar for ephemeral work.

**Cons**

- Failover state is durable database state.
- Cache failure/eviction can create duplicate claims.
- Harder to make state transition and lock ownership atomic.

**Decision:** rejected as the authority.

### Alternative C — Database state row + tokenized lease + compare-and-set settlement

**Pros**

- Durable and multi-replica safe.
- Claim and state transition can be atomic.
- Late results can be fenced by token/state/version.
- Existing PostgreSQL worker patterns provide precedent.

**Cons**

- Requires careful transaction/lock ordering.
- Lease timeout must exceed normal probe execution time.

**Decision:** accepted design, pending ADR approval.

## Decision

Use the durable `agent_failover_state` row as the coordination point for failover transitions and probe claims.

### Quota transition concurrency

`ActivateFallbackFromQuota` must use a transactional conditional transition.

Conceptually:

```text
lock/read failover state for Agent
if state is PRIMARY_ACTIVE (or absent primary-equivalent):
    set FALLBACK_ACTIVE
    primary_failed_at = triggering failure time
    next_probe_at = primary_failed_at + 30m
    record triggering primary task
else:
    no schedule-changing transition
commit
```

Concurrent quota failures after the first transition are idempotent and must not push `next_probe_at` later.

If state absence represents primary-active, lazy row creation must use uniqueness/CAS semantics so concurrent creators cannot produce duplicate rows or divergent schedules.

### Due-probe claim

Server schedulers scan only due `FALLBACK_ACTIVE` rows.

Preferred database pattern:

```text
SELECT due rows
FOR UPDATE SKIP LOCKED
```

For each claimed Agent, atomically set:

```text
status = PRIMARY_PROBING
probe_claim_token = random unguessable token
probe_lease_until = now + lease duration
probe_dispatched_at = NULL
last_probe_at = now (or claim timestamp)
```

Only one current claim token is valid for an Agent.

### Heartbeat delivery

A daemon heartbeat for the Agent's **primary runtime** may receive the pending probe only when:

- state is `PRIMARY_PROBING`;
- claim token is current;
- lease is not expired;
- probe has not already been durably marked as dispatched in a way that would create duplicate provider calls;
- daemon capability is compatible.

When practical, marking a claim as dispatched should be a compare-and-set operation tied to token + unexpired lease.

The exact transport may use an existing control-plane pending-request store as a delivery optimization, but that store does not own the claim. Database token/lease is authoritative.

### Daemon duplicate suppression

Server-side token fencing is primary. The daemon should additionally avoid concurrently executing the same probe token twice if it receives a duplicate heartbeat payload during reconnect/retry.

This is defense in depth, not the durable authority.

### Probe settlement

Probe result carries the claim token.

Settlement succeeds only if all relevant conditions still hold, conceptually:

```text
agent state == PRIMARY_PROBING
AND probe_claim_token == result.token
AND claim is still current
AND Agent/configuration is still eligible
```

A stale token, superseded claim or archived/disabled Agent produces a no-op rather than an error that could resurrect old state.

Success transitions to `PRIMARY_ACTIVE`.

Any valid non-success result transitions to `FALLBACK_ACTIVE` and schedules the next 30-minute probe.

### Lease expiry / crash recovery

If `PRIMARY_PROBING` exceeds its lease without a trustworthy current result:

- treat the attempt as inconclusive;
- clear/rotate claim metadata;
- return to `FALLBACK_ACTIVE`;
- set the next probe for 30 minutes after recovery settlement;
- never immediately issue repeated provider calls in a tight loop.

A late result for the expired token is ignored.

Lease duration must be longer than the probe's normal timeout and transport/report allowance, with bounded values chosen during implementation.

### Routing/recovery race

A normal task created while failover state is fallback/probing must remain fallback even if the primary probe succeeds milliseconds later.

Conversely, a task whose route decision occurs after successful recovery should use primary.

Therefore target selection and task creation/route-marker insertion must have a serializable ordering with failover state transition.

The preferred v1 rule is:

1. route transaction reads/locks the relevant failover state sufficiently to prevent it changing during target decision;
2. task row is inserted with selected `runtime_id`;
3. fallback route marker, when required, is inserted in the same transaction;
4. transaction commits;
5. recovery transition waiting on that state lock may then commit.

This produces one of two valid orders:

```text
fallback route commits -> recovery commits
```

or

```text
recovery commits -> primary route commits
```

There is no halfway route whose task runtime disagrees with its route identity.

### Lock-order discipline

Do not casually hold a failover-state lock while acquiring upstream task locks in an order that can deadlock with task settlement.

Implementation must map current transaction boundaries and establish one documented lock order before coding. In particular:

- quota failover transition should occur after canonical task settlement in a way that does not hold the task-row lock while waiting on a conflicting state→task lock chain;
- routing transaction coordination must compose with `lock_task_owner_rows` and existing task uniqueness/ownership fences;
- probe claim/settlement should avoid task locks entirely because probes are control-plane work.

If necessary, post-settlement failover activation may use a separate idempotent transaction after the task's durable failure commit.

## Consequences

### Positive

- Multi-server safe.
- Duplicate quota signals do not distort recovery timing.
- One valid probe claim per Agent.
- Daemon/server crashes recover without operator intervention.
- Late results cannot falsely restore primary.
- Task route/recovery race has deterministic outcomes.

### Negative / Trade-offs

- More state transitions and database coordination than a simple boolean.
- Requires explicit lease sweeper/recovery path.
- Enqueue paths need transaction-aware target resolution.
- Lock-order regression tests are mandatory.

## Data Impact

`agent_failover_state` needs claim/fencing concepts such as:

```text
probe_claim_token
probe_lease_until
probe_dispatched_at
version
```

A partial index for due fallback rows may be appropriate, conceptually:

```text
(status, next_probe_at)
WHERE status = FALLBACK_ACTIVE
```

Exact index/column names are deferred.

`agent_task_execution_route` and the task row must be created transactionally for fallback-routed work.

## Backend / Runtime Impact

### Server

- due-probe scheduler with bounded batches;
- row claims using durable locking;
- lease recovery sweep;
- CAS delivery/settlement;
- transaction-aware execution target resolver.

### Daemon

- execute only current token received through supported protocol;
- local duplicate suppression where useful;
- always echo token in result;
- no authority to mutate global failover state directly.

## API / CLI / UI Impact

Operational status may expose probe in-flight/lease timing at a high level, but raw claim tokens must not be exposed to untrusted clients.

UI should distinguish:

- waiting for next probe;
- probing now;
- recovery succeeded;
- probe timed out/inconclusive;
- daemon upgrade/intervention required.

## Failure and Edge Cases

- 10 primary quota failures concurrently: one failover schedule.
- Two server replicas scan same due row: one claims; the other skips/observes probing state.
- Two heartbeats race: at most one valid dispatched provider probe.
- Daemon reports twice: first current settlement wins; duplicate is no-op.
- Daemon crashes after dispatch: lease expires; fallback remains active; next probe waits 30 minutes.
- Result arrives after lease expiry: ignored.
- Agent archived during probe: late result cannot restore primary.
- Fallback task enqueue races with probe success: task is atomically either fallback-before-recovery or primary-after-recovery.
- Fallback runtime config changes while task queued: ADR-007 claim validation fails safely rather than rerouting.

## Testing Requirements

### Database/concurrency

- concurrent quota activation settles once;
- repeated quota does not move first `next_probe_at`;
- two scheduler workers cannot claim same Agent;
- one claim token at a time;
- duplicate delivery/result is idempotent;
- stale token result cannot transition state;
- lease expiry safely returns to fallback;
- archive/reset invalidates outstanding claim;
- recovery/enqueue race produces only valid primary or fallback task state;
- rollback of task+route insert leaves neither half persisted;
- lock-order stress test shows no deadlock across routing, settlement and probe transitions.

### Multi-process simulation

- two server workers + two daemon heartbeat attempts do not create duplicate provider probes;
- server restart during probing recovers from durable lease;
- daemon restart during probing cannot falsely succeed an old token.

## Upstream Compatibility

This design uses PostgreSQL coordination patterns already familiar in Multica and keeps probe coordination outside ordinary task rows.

The main upstream-sensitive area is route selection around existing enqueue paths. Integration should be centralized rather than copying failover locks into every handler.

If upstream adds a generic durable control-work lease mechanism, the probe scheduler should converge toward it.

## Open Questions

- Exact lock order with current task enqueue/settlement transactions must be documented from implementation-time code inspection before any FAIL code is merged.
- Exact probe timeout/lease values are implementation configuration, provided lease > normal probe timeout and lease recovery cannot form a tight retry loop.
