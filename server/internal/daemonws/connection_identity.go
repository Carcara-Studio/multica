package daemonws

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

type rpcConnectionKey struct{}

// RPCConnectionIdentity is a snapshot of server-authenticated scalar identity.
// It cannot be used to mint an RPC context or grant current DB authorization.
type RPCConnectionIdentity struct {
	ConnectionID     uuid.UUID
	DaemonID, UserID string
}

// RuntimeConnectionScope additionally binds the connection-time workspace
// scope. Current membership/owner authorization must still be checked in DB.
func RuntimeConnectionScope(ctx context.Context, runtimeID, workspaceID string) (RPCConnectionIdentity, bool) {
	if workspaceID == "" {
		return RPCConnectionIdentity{}, false
	}
	return runtimeConnectionScope(ctx, runtimeID, workspaceID)
}

// RuntimeConnectionID identifies the actual socket carrying this RPC, never a
// daemon-supplied ID or an arbitrary socket watching the same runtime. The
// runtime must remain attached to this live connection. HTTP/background contexts
// have no such identity. A copied ClientIdentity cannot mint one.
//
// This is a point-in-time transport snapshot, not runtime authorization, an
// execution lease or credential/config evidence. Callers must recheck it at use
// time alongside current DB authorization and target-specific identity epochs.
func RuntimeConnectionID(ctx context.Context, runtimeID string) (uuid.UUID, bool) {
	scope, ok := runtimeConnectionScope(ctx, runtimeID, "")
	return scope.ConnectionID, ok
}

func runtimeConnectionScope(ctx context.Context, runtimeID, workspaceID string) (RPCConnectionIdentity, bool) {
	c, ok := ctx.Value(rpcConnectionKey{}).(*client)
	if !ok || c == nil || c.hub == nil || c.ctx == nil || runtimeID == "" || ctx.Err() != nil || c.ctx.Err() != nil || c.connectionID == uuid.Nil {
		return RPCConnectionIdentity{}, false
	}
	// Match the existing register/invalidate/unregister lock order. Never call
	// a handler or touch DB while holding the hub lock.
	c.hub.mu.RLock()
	defer c.hub.mu.RUnlock()
	if !c.hub.clients[c] || !c.hub.byRuntime[runtimeID][c] || !c.allowsRuntime(runtimeID) || (workspaceID != "" && !slices.Contains(c.identity.AuthorizedWorkspaceIDs(), workspaceID)) || ctx.Err() != nil || c.ctx.Err() != nil {
		return RPCConnectionIdentity{}, false
	}
	return RPCConnectionIdentity{ConnectionID: c.connectionID, DaemonID: c.identity.DaemonID, UserID: c.identity.UserID}, true
}
