package selfinstall

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

const defaultDirectory = "~/.local/bin"

// Options controls a self-install operation.
type Options struct {
	Directory string
	AssumeYes bool
}

// Runtime contains process and filesystem dependencies used by the installer.
type Runtime struct {
	GOOS         string
	Executable   func() (string, error)
	UserHomeDir  func() (string, error)
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	Getenv       func(string) string
	Lstat        func(string) (fs.FileInfo, error)
	EvalSymlinks func(string) (string, error)
	Rename       func(string, string) error
}

// Paths identifies the validated source and canonical install destination.
type Paths struct {
	Source     string
	Directory  string
	Target     string
	SourceMode fs.FileMode
}

// Installer holds validated options and runtime dependencies.
type Installer struct {
	options Options
	runtime Runtime
}

// DefaultOptions returns the cross-platform default install options.
func DefaultOptions() Options {
	return Options{Directory: defaultDirectory}
}

// SystemRuntime returns dependencies backed by the current process and OS.
func SystemRuntime() Runtime {
	return Runtime{
		GOOS:         goruntime.GOOS,
		Executable:   os.Executable,
		UserHomeDir:  os.UserHomeDir,
		Stdin:        os.Stdin,
		Stdout:       os.Stdout,
		Stderr:       os.Stderr,
		Getenv:       os.Getenv,
		Lstat:        os.Lstat,
		EvalSymlinks: filepath.EvalSymlinks,
		Rename:       os.Rename,
	}
}

// New validates options and dependencies without touching the filesystem.
func New(options Options, runtime Runtime) (*Installer, error) {
	if strings.TrimSpace(options.Directory) == "" {
		return nil, fmt.Errorf("validate install directory %q: blank path", options.Directory)
	}
	if err := validateRuntime(runtime); err != nil {
		return nil, fmt.Errorf("validate runtime: %w", err)
	}
	return &Installer{options: options, runtime: runtime}, nil
}

// Resolve validates and returns paths for a future install operation.
func (installer *Installer) Resolve() (Paths, error) {
	source, mode, err := installer.resolveSource()
	if err != nil {
		return Paths{}, err
	}
	directory, err := installer.normalizePath(installer.options.Directory)
	if err != nil {
		return Paths{}, fmt.Errorf("normalize install directory %q: %w", installer.options.Directory, err)
	}
	if err := installer.validateDirectory(directory); err != nil {
		return Paths{}, err
	}
	target := filepath.Join(directory, installer.targetLeaf())
	if err := installer.validateTarget(target); err != nil {
		return Paths{}, err
	}
	return Paths{Source: source, Directory: directory, Target: target, SourceMode: mode}, nil
}

func validateRuntime(runtime Runtime) error {
	if strings.TrimSpace(runtime.GOOS) == "" {
		return errors.New("GOOS is required")
	}
	missing := ""
	switch {
	case runtime.Executable == nil:
		missing = "Executable"
	case runtime.UserHomeDir == nil:
		missing = "UserHomeDir"
	case runtime.Stdin == nil:
		missing = "Stdin"
	case runtime.Stdout == nil:
		missing = "Stdout"
	case runtime.Stderr == nil:
		missing = "Stderr"
	case runtime.Getenv == nil:
		missing = "Getenv"
	case runtime.Lstat == nil:
		missing = "Lstat"
	case runtime.EvalSymlinks == nil:
		missing = "EvalSymlinks"
	case runtime.Rename == nil:
		missing = "Rename"
	}
	if missing != "" {
		return fmt.Errorf("%s is required", missing)
	}
	return nil
}

func (installer *Installer) resolveSource() (string, fs.FileMode, error) {
	raw, err := installer.runtime.Executable()
	if err != nil {
		return "", 0, fmt.Errorf("resolve source executable: %w", err)
	}
	if strings.TrimSpace(raw) == "" {
		return "", 0, errors.New("resolve source executable: empty path")
	}
	absolute, err := installer.absolutePath(raw)
	if err != nil {
		return "", 0, fmt.Errorf("normalize source path %q: %w", raw, err)
	}
	resolved, err := installer.runtime.EvalSymlinks(absolute)
	if err != nil {
		return "", 0, fmt.Errorf("resolve source symlinks %q: %w", absolute, err)
	}
	if strings.TrimSpace(resolved) == "" {
		return "", 0, fmt.Errorf("resolve source symlinks %q: empty path", absolute)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", 0, fmt.Errorf("make resolved source absolute %q: %w", resolved, err)
	}
	resolved = filepath.Clean(resolved)
	info, err := installer.runtime.Lstat(resolved)
	if err != nil {
		return "", 0, fmt.Errorf("inspect source %q: %w", resolved, err)
	}
	if !info.Mode().IsRegular() {
		return "", 0, fmt.Errorf("validate source %q: mode %s is not a regular file", resolved, info.Mode())
	}
	return resolved, info.Mode(), nil
}

func (installer *Installer) targetLeaf() string {
	if installer.runtime.GOOS == "windows" {
		return "saki.exe"
	}
	return "saki"
}

func (installer *Installer) validateDirectory(directory string) error {
	info, err := installer.runtime.Lstat(directory)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("validate install directory %q: mode %s is not a directory", directory, info.Mode())
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect install directory %q: %w", directory, err)
	}
	return installer.validateDirectoryAncestors(directory)
}

func (installer *Installer) validateDirectoryAncestors(directory string) error {
	for parent := filepath.Dir(directory); ; parent = filepath.Dir(parent) {
		info, err := installer.runtime.Lstat(parent)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("validate install directory parent %q: mode %s is not a directory", parent, info.Mode())
			}
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("inspect install directory parent %q: %w", parent, err)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return fmt.Errorf("inspect install directory parent %q: %w", parent, err)
		}
	}
}

func (installer *Installer) validateTarget(target string) error {
	info, err := installer.runtime.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect install target %q: %w", target, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("validate install target %q: symbolic link is not allowed", target)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("validate install target %q: mode %s is not a regular file", target, info.Mode())
	}
	return nil
}
