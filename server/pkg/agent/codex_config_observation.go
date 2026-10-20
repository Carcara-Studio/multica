package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

var errCodexConfigObservation = errors.New("codex configuration observation unavailable")

const codexObservedConfigMaxBytes = 256 << 10

// This is layered configuration, not the effective thread model, credentials,
// capabilities or a lease. In particular, config/read does not apply the full
// thread/start/profile/turn override path. No execution path calls this yet.
type codexConfigObservation struct {
	epoch                               uuid.UUID
	configuredModel, configuredProvider string
}

// Neither configuration values nor a secret-derived comparison digest belong
// in JSON, diagnostics or the future observation wire protocol.
func (codexConfigObservation) String() string               { return "<codex configuration observation>" }
func (o codexConfigObservation) GoString() string           { return o.String() }
func (o codexConfigObservation) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, o.String()) }
func (o codexConfigObservation) LogValue() slog.Value       { return slog.StringValue(o.String()) }

// One observer belongs to one actual app-server client and one fixed cwd. It
// reads only through that client's existing RPC transport, never ambient files,
// installed CLIs, account APIs or caller-supplied response snapshots.
type codexConfigObserver struct {
	*codexConfigObserverState
}

type codexConfigObserverState struct {
	mu      sync.Mutex
	client  *codexClient
	cwd     string
	digest  [sha256.Size]byte
	current codexConfigObservation
}

func (codexConfigObserver) String() string               { return "<codex configuration observer>" }
func (o codexConfigObserver) GoString() string           { return o.String() }
func (o codexConfigObserver) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, o.String()) }
func (o codexConfigObserver) LogValue() slog.Value       { return slog.StringValue(o.String()) }

func newCodexConfigObserver(client *codexClient, cwd string) (*codexConfigObserver, error) {
	if client == nil || !filepath.IsAbs(cwd) || strings.ContainsRune(cwd, 0) {
		return nil, errCodexConfigObservation
	}
	return &codexConfigObserver{&codexConfigObserverState{client: client, cwd: cwd}}, nil
}

func (o *codexConfigObserver) observe(ctx context.Context) (codexConfigObservation, error) {
	if o == nil || o.codexConfigObserverState == nil {
		return codexConfigObservation{}, errCodexConfigObservation
	}
	// This bounds response waiting through the existing RPC implementation.
	// Its stdin Write can still block; future execution admission needs a
	// cancellable transport before this can become an approved adapter.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	o.mu.Lock()
	defer o.mu.Unlock()
	reject := func() (codexConfigObservation, error) {
		o.digest = [sha256.Size]byte{}
		o.current = codexConfigObservation{}
		return codexConfigObservation{}, errCodexConfigObservation
	}
	raw, err := o.client.request(ctx, "config/read", map[string]any{"includeLayers": true, "cwd": o.cwd})
	if err != nil {
		return reject()
	}
	value, err := decodeCodexObservedConfig(raw)
	if err != nil {
		return reject()
	}
	config, ok := value["config"].(map[string]any)
	if !ok {
		return reject()
	}
	model, ok := codexConfiguredSelection(config, "model")
	if !ok {
		return reject()
	}
	provider, ok := codexConfiguredSelection(config, "model_provider")
	if !ok {
		return reject()
	}
	origins, ok := value["origins"].(map[string]any)
	if !ok {
		return reject()
	}
	for _, origin := range origins {
		if !codexObservedLayerMetadata(origin) {
			return reject()
		}
	}
	layers, ok := value["layers"].([]any)
	if !ok || len(layers) > 128 {
		return reject()
	}
	for _, layer := range layers {
		m, ok := layer.(map[string]any)
		if !ok || !codexObservedLayerMetadata(m) {
			return reject()
		}
		if _, ok := m["config"].(map[string]any); !ok {
			return reject()
		}
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return reject()
	}
	digest := sha256.Sum256(canonical)
	next := o.current
	if next.epoch == uuid.Nil || digest != o.digest {
		epoch, err := uuid.NewRandom()
		if err != nil {
			return reject()
		}
		next = codexConfigObservation{epoch: epoch, configuredModel: model, configuredProvider: provider}
	}
	// A buffered response must not outlive a cancelled read or a dead client.
	o.client.mu.Lock()
	dead := o.client.processErr != nil
	select {
	case <-o.client.processDone:
		dead = true
	default:
	}
	o.client.mu.Unlock()
	if dead || ctx.Err() != nil {
		return reject()
	}
	o.digest, o.current = digest, next
	return next, nil
}

func codexConfiguredSelection(config map[string]any, key string) (string, bool) {
	v := config[key]
	if v == nil {
		return "", true
	}
	s, ok := v.(string)
	if !ok || len(s) > 256 || strings.TrimSpace(s) != s {
		return "", false
	}
	for _, r := range s {
		if r < 32 || r == 127 {
			return "", false
		}
	}
	return s, true
}
func codexObservedLayerMetadata(value any) bool {
	m, ok := value.(map[string]any)
	if !ok {
		return false
	}
	name, ok := m["name"].(map[string]any)
	if !ok {
		return false
	}
	kind, ok := name["type"].(string)
	if !ok || kind == "" || len(kind) > 128 {
		return false
	}
	version, ok := m["version"].(string)
	return ok && version != "" && len(version) <= 1024
}

// Preserve unknown fields for conservative change detection. Duplicate keys,
// excessive nesting/counts, truncated JSON and trailing values are ambiguous.
// UseNumber prevents float64 rounding from hiding changes in future fields.
func decodeCodexObservedConfig(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > codexObservedConfigMaxBytes || !utf8.Valid(raw) {
		return nil, errCodexConfigObservation
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		nodes++
		if depth > 24 || nodes > 8192 {
			return nil, errCodexConfigObservation
		}
		token, err := d.Token()
		if err != nil {
			return nil, errCodexConfigObservation
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return token, nil
		}
		switch delimiter {
		case '{':
			out := map[string]any{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, errCodexConfigObservation
				}
				name, ok := key.(string)
				if !ok {
					return nil, errCodexConfigObservation
				}
				if _, duplicate := out[name]; duplicate {
					return nil, errCodexConfigObservation
				}
				value, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				out[name] = value
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, errCodexConfigObservation
			}
			return out, nil
		case '[':
			out := []any{}
			for d.More() {
				value, err := read(depth + 1)
				if err != nil {
					return nil, err
				}
				out = append(out, value)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, errCodexConfigObservation
			}
			return out, nil
		}
		return nil, errCodexConfigObservation
	}
	value, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errCodexConfigObservation
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errCodexConfigObservation
	}
	return object, nil
}
