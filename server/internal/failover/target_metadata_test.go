package failover

import (
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func metadataFixture() (db.Agent, db.AgentFallbackConfig, db.AgentRuntime, agent.Catalog) {
	a := db.Agent{ID: newGeneration(), WorkspaceID: newGeneration(), OwnerID: newGeneration(), Kind: "user"}
	r := db.AgentRuntime{ID: newGeneration(), WorkspaceID: a.WorkspaceID, OwnerID: a.OwnerID, Visibility: "private", Provider: "codex", Status: "online"}
	c := db.AgentFallbackConfig{AgentID: a.ID, WorkspaceID: a.WorkspaceID, RuntimeID: r.ID, Generation: newGeneration()}
	return a, c, r, agent.Catalog{}
}

func TestFallbackMetadataBindingAndLiteralGates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*db.Agent, *db.AgentFallbackConfig, *db.AgentRuntime)
		status MetadataStatus
		reason string
	}{
		{"runtime defaults", func(*db.Agent, *db.AgentFallbackConfig, *db.AgentRuntime) {}, MetadataCompatible, ""},
		{"offline is not incompatibility", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) { r.Status = "offline" }, MetadataCompatible, ""},
		{"public runtime", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) {
			r.Visibility = "public"
			r.OwnerID = pgtype.UUID{}
		}, MetadataCompatible, ""},
		{"missing agent", func(a *db.Agent, _ *db.AgentFallbackConfig, _ *db.AgentRuntime) { a.ID = pgtype.UUID{} }, MetadataIncompatible, "agent_binding"},
		{"zero config generation", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) {
			c.Generation = pgtype.UUID{Valid: true}
		}, MetadataIncompatible, "agent_binding"},
		{"foreign agent", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) { c.AgentID = newGeneration() }, MetadataIncompatible, "agent_binding"},
		{"foreign config workspace", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) { c.WorkspaceID = newGeneration() }, MetadataIncompatible, "agent_binding"},
		{"system agent", func(a *db.Agent, _ *db.AgentFallbackConfig, _ *db.AgentRuntime) { a.Kind = "system" }, MetadataIncompatible, "agent_inactive"},
		{"archived agent", func(a *db.Agent, _ *db.AgentFallbackConfig, _ *db.AgentRuntime) { a.ArchivedAt.Valid = true }, MetadataIncompatible, "agent_inactive"},
		{"missing runtime", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) { r.ID = pgtype.UUID{} }, MetadataIncompatible, "runtime_binding"},
		{"foreign runtime workspace", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) { r.WorkspaceID = newGeneration() }, MetadataIncompatible, "runtime_binding"},
		{"changed runtime", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) { c.RuntimeID = newGeneration() }, MetadataIncompatible, "runtime_binding"},
		{"private runtime wrong owner", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) { r.OwnerID = newGeneration() }, MetadataIncompatible, "runtime_owner"},
		{"private runtime missing owner", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) { r.OwnerID = pgtype.UUID{} }, MetadataIncompatible, "runtime_owner"},
		{"unknown visibility", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) { r.Visibility = "future" }, MetadataIncompatible, "runtime_visibility"},
		{"unknown adapter", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) { r.Provider = "future-adapter" }, MetadataUnknown, "runtime_provider"},
		{"builtin identity", func(_ *db.Agent, _ *db.AgentFallbackConfig, r *db.AgentRuntime) { r.Provider = "omp" }, MetadataCompatible, ""},
		{"builtin identity explicit model guard", func(_ *db.Agent, c *db.AgentFallbackConfig, r *db.AgentRuntime) {
			r.Provider = "omp"
			c.ThinkingLevel = "high"
		}, MetadataIncompatible, "thinking_requires_model"},
		{"model override unsupported", func(_ *db.Agent, c *db.AgentFallbackConfig, r *db.AgentRuntime) {
			r.Provider = "qwenpaw"
			c.Model = "custom"
		}, MetadataIncompatible, "model_override_unsupported"},
		{"known model family mismatch", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) { c.Model = "claude-opus-5" }, MetadataIncompatible, "model_provider"},
		{"invalid thinking literal", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) { c.ThinkingLevel = "--unsafe" }, MetadataIncompatible, "thinking_value"},
		{"thinking without pinned model", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) { c.ThinkingLevel = "high" }, MetadataIncompatible, "thinking_requires_model"},
		{"invalid tier literal", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) { c.ServiceTier = "--secret" }, MetadataIncompatible, "service_tier_value"},
		{"tier on another provider", func(_ *db.Agent, c *db.AgentFallbackConfig, r *db.AgentRuntime) {
			r.Provider = "claude"
			c.ServiceTier = "fast"
		}, MetadataIncompatible, "service_tier_value"},
		{"tier requires model", func(_ *db.Agent, c *db.AgentFallbackConfig, _ *db.AgentRuntime) { c.ServiceTier = "fast" }, MetadataIncompatible, "service_tier_requires_model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, c, r, catalog := metadataFixture()
			tc.change(&a, &c, &r)
			got := AssessFallbackMetadata(a, c, r, catalog)
			if got.Status != tc.status || got.Reason != tc.reason {
				t.Fatalf("got %+v want %s/%s", got, tc.status, tc.reason)
			}
		})
	}
}

func TestFallbackMetadataUsesUpstreamCatalogSemantics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*db.AgentFallbackConfig, *db.AgentRuntime, *agent.Catalog)
		status MetadataStatus
		reason string
	}{
		{"unknown thinking catalog", func(c *db.AgentFallbackConfig, r *db.AgentRuntime, k *agent.Catalog) {
			r.Provider = "claude"
			c.ThinkingLevel = "high"
			k.Models = nil
		}, MetadataUnknown, "thinking_effective_model"},
		{"known binary has no effort flag", func(c *db.AgentFallbackConfig, r *db.AgentRuntime, k *agent.Catalog) {
			r.Provider = "claude"
			c.ThinkingLevel = "high"
			k.Models = nil
			k.CLIThinkingLevels = []string{}
		}, MetadataIncompatible, "thinking_capability"},
		{"default badge does not prove effective model", func(c *db.AgentFallbackConfig, r *db.AgentRuntime, k *agent.Catalog) {
			r.Provider = "claude"
			c.ThinkingLevel = "high"
			k.Models = []agent.Model{{ID: "claude-test", Default: true, Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{{Value: "high"}}}}}
		}, MetadataUnknown, "thinking_effective_model"},
		{"explicit verified model", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, _ *agent.Catalog) { c.Model = "gpt-test" }, MetadataCompatible, ""},
		{"unverified catalog", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, k *agent.Catalog) {
			c.Model = "gpt-test"
			k.Fallback = true
		}, MetadataUnknown, "model_catalog"},
		{"empty catalog", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, k *agent.Catalog) {
			c.Model = "gpt-test"
			k.Models = nil
		}, MetadataUnknown, "model_catalog"},
		{"unknown manual model", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, _ *agent.Catalog) { c.Model = "custom-secret-model" }, MetadataUnknown, "model_unverified"},
		{"runtime unavailable model", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, k *agent.Catalog) {
			c.Model = "gpt-test"
			k.Unavailable = []agent.UnavailableModel{{ID: c.Model, Reason: "secret diagnostic"}}
		}, MetadataIncompatible, "model_unavailable"},
		{"unavailable in fallback list is not proof", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, k *agent.Catalog) {
			c.Model = "gpt-test"
			k.Fallback = true
			k.Unavailable = []agent.UnavailableModel{{ID: c.Model}}
		}, MetadataUnknown, "model_catalog"},
		{"supported thinking", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, _ *agent.Catalog) {
			c.Model = "gpt-test"
			c.ThinkingLevel = "high"
		}, MetadataCompatible, ""},
		{"model thinking mismatch", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, _ *agent.Catalog) {
			c.Model = "gpt-test"
			c.ThinkingLevel = "future-level"
		}, MetadataIncompatible, "thinking_capability"},
		{"CLI effort restriction", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, k *agent.Catalog) {
			c.Model = "gpt-test"
			c.ThinkingLevel = "high"
			k.CLIThinkingLevels = []string{}
		}, MetadataIncompatible, "thinking_capability"},
		{"supported tier", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, _ *agent.Catalog) {
			c.Model = "gpt-test"
			c.ServiceTier = "fast"
		}, MetadataCompatible, ""},
		{"unknown tier", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, _ *agent.Catalog) {
			c.Model = "gpt-test"
			c.ServiceTier = "future-tier"
		}, MetadataIncompatible, "service_tier_capability"},
		{"standard routing binary capability", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, k *agent.Catalog) {
			c.ServiceTier = "default"
			k.Fallback = true
			k.Models[0].SupportsExplicitStandardServiceTier = true
		}, MetadataCompatible, ""},
		{"unknown standard routing binary", func(c *db.AgentFallbackConfig, _ *db.AgentRuntime, k *agent.Catalog) {
			c.ServiceTier = "default"
			k.Models = nil
		}, MetadataUnknown, "service_tier_catalog"},
		{"qualified gateway model", func(c *db.AgentFallbackConfig, r *db.AgentRuntime, k *agent.Catalog) {
			r.Provider = "opencode"
			c.Model = "gateway/model"
			k.Models = []agent.Model{{ID: "provider/gateway/model", Provider: "provider"}}
		}, MetadataCompatible, ""},
		{"ambiguous gateway model", func(c *db.AgentFallbackConfig, r *db.AgentRuntime, k *agent.Catalog) {
			r.Provider = "opencode"
			c.Model = "gateway/model"
			k.Models = []agent.Model{{ID: "one/gateway/model", Provider: "one"}, {ID: "two/gateway/model", Provider: "two"}}
		}, MetadataUnknown, "model_unverified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, c, r, _ := metadataFixture()
			k := agent.Catalog{Models: []agent.Model{{ID: "gpt-test", Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{{Value: "high"}}}, ServiceTiers: []agent.ModelServiceTier{{ID: "fast"}}}}}
			tc.change(&c, &r, &k)
			before := c
			got := AssessFallbackMetadata(a, c, r, k)
			if got.Status != tc.status || got.Reason != tc.reason {
				t.Fatalf("got %+v want %s/%s", got, tc.status, tc.reason)
			}
			if !reflect.DeepEqual(c, before) {
				t.Fatal("assessment rewrote saved options")
			}
		})
	}
}
