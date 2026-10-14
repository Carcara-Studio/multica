# ADR-010 — 30-minute primary recovery probe

- Status: Accepted
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #7
- Supersedes: None
- Superseded by: None

## Context

When primary quota/balance is exhausted, normal work moves to fallback. The user requirement is explicit: the system must periodically test the primary again, at a 30-minute cadence, and automatically return to it once a real request succeeds.

The recovery test is not user work. Creating a normal Issue/task for every probe would pollute history, metrics, billing attribution and Agent context.

Current upstream Multica already has a useful control-plane precedent for work that must run on a user's runtime behind NAT. Runtime model discovery uses a pending-request pattern:

```text
server creates pending control request
    -> daemon receives it on heartbeat
    -> daemon performs local/provider operation
    -> daemon reports result
```

The server cannot assume it can call the runtime/provider directly.

## Decision Drivers

- Probe every 30 minutes after failover until recovery is proven.
- Test actual primary provider/model availability rather than merely runtime liveness.
- Probe must have the lowest practical cost.
- Probe must not execute Agent tools, MCP, GitHub/file/database side effects or user task instructions.
- Probe must not resume a user/provider session.
- Probe must not appear as ordinary Issue/task/chat work.
- Old daemons must remain compatible and must never be mistaken for successful recovery.
- Result interpretation must reuse canonical provider failure semantics where possible.

## Considered Alternatives

### Alternative A — Retry the failed user task every 30 minutes

**Pros**

- Naturally tests the real provider path.

**Cons**

- Replays potentially side-effecting work.
- Pollutes user task history.
- Violates ADR-012.

**Decision:** rejected.

### Alternative B — Treat runtime heartbeat/model listing as proof of recovery

**Pros**

- Very cheap.
- Existing control-plane support.

**Cons**

- Proves daemon/runtime reachability, not that provider quota has recovered.
- A model can still list successfully while inference remains quota-blocked.

**Decision:** rejected as recovery proof.

### Alternative C — Query a provider billing/quota endpoint directly from the server

**Pros**

- Could be cheap and structured for some providers.

**Cons**

- Not uniformly available across providers/runtimes.
- Server may not hold provider credentials.
- Does not exercise the same runtime/provider path used by normal work.

**Decision:** rejected as the generic v1 contract.

### Alternative D — Heartbeat-carried minimal inference probe against the primary target

**Pros**

- Uses the real primary runtime/provider/model credentials.
- Proves the path normal work needs.
- Fits existing daemon control-plane topology.
- Can be kept isolated from task history and tools.

**Cons**

- Requires a new optional daemon/server protocol capability.
- Costs a small provider request every 30 minutes while fallback remains active.

**Decision:** accepted.

## Decision

A primary recovery probe is **control-plane work**, not an Agent task.

### Cadence

On initial quota failover:

```text
next_probe_at = primary_failed_at + 30 minutes
```

After every failed, quota-blocked, transient, permanent or otherwise inconclusive probe attempt that does not prove primary recovery:

```text
next_probe_at = probe_settled_at + 30 minutes
```

The 30-minute user requirement is not exponential backoff in v1.

If a probe was claimed but never produced a trustworthy result, lease recovery under ADR-011 settles it as inconclusive and establishes the next 30-minute attempt without creating a rapid retry storm.

### What the probe executes

The daemon performs the smallest provider/runtime-compatible request that can prove successful primary inference.

The probe uses the primary target's:

- runtime/provider;
- model selection or runtime default;
- credentials/configuration required to reach the provider;
- minimal provider-compatible request shape.

The probe does **not** include:

- Issue/project/chat context;
- Agent instructions beyond what the adapter strictly requires;
- prior provider session ID;
- task history;
- tools;
- MCP servers;
- connected apps/plugin tools;
- repository checkout/worktree operations;
- user files;
- ordinary task authorization token;
- external side-effect actions.

Generation/output budget should be the minimum practical value supported by the adapter/provider.

### Recovery proof

Primary becomes `PRIMARY_ACTIVE` only after a probe receives a valid successful provider/model response through the primary execution path.

Runtime heartbeat, model catalog success, network connectivity alone or an expired claim is not recovery proof.

### Probe failure interpretation

Probe result should be normalized into a small control-plane outcome while reusing canonical provider classification where meaningful.

Examples:

```text
success
provider_quota_limit
provider_capacity_or_rate_limit
provider_server_error
provider_network
provider_auth_or_access
missing_config / model unavailable
probe_timeout / inconclusive
```

Only success restores primary.

All other outcomes keep normal work on fallback and schedule another attempt 30 minutes later.

Auth/config/model failures additionally surface a persistent intervention warning because repeated probing may not repair them automatically. However, the user-requested periodic recovery check continues while automatic failover remains enabled; v1 does not silently stop probing merely because the current failure appears permanent.

### Protocol shape

The server advertises a pending probe to the **primary runtime** using the same general server→heartbeat→daemon→result topology already used by model discovery and other runtime-local control-plane work.

Exact protocol field names, endpoint names and capability tokens are implementation details. Names such as `primary-probe-v1` are proposals, not upstream facts.

### Old daemon compatibility

A daemon that does not support the probe capability:

- continues normal upstream task behavior;
- does not receive an unknown probe it could mis-handle;
- leaves the Agent on fallback;
- surfaces that automatic primary recovery is unavailable until a compatible daemon is running.

The server must never interpret capability absence as probe success.

### Observability separation

Probes should have dedicated control-plane metrics/logging, for example conceptually:

```text
primary_probe_total
primary_probe_duration
primary_probe_outcome
failover_recovery_seconds
```

Do not record probes as ordinary Agent task/activity history or user work completion.

Provider cost, where observable, may be attributed to failover-control overhead rather than to an Issue/task.

## Consequences

### Positive

- Recovery is automatic and based on the real execution path.
- No potentially side-effecting user work is replayed.
- Probe traffic stays out of task history and prompts.
- Existing daemon heartbeat topology can be extended instead of adding inbound server→daemon connectivity.

### Negative / Trade-offs

- Periodic probes have a small provider cost.
- Every supported runtime/provider family needs a safe minimal-probe implementation or capability declaration.
- Permanent auth/config problems continue generating low-frequency attempts until corrected or failover is explicitly disabled.

## Data Impact

Probe schedule/claim/result metadata belongs to `agent_failover_state` from ADR-009, including concepts such as:

- `next_probe_at`;
- `last_probe_at`;
- `last_probe_reason`;
- claim token/lease/dispatched metadata from ADR-011.

Do not create normal `agent_task_queue` rows solely for recovery probes.

Transient pending-request transport state may use an existing-style memory/Redis mechanism where appropriate, but the authoritative failover/probe schedule and lease must remain durable in the database.

## Backend / Runtime Impact

### Server

- scheduler scans due failover state;
- durable claim occurs before daemon delivery;
- heartbeat response may include an optional pending probe for the current runtime;
- result endpoint settles the claim by token/state validation.

### Daemon

- advertise/recognize probe capability;
- execute provider-aware minimal request without normal task preparation;
- enforce no-tool/no-MCP/no-session behavior;
- report structured result tied to claim token.

Probe code should reuse provider adapters where possible without entering the ordinary task lifecycle.

## API / CLI / UI Impact

UI/status surfaces should show:

- fallback active;
- next scheduled primary probe;
- probe currently in progress;
- last probe outcome;
- daemon compatibility/intervention warning.

Manual `probe now` functionality is optional future work and must still use the same safe control-plane path if added.

## Failure and Edge Cases

- Primary runtime offline: probe cannot prove recovery; remain fallback and reschedule.
- Primary provider quota still exhausted: remain fallback and reschedule.
- Primary responds 429/5xx/network error: inconclusive/transient; remain fallback and reschedule.
- Primary auth/model/config is broken: remain fallback, warn, continue 30-minute checks while enabled.
- Daemon lacks probe support: remain fallback and surface upgrade requirement.
- Probe succeeds after its claim expired: result is stale and cannot restore primary.
- Agent archived during probe: late result cannot reactivate routing.
- Fallback has also failed: primary probe schedule remains independent; ordinary fallback task failure does not transform the probe into user work.

## Testing Requirements

- first probe due exactly from configured 30-minute policy;
- probe has no task/Issue/chat history row;
- request excludes tools/MCP/session/user prompt context;
- successful primary inference restores primary;
- quota/transient/auth/config/timeout results do not restore primary;
- every non-success schedules next attempt 30 minutes later;
- unsupported old daemon never produces false success;
- stale result after lease expiry is ignored;
- archived Agent cannot be recovered by late probe;
- metrics/activity distinguish probes from user executions.

## Upstream Compatibility

The design extends the existing heartbeat-carried control-plane pattern instead of requiring inbound access to daemons or hijacking normal task semantics.

Protocol additions must be optional so old daemons and upstream servers remain understandable during rolling upgrades.

Provider probe implementations should be isolated from central task execution wherever possible.

## Open Questions

- Exact minimal request shape is provider/runtime-specific and must be defined with adapter-level tests during FAIL-009.
- Exact probe timeout and lease duration are implementation parameters governed by ADR-011's safety constraints.
- Cost observability depends on what current provider adapters expose for non-task control requests.
