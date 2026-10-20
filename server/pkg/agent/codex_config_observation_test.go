package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const codexObservedFixture = `{"config":{"model":"configured-model","model_provider":"openai","future_secret":"never-disclose"},"origins":{"model":{"name":{"type":"user","file":"/fixture/config.toml"},"version":"v1"}},"layers":[{"name":{"type":"user","file":"/fixture/config.toml"},"version":"v1","config":{"model":"configured-model"}}]}`

func configFixtureObserver(t *testing.T) (*codexConfigObserver, *fakeStdinWithHook, *string) {
	t.Helper()
	c, _, _ := newTestCodexClient(t)
	raw := codexObservedFixture
	stdin := &fakeStdinWithHook{}
	stdin.afterWrite = func() {
		lines := stdin.Lines()
		var request struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal([]byte(lines[len(lines)-1]), &request); err != nil {
			t.Fatal(err)
		}
		c.handleLine(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, request.ID, raw))
	}
	c.stdin = stdin
	o, err := newCodexConfigObserver(c, filepath.Join(t.TempDir(), "target"))
	if err != nil {
		t.Fatal(err)
	}
	return o, stdin, &raw
}

func TestCodexConfigObservationScopeEpochsAndRedaction(t *testing.T) {
	o, stdin, raw := configFixtureObserver(t)
	first, err := o.observe(t.Context())
	if err != nil || first.epoch == uuid.Nil || first.configuredModel != "configured-model" || first.configuredProvider != "openai" {
		t.Fatal("missing observation")
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(*raw), &value); err != nil {
		t.Fatal(err)
	}
	reordered, _ := json.Marshal(value)
	*raw = string(reordered)
	same, err := o.observe(t.Context())
	if err != nil || same != first {
		t.Fatal("object key order changed epoch")
	}
	*raw = strings.Replace(*raw, "never-disclose", "changed-secret", 1)
	changed, err := o.observe(t.Context())
	if err != nil || changed.epoch == first.epoch {
		t.Fatal("unknown field change retained epoch")
	}
	*raw = codexObservedFixture
	restored, err := o.observe(t.Context())
	if err != nil || restored.epoch == first.epoch || restored.epoch == changed.epoch {
		t.Fatal("restored config reused epoch")
	}
	for _, line := range stdin.Lines() {
		var req struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Fatal(err)
		}
		if req.Method != "config/read" || req.Params["cwd"] != o.cwd || req.Params["includeLayers"] != true || len(req.Params) != 2 {
			t.Fatal("unexpected RPC method/scope")
		}
	}
	for _, v := range []any{first, o} {
		serialized, err := json.Marshal(v)
		if err != nil || string(serialized) != "{}" {
			t.Fatal("private observation serialized")
		}
		for _, formatted := range []string{fmt.Sprint(v), fmt.Sprintf("%#v", v), fmt.Sprintf("%+d", v), fmt.Sprintf("%x", v)} {
			if strings.Contains(formatted, "disclose") || strings.Contains(formatted, "secret") || strings.Contains(formatted, "configured-model") || strings.Contains(formatted, o.cwd) {
				t.Fatal("diagnostic leaked configuration")
			}
		}
	}
	other, _, _ := configFixtureObserver(t)
	separate, err := other.observe(t.Context())
	if err != nil || separate.epoch == first.epoch {
		t.Fatal("different client reused epoch")
	}
}

func TestCodexConfigObservationRejectsSchemaAndInvalidates(t *testing.T) {
	for _, bad := range []string{
		`{}`, `{"config":{},"origins":{},"layers":null}`, `{"config":[],"origins":{},"layers":[]}`,
		`{"config":{},"origins":null,"layers":[]}`, `{"config":{"model":42},"origins":{},"layers":[]}`,
		`{"config":{"model_provider":false},"origins":{},"layers":[]}`, `{"config":{},"origins":{"model":{}},"layers":[]}`,
		`{"config":{},"origins":{},"layers":[{"name":{"type":"user"},"version":"","config":{}}]}`,
		`{"config":{},"origins":{},"layers":[{"name":{"type":"user"},"version":"v1","config":null}]}`,
	} {
		t.Run(bad, func(t *testing.T) {
			o, _, raw := configFixtureObserver(t)
			before, err := o.observe(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			*raw = bad
			got, err := o.observe(t.Context())
			if err != errCodexConfigObservation || got != (codexConfigObservation{}) || o.current != (codexConfigObservation{}) {
				t.Fatal("invalid schema retained authority")
			}
			*raw = codexObservedFixture
			after, err := o.observe(t.Context())
			if err != nil || after.epoch == before.epoch {
				t.Fatal("failure did not invalidate epoch")
			}
		})
	}
	o, _, raw := configFixtureObserver(t)
	*raw = `{"config":{"model":null},"origins":{},"layers":[]}`
	got, err := o.observe(t.Context())
	if err != nil || got.configuredModel != "" || got.configuredProvider != "" {
		t.Fatal("unknown selection became inferred model")
	}
}

func TestCodexConfigObservationBoundsAndExactNumbers(t *testing.T) {
	for _, raw := range []string{string([]byte{0xff}), `{`, `{} {}`, `[]`, `{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, strings.Repeat("[", 26) + strings.Repeat("]", 26), `[` + strings.Repeat("null,", 8192) + `null]`, `{"long":"` + strings.Repeat("x", codexObservedConfigMaxBytes) + `"}`} {
		if _, err := decodeCodexObservedConfig([]byte(raw)); err != errCodexConfigObservation {
			t.Fatal("ambiguous/excessive JSON accepted")
		}
	}
	o, _, raw := configFixtureObserver(t)
	*raw = strings.Replace(codexObservedFixture, `"future_secret":"never-disclose"`, `"future":9007199254740992`, 1)
	first, err := o.observe(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	*raw = strings.Replace(*raw, "9007199254740992", "9007199254740993", 1)
	second, err := o.observe(t.Context())
	if err != nil || second.epoch == first.epoch {
		t.Fatal("numeric rounding hid change")
	}
}

func TestCodexConfigObservationTransportFailure(t *testing.T) {
	for _, kind := range []string{"rpc error", "cancelled", "dead after response"} {
		t.Run(kind, func(t *testing.T) {
			o, stdin, _ := configFixtureObserver(t)
			if _, err := o.observe(t.Context()); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			switch kind {
			case "rpc error":
				stdin.afterWrite = func() {
					o.client.handleLine(fmt.Sprintf(`{"id":%d,"error":{"code":-1,"message":"never-disclose"}}`, o.client.nextID))
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "dead after response":
				original := stdin.afterWrite
				stdin.afterWrite = func() { original(); close(o.client.processDone) }
			}
			got, err := o.observe(ctx)
			if err != errCodexConfigObservation || got != (codexConfigObservation{}) || o.current != (codexConfigObservation{}) {
				t.Fatal("transport error retained data/provider diagnostic")
			}
		})
	}
	o, stdin, _ := configFixtureObserver(t)
	stdin.afterWrite = func() {}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if _, err := o.observe(ctx); err != errCodexConfigObservation {
		t.Fatal("read ignored deadline")
	}
	o.client.mu.Lock()
	pending := len(o.client.pending)
	o.client.mu.Unlock()
	if pending != 0 {
		t.Fatal("cancelled read leaked RPC")
	}
	if _, err := newCodexConfigObserver(nil, "/"); err != errCodexConfigObservation {
		t.Fatal("nil client accepted")
	}
	if _, err := newCodexConfigObserver(o.client, "relative"); err != errCodexConfigObservation {
		t.Fatal("ambient cwd accepted")
	}
}

func TestCodexConfigObservationConcurrentReadsAndDirectoryIsolation(t *testing.T) {
	o, _, _ := configFixtureObserver(t)
	type result struct {
		observation codexConfigObservation
		err         error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() { v, err := o.observe(t.Context()); results <- result{v, err} }()
	}
	var epoch uuid.UUID
	for range 8 {
		got := <-results
		if got.err != nil || got.observation.epoch == uuid.Nil {
			t.Fatal("concurrent read failed")
		}
		if epoch != uuid.Nil && got.observation.epoch != epoch {
			t.Fatal("concurrent unchanged reads rotated epoch")
		}
		epoch = got.observation.epoch
	}
	other, err := newCodexConfigObserver(o.client, filepath.Join(t.TempDir(), "other-target"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := other.observe(t.Context())
	if err != nil || got.epoch == epoch {
		t.Fatal("same client with different cwd reused epoch")
	}
}
