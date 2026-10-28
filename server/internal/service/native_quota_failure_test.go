package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestNativeQuotaFailureUsesTerminalWriteSource(t *testing.T) {
	for _, mode := range []string{"current", "withheld session", "changed daemon", "changed provider", "changed task runtime", "foreign workspace", "not running", "foreign session", "unknown native code", "commit failure"} {
		t.Run(mode, func(t *testing.T) {
			pool := sharedTestPool(t)
			f := testutil.New(pool, "", "")
			f.UserID = f.User(t, "Quota terminal", uuid.NewString()+"@example.test")
			f.WorkspaceID = f.Workspace(t, "Quota terminal", "quota-"+uuid.NewString())
			f.Member(t, f.WorkspaceID, f.UserID, "owner")
			runtime := f.Runtime(t, "Quota terminal", testutil.Cols{"provider": "codex", "daemon_id": "quota-terminal"})
			agentID := f.Agent(t, "Quota terminal", runtime)
			chat := f.ChatSession(t, agentID)
			taskID := f.Task(t, agentID, testutil.Cols{"runtime_id": runtime, "chat_session_id": chat, "status": "running", "attempt": 1, "max_attempts": 2})
			f.Cleanup(t, `DELETE FROM chat_message WHERE chat_session_id=$1`, chat)
			source := &NativeQuotaFailureSource{TaskID: util.MustParseUUID(taskID), RuntimeID: util.MustParseUUID(runtime), WorkspaceID: util.MustParseUUID(f.WorkspaceID), DaemonID: "quota-terminal",
				Observation: agent.NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "turn-1"}}
			// Capture a preflight, then change the DB before the terminal write.
			// The source object cannot rehabilitate those stale runtime facts.
			session, withheld := "thread-1", false
			switch mode {
			case "withheld session":
				session, withheld = "", true
			case "changed daemon":
				f.Exec(t, `UPDATE agent_runtime SET daemon_id='replacement' WHERE id=$1`, runtime)
			case "changed provider":
				f.Exec(t, `UPDATE agent_runtime SET provider='opencode' WHERE id=$1`, runtime)
			case "changed task runtime":
				other := f.Runtime(t, "Replacement", testutil.Cols{"provider": "codex", "daemon_id": "replacement-daemon"})
				f.Exec(t, `UPDATE agent_task_queue SET runtime_id=$2 WHERE id=$1`, taskID, other)
				f.Cleanup(t, `UPDATE agent_task_queue SET runtime_id=$2 WHERE id=$1`, taskID, runtime)
			case "foreign workspace":
				source.WorkspaceID = util.MustParseUUID(uuid.NewString())
			case "not running":
				f.Exec(t, `UPDATE agent_task_queue SET status='dispatched' WHERE id=$1`, taskID)
			case "foreign session":
				source.Observation.SessionID = "other-thread"
			case "unknown native code":
				source.Observation.Code = "rateLimitExceeded"
			}
			f.Cleanup(t, `DELETE FROM agent_task_queue WHERE chat_session_id=$1`, chat)
			q := db.New(pool)
			svc := &TaskService{Queries: q, TxStarter: pool, Bus: events.New()}
			if mode == "commit failure" {
				svc.TxStarter = &rejectIssueCommitTxStarter{pool: pool, err: errors.New("injected quota terminal commit failure")}
			}
			network := taskfailure.ReasonAgentProviderNetwork.String()
			task, transitioned, err := svc.FailTaskWithTransition(t.Context(), source.TaskID, "connection closed mid-response", session, "", "", network, withheld, "", "", source)
			if mode == "commit failure" {
				if err == nil || transitioned || task != nil {
					t.Fatal("failed commit released a terminal result", err)
				}
				stored, err := q.GetAgentTask(t.Context(), source.TaskID)
				if err != nil || stored.Status != "running" || stored.FailureReason.Valid {
					t.Fatal("failed commit persisted quota classification", err)
				}
			} else {
				want := network
				if mode == "current" || mode == "withheld session" {
					want = taskfailure.ReasonAgentProviderQuotaLimit.String()
				}
				if err != nil || !transitioned || task == nil || task.Status != "failed" || task.FailureReason.String != want || task.Error.String != "connection closed mid-response" {
					t.Fatalf("terminal result=%+v transitioned=%v error=%v; want reason %s", task, transitioned, err, want)
				}
				if withheld && (task.SessionID.Valid || !task.SessionRolloutMissing) {
					t.Fatal("native quota restored withheld resume pointer")
				}
				// A duplicate cannot narrow an already terminal ordinary failure.
				source.Observation = agent.NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "later-turn"}
				again, changed, err := svc.FailTaskWithTransition(t.Context(), source.TaskID, "different error", session, "", "", network, withheld, "", "", source)
				if err != nil || changed || again.FailureReason.String != want || again.Error != task.Error {
					t.Fatal("duplicate changed terminal classification", err)
				}
			}
			var retries, availability int
			f.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE parent_task_id=$1 OR retry_of_task_id=$1`, taskID).Scan(&retries)
			f.QueryRow(t, `SELECT count(*) FROM agent_failover_state WHERE agent_id=$1`, agentID).Scan(&availability)
			wantRetries := 0
			if mode == "foreign session" || mode == "unknown native code" {
				wantRetries = 1
			}
			if retries != wantRetries || availability != 0 {
				t.Fatalf("retry count=%d want=%d; availability rows=%d", retries, wantRetries, availability)
			}
		})
	}
}
