package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func codexStdinPipe(t *testing.T, abort func()) (*os.File, *codexStdin) {
	t.Helper()
	r, p, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(); _ = r.Close() })
	return r, newCodexStdin(p, func() { _ = r.Close(); abort() })
}

type codexStdinResult struct {
	n   int
	err error
}

func awaitCodexStdinWrite(t *testing.T, done <-chan codexStdinResult) codexStdinResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("stdin write ignored cancellation")
		return codexStdinResult{}
	}
}
func requireCodexStdinStarted(t *testing.T, r *os.File) {
	t.Helper()
	if _, err := io.ReadFull(r, make([]byte, 1)); err != nil {
		t.Fatalf("write did not reach pipe: %v", err)
	}
}

func TestCodexStdinCancelsBlockedWrite(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
			var aborts atomic.Int32
			r, w := codexStdinPipe(t, func() { aborts.Add(1) })
			ctx, cancel := context.WithCancel(t.Context())
			if deadline {
				ctx, cancel = context.WithTimeout(t.Context(), 500*time.Millisecond)
			}
			defer cancel()
			payload := bytes.Repeat([]byte("private-frame"), 128<<10)
			done := make(chan codexStdinResult, 1)
			go func() { n, err := w.WriteContext(ctx, payload); done <- codexStdinResult{n, err} }()
			requireCodexStdinStarted(t, r)
			if !deadline {
				cancel()
			}
			result := awaitCodexStdinWrite(t, done)
			want := context.Canceled
			if deadline {
				want = context.DeadlineExceeded
			}
			if !errors.Is(result.err, want) || result.n == len(payload) || aborts.Load() != 1 {
				t.Fatalf("blocked write not retired: n=%d err=%v aborts=%d", result.n, result.err, aborts.Load())
			}
			if _, err := w.Write([]byte("next-frame")); err != errCodexStdinWrite || aborts.Load() != 1 {
				t.Fatal("damaged stream reused or owner aborted twice")
			}
		})
	}
}

func TestCodexStdinBoundsBackgroundWrite(t *testing.T) {
	var aborts atomic.Int32
	r, w := codexStdinPipe(t, func() { aborts.Add(1) })
	w.maxWait = 500 * time.Millisecond // Fixture only; production always uses the fixed cap.
	done := make(chan codexStdinResult, 1)
	go func() { n, err := w.Write(bytes.Repeat([]byte("x"), 1<<20)); done <- codexStdinResult{n, err} }()
	requireCodexStdinStarted(t, r)
	if result := awaitCodexStdinWrite(t, done); !errors.Is(result.err, context.DeadlineExceeded) || aborts.Load() != 1 {
		t.Fatal("notification/response write remained unbounded")
	}
}

func TestCodexStdinGateCancellationDoesNotAbortAnotherFrame(t *testing.T) {
	var aborts atomic.Int32
	r, w := codexStdinPipe(t, func() { aborts.Add(1) })
	owner, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan codexStdinResult, 1)
	go func() {
		n, err := w.WriteContext(owner, bytes.Repeat([]byte("x"), 1<<20))
		done <- codexStdinResult{n, err}
	}()
	requireCodexStdinStarted(t, r)
	queued, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	n, err := w.WriteContext(queued, []byte("queued-frame"))
	if n != 0 || !errors.Is(err, context.DeadlineExceeded) || aborts.Load() != 0 {
		t.Fatal("cancelled gate waiter disturbed another frame")
	}
	cancel()
	if result := awaitCodexStdinWrite(t, done); !errors.Is(result.err, context.Canceled) || aborts.Load() != 1 {
		t.Fatal("active frame did not retire after its own cancellation")
	}
}

func TestCodexStdinSerializesFramesAndResetsDeadlines(t *testing.T) {
	var aborts atomic.Int32
	r, w := codexStdinPipe(t, func() { aborts.Add(1) })
	const count = 8
	read := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(r)
		seen := map[int]bool{}
		for range count + 1 {
			var frame struct {
				ID      int    `json:"id"`
				Payload string `json:"payload"`
			}
			if err := decoder.Decode(&frame); err != nil {
				read <- err
				return
			}
			if seen[frame.ID] || frame.Payload != strings.Repeat("x", 16<<10) {
				read <- errors.New("interleaved frame")
				return
			}
			seen[frame.ID] = true
		}
		read <- nil
	}()
	var wg sync.WaitGroup
	failures := make(chan error, count)
	for id := range count {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			raw, _ := json.Marshal(map[string]any{"id": id, "payload": strings.Repeat("x", 16<<10)})
			_, err := w.WriteContext(ctx, append(raw, '\n'))
			cancel() // A stopped cancellation callback must not poison the next frame.
			failures <- err
		})
	}
	wg.Wait()
	for range count {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	last, _ := json.Marshal(map[string]any{"id": count, "payload": strings.Repeat("x", 16<<10)})
	if _, err := w.Write(append(last, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := <-read; err != nil || aborts.Load() != 0 {
		t.Fatalf("frame/reset failure: %v", err)
	}
}

func TestCodexStdinRejectsUnsupportedAndPreCancelledWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private-path")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var aborts atomic.Int32
	w := newCodexStdin(file, func() { aborts.Add(1) })
	if _, err := w.Write([]byte("private-content")); err != errCodexStdinWrite || aborts.Load() != 1 {
		t.Fatal("unsupported destination reached I/O or leaked an error")
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		t.Fatal("unsupported destination received data")
	}
	r, pipe := codexStdinPipe(t, func() { aborts.Add(1) })
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if n, err := pipe.WriteContext(ctx, []byte("must-not-send")); n != 0 || !errors.Is(err, context.Canceled) || aborts.Load() != 1 {
		t.Fatal("pre-cancelled write touched transport")
	}
	done := make(chan error, 1)
	go func() { _, err := io.ReadFull(r, make([]byte, 1)); done <- err }()
	if _, err := pipe.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil || aborts.Load() != 1 {
		t.Fatal("pre-cancelled caller retired a healthy stream")
	}
}

func TestCodexStdinRPCTimeoutRetiresClientAndPreservesCause(t *testing.T) {
	for _, parentFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "handshake", true: "parent"}[parentFirst], func(t *testing.T) {
			c, _, _ := newTestCodexClient(t)
			var aborts atomic.Int32
			r, w := codexStdinPipe(t, func() { aborts.Add(1); c.markProcessExited(errCodexStdinWrite) })
			c.stdin = w
			observer, err := newCodexThreadObserver(c, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			c.handshakeTimeout = 500 * time.Millisecond
			budget := 2 * time.Second
			if parentFirst {
				c.handshakeTimeout = 2 * time.Second
				budget = 500 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), budget)
			defer cancel()
			done := make(chan codexStdinResult, 1)
			go func() {
				_, err := c.request(ctx, "initialize", map[string]any{"payload": strings.Repeat("private-data", 128<<10)})
				done <- codexStdinResult{err: err}
			}()
			requireCodexStdinStarted(t, r)
			result := awaitCodexStdinWrite(t, done)
			var handshake *codexHandshakeTimeoutError
			if !errors.Is(result.err, context.DeadlineExceeded) || errors.As(result.err, &handshake) == parentFirst {
				t.Fatalf("wrong timeout cause: %v", result.err)
			}
			if aborts.Load() != 1 {
				t.Fatal("owned process was not aborted exactly once")
			}
			c.mu.Lock()
			pending, dead := len(c.pending), c.processErr != nil
			c.mu.Unlock()
			if pending != 0 || !dead || c.rpcActive.Load() != 0 {
				t.Fatal("write timeout left pending/live client authority")
			}
			requireNoThreadObservation(t, observer)
		})
	}
}
