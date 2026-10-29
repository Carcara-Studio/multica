package projectmemory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/pkg/redact"
)

type DraftSource struct {
	TaskID        string `json:"task_id"`
	IssueID       string `json:"issue_id"`
	IssueRevision int64  `json:"issue_revision"`
	IssueStatus   string `json:"issue_status"`
	CompletedAt   string `json:"completed_at"`
	SHA256        string `json:"sha256"`
	State         string `json:"state"`
	ReportExcerpt string `json:"report_excerpt"`
	Truncated     bool   `json:"truncated"`
}

type Draft struct {
	Version          int          `json:"version"`
	Available        bool         `json:"available"`
	Reason           string       `json:"reason"`
	Current          Checkpoint   `json:"current"`
	Source           *DraftSource `json:"source"`
	SuggestedContent *string      `json:"suggested_content"`
}

func EmptyDraft(current Checkpoint, reason string) Draft {
	return Draft{Version: 1, Current: current, Reason: reason}
}

// BuildDraft quotes a bounded final report, never extracts verified decisions.
// The source fingerprint binds the exact stored result and its issue identity;
// it is not a code freshness check or permission to publish without review.
func BuildDraft(current Checkpoint, source DraftSource, result []byte) Draft {
	draft := EmptyDraft(current, "source_unavailable")
	if !current.Valid() || !ValidUUID(source.TaskID) || !ValidUUID(source.IssueID) || source.IssueRevision <= 0 || len(result) > 1<<20 {
		return draft
	}
	if _, err := time.Parse(time.RFC3339Nano, source.CompletedAt); err != nil {
		return draft
	}
	var final struct {
		Output string `json:"output"`
	}
	if json.Unmarshal(result, &final) != nil || strings.TrimSpace(final.Output) == "" {
		return draft
	}
	// Ignore all other completion fields: paths, sessions, logs and tool data
	// do not belong in a project-wide candidate.
	source.State = "unreviewed"
	source.ReportExcerpt = redact.Text(strings.TrimSpace(final.Output))
	if len(source.ReportExcerpt) > 1200 {
		source.ReportExcerpt = source.ReportExcerpt[:1200]
		for !utf8.ValidString(source.ReportExcerpt) {
			source.ReportExcerpt = source.ReportExcerpt[:len(source.ReportExcerpt)-1]
		}
		source.Truncated = true
	}
	fingerprint, _ := json.Marshal(struct {
		Task, Issue, Completed string
		IssueRevision          int64
		Result                 json.RawMessage
	}{source.TaskID, source.IssueID, source.CompletedAt, source.IssueRevision, json.RawMessage(result)})
	sum := sha256.Sum256(fingerprint)
	source.SHA256 = hex.EncodeToString(sum[:])
	draft.Available = true
	draft.Source = &source
	draft.Reason = "checkpoint_budget_exceeded"
	quoted, _ := json.Marshal(source.ReportExcerpt)
	proposal := current.Content + "\n\nUnreviewed task report (completion is not acceptance):\nIssue " + source.IssueID + ", task " + source.TaskID + ", completed " + source.CompletedAt + ", source SHA256 " + source.SHA256 + ".\n" + string(quoted)
	if source.Truncated {
		proposal += "\n[Report excerpt truncated; inspect the full source before review.]"
	}
	if ValidateContent(proposal) == nil {
		draft.SuggestedContent = &proposal
		draft.Reason = "review_required"
	}
	return draft
}

func (d Draft) Valid() bool {
	if d.Version != 1 || !d.Current.Valid() || d.Reason == "" {
		return false
	}
	if !d.Available {
		return (d.Reason == "no_eligible_completed_task" || d.Reason == "source_unavailable") && d.Source == nil && d.SuggestedContent == nil
	}
	s := d.Source
	if s == nil || !ValidUUID(s.TaskID) || !ValidUUID(s.IssueID) || s.IssueRevision <= 0 || s.State != "unreviewed" || len(s.ReportExcerpt) > 1200 || !utf8.ValidString(s.ReportExcerpt) || strings.TrimSpace(s.ReportExcerpt) == "" {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, s.CompletedAt); err != nil {
		return false
	}
	hash, err := hex.DecodeString(s.SHA256)
	if err != nil || len(hash) != 32 {
		return false
	}
	if d.SuggestedContent == nil {
		return d.Reason == "checkpoint_budget_exceeded"
	}
	return d.Reason == "review_required" && ValidateContent(*d.SuggestedContent) == nil
}
