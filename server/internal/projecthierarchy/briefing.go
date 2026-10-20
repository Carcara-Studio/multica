package projecthierarchy

import (
	"bytes"
	"sort"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// BriefingContext is informational, never delegation or invocation authority.
type BriefingContext struct {
	Self          db.ProjectAgentMembership
	Manager       *db.ProjectAgentMembership
	DirectReports []db.ProjectAgentMembership
}

// BuildBriefingContext projects one scoped, active tree to one-hop context.
// Bounds apply before recursive validation; invalid or excessive data is omitted
// as a whole rather than presenting a partial roster as complete.
func BuildBriefingContext(rows []db.ProjectAgentMembership, workspaceID, projectID, agentID pgtype.UUID) (BriefingContext, bool) {
	empty := BriefingContext{}
	if !briefingUUID(workspaceID) || !briefingUUID(projectID) || !briefingUUID(agentID) || len(rows) > 1024 {
		return empty, false
	}
	ids := make(map[pgtype.UUID]bool)
	agents := make(map[pgtype.UUID]bool)
	var self *db.ProjectAgentMembership
	for _, row := range rows {
		if row.WorkspaceID != workspaceID || row.ProjectID != projectID {
			return empty, false
		}
		if row.ArchivedAt.Valid {
			continue
		}
		if !briefingUUID(row.ID) || !briefingUUID(row.AgentID) || ids[row.ID] || agents[row.AgentID] {
			return empty, false
		}
		ids[row.ID], agents[row.AgentID] = true, true
		if row.AgentID == agentID {
			copy := row
			self = &copy
		}
	}
	if self == nil || validateActiveTree(rows) != nil {
		return empty, false
	}
	result := BriefingContext{Self: *self, DirectReports: []db.ProjectAgentMembership{}}
	for _, row := range rows {
		if row.ArchivedAt.Valid {
			continue
		}
		if self.ManagerMembershipID.Valid && row.ID == self.ManagerMembershipID {
			copy := row
			result.Manager = &copy
		}
		if row.ManagerMembershipID.Valid && row.ManagerMembershipID == self.ID {
			result.DirectReports = append(result.DirectReports, row)
		}
	}
	if len(result.DirectReports) > 32 || !briefingTitle(result.Self.Title) {
		return empty, false
	}
	if result.Manager != nil && !briefingTitle(result.Manager.Title) {
		return empty, false
	}
	for _, row := range result.DirectReports {
		if !briefingTitle(row.Title) {
			return empty, false
		}
	}
	sort.Slice(result.DirectReports, func(i, j int) bool {
		return bytes.Compare(result.DirectReports[i].ID.Bytes[:], result.DirectReports[j].ID.Bytes[:]) < 0
	})
	return result, true
}

func briefingUUID(id pgtype.UUID) bool {
	return id.Valid && id.Bytes != [16]byte{}
}

func briefingTitle(title string) bool {
	return len(title) <= 256 && utf8.ValidString(title)
}
