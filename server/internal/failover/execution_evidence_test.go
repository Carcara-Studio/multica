package failover

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func evidenceFixture(use EvidenceUse) (EvidenceVerifier, EvidenceChallenge, EvidenceReport, time.Time) {
	now := time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC)
	binding := TargetBinding{AgentID: newGeneration(), WorkspaceID: newGeneration(), AgentOwnerID: newGeneration(), RuntimeID: newGeneration(), PrimaryGeneration: newGeneration(), FallbackGeneration: newGeneration(), Route: "primary", RuntimeMode: "local", Model: "verified-model"}
	connection := RuntimeConnection{RuntimeID: binding.RuntimeID, WorkspaceID: binding.WorkspaceID, OwnerID: binding.AgentOwnerID, SessionID: newGeneration(), Provider: "claude"}
	identity := ExecutionIdentity{CredentialEpoch: newGeneration(), ConfigurationEpoch: newGeneration(), EffectiveModel: binding.Model}
	challenge := EvidenceChallenge{Token: newGeneration(), Binding: binding, Connection: connection, Identity: identity, Use: use, IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute)}
	report := EvidenceReport{Version: 1, Challenge: challenge.Token, Binding: binding, Connection: connection, Identity: identity, Use: use, AdapterContract: "fixture-only/v1", Features: taskEvidenceFeatures | probeEvidenceFeatures}
	v := newEvidenceVerifier(map[AdapterContract]EvidenceFeature{{Provider: connection.Provider, Version: report.AdapterContract}: report.Features})
	return v, challenge, report, now
}

func TestTargetEvidenceRequiresServerApprovalAndExactAuthenticatedBinding(t *testing.T) {
	for _, use := range []EvidenceUse{TaskEvidence, ProbeEvidence} {
		t.Run(string(use), func(t *testing.T) {
			for _, tc := range []struct {
				name   string
				change func(*EvidenceChallenge, *EvidenceReport, *RuntimeConnection, *ExecutionIdentity, *time.Time)
			}{
				{"version", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Version = 2
				}},
				{"nonce", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Challenge = newGeneration()
				}},
				{"agent", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Binding.AgentID = newGeneration()
				}},
				{"workspace", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Binding.WorkspaceID = newGeneration()
				}},
				{"target route", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Binding.Route = "fallback"
				}},
				{"primary generation", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Binding.PrimaryGeneration = newGeneration()
				}},
				{"fallback generation", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Binding.FallbackGeneration = newGeneration()
				}},
				{"thinking", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Binding.ThinkingLevel = "different"
				}},
				{"tier", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Binding.ServiceTier = "different"
				}},
				{"runtime", func(_ *EvidenceChallenge, _ *EvidenceReport, c *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					c.RuntimeID = newGeneration()
				}},
				{"owner", func(_ *EvidenceChallenge, _ *EvidenceReport, c *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					c.OwnerID = newGeneration()
				}},
				{"session", func(_ *EvidenceChallenge, _ *EvidenceReport, c *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					c.SessionID = newGeneration()
				}},
				{"credentials", func(_ *EvidenceChallenge, _ *EvidenceReport, _ *RuntimeConnection, i *ExecutionIdentity, _ *time.Time) {
					i.CredentialEpoch = newGeneration()
				}},
				{"local config", func(_ *EvidenceChallenge, _ *EvidenceReport, _ *RuntimeConnection, i *ExecutionIdentity, _ *time.Time) {
					i.ConfigurationEpoch = newGeneration()
				}},
				{"effective model", func(_ *EvidenceChallenge, _ *EvidenceReport, _ *RuntimeConnection, i *ExecutionIdentity, _ *time.Time) {
					i.EffectiveModel = "other-model"
				}},
				{"purpose", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.Use = "unsupported"
				}},
				{"unapproved adapter", func(_ *EvidenceChallenge, r *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					r.AdapterContract = "unapproved-private-input"
				}},
				{"expired inclusive", func(c *EvidenceChallenge, _ *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, n *time.Time) {
					*n = c.ExpiresAt
				}},
				{"future issue", func(c *EvidenceChallenge, _ *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, n *time.Time) {
					c.IssuedAt = n.Add(time.Second)
				}},
				{"unbounded lifetime", func(c *EvidenceChallenge, _ *EvidenceReport, _ *RuntimeConnection, _ *ExecutionIdentity, _ *time.Time) {
					c.ExpiresAt = c.IssuedAt.Add(evidenceMaxLifetime + time.Nanosecond)
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					v, c, r, now := evidenceFixture(use)
					current, identity := c.Connection, c.Identity
					tc.change(&c, &r, &current, &identity, &now)
					proof, err := v.Verify(c, r, current, identity, now)
					if !errors.Is(err, ErrExecutionEvidence) || proof.verified {
						t.Fatal("mismatched evidence granted authority")
					}
					if strings.Contains(err.Error(), "private-input") {
						t.Fatal("evidence error echoed report contents")
					}
				})
			}
			v, c, r, now := evidenceFixture(use)
			proof, err := v.Verify(c, r, c.Connection, c.Identity, now)
			if err != nil || !proof.matches(c.Binding, c.Connection, c.Identity, use, now) {
				t.Fatal("approved fixture evidence rejected", err)
			}
			if _, err = NewEvidenceVerifier().Verify(c, r, c.Connection, c.Identity, now); !errors.Is(err, ErrExecutionEvidence) {
				t.Fatal("default policy approved daemon claims")
			}
			if proof.matches(c.Binding, c.Connection, c.Identity, use, c.ExpiresAt) || proof.matches(c.Binding, c.Connection, c.Identity, use, c.IssuedAt.Add(-time.Nanosecond)) {
				t.Fatal("sealed evidence survived invalid time window")
			}
		})
	}
}

func TestEveryCapabilityMustBeReportedAndServerApproved(t *testing.T) {
	for _, use := range []EvidenceUse{TaskEvidence, ProbeEvidence} {
		needed, _ := requiredEvidenceFeatures(use)
		for bit := EvidenceFeature(1); bit <= ProbeNoSession; bit <<= 1 {
			if needed&bit == 0 {
				continue
			}
			v, c, r, now := evidenceFixture(use)
			r.Features &^= bit
			if _, err := v.Verify(c, r, c.Connection, c.Identity, now); !errors.Is(err, ErrExecutionEvidence) {
				t.Fatal("missing advertised guarantee accepted", use, bit)
			}
			_, c, r, now = evidenceFixture(use)
			v = newEvidenceVerifier(map[AdapterContract]EvidenceFeature{{Provider: c.Connection.Provider, Version: r.AdapterContract}: r.Features &^ bit})
			if _, err := v.Verify(c, r, c.Connection, c.Identity, now); !errors.Is(err, ErrExecutionEvidence) {
				t.Fatal("daemon self-approved unsupported guarantee", use, bit)
			}
		}
	}
	v, c, r, now := evidenceFixture(ProbeEvidence)
	c.Binding.Route = "fallback"
	r.Binding = c.Binding
	if _, err := v.Verify(c, r, c.Connection, c.Identity, now); !errors.Is(err, ErrExecutionEvidence) {
		t.Fatal("fallback evidence became primary recovery capability")
	}
}

func TestEvidenceCannotTransferAcrossUseOrTargetScopedAccountEpoch(t *testing.T) {
	v, c, r, now := evidenceFixture(TaskEvidence)
	proof, err := v.Verify(c, r, c.Connection, c.Identity, now)
	if err != nil {
		t.Fatal(err)
	}
	if proof.matches(c.Binding, c.Connection, c.Identity, ProbeEvidence, now) {
		t.Fatal("task launch became tool-free inference proof")
	}
	current := c.Connection
	current.SessionID = newGeneration()
	if proof.matches(c.Binding, current, c.Identity, TaskEvidence, now) {
		t.Fatal("old connection evidence reused")
	}
	identity := c.Identity
	identity.CredentialEpoch = newGeneration()
	if proof.matches(c.Binding, c.Connection, identity, TaskEvidence, now) {
		t.Fatal("same-runtime credential replacement reused evidence")
	}
	binding := c.Binding
	binding.Route = "fallback"
	if proof.matches(binding, c.Connection, c.Identity, TaskEvidence, now) {
		t.Fatal("same-runtime route evidence reused")
	}
	var empty VerifiedEvidence
	if empty.matches(c.Binding, c.Connection, c.Identity, TaskEvidence, now) {
		t.Fatal("zero evidence authorized execution")
	}
}
