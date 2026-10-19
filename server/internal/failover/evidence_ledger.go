package failover

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var ErrEvidenceStore = errors.New("execution evidence persistence unavailable")

// EvidenceRequest contains independently current server facts, not report/body
// fields. Build Binding from DescribeTargetBinding under upstream owner locks,
// Connection from the actual authenticated RPC and current runtime row, and
// Identity from the approved target-specific observer. Those callers do not
// exist in production yet; the ledger cannot establish those facts itself.
type EvidenceRequest struct {
	Binding    TargetBinding     `json:"binding"`
	Connection RuntimeConnection `json:"connection"`
	Identity   ExecutionIdentity `json:"identity"`
	Use        EvidenceUse       `json:"use"`
}

// EvidenceLedger is internal leaf persistence, not an authorization boundary.
// Use Queries.WithTx in the caller's owner transaction; after entering the
// config->state leaf, do not acquire upstream/task locks. Do not publish a
// challenge or use verified evidence until that transaction commits. A rollback
// restores the pending slot. Single-statement callers may use autocommit.
// No endpoint/worker/writer currently calls this ledger.
type EvidenceLedger struct {
	Queries  *db.Queries
	Verifier EvidenceVerifier
}

func (r EvidenceRequest) document() ([]byte, error) {
	_, known := requiredEvidenceFeatures(r.Use)
	if !known || !validBinding(r.Binding) || !validConnection(r.Connection) || !validExecutionIdentity(r.Identity) || r.Connection.RuntimeID != r.Binding.RuntimeID || r.Connection.WorkspaceID != r.Binding.WorkspaceID || (r.Use == ProbeEvidence && r.Binding.Route != "primary") || (r.Binding.Model != "" && r.Identity.EffectiveModel != r.Binding.Model) {
		return nil, ErrExecutionEvidence
	}
	// Persist only the typed, non-secret context, with a smaller application
	// limit than the DB's normalized JSONB limit. Never persist a raw report.
	raw, err := json.Marshal(struct {
		Version int `json:"version"`
		EvidenceRequest
	}{1, r})
	if err != nil || len(raw) > 8192 {
		return nil, ErrExecutionEvidence
	}
	return raw, nil
}

// Issue replaces the single pending slot. Token and exclusive two-minute
// deadline come from PostgreSQL after config/state locks, never caller time.
// It changes no availability version, health, probe lease or task route.
func (l EvidenceLedger) Issue(ctx context.Context, r EvidenceRequest) (EvidenceChallenge, error) {
	raw, err := r.document()
	if err != nil {
		return EvidenceChallenge{}, err
	}
	if l.Queries == nil {
		return EvidenceChallenge{}, ErrEvidenceStore
	}
	b, c := r.Binding, r.Connection
	row, err := l.Queries.IssueAgentExecutionChallenge(ctx, db.IssueAgentExecutionChallengeParams{
		AgentID: b.AgentID, WorkspaceID: b.WorkspaceID, PrimaryGeneration: b.PrimaryGeneration, FallbackGeneration: b.FallbackGeneration,
		AgentOwnerID: b.AgentOwnerID, RuntimeID: b.RuntimeID, RuntimeOwnerID: c.OwnerID, Provider: c.Provider,
		RuntimeProfileID: b.RuntimeProfileID, Route: b.Route, RuntimeMode: b.RuntimeMode, EvidenceContext: raw,
	})
	if err != nil {
		return EvidenceChallenge{}, evidenceStoreError(err)
	}
	return r.challenge(row.EvidenceToken, row.EvidenceIssuedAt.Time, row.EvidenceExpiresAt.Time), nil
}

// Consume clears a matching, unexpired pending challenge atomically. Invalid
// capabilities/reports burn that nonce but never yield a verified value. A
// repeated response cannot win again, even after process restart. The returned
// capability evidence is not a one-use execution ticket or inference result.
func (l EvidenceLedger) Consume(ctx context.Context, r EvidenceRequest, report EvidenceReport) (VerifiedEvidence, error) {
	raw, err := r.document()
	if err != nil || !nonzeroUUID(report.Challenge) {
		return VerifiedEvidence{}, ErrExecutionEvidence
	}
	if l.Queries == nil {
		return VerifiedEvidence{}, ErrEvidenceStore
	}
	b, c := r.Binding, r.Connection
	row, err := l.Queries.ConsumeAgentExecutionChallenge(ctx, db.ConsumeAgentExecutionChallengeParams{
		AgentID: b.AgentID, WorkspaceID: b.WorkspaceID, PrimaryGeneration: b.PrimaryGeneration, FallbackGeneration: b.FallbackGeneration,
		AgentOwnerID: b.AgentOwnerID, RuntimeID: b.RuntimeID, RuntimeOwnerID: c.OwnerID, Provider: c.Provider,
		RuntimeProfileID: b.RuntimeProfileID, Route: b.Route, RuntimeMode: b.RuntimeMode, EvidenceContext: raw, EvidenceToken: report.Challenge,
	})
	if err != nil {
		return VerifiedEvidence{}, evidenceStoreError(err)
	}
	challenge := r.challenge(row.EvidenceToken, row.EvidenceIssuedAt.Time, row.EvidenceExpiresAt.Time)
	return l.Verifier.Verify(challenge, report, r.Connection, r.Identity, row.EvaluatedAt.Time)
}

func (r EvidenceRequest) challenge(token pgtype.UUID, issued, expires time.Time) EvidenceChallenge {
	return EvidenceChallenge{Token: token, Binding: r.Binding, Connection: r.Connection, Identity: r.Identity, Use: r.Use, IssuedAt: issued, ExpiresAt: expires}
}

func evidenceStoreError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrExecutionEvidence
	}
	return ErrEvidenceStore
}
