package selfinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathReport_target_directory_on_path_is_reachable(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	targetDirectory := filepath.Join(root, "bin")
	otherDirectory := filepath.Join(root, "other")
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	runtime := runtimeFor(source, root, "linux")
	runtime.Getenv = func(key string) string {
		if key == "PATH" {
			return targetDirectory + runtime.PathSeparator + otherDirectory
		}
		if key == "SHELL" {
			return "/bin/bash"
		}
		return ""
	}
	installer := mustNew(t, Options{Directory: targetDirectory}, runtime)
	report := installer.pathReport(Paths{Directory: targetDirectory, Target: filepath.Join(targetDirectory, "saki")})
	if !report.DirectoryOnPath || !report.Reachable || report.ShadowedBy != "" || report.CommandPath == "" {
		t.Fatalf("path report = %#v", report)
	}
}

func TestPathReport_detects_earlier_executable_shadowing(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	shadowDirectory := filepath.Join(root, "shadow")
	targetDirectory := filepath.Join(root, "bin")
	if err := os.MkdirAll(shadowDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	shadow := filepath.Join(shadowDirectory, "saki")
	if err := os.WriteFile(shadow, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtime := runtimeFor(source, root, "linux")
	baseLstat := runtime.Lstat
	runtime.Lstat = func(path string) (os.FileInfo, error) {
		if path == shadow {
			return stubInfo{mode: 0o755}, nil
		}
		return baseLstat(path)
	}
	runtime.Getenv = func(key string) string {
		if key == "PATH" {
			return shadowDirectory + runtime.PathSeparator + targetDirectory
		}
		return ""
	}
	installer := mustNew(t, Options{Directory: targetDirectory}, runtime)
	report := installer.pathReport(Paths{Directory: targetDirectory, Target: filepath.Join(targetDirectory, "saki")})
	if !report.DirectoryOnPath || report.Reachable || report.ShadowedBy != shadow {
		t.Fatalf("path report = %#v, want shadow %q", report, shadow)
	}
}

func TestPathReport_ignores_nonexecutable_unix_shadow(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	shadowDirectory := filepath.Join(root, "shadow")
	targetDirectory := filepath.Join(root, "bin")
	if err := os.MkdirAll(shadowDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shadowDirectory, "saki"), []byte("not executable"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime := runtimeFor(source, root, "linux")
	runtime.Getenv = func(key string) string {
		if key == "PATH" {
			return shadowDirectory + runtime.PathSeparator + targetDirectory
		}
		return ""
	}
	installer := mustNew(t, Options{Directory: targetDirectory}, runtime)
	report := installer.pathReport(Paths{Directory: targetDirectory, Target: filepath.Join(targetDirectory, "saki")})
	if !report.DirectoryOnPath || !report.Reachable || report.ShadowedBy != "" {
		t.Fatalf("path report = %#v", report)
	}
}

func TestPathReport_windows_uses_PATHEXT_and_case_folding(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	shadowDirectory := filepath.Join(root, "shadow")
	targetDirectory := filepath.Join(root, "bin")
	if err := os.MkdirAll(shadowDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	shadow := filepath.Join(shadowDirectory, "SAKI.BAT")
	if err := os.WriteFile(shadow, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime := runtimeFor(source, root, "windows")
	runtime.Getenv = func(key string) string {
		switch key {
		case "PATH":
			return shadowDirectory + ";" + strings.ToUpper(targetDirectory)
		case "PATHEXT":
			return ".BAT;.EXE"
		case "PSModulePath":
			return "present"
		default:
			return ""
		}
	}
	installer := mustNew(t, Options{Directory: targetDirectory}, runtime)
	report := installer.pathReport(Paths{Directory: targetDirectory, Target: filepath.Join(targetDirectory, "saki.exe")})
	if !report.DirectoryOnPath || report.Reachable || !strings.EqualFold(report.ShadowedBy, shadow) {
		t.Fatalf("path report = %#v, want shadow %q", report, shadow)
	}
	if !strings.Contains(report.Hint, "PowerShell") || !strings.Contains(report.Hint, "advisory only") {
		t.Fatalf("windows hint = %q", report.Hint)
	}
}

func TestPathReport_missing_directory_returns_quoted_advisory_hint(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	directory := filepath.Join(root, "bin with space")
	runtime := runtimeFor(source, root, "linux")
	runtime.Getenv = func(key string) string {
		if key == "PATH" {
			return ""
		}
		if key == "SHELL" {
			return "/bin/zsh"
		}
		return ""
	}
	installer := mustNew(t, Options{Directory: directory}, runtime)
	report := installer.pathReport(Paths{Directory: directory, Target: filepath.Join(directory, "saki")})
	if report.DirectoryOnPath || report.Reachable || !strings.Contains(report.Hint, "'C:") || !strings.Contains(report.Hint, "bin with space") || !strings.Contains(report.Hint, "advisory only") {
		t.Fatalf("path report = %#v", report)
	}
}
