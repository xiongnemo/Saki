package selfinstall

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

func (installer *Installer) copyToStage(paths Paths) (string, error) {
	stage, err := installer.uniquePath(paths.Directory, ".saki-stage")
	if err != nil {
		return "", err
	}
	file, err := installer.runtime.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create temporary install %q: %w", stage, err)
	}
	source, err := installer.runtime.Open(paths.Source)
	if err != nil {
		_ = file.Close()
		_ = installer.runtime.Remove(stage)
		return "", fmt.Errorf("open source executable %q: %w", paths.Source, err)
	}
	_, copyErr := io.Copy(file, source)
	sourceCloseErr := source.Close()
	if copyErr != nil {
		_ = file.Close()
		_ = installer.runtime.Remove(stage)
		return "", fmt.Errorf("copy source executable %q to %q: %w", paths.Source, stage, copyErr)
	}
	if sourceCloseErr != nil {
		_ = file.Close()
		_ = installer.runtime.Remove(stage)
		return "", fmt.Errorf("close source executable %q: %w", paths.Source, sourceCloseErr)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = installer.runtime.Remove(stage)
		return "", fmt.Errorf("sync temporary install %q: %w", stage, err)
	}
	if err := file.Close(); err != nil {
		_ = installer.runtime.Remove(stage)
		return "", fmt.Errorf("close temporary install %q: %w", stage, err)
	}
	if installer.runtime.GOOS != "windows" {
		if err := installer.runtime.Chmod(stage, executableMode(paths.SourceMode)); err != nil {
			_ = installer.runtime.Remove(stage)
			return "", fmt.Errorf("set executable permissions on %q: %w", stage, err)
		}
	}
	return stage, nil
}

func (installer *Installer) sameContent(left, right string) (bool, error) {
	leftSize, leftHash, err := installer.digest(left)
	if err != nil {
		return false, fmt.Errorf("hash %q: %w", left, err)
	}
	rightSize, rightHash, err := installer.digest(right)
	if err != nil {
		return false, fmt.Errorf("hash %q: %w", right, err)
	}
	return leftSize == rightSize && leftHash == rightHash, nil
}

func (installer *Installer) digest(path string) (int64, [sha256.Size]byte, error) {
	file, err := installer.runtime.Open(path)
	if err != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("open file: %w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("read file: %w", copyErr)
	}
	if closeErr != nil {
		return 0, [sha256.Size]byte{}, fmt.Errorf("close file: %w", closeErr)
	}
	var sum [sha256.Size]byte
	copy(sum[:], hash.Sum(nil))
	return size, sum, nil
}

func executableMode(sourceMode fs.FileMode) fs.FileMode {
	mode := sourceMode.Perm()
	if mode&0o111 == 0 {
		return 0o755
	}
	return mode
}

func (installer *Installer) repairExecutableMode(path string, sourceMode fs.FileMode) error {
	if installer.runtime.GOOS == "windows" {
		return nil
	}
	if err := installer.runtime.Chmod(path, executableMode(sourceMode)); err != nil {
		return fmt.Errorf("set executable permissions on %q: %w", path, err)
	}
	return nil
}

func (installer *Installer) uniquePath(directory, prefix string) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		candidate := filepath.Join(directory, fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), attempt))
		_, err := installer.runtime.Lstat(candidate)
		if errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("inspect temporary install path %q: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("allocate temporary install path in %q: exhausted candidates", directory)
}

func (installer *Installer) pathExists(path string) (bool, error) {
	_, err := installer.runtime.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}
