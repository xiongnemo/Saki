//go:build windows

package instancelock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Supported reports whether this platform can detect other Saki processes.
const Supported = true

func lockFile(file *os.File, exclusive bool) error {
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	overlapped := new(windows.Overlapped)
	err := windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, overlapped)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return ErrLocked
	}
	return err
}

func unlockFile(file *os.File) error {
	return windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, new(windows.Overlapped))
}
