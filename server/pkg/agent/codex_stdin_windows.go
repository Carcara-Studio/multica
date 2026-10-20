package agent

import (
	"context"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/windows"
)

var codexCancelSynchronousIO = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

func writeCodexPipe(ctx context.Context, pipe *os.File, data []byte, retire func()) (int, error) {
	if err := codexCancelSynchronousIO.Find(); err != nil {
		return 0, err
	}
	// StdinPipe uses synchronous CreatePipe handles on Windows. Pin the writer
	// until cancellation has joined, so native cancellation cannot hit another
	// goroutine's I/O on a reused OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	thread, err := windows.OpenThread(windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(thread)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	written := make(chan struct{})
	joined := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(joined)
		for {
			select {
			case <-written:
				return
			default:
			}
			ok, _, cancelErr := codexCancelSynchronousIO.Call(uintptr(thread))
			if ok == 0 && cancelErr != windows.ERROR_NOT_FOUND {
				// The owned process closes the peer if native cancellation fails.
				retire()
				return
			}
			// Cancellation can race the interval just before WriteFile starts.
			// Retry only while this specific, pinned write remains outstanding.
			select {
			case <-written:
				return
			case <-time.After(time.Millisecond):
			}
		}
	})
	n, err := pipe.Write(data)
	close(written)
	if !stop() {
		<-joined
	}
	return n, err
}
