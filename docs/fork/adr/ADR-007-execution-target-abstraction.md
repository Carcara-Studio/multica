# ADR-007 — Execution Target abstraction

- Status: Proposed
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #7
- Supersedes: None
- Superseded by: None

## Context

### Confirmed upstream behavior

At the investigated Multica upstream baseline `52b26900c51f759b87707b4ea35fa9f141b83b52`, an Agent's execution configuration is stored directly on the Agent. Relevant fields include:

```text
runtime_id
runtime_config
custom_env
custom_args
mcp_config
model
thinking_level
service_tier
```

`server/pkg/db/queries/agent.sql` persists and updates these fields.

Tasks already persist their selected `runtime_id` in `agent_task_queue` at enqueue time. Multiple upstream enqueue paths explicitly stamp a runtime derived from the resolved Agent, including ordinary Agent work, Autopilot and wakeup paths.

The daemon claim payload then combines:

- task identity and persisted `task.runtime_id`;
- current Agent identity/instructions/configuration;
- current `model`, `thinking_level`, `service_tier`, runtime configuration and tool/MCP-related data.

A critical current invariant in `server/internal/handler/daemon.go` rejects delivery when the Agent has moved to another runtime after the task was queued:

```text
agent.runtime_id != task.runtime_id
    -> fail before launch
```

Current session continuation is also runtime-scoped. Claim-time session lookup only supplies `PriorSessionID` when the prior task/session runtime matches the claiming task's `runtime_id`.

### Fork requirement

An Agent must retain its existing upstream execution configuration as its **primary** configuration while optionally having a second **fallback** execution configuration.

When quota failover is active, future work must use the fallback without:

- overwriting the primary Agent fields;
- cloning the Agent into a second identity;
- losing the runtime selected for a task if recovery occurs before claim;
- bypassing current runtime/session safety checks.

## Decision Drivers

- Preserve existing Agent execution fields as the canonical primary configuration.
- Agents without fallback must remain byte-for-behavior equivalent to upstream wherever practical.
- A fallback may differ in runtime/provider, model, reasoning level, service tier and provider-specific configuration.
- Route choice must survive queue/claim races.
- Existing `agent_task_queue.runtime_id` should continue to be authoritative for the runtime selected for a task.
- Avoid adding broad fork-specific columns to the central task queue when an additive side structure is sufficient.
- Keep Agent identity, instructions, skills and organizational identity independent from execution-provider choice.

## Considered Alternatives

### Alternative A — Mutate the Agent's existing runtime/model fields to the fallback

**Pros**

- Existing enqueue and claim code would naturally read the fallback values.

**Cons**

- Destroys the distinction between primary configuration and temporary failover state.
- Recovery requires reconstructing the prior Agent configuration.
- Races with queued/running work and ordinary Agent edits.
- Broadly changes upstream semantics for Agent configuration.
- A queued primary task can become inconsistent with the mutated Agent and fail the existing runtime-delivery gate.

**Decision:** rejected.

### Alternative B — Create a second fallback Agent

**Pros**

- Reuses all existing execution fields and task routing.

**Cons**

- Splits identity, permissions, Issue history, skills, hierarchy membership and observability.
- Requires hidden synchronization between two Agent records.
- Conceptually incorrect: fallback is an execution target, not another organizational Agent.

**Decision:** rejected.

### Alternative C — Move both primary and fallback into a new generic target table

**Pros**

- Symmetric model.
- Could support more than two targets later.

**Cons**

- Requires migrating existing Agent execution configuration.
- Changes a central upstream contract immediately.
- Makes every existing Agent dependent on fork-specific data.
- Larger long-term merge surface.

**Decision:** rejected for v1.

### Alternative D — Keep current Agent fields as primary and add an optional fallback target plus per-task route identity

**Pros**

- Fully additive for Agents that opt in.
- Preserves upstream primary representation.
- Allows deterministic queue-time route selection.
- Can reuse `task.runtime_id` as the durable selected runtime.
- Limits claim changes to fallback-routed tasks.

**Cons**

- Claim-time code must understand a fallback route.
- Multiple enqueue paths must use one target resolver rather than reading `agent.runtime_id` directly.

**Decision:** accepted design, pending ADR approval.

## Decision

Introduce an internal **Execution Target** abstraction with two v1 routes:

```text
PRIMARY
FALLBACK
```

This is an application/domain abstraction; these names do not imply an upstream API or database enum already exists.

### Primary target

The existing Agent execution fields remain the authoritative primary target.

No primary configuration migration or backfill is introduced.

### Fallback target

Add optional fork-specific fallback configuration associated one-to-one with a user Agent.

A conceptual structure is:

```text
agent_fallback_config
---------------------
agent_id PK
runtime_id
model
thinking_level
service_tier
runtime_config / provider-specific target overrides when required
custom_args / environment overrides only where justified by upstream mechanisms
created_at
updated_at
version
```

Exact columns and secret-bearing representation are deferred until implementation-time inspection. Any environment/credential handling must reuse the same authorization, redaction and audit principles as existing Multica Agent/runtime configuration rather than inventing a parallel secret subsystem.

Agent identity-level configuration remains shared unless a capability genuinely belongs to the target. In particular, Agent ID, instructions, skills and Project/Squad identity do not duplicate into fallback configuration.

### Durable per-task route

Queue-time target selection must be durable.

The preferred additive v1 shape is a side table conceptually equivalent to:

```text
agent_task_execution_route
--------------------------
task_id PK
route
created_at
```

Only fallback-routed tasks need a row; absence means the ordinary primary/upstream path.

The task's existing `runtime_id` stores the runtime selected by the resolved target.

Therefore a fallback-routed task is identified by both:

```text
agent_task_queue.runtime_id = selected fallback runtime
agent_task_execution_route.route = FALLBACK
```

The side row and task insert must commit atomically.

### Claim-time behavior

For a task with no fallback route marker:

- preserve current upstream claim behavior;
- validate `task.runtime_id` against `agent.runtime_id` as today;
- build execution data from the Agent's current primary configuration.

For a fallback-routed task:

- load the current fallback configuration;
- validate that the fallback still belongs to the same Agent and is structurally usable;
- require the current fallback `runtime_id` to equal the task's persisted `runtime_id`;
- do **not** compare that task runtime against primary `agent.runtime_id`;
- construct the effective execution configuration from the fallback target while retaining the Agent's identity/instructions/skills and existing task-scoped authorization.

If the fallback runtime changed or the fallback was removed/disabled after enqueue, fail before launch rather than silently switching the already-routed task to another target.

Model/thinking/service-tier changes on the same fallback runtime may follow the same live-configuration semantics upstream already uses for the primary Agent at claim time. The runtime route itself is the durable queue-time boundary.

### Session continuity

Do not weaken upstream runtime-scoped session safety.

A prior provider session is eligible only when the existing runtime-equality rules consider it resumable. Cross-runtime fallback therefore starts without a primary-runtime provider session automatically.

No new cross-provider session translation is introduced.

## Consequences

### Positive

- Primary configuration stays upstream-native.
- Agents without fallback take the existing path.
- Recovery cannot retroactively reroute already-queued fallback work.
- Cross-runtime session leakage remains blocked by existing runtime checks.
- Fallback remains an execution concern rather than a second Agent identity.

### Negative / Trade-offs

- Target resolution must be integrated into several current enqueue paths.
- Claim delivery gains a fork-specific fallback branch.
- A side table adds one lookup/relationship for fallback tasks.
- Runtime changes to an active fallback config can invalidate already-queued fallback tasks rather than silently adapting them.

## Data Impact

Expected additive persistence:

- optional `agent_fallback_config`;
- optional `agent_task_execution_route` rows only for routed fallback tasks.

No existing Agent or historical task requires a backfill.

Foreign keys and cleanup should follow existing task/Agent deletion lifecycle after implementation-time inspection.

Do not choose migration numbers until current upstream migration head is revalidated.

## Backend / Runtime Impact

Introduce one target resolver used wherever work is routed to an Agent.

Conceptually:

```text
ResolveExecutionTarget(agent, failoverState)
    -> primary or fallback target
```

The resolver should return the effective runtime and execution configuration metadata needed by enqueue/claim code, not mutate the Agent.

Known upstream enqueue call sites are not assumed to be exhaustive; implementation must audit all writers that create `agent_task_queue` work and currently stamp `agent.RuntimeID`.

Daemon process execution can continue receiving an Agent-shaped claim payload; the server is responsible for supplying the target-effective execution fields.

## API / CLI / UI Impact

Fallback configuration should be additive and optional.

Existing Agent create/update payloads need not gain required fields. Prefer a dedicated fallback subresource or optional nested surface if that minimizes conflict with current Agent contracts.

UI should present:

- primary target;
- optional fallback target;
- current operational route/state separately.

Configuration and failover state must not be conflated.

## Failure and Edge Cases

- No fallback configured: always use ordinary primary behavior.
- Fallback state active but config missing: fail closed and surface configuration inconsistency; do not silently use primary.
- Fallback runtime changed after task enqueue: queued task fails pre-launch rather than switching runtime.
- Primary recovers after fallback task enqueue: queued fallback task remains fallback.
- Fallback task is retried through a retry mechanism explicitly permitted by upstream: retry lineage must preserve fallback route unless a later ADR explicitly defines otherwise.
- Primary and fallback use the same runtime: route marker still distinguishes which execution configuration is authoritative.
- Fallback uses another runtime/provider: existing session runtime checks prevent primary session reuse.

## Testing Requirements

- Agent without fallback follows unchanged primary path;
- primary task preserves current runtime mismatch rejection;
- fallback route persists fallback runtime atomically;
- fallback task can claim while `agent.runtime_id` remains primary;
- fallback runtime/config removal after enqueue fails before launch;
- recovery after enqueue does not reroute fallback task;
- same-runtime primary/fallback route uses correct target configuration;
- cross-runtime fallback does not receive primary runtime session ID;
- all audited enqueue paths use the target resolver or prove fallback cannot reach them.

## Upstream Compatibility

This decision deliberately keeps existing Agent fields and ordinary task behavior intact.

Fork-specific state is additive, and primary tasks use the exact current claim invariant. The main upstream hotspots are task-enqueue call sites and `server/internal/handler/daemon.go`; modifications there should be narrow and delegated to isolated target-resolution helpers.

If upstream later introduces a first-class execution-target abstraction, this fork should map fallback configuration/state onto it and remove redundant routing structures.

## Open Questions

- Final fallback configuration columns depend on current runtime/provider configuration mechanisms at implementation time.
- Exact API shape is deferred.
- The complete current task-writer inventory must be captured in MFK-006/HIER/FAIL implementation planning before code changes.
