//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package instancelock

import (
	"errors"
	"os"
	"syscall"
)

// Supported reports whether this platform can detect other Saki processes.
const Supported = true

func lockFile(file *os.File, exclusive bool) error {
	how := syscall.LOCK_SH
	if exclusive {
		how = syscall.LOCK_EX
	}
	for {
		err := syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrLocked
		}
		return err
	}
}

func unlockFile(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
