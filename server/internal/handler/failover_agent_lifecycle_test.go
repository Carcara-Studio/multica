package handler

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func probingAgentForMutation(t *testing.T, over testutil.Cols) (*testutil.Fixture, string, string, db.AgentFailoverState) {
	t.Helper()
	if testPool == nil || testHandler == nil {
		t.Skip("database not available")
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	runtime := fx.Runtime(t, "Mutation primary", testutil.Cols{"provider": "claude", "runtime_mode": "local"})
	cols := testutil.Cols{"model": "claude-test", "custom_env": testutil.Raw(`'{"KEY":"old-secret"}'::jsonb`)}
	for k, v := range over {
		cols[k] = v
	}
	id := fx.Agent(t, "Mutation agent "+uuid.NewString(), runtime, cols)
	seedMutationProbe(t, fx, id, runtime, testWorkspaceID)
	return fx, id, runtime, mutationState(t, id)
}

func seedMutationProbe(t *testing.T, fx *testutil.Fixture, id, runtime, workspace string) {
	t.Helper()
	fx.InsertNoID(t, "agent_failover_state", testutil.Cols{
		"agent_id": id, "workspace_id": workspace, "primary_generation": uuid.NewString(), "version": 5,
		"state": "primary_probing", "probe_token": uuid.NewString(), "probe_runtime_id": runtime,
		"last_probe_at": testutil.Raw("now()"), "probe_dispatched_at": testutil.Raw("now()"), "probe_lease_until": testutil.Raw("now()+interval '2 minutes'"),
	}, "agent_id=$1", id)
}
func mutationState(t *testing.T, id string) db.AgentFailoverState {
	t.Helper()
	s, err := testHandler.Queries.GetAgentFailoverState(t.Context(), db.GetAgentFailoverStateParams{AgentID: parseUUID(id), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func assertMutationFenced(t *testing.T, id string, old db.AgentFailoverState, suspended bool) {
	t.Helper()
	s := mutationState(t, id)
	if s.PrimaryGeneration == old.PrimaryGeneration || s.Version <= old.Version || s.ProbeToken.Valid || s.ProbeRuntimeID.Valid || s.ProbeDispatchedAt.Valid || s.ProbeLeaseUntil.Valid || s.State != "fallback_active" || s.SuspendedAt.Valid != suspended {
		t.Fatal("Agent mutation did not fence probe and preserve availability")
	}
	_, err := testHandler.Queries.SettleAgentPrimaryProbe(t.Context(), db.SettleAgentPrimaryProbeParams{
		AgentID: old.AgentID, WorkspaceID: old.WorkspaceID, PrimaryGeneration: old.PrimaryGeneration, ProbeToken: old.ProbeToken, PrimaryRuntimeID: old.ProbeRuntimeID, Outcome: "success",
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("old success err=%v", err)
	}
}

func TestAgentEditsInvalidateExecutionPolicyOnlyForStoredChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		over   testutil.Cols
		body   map[string]any
		change bool
	}{
		{"cosmetic", nil, map[string]any{"description": "cosmetic edit"}, false},
		{"no-op model", nil, map[string]any{"model": "claude-test"}, false},
		{"model", nil, map[string]any{"model": "claude-new"}, true},
		{"runtime config", nil, map[string]any{"runtime_config": map[string]any{"mode": "embedded"}}, true},
		{"custom arguments", nil, map[string]any{"custom_args": []string{"--test-option"}}, true},
		{"MCP", nil, map[string]any{"mcp_config": map[string]any{}}, true},
		{"clear MCP", testutil.Cols{"mcp_config": testutil.Raw(`'{"mcpServers":{}}'::jsonb`)}, map[string]any{"mcp_config": nil}, true},
		{"clear thinking", testutil.Cols{"thinking_level": "high"}, map[string]any{"thinking_level": ""}, true},
		{"clear tier", testutil.Cols{"service_tier": "fast"}, map[string]any{"service_tier": ""}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, id, _, old := probingAgentForMutation(t, tc.over)
			req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+id, tc.body), "id", id)
			testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusOK)
			if tc.change {
				assertMutationFenced(t, id, old, false)
			} else if !reflect.DeepEqual(mutationState(t, id), old) {
				t.Fatal("cosmetic/no-op edit changed availability")
			}
		})
	}
}

func TestAgentEnvAndArchiveUseAtomicInvalidation(t *testing.T) {
	t.Run("env", func(t *testing.T) {
		_, id, _, old := probingAgentForMutation(t, nil)
		req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+id+"/env", map[string]any{"custom_env": map[string]string{"KEY": "new-secret"}}), "id", id)
		testutil.Call(t, testHandler.UpdateAgentEnv, req).Want(http.StatusOK)
		assertMutationFenced(t, id, old, false)
	})
	t.Run("archive and restore", func(t *testing.T) {
		_, id, _, old := probingAgentForMutation(t, nil)
		req := withURLParam(newRequest(http.MethodPost, "/api/agents/"+id+"/archive", nil), "id", id)
		testutil.Call(t, testHandler.ArchiveAgent, req).Want(http.StatusOK)
		assertMutationFenced(t, id, old, true)
		suspended := mutationState(t, id)
		req = withURLParam(newRequest(http.MethodPost, "/api/agents/"+id+"/restore", nil), "id", id)
		testutil.Call(t, testHandler.RestoreAgent, req).Want(http.StatusOK)
		if !reflect.DeepEqual(mutationState(t, id), suspended) {
			t.Fatal("restore silently resumed or recovered primary")
		}
	})
	t.Run("runtime move", func(t *testing.T) {
		fx, id, _, old := probingAgentForMutation(t, nil)
		next := fx.Runtime(t, "Next primary", testutil.Cols{"provider": "claude"})
		req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+id, map[string]any{"runtime_id": next}), "id", id)
		testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusOK)
		assertMutationFenced(t, id, old, false)
	})
}

func TestAgentInvalidationFailureRollsBackMutationAndEnvAudit(t *testing.T) {
	for _, kind := range []string{"model", "env", "archive"} {
		t.Run(kind, func(t *testing.T) {
			fx, id, _, _ := probingAgentForMutation(t, nil)
			// A valid bigint at its ceiling makes rotation fail AFTER the Agent
			// and (for env) audit writes, without installing shared test triggers.
			fx.Exec(t, `UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2`, int64(math.MaxInt64), id)
			old := mutationState(t, id)
			switch kind {
			case "model":
				req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+id, map[string]any{"model": "claude-new"}), "id", id)
				testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusInternalServerError)
			case "env":
				req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+id+"/env", map[string]any{"custom_env": map[string]string{"KEY": "new-secret"}}), "id", id)
				testutil.Call(t, testHandler.UpdateAgentEnv, req).Want(http.StatusInternalServerError)
			case "archive":
				req := withURLParam(newRequest(http.MethodPost, "/api/agents/"+id+"/archive", nil), "id", id)
				testutil.Call(t, testHandler.ArchiveAgent, req).Want(http.StatusInternalServerError)
			}
			var model, env string
			var archived bool
			var audits int
			fx.QueryRow(t, `SELECT model,custom_env->>'KEY',archived_at IS NOT NULL FROM agent WHERE id=$1`, id).Scan(&model, &env, &archived)
			fx.QueryRow(t, `SELECT count(*) FROM activity_log WHERE details->>'agent_id'=$1 AND action='agent_env_updated'`, id).Scan(&audits)
			if model != "claude-test" || env != "old-secret" || archived || audits != 0 || !reflect.DeepEqual(mutationState(t, id), old) {
				t.Fatal("failed invalidation committed a partial Agent/env/audit/archive mutation")
			}
		})
	}
}

func TestAgentMutationDoesNotInitializeFailoverState(t *testing.T) {
	fx, id, _, _ := probingAgentForMutation(t, nil)
	fx.Exec(t, `DELETE FROM agent_failover_state WHERE agent_id=$1`, id)
	req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+id, map[string]any{"model": "claude-new"}), "id", id)
	testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusOK)
	_, err := testHandler.Queries.GetAgentFailoverState(t.Context(), db.GetAgentFailoverStateParams{AgentID: parseUUID(id), WorkspaceID: parseUUID(testWorkspaceID)})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("mutation initialized failover: %v", err)
	}
}

func TestRuntimeTeardownSuspendsPrimaryFailoverPolicy(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint("rollback=", fail), func(t *testing.T) {
			fx, id, runtime, old := probingAgentForMutation(t, nil)
			fx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, id)
			status := http.StatusOK
			if fail {
				fx.Exec(t, `UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2`, int64(math.MaxInt64), id)
				old = mutationState(t, id)
				status = http.StatusInternalServerError
			}
			req := withURLParam(newRequest(http.MethodDelete, "/api/runtimes/"+runtime, nil), "runtimeId", runtime)
			testutil.Call(t, testHandler.DeleteAgentRuntime, req).Want(status)
			a, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(id))
			if err != nil {
				t.Fatal(err)
			}
			if fail {
				if a.RuntimeID != parseUUID(runtime) || !reflect.DeepEqual(mutationState(t, id), old) {
					t.Fatal("failed invalidation did not roll back runtime teardown")
				}
				if _, err := testHandler.Queries.GetAgentRuntime(t.Context(), parseUUID(runtime)); err != nil {
					t.Fatal(err)
				}
			} else {
				assertMutationFenced(t, id, old, true)
				if a.RuntimeID.Valid {
					t.Fatal("runtime still bound")
				}
			}
		})
	}
}

func TestConcurrentAgentEditsSerializeFailoverGenerations(t *testing.T) {
	_, id, _, old := probingAgentForMutation(t, nil)
	var wg sync.WaitGroup
	for _, model := range []string{"claude-first", "claude-second"} {
		wg.Go(func() {
			req := withURLParam(newRequest(http.MethodPatch, "/api/agents/"+id, map[string]any{"model": model}), "id", id)
			testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusOK)
		})
	}
	wg.Wait()
	s := mutationState(t, id)
	if s.Version != old.Version+2 {
		t.Fatalf("lost concurrent invalidation: version=%d", s.Version)
	}
	assertMutationFenced(t, id, old, false)
}

func TestRuntimeSkillControlInvalidatesFailoverPolicy(t *testing.T) {
	_, id, runtime, old := probingAgentForMutation(t, nil)
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+id+"/runtime-skills/enabled", map[string]any{
		"runtime_id": runtime, "root": "provider", "key": "example-skill", "name": "Example", "enabled": false,
	}), "id", id)
	testutil.Call(t, testHandler.SetAgentRuntimeSkillEnabled, req).Want(http.StatusNoContent)
	assertMutationFenced(t, id, old, false)
}

func TestMemberRevocationSuspendsArchivedAgentPolicy(t *testing.T) {
	if testPool == nil || testHandler == nil {
		t.Skip("database not available")
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	owner := fx.User(t, "Revoked policy owner", "revoked-policy-"+uuid.NewString()+"@example.test")
	member := fx.Member(t, testWorkspaceID, owner, "member")
	runtime := fx.Runtime(t, "Revoked policy runtime", testutil.Cols{"owner_id": owner, "provider": "claude"})
	id := fx.Agent(t, "Revoked policy agent "+uuid.NewString(), runtime, testutil.Cols{"owner_id": owner})
	seedMutationProbe(t, fx, id, runtime, testWorkspaceID)
	old := mutationState(t, id)
	if _, err := testHandler.revokeAndRemoveMember(t.Context(), parseUUID(testWorkspaceID), parseUUID(owner), parseUUID(member), parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	assertMutationFenced(t, id, old, true)
}

func TestProfileTeardownInvalidatesAllPoliciesAfterAllRuntimes(t *testing.T) {
	if testPool == nil || testHandler == nil {
		t.Skip("database not available")
	}
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	profile := insertRuntimeProfileFixture(t, t.Context(), "Policy profile "+uuid.NewString(), "claude", "claude")
	var ids []string
	var states []db.AgentFailoverState
	for range 2 {
		runtime := fx.Runtime(t, "Profile policy runtime", testutil.Cols{"profile_id": profile, "provider": "claude"})
		id := fx.Agent(t, "Profile policy agent "+uuid.NewString(), runtime, testutil.Cols{"archived_at": testutil.Raw("now()")})
		seedMutationProbe(t, fx, id, runtime, testWorkspaceID)
		ids = append(ids, id)
		states = append(states, mutationState(t, id))
	}
	req := withURLParams(newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/runtime-profiles/"+profile, nil), "id", testWorkspaceID, "profileId", profile)
	testutil.Call(t, testHandler.DeleteRuntimeProfile, req).Want(http.StatusNoContent)
	for i, id := range ids {
		assertMutationFenced(t, id, states[i], true)
	}
}
