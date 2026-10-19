# ADR-022 — Protected target configuration boundary

- Status: Accepted
- Date: 2026-10-19
- Decision owners: Repository agent under standing implementation authority
- Related issues: #37, #51
- Extends: ADR-007, ADR-013, ADR-018, ADR-019, ADR-021

## Decision

Add an optional version-1 `execution_config` JSONB document to fallback config.
NULL remains metadata-only; existing rows receive no credentials or readiness
backfill. The document contains complete target-local environment, arguments,
runtime configuration and MCP configuration. Empty values never inherit primary
values. Agent identity, instructions, organizational membership and shared skills
remain upstream-owned. Version describes serialization, never execution readiness.

Use the existing Agent secret-management principles: database storage as used by
upstream Agent configuration, explicit human management authority, redacted
inspection, key/field-only activity_log audits and fail-closed audited reveal.
No alternate vault, cryptographic scheme or executor is introduced. Extract the
existing env sentinel/diff and ownership predicate into `internal/agentsecrets`;
primary env handlers and the new boundary call the same implementation. Existing
primary env HTTP contracts and audit details remain unchanged.

`ConfigManager` is internal only: no HTTP, CLI/UI, daemon transport or automatic
activation is wired. ManagementActor comes from authenticated server actor
resolution, not request JSON. Only member actors with owner/admin role or the
Agent's human owner with member role can manage. Agent actors are denied even if
the backing user is an owner. System Agents are excluded; archived user Agents
can be inspected/revealed/removed, but cannot save new target settings.

## Transactions and concurrency

Acquire workspace KEY SHARE, check current membership and ownership, then for a
save lock primary/requested fallback runtimes in stable ID order, then the Agent.
Recheck membership/ownership and current primary identity before mutation. Runtime
workspace/private-owner constraints apply to the Agent owner, not the managing
admin. No runtime lookup occurs after config/state locks.

Save/remove require the expected config generation, primary generation and state
version; stale or concurrent edits fail without a lasting audit/write. All owner
and audit writes finish before the config -> state leaf. First opt-in initializes
config then policy in one transaction. A pre-existing policy must match its
expected revision and is rotated; its suspension/availability is preserved.
Existing config with missing state is an inconsistency, never an implicit reset.
Removal rotates/suspends policy and retains immutable queued route evidence.

Environment sentinel merging uses the locked current fallback document. On a
runtime change masked keys are dropped rather than copying credentials to the
new runtime; removals remain in the key-only audit. Other secret-bearing fields
are explicit full replacements, and redacted inspection returns no masked blobs
that could silently replace those values. A metadata-only low-level config
update sets execution_config to NULL, preventing stale credentials from surviving
an edit that does not supply a complete protected document.

Reveal returns plaintext only after its audit transaction commits. Audit,
rotation or commit failures return no secret document. Persistence errors are
static categories: raw database errors can contain secret-bearing rejected rows
and must not be returned/logged by this boundary. Normal inspection uses a
separate projection without raw DB fields, arguments or JSON secret values.

## Validation and remaining gates

Documents are versioned, bounded to 1 MiB, strictly decoded at the top level and
shape-checked. Validation rejects malformed documents without echoing input.
Metadata incompatibility rejects a save; unknown model/catalog capability can
be stored internally for later validation but grants no launch authority.
Arbitrary provider JSON/MCP is stored structurally, not approved for execution.
Provider/tool/MCP/session/context semantics, authenticated capability/account
and default-model epochs, all task writers/retries/claim fences and tool-free
real-inference adapters still need integration before exposing configuration or
enabling failover. No secret is handed to a daemon by this increment.

## Migration and verification

900022 adds one nullable JSONB column with object/version checks, no FK, index or
backfill. Down drops only the document. Reapplying is idempotent. If the base
table was missing when the guarded ALTER was recorded, restore the base table,
remove the 900022 schema_migrations record and rerun it; recording a no-op does
not create the missing column later automatically.

Coverage includes the shared upstream env behavior, isolation from primary
secrets, redaction/audit, human owner/admin/member gates, foreign private and
cross-workspace runtimes, stale/concurrent revisions, dispatched-probe rejection,
first initialization, missing-state rejection, preserved queued proof, and
rollback/no-plaintext on audit/rotation/commit failure. The migration suite runs
up/down/up and rejects unsupported document versions. Local DB skips do not
verify those transactional behaviors; require exact-head PostgreSQL/race CI.
