//go:build !windows

package agent

import (
	"context"
	"os"
	"time"
)

func writeCodexPipe(ctx context.Context, pipe *os.File, data []byte, _ func()) (int, error) {
	if err := pipe.SetWriteDeadline(time.Time{}); err != nil {
		return 0, err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		_ = pipe.SetWriteDeadline(time.Now())
	})
	n, err := pipe.Write(data)
	// Join before resetting: a late callback must not poison the next frame.
	if !stop() {
		<-done
	}
	if resetErr := pipe.SetWriteDeadline(time.Time{}); err == nil {
		err = resetErr
	}
	return n, err
}
