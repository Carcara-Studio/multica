package failover

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func resolutionFixture(t *testing.T) (db.Agent, db.AgentFallbackConfig, db.AgentFailoverState, db.AgentRuntime, db.AgentRuntime) {
	t.Helper()
	a, c, primary, _ := metadataFixture()
	primary.Provider = "claude"
	primary.RuntimeMode = "local"
	a.RuntimeID = primary.ID
	a.RuntimeMode = "local"
	a.Name = "shared identity"
	a.Instructions = "shared instructions"
	a.PermissionMode = "private"
	a.CustomEnv = []byte(`{"PRIMARY":"primary-private-value"}`)
	a.CustomArgs = []byte(`["--primary-private-argument"]`)
	a.RuntimeConfig = []byte(`{"token":"primary-private-config"}`)
	a.McpConfig = []byte(`{"primary":"primary-private-MCP"}`)
	a.DisabledRuntimeSkills = []byte(`{"provider":["private-skill"]}`)
	a.ComposioToolkitAllowlist = []string{"shared-app"}
	fallback := primary
	fallback.ID = newGeneration()
	fallback.Provider = "codex"
	fallback.RuntimeMode = "cloud"
	c.RuntimeID = fallback.ID
	raw, err := json.Marshal(ExecutionOverrides{Version: 1, CustomEnv: map[string]string{"FALLBACK": "fallback-private-value"}, CustomArgs: []string{"--fallback-private-argument"}, RuntimeConfig: json.RawMessage(`{"token":"fallback-private-config"}`), MCPConfig: json.RawMessage(`{"fallback":"fallback-private-MCP"}`)})
	if err != nil {
		t.Fatal(err)
	}
	c.ExecutionConfig = raw
	s := db.AgentFailoverState{AgentID: a.ID, WorkspaceID: a.WorkspaceID, PrimaryGeneration: newGeneration(), Version: 10, State: "primary_active"}
	return a, c, s, primary, fallback
}
func resolutionContext(t *testing.T, a db.Agent, c db.AgentFallbackConfig, s db.AgentFailoverState, route string, r db.AgentRuntime, catalogs ...agent.Catalog) ResolutionContext {
	t.Helper()
	now := time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC)
	catalog := agent.Catalog{}
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	binding, err := DescribeTargetBinding(a, c, s, route, r, catalog)
	if err != nil {
		t.Fatal(err)
	}
	connection := RuntimeConnection{RuntimeID: r.ID, WorkspaceID: r.WorkspaceID, OwnerID: r.OwnerID, SessionID: newGeneration(), Provider: r.Provider}
	identity := ExecutionIdentity{CredentialEpoch: newGeneration(), ConfigurationEpoch: newGeneration(), EffectiveModel: "verified-default-model"}
	if binding.Model != "" {
		identity.EffectiveModel = binding.Model
	}
	ch := EvidenceChallenge{Token: newGeneration(), Binding: binding, Connection: connection, Identity: identity, Use: TaskEvidence, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute)}
	report := EvidenceReport{Version: 1, Challenge: ch.Token, Binding: binding, Connection: connection, Identity: identity, Use: TaskEvidence, AdapterContract: "fixture-only/v1", Features: taskEvidenceFeatures}
	v := newEvidenceVerifier(map[AdapterContract]EvidenceFeature{{Provider: r.Provider, Version: report.AdapterContract}: taskEvidenceFeatures})
	proof, err := v.Verify(ch, report, connection, identity, now)
	if err != nil {
		t.Fatal(err)
	}
	return ResolutionContext{Runtime: r, Catalog: catalog, Connection: connection, Identity: identity, Evidence: proof, Now: now}
}
func queuedResolution(t *testing.T, d ResolvedTarget) (db.AgentTaskQueue, db.AgentTaskExecutionRoute) {
	t.Helper()
	task := db.AgentTaskQueue{ID: newGeneration(), AgentID: d.Agent().ID, RuntimeID: d.Agent().RuntimeID, Status: "queued"}
	p, err := d.RouteProof(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return task, db.AgentTaskExecutionRoute{TaskID: task.ID, AgentID: p.AgentID, WorkspaceID: p.WorkspaceID, RuntimeID: task.RuntimeID, Route: p.Route, PrimaryGeneration: p.PrimaryGeneration, FallbackGeneration: p.FallbackGeneration, StateVersion: p.StateVersion}
}

func TestUnconfiguredResolutionPreservesUpstreamPrimaryAndNoProof(t *testing.T) {
	a, _, _, primary, _ := resolutionFixture(t)
	for _, kind := range []string{"user", "system"} {
		a.Kind = kind
		d, err := ResolveEnqueueTarget(a, nil, nil, ResolutionContext{})
		if err != nil || d.OptedIn() || d.FreshSessionRequired() || d.Route() != "primary" || !reflect.DeepEqual(d.Agent(), a) {
			t.Fatal("ordinary Agent behavior changed", err)
		}
		if _, err = d.RouteProof(newGeneration()); !errors.Is(err, ErrTargetResolution) {
			t.Fatal("ordinary work gained opt-in quota proof")
		}
		task := db.AgentTaskQueue{ID: newGeneration(), AgentID: a.ID, RuntimeID: a.RuntimeID}
		got, err := ResolveQueuedTarget(a, nil, nil, task, nil, ResolutionContext{Runtime: primary})
		if err != nil || !reflect.DeepEqual(got.Agent(), a) {
			t.Fatal("legacy claim changed", err)
		}
		task.RuntimeID = newGeneration()
		if _, err = ResolveQueuedTarget(a, nil, nil, task, nil, ResolutionContext{Runtime: primary}); !errors.Is(err, ErrTargetResolution) {
			t.Fatal("unmarked claim bypassed runtime fence")
		}
	}
}

func TestFallbackResolutionOverlaysTargetWithoutDuplicatingIdentityOrSecrets(t *testing.T) {
	a, c, s, _, fallback := resolutionFixture(t)
	s.State = "fallback_active"
	ctx := resolutionContext(t, a, c, s, "fallback", fallback)
	before := cloneExecutionAgent(a)
	d, err := ResolveEnqueueTarget(a, &c, &s, ctx)
	if err != nil {
		t.Fatal(err)
	}
	effective := d.Agent()
	if !d.OptedIn() || !d.FreshSessionRequired() || d.Route() != "fallback" || effective.RuntimeID != fallback.ID || effective.RuntimeMode != fallback.RuntimeMode {
		t.Fatal("fallback selection wrong")
	}
	if effective.ID != a.ID || effective.WorkspaceID != a.WorkspaceID || effective.OwnerID != a.OwnerID || effective.Instructions != a.Instructions || effective.PermissionMode != a.PermissionMode || !reflect.DeepEqual(effective.ComposioToolkitAllowlist, a.ComposioToolkitAllowlist) || !reflect.DeepEqual(effective.DisabledRuntimeSkills, a.DisabledRuntimeSkills) {
		t.Fatal("fallback duplicated/lost shared identity/context")
	}
	var env map[string]string
	if json.Unmarshal(effective.CustomEnv, &env) != nil || env["FALLBACK"] != "fallback-private-value" || len(env) != 1 {
		t.Fatal("primary credential leaked into fallback")
	}
	if !reflect.DeepEqual(a, before) {
		t.Fatal("resolution mutated primary")
	}
	effective.CustomEnv[0] = 'x'
	effective.DisabledRuntimeSkills[0] = 'x'
	effective.ComposioToolkitAllowlist[0] = "mutated"
	if !reflect.DeepEqual(a, before) || !json.Valid(d.Agent().CustomEnv) {
		t.Fatal("resolved payload aliases source or previous caller")
	}
	task, proof := queuedResolution(t, d)
	// Availability-only recovery changes version, not ownership/identity epochs.
	s.State = "primary_active"
	s.Version++
	claimed, err := ResolveQueuedTarget(a, &c, &s, task, &proof, ctx)
	if err != nil || claimed.Route() != "fallback" || claimed.Agent().RuntimeID != fallback.ID {
		t.Fatal("recovery rerouted queued fallback", err)
	}
	if _, err = claimed.RouteProof(task.ID); !errors.Is(err, ErrTargetResolution) {
		t.Fatal("claim result can rewrite queue proof")
	}
}

func TestQueuedPrimaryRemainsPrimaryAfterFailoverAndLegacyCannotBecomeFallback(t *testing.T) {
	a, c, s, primary, fallback := resolutionFixture(t)
	ctx := resolutionContext(t, a, c, s, "primary", primary)
	d, err := ResolveEnqueueTarget(a, &c, &s, ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, proof := queuedResolution(t, d)
	s.State = "fallback_active"
	s.Version++
	got, err := ResolveQueuedTarget(a, &c, &s, task, &proof, ctx)
	if err != nil || got.Route() != "primary" || !reflect.DeepEqual(got.Agent(), a) {
		t.Fatal("failover changed queued primary", err)
	}
	legacy, err := ResolveQueuedTarget(a, &c, &s, task, nil, ResolutionContext{Runtime: primary})
	if err != nil || legacy.OptedIn() {
		t.Fatal("legacy work gained opt-in authority", err)
	}
	task.RuntimeID = fallback.ID
	if _, err = ResolveQueuedTarget(a, &c, &s, task, nil, ResolutionContext{Runtime: fallback}); !errors.Is(err, ErrTargetResolution) {
		t.Fatal("missing marker granted fallback payload")
	}
}

func TestTargetResolutionRejectsStaleEpochsProofAndPolicy(t *testing.T) {
	for _, name := range []string{"primary generation", "fallback generation", "removed config", "missing state", "suspended", "archived", "wrong task", "wrong workspace", "wrong runtime", "future version", "invalid route", "expired evidence", "connection", "credentials", "local config", "metadata-only"} {
		t.Run(name, func(t *testing.T) {
			a, c, s, _, fallback := resolutionFixture(t)
			s.State = "fallback_active"
			ctx := resolutionContext(t, a, c, s, "fallback", fallback)
			d, err := ResolveEnqueueTarget(a, &c, &s, ctx)
			if err != nil {
				t.Fatal(err)
			}
			task, proof := queuedResolution(t, d)
			cfg, state := &c, &s
			switch name {
			case "primary generation":
				s.PrimaryGeneration = newGeneration()
			case "fallback generation":
				c.Generation = newGeneration()
			case "removed config":
				cfg = nil
			case "missing state":
				state = nil
			case "suspended":
				s.SuspendedAt = pgtype.Timestamptz{Time: ctx.Now, Valid: true}
			case "archived":
				a.ArchivedAt = pgtype.Timestamptz{Time: ctx.Now, Valid: true}
			case "wrong task":
				proof.TaskID = newGeneration()
			case "wrong workspace":
				proof.WorkspaceID = newGeneration()
			case "wrong runtime":
				proof.RuntimeID = newGeneration()
			case "future version":
				proof.StateVersion = s.Version + 1
			case "invalid route":
				proof.Route = "unknown"
			case "expired evidence":
				ctx.Now = ctx.Now.Add(time.Minute)
			case "connection":
				ctx.Connection.SessionID = newGeneration()
			case "credentials":
				ctx.Identity.CredentialEpoch = newGeneration()
			case "local config":
				ctx.Identity.ConfigurationEpoch = newGeneration()
			case "metadata-only":
				c.ExecutionConfig = nil
			}
			if _, err = ResolveQueuedTarget(a, cfg, state, task, &proof, ctx); err == nil {
				t.Fatal("stale/incomplete route launched")
			}
		})
	}
}

func TestSameRuntimeTargetsRequireDifferentEvidenceAndFreshSessions(t *testing.T) {
	a, c, s, primary, _ := resolutionFixture(t)
	c.RuntimeID = primary.ID
	ctxPrimary := resolutionContext(t, a, c, s, "primary", primary)
	s.State = "fallback_active"
	if _, err := ResolveEnqueueTarget(a, &c, &s, ctxPrimary); !errors.Is(err, ErrExecutionEvidence) {
		t.Fatal("primary evidence granted fallback on same runtime")
	}
	ctxFallback := resolutionContext(t, a, c, s, "fallback", primary)
	d, err := ResolveEnqueueTarget(a, &c, &s, ctxFallback)
	if err != nil || !d.FreshSessionRequired() || d.Agent().RuntimeID != a.RuntimeID {
		t.Fatal("same-runtime fallback session isolation missing", err)
	}
	s.State = "primary_probing"
	if d, err = ResolveEnqueueTarget(a, &c, &s, ctxFallback); err != nil || d.Route() != "fallback" {
		t.Fatal("probe activity routed new work to primary", err)
	}
}

func TestConfiguredTargetsNeedEvidenceAndUnknownStateCannotFallThrough(t *testing.T) {
	a, c, s, primary, _ := resolutionFixture(t)
	if _, err := ResolveEnqueueTarget(a, &c, &s, ResolutionContext{Runtime: primary}); !errors.Is(err, ErrExecutionEvidence) {
		t.Fatal("saved config or catalog became readiness")
	}
	for _, name := range []string{"missing config", "unknown state", "system", "invalid overrides"} {
		t.Run(name, func(t *testing.T) {
			a, c, s, _, fallback := resolutionFixture(t)
			s.State = "fallback_active"
			cfg := &c
			switch name {
			case "missing config":
				cfg = nil
			case "unknown state":
				s.State = "new-state"
			case "system":
				a.Kind = "system"
			case "invalid overrides":
				c.ExecutionConfig = []byte(`{"version":2}`)
			}
			if _, err := ResolveEnqueueTarget(a, cfg, &s, ResolutionContext{Runtime: fallback}); err == nil {
				t.Fatal("invalid opt-in silently used primary")
			}
		})
	}
}

func TestTargetBindingRequiresKnownPinnedModelAndPreservesRequestedAlias(t *testing.T) {
	a, c, s, _, fallback := resolutionFixture(t)
	s.State = "fallback_active"
	fallback.Provider = "opencode"
	c.Model = "coder"
	if _, err := DescribeTargetBinding(a, c, s, "fallback", fallback, agent.Catalog{}); !errors.Is(err, ErrTargetResolution) {
		t.Fatal("unverified pinned model granted binding")
	}
	catalog := agent.Catalog{Models: []agent.Model{{ID: "vendor/coder", Provider: "vendor"}}}
	binding, err := DescribeTargetBinding(a, c, s, "fallback", fallback, catalog)
	if err != nil || binding.Model != "vendor/coder" {
		t.Fatal("binding did not use verified canonical model", err)
	}
	ctx := resolutionContext(t, a, c, s, "fallback", fallback, catalog)
	target, err := ResolveEnqueueTarget(a, &c, &s, ctx)
	if err != nil || target.Agent().Model.String != "coder" {
		t.Fatal("resolver rewrote configured model intent", err)
	}
	ctx.Identity.EffectiveModel = "other/model"
	if _, err := ResolveEnqueueTarget(a, &c, &s, ctx); !errors.Is(err, ErrExecutionEvidence) {
		t.Fatal("different effective model reused capability proof")
	}
}
