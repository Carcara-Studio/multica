# Transactional direct-report delegation admission

- Issue: #75; follows #73/#74
- Authority: ADR-001, ADR-003, ADR-004 and ADR-014
- Fork baseline: `53a17cd044f1d8012e1162bf7c0b3fdb45c0e631`
- Upstream baseline: `47120a4131564175da044d63d897ed9b5d76979a`

## Implemented boundary

`projecthierarchy.CheckDelegationRelation` is an internal structural gate.
The caller must supply independently resolved identities and a real, existing
`pgx.Tx`, use one Project per action and roll back on any error. The function
neither begins nor commits a transaction and creates no Issue/task.

The first delegation slice is deliberately limited to a parent Issue currently
assigned directly to the delegating Agent. The parent must still be in the
expected workspace/Project. Both Agents must be live user Agents in that
workspace, with active memberships in the same valid Project tree. The target
must report directly to the delegator. Peers, upward/self/deeper-descendant and
cross-Project delegation are rejected. Mention and Squad routing are unchanged.

The caller retains these locks until commit/rollback:

1. workspace `FOR NO KEY UPDATE`, fencing teardown and serializing with the native Issue counter before Project locks;
2. Project structural `FOR UPDATE`, using the existing hierarchy writer protocol;
3. parent Issue `FOR UPDATE`, freezing its Project/assignment;
4. relevant Agent rows `FOR SHARE` in UUID order, freezing archive/owner/config
   changes to those rows.

The workspace lock is intentionally stronger than a lifetime key-share fence:
ordinary Issue creation holds its counter row before INSERT checks Project
foreign keys. Taking Project first and upgrading workspace later would permit an
inverted wait. The gate acquires the counter-compatible lock first; it does not
increment the counter. This serializes admission with other workspace Issue
creates and is a deliberate contention cost.

Call before taking Issue/property/Agent/counter locks. Future integration must
review the complete upstream create/teardown/permission lock graph; this order
is not a blanket approval for arbitrary outer transactions or multi-action use.

The returned identifiers are structural facts, not a transferable authorization
receipt. They grant no caller authentication, target invocation permission,
runtime readiness or execution/idempotency capability. Those checks must be
composed independently in the same final create operation; no public caller
can invoke this gate yet. A fresh transaction must recheck the relation.

## Child creation and idempotency remain open

The inspected `IssueService.Create` owns its transaction and commits before
publishing events and ordinary assignment enqueue. Wrapping it with another
transaction would not make a hierarchy check atomic with its child write or
side effects. Do not hold a relation-check transaction open while calling
ordinary Create through a separate connection.

The existing active-title duplicate guard is not a stable delegation-operation
key: a deliberate follow-up may share a title, and a completed/deleted child
must not allow a network retry to create a new logical operation. Inspection
found no general Issue-create idempotency key in the current params/schema.
Autopilot quota and maintenance-job keys are unrelated subsystems and must not
be reused for hierarchy work.

The next integration needs a narrow in-transaction admission point in native
Issue creation, durable operation identity/lineage, replay and changed-payload
semantics, deletion/tombstone behavior, and atomic rollback/commit coverage.
Storage/index/lifecycle choices require explicit design under ADR-004/014/015.
No new ledger/table, generic callback framework or separate executor is added
by this prerequisite. Do not expose a delegation endpoint/CLI until the entire
authenticated invocation and idempotent child-create path is complete.

## Validation

Tests cover direct-edge acceptance; peer/upward/self/deeper/cross-Project/
cross-workspace rejection; missing/no-Project/moved/reassigned/Squad parents;
archived relevant Agents/memberships; invalid identity and absent transaction.
Separate transactions attempt conflicting row locks with NOWAIT and must receive
55P03 while the caller retains admission. After caller commit/rollback, those
locks are available; an actual reparent then invalidates the old relation.
A busy native workspace counter blocks admission before Project acquisition.
The gate creates neither child Issues nor queue rows.

No local execution environment is available. Compile/vet/full PostgreSQL/race
CI is required for the exact merge head. No local test/formatting result or
installed-agent/account/inference use is claimed. Public delegation, automatic
fallback and primary recovery probes remain disabled.
