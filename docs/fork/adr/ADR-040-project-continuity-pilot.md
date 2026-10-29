# ADR-040 — Bounded project continuity pilot

- Status: Accepted for this increment
- Date: 2026-10-29
- Scope: #159, explicit maintainer request to prioritize persistent memory and pause R4

## Decision

Deliver one explicitly published continuity checkpoint per project before adding
automatic extraction or retrieval models. Store bounded text, a CAS revision,
member identity and publication timestamp on the existing project row. Treat it
as historical context, never governance or execution authority. Project deletion
already removes the row; no new collection, index, foreign key, queue or executor
is needed. The memory prototype's provenance/scope/budget lessons inform the
contract; its Python engines and external inference adapters are not dependencies.

Authenticated owner/admin/member actors may publish through a dedicated API/CLI.
Task/daemon actors cannot publish or approve context. Membership is rechecked
under the workspace membership fence in the write transaction. Member identity
records who published through that account; it is not evidence that a person read
every source or that the content is correct. No automatic promotion of task output.

Every write carries the previously read revision; first publication uses empty.
Clearing retains a new revision and removes content, preventing stale create/update
requests from reviving old text. There is no blind merge or automatic CAS retry.
Concurrent ordinary project edits do not overwrite checkpoint columns.

The shared claim project resolver appends a labeled JSON-quoted checkpoint to the
existing ProjectDescription transport. Issue/chat/autopilot/quick-create therefore
use one scope boundary and existing daemon briefing/materialization. No extra
wire contract, parallel context store or dependency on an updated daemon is
introduced. Normal project API responses retain their existing description.
Unknown/deleted/foreign project references never retrieve a checkpoint by text.
Invalid optional checkpoint data is omitted with a metadata-only warning rather
than launching with corrupt memory or blocking otherwise valid work.

## Bounds and retention

Content is at most4000 UTF-8 bytes and6000 JSON-encoded bytes; the complete optional
memory section is bounded below8192 bytes. Source identity/revision/member/time
travel with it; the agent must verify mutable claims against current evidence.
Only one current checkpoint exists; this increment keeps no superseded text or
conversation transcript. Do not publish credentials or personal memory here.

Clearing stops future delivery. It cannot erase text already delivered in task
briefs, runtime context files, sessions or user-copied output; those follow their
existing retention/cleanup. A downgrade drops these optional columns: back up or
export desired content before rollback. Automatic extraction, squad-private notes,
cross-project sharing, source freshness verification and delivery-use telemetry
are outside this first increment, not silently claimed as implemented.

## Acceptance

### Follow-up #161: derived report preview

After the first delivery pilot, allow a human-only read endpoint/CLI to derive a
bounded, unreviewed final-report draft without another model call or persistent
draft store. Capture resolved project/explicit squad provenance in new issue
claim snapshots; missing/mismatched scope fails closed. No automatic publication,
source freshness claim, squad-private collection or change to issue-body delta
comparison is authorized by this addition. Existing CAS publication remains the
only path that makes the reviewed checkpoint available to subsequent tasks.

Native PostgreSQL tests must show publish/read/CAS/clear/recreate and concurrent
conflict, member authorization and project/workspace isolation. A scoped checkpoint
must appear in the shared claim context and disappear after clearing. CLI tests
cover file/stdin requests, typed receipts, errors and no replay. This validates
delivery, not actual agent use or savings. The first real-project pilot separately
checks whether a new task uses relevant decisions; tokens are secondary.
