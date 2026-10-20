package agent

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"
)

var errCodexStdinWrite = errors.New("codex stdin transport unavailable")

// The gate serializes whole frames and their platform cancellation callbacks.
type codexStdin struct {
	pipe    *os.File
	gate    chan struct{}
	abort   func()
	once    sync.Once
	maxWait time.Duration
}

func newCodexStdin(pipe *os.File, abort func()) *codexStdin {
	return &codexStdin{pipe: pipe, gate: make(chan struct{}, 1), abort: abort, maxWait: defaultCodexHandshakeTimeout}
}
func (w *codexStdin) Write(data []byte) (int, error) {
	return w.WriteContext(context.Background(), data)
}
func (w *codexStdin) Close() error {
	if w == nil || w.pipe == nil {
		return errCodexStdinWrite
	}
	return w.pipe.Close()
}
func (w *codexStdin) WriteContext(parent context.Context, data []byte) (int, error) {
	if w == nil || w.pipe == nil || w.gate == nil || w.abort == nil || w.maxWait <= 0 || w.maxWait > defaultCodexHandshakeTimeout {
		return 0, errCodexStdinWrite
	}
	ctx, cancel := context.WithTimeout(parent, w.maxWait)
	defer cancel()
	select {
	case w.gate <- struct{}{}:
		defer func() { <-w.gate }()
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	fail := func() { w.once.Do(func() { w.abort(); _ = w.pipe.Close() }) }
	info, err := w.pipe.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		fail()
		return 0, errCodexStdinWrite
	}
	n, err := writeCodexPipe(ctx, w.pipe, data, fail)
	if err != nil || n != len(data) {
		// An incomplete frame makes this stream unsafe for subsequent requests.
		fail()
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		return n, errCodexStdinWrite
	}
	if ctx.Err() != nil {
		return n, ctx.Err()
	}
	return n, nil
}

func (c *codexClient) writeRPCFrame(ctx context.Context, data []byte) (int, error) {
	if writer, ok := c.stdin.(interface {
		WriteContext(context.Context, []byte) (int, error)
	}); ok {
		return writer.WriteContext(ctx, data)
	}
	// Existing in-memory test writers do not own an OS pipe/process. They retain
	// their original behavior and are not evidence of bounded production I/O.
	return c.stdin.Write(data)
}
