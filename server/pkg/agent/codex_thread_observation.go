package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

var errCodexThreadObservation = errors.New("codex thread selection observation unavailable")

// An acknowledgement of setup, not credentials, inference, current provider
// state or an execution lease. Fields stay private to the actual client source.
type codexThreadObservation struct {
	epoch                                  uuid.UUID
	threadID, model, provider, cwd, method string
}

func (codexThreadObservation) String() string               { return "<codex thread observation>" }
func (o codexThreadObservation) GoString() string           { return o.String() }
func (o codexThreadObservation) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, o.String()) }
func (o codexThreadObservation) LogValue() slog.Value       { return slog.StringValue(o.String()) }

type codexThreadObserver struct{ state *codexThreadObserverState }

func (codexThreadObserver) String() string               { return "<codex thread observer>" }
func (o codexThreadObserver) GoString() string           { return o.String() }
func (o codexThreadObserver) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, o.String()) }
func (o codexThreadObserver) LogValue() slog.Value       { return slog.StringValue(o.String()) }

// All state, including the pending-response publication, uses client.mu. The
// pointer wrapper preserves identity and redaction when an observer is copied.
type codexThreadObserverState struct {
	client    *codexClient
	cwd       string
	active    int
	candidate *codexThreadRPC
	current   codexThreadObservation
}
type codexThreadRPC struct {
	state                                                     *codexThreadObserverState
	method, expectedThread, requestedModel, requestedProvider string
	readOnly                                                  bool
	setup                                                     bool
}

// Only internal opt-in callers can install an observer. Production constructors
// install none; this does not start a thread, process, account read or inference.
func newCodexThreadObserver(c *codexClient, cwd string) (*codexThreadObserver, error) {
	if c == nil || !filepath.IsAbs(cwd) || strings.ContainsRune(cwd, 0) {
		return nil, errCodexThreadObservation
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.threadObservation.Load() != nil || c.stdin == nil || c.pending == nil || c.processDone == nil || len(c.pending) != 0 || c.rpcActive.Load() != 0 || !codexThreadClientAliveLocked(c) {
		return nil, errCodexThreadObservation
	}
	state := &codexThreadObserverState{client: c, cwd: filepath.Clean(cwd)}
	c.threadObservation.Store(state)
	return &codexThreadObserver{state: state}, nil
}
func codexThreadClientAliveLocked(c *codexClient) bool {
	if c.processErr != nil {
		return false
	}
	select {
	case <-c.processDone:
		return false
	default:
		return true
	}
}
func (o *codexThreadObserver) snapshot(ctx context.Context) (codexThreadObservation, error) {
	if o == nil || o.state == nil || ctx.Err() != nil {
		return codexThreadObservation{}, errCodexThreadObservation
	}
	s := o.state
	s.client.mu.Lock()
	defer s.client.mu.Unlock()
	if ctx.Err() != nil {
		return codexThreadObservation{}, errCodexThreadObservation
	}
	if s.client.threadObservation.Load() != s || !codexThreadClientAliveLocked(s.client) {
		s.current = codexThreadObservation{}
		s.candidate = nil
	}
	if s.active != 0 || s.client.rpcActive.Load() != 0 || s.current.epoch == uuid.Nil {
		return codexThreadObservation{}, errCodexThreadObservation
	}
	return s.current, nil
}

func (c *codexClient) beginCodexThreadRPC(method string, params any) *codexThreadRPC {
	if c.threadObservation.Load() == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.threadObservation.Load()
	s.active++
	a := &codexThreadRPC{state: s, method: method}
	// A scoped config read composes with ADR-028 without mutating the thread.
	// Overlapping RPCs still discard both the receipt and pending candidate.
	if p, ok := params.(map[string]any); ok && method == "config/read" && s.active == 1 && c.rpcActive.Load() == 1 {
		cwd, ok := p["cwd"].(string)
		if ok && filepath.IsAbs(cwd) && !strings.ContainsRune(cwd, 0) && filepath.Clean(cwd) == s.cwd && p["includeLayers"] == true {
			a.readOnly = true
			return a
		}
	}
	s.current = codexThreadObservation{}
	s.candidate = nil
	if !isCodexThreadSetupRPC(method) || (s.active != 1 || c.rpcActive.Load() != 1) || !codexThreadClientAliveLocked(c) {
		return a
	}
	p, ok := params.(map[string]any)
	if !ok {
		return a
	}
	cwd, ok := p["cwd"].(string)
	if !ok || !filepath.IsAbs(cwd) || strings.ContainsRune(cwd, 0) || filepath.Clean(cwd) != s.cwd {
		return a
	}
	model, ok := codexConfiguredSelection(p, "model")
	if !ok {
		return a
	}
	provider, ok := codexConfiguredSelection(p, "modelProvider")
	if !ok {
		return a
	}
	if method == "thread/resume" {
		id, ok := codexConfiguredSelection(p, "threadId")
		if !ok || id == "" {
			return a
		}
		a.expectedThread = id
	}
	a.requestedModel, a.requestedProvider, a.setup = model, provider, true
	s.candidate = a
	return a
}

// Called only for an actual pending request, on the ordered response dispatcher
// before waking its caller. Later inbound traffic cannot be overtaken by a
// delayed request goroutine publishing an older acknowledgement.
func (c *codexClient) recordCodexThreadResponse(a *codexThreadRPC, raw json.RawMessage) {
	if a == nil || !a.setup {
		return
	}
	observed, err := parseCodexThreadResponse(a, raw)
	if err != nil {
		return
	}
	epoch, err := uuid.NewRandom()
	if err != nil {
		return
	}
	observed.epoch = epoch
	c.mu.Lock()
	defer c.mu.Unlock()
	s := a.state
	if c.threadObservation.Load() == s && s.candidate == a && s.active == 1 && c.rpcActive.Load() == 1 && codexThreadClientAliveLocked(c) {
		s.current = observed
	}
}
func (c *codexClient) finishCodexThreadRPC(a *codexThreadRPC, err error, contextErr error) {
	if a == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := a.state
	s.active--
	if a.readOnly {
		if err != nil || contextErr != nil || s.active != 0 || !codexThreadClientAliveLocked(c) {
			s.current = codexThreadObservation{}
			s.candidate = nil
		}
		return
	}
	if s.candidate != a {
		return
	}
	s.candidate = nil
	if err != nil || contextErr != nil || s.active != 0 || !codexThreadClientAliveLocked(c) {
		s.current = codexThreadObservation{}
	}
}

// RPC/notification writes and malformed protocol frames invalidate even a
// pending candidate. Inbound notifications invalidate a recorded response; events
// preceding that response do not substitute for its subsequent acknowledgement.
func (c *codexClient) invalidateCodexThreadObservation(recordedOnly bool) {
	if c.threadObservation.Load() == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.threadObservation.Load()
	if !recordedOnly || s.current.epoch != uuid.Nil {
		s.current = codexThreadObservation{}
		s.candidate = nil
	}
}

func parseCodexThreadResponse(a *codexThreadRPC, raw []byte) (codexThreadObservation, error) {
	reject := func() (codexThreadObservation, error) { return codexThreadObservation{}, errCodexThreadObservation }
	root, err := decodeCodexObservedConfig(raw)
	if err != nil {
		return reject()
	}
	model, ok := codexConfiguredSelection(root, "model")
	if !ok || model == "" || (a.requestedModel != "" && model != a.requestedModel) {
		return reject()
	}
	provider, ok := codexConfiguredSelection(root, "modelProvider")
	if !ok || provider == "" || (a.requestedProvider != "" && provider != a.requestedProvider) {
		return reject()
	}
	cwd, ok := root["cwd"].(string)
	if !ok || !filepath.IsAbs(cwd) || strings.ContainsRune(cwd, 0) || filepath.Clean(cwd) != a.state.cwd {
		return reject()
	}
	thread, ok := root["thread"].(map[string]any)
	if !ok {
		return reject()
	}
	id, ok := codexConfiguredSelection(thread, "id")
	if !ok || id == "" || (a.expectedThread != "" && id != a.expectedThread) {
		return reject()
	}
	// Nested historical model/provider fields are deliberately not used.
	return codexThreadObservation{threadID: id, model: model, provider: provider, cwd: a.state.cwd, method: a.method}, nil
}
