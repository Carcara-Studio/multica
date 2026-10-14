# ADR-002 — Squad remains workspace-scoped and separate from Project Hierarchy

- Status: Proposed
- Date: 2026-10-14
- Decision owners: Human maintainer
- Related issues: #5
- Supersedes: None
- Superseded by: None

## Context

### Confirmed upstream behavior

Current Multica models Squad as a workspace-scoped routing and coordination object.

`server/migrations/084_squad.up.sql` defines:

- `squad.workspace_id`;
- required `squad.leader_id` referencing Agent;
- `squad_member` rows containing `member_type`, `member_id` and `role`;
- Squad as a supported Issue assignee type.

The current reference documentation at `server/internal/service/builtin_skills/multica-platform/references/squads.md` explicitly states:

- Squad is not an Agent and does not run work by itself;
- assigning an Issue to a Squad routes to `squad.leader_id`;
- mentioning a Squad routes to the leader;
- Squad autopilot resolves to the leader;
- members are not automatically fanned out;
- `squad_member.role` is a roster label, not scheduling, permissions or routing authority.

The hierarchy requirement, by contrast, is Project-scoped. The same Agents can have different relationships in different Projects.

## Decision Drivers

- Preserve current Squad routing semantics.
- Avoid changing Squad scope from Workspace to Project.
- Allow one Squad and its Agents to participate in multiple Projects.
- Keep organizational hierarchy separate from collaboration/routing grouping.
- Avoid turning `squad_member.role` into hidden operational semantics.
- Minimize changes to an active upstream subsystem.

## Considered Alternatives

### Alternative A — Add `project_id` to Squad and make each Squad Project-scoped

**Pros**

- Project and Squad organization become easier to reason about locally.

**Cons**

- Changes the existing meaning and reuse of Squad.
- A Squad could no longer naturally coordinate work across multiple Projects without duplication.
- Existing API/UI/runtime assumptions would require migration or compatibility logic.
- High upstream conflict risk.

**Decision:** rejected.

### Alternative B — Encode the hierarchy in `squad_member.role`

**Pros**

- No new table.

**Cons**

- `role` is free-form roster context today.
- A role label cannot reliably encode one manager, arbitrary depth, cycle prevention or Project scope.
- Would create implicit behavior from a field that upstream explicitly documents as non-routing metadata.

**Decision:** rejected.

### Alternative C — Treat Squad leader/member relationships as the Project hierarchy

**Pros**

- Reuses `leader_id` and membership.

**Cons**

- Only models one leader layer.
- Squad is Workspace-scoped.
- Members may be human Members as well as Agents.
- Current Squad runtime semantics route all Squad work through the leader rather than through a reporting tree.

**Decision:** rejected.

### Alternative D — Keep Squad and Project Hierarchy independent, then compose them at runtime

**Pros**

- Preserves upstream contracts.
- Correctly models both domains.
- Allows the same Squad to be used in multiple Projects.
- Keeps Project-specific organization optional.

**Cons**

- Runtime/briefing logic must understand both concepts where they intersect.
- UI must communicate that Squad role and Project position are different concepts.

**Decision:** accepted design, pending ADR approval.

## Decision

Squad remains a workspace-scoped routing/coordination object exactly as upstream defines it.

Project Hierarchy is a separate, Project-scoped organizational domain defined by ADR-001.

No hierarchy manager relation is inferred from:

- `squad.leader_id`;
- Squad membership;
- `squad_member.role`;
- Squad instructions.

No Squad schema change is required merely to support Project Hierarchy.

### Composition rule for Project Issues assigned to a Squad

For an Issue with both:

```text
project_id = P
assignee_type = squad
assignee_id = S
```

execution continues to resolve through upstream Squad behavior:

```text
Issue
  -> Squad S
  -> S.leader_id
  -> executing Agent
```

Only after the executing Agent is resolved may the fork resolve that Agent's active membership in Project `P` to provide Project-hierarchy context.

The fork must **not** replace `squad.leader_id` with the Project hierarchy root.

If the Squad leader has no active membership in Project `P`, normal Squad execution still follows upstream behavior. Hierarchy-only operations such as validated hierarchical delegation/escalation are unavailable for that execution until a valid Project membership exists.

## Consequences

### Positive

- Very low change surface in upstream Squad code.
- Existing Squad assignment, mention and autopilot behavior remains stable.
- A Squad can participate in multiple Projects without duplication.
- An Agent's Project manager/title can vary independently of Squad membership.
- Existing `role` remains understandable and backward compatible.

### Negative / Trade-offs

- There are two legitimate organizational-looking concepts in the product.
- UI and prompts must clearly distinguish Squad roster role from Project hierarchy title/manager.
- An Agent may be a Squad leader while reporting to another Agent in a particular Project; this is valid but must be explained in context.

## Data Impact

No existing Squad table or SquadMember table is changed by this decision.

Project hierarchy data remains in the additive structure defined by ADR-001.

No Squad backfill is required.

## Backend / Runtime Impact

Existing Squad resolution remains authoritative for Squad-assigned work.

Potential integration is limited to additive Project-hierarchy context after executable Agent resolution.

If a leader briefing is later extended with Project-hierarchy context, the extension should be small and isolated; the existing Squad Operating Protocol/Roster/Instructions contract remains intact.

## API / CLI / UI Impact

Squad endpoints/commands retain current meaning.

Project hierarchy receives separate API/CLI/UI surfaces.

UI should avoid presenting `squad_member.role` as the Agent's Project manager/title.

A Project organization tree should be driven only by Project membership data.

## Failure and Edge Cases

- Same Squad used for Project A and B: valid; hierarchy is resolved independently for each Project.
- Squad leader is root in Project A and worker in Project B: valid.
- Squad member is not in the current Project hierarchy: valid; Squad membership does not imply Project membership.
- Project hierarchy root is not Squad leader: valid; no automatic rerouting.
- Squad leader archived: preserve upstream fail-closed behavior; hierarchy must not bypass the archived leader by switching to root.
- Squad leader missing Project membership: normal Squad routing continues, hierarchy-only actions unavailable.
- Issue has no Project: only upstream Squad behavior applies.

## Testing Requirements

Regression tests must cover:

- existing Squad assignment still routes to leader;
- Squad mention still routes to leader;
- Squad autopilot still resolves to leader;
- member fan-out is not introduced;
- `squad_member.role` does not affect hierarchy routing;
- same Squad can execute Issues in multiple Projects;
- Project context resolves the leader's membership only after Squad routing;
- missing Project membership does not silently reroute to Project root.

## Upstream Compatibility

This decision deliberately avoids modifying Squad scope or reinterpreting existing fields.

It should require, at most, a small integration point where Project hierarchy context is added after current Squad leader resolution. New logic belongs in isolated fork modules rather than inside the Squad domain wherever possible.

If upstream later introduces Project-scoped Squad semantics, re-evaluate this ADR instead of preserving separation mechanically.

## Open Questions

- Exact briefing presentation for an Agent that is simultaneously a Squad leader and a subordinate in the current Project should be designed during HIER-006, without changing the routing decision above.
