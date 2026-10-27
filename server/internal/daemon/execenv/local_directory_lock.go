package execenv

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// AcquireLocalDirectoryLock extends the daemon's local queue across profiles.
// The caller supplies its resolved directory path. Stable lock files must not
// be removed: unlinking a held file would allow a new inode to bypass the lock.
func AcquireLocalDirectoryLock(ctx context.Context, realPath string, onWait func()) (func(), error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("local directory lock cache: %w", err)
	}
	key := filepath.Clean(realPath)
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	sum := sha256.Sum256([]byte(key))
	path := filepath.Join(root, "multica-local-directory-locks", fmt.Sprintf("%x.lock", sum))
	return acquireLocalDirectoryLock(ctx, path, onWait)
}

func acquireLocalDirectoryLock(ctx context.Context, path string, onWait func()) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create local directory lock root: %w", err)
	}
	f, err := openLockFile(path)
	if err != nil {
		return nil, fmt.Errorf("open local directory lock: %w", err)
	}
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	announced := false
	for {
		if err := ctx.Err(); err != nil {
			_ = f.Close()
			return nil, err
		}
		locked, err := lockFileExclusiveNonBlocking(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("lock local directory: %w", err)
		}
		if locked {
			var once sync.Once
			return func() { once.Do(func() { releaseLockFile(f) }) }, nil
		}
		if !announced {
			announced = true
			if onWait != nil {
				onWait()
			}
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
