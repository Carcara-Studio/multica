# ADR-012 — No blind retry after ambiguous partial execution

- Status: Accepted
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #7
- Supersedes: None
- Superseded by: None

## Context

A provider quota failure does not prove that the failed Agent run performed no useful or externally visible work before the quota error occurred.

An Agent execution may perform multiple reasoning/provider turns and may invoke tools between them. Before a later provider request hits quota, the run may already have:

- edited files;
- created commits/branches;
- called GitHub or another API;
- written database state;
- posted comments/messages;
- invoked MCP tools;
- created or modified Issues;
- performed other non-idempotent external actions.

Automatically creating a replacement run on fallback would replay the user's instruction from an ambiguous point and can duplicate those side effects.

Current Multica already treats retryability and session continuity as explicit safety concerns. At the investigated baseline, quota is not on the normal retry allowlist.

## Decision Drivers

- Side effects must not be duplicated merely to improve availability.
- A canonical quota reason indicates provider exhaustion, not a proof of pre-execution rejection.
- Failover activation and retry policy are separate decisions.
- Existing durable Issue/comments/task state can be used by a future/new execution to understand what happened.
- Any future automatic replay exception must be backed by structured proof, not string heuristics.
- Session/provider boundaries must remain explicit.

## Considered Alternatives

### Alternative A — Immediately retry every quota-failed task on fallback

**Pros**

- Fastest apparent recovery.
- User may see work continue automatically.

**Cons**

- Can duplicate external side effects.
- Replays from an uncertain point in the workflow.
- Cross-provider sessions may not share execution memory.
- Conflates failover with retry.

**Decision:** rejected.

### Alternative B — Retry automatically only when the raw error looks like an HTTP 402

**Pros**

- More specific than retrying every quota string.

**Cons**

- HTTP status still does not prove when in a multi-turn/tool-using run the failure occurred.
- Raw text is not a trustworthy side-effect boundary.

**Decision:** rejected.

### Alternative C — Use tool-count heuristics to decide whether replay is safe

**Pros**

- Could avoid replay after some known tool calls.

**Cons**

- Not every side effect is represented uniformly.
- Provider/runtime wrappers may perform work outside the counted tool channel.
- A model may have produced externally acted-on output before the observed count.

**Decision:** rejected for v1.

### Alternative D — Fail the triggering task, switch only future work, require structured proof before any future auto-replay optimization

**Pros**

- Safe and deterministic.
- Clean separation between routing state and task retry semantics.
- Allows a later adapter-level optimization if evidence becomes strong enough.

**Cons**

- The run that discovered quota does not automatically continue on fallback.
- User or subsequent workflow may need to re-trigger work after inspecting current state.

**Decision:** accepted.

## Decision

When a primary-routed task settles with canonical `agent_error.provider_quota_limit`:

1. settle that task as failed under ordinary Multica task semantics;
2. activate fallback state for **future** work if ADR-008 conditions are met;
3. do **not** automatically clone/retry/replay that task onto fallback;
4. preserve all existing durable work artifacts and failure history;
5. let a later user/system/Agent trigger create a new governed execution using current state.

Failover is therefore a forward-routing decision, not a retry of the triggering execution.

### Why the canonical quota reason is insufficient for replay

`provider_quota_limit` reliably answers:

> Why did this provider interaction fail?

It does not reliably answer:

> Did this entire Agent run perform zero side effects before that interaction?

The second question is the replay-safety boundary and remains unknown in v1.

### Future exception standard

A future automatic immediate retry on fallback may be considered only when the runtime/provider adapter emits structured, trustworthy evidence that the failed execution was rejected before generation/tool/side-effect execution.

Examples of potentially sufficient future evidence might include an adapter-owned pre-execution phase marker or provider response contract proving no Agent turn began.

Raw error substrings, timing, token counts or tool-count guesses are not sufficient.

Any such optimization requires a separate accepted ADR because it changes the side-effect safety contract.

### Manual rerun

A human may deliberately rerun/re-trigger failed work using ordinary Multica controls after reviewing the current Issue/repository/external state.

That is an explicit new action, not automatic failover replay.

When that new task is created while failover remains active, ordinary target routing selects fallback.

### Session continuity

Do not attempt to translate or inject a primary provider's session ID into another runtime/provider.

Existing Multica claim code already supplies prior provider session IDs only when runtime identity matches. This guard remains authoritative and must not be weakened by failover.

If fallback uses another runtime, the fallback execution reconstructs context through durable Multica state such as:

- Issue title/description/status;
- comments/threads;
- repository/worktree state where applicable;
- prior task results/history exposed through existing mechanisms;
- explicit handoff/current trigger.

If primary and fallback use the same runtime, existing upstream session-resume safety remains applicable. Failover does not add a special cross-target resume override.

### Retry lineage

Existing upstream retries that are already considered safe for their own failure reasons remain governed by upstream logic.

This ADR specifically forbids adding an automatic quota→fallback retry path.

A fallback-routed task's own legitimate upstream retry, where one exists for another failure such as network, must preserve its fallback route so the retry does not silently switch targets mid-lineage.

## Consequences

### Positive

- Avoids duplicate side effects.
- Clear separation of failover state from retry semantics.
- Cross-provider fallback does not invent session portability.
- Failure history remains truthful: the primary task really failed.
- Future safety improvements have a high evidence bar.

### Negative / Trade-offs

- The first quota-failed run is not automatically completed by fallback.
- Users may need to re-trigger a task in some workflows.
- Perceived failover is effective for subsequent work rather than retroactive continuation.

## Data Impact

No automatic child/retry task row is created solely because failover activated.

The original task retains:

- its primary `runtime_id`;
- canonical quota failure reason;
- existing result/error/session/work metadata;
- normal retry/rerun lineage as upstream defines it.

Failover state may reference the triggering task ID for audit, but does not mutate the task into a fallback execution.

## Backend / Runtime Impact

The failover activation hook must not call ordinary retry creation for the triggering quota task.

Target routing applies when a later task is actually created.

Daemon requires no special mechanism to replay partial provider state across execution targets.

## API / CLI / UI Impact

UI should communicate two facts separately:

```text
This run failed because the primary quota was exhausted.
Future runs will use the fallback while primary recovers.
```

Do not imply that the failed run itself was automatically resumed unless a future accepted replay contract is implemented.

A user-facing rerun action remains explicit.

## Failure and Edge Cases

- Quota occurs before any apparent tool call: still no auto-replay because absence of observed tools is not proof of no side effect.
- Quota occurs after several tools: task fails and fallback activates for future work.
- User manually reruns immediately: new task routes according to current failover state.
- Primary recovers before manual rerun: new task may route primary because it is a new routing decision.
- Fallback task gets a retryable network error: existing retry policy may retry it, but route remains fallback.
- Cross-provider fallback: no primary session ID is reused.
- Same-runtime fallback: existing session safety controls remain unchanged; this ADR does not force or forbid a session that upstream considers safe for that runtime.

## Testing Requirements

- primary quota transition creates no automatic retry task;
- original task remains terminal failed with quota reason;
- subsequent new work routes fallback while fallback state active;
- manual rerun is a distinct task/action;
- fallback retry lineage preserves fallback route when upstream retry is otherwise allowed;
- cross-runtime fallback claim receives no primary runtime session;
- no failover code changes upstream quota retryability;
- simulated tool side effects are not duplicated by failover activation.

## Upstream Compatibility

This is intentionally conservative and preserves current task failure/retry semantics.

No new replay engine, side-effect ledger or session translation layer is introduced.

If upstream later exposes a structured execution-stage contract proving pre-side-effect rejection, that evidence can support a separate ADR for safe immediate replay.

## Open Questions

- Whether a future provider/runtime can prove pre-side-effect rejection strongly enough for automatic replay is intentionally deferred.
- UI wording for "future work uses fallback" should be tested so users do not assume the failed run itself continued.
