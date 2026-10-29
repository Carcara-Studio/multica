package service

// A route marker is immutable enqueue intent, not permission to execute. There
// is no production strict/native route resolver yet; do not use ordinary Agent
// payload construction or token issuance to silently reinterpret marked work.
func unresolvedExecutionRouteError() *ClaimDeliveryAuthzError {
	return &ClaimDeliveryAuthzError{
		Reason: "error_execution_route_unavailable",
		Detail: "persisted execution route has no approved production resolver",
	}
}
