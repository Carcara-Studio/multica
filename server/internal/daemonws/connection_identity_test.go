package daemonws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type connectionObservation struct {
	ctx context.Context
	id  uuid.UUID
	ok  bool
}

func connectionTestHub() (*Hub, <-chan connectionObservation) {
	h := NewHub()
	observed := make(chan connectionObservation, 16)
	h.SetRPCHandler(func(ctx context.Context, _ ClientIdentity, method string, _ json.RawMessage) (int, json.RawMessage, error) {
		id, ok := RuntimeConnectionID(ctx, method)
		observed <- connectionObservation{ctx, id, ok}
		return http.StatusOK, json.RawMessage(`{}`), nil
	})
	return h, observed
}

func observeConnection(t *testing.T, observations <-chan connectionObservation) connectionObservation {
	t.Helper()
	select {
	case got := <-observations:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("RPC observation not received")
		return connectionObservation{}
	}
}

func TestRPCConnectionIdentityIsServerOwnedAndSocketScoped(t *testing.T) {
	h, observations := connectionTestHub()
	claimed := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	identity := ClientIdentity{DaemonID: "same-daemon", UserID: "same-user", RuntimeIDs: []string{"runtime-a"}, Capabilities: "connection_id=" + claimed.String()}
	first := dialRPCTestConn(t, h, identity)
	request := protocol.RPCRequestPayload{RequestID: "request", Method: "runtime-a", Body: json.RawMessage(`{"connection_id":"11111111-1111-4111-8111-111111111111"}`)}
	if got := sendRPCRequest(t, first, request); got.Status != http.StatusOK {
		t.Fatal("ordinary RPC changed", got.Status)
	}
	a := observeConnection(t, observations)
	if !a.ok || a.id == uuid.Nil || a.id == claimed {
		t.Fatal("live authorized socket has no identity")
	}
	sendRPCRequest(t, first, request)
	if b := observeConnection(t, observations); !b.ok || b.id != a.id {
		t.Fatal("identity changed within the same socket")
	}
	// Identical daemon/runtime/header claims do not identify a socket. Both
	// sockets may coexist; neither may borrow evidence from the other.
	second := dialRPCTestConn(t, h, identity)
	sendRPCRequest(t, second, request)
	b := observeConnection(t, observations)
	if !b.ok || b.id == uuid.Nil || b.id == a.id {
		t.Fatal("concurrent sockets shared identity")
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-a.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect did not cancel connection context")
	}
	if id, ok := RuntimeConnectionID(a.ctx, "runtime-a"); ok || id != uuid.Nil {
		t.Fatal("disconnected socket remained usable through sibling")
	}
	if id, ok := RuntimeConnectionID(b.ctx, "runtime-a"); !ok || id != b.id {
		t.Fatal("one socket disconnect invalidated its live sibling")
	}
	third := dialRPCTestConn(t, h, identity)
	sendRPCRequest(t, third, request)
	if c := observeConnection(t, observations); !c.ok || c.id == a.id || c.id == b.id {
		t.Fatal("reconnect reused an old socket identity")
	}
}

func TestRPCConnectionIdentityRejectsUnattachedRuntimeAndNonRPCContext(t *testing.T) {
	h, observations := connectionTestHub()
	a := dialRPCTestConn(t, h, ClientIdentity{RuntimeIDs: []string{"runtime-a"}})
	b := dialRPCTestConn(t, h, ClientIdentity{RuntimeIDs: []string{"runtime-b"}})
	sendRPCRequest(t, b, protocol.RPCRequestPayload{RequestID: "b", Method: "runtime-b"})
	if got := observeConnection(t, observations); !got.ok {
		t.Fatal("sibling fixture not connected")
	}
	for _, runtime := range []string{"runtime-b", "missing", ""} {
		sendRPCRequest(t, a, protocol.RPCRequestPayload{RequestID: "a", Method: runtime})
		if got := observeConnection(t, observations); got.ok || got.id != uuid.Nil {
			t.Fatal("socket borrowed another runtime's scope", runtime)
		}
	}
	if id, ok := RuntimeConnectionID(context.Background(), "runtime-a"); ok || id != uuid.Nil {
		t.Fatal("HTTP/background context minted socket identity")
	}
}

func TestRPCConnectionIdentityRevokedForDetachedRuntimeOnly(t *testing.T) {
	h, observations := connectionTestHub()
	conn := dialRPCTestConn(t, h, ClientIdentity{RuntimeIDs: []string{"runtime-a", "runtime-b"}})
	sendRPCRequest(t, conn, protocol.RPCRequestPayload{RequestID: "a", Method: "runtime-a"})
	a := observeConnection(t, observations)
	if !a.ok {
		t.Fatal("fixture has no connection identity")
	}
	h.NotifyRuntimeGone("runtime-a")
	if id, ok := RuntimeConnectionID(a.ctx, "runtime-a"); ok || id != uuid.Nil {
		t.Fatal("removed runtime retained connection identity")
	}
	if id, ok := RuntimeConnectionID(a.ctx, "runtime-b"); !ok || id != a.id {
		t.Fatal("runtime removal invalidated unrelated attached runtime")
	}
	// Reading a stale identity must remain safe while teardown/invalidation
	// competes with consumers. The race suite owns this synchronization check.
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			for range 100 {
				RuntimeConnectionID(a.ctx, "runtime-b")
			}
		})
	}
	h.NotifyRuntimeGone("runtime-b")
	readers.Wait()
	if id, ok := RuntimeConnectionID(a.ctx, "runtime-b"); ok || id != uuid.Nil {
		t.Fatal("concurrently detached runtime remained usable")
	}
}

func TestRPCConnectionIdentityRejectsExpiredRequestWithoutClosingSocket(t *testing.T) {
	h := NewHub()
	observed := make(chan connectionObservation, 2)
	h.SetRPCHandler(func(ctx context.Context, _ ClientIdentity, method string, _ json.RawMessage) (int, json.RawMessage, error) {
		if method == "expire" {
			<-ctx.Done()
		}
		id, ok := RuntimeConnectionID(ctx, "runtime-a")
		observed <- connectionObservation{ctx, id, ok}
		return http.StatusOK, json.RawMessage(`{}`), nil
	})
	conn := dialRPCTestConn(t, h, ClientIdentity{RuntimeIDs: []string{"runtime-a"}})
	sendRPCRequest(t, conn, protocol.RPCRequestPayload{RequestID: "expire", Method: "expire", TimeoutMs: 1})
	if got := observeConnection(t, observed); got.ok || got.id != uuid.Nil {
		t.Fatal("expired RPC retained live connection evidence")
	}
	sendRPCRequest(t, conn, protocol.RPCRequestPayload{RequestID: "live", Method: "live"})
	if got := observeConnection(t, observed); !got.ok || got.id == uuid.Nil {
		t.Fatal("request expiry incorrectly revoked socket")
	}
}
