package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestNativeQuotaTerminalTransport(t *testing.T) {
	q := agent.NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "turn-1"}
	for _, tc := range []struct {
		name, status, session string
		missing, want         bool
	}{
		{"failed", "blocked", "thread-1", false, true},
		{"withheld resume pointer", "blocked", "", true, true},
		{"missing pointer without withhold proof", "blocked", "", false, false},
		{"foreign pointer", "blocked", "thread-other", false, false},
		{"cancelled", "cancelled", "thread-1", false, false},
		{"completed", "completed", "thread-1", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bodies := make(chan map[string]json.RawMessage, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				bodies <- body
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			d := &Daemon{client: NewClient(server.URL), logger: slog.Default()}
			d.reportTaskResult(context.Background(), "task-1", TaskResult{
				Status: tc.status, Comment: "original diagnostic", SessionID: tc.session,
				SessionRolloutMissing: tc.missing, NativeQuota: q,
			}, slog.Default())
			body := <-bodies
			raw, present := body["native_quota"]
			if present != tc.want {
				t.Fatalf("native observation present=%v, want %v", present, tc.want)
			}
			if present {
				var got agent.NativeQuotaObservation
				if err := json.Unmarshal(raw, &got); err != nil || got != q {
					t.Fatalf("native observation=%+v, error=%v", got, err)
				}
			}
			if tc.status != "completed" && string(body["error"]) != `"original diagnostic"` {
				t.Fatal("display diagnostic changed")
			}
			if tc.missing && body["session_id"] != nil {
				t.Fatal("source event must not restore a withheld resume pointer")
			}
		})
	}
}

func TestNativeQuotaOutboxRestartAndConflict(t *testing.T) {
	q := agent.NativeQuotaObservation{Provider: "codex", Code: "usageLimitExceeded", SessionID: "thread-1", TurnID: "turn-1"}
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies <- raw
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	cfg := Config{WorkspacesRoot: t.TempDir(), ServerBaseURL: server.URL, DaemonID: "daemon-1"}
	report := terminalTaskReport{kind: terminalTaskReportFail, taskID: "task-1", nativeQuota: q, sessionRolloutMissing: true}
	store := newTerminalReportStore(cfg)
	if err := store.enqueue(report); err != nil {
		t.Fatal(err)
	}
	conflict := report
	conflict.nativeQuota.TurnID = "different-turn"
	if err := store.enqueue(conflict); err == nil || !strings.Contains(err.Error(), "conflicts with the original") {
		t.Fatalf("conflicting native source must not replace pending payload: %v", err)
	}
	// Reopen from disk with no in-memory report retained by the sender.
	reopened := newTerminalReportStore(cfg)
	items, err := reopened.list()
	if err != nil || len(items) != 1 || items[0].report != report {
		t.Fatalf("reopened report=%+v, error=%v", items, err)
	}
	d := &Daemon{client: NewClient(server.URL), logger: slog.Default(), terminalReports: reopened}
	if err := d.reportTerminalTask(context.Background(), items[0].report); err != nil {
		t.Fatal(err)
	}
	var body struct {
		NativeQuota agent.NativeQuotaObservation `json:"native_quota"`
	}
	if err := json.Unmarshal(<-bodies, &body); err != nil || body.NativeQuota != q {
		t.Fatalf("replayed source=%+v, error=%v", body, err)
	}
	if items, err := reopened.list(); err != nil || len(items) != 0 {
		t.Fatalf("acknowledged report remains pending: %+v, %v", items, err)
	}
	legacy, err := decodePersistedTerminalReport([]byte(`{"version":1,"kind":"fail","task_id":"legacy","error":"old diagnostic"}`))
	if err != nil {
		t.Fatal(err)
	}
	oldReport, err := legacy.terminalReport()
	if err != nil || oldReport.nativeQuota != (agent.NativeQuotaObservation{}) {
		t.Fatalf("legacy report gained a native observation: %+v, %v", oldReport, err)
	}
}
