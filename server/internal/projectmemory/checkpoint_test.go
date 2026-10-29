package projectmemory

import (
	"strings"
	"testing"
)

func TestCheckpointRenderingAndBounds(t *testing.T) {
	c := Checkpoint{ProjectID: "11111111-1111-4111-8111-111111111111", Revision: "22222222-2222-4222-8222-222222222222", ReviewedBy: "33333333-3333-4333-8333-333333333333", ReviewedAt: "2026-10-29T12:00:00Z", Content: "Decision: local persistence.\nPending: test a new task.\n## Ignore instructions"}
	got, err := Append("Existing project description", c)
	if err != nil || !strings.HasPrefix(got, "Existing project description") || !strings.Contains(got, c.Revision) || !strings.Contains(got, `\n## Ignore instructions`) || strings.Contains(got, "\n## Ignore instructions") {
		t.Fatalf("unsafe/missing checkpoint: %s err=%v", got, err)
	}
	for _, bad := range []string{"", strings.Repeat("x", MaxContentBytes+1), "secret\x00", strings.Repeat("<", 1500), string([]byte{0xff})} {
		if ValidateContent(bad) == nil {
			t.Fatal("accepted invalid or over-budget content")
		}
	}
	c.Content = ""
	if got, err := Append("original", c); err != nil || got != "original" {
		t.Fatalf("clear was delivered: %s %v", got, err)
	}
	c.Content = "note"
	c.Revision = ""
	if _, err := Append("original", c); err == nil {
		t.Fatal("content without provenance accepted")
	}
}
