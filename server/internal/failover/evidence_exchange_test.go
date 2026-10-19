package failover

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type fixtureTargetObserver struct {
	mu         sync.Mutex
	identity   ExecutionIdentity
	calls      int
	onIdentity func(int)
}

func (*fixtureTargetObserver) Catalog(context.Context, RuntimeConnection) (agent.Catalog, error) {
	return agent.Catalog{}, nil
}
func (o *fixtureTargetObserver) Identity(_ context.Context, _ TargetBinding, _ RuntimeConnection) (ExecutionIdentity, error) {
	o.mu.Lock()
	o.calls++
	n, hook := o.calls, o.onIdentity
	o.mu.Unlock()
	if hook != nil {
		hook(n)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.identity, nil
}
func (o *fixtureTargetObserver) setHook(hook func(int)) {
	o.mu.Lock()
	o.onIdentity = hook
	o.mu.Unlock()
}

func exchangeFixture(t *testing.T) (*stateFixture, EvidenceExchange, EvidenceSelection, *fixtureTargetObserver) {
	t.Helper()
	f, cfg := newTargetFixture(t)
	// Known provider/default model; no CLI/catalog/account access. The protected
	// target document is complete even though this exchange never returns it.
	f.fx.Exec(t, `UPDATE agent_runtime SET provider='codex' WHERE id=ANY($1::uuid[])`, []string{uuidString(f.runtime), uuidString(cfg.RuntimeID)})
	f.fx.Exec(t, `UPDATE agent_fallback_config SET model='',execution_config='{"version":1,"custom_env":{},"custom_args":[],"runtime_config":{},"mcp_config":null}' WHERE agent_id=$1`, f.row.AgentID)
	o := &fixtureTargetObserver{identity: ExecutionIdentity{CredentialEpoch: newGeneration(), ConfigurationEpoch: newGeneration(), EffectiveModel: "fixture-default"}}
	e := EvidenceExchange{manager: ConfigManager{Queries: f.q, Transactions: f.pool}, observer: o, verifier: newEvidenceVerifier(map[AdapterContract]EvidenceFeature{{Provider: "codex", Version: "fixture-only/v1"}: taskEvidenceFeatures | probeEvidenceFeatures})}
	s := EvidenceSelection{AgentID: f.row.AgentID, WorkspaceID: f.row.WorkspaceID, RuntimeID: f.runtime, Route: "primary", Use: TaskEvidence}
	return f, e, s, o
}

type exchangeOutcome struct {
	challenge EvidenceChallenge
	proof     VerifiedEvidence
	err       error
}
type exchangeOperation func(context.Context) exchangeOutcome

// Test-only RPC dispatch: no new public method or wire schema is registered by
// the implementation. Operations receive a real hub-created private context.
func exchangeSocket(t *testing.T, identity daemonws.ClientIdentity) (*daemonws.Hub, func(exchangeOperation) exchangeOutcome) {
	t.Helper()
	h := daemonws.NewHub()
	operations := make(chan exchangeOperation, 1)
	results := make(chan exchangeOutcome, 1)
	h.SetRPCHandler(func(ctx context.Context, _ daemonws.ClientIdentity, _ string, _ json.RawMessage) (int, json.RawMessage, error) {
		results <- (<-operations)(ctx)
		return http.StatusOK, json.RawMessage(`{}`), nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.HandleWebSocket(w, r, identity) }))
	t.Cleanup(server.Close)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return h, func(op exchangeOperation) exchangeOutcome {
		t.Helper()
		operations <- op
		if err := conn.WriteJSON(protocol.Message{Type: protocol.EventDaemonRPCRequest, Payload: json.RawMessage(`{"request_id":"fixture","method":"fixture.exchange"}`)}); err != nil {
			t.Fatal(err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
			t.Fatal(err)
		}
		for {
			var message protocol.Message
			if err := conn.ReadJSON(&message); err != nil {
				t.Fatal(err)
			}
			if message.Type == protocol.EventDaemonRPCResponse {
				break
			}
		}
		select {
		case result := <-results:
			return result
		case <-time.After(3 * time.Second):
			t.Fatal("exchange result missing")
			return exchangeOutcome{}
		}
	}
}

func exchangeIdentity(f *stateFixture, runtime string) daemonws.ClientIdentity {
	return daemonws.ClientIdentity{UserID: f.fx.UserID, HumanActor: true, WorkspaceID: f.fx.WorkspaceID, RuntimeIDs: []string{runtime}}
}

func issueExchange(e EvidenceExchange, s EvidenceSelection) exchangeOperation {
	return func(ctx context.Context) exchangeOutcome {
		c, err := e.Issue(ctx, s)
		return exchangeOutcome{challenge: c, err: err}
	}
}
func consumeExchange(e EvidenceExchange, s EvidenceSelection, c EvidenceChallenge) exchangeOperation {
	return func(ctx context.Context) exchangeOutcome {
		proof, err := e.Consume(ctx, s, ledgerReport(c))
		return exchangeOutcome{proof: proof, err: err}
	}
}

func TestEvidenceExchangeCommitsLiveOwnerIssueAndSingleConsumption(t *testing.T) {
	f, e, s, _ := exchangeFixture(t)
	_, run := exchangeSocket(t, exchangeIdentity(f, uuidString(s.RuntimeID)))
	issued := run(issueExchange(e, s))
	if issued.err != nil || !nonzeroUUID(issued.challenge.Token) {
		t.Fatal("live owner issuance failed", issued.err)
	}
	if saved := storedState(t, f.q, f); saved.EvidenceToken != issued.challenge.Token {
		t.Fatal("challenge escaped before commit")
	}
	consumed := run(consumeExchange(e, s, issued.challenge))
	if consumed.err != nil || !consumed.proof.verified {
		t.Fatal("live owner evidence failed", consumed.err)
	}
	if storedState(t, f.q, f).EvidenceToken.Valid {
		t.Fatal("proof escaped without committed consumption")
	}
	if repeated := run(consumeExchange(e, s, issued.challenge)); !errors.Is(repeated.err, ErrExecutionEvidence) || repeated.proof.verified {
		t.Fatal("exchange accepted replay", repeated.err)
	}
}

func TestEvidenceExchangeDefaultAndForgedContextsFailClosed(t *testing.T) {
	if _, err := NewEvidenceExchange(ConfigManager{}).Issue(t.Context(), EvidenceSelection{}); !errors.Is(err, ErrExecutionEvidence) {
		t.Fatal("default exchange did not fail closed", err)
	}
	f, e, s, _ := exchangeFixture(t)
	if c, err := e.Issue(t.Context(), s); !errors.Is(err, ErrExecutionEvidence) || c.Token.Valid {
		t.Fatal("background/body identity minted authority", err)
	}
	_, run := exchangeSocket(t, exchangeIdentity(f, uuidString(s.RuntimeID)))
	if got := run(issueExchange(NewEvidenceExchange(e.manager), s)); !errors.Is(got.err, ErrExecutionEvidence) || got.challenge.Token.Valid {
		t.Fatal("production factory approved fixture observer", got.err)
	}
	bad := s
	bad.WorkspaceID = newGeneration()
	if got := run(issueExchange(e, bad)); !errors.Is(got.err, ErrExecutionEvidence) {
		t.Fatal("workspace selection widened RPC scope", got.err)
	}
	bad = s
	bad.RuntimeID = newGeneration()
	if got := run(issueExchange(e, bad)); !errors.Is(got.err, ErrExecutionEvidence) {
		t.Fatal("runtime selection widened RPC scope", got.err)
	}
	if storedState(t, f.q, f).EvidenceToken.Valid {
		t.Fatal("failed authorization published pending challenge")
	}
}

func TestEvidenceExchangeSupportsFallbackAndSeparatePrimaryProbeSelection(t *testing.T) {
	f, e, s, _ := exchangeFixture(t)
	cfg := storedConfig(t, f.q, db.AgentFallbackConfig{AgentID: s.AgentID, WorkspaceID: s.WorkspaceID})
	identity := exchangeIdentity(f, uuidString(s.RuntimeID))
	identity.RuntimeIDs = append(identity.RuntimeIDs, uuidString(cfg.RuntimeID))
	_, run := exchangeSocket(t, identity)
	for _, kind := range []string{"fallback", "primary probe"} {
		selection := s
		if kind == "fallback" {
			selection.Route = "fallback"
			selection.RuntimeID = cfg.RuntimeID
		} else {
			selection.Use = ProbeEvidence
		}
		issued := run(issueExchange(e, selection))
		if issued.err != nil || issued.challenge.Binding.Route != selection.Route || issued.challenge.Use != selection.Use {
			t.Fatal("selected capability challenge failed", kind, issued.err)
		}
		if consumed := run(consumeExchange(e, selection, issued.challenge)); consumed.err != nil || !consumed.proof.verified {
			t.Fatal("selected capability report failed", kind, consumed.err)
		}
	}
}

func TestEvidenceExchangeCurrentMembershipAndOwnerPolicy(t *testing.T) {
	for _, kind := range []string{"member denied", "membership removed", "admin allowed", "daemon denied", "machine owner denied"} {
		t.Run(kind, func(t *testing.T) {
			f, e, s, _ := exchangeFixture(t)
			identity := exchangeIdentity(f, uuidString(s.RuntimeID))
			if kind == "member denied" || kind == "admin allowed" {
				user := f.fx.User(t, "Other member", "exchange-"+uuidString(newGeneration())+"@example.test")
				role := "member"
				if kind == "admin allowed" {
					role = "admin"
				}
				f.fx.Member(t, f.fx.WorkspaceID, user, role)
				identity.UserID = user
			}
			if kind == "daemon denied" {
				identity.DaemonID = "daemon-token"
			}
			if kind == "machine owner denied" {
				identity.HumanActor = false
			}
			_, run := exchangeSocket(t, identity)
			if kind == "membership removed" {
				f.fx.Exec(t, `DELETE FROM member WHERE user_id=$1 AND workspace_id=$2`, f.fx.UserID, f.fx.WorkspaceID)
			}
			got := run(issueExchange(e, s))
			if kind == "admin allowed" {
				if got.err != nil || !got.challenge.Token.Valid {
					t.Fatal("admin preflight denied", got.err)
				}
			} else if got.err == nil || got.challenge.Token.Valid {
				t.Fatal("stale/unauthorized actor received challenge")
			}
		})
	}
}

func TestEvidenceExchangeLateIdentityChangeAndRuntimeDetachRollback(t *testing.T) {
	for _, kind := range []string{"identity issue", "identity consume", "detach issue", "detach consume"} {
		t.Run(kind, func(t *testing.T) {
			f, e, s, o := exchangeFixture(t)
			h, run := exchangeSocket(t, exchangeIdentity(f, uuidString(s.RuntimeID)))
			var prior EvidenceChallenge
			consume := strings.HasSuffix(kind, "consume")
			if consume {
				got := run(issueExchange(e, s))
				if got.err != nil {
					t.Fatal(got.err)
				}
				prior = got.challenge
			}
			trigger := 2
			if consume {
				trigger = 4
			}
			o.setHook(func(call int) {
				if call != trigger {
					return
				}
				if strings.HasPrefix(kind, "identity") {
					o.mu.Lock()
					o.identity.CredentialEpoch = newGeneration()
					o.mu.Unlock()
				} else {
					h.NotifyRuntimeGone(uuidString(s.RuntimeID))
				}
			})
			op := issueExchange(e, s)
			if consume {
				op = consumeExchange(e, s, prior)
			}
			got := run(op)
			if !errors.Is(got.err, ErrExecutionEvidence) || got.challenge.Token.Valid || got.proof.verified {
				t.Fatal("late observation escaped rollback", got.err)
			}
			saved := storedState(t, f.q, f)
			if consume {
				if saved.EvidenceToken != prior.Token {
					t.Fatal("failed final validation consumed pending nonce")
				}
			} else if saved.EvidenceToken.Valid {
				t.Fatal("failed final validation published pending nonce")
			}
		})
	}
}

func TestEvidenceExchangeCommitFailurePublishesNoAuthority(t *testing.T) {
	f, e, s, _ := exchangeFixture(t)
	_, run := exchangeSocket(t, exchangeIdentity(f, uuidString(s.RuntimeID)))
	failing := e
	failing.manager.Transactions = failingConfigStarter{base: f.pool, mode: "commit"}
	got := run(issueExchange(failing, s))
	if !errors.Is(got.err, ErrEvidenceStore) || got.challenge.Token.Valid || storedState(t, f.q, f).EvidenceToken.Valid || strings.Contains(got.err.Error(), "private-value") {
		t.Fatal("failed commit published challenge", got.err)
	}
	issued := run(issueExchange(e, s))
	if issued.err != nil {
		t.Fatal(issued.err)
	}
	got = run(consumeExchange(failing, s, issued.challenge))
	if !errors.Is(got.err, ErrEvidenceStore) || got.proof.verified || storedState(t, f.q, f).EvidenceToken != issued.challenge.Token {
		t.Fatal("failed commit published proof or lost nonce", got.err)
	}
	if retry := run(consumeExchange(e, s, issued.challenge)); retry.err != nil || !retry.proof.verified {
		t.Fatal("commit rollback did not preserve usable nonce", retry.err)
	}
}

func TestEvidenceExchangeStaleConfigAndRejectedReports(t *testing.T) {
	f, e, s, _ := exchangeFixture(t)
	_, run := exchangeSocket(t, exchangeIdentity(f, uuidString(s.RuntimeID)))
	issued := run(issueExchange(e, s))
	if issued.err != nil {
		t.Fatal(issued.err)
	}
	bad := ledgerReport(issued.challenge)
	bad.Features &^= TargetSessionIsolation
	got := run(func(ctx context.Context) exchangeOutcome {
		p, err := e.Consume(ctx, s, bad)
		return exchangeOutcome{proof: p, err: err}
	})
	if !errors.Is(got.err, ErrExecutionEvidence) || got.proof.verified || storedState(t, f.q, f).EvidenceToken.Valid {
		t.Fatal("invalid report did not commit nonce burn", got.err)
	}
	issued = run(issueExchange(e, s))
	if issued.err != nil {
		t.Fatal(issued.err)
	}
	f.fx.Exec(t, `UPDATE agent SET archived_at=clock_timestamp() WHERE id=$1`, s.AgentID)
	if got := run(consumeExchange(e, s, issued.challenge)); got.err == nil || got.proof.verified {
		t.Fatal("archived Agent produced proof")
	}
}
