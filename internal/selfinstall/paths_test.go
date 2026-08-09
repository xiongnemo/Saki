package selfinstall

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalize_expands_home_and_cleans_paths(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	installer := mustNew(t, Options{Directory: "bin"}, runtimeFor(source, root, "linux"))
	absolute, err := filepath.Abs(filepath.Join("relative", "..", "bin"))
	if err != nil {
		t.Fatal(err)
	}

	for raw, want := range map[string]string{
		"~/.local/../bin":                          filepath.Join(root, "bin"),
		filepath.Join("relative", "..", "bin"):     absolute,
		filepath.Join(root, "nested", "..", "bin"): filepath.Join(root, "bin"),
	} {
		got, err := installer.normalizePath(raw)
		if err != nil || got != want {
			t.Errorf("normalizePath(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}

	broken := runtimeFor(source, root, "linux")
	broken.UserHomeDir = func() (string, error) { return "", errors.New("home unavailable") }
	installer = mustNew(t, DefaultOptions(), broken)
	_, err = installer.Resolve()
	assertErrorContains(t, err, "resolve user home for path \"~/.local/bin\"", "home unavailable")
}

func TestNormalize_resolves_existing_symlink_prefix(t *testing.T) {
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	mustMkdir(t, realDirectory)
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realDirectory, alias); err != nil {
		t.Skipf("create directory symlink: %v", err)
	}
	source := writeSource(t, root)
	installer := mustNew(t, Options{Directory: root}, runtimeFor(source, root, "linux"))
	aliasTarget := filepath.Join(alias, "missing", "saki")
	realTarget := filepath.Join(realDirectory, "missing", "saki")

	got, err := installer.normalizePath(aliasTarget)
	if err != nil || got != realTarget {
		t.Fatalf("normalizePath() = %q, %v; want %q", got, err, realTarget)
	}
	same, err := installer.samePath(aliasTarget, realTarget)
	if err != nil || !same {
		t.Fatalf("samePath() = %t, %v; want true", same, err)
	}
}

func TestNormalize_same_path_uses_injected_platform(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	upper := strings.ToUpper(filepath.Join(root, "CasePath"))
	lower := strings.ToLower(filepath.Join(root, "CasePath"))

	for _, test := range []struct {
		goos string
		want bool
	}{{"linux", false}, {"windows", true}} {
		installer := mustNew(t, Options{Directory: root}, runtimeFor(source, root, test.goos))
		got, err := installer.samePath(upper, lower)
		if err != nil || got != test.want {
			t.Errorf("samePath() on %s = %t, %v; want %t", test.goos, got, err, test.want)
		}
		got, err = installer.samePath(filepath.Join("."), filepath.Clean("."))
		if err != nil || !got {
			t.Errorf("relative/absolute samePath() = %t, %v", got, err)
		}
	}
}

func TestNormalize_same_path_rejects_blank_inputs(t *testing.T) {
	root := t.TempDir()
	source := writeSource(t, root)
	installer := mustNew(t, Options{Directory: root}, runtimeFor(source, root, "linux"))

	for _, test := range []struct {
		name        string
		left, right string
		want        string
	}{
		{name: "blank left", left: "", right: root, want: "normalize left path"},
		{name: "whitespace left", left: " \t", right: root, want: "normalize left path"},
		{name: "blank right", left: root, right: "", want: "normalize right path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := installer.samePath(test.left, test.right)
			assertErrorContains(t, err, test.want, "blank path")
		})
	}
}
