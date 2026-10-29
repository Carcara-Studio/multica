# Execution route delivery gate

R4 / #37 / #157 / ADR-018/038/039. Production execution target resolution is
unapproved and automatic routing/recovery remains disabled (#152). A persisted
route records enqueue intent, including a primary selection; it cannot grant
permission to execute an ordinary Agent payload.

Until the approved resolver is integrated, task-token insertion refuses any
existing task route marker. Claim finalization maps the refusal to its existing
typed delivery rejection and rolls back tokens, Remote MCP credentials, snapshots
and delivered-comment receipts. HTTP and WebSocket claim paths share that finalizer.
The handler uses normal terminal settlement with invalid_task_identity and409,
without a substitute task or automatic replay. A failed settlement retains the
existing exact-claim requeue/reclaim repair, not permission to bypass the gate.

Both start SQL paths reject marked tasks before running/supplement capability
writes. Generation-aware start checks the marker after locking its exact claim,
including running replays, so a replay cannot acknowledge unsupported execution.
Its HTTP conflict response does not start or settle already-running work.

The gate checks marker existence independently of mutable Agent/config/state
metadata. Removing configuration, stale generations or orphaned metadata cannot
make marked work legacy. Unmarked tasks retain current behavior, even when an
Agent has a saved fallback configuration. Saving intent alone does not opt a task
into routing. This gate adds no approval map, feature toggle, secret lookup,
route rewrite, availability transition or config/owner lock after a task lock.

Route insertion still belongs in the same transaction as task insertion under
existing upstream owner fences. This barrier does not certify concurrent partial
writers, authorize retroactive route attachment, or replace enqueue/claim/retry
resolution and native run/config acceptance. Do not remove it for a subset of
writers or convert catalog metadata/diagnostic quota into launch authority.

No migration is added. sqlc1.31.1 regenerates the modified token/start queries and
the internal existence query. PostgreSQL tests cover credential/snapshot rollback,
primary/fallback markers with absent configuration, modern/legacy start and
replay, the unmarked configuration-intent control and terminal handler settlement.
These tests do not establish native probe admission or real provider execution.
