// Package instancelock lets running Saki processes and `saki migrate` see each
// other. Every TUI holds a shared lock on a file in the state directory, so any
// number of them can run at once; migrate needs the exclusive lock and so runs
// only when no TUI does.
package instancelock

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ErrLocked means another process holds a conflicting lock.
var ErrLocked = errors.New("locked by another Saki process")

// Lock is a held lock. Close releases it.
type Lock struct {
	file *os.File
}

// Shared takes a shared lock on path, creating the file and its directory.
func Shared(path string) (*Lock, error) { return acquire(path, false, true) }

// Exclusive takes the exclusive lock on path, creating the file and its
// directory.
func Exclusive(path string) (*Lock, error) { return acquire(path, true, true) }

// Probe reports whether another process holds a lock on path without creating
// anything. A missing lock file means no current Saki is running.
func Probe(path string) (bool, error) {
	lock, err := acquire(path, true, false)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case errors.Is(err, ErrLocked):
		return true, nil
	case err != nil:
		return false, err
	}
	return false, lock.Close()
}

func acquire(path string, exclusive, create bool) (*Lock, error) {
	flags := os.O_RDWR
	if create {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		flags |= os.O_CREATE
	}
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, exclusive); err != nil {
		_ = file.Close()
		if errors.Is(err, ErrLocked) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &Lock{file: file}, nil
}

// Close releases the lock. The file stays; deleting it would race with a
// process that just opened it.
func (l *Lock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	_ = unlockFile(l.file)
	err := l.file.Close()
	l.file = nil
	return err
}
