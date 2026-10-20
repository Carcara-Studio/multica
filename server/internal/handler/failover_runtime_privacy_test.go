package handler

import (
	"context"
	"math"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Fallback-only references do not appear in upstream's primary binding plan.
func TestFailoverRuntimePrivacyIsAtomicAcrossAllReferences(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		for _, rollback := range []bool{false, true} {
			name := "direct"
			if confirmed {
				name = "confirmed"
			}
			if rollback {
				name += " rollback"
			}
			t.Run(name, func(t *testing.T) {
				fx, ownerAgent, runtime, _ := probingAgentForMutation(t, nil)
				fx.Exec(t, "UPDATE agent_runtime SET visibility='public',owner_id=$1 WHERE id=$2", testUserID, runtime)
				other := fx.User(t, "Privacy member", uuid.NewString()+"@example.test")
				fx.Member(t, testWorkspaceID, other, "member")
				primary := fx.Runtime(t, "Independent primary", testutil.Cols{"owner_id": other})
				fallbackAgent := fx.Agent(t, "Fallback privacy", primary, testutil.Cols{"owner_id": other})
				fallbackConfigForMutation(t, fx, fallbackAgent, runtime)
				seedMutationProbe(t, fx, fallbackAgent, primary, testWorkspaceID)
				fx.Exec(t, `UPDATE agent_failover_state SET evidence_context='{"version":1}'::jsonb,evidence_token=$1,evidence_issued_at=now(),evidence_expires_at=now()+interval '1 minute' WHERE agent_id IN ($2,$3)`, uuid.NewString(), ownerAgent, fallbackAgent)
				ids := []string{ownerAgent, fallbackAgent}
				suspended := []bool{false, true}
				foreignAgent, task := "", ""
				if confirmed {
					foreignAgent = fx.Agent(t, "Revoked primary", runtime, testutil.Cols{"owner_id": other})
					seedMutationProbe(t, fx, foreignAgent, runtime, testWorkspaceID)
					task = fx.Task(t, foreignAgent, testutil.Cols{"runtime_id": runtime, "status": "running"})
					ids = append(ids, foreignAgent)
					suspended = append(suspended, true)
				}
				if rollback {
					fx.Exec(t, "UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2", int64(math.MaxInt64), fallbackAgent)
				}
				old := make([]db.AgentFailoverState, len(ids))
				for i, id := range ids {
					old[i] = mutationState(t, id)
				}
				want := http.StatusOK
				if rollback {
					want = http.StatusInternalServerError
				}
				if confirmed {
					req := withURLParam(newRequest(http.MethodPost, "/api/runtimes/"+runtime+"/revoke-workspace-access", map[string]any{"expected_nonowner_agent_ids": []string{foreignAgent}, "expected_task_ids": []string{task}}), "runtimeId", runtime)
					testutil.Call(t, testHandler.RevokeRuntimeWorkspaceAccess, req).Want(want)
				} else {
					req := withURLParam(newRequest(http.MethodPatch, "/api/runtimes/"+runtime, map[string]any{"visibility": "private"}), "runtimeId", runtime)
					testutil.Call(t, testHandler.UpdateAgentRuntime, req).Want(want)
				}
				rt, err := testHandler.Queries.GetAgentRuntime(t.Context(), parseUUID(runtime))
				if err != nil {
					t.Fatal(err)
				}
				visibility := "private"
				if rollback {
					visibility = "public"
				}
				if rt.Visibility != visibility {
					t.Fatal("privacy update escaped transaction")
				}
				for i, id := range ids {
					state := mutationState(t, id)
					if rollback {
						if !reflect.DeepEqual(state, old[i]) {
							t.Fatal("failed privacy transition changed policy")
						}
					} else if state.PrimaryGeneration == old[i].PrimaryGeneration || state.ProbeToken.Valid || state.EvidenceToken.Valid || state.EvidenceContext != nil || state.SuspendedAt.Valid != suspended[i] {
						t.Fatal("privacy retained authority or suspended wrong owner")
					}
				}
				a, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(fallbackAgent))
				if err != nil || a.RuntimeID != parseUUID(primary) {
					t.Fatal("fallback reference changed primary binding")
				}
				if confirmed {
					a, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(foreignAgent))
					if err != nil || a.RuntimeID.Valid != rollback {
						t.Fatal("revocation binding was not atomic")
					}
					var status string
					fx.QueryRow(t, "SELECT status FROM agent_task_queue WHERE id=$1", task).Scan(&status)
					expected := "cancelled"
					if rollback {
						expected = "running"
					}
					if status != expected {
						t.Fatalf("task status=%s, want %s", status, expected)
					}
				}
			})
		}
	}
}

func TestFailoverRuntimePrivacyDoesNotWaitOnFallbackAgent(t *testing.T) {
	fx, id, primary, old := probingAgentForMutation(t, nil)
	runtime := fx.Runtime(t, "Privacy fallback", testutil.Cols{"visibility": "public"})
	fallbackConfigForMutation(t, fx, id, runtime)
	tx, err := testPool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), "SELECT id FROM agent WHERE id=$1 FOR UPDATE", id); err != nil {
		t.Fatal(err)
	}
	req := withURLParam(newRequest(http.MethodPatch, "/api/runtimes/"+runtime, map[string]any{"visibility": "private"}), "runtimeId", runtime)
	ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
	defer cancel()
	var response struct {
		Code string `json:"code"`
	}
	testutil.Call(t, testHandler.UpdateAgentRuntime, req.WithContext(ctx)).Want(http.StatusConflict).JSON(&response)
	if response.Code != "runtime_access_revocation_busy" {
		t.Fatalf("busy code=%s", response.Code)
	}
	rt, err := testHandler.Queries.GetAgentRuntime(t.Context(), parseUUID(runtime))
	if err != nil || rt.Visibility != "public" || !reflect.DeepEqual(mutationState(t, id), old) {
		t.Fatal("busy transition changed visibility/policy")
	}
	a, err := testHandler.Queries.GetAgent(t.Context(), parseUUID(id))
	if err != nil || a.RuntimeID != parseUUID(primary) {
		t.Fatal("busy transition changed binding")
	}
}
