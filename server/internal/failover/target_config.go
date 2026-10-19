package failover

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/agentsecrets"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

var (
	ErrTargetAccess       = errors.New("fallback configuration access denied")
	ErrTargetConflict     = errors.New("fallback configuration revision changed")
	ErrTargetUnavailable  = errors.New("fallback configuration unavailable")
	ErrTargetStore        = errors.New("fallback configuration transaction failed")
	ErrTargetIncompatible = errors.New("fallback target metadata incompatible")
)

// ManagementActor must come from authenticated server actor resolution, never
// from a request body. A backing member does not grant an Agent actor access.
type ManagementActor struct {
	Kind                string
	UserID, WorkspaceID pgtype.UUID
}
type ConfigRevision struct {
	Generation, PrimaryGeneration pgtype.UUID
	StateVersion                  int64
}
type SaveTargetConfig struct {
	AgentID, RuntimeID                pgtype.UUID
	Model, ThinkingLevel, ServiceTier string
	Expected                          ConfigRevision
	Overrides                         ExecutionOverrides
}

// ConfigView deliberately cannot serialize raw DB configuration or secrets.
type ConfigView struct {
	AgentID, RuntimeID                pgtype.UUID
	Model, ThinkingLevel, ServiceTier string
	Revision                          ConfigRevision
	OverridesStored                   bool
	EnvironmentKeys                   []string
	ArgumentCount                     int
	HasMCPConfig, HasRuntimeConfig    bool
}

type configTxStarter interface {
	Begin(context.Context) (pgx.Tx, error)
}

// ConfigManager is an internal management boundary only. It has no endpoint,
// executor, transport, automatic enablement or capability approval. The caller
// supplies the authenticated target runtime's catalog; unknown metadata may be
// stored for later validation, never treated as executable readiness.
type ConfigManager struct {
	Queries      *db.Queries
	Transactions configTxStarter
}

func (m ConfigManager) begin(ctx context.Context, actor ManagementActor, id pgtype.UUID, target pgtype.UUID) (pgx.Tx, *db.Queries, db.Agent, error) {
	if actor.Kind != "member" || !nonzeroUUID(actor.UserID) || !nonzeroUUID(actor.WorkspaceID) || !nonzeroUUID(id) {
		return nil, nil, db.Agent{}, ErrTargetAccess
	}
	tx, err := m.Transactions.Begin(ctx)
	if err != nil {
		return nil, nil, db.Agent{}, ErrTargetStore
	}
	q := m.Queries.WithTx(tx)
	fail := func(e error) (pgx.Tx, *db.Queries, db.Agent, error) {
		_ = tx.Rollback(ctx)
		return nil, nil, db.Agent{}, e
	}
	if _, err = q.LockWorkspaceForChatSessionCreate(ctx, actor.WorkspaceID); err != nil {
		return fail(ErrTargetAccess)
	}
	before, err := q.GetAgent(ctx, id)
	if err != nil || before.WorkspaceID != actor.WorkspaceID {
		return fail(ErrTargetAccess)
	}
	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: actor.UserID, WorkspaceID: actor.WorkspaceID})
	if err != nil || !managementMemberAllowed(before, member) {
		return fail(ErrTargetAccess)
	}
	current, err := q.GetAgentForUpdate(ctx, id)
	if err != nil || current.WorkspaceID != actor.WorkspaceID {
		return fail(ErrTargetAccess)
	}
	if target.Valid && current.RuntimeID != before.RuntimeID {
		return fail(ErrTargetConflict)
	}
	member, err = q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: actor.UserID, WorkspaceID: actor.WorkspaceID})
	if err != nil || !managementMemberAllowed(current, member) {
		return fail(ErrTargetAccess)
	}
	if current.Kind != "user" {
		return fail(ErrTargetAccess)
	}
	if target.Valid {
		if !nonzeroUUID(target) || !nonzeroUUID(current.RuntimeID) {
			return fail(ErrTargetIncompatible)
		}
		for _, id := range []pgtype.UUID{current.RuntimeID, target} {
			runtime, err := q.GetAgentRuntime(ctx, id)
			if err != nil || !managementRuntimeAllowed(current, runtime) {
				return fail(ErrTargetIncompatible)
			}
		}
		ids := []pgtype.UUID{current.RuntimeID}
		if target != current.RuntimeID {
			ids = append(ids, target)
		}
		rows, err := q.TryLockExecutionTargetRuntimesForShare(ctx, ids)
		if err != nil {
			var databaseError *pgconn.PgError
			if errors.As(err, &databaseError) && databaseError.Code == "55P03" {
				return fail(ErrTargetConflict)
			}
			return fail(ErrTargetStore)
		}
		if len(rows) != len(ids) {
			return fail(ErrTargetIncompatible)
		}
	}
	return tx, q, current, nil
}

func loadTargetConfig(ctx context.Context, q *db.Queries, a db.Agent) (db.AgentFallbackConfig, db.AgentFailoverState, bool, bool, error) {
	c, err := q.GetAgentFallbackConfig(ctx, db.GetAgentFallbackConfigParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID})
	hasConfig := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c, db.AgentFailoverState{}, false, false, ErrTargetStore
	}
	s, err := q.GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID})
	hasState := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return c, s, false, false, ErrTargetStore
	}
	return c, s, hasConfig, hasState, nil
}
func revision(c db.AgentFallbackConfig, s db.AgentFailoverState, hasConfig, hasState bool) ConfigRevision {
	var r ConfigRevision
	if hasConfig {
		r.Generation = c.Generation
	}
	if hasState {
		r.PrimaryGeneration = s.PrimaryGeneration
		r.StateVersion = s.Version
	}
	return r
}
func configView(c db.AgentFallbackConfig, s db.AgentFailoverState) (ConfigView, error) {
	v := ConfigView{AgentID: c.AgentID, RuntimeID: c.RuntimeID, Model: c.Model, ThinkingLevel: c.ThinkingLevel, ServiceTier: c.ServiceTier, Revision: revision(c, s, true, true), EnvironmentKeys: []string{}}
	if len(c.ExecutionConfig) == 0 {
		return v, nil
	}
	o, err := decodeExecutionOverrides(c.ExecutionConfig)
	if err != nil {
		return ConfigView{}, err
	}
	v.OverridesStored = true
	for key := range o.CustomEnv {
		v.EnvironmentKeys = append(v.EnvironmentKeys, key)
	}
	slices.Sort(v.EnvironmentKeys)
	v.ArgumentCount = len(o.CustomArgs)
	v.HasMCPConfig = len(o.MCPConfig) > 0 && string(o.MCPConfig) != "null" && string(o.MCPConfig) != "{}"
	v.HasRuntimeConfig = string(o.RuntimeConfig) != "{}" && string(o.RuntimeConfig) != "null"
	return v, nil
}
func targetAudit(ctx context.Context, q *db.Queries, a db.Agent, actor ManagementActor, action string, details map[string]any) error {
	details["agent_id"] = uuid.UUID(a.ID.Bytes).String()
	raw, err := json.Marshal(details)
	if err != nil {
		return ErrTargetStore
	}
	_, err = q.CreateActivity(ctx, db.CreateActivityParams{ID: dbid.NewV7(), WorkspaceID: a.WorkspaceID, ActorType: pgtype.Text{String: "member", Valid: true}, ActorID: actor.UserID, Action: action, Details: raw})
	// DB errors can contain rejected secret-bearing row values. Never return or
	// log raw persistence errors across this boundary, including reveal failures.
	if err != nil {
		return ErrTargetStore
	}
	return nil
}
func freshConfigGeneration() pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte(uuid.New()), Valid: true}
}

func (m ConfigManager) Save(ctx context.Context, actor ManagementActor, in SaveTargetConfig, catalog agent.Catalog) (ConfigView, error) {
	if !nonzeroUUID(in.RuntimeID) {
		return ConfigView{}, ErrTargetIncompatible
	}
	tx, q, a, err := m.begin(ctx, actor, in.AgentID, in.RuntimeID)
	if err != nil {
		return ConfigView{}, err
	}
	defer tx.Rollback(ctx)
	if a.ArchivedAt.Valid {
		return ConfigView{}, ErrTargetIncompatible
	}
	primary, err := q.GetAgentRuntime(ctx, a.RuntimeID)
	if err != nil || !managementRuntimeAllowed(a, primary) {
		return ConfigView{}, ErrTargetIncompatible
	}
	runtime, err := q.GetAgentRuntime(ctx, in.RuntimeID)
	if err != nil {
		return ConfigView{}, ErrTargetIncompatible
	}
	c, s, hasConfig, hasState, err := loadTargetConfig(ctx, q, a)
	if err != nil {
		return ConfigView{}, err
	}
	if revision(c, s, hasConfig, hasState) != in.Expected || (hasConfig && !hasState) {
		return ConfigView{}, ErrTargetConflict
	}
	candidate := db.AgentFallbackConfig{AgentID: a.ID, WorkspaceID: a.WorkspaceID, RuntimeID: in.RuntimeID, Generation: freshConfigGeneration(), Model: in.Model, ThinkingLevel: in.ThinkingLevel, ServiceTier: in.ServiceTier}
	if assessment := AssessFallbackMetadata(a, candidate, runtime, catalog); assessment.Status == MetadataIncompatible {
		return ConfigView{}, ErrTargetIncompatible
	}
	o, err := normalizeExecutionOverrides(in.Overrides)
	if err != nil {
		return ConfigView{}, err
	}
	oldEnv := map[string]string{}
	if hasConfig && len(c.ExecutionConfig) > 0 {
		previous, err := decodeExecutionOverrides(c.ExecutionConfig)
		if err != nil {
			return ConfigView{}, err
		}
		oldEnv = previous.CustomEnv
		// Record removals against the old map, but never let its masks carry
		// credentials across runtime identity. The request map is already cloned.
		if c.RuntimeID != in.RuntimeID {
			for key, value := range o.CustomEnv {
				if value == agentsecrets.Sentinel {
					delete(o.CustomEnv, key)
				}
			}
		}
	}
	merged, diff := agentsecrets.MergeEnvironment(oldEnv, o.CustomEnv)
	o.CustomEnv = merged
	encoded, err := json.Marshal(o)
	if err != nil || len(encoded) > 1<<20 {
		return ConfigView{}, ErrTargetDocument
	}
	if err = targetAudit(ctx, q, a, actor, "agent_fallback_updated", map[string]any{"added_keys": diff.Added, "removed_keys": diff.Removed, "changed_keys": diff.Changed, "preserved_keys": diff.Preserved, "fields": []string{"runtime_id", "model", "thinking_level", "service_tier", "execution_config"}}); err != nil {
		return ConfigView{}, err
	}
	// All owner/audit writes finish before config -> state leaf locks.
	if !hasConfig {
		c, err = q.InitializeAgentFallbackConfig(ctx, db.InitializeAgentFallbackConfigParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID, RuntimeID: in.RuntimeID, Generation: candidate.Generation, Model: in.Model, ThinkingLevel: in.ThinkingLevel, ServiceTier: in.ServiceTier, ExecutionConfig: encoded})
		if errors.Is(err, pgx.ErrNoRows) {
			return ConfigView{}, ErrTargetConflict
		}
		if err != nil {
			return ConfigView{}, ErrTargetStore
		}
	}
	if !hasState {
		s, err = q.InitializeAgentFailoverState(ctx, db.InitializeAgentFailoverStateParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID, PrimaryGeneration: freshConfigGeneration()})
	} else {
		c, err = q.UpdateAgentFallbackConfig(ctx, db.UpdateAgentFallbackConfigParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID, RuntimeID: in.RuntimeID, ExpectedGeneration: c.Generation, NewGeneration: freshConfigGeneration(), ExpectedPrimaryGeneration: s.PrimaryGeneration, ExpectedVersion: s.Version, NewPrimaryGeneration: freshConfigGeneration(), Model: in.Model, ThinkingLevel: in.ThinkingLevel, ServiceTier: in.ServiceTier, ExecutionConfig: encoded})
		if err == nil {
			s, err = q.GetAgentFailoverState(ctx, db.GetAgentFailoverStateParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID})
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ConfigView{}, ErrTargetConflict
	}
	if err != nil {
		return ConfigView{}, ErrTargetStore
	}
	view, err := configView(c, s)
	if err != nil {
		return ConfigView{}, err
	}
	if tx.Commit(ctx) != nil {
		return ConfigView{}, ErrTargetStore
	}
	return view, nil
}

func (m ConfigManager) Inspect(ctx context.Context, actor ManagementActor, id pgtype.UUID) (ConfigView, error) {
	tx, q, a, err := m.begin(ctx, actor, id, pgtype.UUID{})
	if err != nil {
		return ConfigView{}, err
	}
	defer tx.Rollback(ctx)
	c, s, hc, hs, err := loadTargetConfig(ctx, q, a)
	if err != nil {
		return ConfigView{}, err
	}
	if !hc || !hs {
		return ConfigView{}, ErrTargetUnavailable
	}
	v, err := configView(c, s)
	if err != nil {
		return ConfigView{}, err
	}
	if tx.Commit(ctx) != nil {
		return ConfigView{}, ErrTargetStore
	}
	return v, nil
}

func (m ConfigManager) Reveal(ctx context.Context, actor ManagementActor, id pgtype.UUID) (ExecutionOverrides, error) {
	tx, q, a, err := m.begin(ctx, actor, id, pgtype.UUID{})
	if err != nil {
		return ExecutionOverrides{}, err
	}
	defer tx.Rollback(ctx)
	c, s, hc, hs, err := loadTargetConfig(ctx, q, a)
	if err != nil {
		return ExecutionOverrides{}, err
	}
	if !hc || !hs || len(c.ExecutionConfig) == 0 {
		return ExecutionOverrides{}, ErrTargetUnavailable
	}
	o, err := decodeExecutionOverrides(c.ExecutionConfig)
	if err != nil {
		return ExecutionOverrides{}, err
	}
	v, err := configView(c, s)
	if err != nil {
		return ExecutionOverrides{}, err
	}
	if err = targetAudit(ctx, q, a, actor, "agent_fallback_revealed", map[string]any{"revealed_keys": v.EnvironmentKeys, "fields": []string{"custom_env", "custom_args", "runtime_config", "mcp_config"}}); err != nil {
		return ExecutionOverrides{}, err
	}
	if tx.Commit(ctx) != nil {
		return ExecutionOverrides{}, ErrTargetStore
	}
	return o, nil
}

func (m ConfigManager) Remove(ctx context.Context, actor ManagementActor, id pgtype.UUID, expected ConfigRevision) error {
	tx, q, a, err := m.begin(ctx, actor, id, pgtype.UUID{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	c, s, hc, hs, err := loadTargetConfig(ctx, q, a)
	if err != nil {
		return err
	}
	if !hc || !hs || revision(c, s, hc, hs) != expected {
		return ErrTargetConflict
	}
	if err = targetAudit(ctx, q, a, actor, "agent_fallback_removed", map[string]any{}); err != nil {
		return err
	}
	n, err := q.DeleteAgentFallbackConfig(ctx, db.DeleteAgentFallbackConfigParams{AgentID: a.ID, WorkspaceID: a.WorkspaceID, ExpectedGeneration: c.Generation, ExpectedPrimaryGeneration: s.PrimaryGeneration, ExpectedVersion: s.Version, NewPrimaryGeneration: freshConfigGeneration()})
	if err != nil {
		return ErrTargetStore
	}
	if n != 1 {
		return ErrTargetConflict
	}
	if tx.Commit(ctx) != nil {
		return ErrTargetStore
	}
	return nil
}

func managementMemberAllowed(a db.Agent, m db.Member) bool {
	return (m.Role == "owner" || m.Role == "admin" || m.Role == "member") && agentsecrets.CanManage(a, m)
}
func managementRuntimeAllowed(a db.Agent, r db.AgentRuntime) bool {
	return r.WorkspaceID == a.WorkspaceID && (r.Visibility == "public" || (r.Visibility == "private" && nonzeroUUID(a.OwnerID) && a.OwnerID == r.OwnerID))
}
