package selfinstall

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

func (installer *Installer) normalizePath(raw string) (string, error) {
	absolute, err := installer.absolutePath(raw)
	if err != nil {
		return "", err
	}
	resolved, err := installer.runtime.EvalSymlinks(absolute)
	if err == nil {
		return absoluteResolvedPath(absolute, resolved)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("resolve path symlinks %q: %w", absolute, err)
	}
	return installer.resolveExistingPrefix(absolute)
}

func absoluteResolvedPath(original, resolved string) (string, error) {
	if strings.TrimSpace(resolved) == "" {
		return "", fmt.Errorf("resolve path symlinks %q: empty path", original)
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("make resolved path absolute %q: %w", resolved, err)
	}
	return filepath.Clean(absolute), nil
}

func (installer *Installer) resolveExistingPrefix(path string) (string, error) {
	current := path
	var suffix []string
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return path, nil
		}
		suffix = append(suffix, filepath.Base(current))
		resolved, err := installer.runtime.EvalSymlinks(parent)
		if err == nil {
			prefix, err := absoluteResolvedPath(parent, resolved)
			if err != nil {
				return "", err
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				prefix = filepath.Join(prefix, suffix[index])
			}
			return filepath.Clean(prefix), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve path symlinks %q: %w", parent, err)
		}
		current = parent
	}
}

func (installer *Installer) absolutePath(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("validate path %q: blank path", raw)
	}
	path := raw
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := installer.runtime.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home for path %q: %w", raw, err)
		}
		if strings.TrimSpace(home) == "" {
			return "", fmt.Errorf("resolve user home for path %q: empty home directory", raw)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, filepath.FromSlash(strings.ReplaceAll(path[2:], `\`, "/")))
		}
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("make path absolute %q: %w", path, err)
	}
	return filepath.Clean(absolute), nil
}

func (installer *Installer) samePath(left, right string) (bool, error) {
	leftPath, err := installer.normalizePath(left)
	if err != nil {
		return false, fmt.Errorf("normalize left path %q: %w", left, err)
	}
	rightPath, err := installer.normalizePath(right)
	if err != nil {
		return false, fmt.Errorf("normalize right path %q: %w", right, err)
	}
	if installer.runtime.GOOS == "windows" {
		return strings.EqualFold(leftPath, rightPath), nil
	}
	return leftPath == rightPath, nil
}
