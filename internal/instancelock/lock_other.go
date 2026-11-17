//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package instancelock

import "os"

// Supported reports whether this platform can detect other Saki processes.
const Supported = false

func lockFile(*os.File, bool) error { return nil }

func unlockFile(*os.File) error { return nil }
