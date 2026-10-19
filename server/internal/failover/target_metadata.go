package failover

import (
	"slices"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MetadataAssessment covers stored binding/options only, never full execution
// readiness, caller authorization or task-specific tools/context compatibility.
type MetadataAssessment struct {
	Status MetadataStatus
	Reason string
}

type MetadataStatus string

const (
	MetadataCompatible   MetadataStatus = "metadata_compatible"
	MetadataIncompatible MetadataStatus = "incompatible"
	MetadataUnknown      MetadataStatus = "unknown"
)

// AssessFallbackMetadata is pure: callers supply current, authenticated runtime
// metadata and that runtime's catalog. It never discovers CLIs or reads accounts.
// A positive assessment is necessary, not sufficient, for automatic execution.
func AssessFallbackMetadata(a db.Agent, c db.AgentFallbackConfig, r db.AgentRuntime, catalog agent.Catalog) MetadataAssessment {
	bad := func(reason string) MetadataAssessment { return MetadataAssessment{MetadataIncompatible, reason} }
	unknown := func(reason string) MetadataAssessment { return MetadataAssessment{MetadataUnknown, reason} }
	if !nonzeroUUID(a.ID) || !nonzeroUUID(a.WorkspaceID) || !nonzeroUUID(a.OwnerID) || !nonzeroUUID(c.Generation) || c.AgentID != a.ID || c.WorkspaceID != a.WorkspaceID {
		return bad("agent_binding")
	}
	if a.Kind != "user" || a.ArchivedAt.Valid {
		return bad("agent_inactive")
	}
	if !nonzeroUUID(r.ID) || c.RuntimeID != r.ID || r.WorkspaceID != a.WorkspaceID {
		return bad("runtime_binding")
	}
	switch r.Visibility {
	case "public":
	case "private":
		if !nonzeroUUID(a.OwnerID) || !nonzeroUUID(r.OwnerID) || r.OwnerID != a.OwnerID {
			return bad("runtime_owner")
		}
	default:
		return bad("runtime_visibility")
	}
	// Availability is deliberately not consulted. An offline fallback must not
	// cause a silent switch back to an exhausted primary.
	provider := r.Provider
	if _, ok := agent.BuiltinRuntimeByID(provider); !ok && !slices.Contains(agent.SupportedTypes, provider) {
		return unknown("runtime_provider")
	}
	if c.Model != "" && !agent.ModelSelectionSupported(provider) {
		return bad("model_override_unsupported")
	}
	if agent.ModelKnownIncompatibleWithProvider(provider, c.Model) {
		return bad("model_provider")
	}
	if !agent.IsKnownThinkingValue(provider, c.ThinkingLevel) {
		return bad("thinking_value")
	}
	if !agent.IsKnownServiceTier(provider, c.ServiceTier) {
		return bad("service_tier_value")
	}
	if c.ThinkingLevel != "" && c.Model == "" && agent.ThinkingRequiresExplicitModel(provider) {
		return bad("thinking_requires_model")
	}
	if c.ThinkingLevel != "" && c.Model == "" {
		if catalog.CLIThinkingLevels != nil && !slices.Contains(catalog.CLIThinkingLevels, c.ThinkingLevel) {
			return bad("thinking_capability")
		}
		// The catalog's Default badge does not prove the effective model in
		// local runtime configuration. Never approve model-specific effort by it.
		return unknown("thinking_effective_model")
	}
	if provider == "codex" && c.ServiceTier != "" && c.ServiceTier != "default" && c.Model == "" {
		return bad("service_tier_requires_model")
	}
	model := c.Model
	if model != "" {
		if !catalog.Fallback {
			for _, unavailable := range catalog.Unavailable {
				if unavailable.ID == model {
					return bad("model_unavailable")
				}
			}
		}
		if !catalog.Verified() {
			return unknown("model_catalog")
		}
		// Reuse canonical qualification, never guess a provider from a slash.
		model, _ = agent.QualifyModelID(catalog, model)
		found := false
		for _, candidate := range catalog.Models {
			if candidate.ID == model {
				found = true
				break
			}
		}
		// A runtime catalog may omit manual aliases/custom models. Absence is
		// insufficient evidence to declare them invalid, but cannot enable them.
		if !found {
			return unknown("model_unverified")
		}
	}
	load := func() (agent.Catalog, error) { return catalog, nil }
	supported, err := agent.ValidateThinkingLevelWith(load, provider, model, c.ThinkingLevel)
	if err != nil {
		return unknown("thinking_catalog")
	}
	if !supported {
		return bad("thinking_capability")
	}
	supported, err = agent.ValidateServiceTierWith(load, provider, model, c.ServiceTier)
	if err != nil {
		return unknown("service_tier_catalog")
	}
	if !supported {
		if !catalog.Verified() {
			return unknown("service_tier_catalog")
		}
		return bad("service_tier_capability")
	}
	return MetadataAssessment{Status: MetadataCompatible}
}

func nonzeroUUID(v pgtype.UUID) bool { return v.Valid && v.Bytes != [16]byte{} }
