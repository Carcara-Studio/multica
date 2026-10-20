package projecthierarchy

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func briefingTestRow(t *testing.T, workspace, project, manager pgtype.UUID) db.ProjectAgentMembership {
	t.Helper()
	return db.ProjectAgentMembership{
		ID:                  testUUID(t),
		WorkspaceID:         workspace,
		ProjectID:           project,
		AgentID:             testUUID(t),
		ManagerMembershipID: manager,
		Title:               "Role",
	}
}

func TestBriefingContextOneHopAndProjectScope(t *testing.T) {
	workspace, project := testUUID(t), testUUID(t)
	root := briefingTestRow(t, workspace, project, pgtype.UUID{})
	self := briefingTestRow(t, workspace, project, root.ID)
	report := briefingTestRow(t, workspace, project, self.ID)
	grandchild := briefingTestRow(t, workspace, project, report.ID)
	peer := briefingTestRow(t, workspace, project, root.ID)
	archived := briefingTestRow(t, workspace, project, self.ID)
	archived.ArchivedAt.Valid = true
	rows := []db.ProjectAgentMembership{root, self, report, grandchild, peer, archived}
	view, ok := BuildBriefingContext(rows, workspace, project, self.AgentID)
	if !ok || view.Self.ID != self.ID || view.Manager == nil || view.Manager.ID != root.ID ||
		len(view.DirectReports) != 1 || view.DirectReports[0].ID != report.ID {
		t.Fatal("briefing did not preserve exactly the active one-hop relationships")
	}
	rootView, ok := BuildBriefingContext(rows, workspace, project, root.AgentID)
	if !ok || rootView.Manager != nil || len(rootView.DirectReports) != 2 {
		t.Fatal("root context included descendants beyond direct reports")
	}
	reversed := append([]db.ProjectAgentMembership(nil), rows...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	again, ok := BuildBriefingContext(reversed, workspace, project, root.AgentID)
	if !ok || again.DirectReports[0].ID != rootView.DirectReports[0].ID || again.DirectReports[1].ID != rootView.DirectReports[1].ID {
		t.Fatal("report ordering changed with query order")
	}
	if _, ok := BuildBriefingContext(rows, workspace, testUUID(t), self.AgentID); ok {
		t.Fatal("another Project reused this role")
	}
	if _, ok := BuildBriefingContext(rows, testUUID(t), project, self.AgentID); ok {
		t.Fatal("another workspace reused this role")
	}
	if _, ok := BuildBriefingContext(rows, workspace, project, testUUID(t)); ok {
		t.Fatal("nonmember received hierarchy context")
	}
}

func TestBriefingContextRejectsInvalidOrExcessiveData(t *testing.T) {
	for _, name := range []string{"cycle", "missing manager", "two roots", "duplicate membership", "duplicate agent", "foreign row", "archived self", "zero id", "long title", "invalid utf8", "too many rows", "too many reports"} {
		t.Run(name, func(t *testing.T) {
			workspace, project := testUUID(t), testUUID(t)
			root := briefingTestRow(t, workspace, project, pgtype.UUID{})
			self := briefingTestRow(t, workspace, project, root.ID)
			rows := []db.ProjectAgentMembership{root, self}
			switch name {
			case "cycle":
				rows[0].ManagerMembershipID = self.ID
			case "missing manager":
				rows[1].ManagerMembershipID = testUUID(t)
			case "two roots":
				rows[1].ManagerMembershipID = pgtype.UUID{}
			case "duplicate membership":
				rows = append(rows, self)
			case "duplicate agent":
				duplicate := briefingTestRow(t, workspace, project, root.ID)
				duplicate.AgentID = self.AgentID
				rows = append(rows, duplicate)
			case "foreign row":
				rows[0].WorkspaceID = testUUID(t)
			case "archived self":
				rows[1].ArchivedAt.Valid = true
			case "zero id":
				rows[1].ID = pgtype.UUID{Valid: true}
			case "long title":
				rows[1].Title = strings.Repeat("x", 257)
			case "invalid utf8":
				rows[1].Title = string([]byte{0xff})
			case "too many rows":
				rows = make([]db.ProjectAgentMembership, 1025)
			case "too many reports":
				for range 33 {
					rows = append(rows, briefingTestRow(t, workspace, project, self.ID))
				}
			}
			if view, ok := BuildBriefingContext(rows, workspace, project, self.AgentID); ok || view.Self.ID.Valid || len(view.DirectReports) != 0 {
				t.Fatal("invalid input produced partial context")
			}
		})
	}
}
