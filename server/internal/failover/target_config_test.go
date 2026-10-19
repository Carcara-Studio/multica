package failover

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func managementFixture(t *testing.T) (*stateFixture, ConfigManager, ManagementActor, SaveTargetConfig) {
	t.Helper()
	f := newStateFixture(t)
	runtime := testUUID(f.fx.Runtime(t, "Protected fallback", testutil.Cols{"provider": "claude"}))
	f.fx.Cleanup(t, `DELETE FROM agent_fallback_config WHERE agent_id=$1`, f.row.AgentID)
	f.fx.Cleanup(t, `DELETE FROM agent_task_execution_route WHERE agent_id=$1`, f.row.AgentID)
	f.fx.Cleanup(t, `DELETE FROM activity_log WHERE workspace_id=$1`, f.row.WorkspaceID)
	m := ConfigManager{Queries: f.q, Transactions: f.pool}
	actor := ManagementActor{Kind: "member", UserID: testUUID(f.fx.UserID), WorkspaceID: f.row.WorkspaceID}
	in := SaveTargetConfig{AgentID: f.row.AgentID, RuntimeID: runtime, Expected: ConfigRevision{PrimaryGeneration: f.row.PrimaryGeneration, StateVersion: f.row.Version}, Overrides: ExecutionOverrides{Version: 1,
		CustomEnv: map[string]string{"FALLBACK_KEY": "env-private-value"}, CustomArgs: []string{"--token=argument-private-value"}, RuntimeConfig: json.RawMessage(`{"gatewayToken":"runtime-private-value"}`), MCPConfig: json.RawMessage(`{"mcpServers":{"example":{"env":{"TOKEN":"mcp-private-value"}}}}`)}}
	return f, m, actor, in
}
func auditCount(t *testing.T, f *stateFixture) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM activity_log WHERE workspace_id=$1 AND action LIKE 'agent_fallback_%'`, f.row.WorkspaceID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func saveManaged(t *testing.T, m ConfigManager, a ManagementActor, in SaveTargetConfig) ConfigView {
	t.Helper()
	v, err := m.Save(t.Context(), a, in, agent.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestProtectedTargetSaveInspectRevealAndRemoval(t *testing.T) {
	f, m, a, in := managementFixture(t)
	f.fx.Exec(t, `UPDATE agent SET custom_env='{"PRIMARY":"primary-private-value"}',custom_args='["primary-argument-private-value"]',runtime_config='{"token":"primary-runtime-private-value"}' WHERE id=$1`, in.AgentID)
	before, err := f.q.GetAgent(t.Context(), in.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	v := saveManaged(t, m, a, in)
	if !v.OverridesStored || !reflect.DeepEqual(v.EnvironmentKeys, []string{"FALLBACK_KEY"}) || v.ArgumentCount != 1 {
		t.Fatal("redacted projection missing metadata")
	}
	inspected, err := m.Inspect(t.Context(), a, in.AgentID)
	if err != nil || !reflect.DeepEqual(inspected, v) {
		t.Fatal("inspect mismatch", err)
	}
	viewJSON, _ := json.Marshal(inspected)
	for _, secret := range []string{"env-private-value", "argument-private-value", "runtime-private-value", "mcp-private-value", "primary-private-value"} {
		if strings.Contains(string(viewJSON), secret) {
			t.Fatal("redacted inspection leaked a value")
		}
	}
	revealed, err := m.Reveal(t.Context(), a, in.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if revealed.CustomEnv["FALLBACK_KEY"] != "env-private-value" || len(revealed.CustomEnv) != 1 {
		t.Fatal("fallback inherited primary credentials")
	}
	if auditCount(t, f) != 2 {
		t.Fatal("update/reveal audit missing or inspection audited as reveal")
	}
	var raw string
	if err := f.pool.QueryRow(t.Context(), `SELECT string_agg(details::text,' ') FROM activity_log WHERE workspace_id=$1 AND action LIKE 'agent_fallback_%'`, a.WorkspaceID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "private-value") || strings.Contains(raw, "gatewayToken") || strings.Contains(raw, "--token") {
		t.Fatal("activity log leaked secret values/options")
	}
	after, err := f.q.GetAgent(t.Context(), in.AgentID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("fallback management mutated primary Agent")
	}
	c := storedConfig(t, f.q, db.AgentFallbackConfig{AgentID: in.AgentID, WorkspaceID: a.WorkspaceID})
	f.row = storedState(t, f.q, f)
	task := targetTask(t, f, f.runtime)
	proof, err := recordRoute(t.Context(), f.q, f, c, task, "primary")
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Remove(t.Context(), a, in.AgentID, v.Revision); err != nil {
		t.Fatal(err)
	}
	state := storedState(t, f.q, f)
	if !state.SuspendedAt.Valid || state.PrimaryGeneration == v.Revision.PrimaryGeneration {
		t.Fatal("removal did not suspend/invalidate")
	}
	saved, err := f.q.GetAgentTaskExecutionRoute(t.Context(), db.GetAgentTaskExecutionRouteParams{TaskID: task, WorkspaceID: a.WorkspaceID})
	if err != nil || saved != proof {
		t.Fatal("removal erased queued route evidence")
	}
	if _, err = m.Reveal(t.Context(), a, in.AgentID); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatal("removed config remained revealable")
	}
}

func TestProtectedTargetSentinelsDoNotCrossRuntimeIdentity(t *testing.T) {
	f, m, a, in := managementFixture(t)
	v := saveManaged(t, m, a, in)
	in.Expected = v.Revision
	in.Overrides.CustomEnv = map[string]string{"FALLBACK_KEY": "****", "MISSING": "****"}
	v = saveManaged(t, m, a, in)
	o, err := m.Reveal(t.Context(), a, in.AgentID)
	if err != nil || o.CustomEnv["FALLBACK_KEY"] != "env-private-value" || len(o.CustomEnv) != 1 {
		t.Fatal("same-runtime mask merge failed")
	}
	in.Expected = v.Revision
	in.RuntimeID = testUUID(f.fx.Runtime(t, "Other account target", testutil.Cols{"provider": "claude"}))
	v = saveManaged(t, m, a, in)
	o, err = m.Reveal(t.Context(), a, in.AgentID)
	if err != nil || len(o.CustomEnv) != 0 {
		t.Fatal("mask moved old credential to new runtime")
	}
	if len(v.EnvironmentKeys) != 0 {
		t.Fatal("new target retained masked key")
	}
}

func TestProtectedTargetAuthorizationAndBinding(t *testing.T) {
	// Actor rejection must precede transactions/account/database access.
	if _, err := (ConfigManager{}).Reveal(t.Context(), ManagementActor{Kind: "agent", UserID: newGeneration(), WorkspaceID: newGeneration()}, newGeneration()); !errors.Is(err, ErrTargetAccess) {
		t.Fatal("Agent actor gained management authority")
	}
	f, m, a, in := managementFixture(t)
	v := saveManaged(t, m, a, in)
	outsider := f.fx.User(t, "Other member", "other-managed@example.test")
	f.fx.Member(t, f.fx.WorkspaceID, outsider, "member")
	denied := a
	denied.UserID = testUUID(outsider)
	for _, actor := range []ManagementActor{denied, {Kind: "agent", UserID: a.UserID, WorkspaceID: a.WorkspaceID}, {Kind: "member", UserID: a.UserID, WorkspaceID: newGeneration()}} {
		if _, err := m.Inspect(t.Context(), actor, in.AgentID); !errors.Is(err, ErrTargetAccess) {
			t.Fatal("unauthorized inspection", err)
		}
		if _, err := m.Reveal(t.Context(), actor, in.AgentID); !errors.Is(err, ErrTargetAccess) {
			t.Fatal("unauthorized reveal", err)
		}
		in.Expected = v.Revision
		if _, err := m.Save(t.Context(), actor, in, agent.Catalog{}); !errors.Is(err, ErrTargetAccess) {
			t.Fatal("unauthorized save", err)
		}
		if err := m.Remove(t.Context(), actor, in.AgentID, v.Revision); !errors.Is(err, ErrTargetAccess) {
			t.Fatal("unauthorized removal", err)
		}
	}
	if auditCount(t, f) != 1 {
		t.Fatal("denied operation wrote audit")
	}
	// Same ownership allowance as upstream env: human Agent owner may be member.
	f.fx.Exec(t, `UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2`, a.WorkspaceID, a.UserID)
	if _, err := m.Reveal(t.Context(), a, in.AgentID); err != nil {
		t.Fatal("Agent owner denied", err)
	}
	f.fx.Exec(t, `UPDATE member SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, a.WorkspaceID, a.UserID)
	in.Expected = v.Revision
	in.RuntimeID = testUUID(f.fx.Runtime(t, "Foreign private", testutil.Cols{"provider": "claude", "owner_id": outsider}))
	if _, err := m.Save(t.Context(), a, in, agent.Catalog{}); !errors.Is(err, ErrTargetIncompatible) {
		t.Fatal("private foreign runtime accepted")
	}
	ws := f.fx.Workspace(t, "Other target workspace", "other-managed-workspace")
	in.RuntimeID = testUUID(f.fx.Runtime(t, "Foreign workspace", testutil.Cols{"workspace_id": ws, "provider": "claude", "visibility": "public"}))
	if _, err := m.Save(t.Context(), a, in, agent.Catalog{}); !errors.Is(err, ErrTargetIncompatible) {
		t.Fatal("cross-workspace runtime accepted")
	}

	f.fx.Exec(t, `UPDATE member SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, a.WorkspaceID, denied.UserID)
	if _, err := m.Reveal(t.Context(), denied, in.AgentID); err != nil {
		t.Fatal("workspace admin who does not own Agent was denied", err)
	}
}

type rejectedAuditRow struct{}

func (rejectedAuditRow) Scan(...any) error {
	return errors.New("fake DB failure containing env-private-value")
}

type failingConfigTx struct {
	pgx.Tx
	mode string
}

func (t failingConfigTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if t.mode == "audit" && strings.Contains(sql, "INSERT INTO activity_log") {
		return rejectedAuditRow{}
	}
	return t.Tx.QueryRow(ctx, sql, args...)
}
func (t failingConfigTx) Commit(ctx context.Context) error {
	if t.mode == "commit" {
		return errors.New("fake commit failure containing env-private-value")
	}
	return t.Tx.Commit(ctx)
}

type failingConfigStarter struct {
	base configTxStarter
	mode string
}

func (s failingConfigStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.base.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failingConfigTx{Tx: tx, mode: s.mode}, nil
}

func TestProtectedTargetFailuresNeverGrantUnauditedSecretsOrPartialWrites(t *testing.T) {
	for _, mode := range []string{"audit", "commit", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			f, m, a, in := managementFixture(t)
			v := saveManaged(t, m, a, in)
			if mode == "overflow" {
				f.fx.Exec(t, `UPDATE agent_failover_state SET version=$1 WHERE agent_id=$2`, int64(math.MaxInt64), in.AgentID)
				v.Revision.StateVersion = math.MaxInt64
			} else {
				m.Transactions = failingConfigStarter{base: f.pool, mode: mode}
			}
			previous := storedConfig(t, f.q, db.AgentFallbackConfig{AgentID: in.AgentID, WorkspaceID: a.WorkspaceID})
			state := storedState(t, f.q, f)
			if mode != "overflow" {
				o, err := m.Reveal(t.Context(), a, in.AgentID)
				if !errors.Is(err, ErrTargetStore) || o.Version != 0 || len(o.CustomEnv) > 0 || strings.Contains(err.Error(), "private-value") {
					t.Fatal("failed audit/commit released plaintext or raw DB error")
				}
			}
			in.Expected = v.Revision
			in.Overrides.CustomEnv = map[string]string{"REPLACED": "replacement-private-value"}
			_, err := m.Save(t.Context(), a, in, agent.Catalog{})
			if !errors.Is(err, ErrTargetStore) || strings.Contains(err.Error(), "private-value") {
				t.Fatal("save failure leaked or was ignored", err)
			}
			if auditCount(t, f) != 1 || !reflect.DeepEqual(storedConfig(t, f.q, previous), previous) || storedState(t, f.q, f) != state {
				t.Fatal("failed save escaped transaction rollback")
			}
		})
	}
}

func TestProtectedTargetConcurrentEditsAndDispatchedProbe(t *testing.T) {
	f, m, a, in := managementFixture(t)
	v := saveManaged(t, m, a, in)
	f.row = storedState(t, f.q, f)
	s, err := f.activate(t.Context(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	f.row = s
	f.due(t)
	p, err := f.claim(t.Context(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	p, err = f.dispatch(t.Context(), f.q, p)
	if err != nil {
		t.Fatal(err)
	}
	in.Expected = ConfigRevision{Generation: v.Revision.Generation, PrimaryGeneration: p.PrimaryGeneration, StateVersion: p.Version}
	var wg sync.WaitGroup
	var winners atomic.Int32
	for range 2 {
		wg.Go(func() {
			_, err := m.Save(t.Context(), a, in, agent.Catalog{})
			if err == nil {
				winners.Add(1)
			} else if !errors.Is(err, ErrTargetConflict) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if winners.Load() != 1 || auditCount(t, f) != 2 {
		t.Fatal("concurrent edit lost CAS/audit atomicity")
	}
	if _, err := f.settle(t.Context(), f.q, p, "success"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("old probe recovered after secret change")
	}
	state := storedState(t, f.q, f)
	if state.State != "fallback_active" || state.ProbeToken.Valid {
		t.Fatal("configuration save claimed primary health")
	}
}

func TestProtectedTargetInitializationAndCorruptStateFailClosed(t *testing.T) {
	f, m, a, in := managementFixture(t)
	// Existing metadata without its policy is corruption, never a reset request.
	f.fx.Exec(t, `DELETE FROM agent_failover_state WHERE agent_id=$1`, in.AgentID)
	in.Expected = ConfigRevision{}
	v := saveManaged(t, m, a, in)
	if v.Revision.StateVersion != 1 || !nonzeroUUID(v.Revision.PrimaryGeneration) {
		t.Fatal("first opt-in did not initialize atomically")
	}
	f.fx.Exec(t, `DELETE FROM agent_failover_state WHERE agent_id=$1`, in.AgentID)
	in.Expected = ConfigRevision{Generation: v.Revision.Generation}
	if _, err := m.Save(t.Context(), a, in, agent.Catalog{}); !errors.Is(err, ErrTargetConflict) {
		t.Fatal("missing state was silently reset")
	}
	if auditCount(t, f) != 1 {
		t.Fatal("failed reset wrote audit")
	}
}

func TestProtectedTargetMalformedAndIncompatibleDocumentsDoNotPersist(t *testing.T) {
	f, m, a, in := managementFixture(t)
	in.Overrides.Version = 2
	if _, err := m.Save(t.Context(), a, in, agent.Catalog{}); !errors.Is(err, ErrTargetDocument) {
		t.Fatal("unknown document version persisted")
	}
	in.Overrides.Version = 1
	in.ThinkingLevel = "not-supported"
	if _, err := m.Save(t.Context(), a, in, agent.Catalog{}); !errors.Is(err, ErrTargetIncompatible) {
		t.Fatal("known incompatible target accepted")
	}
	if auditCount(t, f) != 0 {
		t.Fatal("invalid target wrote audit")
	}
	if _, err := f.q.GetAgentFallbackConfig(t.Context(), db.GetAgentFallbackConfigParams{AgentID: in.AgentID, WorkspaceID: a.WorkspaceID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("rejected document left config")
	}
}

func TestProtectedTargetDocumentValidationNeverEchoesSecretInput(t *testing.T) {
	for _, raw := range []string{`{"version":2}`, `{"version":1,"runtime_config":[]}`, `{"version":1,"runtime_config":null}`, `{"version":1,"custom_args":["bad\u0000value"]}`, `{"version":1,"unexpected":"env-private-value"}`, `{"version":1} {"version":1}`} {
		if _, err := decodeExecutionOverrides([]byte(raw)); !errors.Is(err, ErrTargetDocument) || strings.Contains(err.Error(), "private-value") {
			t.Fatal("malformed document accepted or echoed")
		}
	}

	for _, in := range []ExecutionOverrides{{Version: 1, CustomEnv: map[string]string{"BAD=KEY": "secret"}}, {Version: 1, CustomEnv: map[string]string{"KEY": strings.Repeat("x", 1<<20)}}} {
		if _, err := normalizeExecutionOverrides(in); !errors.Is(err, ErrTargetDocument) {
			t.Fatal("invalid/bounded document accepted")
		}
	}
	owner := newGeneration()
	if managementMemberAllowed(db.Agent{OwnerID: owner}, db.Member{UserID: owner, Role: "unknown"}) {
		t.Fatal("unknown role gained secret-management authority")
	}
}

func TestMetadataOnlyEditClearsProtectedOverrides(t *testing.T) {
	f, m, a, in := managementFixture(t)
	saveManaged(t, m, a, in)
	c := storedConfig(t, f.q, db.AgentFallbackConfig{AgentID: in.AgentID, WorkspaceID: a.WorkspaceID})
	s := storedState(t, f.q, f)
	if _, err := f.q.UpdateAgentFallbackConfig(t.Context(), configEdit(c, s)); err != nil {
		t.Fatal(err)
	}
	view, err := m.Inspect(t.Context(), a, in.AgentID)
	if err != nil || view.OverridesStored {
		t.Fatal("metadata-only writer retained credentials")
	}
	if _, err := m.Reveal(t.Context(), a, in.AgentID); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatal("metadata-only target exposed old override values")
	}
}
