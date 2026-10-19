package daemonws

import (
	"context"

	"github.com/google/uuid"
)

type rpcConnectionKey struct{}

// RuntimeConnectionID identifies the actual socket carrying this RPC, never a
// daemon-supplied ID or an arbitrary socket watching the same runtime. The
// runtime must remain attached to this live connection. HTTP/background contexts
// have no such identity. A copied ClientIdentity cannot mint one.
//
// This is a point-in-time transport snapshot, not runtime authorization, an
// execution lease or credential/config evidence. Callers must recheck it at use
// time alongside current DB authorization and target-specific identity epochs.
func RuntimeConnectionID(ctx context.Context, runtimeID string) (uuid.UUID, bool) {
	c, ok := ctx.Value(rpcConnectionKey{}).(*client)
	if !ok || c == nil || c.hub == nil || c.ctx == nil || runtimeID == "" || ctx.Err() != nil || c.ctx.Err() != nil || c.connectionID == uuid.Nil {
		return uuid.Nil, false
	}
	// Match the existing register/invalidate/unregister lock order. Never call
	// a handler or touch DB while holding the hub lock.
	c.hub.mu.RLock()
	defer c.hub.mu.RUnlock()
	if !c.hub.clients[c] || !c.hub.byRuntime[runtimeID][c] || !c.allowsRuntime(runtimeID) || ctx.Err() != nil || c.ctx.Err() != nil {
		return uuid.Nil, false
	}
	return c.connectionID, true
}
