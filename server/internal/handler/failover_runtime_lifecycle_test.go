package handler

import (
	"context"
	"math"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func fallbackConfigForMutation(t *testing.T, fx *testutil.Fixture, id, runtime string) {
	t.Helper()
	fx.InsertNoID(t, "agent_fallback_config", testutil.Cols{"agent_id": id, "workspace_id": testWorkspaceID, "runtime_id": runtime, "generation": uuid.NewString()}, "agent_id=$1", id)
}

func TestProfileEditsFencePrimaryAndFallbackReferences(t *testing.T) {
	for _, tc := range []struct {
		name               string
		body               map[string]any
		changed, suspended bool
	}{
		{"cosmetic", map[string]any{"description": "cosmetic"}, false, false},
		{"same command", map[string]any{"command_name": "claude"}, false, false},
		{"command", map[string]any{"command_name": "claude-wrapper"}, true, false},
		{"fixed args", map[string]any{"fixed_args": []string{"--test-option"}}, true, false},
		{"disable", map[string]any{"enabled": false}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx, id, runtime, old := probingAgentForMutation(t, nil)
			profile := insertRuntimeProfileFixture(t, t.Context(), "Profile mutation "+uuid.NewString(), "claude", "claude")
			fx.Exec(t, "UPDATE agent_runtime SET profile_id=$1 WHERE id=$2", profile, runtime)
			elsewhere := fx.Runtime(t, "Elsewhere primary", testutil.Cols{"provider": "claude"})
			other := fx.Agent(t, "Fallback dependent "+uuid.NewString(), elsewhere, nil)
			seedMutationProbe(t, fx, other, elsewhere, testWorkspaceID)
			fallbackConfigForMutation(t, fx, other, runtime)
			otherOld := mutationState(t, other)
			req := withURLParams(newRequest(http.MethodPatch, "/", tc.body), "id", testWorkspaceID, "profileId", profile)
			testutil.Call(t, testHandler.UpdateRuntimeProfile, req).Want(http.StatusOK)
			for _, pair := range []struct {
				id string
				s  db.AgentFailoverState
			}{{id, old}, {other, otherOld}} {
				if tc.changed {
					assertMutationFenced(t, pair.id, pair.s, tc.suspended)
				} else if !reflect.DeepEqual(mutationState(t, pair.id), pair.s) {
					t.Fatal("cosmetic/no-op profile edit changed availability")
				}
			}
		})
	}
}

func TestProfileInvalidationFailureRollsBackExecutionSettings(t *testing.T) {
	fx, id, runtime, _ := probingAgentForMutation(t, nil)
	profile := insertRuntimeProfileFixture(t, t.Context(), "Rollback profile "+uuid.NewString(), "claude", "claude")
	fx.Exec(t, "UPDATE agent_runtime SET profile_id=$1 WHERE id=$2", profile, runtime)
	fx.Exec(t, "UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2", int64(math.MaxInt64), id)
	old := mutationState(t, id)
	req := withURLParams(newRequest(http.MethodPatch, "/", map[string]any{"command_name": "changed-wrapper"}), "id", testWorkspaceID, "profileId", profile)
	testutil.Call(t, testHandler.UpdateRuntimeProfile, req).Want(http.StatusInternalServerError)
	p, err := testHandler.Queries.GetRuntimeProfile(t.Context(), parseUUID(profile))
	if err != nil || p.CommandName != "claude" || !reflect.DeepEqual(mutationState(t, id), old) {
		t.Fatalf("profile mutation escaped rollback: %v", err)
	}
}

func TestLegacyMergeFencesOldProbeAndRollsBackOnInvalidationFailure(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "merge", true: "rollback"}[rollback], func(t *testing.T) {
			fx, id, oldRuntime, _ := probingAgentForMutation(t, nil)
			target := fx.Runtime(t, "Merge target", testutil.Cols{"provider": "claude"})
			if rollback {
				fx.Exec(t, "UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2", int64(math.MaxInt64), id)
			}
			old := mutationState(t, id)
			task := fx.Task(t, id, testutil.Cols{"runtime_id": oldRuntime, "status": "queued"})
			err := testHandler.mergeLegacyRuntime(t.Context(), parseUUID(target), parseUUID(oldRuntime), "old-identity", "claude")
			if rollback {
				if err == nil {
					t.Fatal("expected failed invalidation")
				}
				a, e := testHandler.Queries.GetAgent(t.Context(), parseUUID(id))
				if e != nil || a.RuntimeID != parseUUID(oldRuntime) || !reflect.DeepEqual(mutationState(t, id), old) {
					t.Fatal("merge escaped rollback")
				}
				if !rowExists(t, "agent_runtime", oldRuntime) {
					t.Fatal("rollback deleted old runtime")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				assertMutationFenced(t, id, old, true)
				a, e := testHandler.Queries.GetAgent(t.Context(), parseUUID(id))
				if e != nil || a.RuntimeID != parseUUID(target) {
					t.Fatal("agent not reassigned")
				}
			}
			var actual string
			if err := testPool.QueryRow(t.Context(), "SELECT runtime_id::text FROM agent_task_queue WHERE id=$1", task).Scan(&actual); err != nil {
				t.Fatal(err)
			}
			expected := target
			if rollback {
				expected = oldRuntime
			}
			if actual != expected {
				t.Fatal("task reassignment not atomic")
			}
		})
	}
}

func TestRuntimeDeletionSuspendsFallbackReferenceElsewhere(t *testing.T) {
	fx, id, primary, old := probingAgentForMutation(t, nil)
	fallback := fx.Runtime(t, "Deleted fallback", testutil.Cols{"provider": "claude"})
	fallbackConfigForMutation(t, fx, id, fallback)
	req := withURLParam(newRequest(http.MethodDelete, "/api/runtimes/"+fallback, nil), "runtimeId", fallback)
	testutil.Call(t, testHandler.DeleteAgentRuntime, req).Want(http.StatusOK)
	assertMutationFenced(t, id, old, true)
	a, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(id))
	if err != nil || a.RuntimeID != parseUUID(primary) {
		t.Fatal("fallback deletion moved primary")
	}
	c, err := testHandler.Queries.GetAgentFallbackConfig(t.Context(), db.GetAgentFallbackConfigParams{AgentID: parseUUID(id), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil || c.RuntimeID != parseUUID(fallback) {
		t.Fatal("deleted fallback silently retargeted configuration")
	}
}

func TestTerminalCleanupRequiresDeletedOwnerAndRemovesAllForkRows(t *testing.T) {
	fx, id, runtime, old := probingAgentForMutation(t, nil)
	fallbackConfigForMutation(t, fx, id, runtime)
	task := fx.Task(t, id, testutil.Cols{"runtime_id": runtime, "status": "queued"})
	fx.InsertNoID(t, "agent_task_execution_route", testutil.Cols{"task_id": task, "agent_id": id, "workspace_id": testWorkspaceID, "runtime_id": runtime, "route": "primary", "primary_generation": uuid.NewString(), "state_version": 1, "fallback_generation": uuid.NewString()}, "task_id=$1", task)
	args := db.DeleteTerminalAgentFailoverDataParams{AgentID: parseUUID(id), WorkspaceID: parseUUID(testWorkspaceID)}
	if err := testHandler.Queries.DeleteTerminalAgentFailoverData(t.Context(), args); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mutationState(t, id), old) {
		t.Fatal("cleanup removed surviving owner's policy")
	}
	fx.Exec(t, "UPDATE agent SET kind='system', system_key='agent_builder:test' WHERE id=$1", id)
	req := withURLParam(newRequest(http.MethodDelete, "/api/runtimes/"+runtime, nil), "runtimeId", runtime)
	testutil.Call(t, testHandler.DeleteAgentRuntime, req).Want(http.StatusOK)
	for _, table := range []string{"agent_failover_state", "agent_fallback_config", "agent_task_execution_route"} {
		var count int
		if err := testPool.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE agent_id=$1", id).Scan(&count); err != nil || count != 0 {
			t.Fatalf("terminal cleanup left %s: %d %v", table, count, err)
		}
	}
}

func TestChatDeletionOnlyCleansTerminalBuilderPolicy(t *testing.T) {
	for _, builder := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "builder"}[builder], func(t *testing.T) {
			fx, id, runtime, old := probingAgentForMutation(t, nil)
			fallbackConfigForMutation(t, fx, id, runtime)
			if builder {
				fx.Exec(t, "UPDATE agent SET kind='system', system_key='agent_builder:test' WHERE id=$1", id)
			}
			session := fx.ChatSession(t, id)
			req := withURLParam(newRequest(http.MethodDelete, "/api/chat/sessions/"+session, nil), "sessionId", session)
			req = withChatTestWorkspaceCtx(t, req)
			testutil.Call(t, testHandler.DeleteChatSession, req).Want(http.StatusNoContent)
			if !builder {
				if !reflect.DeepEqual(mutationState(t, id), old) {
					t.Fatal("ordinary chat deletion changed policy")
				}
				return
			}
			for _, table := range []string{"agent_failover_state", "agent_fallback_config"} {
				var count int
				if err := testPool.QueryRow(t.Context(), "SELECT count(*) FROM "+table+" WHERE agent_id=$1", id).Scan(&count); err != nil || count != 0 {
					t.Fatalf("builder cleanup left %s: %d %v", table, count, err)
				}
			}
		})
	}
}

func TestRuntimeVisibilityInvalidatesPolicyAtomically(t *testing.T) {
	for _, kind := range []string{"rename", "no-op", "visibility", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			fx, id, runtime, old := probingAgentForMutation(t, nil)
			fx.Exec(t, "UPDATE agent_runtime SET visibility='private',owner_id=$1 WHERE id=$2", testUserID, runtime)
			body := map[string]any{"visibility": "public"}
			switch kind {
			case "rename":
				body = map[string]any{"custom_name": "Renamed runtime"}
			case "no-op":
				body = map[string]any{"visibility": "private"}
			case "rollback":
				fx.Exec(t, "UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2", int64(math.MaxInt64), id)
				old = mutationState(t, id)
			}
			req := withURLParam(newRequest(http.MethodPatch, "/api/runtimes/"+runtime, body), "runtimeId", runtime)
			status := http.StatusOK
			if kind == "rollback" {
				status = http.StatusInternalServerError
			}
			testutil.Call(t, testHandler.UpdateAgentRuntime, req).Want(status)
			if kind == "visibility" {
				assertMutationFenced(t, id, old, false)
			} else if !reflect.DeepEqual(mutationState(t, id), old) {
				t.Fatal("cosmetic/no-op/failed visibility update changed policy")
			}
			if kind == "rollback" {
				rt, err := testHandler.Queries.GetAgentRuntime(t.Context(), parseUUID(runtime))
				if err != nil || rt.Visibility != "private" {
					t.Fatal("visibility escaped rollback")
				}
			}
		})
	}
}

func TestProfileDeletionDeduplicatesBothRuntimeReferences(t *testing.T) {
	fx, id, primary, old := probingAgentForMutation(t, testutil.Cols{"archived_at": testutil.Raw("now()")})
	profile := insertRuntimeProfileFixture(t, t.Context(), "Deduplicated profile "+uuid.NewString(), "claude", "claude")
	fallback := fx.Runtime(t, "Second profile runtime", testutil.Cols{"provider": "claude", "profile_id": profile})
	fx.Exec(t, "UPDATE agent_runtime SET profile_id=$1 WHERE id=$2", profile, primary)
	fallbackConfigForMutation(t, fx, id, fallback)
	req := withURLParams(newRequest(http.MethodDelete, "/", nil), "id", testWorkspaceID, "profileId", profile)
	testutil.Call(t, testHandler.DeleteRuntimeProfile, req).Want(http.StatusNoContent)
	assertMutationFenced(t, id, old, true)
	if s := mutationState(t, id); s.Version != old.Version+2 {
		t.Fatalf("duplicate invalidation: version=%d, want %d", s.Version, old.Version+2)
	}
}

func TestRuntimeCleanupRollsBackWithUserPolicyFailure(t *testing.T) {
	fx, id, runtime, _ := probingAgentForMutation(t, testutil.Cols{"archived_at": testutil.Raw("now()")})
	fx.Exec(t, "UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2", int64(math.MaxInt64), id)
	system := fx.Agent(t, "Rollback system "+uuid.NewString(), runtime, testutil.Cols{"kind": "system", "system_key": "agent_builder:test"})
	seedMutationProbe(t, fx, system, runtime, testWorkspaceID)
	fallbackConfigForMutation(t, fx, system, runtime)
	before := mutationState(t, system)
	req := withURLParam(newRequest(http.MethodDelete, "/", nil), "runtimeId", runtime)
	testutil.Call(t, testHandler.DeleteAgentRuntime, req).Want(http.StatusInternalServerError)
	if !rowExists(t, "agent", system) || !rowExists(t, "agent_runtime", runtime) || !reflect.DeepEqual(mutationState(t, system), before) {
		t.Fatal("terminal cleanup escaped rollback")
	}
	if _, err := testHandler.Queries.GetAgentFallbackConfig(t.Context(), db.GetAgentFallbackConfigParams{AgentID: parseUUID(system), WorkspaceID: parseUUID(testWorkspaceID)}); err != nil {
		t.Fatal("rollback lost system config", err)
	}
}

func TestCrossReferencedRuntimePoliciesLockAgentsInOneOrder(t *testing.T) {
	fx, first, r1, old1 := probingAgentForMutation(t, nil)
	r2 := fx.Runtime(t, "Cross reference", testutil.Cols{"provider": "claude", "visibility": "private", "owner_id": testUserID})
	fx.Exec(t, "UPDATE agent_runtime SET visibility='private',owner_id=$1 WHERE id=$2", testUserID, r1)
	second := fx.Agent(t, "Cross referenced Agent "+uuid.NewString(), r2, nil)
	seedMutationProbe(t, fx, second, r2, testWorkspaceID)
	old2 := mutationState(t, second)
	fallbackConfigForMutation(t, fx, first, r2)
	fallbackConfigForMutation(t, fx, second, r1)
	type result struct {
		runtime  string
		response *testutil.Response
	}
	results := make(chan result, 2)
	change := func(runtime string) *testutil.Response {
		req := withURLParam(newRequest(http.MethodPatch, "/", map[string]any{"visibility": "public"}), "runtimeId", runtime)
		ctx, cancel := context.WithTimeout(req.Context(), 10*time.Second)
		defer cancel()
		return testutil.Call(t, testHandler.UpdateAgentRuntime, req.WithContext(ctx))
	}
	var wg sync.WaitGroup
	for _, runtime := range []string{r1, r2} {
		wg.Go(func() { results <- result{runtime, change(runtime)} })
	}
	wg.Wait()
	close(results)
	var busy []string
	succeeded := int64(0)
	for result := range results {
		if result.response.Code == http.StatusConflict {
			var body struct {
				Code string `json:"code"`
			}
			result.response.JSON(&body)
			if body.Code != "runtime_access_revocation_busy" {
				t.Fatalf("unexpected conflict: %s", body.Code)
			}
			busy = append(busy, result.runtime)
		} else {
			result.response.Want(http.StatusOK)
			succeeded++
		}
	}
	if succeeded == 0 {
		t.Fatal("neither ordered Agent acquisition made progress")
	}
	if mutationState(t, first).Version != old1.Version+succeeded || mutationState(t, second).Version != old2.Version+succeeded {
		t.Fatal("busy transition committed partial invalidation")
	}
	// NOWAIT deliberately replaces the old wait-to-success contract. Retry
	// only identified busy operations after both writers have released locks.
	for _, runtime := range busy {
		rt, err := testHandler.Queries.GetAgentRuntime(t.Context(), parseUUID(runtime))
		if err != nil || rt.Visibility != "private" {
			t.Fatal("busy transition committed visibility")
		}
		change(runtime).Want(http.StatusOK)
	}
	for _, pair := range []struct {
		id string
		s  db.AgentFailoverState
	}{{first, old1}, {second, old2}} {
		assertMutationFenced(t, pair.id, pair.s, false)
		if current := mutationState(t, pair.id); current.Version != pair.s.Version+2 {
			t.Fatal("lost cross-runtime invalidation")
		}
	}
}
