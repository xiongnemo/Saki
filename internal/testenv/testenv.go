// Package testenv gives tests a throwaway home directory so that nothing they
// do can reach the real user's config, library or caches.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// xdgVars are cleared so that a CI runner's own XDG_CONFIG_HOME and friends
// cannot point the tests back at a real directory.
var xdgVars = []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR"}

// Main runs a package's tests with HOME and USERPROFILE set to a new
// temporary directory, APPDATA and LOCALAPPDATA below it and the XDG
// variables cleared. Call it from TestMain. Do not use it in packages whose
// tests start the go command: its build and module caches live in the home.
func Main(m *testing.M) int {
	home, err := os.MkdirTemp("", "saki-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	defer os.RemoveAll(home)
	for key, value := range values(home) {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintln(os.Stderr, "testenv:", err)
			return 1
		}
	}
	return m.Run()
}

// Home gives one test a fresh home the same way and returns it.
func Home(t testing.TB) string {
	t.Helper()
	home := t.TempDir()
	for key, value := range values(home) {
		t.Setenv(key, value)
	}
	return home
}

func values(home string) map[string]string {
	env := map[string]string{
		"HOME":         home,
		"USERPROFILE":  home,
		"APPDATA":      filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
	}
	for _, key := range xdgVars {
		env[key] = ""
	}
	return env
}
