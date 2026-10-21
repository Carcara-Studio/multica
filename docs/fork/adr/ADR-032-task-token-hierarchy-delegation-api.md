# ADR-032 — Task-token hierarchy delegation API

- Status: Accepted
- Date: 2026-10-20
- Decision owners: Human maintainer (standing governed execution authority)
- Related issues: #80; follows #77/#78
- Supersedes: None; composes ADR-004 and ADR-031
- Superseded by: None

## Context

Fork main 7ea1fd99b84d4385f2896b863a53da765bd84e02 contains native
transactional child creation with durable replay and mandatory authorization.
The inspected upstream baseline remains 47120a4131564175da044d63d897ed9b5d76979a.
middleware.Auth strips actor-source/Agent/task headers and re-stamps MAT
bindings from task_token. handler.canInvokeAgent judges A2A invocation by the
task's top-of-chain originator; daemon/Agent ownership and workspace management
are not substitutes. AgentReadiness admits waitable offline runtimes but blocks
unbound, archived, owner-incompatible and explicitly unusable targets.

These are existing contracts, not new public eligibility declarations.

## Decision Drivers

Expose one explicit one-hop operation using existing authenticated actors and
native invocation/runtime checks, without changing generic Issue create,
mentions, Squads or daemon execution protocols.

## Considered Alternatives

- Agent IDs supplied in JSON or resolveActor's middleware-less compatibility
  path: rejected; insufficient proof of an authenticated acting task.
- Human/admin invocation as manager: deferred; not part of executing-Agent scope.
- New credential/permission scheme: rejected; reuse MAT and normal invoke rules.
- A narrow task-token-only additive endpoint: selected.

## Decision

Register POST /api/issues/{id}/hierarchy-delegations under existing Auth and
workspace middleware. Require a bearer MAT and matching server-stamped
user/Agent/task/workspace. Independently re-read the live token before parsing.
Resolve parent UUID or identifier with loadIssueForUser; derive Project and
delegator/task solely from server state.

Strict request fields: request_id UUID, target_agent_id UUID, nonempty title,
optional description and priority. Reject unknown fields and trailing JSON.
Body size is bounded. Response is {issue_id, replayed}, 201 for creation or
200 for replay. No task ID is implied by the receipt; fetch normal Issue/task
surfaces for current details. Reuse native Issue-created event payload.

In the native Create transaction, re-read token bindings/expiry, membership
and running source task, including original parent/Agent and current source
runtime binding. Exclude Squad/leader task contexts. Token expiry also uses
wall clock because PostgreSQL now() is fixed at transaction start, and a lock
wait must not extend a credential's lifetime.

Use only transaction-bound queries for normal canInvokeAgent, taking the
originator from the current source task, and normal AgentReadiness. Preserve
the approved public_to workspace exception for unattributed internal Agents;
member/team/private rules do not gain a hierarchy bypass. Reauthorize replay.

## Consequences

One explicit API can now create native child work and schedule it normally.
No CLI/briefing tool discovery is added. A later Agent turn must supply its own
current MAT while retaining the original request UUID for the same operation.
No recursive fan-out, parent completion or separate hierarchy executor.

## Data Impact

None: reuse ADR-031 ledger. No migration, FK, extra index or token metadata.
Permission/runtime/token/task reads use native snapshot semantics.

## Backend / Runtime Impact

Reuse workspace -> Project -> parent -> ordered Agent locks from ADR-031.
Authorization reads do not acquire task-row locks after Issue/Agent locks:
upstream completion paths can hold task first then update Issue, so adding
such locks here would introduce an inversion. Authorization is linearized at
fresh admission reads; it is not a task-completion/revocation lease spanning
commit or execution. Native dispatch/readiness checks remain authoritative
after admission. No claim of transactional fencing across all runtime and
permission state. The ordinary post-commit scheduling crash gap remains.

## API / CLI / UI Impact

Additive receipt schema and malformed-response tests. Existing API responses
are unchanged. No UI/CLI command or automatic tool advertisement in this slice.
403 denials do not name permission modes, SQL, runtime internals or secrets.
Changed intent returns 409; deleted child returns 410, without recreation.

## Failure and Edge Cases

Revocation, expiry, terminal/mismatched source task, rebound source runtime,
unusable target, invocation denial and relation changes fail closed before
child/identity writes. Authentication failures retain Auth's 401 responses.
Infrastructure failures are generic 500; native issue-count limits retain
their existing transport response. No retry after an ambiguous dispatch is
introduced: durable creation retry only returns the existing child.

## Testing Requirements

Real Auth/workspace middleware and PostgreSQL fixtures: UUID/identifier parent,
forged headers and member PAT denial, strict DTO/UUID validation, original
human propagation to native task, creation/replay/follow-up, changed intent,
fresh token/task/runtime/permission admission, replay permission denial, member
allow-list and approved workspace/no-human behavior. No real installed CLI,
account or inference. Exact-head backend/frontend CI and Windows gates.

## Upstream Compatibility

One router registration plus isolated handler/schema/tests; no central auth,
task, generic Issue handler or daemon refactor. Existing callback seam and
native helpers remain authoritative. Review future upstream MAT/invocation
changes and converge instead of adding duplicate permission logic.

## Open Questions

CLI/tool advertisement, rich UI integration and durable post-commit dispatch
recovery remain separate work. A stronger task-completion lease would require
a complete native lifecycle lock-order design; it is not implied by this API.
