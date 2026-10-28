package service

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// NativeQuotaFailureSource carries a diagnostic preflight, not an authorization
// capability. The handler supplies IDs from authenticated MDT context and loaded
// rows, never adjacent JSON identity fields. The terminal UPDATE rechecks them.
type NativeQuotaFailureSource struct {
	TaskID, RuntimeID, WorkspaceID pgtype.UUID
	DaemonID                       string
	Observation                    agent.NativeQuotaObservation
}

func (s *NativeQuotaFailureSource) apply(p *db.FailAgentTaskParams) {
	if s == nil || !s.TaskID.Valid || s.TaskID != p.ID || !s.RuntimeID.Valid || !s.WorkspaceID.Valid || s.DaemonID == "" || !s.Observation.HasSupportedShape() ||
		!(s.Observation.SessionID == p.SessionID.String || (!p.SessionID.Valid && p.SessionRolloutMissing)) {
		return
	}
	p.NativeQuota = true
	p.NativeQuotaRuntimeID = s.RuntimeID
	p.NativeQuotaWorkspaceID = s.WorkspaceID
	p.NativeQuotaDaemonID = s.DaemonID
}
