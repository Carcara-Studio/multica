package agent

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"
)

var errCodexStdinWrite = errors.New("codex stdin transport unavailable")

// The real process stdin is an os.Pipe writer, which supports I/O deadlines on
// the supported platforms. Never turn a failed deadline into an unbounded Write.
// The gate serializes whole frames and their deadline/reset callbacks.
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
	fail := func() { w.once.Do(func() { _ = w.pipe.Close(); w.abort() }) }
	// Use the context timer as the cancellation source so handshake/parent
	// causes are already established when the pipe reports its deadline.
	if err := w.pipe.SetWriteDeadline(time.Time{}); err != nil {
		fail()
		return 0, errCodexStdinWrite
	}
	callbackDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(callbackDone)
		_ = w.pipe.SetWriteDeadline(time.Now())
	})
	n, err := w.pipe.Write(data)
	// Join a callback that already started before clearing the deadline. Otherwise
	// a late cancellation could poison the next frame after it acquires the gate.
	if !stop() {
		<-callbackDone
	}
	resetErr := w.pipe.SetWriteDeadline(time.Time{})
	if err != nil || n != len(data) || resetErr != nil {
		// Even a zero-byte failure may leave peer/process state uncertain. Retire the
		// owned transport rather than send another JSON frame onto a damaged stream.
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
