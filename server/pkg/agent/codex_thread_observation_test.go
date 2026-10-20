package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func threadObservationReply(cwd, id, model string) string {
	raw, _ := json.Marshal(map[string]any{"model": model, "modelProvider": "openai", "cwd": cwd, "thread": map[string]any{"id": id, "model": "historical-model", "preview": "never-disclose-transcript"}})
	return string(raw)
}
func threadObservationFixture(t *testing.T) (*codexClient, *codexThreadObserver, *fakeStdinWithHook, *string, map[string]any) {
	t.Helper()
	c, _, _ := newTestCodexClient(t)
	c.cfg.Env = map[string]string{"PRIVATE": "never-disclose-token"}
	cwd := filepath.Join(t.TempDir(), "prepared")
	o, err := newCodexThreadObserver(c, cwd)
	if err != nil {
		t.Fatal(err)
	}
	raw := threadObservationReply(cwd, "thread-actual", "selected-model")
	stdin := &fakeStdinWithHook{}
	stdin.afterWrite = func() {
		lines := stdin.Lines()
		var req struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &req); err != nil {
			t.Fatal(err)
		}
		c.handleLine(fmt.Sprintf(`{"id":%d,"result":%s}`, req.ID, raw))
	}
	c.stdin = stdin
	return c, o, stdin, &raw, map[string]any{"cwd": cwd, "model": "selected-model", "modelProvider": nil}
}
func requireThreadObservation(t *testing.T, o *codexThreadObserver) codexThreadObservation {
	t.Helper()
	v, err := o.snapshot(t.Context())
	if err != nil || v.epoch == uuid.Nil {
		t.Fatal("missing acknowledged thread selection")
	}
	return v
}
func requireNoThreadObservation(t *testing.T, o *codexThreadObserver) {
	t.Helper()
	v, err := o.snapshot(t.Context())
	if err != errCodexThreadObservation || v != (codexThreadObservation{}) {
		t.Fatal("invalid/stale setup granted observation")
	}
}

func TestCodexThreadObservationUsesActualPendingResponse(t *testing.T) {
	c, o, stdin, raw, params := threadObservationFixture(t)
	// An unsolicited response cannot establish a receipt, even with correct data.
	c.handleLine(fmt.Sprintf(`{"id":999,"result":%s}`, *raw))
	requireNoThreadObservation(t, o)
	for _, method := range []string{"thread/start", "thread/resume"} {
		params["threadId"] = "thread-actual"
		returned, err := c.request(t.Context(), method, params)
		if err != nil || string(returned) != *raw {
			t.Fatal("observer changed ordinary RPC result")
		}
		v := requireThreadObservation(t, o)
		if v.model != "selected-model" || v.provider != "openai" || v.threadID != "thread-actual" || v.cwd != params["cwd"] || v.method != method {
			t.Fatal("selection came from request/history instead of response")
		}
		for _, value := range []any{v, o, *o} {
			encoded, err := json.Marshal(value)
			if err != nil || string(encoded) != "{}" {
				t.Fatal("private selection serialized")
			}
			var logs bytes.Buffer
			slog.New(slog.NewJSONHandler(&logs, nil)).Info("fixture", "value", value)
			rendered := []string{fmt.Sprint(value), fmt.Sprintf("%#v", value), fmt.Sprintf("%+d", value), logs.String()}
			for _, text := range rendered {
				if strings.Contains(text, "never-disclose") || strings.Contains(text, "selected-model") || strings.Contains(text, v.cwd) {
					t.Fatal("selection diagnostic leaked private data")
				}
			}
		}
	}
	if len(stdin.Lines()) != 2 {
		t.Fatal("observer issued extra RPC")
	}
}

func TestCodexThreadObservationRejectsWithoutChangingExecution(t *testing.T) {
	for _, kind := range []string{"missing model", "missing provider", "wrong model", "wrong provider", "wrong cwd", "wrong resumed thread", "duplicate key", "large response", "wrong request cwd"} {
		t.Run(kind, func(t *testing.T) {
			c, o, _, raw, params := threadObservationFixture(t)
			method := "thread/start"
			switch kind {
			case "missing model":
				*raw = strings.Replace(*raw, `"model":"selected-model"`, `"model":null`, 1)
			case "missing provider":
				*raw = strings.Replace(*raw, `"modelProvider":"openai"`, `"modelProvider":null`, 1)
			case "wrong model":
				params["model"] = "different-requested-model"
			case "wrong provider":
				params["modelProvider"] = "different-provider"
			case "wrong cwd":
				*raw = threadObservationReply(t.TempDir(), "thread-actual", "selected-model")
			case "wrong resumed thread":
				method = "thread/resume"
				params["threadId"] = "another-thread"
			case "duplicate key":
				*raw = strings.Replace(*raw, `"model":"selected-model"`, `"model":"selected-model","model":"selected-model"`, 1)
			case "large response":
				*raw = strings.TrimSuffix(*raw, "}") + `,"extra":"` + strings.Repeat("x", codexObservedConfigMaxBytes) + `"}`
			case "wrong request cwd":
				params["cwd"] = t.TempDir()
			}
			returned, err := c.request(t.Context(), method, params)
			if err != nil || string(returned) != *raw {
				t.Fatal("optional rejection changed execution response")
			}
			requireNoThreadObservation(t, o)
		})
	}
}

func TestCodexThreadObservationInvalidationAndOrderedTraffic(t *testing.T) {
	for _, kind := range []string{"next RPC", "notification", "notify", "respond", "respond error", "malformed frame", "unknown response", "close pending", "process death", "after response notification", "after response cancellation"} {
		t.Run(kind, func(t *testing.T) {
			c, o, stdin, _, params := threadObservationFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			original := stdin.afterWrite
			if kind == "after response notification" {
				stdin.afterWrite = func() { original(); c.handleLine(`{"method":"fixture/changed","params":{}}`) }
			}
			if kind == "after response cancellation" {
				stdin.afterWrite = func() { original(); cancel() }
			}
			_, _ = c.request(ctx, "thread/start", params)
			if strings.HasPrefix(kind, "after response") {
				requireNoThreadObservation(t, o)
				return
			}
			before := requireThreadObservation(t, o)
			stdin.afterWrite = func() {}
			switch kind {
			case "next RPC":
				stdin.afterWrite = original
				_, _ = c.request(t.Context(), "config/read", map[string]any{})
			case "notification":
				c.handleLine(`{"method":"fixture/changed","params":{}}`)
			case "notify":
				c.notify("initialized")
			case "respond":
				c.respond(99, map[string]any{})
			case "respond error":
				c.respondError(99, -1, "fixture")
			case "malformed frame":
				c.handleLine(`{`)
			case "unknown response":
				c.handleLine(`{"id":999,"result":{}}`)
			case "close pending":
				c.closeAllPending(errors.New("fixture"))
			case "process death":
				c.markProcessExited(errors.New("fixture"))
			}
			requireNoThreadObservation(t, o)
			if kind != "process death" {
				stdin.afterWrite = original
				if _, err := c.request(t.Context(), "thread/start", params); err != nil {
					t.Fatal(err)
				}
				if after := requireThreadObservation(t, o); after.epoch == before.epoch {
					t.Fatal("old epoch reused")
				}
			}
		})
	}
	// A notification before the response is not evidence; only the subsequent
	// actual response may establish a setup acknowledgement.
	c, o, stdin, _, params := threadObservationFixture(t)
	original := stdin.afterWrite
	stdin.afterWrite = func() { c.handleLine(`{"method":"fixture/changed","params":{}}`); original() }
	if _, err := c.request(t.Context(), "thread/start", params); err != nil {
		t.Fatal(err)
	}
	requireThreadObservation(t, o)
}

func TestCodexThreadObservationRetainsDefaultAndResumeFallback(t *testing.T) {
	c, o, stdin, raw, params := threadObservationFixture(t)
	stdin.afterWrite = func() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		lines := stdin.Lines()
		_ = json.Unmarshal([]byte(lines[len(lines)-1]), &req)
		if req.Method == "thread/resume" {
			c.handleLine(fmt.Sprintf(`{"id":%d,"error":{"code":-32602,"message":"fixture missing thread"}}`, req.ID))
			return
		}
		c.handleLine(fmt.Sprintf(`{"id":%d,"result":%s}`, req.ID, *raw))
	}
	id, resumed, err := c.startOrResumeThread(t.Context(), ExecOptions{Cwd: params["cwd"].(string), Model: "selected-model", ResumeSessionID: "old-thread"}, slog.Default())
	if err != nil || resumed || id != "thread-actual" || requireThreadObservation(t, o).method != "thread/start" {
		t.Fatal("resume fallback lost actual start observation")
	}
	// Without opting in, even legacy minimal setup responses keep working.
	plain, _, _ := newTestCodexClient(t)
	sink := &fakeStdinWithHook{}
	sink.afterWrite = func() { plain.handleLine(`{"id":1,"result":{"thread":{"id":"legacy"}}}`) }
	plain.stdin = sink
	id, _, err = plain.startOrResumeThread(t.Context(), ExecOptions{}, slog.Default())
	if err != nil || id != "legacy" || plain.threadObservation.Load() != nil || len(sink.Lines()) != 1 {
		t.Fatal("default executor contract changed")
	}
}

type threadObservationSink struct{ requests chan map[string]any }

func (s *threadObservationSink) Write(raw []byte) (int, error) {
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		return 0, err
	}
	s.requests <- req
	return len(raw), nil
}
func TestCodexThreadObservationRejectsOverlappingAndLateResponses(t *testing.T) {
	c, o, _, raw, params := threadObservationFixture(t)
	sink := &threadObservationSink{requests: make(chan map[string]any, 2)}
	c.stdin = sink
	done := make(chan error, 2)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go func() { _, err := c.request(ctx, "thread/start", params); done <- err }()
	first := <-sink.requests
	go func() { _, err := c.request(ctx, "config/read", map[string]any{}); done <- err }()
	second := <-sink.requests
	requireNoThreadObservation(t, o)
	c.handleLine(fmt.Sprintf(`{"id":%v,"result":{}}`, second["id"]))
	c.handleLine(fmt.Sprintf(`{"id":%v,"result":%s}`, first["id"], *raw))
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	requireNoThreadObservation(t, o)
	expired, stop := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer stop()
	go func() { _, err := c.request(expired, "thread/start", params); done <- err }()
	late := <-sink.requests
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("setup did not respect response deadline")
	}
	c.handleLine(fmt.Sprintf(`{"id":%v,"result":%s}`, late["id"], *raw))
	requireNoThreadObservation(t, o)
	if _, err := newCodexThreadObserver(c, params["cwd"].(string)); err != errCodexThreadObservation {
		t.Fatal("second observer replaced live source")
	}
	if _, err := newCodexThreadObserver(&codexClient{}, t.TempDir()); err != errCodexThreadObservation {
		t.Fatal("uninitialized client accepted")
	}
}

func TestCodexThreadObservationComposesWithLayeredConfig(t *testing.T) {
	c, o, _, raw, params := threadObservationFixture(t)
	config, err := newCodexConfigObserver(c, params["cwd"].(string))
	if err != nil {
		t.Fatal(err)
	}
	*raw = codexObservedFixture
	before, err := config.observe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	*raw = threadObservationReply(params["cwd"].(string), "thread-actual", "selected-model")
	if _, err := c.request(t.Context(), "thread/start", params); err != nil {
		t.Fatal(err)
	}
	selected := requireThreadObservation(t, o)
	*raw = codexObservedFixture
	same, err := config.observe(t.Context())
	if err != nil || same.epoch != before.epoch || requireThreadObservation(t, o) != selected {
		t.Fatal("scoped read-only config observation lost acknowledged model")
	}
	*raw = `{}`
	if _, err := config.observe(t.Context()); err != errCodexConfigObservation {
		t.Fatal("malformed config accepted")
	}
	requireNoThreadObservation(t, o)
}

func TestCodexThreadObserverCannotAttachDuringDeliveredRPC(t *testing.T) {
	c, _, _ := newTestCodexClient(t)
	delivered, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	stdin := &fakeStdinWithHook{}
	stdin.afterWrite = func() { c.handleLine(`{"id":1,"result":{}}`); close(delivered); <-release }
	c.stdin = stdin
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	go func() { _, err := c.request(ctx, "initialize", map[string]any{}); done <- err }()
	select {
	case <-delivered:
	case <-ctx.Done():
		t.Fatal("fixture response not delivered")
	}
	// The pending map is already empty, but the RPC caller still owns a live
	// operation. Attaching here must not hide that operation from the observer.
	if _, err := newCodexThreadObserver(c, t.TempDir()); err != errCodexThreadObservation {
		t.Fatal("observer attached inside an unobserved RPC")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("fixture RPC did not finish")
	}
	if _, err := newCodexThreadObserver(c, t.TempDir()); err != nil {
		t.Fatal("idle initialized client rejected")
	}
}
