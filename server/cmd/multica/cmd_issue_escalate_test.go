package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func executeEscalation(args []string) (string, error) {
	cmd := newIssueEscalateCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}
func TestIssueEscalateStrictReceiptAndNoRetry(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		status      int
		valid       bool
	}{{"created", `{"issue_id":"` + delegationChild + `","replayed":false}`, 201, true}, {"replay", `{"issue_id":"` + delegationChild + `","replayed":true}`, 200, true}, {"missing replay", `{"issue_id":"` + delegationChild + `"}`, 200, false}, {"blocked", `{"code":"escalation_not_allowed"}`, 403, false}, {"ambiguous", `{}`, 500, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/api/issues/"+delegationParent+"/hierarchy-escalations" {
					t.Errorf("unexpected fallback %s %s", r.Method, r.URL.Path)
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["request_id"] != delegationRequest || body["reason"] != "Decision" || body["context"] != "Context" || len(body) != 3 {
					t.Errorf("body=%v", body)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.reply)
			})
			out, err := executeEscalation([]string{delegationParent, "--request-id", delegationRequest, "--reason", "Decision", "--context", "Context"})
			if (err == nil) != tc.valid {
				t.Fatalf("output %s error %v", out, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("HTTP calls=%d", calls.Load())
			}
			if !tc.valid && !strings.Contains(err.Error(), delegationRequest) {
				t.Fatalf("lost retry identity: %v", err)
			}
		})
	}
}
func TestIssueEscalateRejectsInvalidInputBeforeNetwork(t *testing.T) {
	for i, args := range [][]string{{delegationParent, "--reason", "Decision"}, {delegationParent, "--request-id", delegationRequest}, {delegationParent, "--request-id", delegationRequest, "--reason", strings.Repeat("x", 8001)}, {delegationParent, "--request-id", delegationRequest, "--reason", "Decision", "--agent", delegationAgent}} {
		t.Run(fmt.Sprintf("invalid-%d", i), func(t *testing.T) {
			var calls atomic.Int32
			delegationTestServer(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) })
			if _, err := executeEscalation(args); err == nil {
				t.Fatal("invalid input admitted")
			}
			if calls.Load() != 0 {
				t.Fatal("invalid input used network")
			}
		})
	}
}

func TestIssueEscalateRequiresTaskTokenAndReadsContextFile(t *testing.T) {
	var calls atomic.Int32
	delegationTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["context"] != "Saved context" {
			t.Errorf("file context=%q", body["context"])
		}
		io.WriteString(w, `{"issue_id":"`+delegationChild+`","replayed":false}`)
	})
	if err := os.WriteFile("context.txt", []byte("Saved context"), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{delegationParent, "--request-id", delegationRequest, "--reason", "Decision", "--context-file", "context.txt"}
	t.Setenv("MULTICA_TOKEN", "mul_member")
	if _, err := executeEscalation(args); err == nil || calls.Load() != 0 {
		t.Fatalf("member call: %v / %d", err, calls.Load())
	}
	t.Setenv("MULTICA_TOKEN", "mat_test")
	if _, err := executeEscalation(args); err != nil || calls.Load() != 1 {
		t.Fatalf("file context: %v / %d", err, calls.Load())
	}
}
