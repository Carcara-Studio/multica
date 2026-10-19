package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-redis/redismock/v9"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestDaemonWebSocketHumanClassificationUsesAuthenticationProvenance(t *testing.T) {
	for _, kind := range []string{"jwt", "pat"} {
		t.Run(kind, func(t *testing.T) {
			userID := uuid.NewString()
			var cache *auth.PATCache
			var token string
			if kind == "jwt" {
				var err error
				token, err = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": userID, "exp": time.Now().Add(time.Minute).Unix()}).SignedString(auth.JWTSecret())
				if err != nil {
					t.Fatal(err)
				}
			} else {
				token = "mul_ws_identity_fixture"
				client, mock := redismock.NewClientMock()
				t.Cleanup(func() { _ = client.Close() })
				mock.ExpectGet("mul:auth:pat:" + auth.HashToken(token)).SetVal(userID)
				cache = auth.NewPATCache(client)
				t.Cleanup(func() {
					if err := mock.ExpectationsWereMet(); err != nil {
						t.Error(err)
					}
				})
			}
			called := false
			endpoint := middleware.DaemonAuth(nil, cache, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				identity, ok := (&Handler{}).buildDaemonWebSocketIdentity(w, r, nil, requestUserID(r))
				if !ok || !identity.HumanActor || identity.UserID != userID {
					t.Error("authenticated human provenance was lost")
				}
				// A server-stamped machine actor must remain a machine even
				// when the token carries a valid human owner's user ID.
				r.Header.Set("X-Actor-Source", "cloud_pat")
				identity, ok = (&Handler{}).buildDaemonWebSocketIdentity(w, r, nil, requestUserID(r))
				if !ok || identity.HumanActor {
					t.Error("machine actor inherited owner management authority")
				}
			}))
			req := httptest.NewRequest(http.MethodGet, "/api/daemon/ws", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("X-Human-Actor", "true")
			w := httptest.NewRecorder()
			endpoint.ServeHTTP(w, req)
			if !called {
				t.Fatal("authentication did not reach identity builder", w.Code)
			}
		})
	}
	for _, daemon := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/api/daemon/ws", nil)
		req.Header.Set("X-Human-Actor", "true")
		if daemon {
			req = req.WithContext(middleware.WithDaemonContext(req.Context(), uuid.NewString(), "daemon-token"))
		}
		if daemonWebSocketHumanActor(req) {
			t.Fatal("missing/daemon auth provenance gained human authority")
		}
	}
}

func TestDaemonWebSocketMissingHubIsInternalError(t *testing.T) {
	h := &Handler{}
	w := httptest.NewRecorder()
	h.DaemonWebSocket(w, httptest.NewRequest(http.MethodGet, "/api/daemon/ws", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["code"] != "daemon_websocket_misconfigured" {
		t.Fatalf("code = %q, want daemon_websocket_misconfigured", body["code"])
	}
}

func TestBuildDaemonWebSocketIdentitySeedsBatchRuntimeLeases(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const daemonID = "lease-daemon"
	runtimeIDs := []string{
		dbfx.Runtime(t, "WS lease runtime 1", testutil.Cols{
			"workspace_id": testWorkspaceID,
			"daemon_id":    daemonID,
			"provider":     "ws-lease-1",
			"device_info":  "WS lease runtime 1",
		}),
		dbfx.Runtime(t, "WS lease runtime 2", testutil.Cols{
			"workspace_id": testWorkspaceID,
			"daemon_id":    daemonID,
			"provider":     "ws-lease-2",
			"device_info":  "WS lease runtime 2",
		}),
	}
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, daemonID)
	req.Header.Set("X-Client-Version", "0.9.0")
	w := httptest.NewRecorder()

	identity, ok := testHandler.buildDaemonWebSocketIdentity(w, req, runtimeIDs, "")
	if !ok {
		t.Fatalf("buildDaemonWebSocketIdentity rejected valid runtimes: %d %s", w.Code, w.Body.String())
	}
	if identity.DaemonID != daemonID || identity.WorkspaceID != testWorkspaceID {
		t.Fatalf("identity scope = daemon %q workspace %q", identity.DaemonID, identity.WorkspaceID)
	}
	if identity.ClientVersion != "0.9.0" {
		t.Fatalf("client version = %q, want 0.9.0", identity.ClientVersion)
	}
	if len(identity.RuntimeLeases) != len(runtimeIDs) {
		t.Fatalf("runtime leases = %d, want %d", len(identity.RuntimeLeases), len(runtimeIDs))
	}
	for _, runtimeID := range runtimeIDs {
		lease := identity.RuntimeLeases[runtimeID]
		if lease == nil {
			t.Fatalf("missing lease for runtime %s", runtimeID)
		}
		state := lease.Snapshot()
		if state.WorkspaceID != testWorkspaceID || state.Status != "online" || !state.LastSeenAtValid {
			t.Fatalf("lease %s = %+v", runtimeID, state)
		}
		if time.Since(state.LastSeenAt) > time.Minute {
			t.Fatalf("lease %s last_seen_at unexpectedly stale: %s", runtimeID, state.LastSeenAt)
		}
	}
}

func TestBuildDaemonWebSocketIdentityFailsClosedForMissingRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, "lease-daemon")
	w := httptest.NewRecorder()

	if _, ok := testHandler.buildDaemonWebSocketIdentity(w, req, []string{uuid.NewString()}, ""); ok {
		t.Fatal("missing runtime unexpectedly authorized")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestBuildDaemonWebSocketIdentityRejectsWrongDaemon(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "WS lease daemon scope", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "owner-daemon",
		"device_info":  "WS lease daemon scope",
	})
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, "other-daemon")
	w := httptest.NewRecorder()

	if _, ok := testHandler.buildDaemonWebSocketIdentity(w, req, []string{runtimeID}, ""); ok {
		t.Fatal("runtime owned by another daemon unexpectedly authorized")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}

func TestBuildDaemonWebSocketIdentityRejectsCrossWorkspaceRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	workspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "WS Lease Other Workspace",
		"slug":         "ws-lease-" + uuid.NewString(),
		"description":  "Cross-workspace lease test",
		"issue_prefix": "HWL",
	})
	runtimeID := dbfx.Runtime(t, "WS lease cross workspace", testutil.Cols{
		"workspace_id": workspaceID,
		"daemon_id":    "lease-daemon",
		"device_info":  "WS lease cross workspace",
	})
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/ws", nil, testWorkspaceID, "lease-daemon")
	w := httptest.NewRecorder()

	if _, ok := testHandler.buildDaemonWebSocketIdentity(w, req, []string{runtimeID}, ""); ok {
		t.Fatal("cross-workspace runtime unexpectedly authorized")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", w.Code, w.Body.String())
	}
}
