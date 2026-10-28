package failover

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func ledgerFixture(t *testing.T) (*stateFixture, db.AgentFallbackConfig, EvidenceLedger, EvidenceRequest) {
	t.Helper()
	f, cfg := newTargetFixture(t)
	a, err := f.q.GetAgent(t.Context(), f.row.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.q.GetAgentRuntime(t.Context(), a.RuntimeID)
	if err != nil {
		t.Fatal(err)
	}
	request := EvidenceRequest{
		Binding:    TargetBinding{AgentID: a.ID, WorkspaceID: a.WorkspaceID, AgentOwnerID: a.OwnerID, RuntimeID: r.ID, RuntimeProfileID: r.ProfileID, PrimaryGeneration: f.row.PrimaryGeneration, FallbackGeneration: cfg.Generation, Route: "primary", RuntimeMode: a.RuntimeMode, Model: a.Model.String, ThinkingLevel: a.ThinkingLevel.String, ServiceTier: a.ServiceTier.String},
		Connection: RuntimeConnection{RuntimeID: r.ID, WorkspaceID: r.WorkspaceID, OwnerID: r.OwnerID, SessionID: newGeneration(), Provider: r.Provider},
		Identity:   ExecutionIdentity{CredentialEpoch: newGeneration(), ConfigurationEpoch: newGeneration(), EffectiveModel: "fixture-model"}, Use: TaskEvidence,
	}
	if request.Binding.Model != "" {
		request.Identity.EffectiveModel = request.Binding.Model
	}
	verifier := newEvidenceVerifier(map[AdapterContract]EvidenceFeature{{Provider: r.Provider, Version: "fixture-only/v1"}: taskEvidenceFeatures | probeEvidenceFeatures})
	return f, cfg, EvidenceLedger{Queries: f.q, Verifier: verifier}, request
}

func ledgerReport(c EvidenceChallenge) EvidenceReport {
	return EvidenceReport{Version: 1, Challenge: c.Token, Binding: c.Binding, Connection: c.Connection, Identity: c.Identity, Use: c.Use, AdapterContract: "fixture-only/v1", Features: taskEvidenceFeatures | probeEvidenceFeatures}
}

func issueEvidence(t *testing.T, l EvidenceLedger, r EvidenceRequest) EvidenceChallenge {
	t.Helper()
	c, err := l.Issue(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	if !nonzeroUUID(c.Token) || c.ExpiresAt.Sub(c.IssuedAt) != evidenceMaxLifetime {
		t.Fatal("database did not issue bounded challenge")
	}
	return c
}

func TestStrictEvidenceLedgerRejectsNativeLoginPathPolicy(t *testing.T) {
	f, _, l, r := ledgerFixture(t)
	c := issueEvidence(t, l, r)
	// Adversarial fixture: even if a writer neglected generation rotation,
	// the strict persistence boundary cannot issue/consume native policy.
	f.fx.Exec(t, `UPDATE agent_fallback_config SET native_login_path=true WHERE agent_id=$1`, f.row.AgentID)
	if got, err := l.Issue(t.Context(), r); !errors.Is(err, ErrExecutionEvidence) || got.Token.Valid {
		t.Fatal("native config acquired strict challenge", err)
	}
	if got, err := l.Consume(t.Context(), r, ledgerReport(c)); !errors.Is(err, ErrExecutionEvidence) || got.verified {
		t.Fatal("native config acquired strict evidence", err)
	}
}

func TestEvidenceLedgerSupersedesAndSurvivesManagerRestartWithSingleConsumer(t *testing.T) {
	f, _, l, r := ledgerFixture(t)
	first := issueEvidence(t, l, r)
	second := issueEvidence(t, l, r)
	if first.Token == second.Token {
		t.Fatal("new challenge reused a nonce")
	}
	if _, err := l.Consume(t.Context(), r, ledgerReport(first)); !errors.Is(err, ErrExecutionEvidence) {
		t.Fatal("superseded challenge accepted", err)
	}
	// No process-local pending map: a newly constructed manager and Queries
	// recover the challenge entirely from the database.
	restarted := EvidenceLedger{Queries: db.New(f.pool), Verifier: l.Verifier}
	var successes atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			proof, err := restarted.Consume(t.Context(), r, ledgerReport(second))
			if err == nil && proof.verified {
				successes.Add(1)
			} else if !errors.Is(err, ErrExecutionEvidence) {
				t.Errorf("consume: %v", err)
			}
		})
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatal("concurrent nonce consumption winners", successes.Load())
	}
	after, err := f.q.GetAgentFailoverState(t.Context(), db.GetAgentFailoverStateParams{AgentID: r.Binding.AgentID, WorkspaceID: r.Binding.WorkspaceID})
	if err != nil || after.EvidenceToken.Valid || len(after.EvidenceContext) != 0 || after.EvidenceIssuedAt.Valid || after.EvidenceExpiresAt.Valid {
		t.Fatal("consumption did not clear bounded slot", err)
	}
	if after.Version != f.row.Version || after.State != f.row.State || after.UpdatedAt != f.row.UpdatedAt || after.ProbeToken != f.row.ProbeToken || after.RecoveredAt != f.row.RecoveredAt {
		t.Fatal("capability exchange changed availability/health/probe authority")
	}
}

func TestEvidenceLedgerRequiresIndependentlyMatchingCurrentContext(t *testing.T) {
	_, _, l, request := ledgerFixture(t)
	for _, name := range []string{"agent", "workspace", "owner", "runtime", "profile", "primary generation", "fallback generation", "route", "session", "credentials", "configuration", "model", "purpose", "runtime owner", "provider"} {
		t.Run(name, func(t *testing.T) {
			c := issueEvidence(t, l, request)
			r := request
			switch name {
			case "agent":
				r.Binding.AgentID = newGeneration()
			case "workspace":
				r.Binding.WorkspaceID = newGeneration()
				r.Connection.WorkspaceID = r.Binding.WorkspaceID
			case "owner":
				r.Binding.AgentOwnerID = newGeneration()
			case "runtime":
				r.Binding.RuntimeID = newGeneration()
				r.Connection.RuntimeID = r.Binding.RuntimeID
			case "profile":
				r.Binding.RuntimeProfileID = newGeneration()
			case "primary generation":
				r.Binding.PrimaryGeneration = newGeneration()
			case "fallback generation":
				r.Binding.FallbackGeneration = newGeneration()
			case "route":
				r.Binding.Route = "fallback"
			case "session":
				r.Connection.SessionID = newGeneration()
			case "credentials":
				r.Identity.CredentialEpoch = newGeneration()
			case "configuration":
				r.Identity.ConfigurationEpoch = newGeneration()
			case "model":
				r.Identity.EffectiveModel = "different-model"
			case "purpose":
				r.Use = ProbeEvidence
			case "runtime owner":
				r.Connection.OwnerID = newGeneration()
			case "provider":
				r.Connection.Provider = "different-provider"
			}
			if proof, err := l.Consume(t.Context(), r, ledgerReport(c)); !errors.Is(err, ErrExecutionEvidence) || proof.verified {
				t.Fatal("mismatched current identity consumed challenge", err)
			}
			if _, err := l.Consume(t.Context(), request, ledgerReport(c)); err != nil {
				t.Fatal("wrong scope destroyed unrelated valid challenge", err)
			}
		})
	}
}

func TestEvidenceLedgerInvalidReportsBurnNonceAndProductionPolicyDenies(t *testing.T) {
	_, _, l, r := ledgerFixture(t)
	for _, name := range []string{"unapproved", "missing feature", "wrong report"} {
		t.Run(name, func(t *testing.T) {
			c := issueEvidence(t, l, r)
			report := ledgerReport(c)
			consumer := l
			switch name {
			case "unapproved":
				consumer.Verifier = NewEvidenceVerifier()
			case "missing feature":
				report.Features &^= TargetSessionIsolation
			case "wrong report":
				report.Binding.ThinkingLevel = "different"
			}
			if proof, err := consumer.Consume(t.Context(), r, report); !errors.Is(err, ErrExecutionEvidence) || proof.verified {
				t.Fatal("invalid report became verified evidence", err)
			}
			if _, err := l.Consume(t.Context(), r, ledgerReport(c)); !errors.Is(err, ErrExecutionEvidence) {
				t.Fatal("invalid report nonce could be replayed", err)
			}
		})
	}
}

func TestEvidenceLedgerSupportsBothTargetsAndSeparateProbeUseWithoutHealthMutation(t *testing.T) {
	for _, name := range []string{"primary task", "fallback task", "primary probe"} {
		t.Run(name, func(t *testing.T) {
			f, cfg, l, r := ledgerFixture(t)
			if name == "primary probe" {
				r.Use = ProbeEvidence
			}
			if name == "fallback task" {
				runtime, err := f.q.GetAgentRuntime(t.Context(), cfg.RuntimeID)
				if err != nil {
					t.Fatal(err)
				}
				r.Binding.Route, r.Binding.RuntimeID, r.Binding.RuntimeProfileID = "fallback", runtime.ID, runtime.ProfileID
				r.Binding.RuntimeMode, r.Binding.Model, r.Binding.ThinkingLevel, r.Binding.ServiceTier = runtime.RuntimeMode, cfg.Model, cfg.ThinkingLevel, cfg.ServiceTier
				r.Connection.RuntimeID, r.Connection.OwnerID, r.Connection.Provider = runtime.ID, runtime.OwnerID, runtime.Provider
				r.Identity.EffectiveModel = cfg.Model
			}
			c := issueEvidence(t, l, r)
			active, err := f.activate(t.Context(), f.q)
			if err != nil {
				t.Fatal(err)
			}
			proof, err := l.Consume(t.Context(), r, ledgerReport(c))
			if err != nil || !proof.verified || proof.use != r.Use {
				t.Fatal("availability-only transition invalidated capability context", err)
			}
			after := storedState(t, f.q, f)
			if after.State != "fallback_active" || after.Version != active.Version || after.RecoveredAt.Valid {
				t.Fatal("capability evidence recovered primary or changed availability")
			}
		})
	}
}

func TestEvidenceLedgerRollbackRestoresChallengeAndDoesNotPublishIssuance(t *testing.T) {
	f, _, l, r := ledgerFixture(t)
	tx, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	transactional := EvidenceLedger{Queries: f.q.WithTx(tx), Verifier: l.Verifier}
	unpublished := issueEvidence(t, transactional, r)
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Consume(t.Context(), r, ledgerReport(unpublished)); !errors.Is(err, ErrExecutionEvidence) {
		t.Fatal("rolled-back issuance survived", err)
	}
	c := issueEvidence(t, l, r)
	tx, err = f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	transactional.Queries = f.q.WithTx(tx)
	if _, err := transactional.Consume(t.Context(), r, ledgerReport(c)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Consume(t.Context(), r, ledgerReport(c)); err != nil {
		t.Fatal("rollback did not restore pending slot", err)
	}
}

func TestEvidenceLedgerExpirySampledAfterRowLockWait(t *testing.T) {
	f, _, l, r := ledgerFixture(t)
	c := issueEvidence(t, l, r)
	f.fx.Exec(t, `UPDATE agent_failover_state SET evidence_issued_at=clock_timestamp()-interval '1 second', evidence_expires_at=clock_timestamp()+interval '50 milliseconds' WHERE agent_id=$1`, r.Binding.AgentID)
	blocker, err := f.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(t.Context())
	if _, err := blocker.Exec(t.Context(), `SELECT agent_id FROM agent_failover_state WHERE agent_id=$1 FOR UPDATE`, r.Binding.AgentID); err != nil {
		t.Fatal(err)
	}
	connection, err := f.pool.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	consumer := EvidenceLedger{Queries: db.New(connection), Verifier: l.Verifier}
	consumerPID := connection.Conn().PgConn().PID()
	result := make(chan error, 1)
	finished := make(chan struct{})
	consumeCtx, cancel := context.WithCancel(t.Context())
	defer func() {
		cancel()
		_ = blocker.Rollback(context.Background())
		<-finished
		connection.Release()
	}()
	go func() {
		defer close(finished)
		_, err := consumer.Consume(consumeCtx, r, ledgerReport(c))
		result <- err
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	waiting := false
	for !waiting {
		select {
		case err := <-result:
			t.Fatalf("consume did not wait for locked state: %v", err)
		case <-deadline.C:
			t.Fatal("consumer not observed waiting on row lock")
		case <-ticker.C:
			if err := f.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, consumerPID).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Wait for the actual DB deadline while holding the row, rather than
	// trusting a client timestamp sampled before the lock became available.
	if _, err := blocker.Exec(t.Context(), `SELECT pg_sleep(GREATEST(0,extract(epoch FROM (evidence_expires_at-clock_timestamp())))+0.01) FROM agent_failover_state WHERE agent_id=$1`, r.Binding.AgentID); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrExecutionEvidence) {
			t.Fatal("expired queued consumer accepted", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("consumer did not complete")
	}
	// A new issue may replace the expired bounded slot.
	if next := issueEvidence(t, l, r); next.Token == c.Token {
		t.Fatal("expired nonce reused")
	}
}

func TestEvidenceLedgerLifecycleChangesClearPendingSlot(t *testing.T) {
	for _, kind := range []string{"rotate", "suspend", "edit", "remove"} {
		t.Run(kind, func(t *testing.T) {
			f, cfg, l, r := ledgerFixture(t)
			c := issueEvidence(t, l, r)
			var err error
			switch kind {
			case "rotate":
				_, err = f.q.RotateAgentFailoverGeneration(t.Context(), db.RotateAgentFailoverGenerationParams{AgentID: r.Binding.AgentID, WorkspaceID: r.Binding.WorkspaceID, ExpectedGeneration: r.Binding.PrimaryGeneration, NewGeneration: newGeneration()})
			case "suspend":
				_, err = f.q.SuspendAgentFailoverState(t.Context(), db.SuspendAgentFailoverStateParams{AgentID: r.Binding.AgentID, WorkspaceID: r.Binding.WorkspaceID})
			case "edit":
				_, err = f.q.UpdateAgentFallbackConfig(t.Context(), db.UpdateAgentFallbackConfigParams{AgentID: cfg.AgentID, WorkspaceID: cfg.WorkspaceID, RuntimeID: cfg.RuntimeID, ExpectedGeneration: cfg.Generation, NewGeneration: newGeneration(), ExpectedPrimaryGeneration: f.row.PrimaryGeneration, NewPrimaryGeneration: newGeneration(), ExpectedVersion: f.row.Version})
			case "remove":
				_, err = f.q.DeleteAgentFallbackConfig(t.Context(), db.DeleteAgentFallbackConfigParams{AgentID: cfg.AgentID, WorkspaceID: cfg.WorkspaceID, ExpectedGeneration: cfg.Generation, ExpectedPrimaryGeneration: f.row.PrimaryGeneration, ExpectedVersion: f.row.Version, NewPrimaryGeneration: newGeneration()})
			}
			if err != nil {
				t.Fatal(err)
			}
			state, err := f.q.GetAgentFailoverState(t.Context(), db.GetAgentFailoverStateParams{AgentID: cfg.AgentID, WorkspaceID: cfg.WorkspaceID})
			if err != nil || state.EvidenceToken.Valid || len(state.EvidenceContext) != 0 || state.EvidenceIssuedAt.Valid || state.EvidenceExpiresAt.Valid {
				t.Fatal("lifecycle left pending authority", err)
			}
			if _, err := l.Consume(t.Context(), r, ledgerReport(c)); !errors.Is(err, ErrExecutionEvidence) {
				t.Fatal("old lifecycle challenge accepted", err)
			}
		})
	}
}

type failedEvidenceDB struct{ db.DBTX }
type failedEvidenceRow struct{}

func (failedEvidenceRow) Scan(...any) error {
	return errors.New("database error including private-value")
}
func (failedEvidenceDB) QueryRow(context.Context, string, ...any) pgx.Row { return failedEvidenceRow{} }

func TestEvidenceLedgerInvalidInputAndStoreErrorsDoNotLeakOrGrantProof(t *testing.T) {
	_, c, _, _ := evidenceFixture(TaskEvidence)
	r := EvidenceRequest{Binding: c.Binding, Connection: c.Connection, Identity: c.Identity, Use: c.Use}
	l := EvidenceLedger{Queries: db.New(failedEvidenceDB{})}
	if challenge, err := l.Issue(t.Context(), r); !errors.Is(err, ErrEvidenceStore) || challenge.Token.Valid || strings.Contains(err.Error(), "private-value") {
		t.Fatal("issue leaked store error or authority", err)
	}
	if proof, err := l.Consume(t.Context(), r, ledgerReport(c)); !errors.Is(err, ErrEvidenceStore) || proof.verified || strings.Contains(err.Error(), "private-value") {
		t.Fatal("consume leaked store error or authority", err)
	}
	for _, change := range []func(*EvidenceRequest){
		func(r *EvidenceRequest) { r.Use = "unknown" },
		func(r *EvidenceRequest) { r.Use = ProbeEvidence; r.Binding.Route = "fallback" },
		func(r *EvidenceRequest) { r.Connection.SessionID = pgtype.UUID{} },
		func(r *EvidenceRequest) { r.Identity.EffectiveModel = strings.Repeat("x", 9000); r.Binding.Model = "" },
	} {
		bad := r
		change(&bad)
		if _, err := l.Issue(t.Context(), bad); !errors.Is(err, ErrExecutionEvidence) {
			t.Fatal("invalid context reached DB", err)
		}
	}
}
