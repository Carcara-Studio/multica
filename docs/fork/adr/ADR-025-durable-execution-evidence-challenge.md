# ADR-025 — Durable single-consumption execution evidence challenges

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #57
- Extends: ADR-023, ADR-024

## Decision

Store one pending capability challenge per optional Agent policy in the existing
`agent_failover_state` row. Migration 900023 adds nullable token/context/issued/
expiry columns, with no new table, index, foreign key, backfill or ordinary task
lookup. The bounded slot is shared by both targets and task/probe purposes: each
new issuance supersedes the previous nonce. Callers must serialize their desired
exchange or tolerate a superseded response; this never replays a failed Task.

The context contains only the versioned typed ADR-023 target binding,
authenticated connection and opaque credential/configuration epochs/effective
model/purpose. No raw reports, credentials, environment, arguments, runtime/MCP
configuration or user context are stored. Application JSON is limited to 8 KiB;
the DB bounds normalized JSONB to 16 KiB and enforces version/shape, nonzero token,
complete-or-null slot and an exclusive deadline within two minutes of issuance.

`EvidenceLedger.Issue` checks the typed context, current configuration/primary
generations and active Agent/runtime ownership binding. It locks config for share
then state for update. PostgreSQL generates a new random nonce and samples wall
time **after** obtaining those locks. No caller clock/TTL/token is accepted.
Issuing changes no availability version, health, scheduling or probe lease.

`Consume` requires independently current server facts, not values copied from a
daemon report. It matches the entire stored context, exact nonce, both current
generations and active ownership/runtime binding, samples DB time after the
state lock, and atomically clears the slot before returning the old challenge.
Only one concurrent consumer succeeds. The pure verifier then checks the report
and server adapter policy using that DB timestamp. Invalid reports/capabilities
produce no verified evidence and burn their nonce if the transaction commits.
No process-local pending map or startup generation substitutes for this state.

Credential/config/default-model changes and connection replacement/removal are
detected by independently current observations supplied at use time; the ledger
does not discover those facts. Context mismatch cannot consume another context's
pending nonce. Expired slots may remain bounded until replaced; expiry does not
mark a runtime healthy or trigger any inference. Lifecycle generation rotation,
suspension and fallback edit/removal clear the slot atomically. Terminal deletion
uses the existing state cleanup, so no new orphan cleanup graph is introduced.

## Transaction and trust boundary

This is internal leaf persistence with no production caller. Future transport
must obtain binding from `DescribeTargetBinding`, live connection identity from
ADR-024 plus authorized current runtime data, and identity epochs from a reviewed
adapter observer. Existing workspace/Agent/runtime/claim owner locks precede the
config-to-state leaf. Do not acquire more upstream/task locks after this boundary.
SQL relationship predicates alone do not fence concurrent teardown.

Pass `Queries.WithTx` when composing an owner transaction. Do not publish a
challenge or use verified evidence before commit; rollback restores the previous
slot. Standalone persistence calls may use autocommit. A consumed capability
observation is not a one-use execution ticket, dispatch lease, quota signal,
proof of successful inference or authorization to enqueue/claim work. Future
execution still needs every transaction/claim/target freshness fence.

Production `NewEvidenceVerifier()` still approves no adapters. No wire/API/UI,
daemon advertisement, credential inspection, provider execution, automatic
routing/quota activation or recovery worker is added. Remaining gates include
real adapter epoch/context/tool/session guarantees, authenticated challenge/report
transport, all writer/retry/claim integration and real minimal inference.

## Verification and migration recovery

Offline tests cover typed input limits/static store errors. PostgreSQL tests cover
supersession, reconstruction with a fresh manager, eight concurrent consumers,
scope/generation/epoch mismatches, both target routes and separate probe purpose,
availability transitions without health mutation, invalid report/default-policy
denial, transaction rollback and config/lifecycle invalidation. A row-lock barrier
holds consumption until the actual DB deadline expires to prove post-lock clock
sampling. Migration tests exercise up/down/up and invalid slot/TTL/size shapes;
existing lifecycle tests compare full content including the new JSON field.

Migration DDL uses `ALTER TABLE IF EXISTS` and idempotent columns because missing
conditional objects may already have ledger entries. If the state table was
absent, restore its creator/key/index migrations and replay 900023 before using
new queries. Down removes only these optional challenge columns and their check;
existing availability, configuration and task route history are retained.
