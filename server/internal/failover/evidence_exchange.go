package failover

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EvidenceSelection is selection intent only. No caller identity, generation,
// catalog, connection ID or credential/config epoch is accepted from a report.
type EvidenceSelection struct {
	AgentID, WorkspaceID, RuntimeID pgtype.UUID
	Route                           string
	Use                             EvidenceUse
}

// targetObservationSource is a reviewed server-owned snapshot source, never
// built from the report being verified. Implementations must perform bounded
// in-memory reads only: no CLI/account lookup, network or DB under owner locks.
// No production implementation is approved. Tests use a private fixture source.
type targetObservationSource interface {
	Catalog(context.Context, RuntimeConnection) (agent.Catalog, error)
	Identity(context.Context, TargetBinding, RuntimeConnection) (ExecutionIdentity, error)
}

type EvidenceExchange struct {
	manager  ConfigManager
	observer targetObservationSource
	verifier EvidenceVerifier
}

// NewEvidenceExchange fails closed until a reviewed observation source and
// adapter contract exist. Transport callers cannot inject report-fed epochs or
// approval policy. This internal boundary has no registered public RPC method.
func NewEvidenceExchange(manager ConfigManager) EvidenceExchange {
	return EvidenceExchange{manager: manager, verifier: NewEvidenceVerifier()}
}

type evidenceExchangeTx struct {
	pgx.Tx
	q       *db.Queries
	agent   db.Agent
	config  db.AgentFallbackConfig
	state   db.AgentFailoverState
	runtime db.AgentRuntime
	request EvidenceRequest
}

func (e EvidenceExchange) begin(ctx context.Context, s EvidenceSelection) (*evidenceExchangeTx, error) {
	if !nonzeroUUID(s.AgentID) || !nonzeroUUID(s.WorkspaceID) || !nonzeroUUID(s.RuntimeID) || (s.Route != "primary" && s.Route != "fallback") || (s.Use != TaskEvidence && s.Use != ProbeEvidence) || (s.Use == ProbeEvidence && s.Route != "primary") {
		return nil, ErrExecutionEvidence
	}
	scope, ok := daemonws.RuntimeConnectionScope(ctx, uuid.UUID(s.RuntimeID.Bytes).String(), uuid.UUID(s.WorkspaceID.Bytes).String())
	// This management preflight boundary deliberately supports human PAT/JWT
	// connections only. A daemon token cannot impersonate its runtime owner.
	if !ok || !scope.HumanActor || scope.DaemonID != "" || e.observer == nil {
		return nil, ErrExecutionEvidence
	}
	userID, err := util.ParseUUID(scope.UserID)
	if err != nil || !nonzeroUUID(userID) {
		return nil, ErrTargetAccess
	}
	actor := ManagementActor{Kind: "member", UserID: userID, WorkspaceID: s.WorkspaceID}
	tx, q, a, err := e.manager.begin(ctx, actor, s.AgentID, s.RuntimeID)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*evidenceExchangeTx, error) { _ = tx.Rollback(ctx); return nil, err }
	c, state, hc, hs, err := loadTargetConfig(ctx, q, a)
	if err != nil {
		return fail(ErrEvidenceStore)
	}
	if !hc || !hs || c.NativeLoginPath || !validPolicySnapshot(a, &c, &state) {
		return fail(ErrExecutionEvidence)
	}
	runtime, err := q.GetAgentRuntime(ctx, s.RuntimeID)
	if err != nil {
		return fail(ErrExecutionEvidence)
	}
	// begin has locked primary and requested runtime without blocking against
	// runtime teardown. The descriptor below must select this exact runtime.
	connection := RuntimeConnection{RuntimeID: runtime.ID, WorkspaceID: runtime.WorkspaceID, OwnerID: runtime.OwnerID, SessionID: pgtype.UUID{Bytes: [16]byte(scope.ConnectionID), Valid: true}, Provider: runtime.Provider}
	current := &evidenceExchangeTx{Tx: tx, q: q, agent: a, config: c, state: state, runtime: runtime}
	request, err := e.observe(ctx, current, s, connection)
	if err != nil {
		return fail(err)
	}
	current.request = request
	return current, nil
}

func (e EvidenceExchange) observe(ctx context.Context, t *evidenceExchangeTx, s EvidenceSelection, connection RuntimeConnection) (EvidenceRequest, error) {
	catalog, err := e.observer.Catalog(ctx, connection)
	if err != nil {
		return EvidenceRequest{}, ErrExecutionEvidence
	}
	binding, err := DescribeTargetBinding(t.agent, t.config, t.state, s.Route, t.runtime, catalog)
	if err != nil {
		return EvidenceRequest{}, ErrExecutionEvidence
	}
	identity, err := e.observer.Identity(ctx, binding, connection)
	if err != nil {
		return EvidenceRequest{}, ErrExecutionEvidence
	}
	r := EvidenceRequest{Binding: binding, Connection: connection, Identity: identity, Use: s.Use}
	if _, err := r.document(); err != nil {
		return EvidenceRequest{}, ErrExecutionEvidence
	}
	return r, nil
}

func (e EvidenceExchange) recheck(ctx context.Context, t *evidenceExchangeTx, s EvidenceSelection) error {
	current, err := e.observe(ctx, t, s, t.request.Connection)
	if err != nil || current != t.request {
		return ErrExecutionEvidence
	}
	scope, ok := daemonws.RuntimeConnectionScope(ctx, uuid.UUID(s.RuntimeID.Bytes).String(), uuid.UUID(s.WorkspaceID.Bytes).String())
	if !ok || [16]byte(scope.ConnectionID) != t.request.Connection.SessionID.Bytes {
		return ErrExecutionEvidence
	}
	return nil
}

// Issue publishes a challenge only after owner authorization, current observed
// identity, durable issuance, final liveness/identity checks and commit.
func (e EvidenceExchange) Issue(ctx context.Context, s EvidenceSelection) (EvidenceChallenge, error) {
	t, err := e.begin(ctx, s)
	if err != nil {
		return EvidenceChallenge{}, err
	}
	defer t.Rollback(ctx)
	l := EvidenceLedger{Queries: t.q, Verifier: e.verifier}
	c, err := l.Issue(ctx, t.request)
	if err != nil {
		return EvidenceChallenge{}, err
	}
	if err := e.recheck(ctx, t, s); err != nil {
		return EvidenceChallenge{}, err
	}
	if err := t.Commit(ctx); err != nil {
		return EvidenceChallenge{}, ErrEvidenceStore
	}
	return c, nil
}

// Consume commits even a rejected report's nonce burn. No proof escapes a
// rollback, failed final observation or failed commit. This remains capability
// evidence, not execution/health authority or a provider session identifier.
func (e EvidenceExchange) Consume(ctx context.Context, s EvidenceSelection, report EvidenceReport) (VerifiedEvidence, error) {
	t, err := e.begin(ctx, s)
	if err != nil {
		return VerifiedEvidence{}, err
	}
	defer t.Rollback(ctx)
	l := EvidenceLedger{Queries: t.q, Verifier: e.verifier}
	proof, outcome := l.Consume(ctx, t.request, report)
	if outcome != nil && !errors.Is(outcome, ErrExecutionEvidence) {
		return VerifiedEvidence{}, outcome
	}
	if err := e.recheck(ctx, t, s); err != nil {
		return VerifiedEvidence{}, err
	}
	if err := t.Commit(ctx); err != nil {
		return VerifiedEvidence{}, ErrEvidenceStore
	}
	if outcome != nil {
		return VerifiedEvidence{}, outcome
	}
	return proof, nil
}
