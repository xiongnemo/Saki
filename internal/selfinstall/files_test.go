package selfinstall

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestInstall_creates_directory_and_installs_canonical_target(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	directory := filepath.Join(root, "bin")
	runtime, stdout, stderr := testRuntime(source, root, "linux")
	runtime.Getenv = func(key string) string {
		if key == "PATH" {
			return ""
		}
		return ""
	}
	installer := mustNew(t, Options{Directory: directory, AssumeYes: true}, runtime)

	if err := installer.Install(); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(directory, "saki"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || !strings.Contains(stdout.String(), "Installed Saki") || stderr.Len() != 0 {
		t.Fatalf("install result bytes=%q stdout=%q stderr=%q", got, stdout, stderr)
	}
}

func TestInstall_prompts_for_missing_directory_and_declines_by_default(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	directory := filepath.Join(root, "bin")
	runtime, stdout, stderr := testRuntime(source, root, "linux")
	runtime.Getenv = func(string) string { return "" }
	runtime.Stdin = strings.NewReader("\n")
	installer := mustNew(t, Options{Directory: directory}, runtime)

	if err := installer.Install(); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("declined install directory stat error = %v", err)
	}
	if !strings.Contains(stderr.String(), "Create install directory") || !strings.Contains(stdout.String(), "cancelled") {
		t.Fatalf("prompt/status output = stdout:%q stderr:%q", stdout, stderr)
	}
}

func TestInstall_invalid_answer_loops_then_accepts(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	runtime, _, stderr := testRuntime(source, root, "linux")
	runtime.Getenv = func(string) string { return "" }
	runtime.Stdin = strings.NewReader("maybe\ny\n")
	installer := mustNew(t, Options{Directory: filepath.Join(root, "bin")}, runtime)

	if err := installer.Install(); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !strings.Contains(stderr.String(), "Please answer yes or no") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestInstall_identical_content_is_idempotent_and_repairs_mode(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix execute bits through os.Chmod")
	}
	root := t.TempDir()
	source := writeSource(t, root)
	directory := filepath.Join(root, "bin")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "saki")
	if err := os.WriteFile(target, []byte("saki-test-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, stdout, stderr := testRuntime(source, root, "linux")
	runtime.Getenv = func(string) string { return "" }
	runtime.Stdin = strings.NewReader("n\n")
	installer := mustNew(t, Options{Directory: directory}, runtime)

	if err := installer.Install(); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 || !strings.Contains(stdout.String(), "Already up to date") || stderr.Len() != 0 {
		t.Fatalf("idempotent result mode=%v stdout=%q stderr=%q", info.Mode().Perm(), stdout, stderr)
	}
}

func TestInstall_replaces_different_content_after_confirmation(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	directory := filepath.Join(root, "bin")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "saki")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtime, _, stderr := testRuntime(source, root, "linux")
	runtime.Getenv = func(string) string { return "" }
	runtime.Stdin = strings.NewReader("yes\n")
	installer := mustNew(t, Options{Directory: directory}, runtime)

	if err := installer.Install(); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "saki-test-binary" || !strings.Contains(stderr.String(), "Replace existing") {
		t.Fatalf("replacement bytes=%q stderr=%q", got, stderr)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".saki-") {
			t.Fatalf("temporary artifact remains: %s", entry.Name())
		}
	}
}

func TestInstall_activation_failure_restores_backup(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	directory := filepath.Join(root, "bin")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "saki")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtime, _, _ := testRuntime(source, root, "linux")
	runtime.Getenv = func(string) string { return "" }
	runtime.Rename = func(from, to string) error {
		if strings.Contains(filepath.Base(from), ".saki-stage-") && to == target {
			return errors.New("activation blocked")
		}
		return os.Rename(from, to)
	}
	installer := mustNew(t, Options{Directory: directory, AssumeYes: true}, runtime)

	err := installer.Install()
	if err == nil || !strings.Contains(err.Error(), "activate staged install") {
		t.Fatalf("Install() error = %v", err)
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil || string(got) != "old" {
		t.Fatalf("restored target bytes=%q error=%v", got, readErr)
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".saki-") {
			t.Fatalf("temporary artifact remains: %s", entry.Name())
		}
	}
}

func TestInstall_activation_and_restore_failure_preserves_backup(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	directory := filepath.Join(root, "bin")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "saki")
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtime, _, _ := testRuntime(source, root, "linux")
	runtime.Getenv = func(string) string { return "" }
	runtime.Rename = func(from, to string) error {
		if strings.Contains(filepath.Base(from), ".saki-stage-") && to == target {
			return errors.New("activation blocked")
		}
		if strings.Contains(filepath.Base(from), ".saki-backup-") && to == target {
			return errors.New("restore blocked")
		}
		return os.Rename(from, to)
	}
	installer := mustNew(t, Options{Directory: directory, AssumeYes: true}, runtime)

	err := installer.Install()
	if err == nil || !strings.Contains(err.Error(), "backup preserved") {
		t.Fatalf("Install() error = %v", err)
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil {
		t.Fatal(readErr)
	}
	foundBackup := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".saki-backup-") {
			foundBackup = true
			bytes, err := os.ReadFile(filepath.Join(directory, entry.Name()))
			if err != nil || string(bytes) != "old" {
				t.Fatalf("backup bytes=%q error=%v", bytes, err)
			}
		}
		if strings.HasPrefix(entry.Name(), ".saki-stage-") {
			t.Fatalf("temporary stage remains: %s", entry.Name())
		}
	}
	if !foundBackup {
		t.Fatal("restore failure did not preserve backup")
	}
}
