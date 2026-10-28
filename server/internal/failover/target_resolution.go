package failover

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrTargetResolution = errors.New("execution target cannot be resolved")

// ResolutionContext contains current server-owned runtime/catalog/connection
// facts. It is not an HTTP/daemon request body or a substitute for task access
// checks, upstream owner locks, credential epoch transport or enqueue CAS.
type ResolutionContext struct {
	Runtime    db.AgentRuntime
	Catalog    agent.Catalog
	Connection RuntimeConnection
	Identity   ExecutionIdentity
	Evidence   VerifiedEvidence
	Now        time.Time
}

// ResolvedTarget preserves Agent identity and overlays only execution fields.
// Opt-in targets request fresh sessions until a target/generation-scoped session
// ledger exists. Runtime equality alone cannot separate same-runtime targets.
type ResolvedTarget struct {
	effective          db.Agent
	binding            TargetBinding
	route              string
	stateVersion       int64
	optedIn, mayRecord bool
}

func (t ResolvedTarget) Agent() db.Agent            { return cloneExecutionAgent(t.effective) }
func (t ResolvedTarget) Route() string              { return t.route }
func (t ResolvedTarget) OptedIn() bool              { return t.optedIn }
func (t ResolvedTarget) FreshSessionRequired() bool { return t.optedIn }
func (t ResolvedTarget) RouteProof(taskID pgtype.UUID) (db.RecordAgentTaskExecutionRouteParams, error) {
	if !t.optedIn || !t.mayRecord || !nonzeroUUID(taskID) {
		return db.RecordAgentTaskExecutionRouteParams{}, ErrTargetResolution
	}
	return db.RecordAgentTaskExecutionRouteParams{TaskID: taskID, AgentID: t.binding.AgentID, WorkspaceID: t.binding.WorkspaceID, Route: t.route, PrimaryGeneration: t.binding.PrimaryGeneration, FallbackGeneration: t.binding.FallbackGeneration, StateVersion: t.stateVersion}, nil
}

// ResolveEnqueueTarget is pure and currently not wired to production writers.
// The caller must insert Task plus required proof in its existing owner WithTx;
// a failed proof insert rolls back the Task. No failed Task is replayed here.
func ResolveEnqueueTarget(a db.Agent, c *db.AgentFallbackConfig, s *db.AgentFailoverState, ctx ResolutionContext) (ResolvedTarget, error) {
	if c == nil && s == nil {
		return ResolvedTarget{effective: cloneExecutionAgent(a), route: "primary"}, nil
	}
	if !validPolicySnapshot(a, c, s) {
		return ResolvedTarget{}, ErrTargetResolution
	}
	route := "fallback"
	if s.State == "primary_active" {
		route = "primary"
	}
	result, err := resolveOptedTarget(a, *c, *s, route, ctx)
	if err != nil {
		return ResolvedTarget{}, err
	}
	result.mayRecord = true
	return result, nil
}

// ResolveQueuedTarget follows persisted proof, not current availability. Legacy
// unmarked work remains primary, never gains opt-in quota authority and cannot
// silently consume fallback secrets. Existing task/workspace authorization and
// claim ownership checks are still mandatory at the integration call site.
func ResolveQueuedTarget(a db.Agent, c *db.AgentFallbackConfig, s *db.AgentFailoverState, task db.AgentTaskQueue, proof *db.AgentTaskExecutionRoute, ctx ResolutionContext) (ResolvedTarget, error) {
	if !nonzeroUUID(task.ID) || task.AgentID != a.ID || !nonzeroUUID(task.RuntimeID) || ctx.Runtime.ID != task.RuntimeID || ctx.Runtime.WorkspaceID != a.WorkspaceID {
		return ResolvedTarget{}, ErrTargetResolution
	}
	if proof == nil {
		if task.RuntimeID != a.RuntimeID {
			return ResolvedTarget{}, ErrTargetResolution
		}
		return ResolvedTarget{effective: cloneExecutionAgent(a), route: "primary"}, nil
	}
	if !validPolicySnapshot(a, c, s) || proof.TaskID != task.ID || proof.AgentID != a.ID || proof.WorkspaceID != a.WorkspaceID || proof.RuntimeID != task.RuntimeID || proof.PrimaryGeneration != s.PrimaryGeneration || proof.FallbackGeneration != c.Generation || proof.StateVersion <= 0 || proof.StateVersion > s.Version {
		return ResolvedTarget{}, ErrTargetResolution
	}
	result, err := resolveOptedTarget(a, *c, *s, proof.Route, ctx)
	if err != nil {
		return ResolvedTarget{}, err
	}
	if result.effective.RuntimeID != task.RuntimeID {
		return ResolvedTarget{}, ErrTargetResolution
	}
	// Preserve the original enqueue version; availability transitions alone do
	// not rewrite its epochs/runtime. This result must never insert new proof.
	result.stateVersion = proof.StateVersion
	return result, nil
}

// DescribeTargetBinding exposes no env/args/MCP/context. A primary binding may
// also be used for a separate tool-free probe capability challenge; it is never
// an inference result or an instruction to execute the Agent-shaped payload.
func DescribeTargetBinding(a db.Agent, c db.AgentFallbackConfig, s db.AgentFailoverState, route string, r db.AgentRuntime, catalog agent.Catalog) (TargetBinding, error) {
	// Credential evidence must never authorize the separately opted-in path
	// policy. Native path execution still needs its reviewed end-to-end gate.
	if c.NativeLoginPath {
		return TargetBinding{}, ErrExecutionEvidence
	}
	if !validPolicySnapshot(a, &c, &s) {
		return TargetBinding{}, ErrTargetResolution
	}
	selected, err := effectiveTarget(a, c, route)
	if err != nil {
		return TargetBinding{}, err
	}
	if route == "fallback" {
		selected.RuntimeMode = r.RuntimeMode
	}
	metadata := db.AgentFallbackConfig{AgentID: a.ID, WorkspaceID: a.WorkspaceID, RuntimeID: selected.RuntimeID, Generation: c.Generation, Model: selected.Model.String, ThinkingLevel: selected.ThinkingLevel.String, ServiceTier: selected.ServiceTier.String}
	if AssessFallbackMetadata(a, metadata, r, catalog).Status != MetadataCompatible {
		return TargetBinding{}, ErrTargetResolution
	}
	model := metadata.Model
	if model != "" {
		model, _ = agent.QualifyModelID(catalog, model)
	}
	return TargetBinding{AgentID: a.ID, WorkspaceID: a.WorkspaceID, AgentOwnerID: a.OwnerID, RuntimeID: r.ID, RuntimeProfileID: r.ProfileID, PrimaryGeneration: s.PrimaryGeneration, FallbackGeneration: c.Generation, Route: route, RuntimeMode: selected.RuntimeMode, Model: model, ThinkingLevel: metadata.ThinkingLevel, ServiceTier: metadata.ServiceTier}, nil
}
func resolveOptedTarget(a db.Agent, c db.AgentFallbackConfig, s db.AgentFailoverState, route string, ctx ResolutionContext) (ResolvedTarget, error) {
	binding, err := DescribeTargetBinding(a, c, s, route, ctx.Runtime, ctx.Catalog)
	if err != nil {
		return ResolvedTarget{}, err
	}
	if ctx.Connection.RuntimeID != ctx.Runtime.ID || ctx.Connection.WorkspaceID != ctx.Runtime.WorkspaceID || ctx.Connection.OwnerID != ctx.Runtime.OwnerID || ctx.Connection.Provider != ctx.Runtime.Provider || !ctx.Evidence.matches(binding, ctx.Connection, ctx.Identity, TaskEvidence, ctx.Now) {
		return ResolvedTarget{}, ErrExecutionEvidence
	}
	effective, err := effectiveTarget(a, c, route)
	if err != nil {
		return ResolvedTarget{}, err
	}
	if route == "fallback" {
		effective.RuntimeMode = ctx.Runtime.RuntimeMode
	}
	return ResolvedTarget{effective: effective, binding: binding, route: route, stateVersion: s.Version, optedIn: true}, nil
}
func validPolicySnapshot(a db.Agent, c *db.AgentFallbackConfig, s *db.AgentFailoverState) bool {
	if c == nil || s == nil || !nonzeroUUID(a.ID) || !nonzeroUUID(a.WorkspaceID) || !nonzeroUUID(a.OwnerID) || a.Kind != "user" || a.ArchivedAt.Valid || c.AgentID != a.ID || c.WorkspaceID != a.WorkspaceID || s.AgentID != a.ID || s.WorkspaceID != a.WorkspaceID || !nonzeroUUID(c.Generation) || !nonzeroUUID(s.PrimaryGeneration) || s.Version <= 0 || s.SuspendedAt.Valid {
		return false
	}
	return s.State == "primary_active" || s.State == "fallback_active" || s.State == "primary_probing"
}
func effectiveTarget(a db.Agent, c db.AgentFallbackConfig, route string) (db.Agent, error) {
	out := cloneExecutionAgent(a)
	switch route {
	case "primary":
		return out, nil
	case "fallback":
		o, err := decodeExecutionOverrides(c.ExecutionConfig)
		if err != nil {
			return db.Agent{}, err
		}
		out.RuntimeID = c.RuntimeID
		out.RuntimeConfig = bytes.Clone(o.RuntimeConfig)
		out.McpConfig = bytes.Clone(o.MCPConfig)
		out.CustomEnv, err = json.Marshal(o.CustomEnv)
		if err != nil {
			return db.Agent{}, ErrTargetDocument
		}
		out.CustomArgs, err = json.Marshal(o.CustomArgs)
		if err != nil {
			return db.Agent{}, ErrTargetDocument
		}
		out.Model = targetText(c.Model)
		out.ThinkingLevel = targetText(c.ThinkingLevel)
		out.ServiceTier = targetText(c.ServiceTier)
		return out, nil
	}
	return db.Agent{}, ErrTargetResolution
}
func targetText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
func cloneExecutionAgent(a db.Agent) db.Agent {
	a.RuntimeConfig = bytes.Clone(a.RuntimeConfig)
	a.CustomEnv = bytes.Clone(a.CustomEnv)
	a.CustomArgs = bytes.Clone(a.CustomArgs)
	a.McpConfig = bytes.Clone(a.McpConfig)
	a.DisabledRuntimeSkills = bytes.Clone(a.DisabledRuntimeSkills)
	a.ConversationStarters = bytes.Clone(a.ConversationStarters)
	a.ComposioToolkitAllowlist = slices.Clone(a.ComposioToolkitAllowlist)
	return a
}
