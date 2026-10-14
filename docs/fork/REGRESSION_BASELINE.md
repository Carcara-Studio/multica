# Multica Fork — Upstream Regression Baseline

> Issue: #9 — MFK-006  
> Snapshot date: 2026-10-14  
> Fork baseline: `0b233f5e72234c5c5cb8f063a983657578ebf0ed`  
> Investigated upstream baseline: `52b26900c51f759b87707b4ea35fa9f141b83b52`

## 1. Purpose

This document defines the regression baseline that must protect existing Multica behavior while the fork introduces Project Hierarchy and primary/fallback failover.

The baseline intentionally reuses upstream commands, tests and CI semantics. It is not a parallel fork-specific test framework.

The baseline distinguishes three kinds of evidence:

- **Executed** — a command/job was actually run against the stated SHA and its result is known.
- **Mapped** — an existing upstream test was inspected and shown to protect the relevant behavior, but MFK-006 did not independently execute that individual test outside its containing CI run.
- **Gap** — current behavior is important to the fork, but MFK-006 did not locate a sufficiently narrow regression test for the exact contract. A focused fork regression test is required before modifying that contract.

A green workflow must not be interpreted as proof that every path-filtered job executed.

---

## 2. Baseline SHAs

### Fork

```text
Carcara-Studio/multica
main
0b233f5e72234c5c5cb8f063a983657578ebf0ed
```

At this point the fork contains governance and architecture documentation only. No hierarchy or failover product behavior has been implemented.

### Upstream

```text
multica-ai/multica
main
52b26900c51f759b87707b4ea35fa9f141b83b52
```

Commit message:

```text
MUL-8012: fix(inbox): restrict notifications to current workspace recipients (#9242)
```

The fork's product code is therefore still behaviorally based on this upstream product baseline; fork-only differences at MFK-006 are documentation/governance changes.

---

## 3. Current Upstream Workflow Inventory

Current `.github/workflows/` contains:

```text
ci.yml
desktop-smoke.yml
mobile-verify.yml
openclaw-config-smoke.yml
release.yml
ui-performance.yml
```

`ci.yml` is the primary repository validation workflow. It runs on:

```text
push to main
pull_request to main
nightly schedule
workflow_dispatch
```

It uses path-based scope selection so not every job runs for every commit.

Important current CI lanes include:

```text
changes / CI scope validation
sqlc-check
frontend build/typecheck/lint
frontend tests
views test shards
backend tests
backend agent tests
quality gates
script/installer checks
image checks
Playwright smoke coverage
```

Do not replace this scope mechanism in the fork merely to create a fork baseline.

---

## 4. Repository Command Inventory

### Root `package.json`

Current upstream root scripts include the following baseline entry points:

```text
pnpm check
pnpm run check:go
pnpm run check:frontend
pnpm test
```

Current `check:go` resolves to the equivalent of:

```text
cd server
golangci-lint run ./...
go vet ./...
go test ./...
```

The frontend check uses Turbo and excludes documentation/mobile from the ordinary web/desktop lane, matching upstream CI scope.

### Makefile

Relevant targets include:

```text
make test-fast
make test-race
make test
make check
make sqlc
```

`make test-fast` and `make test-race` call the repository's `scripts/test-go.sh` helper.

`make test` is the database-backed race-test path: it creates/resets a temporary PostgreSQL database, runs migrations and executes the Go suite with race detection before cleanup.

### `scripts/test-go.sh`

The current helper:

- forces `GOWORK=off`;
- runs the server Go test graph;
- supports ordinary and `--race` modes;
- bounds package parallelism;
- remains usable without a database because integration tests that require unavailable services deliberately skip.

This makes the script suitable as a fast developer smoke test, but a run with skipped DB/Redis integration tests is **not** equivalent to the CI backend baseline.

---

## 5. Toolchain and Service Prerequisites

### Backend CI-equivalent environment

Current upstream `ci.yml` uses:

```text
Go: ~1.26.9 (resolved to go1.26.9 in the baseline run)
PostgreSQL: pgvector/pgvector:pg17
Redis: redis:7-alpine
```

Environment in the backend CI job:

```text
DATABASE_URL=postgres://multica:multica@localhost:5432/multica?sslmode=disable
REDIS_TEST_URL=redis://localhost:6379/1
GOFLAGS=-count=1
```

The explicit `-count=1` is intentional: current upstream comments explain that migration files are read at runtime, so cached Go test results could otherwise hide schema regressions.

### Frontend

Current primary frontend CI uses:

```text
Node.js 22
pnpm
Turbo
```

Dependencies are installed with the repository lockfile before build/typecheck/lint/tests.

### Local developer modes

| Mode | DB | Redis | Race | Purpose |
| --- | --- | --- | --- | --- |
| `make test-fast` / `scripts/test-go.sh` | optional | optional | no | fast Go smoke; integration tests may skip |
| `make test-race` | optional | optional | yes | fast race smoke; integration tests may skip |
| `make test` | PostgreSQL required/provisioned by target | depends on tests | yes | stronger local DB-backed suite |
| upstream `backend-tests` CI | pgvector PostgreSQL | Redis | yes | authoritative backend regression gate |

On Windows, Git Bash/WSL is required for the repository shell helpers. A PowerShell developer can also execute the underlying Go commands directly from `server`, but the authoritative command semantics remain those defined by upstream scripts/CI.

---

## 6. Executed Baseline Evidence

### Exact upstream SHA

For upstream SHA:

```text
52b26900c51f759b87707b4ea35fa9f141b83b52
```

GitHub Actions CI run:

```text
run id: 38525111647
event: push
conclusion: success
created: 2026-10-14T06:56:37Z
completed: 2026-10-14T07:06:10Z
```

### Backend Tests

The `Backend Tests` job actually executed and passed against the exact upstream SHA.

The job provisioned healthy PostgreSQL/pgvector and Redis services and resolved Go `1.26.9`.

The effective backend validation included:

```text
cd server
go build -race ./...
go vet ./...
go test -race ./... -timeout 120s
```

with `GOFLAGS=-count=1` from the job environment.

The job passed packages including the areas most relevant to the fork, such as:

```text
server/internal/handler
server/internal/scheduler
server/internal/service
server/pkg/agent
server/pkg/db
server/pkg/taskfailure
```

Result:

```text
EXECUTED — PASS
```

### Other jobs on the same CI run

The exact-SHA run also reported success for several other gates, including CI scope validation, Backend Agent Tests, image/script/installer lanes and aggregate quality/backend gates.

However, path filtering skipped several frontend jobs on this particular commit, including frontend tests/build/views/quality lanes. Therefore:

> The exact upstream CI run proves the backend baseline passed. It does not prove that every frontend suite executed for this SHA.

MFK-006 does not manufacture a frontend PASS from a skipped job.

### Fork-local execution during MFK-006

No command was executed on the maintainer's local Windows clone by this ChatGPT/GitHub-only work session. The GitHub connector can inspect repository and Actions evidence but cannot execute commands on `C:\Users\drato\multica`.

Because the fork has not changed product code since the investigated upstream product baseline, the exact upstream backend CI run is accepted as the initial product-code baseline evidence. The first product-code PR must cause the appropriate fork CI paths to execute before merge.

---

## 7. Regression Contract Mapping

### Legend

```text
STRONG   = focused upstream regression test protects the contract
PARTIAL  = relevant behavior is tested, but not the entire fork-sensitive boundary
GAP      = no sufficiently narrow test located for the exact boundary
```

### 7.1 Direct Agent assignment / execution

**Status: PARTIAL / GAP before changing enqueue routing**

Broad handler/service tests exercise direct Agent work and ordinary daemon claim behavior, but MFK-006 did not locate one narrow test whose sole contract is:

```text
Issue assigned directly to Agent
    -> exactly the expected Agent is queued
    -> current Agent runtime is stamped into task.runtime_id
    -> no Squad/hierarchy routing participates
```

This matters because hierarchy and failover will add routing/context logic around the same task creation paths.

**Gate:** before changing the common Agent enqueue/target-resolution path, add a focused fork regression test pinning ordinary direct assignment behavior.

### 7.2 Squad assignment routes only to `leader_id`

**Status: STRONG**

Existing upstream test:

```text
server/internal/handler/squad_assign_trigger_test.go
TestIssueAssignmentTrigger_SquadAssigneeQueuesLeader
```

The fixture creates a Squad with leader/member, assigns an Issue to the Squad and verifies that exactly the leader receives queued work while the member does not.

This is a mandatory regression for Project Hierarchy integration: hierarchy must not replace the Squad entry-point leader.

### 7.3 Squad mention routing

**Status: STRONG**

Existing upstream coverage:

```text
server/internal/handler/squad_comment_trigger_test.go
```

The suite covers `@squad` comment-trigger routing to the Squad leader and ensures members are not automatically fanned out. It also covers ordering/fallback interactions with explicit Agent mentions.

### 7.4 Squad autopilot routing / validation

**Status: STRONG for current Squad/leader semantics**

Existing upstream coverage includes:

```text
server/internal/service/autopilot_squad_test.go
server/internal/handler/autopilot_private_leader_test.go
server/internal/handler/autopilot_mention_authority_test.go
server/internal/handler/issue_autopilot_assignment_authority_test.go
```

The tests cover Squad-assignee resolution/validation, leader availability/access conditions and authority around Squad-driven automation.

Hierarchy implementation must not reinterpret a Squad autopilot target as the Project hierarchy root.

### 7.5 Squad leader briefing

**Status: STRONG**

Existing upstream tests:

```text
server/internal/handler/squad_briefing_test.go
server/internal/handler/squad_briefing_claim_test.go
server/internal/daemon/prompt_test.go
```

They protect the Squad Operating Protocol / Roster generation and claim-time distinction between leader and non-leader tasks. The claim suite explicitly verifies that a non-leader task must not receive the Squad leader briefing.

Any future Project hierarchy briefing must be additive and must not accidentally make ordinary/non-leader work appear to be Squad-leader work.

### 7.6 Child/sub-Issue completion wakeups

**Status: STRONG**

Existing upstream coverage includes:

```text
server/internal/handler/squad_comment_trigger_test.go
  TestIssueCommentTriggerChildDoneWakesParent

server/internal/service/squad_task_enqueue_test.go
  TestCompleteTaskChildDoneParentSquadLeaderNotificationsDoNotSuppressWakeup

server/internal/service/stage_notifications_test.go
server/internal/service/stage_auto_approve_test.go
```

These tests are directly relevant to ADR-004 delegation because Project Hierarchy intends to reuse child Issues and existing completion wakeups instead of inventing a parallel completion channel.

### 7.7 Canonical provider failure classification

**Status: STRONG**

Existing upstream test:

```text
server/pkg/taskfailure/classify_test.go
```

The table-driven classifier suite distinguishes the failure classes relevant to ADR-008, including:

```text
provider quota/balance exhaustion
provider capacity/rate limit
provider auth/access
provider network
provider server errors
context overflow
model/config/runtime classes
unknown/local-tool cases
```

This is the primary regression contract for the v1 failover trigger.

The fork must consume the canonical classified reason rather than duplicate raw-error matching.

### 7.8 Existing retry semantics and concurrency

**Status: STRONG for retry race/idempotency; PARTIAL for future fallback route lineage**

Existing upstream tests include:

```text
server/internal/service/task_complete_race_test.go
  TestCompleteTaskAtomic_RetryableFailureDoesNotRetrySupersededTask
  TestCompleteTaskAtomic_ConcurrentRetryableFailuresCreateOneRetry
```

The same area explicitly pins that `agent_error.provider_network` remains retryable under the current policy.

This protects the distinction required by ADR-008/012:

```text
ordinary upstream retry policy != automatic quota failover
```

**Future gap:** once fallback routing exists, add a focused test that a legitimate upstream retry of a fallback-routed task preserves the fallback route. That behavior cannot exist upstream yet.

### 7.9 Agent archive / restore

**Status: STRONG for several current archive invariants; hierarchy interaction is necessarily future coverage**

Existing upstream coverage includes:

```text
server/internal/handler/agent_test.go
  TestArchiveRestoreAgent_PreservesSkillsInResponse

server/internal/handler/mika_agent_test.go
  TestArchiveMikaIsRejected

server/internal/service/delegated_failure_recovery_settled_test.go
  archived-Agent task cancellation/settlement regression

server/internal/handler/chat_test.go
  TestSendChatMessage_ArchivedAgent
```

These protect important current archive semantics: archive/restore representation, built-in Agent protection, task settlement and rejection of new chat work to an archived Agent.

ADR-006 adds Project-hierarchy invariants on top of this behavior. Before modifying `ArchiveAgent` transaction boundaries, hierarchy tests must prove that manager/root protection composes with the existing archive behavior rather than replacing it.

### 7.10 Persisted task runtime / runtime-specific claim

**Status: STRONG for runtime ownership of claimed work; GAP for the exact future primary-vs-fallback claim branch**

Existing upstream coverage includes:

```text
server/internal/handler/daemon_test.go
  TestClaimTaskByRuntime_DoesNotReclaimDifferentRuntimeTask
```

The test pins that a daemon/runtime cannot reclaim work persisted for another runtime and that the task's `runtime_id` remains its owning runtime.

This is relevant to ADR-007 because fallback routing deliberately keeps `task.runtime_id` as the durable selected runtime.

**Gap:** before altering the current claim-time check that compares queued task runtime with live Agent runtime, add a focused test for the existing primary-path mismatch behavior. Fallback will require a narrow exception only for tasks explicitly marked as fallback; the ordinary primary path must retain its current rejection semantics.

### 7.11 Prior provider-session continuity

**Status: STRONG**

Existing coverage is concentrated in daemon claim tests, including:

```text
server/internal/handler/daemon_test.go
server/internal/handler/daemon_claim_continuity_gap_test.go
server/internal/handler/daemon_claim_cancelled_session_test.go
server/internal/handler/daemon_chat_resume_fallback_test.go
server/internal/handler/daemon_issue_snapshot_test.go
server/internal/handler/channel_new_e2e_test.go
```

The claim response carries `prior_session_id`, and current tests cover resumable/non-resumable session selection and continuity edge cases.

For failover, the protected invariant is:

> runtime/provider session continuity must never be broadened merely because a fallback target exists.

A cross-runtime fallback must not receive a primary runtime's provider session ID.

### 7.12 Daemon heartbeat / control-plane request topology

**Status: PARTIAL / focused failover test required before probe implementation**

Current production model discovery uses the server → heartbeat → daemon → result pattern that ADR-010 intends to reuse. Frontend/core runtime model resolution also explicitly describes model listing as a heartbeat-piggyback request.

The broader daemon handler test suite contains heartbeat/control-request coverage for existing runtime-local operations.

However, MFK-006 did not locate one narrow end-to-end regression whose contract exactly freezes the model-discovery request lifecycle across server pending store, heartbeat delivery and result settlement.

**Gate:** before introducing `primary probe` protocol fields, add a focused baseline/contract test around the existing optional heartbeat control-request behavior and then add probe-specific capability/old-daemon tests. Do not weaken unknown/old daemon compatibility.

### 7.13 Project / Issue relationship and workspace scope

**Status: STRONG for handler-level workspace integrity and claim context**

Existing upstream tests include:

```text
server/internal/handler/issue_update_project_scope_test.go
  cross-workspace project is rejected

server/internal/handler/issue_move_test.go
  project move/scope behavior

server/internal/handler/claim_project_context_test.go
  Project context at daemon claim

server/internal/handler/issue_table_project_status_test.go
  Project status/query behavior stays workspace-scoped
```

This is the baseline for Project Hierarchy. Hierarchy membership/manager relationships must be stricter still: cross-Project and cross-Workspace structural relationships are invalid.

### 7.14 Issue without Project

**Status: PARTIAL**

Ordinary Issue paths support work without Project context, and many existing tests exercise issues without a project.

There is not yet a fork-specific regression explicitly asserting the new requirement:

```text
issue.project_id is absent
=> no hierarchy lookup, briefing, delegation restriction or escalation behavior is introduced
```

**Gate:** add this regression when the first hierarchy runtime integration is implemented.

---

## 8. Confirmed Coverage Gaps Before Fork Hotspot Changes

The following are not failures of upstream. They are fork-specific boundaries that become important only because we plan to modify nearby behavior.

### HIER-GAP-1 — Direct Agent routing invariant

Before introducing hierarchy-aware runtime context or delegation hooks around direct assignment, add a focused regression proving ordinary direct Agent assignment remains unchanged.

### HIER-GAP-2 — Issue without Project is hierarchy-neutral

Before injecting hierarchy context into daemon/task briefing, prove that an Issue with no `project_id` remains ordinary upstream execution.

### FAIL-GAP-1 — Primary claim runtime mismatch remains rejected

Before adding fallback claim behavior, pin the current ordinary primary-path runtime mismatch rejection with a dedicated regression.

### FAIL-GAP-2 — Heartbeat control request backward compatibility

Before adding a primary-probe control message, pin the existing optional heartbeat-control behavior and prove an old daemon can ignore/omit unsupported control capabilities safely.

### FAIL-GAP-3 — Retry lineage preserves fallback route

This test can only be introduced once durable fallback task routing exists. It must prove that an upstream-authorized retry does not accidentally return a fallback task to primary.

These gaps should be implemented in the focused HIER/FAIL Issues that first touch the associated behavior, not as speculative production changes in MFK-006.

---

## 9. Mandatory Baseline Gates

### Gate A — Before MFK-007 / first hierarchy implementation branch

The following must be true:

1. upstream baseline SHA and fork base SHA are recorded;
2. upstream has not advanced without review; if it has, perform the documented upstream-sync process first;
3. the exact upstream backend CI baseline is known green or any upstream failure is documented and explicitly accepted;
4. no fork-specific product code exists outside an approved implementation Issue;
5. HIER-GAP-1 is added in or before the first change to direct/common Agent routing;
6. HIER-GAP-2 is added in or before the first hierarchy runtime-context integration;
7. existing Squad assignment, Squad mention/autopilot, Squad briefing and child wakeup tests remain green.

### Gate B — Every backend hierarchy PR

At minimum:

```text
relevant focused Go tests
+ go vet for affected packages / upstream-required scope
+ backend race suite through GitHub CI when backend paths changed
```

Database/migration changes additionally require:

```text
migration apply on clean database
migration behavior on existing schema
sqlc generation/check when queries change
DB constraint tests
concurrency tests when hierarchy structure mutates
```

### Gate C — Before failover implementation later

In addition to the general backend gate:

```text
server/pkg/taskfailure classifier tests
existing retry/race tests
runtime-specific claim/session continuity tests
FAIL-GAP-1 primary runtime mismatch regression
FAIL-GAP-2 control-plane/old-daemon compatibility regression
```

FAIL-GAP-3 becomes mandatory when fallback retry lineage becomes reachable.

### Gate D — Frontend/mobile changes

A backend-only PR does not need to invent frontend execution beyond upstream path rules.

When Web/Core/Desktop paths change, the upstream frontend lanes selected by `.github/ci-paths.json` must be green.

When Mobile paths change, use the existing `mobile-verify.yml` workflow rather than folding mobile into an unrelated fork-specific check.

---

## 10. Upstream Sync Interaction

Before each implementation phase:

```text
git fetch upstream
compare upstream/main with documented investigated baseline
```

If upstream has advanced materially:

1. synchronize according to `docs/fork/UPSTREAM_SYNC.md`;
2. re-run/review baseline areas touched by upstream;
3. update this document only when commands/contracts actually changed;
4. do not preserve obsolete fork assumptions merely because an ADR cited an older implementation detail.

Accepted ADR semantics remain authoritative unless upstream evidence requires a new/superseding ADR.

---

## 11. What MFK-006 Does Not Do

MFK-006 does not:

- implement hierarchy;
- implement failover;
- change production behavior;
- rewrite upstream CI;
- add speculative schema;
- pretend skipped tests passed;
- claim local execution that did not happen;
- require every upstream release/mobile/performance workflow for every backend PR.

Its purpose is to establish the evidence boundary from which fork product changes can safely begin.

---

## 12. MFK-007 Pre-implementation Decision

MFK-007 may activate Project Hierarchy implementation only after this baseline is merged and the implementation Issue identifies which baseline contracts its first change can affect.

The first hierarchy implementation sequence should remain data-model-first. It should not modify Squad routing, daemon claim semantics or failover behavior merely to introduce the Project membership schema.

For every future PR, review the following question explicitly:

> Which upstream behavior in this baseline could this change regress, and what test proves it did not?

That question is part of the fork's merge gate.