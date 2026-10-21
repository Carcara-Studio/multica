package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func escalationHTTPRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Auth(h.Queries, nil, nil, nil))
	r.Use(middleware.RequireWorkspaceMember(h.Queries))
	r.Post("/api/issues/{id}/hierarchy-escalations", h.CreateHierarchyEscalation)
	r.Get("/api/issues/{id}/hierarchy-escalations", h.ListHierarchyEscalations)
	return r
}
func escalationHTTP(t *testing.T, token, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	r := newRequest(method, path, body)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	escalationHTTPRouter(testHandler).ServeHTTP(w, r)
	return w
}
func escalationHTTPFixture(t *testing.T) hierarchyHTTPFixture {
	t.Helper()
	f := newHierarchyHTTPFixture(t)
	dbfx.Cleanup(t, "DELETE FROM project_hierarchy_escalation WHERE source_issue_id=$1", f.parent)
	dbfx.Cleanup(t, "DELETE FROM inbox_item WHERE issue_id IN (SELECT decision_issue_id FROM project_hierarchy_escalation WHERE source_issue_id=$1)", f.parent)
	dbfx.Cleanup(t, "DELETE FROM agent_task_queue WHERE issue_id IN (SELECT decision_issue_id FROM project_hierarchy_escalation WHERE source_issue_id=$1)", f.parent)
	return f
}
func TestHierarchyEscalationHTTPStrictCreateReplayAndRead(t *testing.T) {
	f := escalationHTTPFixture(t)
	path := "/api/issues/" + f.parent + "/hierarchy-escalations"
	body := map[string]any{"request_id": uuid.NewString(), "reason": "Need human decision", "context": "Keep partial work"}
	first := hierarchyReceipt(t, escalationHTTP(t, f.token, http.MethodPost, path, body), 201)
	replay := hierarchyReceipt(t, escalationHTTP(t, f.token, http.MethodPost, path, body), 200)
	if replay.IssueID != first.IssueID || !replay.Replayed {
		t.Fatalf("bad replay: %+v", replay)
	}
	for _, field := range []string{"target_agent_id", "source_task_id", "project_id", "source_agent_id", "assignee_id"} {
		bad := map[string]any{"request_id": uuid.NewString(), "reason": "Decision", field: uuid.NewString()}
		if w := escalationHTTP(t, f.token, http.MethodPost, path, bad); w.Code != 400 {
			t.Fatalf("forged %s: %d %s", field, w.Code, w.Body.String())
		}
	}
	body["context"] = "changed"
	if w := escalationHTTP(t, f.token, http.MethodPost, path, body); w.Code != 409 {
		t.Fatalf("intent: %d", w.Code)
	}
	body["reason"] = strings.Repeat("x", 33000)
	if w := escalationHTTP(t, f.token, http.MethodPost, path, body); w.Code != 400 {
		t.Fatalf("size: %d", w.Code)
	}
	pat := "mul_" + uuid.NewString()
	dbfx.Insert(t, "personal_access_token", testutil.Cols{"user_id": testUserID, "name": "escalation reader", "token_hash": auth.HashToken(pat), "token_prefix": "mul_test"})
	if w := escalationHTTP(t, pat, http.MethodPost, path, map[string]any{"request_id": uuid.NewString(), "reason": "No impersonation"}); w.Code != 403 {
		t.Fatalf("member impersonation: %d", w.Code)
	}
	for _, id := range []string{f.parent, first.IssueID} {
		w := escalationHTTP(t, pat, http.MethodGet, "/api/issues/"+id+"/hierarchy-escalations", nil)
		if w.Code != 200 {
			t.Fatalf("read: %d %s", w.Code, w.Body.String())
		}
		var page struct {
			Items []hierarchyEscalationRecord `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
			t.Fatalf("page %s / %v", w.Body.String(), err)
		}
		x := page.Items[0]
		if !x.HumanIntervention || x.ManagerAgentID != nil || x.SourceIssueID != f.parent || x.DecisionIssueID != first.IssueID || x.DecisionStatus == nil || x.Reason != "Need human decision" {
			t.Fatalf("record: %+v", x)
		}
	}
	dbfx.Exec(t, "DELETE FROM issue WHERE id=$1", first.IssueID)
	w := escalationHTTP(t, pat, http.MethodGet, path, nil)
	if !strings.Contains(w.Body.String(), "decision_deleted") {
		t.Fatalf("tombstone: %s", w.Body.String())
	}
	if w = escalationHTTP(t, pat, http.MethodGet, path+"?before=bad", nil); w.Code != 400 {
		t.Fatalf("cursor: %d", w.Code)
	}
	dbfx.Exec(t, "DELETE FROM task_token WHERE token_hash=$1", auth.HashToken(f.token))
	if w = escalationHTTP(t, f.token, http.MethodPost, path, map[string]any{"request_id": uuid.NewString(), "reason": "revoked"}); w.Code != 401 {
		t.Fatalf("revoked: %d", w.Code)
	}
}

func TestHierarchyEscalationHTTPManagerAdmissionAndDiagnostics(t *testing.T) {
	f := escalationHTTPFixture(t)
	manager := f.source
	f.source = f.target
	dbfx.Exec(t, "UPDATE issue SET assignee_id=$1 WHERE id=$2", f.source, f.parent)
	dbfx.Exec(t, "UPDATE agent_task_queue SET agent_id=$1 WHERE id=$2", f.source, f.task)
	f.token = f.newToken(t, f.task)
	path := "/api/issues/" + f.parent + "/hierarchy-escalations"
	body := map[string]any{"request_id": uuid.NewString(), "reason": "Manager decision"}
	first := hierarchyReceipt(t, escalationHTTP(t, f.token, http.MethodPost, path, body), 201)
	var assigned string
	dbfx.QueryRow(t, "SELECT assignee_id::text FROM issue WHERE id=$1", first.IssueID).Scan(&assigned)
	if assigned != manager {
		t.Fatalf("wrong manager=%s", assigned)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", first.IssueID); n != 1 {
		t.Fatalf("tasks=%d", n)
	}
	dbfx.Exec(t, "UPDATE agent SET runtime_id=NULL WHERE id=$1", manager)
	if w := escalationHTTP(t, f.token, http.MethodPost, path, body); w.Code != 403 {
		t.Fatalf("unavailable manager replay=%d %s", w.Code, w.Body.String())
	}
	if w := escalationHTTP(t, f.token, http.MethodGet, path, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "manager_unavailable") {
		t.Fatalf("repair=%d %s", w.Code, w.Body.String())
	}
	dbfx.Exec(t, "DELETE FROM agent_task_queue WHERE issue_id=$1", first.IssueID)
	if w := escalationHTTP(t, f.token, http.MethodGet, path, nil); !strings.Contains(w.Body.String(), "dispatch_not_recorded") {
		t.Fatalf("missing dispatch: %s", w.Body.String())
	}
	dbfx.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", first.IssueID)
	if w := escalationHTTP(t, f.token, http.MethodGet, path, nil); strings.Contains(w.Body.String(), "dispatch_not_recorded") || strings.Contains(w.Body.String(), "manager_unavailable") {
		t.Fatalf("closed decision prompted recovery: %s", w.Body.String())
	}
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", f.task)
	if w := escalationHTTP(t, f.token, http.MethodPost, path, map[string]any{"request_id": uuid.NewString(), "reason": "stopped"}); w.Code != 403 {
		t.Fatalf("stopped=%d %s", w.Code, w.Body.String())
	}
}

func TestHierarchyEscalationHTTPPaginationAndReadBoundary(t *testing.T) {
	f := escalationHTTPFixture(t)
	path := "/api/issues/" + f.parent + "/hierarchy-escalations"
	for range 51 {
		hierarchyReceipt(t, escalationHTTP(t, f.token, http.MethodPost, path, map[string]any{"request_id": uuid.NewString(), "reason": "Decision"}), 201)
	}
	var page struct {
		Items []hierarchyEscalationRecord `json:"items"`
		Next  *string                     `json:"next_cursor"`
	}
	w := escalationHTTP(t, f.token, http.MethodGet, path, nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 50 || page.Next == nil {
		t.Fatalf("first page: %s / %v", w.Body.String(), err)
	}
	seen := map[string]bool{}
	for _, item := range page.Items {
		seen[item.DecisionIssueID] = true
	}
	w = escalationHTTP(t, f.token, http.MethodGet, path+"?before="+*page.Next, nil)
	page.Items = nil
	page.Next = nil
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Next != nil || seen[page.Items[0].DecisionIssueID] {
		t.Fatalf("second page: %s / %v", w.Body.String(), err)
	}
	other := dbfx.User(t, "outside workspace", uuid.NewString()+"@multica.test")
	pat := "mul_" + uuid.NewString()
	dbfx.Insert(t, "personal_access_token", testutil.Cols{"user_id": other, "name": "outsider", "token_hash": auth.HashToken(pat), "token_prefix": "mul_test"})
	if w = escalationHTTP(t, pat, http.MethodGet, path, nil); w.Code != 403 {
		t.Fatalf("outside read: %d %s", w.Code, w.Body.String())
	}
}
