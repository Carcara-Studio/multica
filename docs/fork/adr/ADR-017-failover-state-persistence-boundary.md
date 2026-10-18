# ADR-017 — Durable failover state and lease persistence boundary

- Status: Accepted
- Date: 2026-10-18
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #39
- Implements in part: ADR-009, ADR-010, ADR-011
- Supersedes: None

## Scope and evidence

Base: fork `8cbf332a7`; upstream `f896cc483`. The upstream has not advanced.
The next free fork migration is `900007`. The existing migration runner supports
conditions, invalid concurrent-index cleanup, advisory-lock serialization and
lexical ordering. Reuse those mechanisms, not another schema ledger.

Current chat enqueue locks its session then Agent before insertion;
`lock_task_owner_rows` acquires workspace/Agent/Issue/runtime owner locks.
Wakeups have additional issue/wakeup/task locks. Failure settlement locks chat
and Task before terminal supplement/comment work. These cannot safely be folded
into a global state-first lock order by adding one call to `FailTask`.

This increment implements **only persistence primitives**. No task routing,
failure hook, config endpoint, maintenance worker or daemon delivery calls them.
There is no fallback configuration to enable through the product yet. Fallback
config and task route tables are deliberately separate future increments.

## Alternatives

- Add fields to Agent/task tables: rejected; couples availability to upstream
  identity and requires rewriting central contracts before routing is ready.
- Memory/Redis state: rejected; cannot fence restarts and multiple servers.
- Implement schema, routing and adapters together: rejected; a large cross-cutting
  change hides migration/lease failures behind unrelated runtime behavior.
- Add a durable optional state table and transaction-bound sqlc primitives first:
  accepted; independently testable and behavior-neutral until integration.

## Persistence contract

`agent_failover_state` is keyed by Agent, workspace-scoped, and not backfilled.
Absent state retains ordinary upstream behavior. Stored states are
`primary_active`, `fallback_active`, and `primary_probing`.

The row stores an opaque primary policy generation, monotonic version, first
failure task/time, next probe time, last probe time/outcome, recovery time,
suspension time and claim metadata. A claim binds a random UUID token to its
primary runtime and lease. CHECK constraints enforce coherent schedules and
claim tuples. No secrets or provider prompts are stored here.

Relationships are application-owned, with no FKs/cascades (ADR-014). The first
unique index is built concurrently, then attached as a PK. Due-probe, expired
lease and workspace indexes are separate concurrent migrations. Registers in
`cmd/migrate/failover_indexes.go` reuse interrupted-index cleanup. PK conditions
also recover a crash after attachment but before ledger recording without
creating an extra unique index or attaching a second PK.

## Transitions and timing

- Initialization is explicit and requires an existing active Agent in the same
  workspace. Conflict is a no-op; it never replaces another generation.
- Activation requires the expected primary generation/version and primary-active
  state. It also verifies a persisted failed canonical-quota Task owned by the
  Agent. First probe time is exactly that Task's `completed_at + 30 minutes`.
  Duplicate/stale activation does not move the schedule.
- Due claims use `FOR UPDATE SKIP LOCKED`, scoped to workspace and primary runtime.
  They move to probing, remove the due time and create a two-minute lease/token.
  This lease reserves a contract for a later adapter timeout of at most 60 seconds
  plus transport/report allowance. No inference adapter is implemented here.
- Dispatch is a once-only conditional write for the same token/generation/runtime.
- Settlement requires dispatch, an unexpired matching claim and current Agent
  eligibility. Exact `success` restores primary; all other outcomes retain
  fallback and schedule 30 minutes after settlement. No task rows are created.
- Lease expiry is bounded to 100 rows per scan, clears claim authority and
  schedules 30 minutes after crash-recovery settlement. It never immediately
  dispatches another probe.
- Suspension invalidates a claim without deleting audit state. Generation rotation
  invalidates claims and preserves fallback availability state; editing config
  does not implicitly prove primary health. Neither primitive silently resumes a
  suspended Agent.
- Deletion primitives are for terminal Agent/workspace cleanup, not routine reset.

Activation, dispatch and settlement lock the state row **before** sampling the
database wall clock. A statement/transaction timestamp sampled before lock wait
must not allow an expired result to restore primary. Tests explicitly hold a row
lock, expire its lease while a competing settlement waits, then release it.

## Transaction and integration contract

These generated primitives operate through caller-supplied `db.Queries`, including
`WithTx`. They do not open nested transactions, acquire upstream owner row locks,
perform network I/O, or mutate user Tasks. Probe-only workers may lock state rows
and perform MVCC eligibility reads, but must not acquire task/chat/owner locks
while holding those state locks.

State-row locking serializes activation, probe claim/dispatch/settlement, generation
rotation and suspension. The later routing/config/lifecycle integration must
compose this with upstream transactions and test its complete lock graph before
being enabled. In particular, archive must suspend state atomically and terminal
deletion must clean it explicitly; the MVCC Agent eligibility read alone is not
a proof against a concurrent archive/rebind. These lifecycle hooks are not wired
in this increment.

Activation is a low-level necessary check, **not authorization to fail over**.
The future transition service must additionally prove ADR-016 reliable evidence,
primary route, a compatible enabled fallback, and an enqueue-time generation and
state version. Reading the latest version at failure time is insufficient: an old
primary Task must not reactivate fallback after recovery. Task route persistence
must make this proof durable before hooking activation into settlement.

An opaque generation is not a credential hash. Future configuration transactions
must rotate it for execution-critical changes. Daemon-local credential/account
identity needs an explicit adapter/runtime epoch contract; it cannot be inferred
from the Agent row. No claim of that runtime capability is made here.

## Probe protocol boundary

Future heartbeat delivery is optional and capability-gated; result authorization
must use existing runtime/workspace gates before invoking these primitives.
Only a valid minimal inference response may report `success`. The token is not a
replacement for authentication. Old daemons cannot prove recovery by heartbeat,
model listing, exit code alone or lease expiry. No real CLI/account is accessed by
the persistence tests.

Generated rows are internal and include claim tokens: do not serialize them
directly as a public status response. A later API needs a redacted DTO. Public API,
CLI, shared web/desktop and mobile contracts remain unchanged in this increment.

## Validation and rollout

DB tests cover CAS activation, duplicate/distributed claim and dispatch, rollback,
failed outcomes, generation rotation, suspension, archive/delete/runtime/workspace
fences, lease crash recovery and lock-wait expiry. Migration tests run up/down/up
in an isolated schema, verify PK/index/constraint shape, no FKs/backfill and
attachment-ledger crash recovery. sqlc output is regenerated with `make sqlc`.

Local DB tests cannot run without the managed PostgreSQL environment. Compilation
and skipped DB tests are not evidence of DB correctness; PR CI must execute the
DB-backed suites and migrations before merge.

No existing execution behavior changes until later integration is explicitly
implemented. Rollback is safe now while the table is unused. Once enabled later,
disable failover and drain routed work before dropping state; deleting availability
state must never be used as an automatic recovery mechanism.

## Upstream impact and remaining work

Only new migrations, an isolated query file, generated DB code, migration hooks,
tests and fork docs are changed. No Agent/task schema or upstream runtime hotspot
is modified. Keep future config, route persistence, lifecycle cleanup, complete
lock-order tests, safe adapters, heartbeat protocol and user-facing configuration
in separate reviewed increments. This ADR closes this persistence boundary; it
does not declare the complete FAIL-001 integration design or feature finished.
