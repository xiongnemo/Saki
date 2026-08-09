package selfinstall

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntime_defaults_and_validation(t *testing.T) {
	// Given
	runtime := SystemRuntime()
	options := DefaultOptions()

	// When
	installer, err := New(Options{Directory: "bin", AssumeYes: true}, runtime)

	// Then
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if options.Directory != "~/.local/bin" || options.AssumeYes {
		t.Fatalf("DefaultOptions() = %#v", options)
	}
	if !installer.options.AssumeYes {
		t.Fatal("New() did not retain AssumeYes")
	}

	tests := []struct {
		name         string
		breakRuntime func(*Runtime)
	}{
		{"GOOS", func(runtime *Runtime) { runtime.GOOS = "" }},
		{"Executable", func(runtime *Runtime) { runtime.Executable = nil }},
		{"UserHomeDir", func(runtime *Runtime) { runtime.UserHomeDir = nil }},
		{"Stdin", func(runtime *Runtime) { runtime.Stdin = nil }},
		{"Stdout", func(runtime *Runtime) { runtime.Stdout = nil }},
		{"Stderr", func(runtime *Runtime) { runtime.Stderr = nil }},
		{"Getenv", func(runtime *Runtime) { runtime.Getenv = nil }},
		{"Lstat", func(runtime *Runtime) { runtime.Lstat = nil }},
		{"EvalSymlinks", func(runtime *Runtime) { runtime.EvalSymlinks = nil }},
		{"Rename", func(runtime *Runtime) { runtime.Rename = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			broken := SystemRuntime()
			test.breakRuntime(&broken)
			_, err := New(Options{Directory: "bin"}, broken)
			assertErrorContains(t, err, "validate runtime", test.name)
		})
	}
}

func TestRuntime_rejects_blank_explicit_directory(t *testing.T) {
	// When
	_, err := New(Options{Directory: " \t"}, SystemRuntime())

	// Then
	assertErrorContains(t, err, "validate install directory", "blank")
}

func TestRuntime_preserves_nonblank_directory_whitespace(t *testing.T) {
	// Given
	directory := " directory with surrounding spaces "

	// When
	installer := mustNew(t, Options{Directory: directory}, SystemRuntime())

	// Then
	if installer.options.Directory != directory {
		t.Fatalf("directory = %q, want %q", installer.options.Directory, directory)
	}
}

func TestResolve_uses_canonical_leaf_for_renamed_source(t *testing.T) {
	for _, test := range []struct{ goos, leaf string }{{"linux", "saki"}, {"windows", "saki.exe"}} {
		t.Run(test.goos, func(t *testing.T) {
			// Given
			root := t.TempDir()
			source := writeSource(t, root)
			directory := filepath.Join(root, "missing-bin")
			runtime, stdout, stderr := testRuntime(source, root, test.goos)
			renames := 0
			runtime.Rename = func(_, _ string) error { renames++; return nil }
			runtime.Getenv = func(string) string { t.Fatal("Resolve read environment"); return "" }
			installer := mustNew(t, Options{Directory: directory}, runtime)

			// When
			paths, err := installer.Resolve()

			// Then
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if paths.Source != source || paths.Directory != directory || paths.Target != filepath.Join(directory, test.leaf) {
				t.Fatalf("Resolve() = %#v", paths)
			}
			if !paths.SourceMode.IsRegular() || renames != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("unexpected effects: mode=%v renames=%d stdout=%q stderr=%q", paths.SourceMode, renames, stdout, stderr)
			}
			if _, err := os.Stat(directory); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("Resolve mutated missing directory: %v", err)
			}
		})
	}
}

func TestResolve_rejects_invalid_sources_with_context(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name      string
		configure func(*Runtime)
		want      string
	}{
		{"resolver error", func(runtime *Runtime) {
			runtime.Executable = func() (string, error) { return "", errors.New("resolver failed") }
		}, "resolve source executable: resolver failed"},
		{"missing", func(runtime *Runtime) {
			runtime.Executable = func() (string, error) { return filepath.Join(root, "missing"), nil }
		}, "resolve source symlinks"},
		{"directory", func(runtime *Runtime) { runtime.Executable = func() (string, error) { return root, nil } }, "validate source"},
		{"filesystem", func(runtime *Runtime) {
			runtime.Lstat = func(path string) (fs.FileInfo, error) { return nil, errors.New("lstat failed: " + path) }
		}, "inspect source"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			source := writeSource(t, t.TempDir())
			runtime := runtimeFor(source, root, "linux")
			test.configure(&runtime)
			installer := mustNew(t, Options{Directory: filepath.Join(root, "bin")}, runtime)

			// When
			_, err := installer.Resolve()

			// Then
			assertErrorContains(t, err, test.want)
		})
	}
}

func TestTarget_rejects_nonregular_entries_and_bad_parents_without_mutation(t *testing.T) {
	for _, test := range []struct {
		name, fixture, want string
		mode                fs.FileMode
	}{
		{name: "target symlink", mode: fs.ModeSymlink, want: "symbolic link"},
		{name: "target named pipe", mode: fs.ModeNamedPipe, want: "not a regular file"},
		{name: "target directory", fixture: "target-directory", want: "not a regular file"},
		{name: "directory occupied by file", fixture: "directory-file", want: "not a directory"},
		{name: "non-directory parent", fixture: "parent-file", want: "install directory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Given
			root := t.TempDir()
			source := writeSource(t, root)
			runtime := runtimeFor(source, root, "linux")
			directory := filepath.Join(root, "bin")
			if test.mode != 0 {
				base := runtime.Lstat
				target := filepath.Join(directory, "saki")
				runtime.Lstat = func(path string) (fs.FileInfo, error) {
					if path == target {
						return stubInfo{mode: test.mode}, nil
					}
					return base(path)
				}
			}
			switch test.fixture {
			case "target-directory":
				mustMkdir(t, filepath.Join(directory, "saki"))
			case "directory-file":
				mustWrite(t, directory, "occupied")
			case "parent-file":
				parent := filepath.Join(root, "parent-file")
				mustWrite(t, parent, "occupied")
				directory = filepath.Join(parent, "child")
			}
			before, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			installer := mustNew(t, Options{Directory: directory}, runtime)

			// When
			for range 3 {
				_, err = installer.Resolve()
				assertErrorContains(t, err, test.want)
			}

			// Then
			after, err := os.ReadFile(source)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("validation mutated source: bytes=%q error=%v", after, err)
			}
		})
	}
}

func runtimeFor(source, home, goos string) Runtime {
	runtime := SystemRuntime()
	runtime.GOOS = goos
	runtime.Executable = func() (string, error) { return source, nil }
	runtime.UserHomeDir = func() (string, error) { return home, nil }
	return runtime
}

func testRuntime(source, home, goos string) (Runtime, *bytes.Buffer, *bytes.Buffer) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	runtime := runtimeFor(source, home, goos)
	runtime.Stdin, runtime.Stdout, runtime.Stderr = strings.NewReader(""), stdout, stderr
	return runtime, stdout, stderr
}

func writeSource(t *testing.T, root string) string {
	t.Helper()
	path := filepath.Join(root, "renamed-download.bin")
	if err := os.WriteFile(path, []byte("saki-test-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertErrorContains(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error %q does not contain %q", err, part)
		}
	}
}

func mustNew(t *testing.T, options Options, runtime Runtime) *Installer {
	t.Helper()
	installer, err := New(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	return installer
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

type stubInfo struct{ mode fs.FileMode }

func (info stubInfo) Name() string       { return "saki" }
func (info stubInfo) Size() int64        { return 0 }
func (info stubInfo) Mode() fs.FileMode  { return info.mode }
func (info stubInfo) ModTime() time.Time { return time.Time{} }
func (info stubInfo) IsDir() bool        { return info.mode.IsDir() }
func (info stubInfo) Sys() any           { return nil }
