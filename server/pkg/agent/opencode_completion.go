package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"time"
)

// OpenCode 2.0.28 can reconcile persisted output after session.wait while its
// JSON event consumer discards the final session.step.ended. A clean CLI exit
// alone is not completion proof. Consult the native sanitized transcript and
// require its final successful idle marker for the exact streamed assistant.
func verifyOpencodeV2Completion(ctx context.Context, conn opencodeRunConnection, sessionID, messageID string, logger *slog.Logger) bool {
	if !strings.HasPrefix(sessionID, "ses_") || !strings.HasPrefix(messageID, "msg_") {
		return false
	}
	// Never ensure/restart the managed service for a diagnostic: its boot
	// resumes suspended executions. Native private servers do not install that
	// restart-continuity hook; an explicit server connects without starting one.
	if !conn.standalone && conn.server == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	args := []string{"session", "export", "--sanitize", sessionID}
	if conn.standalone {
		args = append(args, "--standalone")
	} else if conn.server != "" {
		args = append(args, "--server", conn.server)
	}
	cmd := conn.cmd.exec(ctx, args...)
	hideAgentWindow(cmd)
	cmd.Env, cmd.Dir = conn.env, conn.dir
	out := &opencodeProofBuffer{}
	cmd.Stdout, cmd.Stderr = out, io.Discard
	if err := runOwned(cmd, logger); err != nil || ctx.Err() != nil || out.overflow {
		logger.Warn("opencode: native completion proof unavailable", "session", sessionID, "oversized", out.overflow)
		return false
	}
	if !opencodeCompletionProven(out.Bytes(), sessionID, messageID) {
		logger.Warn("opencode: native completion proof rejected", "session", sessionID, "message", messageID)
		return false
	}
	logger.Info("opencode: completion confirmed by native session idle", "session", sessionID, "message", messageID)
	return true
}

const opencodeProofMaxBytes = 8 << 20

type opencodeProofBuffer struct {
	buf      bytes.Buffer
	overflow bool
}

func (b *opencodeProofBuffer) Bytes() []byte { return b.buf.Bytes() }
func (b *opencodeProofBuffer) Len() int      { return b.buf.Len() }

func (b *opencodeProofBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := opencodeProofMaxBytes - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.buf.Write(p)
	return n, nil
}

func opencodeCompletionProven(raw []byte, sessionID, messageID string) bool {
	type message struct {
		ID, Type, Finish, Outcome string
		Error                     json.RawMessage
		Time                      struct{ Created, Completed json.RawMessage }
		Content                   []struct {
			Type  string
			State struct{ Status string }
		}
	}
	var export struct {
		Info     struct{ ID string }
		Messages []message
	}
	if json.Unmarshal(raw, &export) != nil || export.Info.ID != sessionID || len(export.Messages) < 3 {
		return false
	}
	last := len(export.Messages) - 1
	idle, assistant := export.Messages[last], export.Messages[last-1]
	if idle.Type != "idle" || idle.Outcome != "succeeded" || hasOpencodeProofError(idle.Error) ||
		assistant.Type != "assistant" || assistant.ID != messageID || assistant.Finish != "stop" || hasOpencodeProofError(assistant.Error) {
		return false
	}
	created, completed, idleAt := opencodeProofTime(assistant.Time.Created), opencodeProofTime(assistant.Time.Completed), opencodeProofTime(idle.Time.Created)
	if created.IsZero() || completed.Before(created) || idleAt.Before(completed) {
		return false
	}
	for _, part := range assistant.Content {
		if part.Type == "tool" && part.State.Status != "completed" && part.State.Status != "error" {
			return false
		}
	}
	// The final assistant must belong to a persisted prompt, not an orphaned
	// record or a stale idle marker preceding a newly admitted user message.
	for i := last - 2; i >= 0; i-- {
		if export.Messages[i].Type == "idle" {
			return false
		}
		if export.Messages[i].Type == "user" {
			return true
		}
	}
	return false
}

func hasOpencodeProofError(raw json.RawMessage) bool {
	return len(raw) != 0 && string(raw) != "null"
}

func opencodeProofTime(raw json.RawMessage) time.Time {
	var millis int64
	if json.Unmarshal(raw, &millis) == nil && millis > 0 {
		return time.UnixMilli(millis)
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
