# ADR-026 — Protected RPC evidence exchange transaction boundary

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #59
- Extends: ADR-022, ADR-023, ADR-024, ADR-025

## Decision

Compose the live hub connection source, current protected configuration authority,
target metadata assessment, independent observation snapshots and durable challenge
ledger in an internal `EvidenceExchange`. No public RPC method/endpoint or daemon
capability is registered. Positive integration tests use local WebSockets and a
private fixture observer; production construction has **no approved observer**
and still uses the empty production adapter approval policy.

Selection intent contains Agent/workspace/runtime/route/purpose only. Caller user,
connection UUID, generations, catalog and credential/configuration epochs are
derived independently of reports. `RuntimeConnectionScope` returns immutable scalar
identity from the real hub RPC context, only for a live attached runtime and the
connection's authorized workspace scope. A serialized/copied identity or request
body cannot manufacture that context or broaden its runtime/workspace scope.

This boundary deliberately supports protected management preflight through human
PAT/JWT connections. It parses the authenticated user UUID and reuses
`ConfigManager.begin`: workspace owner fence, current membership/Agent owner/admin
policy, Agent lock and sorted primary/requested runtime SHARE NOWAIT locks. There
is no runtime-owner impersonation for daemon tokens; those are rejected even when
a user field is present. A worker/delegated execution authorization protocol is
separate future work, not an exception to protected configuration authority.

Under these locks, reload optional config/state and the selected runtime, reject
missing/suspended/archived/inconsistent policy, and build connection identity from
current DB runtime facts plus the actual socket UUID. A reviewed server-owned
source supplies bounded in-memory catalog/target identity snapshots. Resolve the
canonical binding with `DescribeTargetBinding`, then issue/consume through the
ADR-025 ledger in the same transaction. No observer may do CLI/account discovery,
network or DB work while owner/state locks are held.

Before commit, repeat catalog/identity observation and require an identical typed
request. Then recheck live runtime/connection scope, including removal during the
last observation. Any failure rolls back and returns no challenge or verified
value. Commit failures return a static persistence error and no authority. A
validly scoped but rejected report commits its nonce burn before returning the
verification error. Successful results are returned only after commit.

## Limits and compatibility

These observations and live-scope checks are point-in-time snapshots, not a lease
over future execution. Changes can happen after commit/return. Writers/claims must
still revalidate current identity, scope, generations and owner/claim fences at
their own execution boundary. This exchange never changes availability, restores
health, claims a task, performs inference or conveys a provider conversation ID.

The observer interface and policy fields are private. Transport callers cannot
install an observer built from the report under verification. No production
constructor approves a fake or real observer. Implementing/reviewing actual
effective credential/config/default-model observation and complete task/probe
guarantees remains necessary before any public transport integration is enabled.
The Codex protocol's account description alone does not establish the effective
credential/configuration epochs required by ADR-023; the official account schema
was inspected as a candidate, not accepted as such evidence:
https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/schema/json/v2/GetAccountResponse.json

No SQL/schema, existing RPC signature/envelope, public API/UI, default Agent path,
executor or provider invocation changes. Remaining gates include reviewed adapter
observers, authenticated observation/challenge/report wire transport, delegated
daemon authorization, all enqueue/retry/claim integration, reliable quota
activation and real minimal inference/settlement. Automatic fallback remains off.

## Verification

Existing hub tests retain socket/reconnect/disconnect/sibling/timeout behavior.
New scope tests reject body-supplied user/workspace and removed runtime scope.
PostgreSQL tests compose actual hub-created RPC contexts with protected owner
transactions and the durable ledger: commit-before-output, replay rejection,
default/no-context/foreign scope denial, current membership/owner/admin policy,
daemon-token denial, both targets and separate primary probe purpose, late epoch
change/runtime detachment rollback, commit failure rollback, invalid-report burn
and archived Agent rejection. No installed agent CLI, account or real inference
is accessed. Existing handler/service/server suites retain production behavior.
