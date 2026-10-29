package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/failover"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func fallbackAPIFixture(t *testing.T) (string, SaveAgentFallbackRequest) {
	t.Helper()
	primary := dbfx.Runtime(t, "Fallback API primary", testutil.Cols{"provider": "codex", "visibility": "public"})
	runtime := dbfx.Runtime(t, "Fallback API target", testutil.Cols{"provider": "codex", "visibility": "public"})
	id := dbfx.Agent(t, "Fallback API", primary, testutil.Cols{"kind": "user", "visibility": "workspace"})
	dbfx.Cleanup(t, "DELETE FROM agent_failover_state WHERE agent_id=$1", id)
	dbfx.Cleanup(t, "DELETE FROM agent_fallback_config WHERE agent_id=$1", id)
	dbfx.Cleanup(t, "DELETE FROM activity_log WHERE details->>'agent_id'=$1", id)
	return id, SaveAgentFallbackRequest{Mode: "native_login_path", RuntimeID: runtime, Model: "model-pending", ExpectedRevision: &FallbackRevision{}, Overrides: &failover.ExecutionOverrides{
		Version: 1, CustomEnv: map[string]string{"TOKEN": "secret-env-value"}, CustomArgs: []string{"--secret-argument"},
		RuntimeConfig: json.RawMessage(`{"setting":"secret-runtime-value"}`), MCPConfig: json.RawMessage(`{"headers":{"Authorization":"secret-mcp-value"}}`),
	}}
}

func fallbackAPIRequest(id, method string, body any) *http.Request {
	return withURLParam(newRequest(method, "/api/agents/"+id+"/fallback", body), "id", id)
}

func TestAgentFallbackAPILifecycleCASRedactionAndRecreation(t *testing.T) {
	id, body := fallbackAPIFixture(t)
	var empty, saved, updated, removed, recreated AgentFallbackResponse
	testutil.Call(t, testHandler.GetAgentFallback, fallbackAPIRequest(id, http.MethodGet, nil)).Want(200).JSON(&empty)
	if empty.Configured || empty.ExecutionEnabled || empty.Configuration != nil || empty.Revision != (FallbackRevision{}) {
		t.Fatalf("unexpected unconfigured state: %+v", empty)
	}
	response := testutil.Call(t, testHandler.SaveAgentFallback, fallbackAPIRequest(id, http.MethodPut, body)).Want(200).JSON(&saved)
	if !saved.Configured || saved.ExecutionEnabled || saved.Configuration == nil || saved.Configuration.Mode != "native_login_path" || saved.Revision.Generation == "" || saved.Revision.StateVersion <= 0 {
		t.Fatalf("invalid inactive receipt: %+v", saved)
	}
	for _, secret := range []string{"secret-env-value", "--secret-argument", "secret-runtime-value", "secret-mcp-value", "custom_env", "custom_args", "execution_config"} {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("response leaked %s", secret)
		}
	}
	if len(saved.Configuration.EnvironmentKeys) != 1 || saved.Configuration.EnvironmentKeys[0] != "TOKEN" || saved.Configuration.ArgumentCount != 1 || !saved.Configuration.HasMCPConfig || !saved.Configuration.HasRuntimeConfig {
		t.Fatalf("redacted metadata missing: %+v", saved.Configuration)
	}
	testutil.Call(t, testHandler.SaveAgentFallback, fallbackAPIRequest(id, http.MethodPut, body)).Want(409)
	body.ExpectedRevision, body.Model = &saved.Revision, "model-updated"
	testutil.Call(t, testHandler.SaveAgentFallback, fallbackAPIRequest(id, http.MethodPut, body)).Want(200).JSON(&updated)
	if updated.Revision.Generation == saved.Revision.Generation || updated.Revision.StateVersion <= saved.Revision.StateVersion {
		t.Fatal("update did not rotate the revision")
	}
	testutil.Call(t, testHandler.DeleteAgentFallback, fallbackAPIRequest(id, http.MethodDelete, DeleteAgentFallbackRequest{&saved.Revision})).Want(409)
	testutil.Call(t, testHandler.DeleteAgentFallback, fallbackAPIRequest(id, http.MethodDelete, DeleteAgentFallbackRequest{&updated.Revision})).Want(204)
	testutil.Call(t, testHandler.GetAgentFallback, fallbackAPIRequest(id, http.MethodGet, nil)).Want(200).JSON(&removed)
	if removed.Configured || removed.ExecutionEnabled || removed.Configuration != nil || removed.Revision.Generation != "" || removed.Revision.PrimaryGeneration == "" || removed.Revision.StateVersion <= updated.Revision.StateVersion {
		t.Fatalf("state-only revision was lost: %+v", removed)
	}
	body.ExpectedRevision = &removed.Revision
	testutil.Call(t, testHandler.SaveAgentFallback, fallbackAPIRequest(id, http.MethodPut, body)).Want(200).JSON(&recreated)
	if !recreated.Configured || recreated.ExecutionEnabled {
		t.Fatal("recreation failed or enabled execution")
	}
	var count int
	if err := testPool.QueryRow(t.Context(), "SELECT count(*) FROM agent_task_queue WHERE agent_id=$1", id).Scan(&count); err != nil || count != 0 {
		t.Fatalf("management created work: count=%d err=%v", count, err)
	}
	rows, err := testPool.Query(t.Context(), "SELECT details::text FROM activity_log WHERE details->>'agent_id'=$1", id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	audits := 0
	for rows.Next() {
		var details string
		if err := rows.Scan(&details); err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"secret-env-value", "--secret-argument", "secret-runtime-value", "secret-mcp-value"} {
			if strings.Contains(details, secret) {
				t.Fatal("audit exposed execution values")
			}
		}
		audits++
	}
	if rows.Err() != nil || audits != 4 {
		t.Fatalf("audit count=%d err=%v", audits, rows.Err())
	}
}

func TestAgentFallbackAPIAuthorization(t *testing.T) {
	id, body := fallbackAPIFixture(t)
	member := dbfx.User(t, "Fallback owner", uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	dbfx.Exec(t, "UPDATE agent SET owner_id=$1 WHERE id=$2", member, id)
	var saved AgentFallbackResponse
	req := fallbackAPIRequest(id, http.MethodPut, body)
	req.Header.Set("X-User-ID", member)
	testutil.Call(t, testHandler.SaveAgentFallback, req).Want(200).JSON(&saved)
	admin := dbfx.User(t, "Fallback admin", uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, admin, "admin")
	req = fallbackAPIRequest(id, http.MethodGet, nil)
	req.Header.Set("X-User-ID", admin)
	testutil.Call(t, testHandler.GetAgentFallback, req).Want(200)
	other := dbfx.User(t, "Fallback other", uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, other, "member")
	body.ExpectedRevision = &saved.Revision
	host := dbfx.Agent(t, "Fallback actor", testRuntimeID)
	task := dbfx.Task(t, host, testutil.Cols{"runtime_id": testRuntimeID})
	for _, action := range []struct {
		method  string
		handler http.HandlerFunc
		body    any
	}{
		{http.MethodGet, testHandler.GetAgentFallback, nil},
		{http.MethodPut, testHandler.SaveAgentFallback, body},
		{http.MethodDelete, testHandler.DeleteAgentFallback, DeleteAgentFallbackRequest{&saved.Revision}},
	} {
		for _, actor := range []string{"other_member", "agent", "daemon"} {
			t.Run(action.method+"/"+actor, func(t *testing.T) {
				req := fallbackAPIRequest(id, action.method, action.body)
				switch actor {
				case "other_member":
					req.Header.Set("X-User-ID", other)
				case "agent":
					req.Header.Set("X-Agent-ID", host)
					req.Header.Set("X-Task-ID", task)
				case "daemon":
					req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, uuid.NewString()))
				}
				testutil.Call(t, action.handler, req).Want(403)
			})
		}
	}
}

func TestAgentFallbackAPIRejectsInvalidIntentAndPrivateRuntime(t *testing.T) {
	id, valid := fallbackAPIFixture(t)
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		field string
		value any
	}{
		{"mode", "strict"}, {"mode", "future"}, {"runtime_id", "invalid"}, {"model", " implicit "},
		{"expected_revision", nil}, {"expected_revision", map[string]any{"state_version": 1}},
		{"expected_revision", map[string]any{"generation": uuid.NewString(), "primary_generation": uuid.NewString(), "state_version": -1}},
		{"overrides", map[string]any{"version": 2}}, {"owner_id", testUserID}, {"catalog", map[string]any{}}, {"execution_enabled", true},
	} {
		t.Run(change.field+"/"+stringMustJSON(t, change.value), func(t *testing.T) {
			var body map[string]any
			if err := json.Unmarshal(encoded, &body); err != nil {
				t.Fatal(err)
			}
			body[change.field] = change.value
			testutil.Call(t, testHandler.SaveAgentFallback, fallbackAPIRequest(id, http.MethodPut, body)).Want(400)
		})
	}
	for _, raw := range []string{"null", string(encoded) + " {}", `{"mode":"` + strings.Repeat("x", 1<<20) + `"}`} {
		req := withURLParam(httptest.NewRequest(http.MethodPut, "/api/agents/"+id+"/fallback", strings.NewReader(raw)), "id", id)
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		testutil.Call(t, testHandler.SaveAgentFallback, req).Want(400)
	}
	other := dbfx.User(t, "Private runtime owner", uuid.NewString()+"@example.test")
	private := dbfx.Runtime(t, "Private fallback", testutil.Cols{"provider": "codex", "visibility": "private", "owner_id": other})
	valid.RuntimeID = private
	testutil.Call(t, testHandler.SaveAgentFallback, fallbackAPIRequest(id, http.MethodPut, valid)).Want(400)
}

func stringMustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
