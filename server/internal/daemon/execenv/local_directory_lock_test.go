package execenv

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalDirectoryLockHolderProcess(t *testing.T) {
	if os.Getenv("MULTICA_TEST_LOCAL_LOCK_CHILD") != "1" {
		return
	}
	release, err := AcquireLocalDirectoryLock(context.Background(), os.Getenv("MULTICA_TEST_LOCAL_LOCK_PATH"), nil)
	if err != nil {
		os.Exit(2)
	}
	if os.WriteFile(os.Getenv("MULTICA_TEST_LOCAL_LOCK_READY"), []byte("ready"), 0o600) != nil {
		os.Exit(3)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
	release()
	os.Exit(0)
}

func TestLocalDirectoryProcessLock(t *testing.T) {
	for _, crash := range []bool{false, true} {
		name := "release"
		if crash {
			name = "crash"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path, ready := filepath.Join(dir, "work"), filepath.Join(dir, "ready")
			child := exec.Command(os.Args[0], "-test.run=^TestLocalDirectoryLockHolderProcess$")
			child.Env = append(os.Environ(), "MULTICA_TEST_LOCAL_LOCK_CHILD=1", "MULTICA_TEST_LOCAL_LOCK_PATH="+path, "MULTICA_TEST_LOCAL_LOCK_READY="+ready)
			stdin, err := child.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = child.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			t.Cleanup(func() {
				_ = stdin.Close()
				if !waited {
					_ = child.Process.Kill()
					_ = child.Wait()
				}
			})
			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("child did not acquire lock")
				}
				time.Sleep(10 * time.Millisecond)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			calls := 0
			release, err := AcquireLocalDirectoryLock(ctx, path, func() { calls++ })
			cancel()
			if release != nil {
				release()
			}
			if !errors.Is(err, context.DeadlineExceeded) || calls != 1 {
				t.Fatalf("cross-process exclusion/cancellation: error=%v, wait calls=%d", err, calls)
			}
			other, err := AcquireLocalDirectoryLock(context.Background(), filepath.Join(dir, "other"), nil)
			if err != nil {
				t.Fatal(err)
			}
			other()

			if crash {
				if err := child.Process.Kill(); err != nil {
					t.Fatal(err)
				}
			} else {
				_ = stdin.Close()
			}
			err = child.Wait()
			waited = true
			if !crash && err != nil {
				t.Fatalf("child release: %v", err)
			}
			ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			release, err = AcquireLocalDirectoryLock(ctx, path, nil)
			if err != nil {
				t.Fatalf("lock not released on %s: %v", name, err)
			}
			release()
			release()
		})
	}
}
