package handler

import (
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/projectmemory"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func continuityRequest(id, method string, body any) *http.Request {
	return withURLParam(newRequest(method, "/api/projects/"+id+"/continuity", body), "id", id)
}

func TestProjectContinuityLifecycleScopeAndClaimDelivery(t *testing.T) {
	id := dbfx.Project(t, "Continuity A")
	other := dbfx.Project(t, "Continuity B")
	var empty, saved, cleared projectmemory.Checkpoint
	testutil.Call(t, testHandler.GetProjectContinuity, continuityRequest(id, "GET", nil)).Want(200).JSON(&empty)
	if !empty.Valid() || empty.Content != "" || empty.Revision != "" {
		t.Fatalf("bad empty state: %+v", empty)
	}
	text := "Decision: preserve project isolation. Pending: verify a new task. Next: inspect source before editing."
	body := map[string]any{"expected_revision": "", "content": text}
	testutil.Call(t, testHandler.SetProjectContinuity, continuityRequest(id, "PUT", body)).Want(200).JSON(&saved)
	if !saved.Valid() || saved.Content != text || saved.ReviewedBy != testUserID {
		t.Fatalf("invalid publication: %+v", saved)
	}
	testutil.Call(t, testHandler.SetProjectContinuity, continuityRequest(id, "PUT", body)).Want(409)
	ctx, err := testHandler.resolveClaimProjectContext(t.Context(), parseUUID(id), parseUUID(testWorkspaceID))
	if err != nil || !strings.Contains(ctx.Description, text) || !strings.Contains(ctx.Description, saved.Revision) {
		t.Fatalf("checkpoint not delivered: %+v %v", ctx, err)
	}
	for _, project := range []string{other, uuid.NewString()} {
		ctx, err := testHandler.resolveClaimProjectContext(t.Context(), parseUUID(project), parseUUID(testWorkspaceID))
		if err != nil || strings.Contains(ctx.Description, text) {
			t.Fatalf("cross-project context: %+v %v", ctx, err)
		}
	}
	foreignWS := dbfx.Workspace(t, "Other workspace", "continuity-"+uuid.NewString())
	ctx, err = testHandler.resolveClaimProjectContext(t.Context(), parseUUID(id), parseUUID(foreignWS))
	if err != nil || ctx.ProjectID != "" || strings.Contains(ctx.Description, text) {
		t.Fatalf("cross-workspace context: %+v %v", ctx, err)
	}
	testutil.Call(t, testHandler.ClearProjectContinuity, continuityRequest(id, "DELETE", map[string]any{"expected_revision": saved.Revision})).Want(200).JSON(&cleared)
	if !cleared.Valid() || cleared.Content != "" || cleared.Revision == saved.Revision {
		t.Fatalf("clear lost revision: %+v", cleared)
	}
	testutil.Call(t, testHandler.SetProjectContinuity, continuityRequest(id, "PUT", body)).Want(409)
	ctx, err = testHandler.resolveClaimProjectContext(t.Context(), parseUUID(id), parseUUID(testWorkspaceID))
	if err != nil || strings.Contains(ctx.Description, text) {
		t.Fatal("cleared content still delivered")
	}
	body["expected_revision"] = cleared.Revision
	testutil.Call(t, testHandler.SetProjectContinuity, continuityRequest(id, "PUT", body)).Want(200)
	// Normal metadata updates cannot overwrite this independent checkpoint.
	dbfx.Exec(t, `UPDATE project SET description='original description' WHERE id=$1`, id)
	ctx, err = testHandler.resolveClaimProjectContext(t.Context(), parseUUID(id), parseUUID(testWorkspaceID))
	if err != nil || !strings.HasPrefix(ctx.Description, "original description") || !strings.Contains(ctx.Description, text) {
		t.Fatal("description or checkpoint lost")
	}
}

func TestProjectContinuityAuthorizationAndInvalidInput(t *testing.T) {
	id := dbfx.Project(t, "Continuity protected")
	body := map[string]any{"expected_revision": "", "content": "decision"}
	for _, change := range []map[string]any{{"content": "missing revision"}, {"expected_revision": "", "content": ""}, {"expected_revision": "broken", "content": "text"}, {"expected_revision": "", "content": strings.Repeat("x", 4001)}, {"expected_revision": "", "content": "text", "squad_id": uuid.NewString()}} {
		testutil.Call(t, testHandler.SetProjectContinuity, continuityRequest(id, "PUT", change)).Want(400)
	}
	for _, actor := range []string{"agent", "daemon", "nonmember"} {
		t.Run(actor, func(t *testing.T) {
			req := continuityRequest(id, "PUT", body)
			switch actor {
			case "agent":
				req.Header.Set("X-Actor-Source", "task_token")
				req.Header.Set("X-Agent-ID", uuid.NewString())
			case "daemon":
				req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, uuid.NewString()))
			case "nonmember":
				user := dbfx.User(t, "Not a member", uuid.NewString()+"@example.test")
				req.Header.Set("X-User-ID", user)
			}
			status := 403
			if actor == "nonmember" {
				status = 404
			}
			testutil.Call(t, testHandler.SetProjectContinuity, req).Want(status)
			testutil.Call(t, testHandler.GetProjectContinuityDraft, req).Want(status)
		})
	}
}

func TestProjectContinuityConcurrentPublicationCAS(t *testing.T) {
	id := dbfx.Project(t, "Concurrent continuity")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := continuityRequest(id, "PUT", map[string]any{"expected_revision": "", "content": "published decision"})
			rec := testutil.Call(t, testHandler.SetProjectContinuity, req)
			codes <- rec.Code
		}()
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("CAS did not select one publication: %v", counts)
	}
}
