package failover

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestNativePathBindingDescribesSelectedTargetWithoutExecutionAuthority(t *testing.T) {
	a, c, s, primary, fallback := resolutionFixture(t)
	c.NativeLoginPath = true
	primary.Provider = "codex"
	primary.ProfileID, fallback.ProfileID = newGeneration(), newGeneration()
	a.Model, a.ThinkingLevel = targetText("gpt-primary-test"), targetText("low")
	a.ServiceTier = targetText("fast")
	c.Model, c.ThinkingLevel = "gpt-fallback-test", "high"
	catalog := agent.Catalog{Models: []agent.Model{
		{ID: a.Model.String, Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{{Value: "low"}}}, ServiceTiers: []agent.ModelServiceTier{{ID: "fast"}}},
		{ID: c.Model, Thinking: &agent.ModelThinking{SupportedLevels: []agent.ThinkingLevel{{Value: "high"}}}},
	}}
	beforeAgent, beforeConfig := cloneExecutionAgent(a), c
	beforeConfig.ExecutionConfig = bytes.Clone(c.ExecutionConfig)
	for _, tc := range []struct {
		route, model, effort, mode, tier string
		runtime                          db.AgentRuntime
	}{
		{"primary", a.Model.String, "low", a.RuntimeMode, "fast", primary},
		{"fallback", c.Model, "high", fallback.RuntimeMode, "", fallback},
	} {
		t.Run(tc.route, func(t *testing.T) {
			b, err := DescribeNativePathBinding(a, c, s, tc.route, tc.runtime, catalog)
			if err != nil || b.AgentID != a.ID || b.WorkspaceID != a.WorkspaceID || b.AgentOwnerID != a.OwnerID || b.RuntimeID != tc.runtime.ID || b.RuntimeProfileID != tc.runtime.ProfileID || b.PrimaryGeneration != s.PrimaryGeneration || b.FallbackGeneration != c.Generation || b.Route != tc.route || b.Model != tc.model || b.ThinkingLevel != tc.effort || b.RuntimeMode != tc.mode || b.ServiceTier != tc.tier {
				t.Fatal("native descriptor lost target binding", err)
			}
			raw, err := json.Marshal(b)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"primary-private", "fallback-private", "shared instructions", "private-skill", "shared-app"} {
				if strings.Contains(string(raw), secret) {
					t.Fatal("descriptor exposed execution context")
				}
			}
			// Describing options cannot bypass the strict/native execution gate.
			if _, err := ResolveEnqueueTarget(a, &c, &s, ResolutionContext{Runtime: tc.runtime, Catalog: catalog}); !errors.Is(err, ErrExecutionEvidence) {
				t.Fatal("metadata description granted execution", err)
			}
		})
	}
	if !reflect.DeepEqual(a, beforeAgent) || !reflect.DeepEqual(c, beforeConfig) {
		t.Fatal("native description rewrote shared context or saved target options")
	}
}

func TestNativePathBindingRejectsMissingOrUnverifiedSelection(t *testing.T) {
	for _, mode := range []string{"strict config", "implicit primary", "implicit fallback", "default badge", "unverified catalog", "unknown model", "unavailable model", "wrong runtime", "suspended policy", "unknown route"} {
		t.Run(mode, func(t *testing.T) {
			a, c, s, primary, fallback := resolutionFixture(t)
			c.NativeLoginPath, c.Model = true, "gpt-test"
			a.Model, primary.Provider = targetText("gpt-test"), "codex"
			route, runtime := "primary", primary
			catalog := agent.Catalog{Models: []agent.Model{{ID: "gpt-test"}}}
			switch mode {
			case "strict config":
				c.NativeLoginPath = false
			case "implicit primary":
				a.Model = targetText("")
			case "implicit fallback":
				c.Model, route, runtime = "", "fallback", fallback
			case "default badge":
				a.Model = targetText("")
				catalog.Models[0].Default = true
			case "unverified catalog":
				catalog.Fallback = true
			case "unknown model":
				a.Model = targetText("custom-unverified")
			case "unavailable model":
				catalog.Unavailable = []agent.UnavailableModel{{ID: "gpt-test"}}
			case "wrong runtime":
				runtime = fallback
			case "suspended policy":
				s.SuspendedAt.Valid = true
			case "unknown route":
				route = "future"
			}
			b, err := DescribeNativePathBinding(a, c, s, route, runtime, catalog)
			if err == nil || b != (TargetBinding{}) {
				t.Fatal("inconclusive native selection produced a descriptor")
			}
		})
	}
}
