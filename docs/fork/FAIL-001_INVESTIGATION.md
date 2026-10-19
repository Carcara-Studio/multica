# FAIL-001 — Implementation-time evidence and remaining integration gates

Issue: #37. Snapshot: 2026-10-18.

## Persistence increment

After PR #38 (`8cbf332a7`), the next isolated increment implements the durable
state/lease boundary in [ADR-017](./adr/ADR-017-failover-state-persistence-boundary.md).
It allocates migrations `900007–900012` and adds transaction-bound sqlc primitives.
It does not enable configuration, automatic routing, inference probes or Agent/runtime
lifecycle hooks. Workspace deletion explicitly cleans the new state through its
existing transaction and teardown manifest. The remaining integration gates below
still apply. Earlier migration-head
and SHA observations below describe the original investigation snapshot.

## Baselines and corrected handoff

- Fork main: `3aec44a5ce2cfd036be5d97cfc11d5c68b537c78`.
- Upstream: `f896cc483379ff16735a638e94b6c64d128329d2`.
- Upstream is four commits ahead of the previous `18fa9f728` baseline; the
  fork has 54 exclusive commits. Synchronization is isolated in PR #36.
- PR #35 is merged and issue #34 is closed/completed.
- The actual hierarchy service is `server/internal/projecthierarchy/`, not
  `server/internal/hierarchy/`. The hierarchy API already exists in
  `server/internal/handler/project_hierarchy.go`; the fork README phase list
  predates that implementation and is not current implementation authority.
- Fork migrations end at `900006`; current ordinary migrations end at `582`.
  `900007` is the next candidate, not a reservation. Recheck before adding DDL.
- PK, no-FK, concurrent-index and legacy-numbering decisions remain unchanged.

## Confirmed seams

| Concern | Actual source | Integration implication |
| --- | --- | --- |
| Claim payload | `server/internal/handler/daemon.go`, Agent load/runtime fence and `TaskAgentData` assembly | Keep the primary fence; permit fallback only through a validated durable route. Apply target fields before payload/options/MCP composition. |
| Model selection | `server/internal/daemon/daemon.go`, `runTask`, `resolveTaskModelSelection`, `qualifyTaskModel` | Agent model wins, then daemon entry model, then provider default. Preserve the same resolution for each target. |
| Provider/backend selection | `server/internal/daemon/daemon.go`, `runTask` and backend construction | Runtime provider/custom profile selects executable/protocol; model string alone cannot switch providers. |
| Failure production | `server/internal/daemon/daemon.go`, `classifyPoisonedError` and final failure reporting | Reuse canonical taskfailure reasons; do not infer quota from liveness. |
| Failure consumer | `server/internal/service/task.go`, `FailTaskWithTransition` | Sanitizes diagnostic, classifies/normalizes reason, locks chat session, settles task and creates only allowlisted retries in a transaction. |
| Historical classification | `server/pkg/taskfailure/classify.go` | Broad quota/credits words can precede rate-limit matching. ADR-016 adds affirmative evidence without changing upstream taxonomy. |
| Config mutations | `server/internal/handler/agent.go`, `UpdateAgent`, model/thinking/service-tier validation and response redaction | New fallback subresource must use existing management gates and secret handling. Do not add an unredacted target snapshot API. |
| Archive/restore | `server/internal/handler/agent.go`, `ArchiveAgent`, `RestoreAgent` | Archive currently updates row then cancels tasks. Probe invalidation must join archive atomically; restore must not resurrect tokens. |
| Runtime cleanup | `server/internal/service/runtime_teardown.go`; `server/pkg/db/queries/runtime.sql` | Runtime teardown deletes system Agents and clears related tables. Optional fallback rows must be cleaned before Agent removal, including fallback runtime references. |
| Workspace deletion | `server/pkg/db/queries/workspace_delete.sql` | Add explicit cleanup for fork tables before deleting Agents/tasks; no FKs/cascades. |
| Periodic work | `server/cmd/server/runtime_sweeper.go`, `runPeriodicSweep`; `server/cmd/server/main.go` | Reuse bounded periodic worker pattern for durable due/expired-lease scans. Do not place network inference inside runtime liveness sweep. |
| Runtime-local control transport | `server/internal/handler/daemon.go`, model-list heartbeat handling; `server/internal/daemon/daemon.go`, `handleModelList`; `server/internal/daemon/client.go` | Reuse optional heartbeat/result topology. Redis pending transport is not durable lease authority or recovery proof. |
| Existing locking | `server/pkg/db/queries/agent.sql`, claim/reclaim queries | PostgreSQL row locks and SKIP LOCKED are already used. All candidate/list/reclaim fences must understand route markers, not only final payload assembly. |
| CLI | `server/cmd/multica/cmd_agent.go` | Add optional fallback subgroup; existing create/update defaults remain unchanged. |
| Shared clients/UI | `packages/core/api/schemas.ts`, `packages/core/api/client.ts`; `packages/views/agents/components/tabs/runtime-config-tab.tsx` | Add independent optional schema/endpoints and configuration/status view; account for shared web/desktop and separate mobile client. |

All upstream-owned production files above inspected for routing, classification,
Agent mutation, heartbeat and task creation are identical between the merged
sync tree and upstream at this snapshot. Fork-only regression tests are additive.
The sync itself changes daemon/agent Cursor steering; do not mix feature edits
with that merge.

## Task writer inventory

Production SQL inserts into `agent_task_queue` occur in four query files:

| SQL writer | Production callers |
| --- | --- |
| `agent.sql`: `CreateAgentTask`, `CreateDeferredChannelIssueTask` | `service/task.go`: issue enqueue, mentions/squad leaders, later rerun creation |
| `agent.sql`: `CreateQuickCreateTask` | `service/task.go`: quick-create, with and without source-context transaction |
| `agent.sql`: `CreateRetryTask`, `CreateManualQuickCreateRetryTask` | `service/task.go`: failure auto-retry, retry recovery, explicit source-context retry |
| `chat.sql`: `CreateChatTask` | `service/task.go`: `enqueueChatTaskTx`, quick-action regeneration |
| `autopilot.sql`: `CreateAutopilotTask` | `service/autopilot.go` |
| `wakeup.sql`: `CreateWakeupTask` | `service/issue_wakeup.go`, `service/issue_wakeup_system.go` |

Several callers already hold chat/wakeup transactions. A resolver that opens a
separate transaction would break atomic task+route insertion and risk lock
inversion. Pass the caller's transaction into target selection. Do not assume
centralizing ordinary issue enqueue covers autopilot, wakeups or chats.

Retries must copy the original route and target generation in the same
transaction as the child, not re-resolve availability. Explicit new reruns need
their own documented policy. There must be no route-marker insert after task
publication/commit.

## Design carried forward from accepted ADRs

Use three additive structures: optional `agent_fallback_config`, durable
`agent_failover_state`, and `agent_task_execution_route` for fallback-routed tasks.
Absence preserves primary behavior. Do not rewrite Agent primary fields or
historical task runtime IDs. No backfill of idle Agents is required.

Fallback config needs a generation; route markers bind task ID, Agent/workspace,
runtime and generation. State needs primary generation identity, version, reason,
first failure time/task, next probe time, last outcome, claim token, lease and
dispatch time. Use constrained text states and explicit application cleanup,
not new FKs. Every index gets a separate concurrent-index migration.

First quota transition schedules `failed_at + 30m`; repeated failures do not
postpone it. Probe claim is durable and fenced. Dispatch and settlement require
the same current generation/token and unexpired lease; duplicate/stale results
are no-ops. Probe timeout/crash keeps fallback and schedules another attempt
30 minutes after settlement. Ordinary work remains fallback while probing.

Probe uses the primary runtime's actual credentials/model path. It must bypass
`runTask`, repository preparation and session resume. `agent.ExecOptions` has
no universal no-tools/no-MCP inference contract. Merely calling ordinary Execute
with a short prompt or MaxTurns=1 does not prove a safe probe. Capability-gate
each adapter; model-list success and exit-code-only success are insufficient.

## Unclosed gates before enabling failover

1. Complete safe inference adapters and an explicit tested compatibility matrix.
   The current generic execution API cannot guarantee tool-free inference.
2. Finalize transaction lock order against chat, wakeup, quota settlement,
   config changes and archive; ADR-011 explicitly requires this before merge
   of stateful integration. Never hold a state row while waiting on a chat lock
   if failure settlement holds chat first and then requests state.
3. Specify primary generation fingerprint/invalidation for Agent settings,
   runtime profiles/default models and credential changes. Runtime-local
   credentials cannot be detected by hashing server-side Agent config alone.
4. Design redacted fallback config/status API with existing management gates,
   including invalid config edits while fallback-active and stale queued routes.
5. Verify DB migration, claim/reclaim discovery, enqueue/retry lineage and
   lifecycle cleanup under concurrent workers before exposing configuration.

These are engineering work items, not requests for human approval. Do not
describe the end-to-end design as closed or fallback as enabled until they pass.

## Initial implementation and validation

The first isolated increment is ADR-016's conservative quota evidence predicate.
It is deliberately not connected to task settlement. It introduces no runtime,
API, CLI, UI, database or task behavior change and can be reviewed independently.

Local checks at the synchronized source tree: `go test ./pkg/taskfailure` and
selected daemon/agent Cursor supplement/model-resolution tests passed with Go
1.26.9. The evidence gate has its own positive/negative and canonical-reason
matrix. Full DB/concurrency/migration verification belongs to subsequent
stateful increments and CI; no skipped DB test is counted as a pass.

## Optional target and task proof persistence (2026-10-19)

Fork baseline `3efc8afd7`, synchronized through upstream `e752b971b`. ADR-018
refines ADR-007: primary tasks of fallback-enabled Agents also require immutable
enqueue proof, avoiding stale primary failures after recovery. Migrations
900013–900021 add optional secret-free target metadata and route proofs; sqlc
operations remain internal and unused by production task/config flows. Workspace
cleanup is integrated. Model/provider capability validation, target-specific
overrides, protected secret handling, all enqueue/claim/lifecycle integrations
and safe primary inference adapters remain required. This is a persistence
increment, not an enabled fallback feature.

## Metadata gate and atomic invalidation (2026-10-19)

ADR-019 reuses the current upstream option/catalog helpers for an isolated
necessary metadata assessment; positive metadata is not full tools/probe readiness.
Existing config update/removal now atomically rotate/invalidate state using both
epochs and expected version, preserving known unavailability and queued proof.
No production config/task integration is enabled. Complete protected target
configuration, owner/lifecycle invalidation, all task writers/claim fences and
credential-faithful tool-free adapters remain required.

## Agent lifecycle integration (2026-10-19)

ADR-020 connects existing Agent edits/clears, protected env/audit, runtime-skill
controls, archive, member revocation and user-Agent runtime teardown to atomic
optional state invalidation. Default Agents are never initialized. State locks
are a final leaf after existing owner/chat/task/audit work; single archive commits
before cancellation. SQL Agent method signatures/public responses are unchanged.
Runtime lifecycle and terminal cleanup are extended by ADR-021 below; runtime-local
account/default-model epochs remain a separate gate, alongside config/routing/claim/probe enablement.


## Runtime lifecycle integration (2026-10-19)

ADR-021 adds transactional profile command/args/enable invalidation, runtime
visibility invalidation, and conservative legacy consolidation suspension. Both
primary and fallback runtime references participate, including Agents whose
primary is elsewhere. Runtime/profile teardown and retention GC finalize optional
policy only after all owner/task writes, deduplicating Agents shared across
profile instances. Terminal system-Agent and builder-chat deletion explicitly
prune optional config/state/route rows; ordinary chat deletion preserves policy.
No automatic routing or inference is enabled. Account/credential/default-model
epoch transport, protected fallback configuration, all enqueue/retry/claim gates
and safe inference adapters remain open.


## Protected target configuration boundary (2026-10-19)

ADR-022 adds a versioned nullable execution document and an internal transactional
ConfigManager for authorized human save/inspection/reveal/removal. It reuses
upstream env sentinel/ownership logic and activity_log auditing, never inherits
primary secrets, drops masked credentials when changing runtimes, and returns no
plaintext after audit/commit failure. Metadata-only writes clear the document.
900022 contains no FK/index/backfill; existing metadata rows remain incomplete.
No endpoint/UI/CLI, daemon secret delivery, execution approval or automatic
routing/probe worker is enabled. Provider/tool/MCP/context validation, runtime
credential/default-model epochs and all enqueue/retry/claim gates remain required.


## Target evidence and resolution (2026-10-19)

ADR-023 implements a pure verifier bound to target generations, authenticated
runtime connection and target-specific credential/config epochs. Its production
factory approves no adapters, so neither saved config nor daemon feature claims
can grant readiness. Task/probe guarantees are separate and no evidence marks
primary healthy. Internal enqueue/queued-target helpers isolate fallback fields,
keep shared identity, preserve persisted routes through availability changes,
reject stale generations and require fresh opt-in sessions pending a scoped
session ledger. No production writers/claims/worker/transport or public API/UI
are wired. Real adapter/epoch tracking, ledger/transport, complete context/tool
compatibility and all writer/retry/claim/quota/probe integration remain required.

## Live transport identity (2026-10-19)

ADR-024 adds a private server-generated UUID per daemon WebSocket, available to
actual hub RPC contexts only while their connection and selected runtime scope
remain live. Reconnects/concurrent sockets cannot share identity; disconnect,
runtime removal and request cancellation invalidate lookup. This supplies a
current connection source for future evidence transport without trusting a
header/body or broadening generic RPC authorization. Credential/configuration
epochs, durable challenge consumption, evidence transport, real adapter approval
and production routing/probing remain unimplemented and disabled.
