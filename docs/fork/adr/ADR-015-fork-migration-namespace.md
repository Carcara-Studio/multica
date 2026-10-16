# ADR-015 — Reserved migration namespace for fork-owned schema changes

- Status: Accepted
- Date: 2026-10-16
- Decision owners: Human maintainer
- Related issues: #29
- Supersedes: None
- Superseded by: None

## Context

The fork originally introduced Project hierarchy migrations using prefixes 573 through 579. During MFK-010, official upstream had independently advanced into the same numeric range with unrelated migrations, including issue workflow and Project workflow changes.

Confirmed upstream behavior:

- migrations are stored in `server/migrations/` and named with a numeric prefix;
- upstream contributor guidance requires the next unused numeric prefix for new migrations;
- the migration runner records the complete migration basename in `schema_migrations.version`, not only the numeric prefix;
- the runner sorts migration filenames lexicographically;
- upstream has previously renumbered unpublished or branch-local migration series and documented ledger convergence for installations that had already applied the previous filenames.

Although two migrations with the same numeric prefix but different complete basenames are technically distinct to the runner, allowing the fork to consume upstream's normal sequential namespace would create recurring ambiguity and merge churn on nearly every upstream synchronization.

## Decision Drivers

- preserve normal upstream ancestry and make recurring merges predictable;
- keep fork migrations visually and operationally distinct from upstream-owned migrations;
- avoid repeated renumbering every time upstream advances through the fork's chosen numbers;
- support existing fork installations that already applied the original 573-579 hierarchy migrations;
- avoid a separate migration engine or directory that would diverge from upstream tooling;
- keep migration ordering explicit and testable.

## Considered Alternatives

### A. Keep the duplicate 573-579 prefixes

The migration runner would distinguish the files by complete basename, so this can execute. However, it violates the repository's numeric-prefix convention, creates misleading migration ordering, and makes future upstream comparisons and reviews harder.

Rejected.

### B. Renumber to the next currently-unused upstream sequence

For MFK-010 that would place the fork close to the current upstream tail. Upstream would likely consume those numbers shortly afterward, forcing another renumber and another compatibility bridge.

Rejected because it optimizes one sync at the cost of repeated future maintenance.

### C. Use a reserved high-number fork band

Place fork-owned migrations in a clearly separated high-number range beginning at `900000`. Continue using the existing upstream migration runner, hooks, conditions, rollback mechanics, and `schema_migrations` ledger.

Accepted.

### D. Maintain a separate fork migration directory or migration engine

This would avoid filename collisions but would fork a central operational subsystem and require custom startup, rollback, deployment, and test behavior.

Rejected because its upstream compatibility cost is much higher than an isolated filename namespace.

## Decision

Fork-owned schema migrations use a reserved high-number band beginning at `900000`.

The Project hierarchy sequence introduced by HIER-001 is:

```text
900000_project_agent_membership
900001_project_agent_membership_id_idx
900002_project_agent_membership_project_agent_idx
900003_project_agent_membership_active_root_idx
900004_project_agent_membership_manager_idx
900005_project_agent_membership_workspace_idx
900006_project_agent_membership_primary_key
```

New fork-owned migrations must allocate monotonically within the reserved fork band and must not take numbers from upstream's normal sequential range.

Fork migrations must not depend on being numerically adjacent to a particular upstream migration. Dependencies must be expressed through the actual schema or application contract they require.

When a published fork migration must be renamed because of a namespace correction:

1. preserve the existing schema semantics;
2. make the new migration sequence safe against installations that already applied the legacy filenames;
3. prefer idempotent DDL and read-only migration conditions over destructive ledger rewriting;
4. preserve historical `schema_migrations` rows unless removing or rewriting them is strictly required;
5. add an upgrade regression test covering the legacy ledger state;
6. verify both fresh install and legacy-upgrade paths.

For the 573-579 to 900000-900006 transition, installations that already have the hierarchy primary key skip the renamed unique-id-index and primary-key steps through read-only migration conditions. The other hierarchy steps are idempotent and converge the new ledger entries without rebuilding the schema.

If official upstream ever approaches the reserved fork band, a new ADR amendment must reserve another non-conflicting band before adding further migrations. Do not silently start interleaving with upstream again.

## Consequences

### Positive

- upstream can continue allocating its normal migration sequence without routine conflicts with the fork;
- future sync diffs make fork-owned schema changes immediately recognizable;
- no custom migration engine is introduced;
- existing migration hooks, conditions, advisory locking, rollback code, and CI remain authoritative;
- legacy fork installations have an explicit and tested convergence path.

### Negative / Trade-offs

- fork migration numbers no longer communicate chronological adjacency with upstream migrations;
- the reserved range is a fork convention that must be maintained in governance documentation;
- legacy installations temporarily retain both old and new migration ledger entries after convergence;
- every upstream sync must still verify that upstream has not entered the reserved range.

## Data Impact

The migration filename namespace changes; the Project hierarchy schema itself does not.

Existing databases that applied the original 573-579 fork migrations retain those historical ledger rows. Running the 900000-900006 sequence records the new names as well. Idempotent DDL and guarded PK/index steps prevent duplicate schema objects.

Migration execution remains serialized by the existing migrator advisory lock. No new database foreign keys, cascade behavior, or runtime locks are introduced.

## Backend / Runtime Impact

`server/cmd/migrate/project_hierarchy_indexes.go` owns the fork-specific concurrent-index registrations and compatibility conditions for the renamed hierarchy sequence.

There is no daemon or task-runtime behavior change.

## API / CLI / UI Impact

None. The hierarchy API and UI contracts are unchanged by migration renumbering.

## Failure and Edge Cases

- Fresh database: all 900000-900006 migrations apply normally.
- Database with legacy 573-579 hierarchy ledger and schema: idempotent steps converge, while the renamed ID-index and PK steps detect the existing expected PK and become recorded no-ops.
- Database with an unexpected primary-key shape: migration fails closed rather than silently accepting incompatible schema.
- Interrupted concurrent index creation: existing fork registrations continue to use upstream's invalid-index cleanup hook.
- Upstream reaches the reserved range: stop allocating fork migrations in that range and amend this ADR before proceeding.

## Testing Requirements

- isolated up/down/up hierarchy migration test using current reserved names;
- legacy-numbering upgrade test preserving old ledger entries and converging current names;
- assertion that exactly one hierarchy primary key remains;
- assertion that the legacy convergence path does not leave a redundant `project_agent_membership_id_idx`;
- generated SQLC consistency check;
- normal backend migration and regression CI.

## Upstream Compatibility

This decision deliberately uses upstream's migration runner and conventions while isolating only the fork's filename allocation policy. It avoids modifications to the central migration discovery/ledger algorithm.

The compatibility hotspot is limited to fork-owned registration code in `server/cmd/migrate/project_hierarchy_indexes.go` plus the fork migration files themselves.

During every upstream synchronization, compare `server/migrations/` against the reserved band before merging.

## Open Questions

None for the current hierarchy migration series.
