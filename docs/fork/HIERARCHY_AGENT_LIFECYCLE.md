# Agent lifecycle: archive-writer audit

Status: investigation complete; lifecycle enforcement is **not implemented**.
Issue: #90. Governing decisions: ADR-003/006/014/015.
Reviewed on 2026-10-21 against fork `bc753d218d60793539751ef92029419a5070a3bc`
and upstream `2b645691387e1535cc63d6ebf2ba4e996387c8b2`.
Re-run the inventory before implementation or after an upstream sync.

## Confirmed gap

Current Agent archival does not update Project hierarchy memberships or reject
structural nodes. This follows directly from the writers below; it is a static
code finding, not a newly executed database reproduction. Even sequentially,
archiving a root, manager or leaf can leave its active membership behind.
Hierarchy mutation validation and delegation reject archived Agents, but that
read-time protection does not satisfy ADR-006's commit-time invariant.

The next production change must cover both live archive callers together.
Protecting only the public archive handler would leave workspace member removal
as a bypass. Do not ship a partial guard as complete lifecycle support.

## Writers and containing deletions

Paths below are relative to the repository root. SQL source is authoritative;
generated sqlc wrappers mirror it and require regeneration if SQL changes.

| Entry / source | Current transaction and effects | Required treatment |
| --- | --- | --- |
| `server/internal/handler/agent.go`: `ArchiveAgent` | Authorize, reject system Agent, begin, `GetAgentForUpdate`, `ArchiveAgent`, failover invalidation, commit; task cancellation and archive event follow commit | Guard and leaf membership deactivation inside the existing transaction; preserve authorization, failover invalidation, cancellation and response behavior |
| `server/internal/handler/workspace_revoke.go`: `revokeAndRemoveMember` | Shared by `DeleteMember` and `LeaveWorkspace` in `workspace.go`; subscriber advisory lock first, list owned runtimes, bulk `ArchiveAgentsByRuntime`, task cancellation/settlement, offline/token/subscription/member cleanup, failover invalidation, one commit; events after commit | Preflight the whole affected Agent set; if any root/manager blocks, roll back the whole existing atomic removal, including leaf changes; preserve system-key exclusions and all revocation effects |
| `server/pkg/db/queries/agent.sql`: `ArchiveAgentsByIDs` | Defined/generated, but no live Go caller at this baseline; comment still describes old cascade archival | Do not mistake its historical comment for a live path; require the same guard if a caller is introduced |
| `server/internal/handler/agent.go`: `RestoreAgent` / SQL `RestoreAgent` | Single Agent update; no membership writes | Keep archived memberships inactive; restoration is not hierarchy reactivation |
| `server/internal/service/runtime_teardown.go`: `TeardownRuntime` | Runtime and Agent locks, task cleanup, user Agents unbound, system Agents hard-deleted | Not a user-Agent archive path. Preserve the upstream unbind contract and Project positions; unavailable runtime does not rewrite hierarchy |
| Runtime delete/unbind handlers, profile deletion and runtime GC | Reuse native teardown; legacy `/archive-agents-and-delete` route invokes unbind behavior | Do not restore removed archive behavior based on the legacy route name; preserve runtime, Squad and history regressions |
| `server/internal/handler/daemon.go`: runtime replacement/merge | Locks runtime/dependency rows, reassigns tasks and Agents to replacement runtime, deletes old runtime, invalidates Agent policies | Not an archive path; preserve reassignment and hierarchy positions |
| `server/internal/handler/chat.go`: builder-session deletion | `DeleteSystemAgentByID` is constrained to `kind = system` and `agent_builder:%` | Excluded: hierarchy eligibility query accepts only user Agents |
| `server/internal/handler/project.go`: `DeleteProject` | Lock Project, delete views/context and `DeleteProject` in one transaction; its SQL also deletes Project memberships | No user Agent archive. Keep application-owned hierarchy cleanup atomic with containing deletion |
| `server/internal/handler/workspace.go`: `DeleteWorkspace` | Workspace delete fence, task-owner locks and ordered teardown; `DeleteWorkspaceAgents` hard-deletes Agents; final `DeleteWorkspace` SQL deletes memberships and delegation identities in the same transaction | No separate Agent archive guard needed for the containing deletion; retain atomic rollback and revalidate concurrency against any new hierarchy lock protocol |

The SQL archive inventory is three queries in `agent.sql`: `ArchiveAgent`,
`ArchiveAgentsByRuntime`, `ArchiveAgentsByIDs`. Restore is a fourth query.
Searches of production Go callers and SQL writes found no additional live
user-Agent archive writer at this baseline. Test fixture SQL is not a production
entry point. System-Agent hard deletion and whole-Workspace hard deletion are
separate cases, not implicit permission to delete user Agents elsewhere.

## Existing locks and why a handler-only check is unsafe

| Operation | Observed relevant order |
| --- | --- |
| Hierarchy create/reactivate/reparent/archive/apply | Project `FOR UPDATE`, membership snapshot, validate tree/Agents, membership writes; `validateActiveAgents` uses ordinary `GetAgentInWorkspace` reads |
| Native hierarchy delegation | Workspace `FOR NO KEY UPDATE`, Project `FOR UPDATE`, parent Issue lock, source/target Agents `FOR SHARE` in UUID order, fresh hierarchy/Agent validation |
| Single Agent archive | Agent `FOR UPDATE`, archive update, failover-state invalidation |
| Member removal | Subscriber advisory lock, runtime enumeration, bulk Agent updates, task/runtime/member cleanup, failover-state invalidation |
| Runtime teardown | Runtime lock, failover dependency Agent locks, user Agent locks, native teardown writes, final failover cleanup |
| Project / Workspace deletion | Project lock / Workspace delete fence plus native ordered task-owner locks, respectively |

A plain membership check before `ArchiveAgent` is a time-of-check race. Another
transaction can add or reactivate membership after that check. Locking only the
currently discovered Projects also misses a new membership in a different
Project. Ordinary Agent reads in `validateActiveAgents` do not wait for an
uncommitted archive and cannot close this race.

Do not simply add Project locks after the current archive Agent lock: delegation
already takes Project before Agent, so the inverse order can deadlock. Similarly,
adding Agent locks only to hierarchy validation introduces Project-to-Agent
ordering and must be reviewed with both archive paths. Sorting known Projects
alone solves neither the phantom-membership race nor this inversion.

## Bounded implementation plan and acceptance gates

1. Design one shared transaction-scoped lifecycle guard in the hierarchy domain,
   called by both live archive paths before any archive write. It must discover
   and revalidate all affected memberships, return typed structural conflicts
   with repairable Project/membership identities, and deactivate only non-root
   leaves. No automatic reparenting, ancestor skipping or restore reactivation.
2. Establish one serialization protocol across membership creation/reactivation,
   structural edits, delegation, both archive paths and containing deletion.
   Select its exact lock order before code changes. A workspace-level gate is a
   candidate, not an accepted implementation: evaluate its contention and order
   against subscriber advisory locks, workspace counter/delete fencing,
   runtime-first teardown and Agent/failover locks. Agent-to-Project discovery
   must remain protected from newly inserted memberships. Record the chosen
   protocol and its concrete wait graph in the implementation Issue/PR.
3. Preserve native batch atomicity: preflight all Agents and Projects before leaf
   writes. A manager/root in any Project rejects the whole revocation, not just
   that Agent. Report a conflict through both member-removal adapters; leave
   membership, runtime, tasks, tokens and events unchanged on rollback. Removing
   a workspace member with a structural Agent therefore requires hierarchy
   repair first, as required by ADR-006; it must not silently bypass the guard.
4. Add PostgreSQL integration regressions: ordinary Agent archive; root including
   single-node root; manager; multiple leaf Projects; mixed leaf/manager roles;
   failed archive rollback; both removal adapters and multi-Agent batch rollback;
   system-Agent exclusions; restore leaves memberships archived; explicit
   hierarchy re-entry; cross-workspace isolation. Verify no archive events or
   task cancellation escape a rejected transaction.
5. Add deterministic concurrent tests, using lock barriers rather than sleeps:
   archive versus create/reactivate in a previously unrelated Project, reparent,
   root replacement, delegation and containing deletion; overlapping multi-Agent,
   multi-Project removals in opposite request order. Assert the final invariant
   and bounded completion, not only a particular winning transaction.
6. Run native PG17 migrations/backend race CI and retain archive/restore response,
   skills, failover invalidation, member revocation, runtime unbind/profile/GC,
   Squad leader and hierarchy cleanup/delegation regression coverage. Regenerate
   sqlc for query changes. Prefer no migration; new relationships stay FK-free.

This audit completes only the prerequisite investigation from ADR-006. It does
not change accepted lifecycle policy, choose a new escalation surface, enable
fallback/recovery, or claim real installed-Agent/provider smoke coverage.
