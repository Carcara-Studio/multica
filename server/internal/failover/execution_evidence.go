package failover

import (
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

var ErrExecutionEvidence = errors.New("execution target evidence unavailable")

type EvidenceUse string

const (
	TaskEvidence  EvidenceUse = "task"
	ProbeEvidence EvidenceUse = "primary_probe"
)

// EvidenceFeature describes independently approved adapter guarantees, not
// client-advertised header capabilities. None is an inference-success result.
type EvidenceFeature uint64

const (
	TargetOptionsExact EvidenceFeature = 1 << iota
	EffectiveCredentialsKnown
	EffectiveModelKnown
	SharedTaskContextExact
	TaskToolsAndMCPExact
	TargetSessionIsolation
	MinimalInferenceOnly
	ProbeNoTools
	ProbeNoMCP
	ProbeNoHooks
	ProbeNoRepository
	ProbeNoUserContext
	ProbeNoSession
)
const taskEvidenceFeatures = TargetOptionsExact | EffectiveCredentialsKnown | EffectiveModelKnown | SharedTaskContextExact | TaskToolsAndMCPExact | TargetSessionIsolation
const probeEvidenceFeatures = TargetOptionsExact | EffectiveCredentialsKnown | EffectiveModelKnown | MinimalInferenceOnly | ProbeNoTools | ProbeNoMCP | ProbeNoHooks | ProbeNoRepository | ProbeNoUserContext | ProbeNoSession
const evidenceMaxLifetime = 2 * time.Minute

// TargetBinding includes only non-secret identity/options. Both routes bind the
// fallback generation (ADR-018). Availability version is deliberately absent:
// recovery must not reroute/invalidate an already queued fallback Task.
type TargetBinding struct {
	AgentID, WorkspaceID, AgentOwnerID, RuntimeID, RuntimeProfileID pgtype.UUID
	PrimaryGeneration, FallbackGeneration                           pgtype.UUID
	Route, RuntimeMode, Model, ThinkingLevel, ServiceTier           string
}

// RuntimeConnection must come from current authenticated server connection
// state. A report/header cannot grant itself another runtime/session identity.
type RuntimeConnection struct {
	RuntimeID, WorkspaceID, OwnerID, SessionID pgtype.UUID
	Provider                                   string
}

// ExecutionIdentity is target-scoped: two targets on one runtime can use
// different credentials. Epochs are opaque, never hashes of credentials.
// The approved adapter must detect effective credential/default/config changes;
// startup IDs, heartbeat and catalog freshness cannot substitute for that work.
type ExecutionIdentity struct {
	CredentialEpoch, ConfigurationEpoch pgtype.UUID
	EffectiveModel                      string
}

// EvidenceChallenge is server-issued. EvidenceLedger persists/consumes it
// atomically; the future transport must use that boundary. Pure verification
// below does not consume it.
type EvidenceChallenge struct {
	Token               pgtype.UUID
	Binding             TargetBinding
	Connection          RuntimeConnection
	Identity            ExecutionIdentity
	Use                 EvidenceUse
	IssuedAt, ExpiresAt time.Time
}
type EvidenceReport struct {
	Version         int
	Challenge       pgtype.UUID
	Binding         TargetBinding
	Connection      RuntimeConnection
	Identity        ExecutionIdentity
	Use             EvidenceUse
	AdapterContract string
	Features        EvidenceFeature
}

type AdapterContract struct{ Provider, Version string }

// EvidenceVerifier's approval map is server-owned implementation policy, never
// report/request input. Empty/default policy approves no adapters. No production
// adapter is approved by this increment; tests inject a fixture-only contract.
type EvidenceVerifier struct {
	approved map[AdapterContract]EvidenceFeature
}

// NewEvidenceVerifier approves no adapters until their credential/config epoch
// tracking and execution guarantees are implemented and reviewed in this package.
// Transport callers cannot populate approval from a daemon report or header.
func NewEvidenceVerifier() EvidenceVerifier { return newEvidenceVerifier(nil) }

func newEvidenceVerifier(approved map[AdapterContract]EvidenceFeature) EvidenceVerifier {
	copy := make(map[AdapterContract]EvidenceFeature, len(approved))
	for k, v := range approved {
		copy[k] = v
	}
	return EvidenceVerifier{approved: copy}
}

// VerifiedEvidence has no exported fields so a zero value or JSON-decoded
// report cannot masquerade as verified. It authorizes neither enqueue nor health
// recovery by itself; owner locks, ledger consumption and real inference remain
// separate caller gates. Runtime connection/epochs are rechecked at use time.
type VerifiedEvidence struct {
	binding      TargetBinding
	connection   RuntimeConnection
	identity     ExecutionIdentity
	use          EvidenceUse
	since, until time.Time
	features     EvidenceFeature
	verified     bool
}

func (v EvidenceVerifier) Verify(challenge EvidenceChallenge, report EvidenceReport, current RuntimeConnection, identity ExecutionIdentity, now time.Time) (VerifiedEvidence, error) {
	reject := func() (VerifiedEvidence, error) { return VerifiedEvidence{}, ErrExecutionEvidence }
	if report.Version != 1 || !nonzeroUUID(challenge.Token) || report.Challenge != challenge.Token || !validBinding(challenge.Binding) {
		return reject()
	}
	if !validConnection(current) || current != challenge.Connection || report.Connection != current || current.RuntimeID != challenge.Binding.RuntimeID || current.WorkspaceID != challenge.Binding.WorkspaceID {
		return reject()
	}
	if !validExecutionIdentity(identity) || identity != challenge.Identity || report.Identity != identity {
		return reject()
	}
	if challenge.Binding.Model != "" && identity.EffectiveModel != challenge.Binding.Model {
		return reject()
	}
	if report.Binding != challenge.Binding || report.Use != challenge.Use {
		return reject()
	}
	if now.IsZero() || challenge.IssuedAt.IsZero() || challenge.ExpiresAt.IsZero() || challenge.IssuedAt.After(now) || !challenge.ExpiresAt.After(now) || !challenge.ExpiresAt.After(challenge.IssuedAt) || challenge.ExpiresAt.Sub(challenge.IssuedAt) > evidenceMaxLifetime {
		return reject()
	}
	required, ok := requiredEvidenceFeatures(challenge.Use)
	if !ok || (challenge.Use == ProbeEvidence && challenge.Binding.Route != "primary") {
		return reject()
	}
	approved, ok := v.approved[AdapterContract{Provider: current.Provider, Version: report.AdapterContract}]
	if !ok || approved&report.Features&required != required {
		return reject()
	}
	return VerifiedEvidence{binding: challenge.Binding, connection: current, identity: identity, use: challenge.Use, since: challenge.IssuedAt, until: challenge.ExpiresAt, features: approved & report.Features, verified: true}, nil
}
func (v VerifiedEvidence) matches(binding TargetBinding, current RuntimeConnection, identity ExecutionIdentity, use EvidenceUse, now time.Time) bool {
	required, ok := requiredEvidenceFeatures(use)
	return ok && v.verified && validBinding(binding) && validConnection(current) && validExecutionIdentity(identity) && !now.IsZero() && !now.Before(v.since) && now.Before(v.until) && v.binding == binding && v.connection == current && v.identity == identity && v.use == use && v.features&required == required
}
func requiredEvidenceFeatures(use EvidenceUse) (EvidenceFeature, bool) {
	switch use {
	case TaskEvidence:
		return taskEvidenceFeatures, true
	case ProbeEvidence:
		return probeEvidenceFeatures, true
	}
	return 0, false
}
func validBinding(b TargetBinding) bool {
	return nonzeroUUID(b.AgentID) && nonzeroUUID(b.WorkspaceID) && nonzeroUUID(b.AgentOwnerID) && nonzeroUUID(b.RuntimeID) && nonzeroUUID(b.PrimaryGeneration) && nonzeroUUID(b.FallbackGeneration) && b.RuntimeMode != "" && (b.Route == "primary" || b.Route == "fallback")
}
func validConnection(c RuntimeConnection) bool {
	return nonzeroUUID(c.RuntimeID) && nonzeroUUID(c.WorkspaceID) && nonzeroUUID(c.OwnerID) && nonzeroUUID(c.SessionID) && c.Provider != ""
}
func validExecutionIdentity(i ExecutionIdentity) bool {
	return nonzeroUUID(i.CredentialEpoch) && nonzeroUUID(i.ConfigurationEpoch) && i.EffectiveModel != ""
}
