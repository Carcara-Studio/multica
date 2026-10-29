package projectmemory

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDraftReportIsBoundedUnreviewedAndDoesNotPublish(t *testing.T) {
	current := Checkpoint{ProjectID: "11111111-1111-4111-8111-111111111111"}
	source := DraftSource{TaskID: current.ProjectID, IssueID: current.ProjectID, IssueRevision: 1, CompletedAt: "2026-10-29T12:00:00Z"}
	raw, _ := json.Marshal(map[string]string{"output": "Result: keep IDs opaque. AKIA" + "IOSFODNN7EXAMPLE", "session_path": "private-session-data"})
	draft := BuildDraft(current, source, raw)
	if !draft.Valid() || !draft.Available || draft.Source.State != "unreviewed" || draft.SuggestedContent == nil || draft.Current != current {
		t.Fatalf("bad draft: %+v", draft)
	}
	encoded, _ := json.Marshal(draft)
	if strings.Contains(string(encoded), "private-session-data") || strings.Contains(string(encoded), "IOSFODNN7EXAMPLE") {
		t.Fatal("completion metadata or credential leaked")
	}
	if !strings.Contains(*draft.SuggestedContent, "completion is not acceptance") {
		t.Fatal("lost review boundary")
	}
	if BuildDraft(current, source, raw).Source.SHA256 != draft.Source.SHA256 {
		t.Fatal("unstable fingerprint")
	}
	source.IssueRevision++
	if BuildDraft(current, source, raw).Source.SHA256 == draft.Source.SHA256 {
		t.Fatal("fingerprint ignored source revision")
	}
	raw, _ = json.Marshal(map[string]string{"output": strings.Repeat("á", 701)})
	draft = BuildDraft(current, source, raw)
	if !draft.Valid() || !draft.Source.Truncated || !utf8.ValidString(draft.Source.ReportExcerpt) || len(draft.Source.ReportExcerpt) > 1200 {
		t.Fatal("unsafe report truncation")
	}
}

func TestDraftPreservesCheckpointBudgetAndUnavailableSource(t *testing.T) {
	current := Checkpoint{ProjectID: "11111111-1111-4111-8111-111111111111", Revision: "22222222-2222-4222-8222-222222222222", ReviewedBy: "33333333-3333-4333-8333-333333333333", ReviewedAt: "2026-10-29T12:00:00Z", Content: strings.Repeat("x", MaxContentBytes)}
	source := DraftSource{TaskID: current.ProjectID, IssueID: current.ProjectID, IssueRevision: 1, CompletedAt: current.ReviewedAt}
	draft := BuildDraft(current, source, []byte(`{"output":"finished"}`))
	if !draft.Valid() || !draft.Available || draft.Reason != "checkpoint_budget_exceeded" || draft.SuggestedContent != nil || draft.Current != current {
		t.Fatal("existing checkpoint silently shortened")
	}
	for _, raw := range []string{`{}`, `{"output":null}`, `{"output":42}`, `broken`, strings.Repeat("x", (1<<20)+1)} {
		draft = BuildDraft(current, source, []byte(raw))
		if !draft.Valid() || draft.Available || draft.Source != nil {
			t.Fatal("accepted unavailable report")
		}
	}
}
