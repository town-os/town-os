package packages

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// atomicWriteJSON writes v as indented JSON to path atomically. It writes to
// a temporary file in the same directory and renames it into place so that
// readers never see a partially-written file.
func atomicWriteJSON(path string, v any) (err error) {
	dir := filepath.Dir(path)

	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := f.Name()

	defer func() {
		_, statErr := os.Stat(tmpPath)
		if statErr == nil {
			err = errors.Join(err, os.Remove(tmpPath))
		}
	}()

	en := json.NewEncoder(f)
	en.SetIndent("", "  ")
	err = en.Encode(v)
	if err != nil {
		return errors.Join(err, f.Close())
	}

	err = f.Close()
	if err != nil {
		return err
	}

	return os.Rename(tmpPath, path)
}

// fileLock represents an exclusive advisory lock on a file.
type fileLock struct {
	f *os.File
}

// lockDir acquires an exclusive advisory lock on the given directory by
// creating/opening a .lock file inside it. The caller must call Unlock
// when done.
func lockDir(dir string) (_ *fileLock, err error) {
	safeLockPath, err := SafePath(dir, ".lock")
	if err != nil {
		return nil, fmt.Errorf("validate lock path: %w", err)
	}
	f, err := os.OpenFile(safeLockPath, os.O_CREATE|os.O_RDWR, 0600) //nolint:gosec // safeLockPath validated by SafePath above
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("acquire lock: %w", err), f.Close())
	}

	return &fileLock{f: f}, nil
}

// Unlock releases the advisory lock and closes the lock file.
func (l *fileLock) Unlock() error {
	unlockErr := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return errors.Join(unlockErr, l.f.Close())
}
