package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEnv(t *testing.T, goos string, vars map[string]string) Env {
	t.Helper()
	return Env{GOOS: goos, Home: t.TempDir(), Getenv: func(key string) string { return vars[key] }}
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(path), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBaseDirectories(t *testing.T) {
	abs := t.TempDir()
	tests := []struct {
		name  string
		value string
		want  func(home string) string
	}{
		{"unset", "", nil},
		{"absolute", filepath.Join(abs, "base"), func(string) string { return filepath.Join(abs, "base") }},
		{"relative", filepath.Join("relative", "base"), nil},
	}
	dirs := []struct {
		variable string
		get      func(Env) string
		fallback []string
	}{
		{"XDG_CONFIG_HOME", Env.ConfigDir, []string{".config"}},
		{"XDG_DATA_HOME", Env.DataDir, []string{".local", "share"}},
		{"XDG_STATE_HOME", Env.StateDir, []string{".local", "state"}},
		{"XDG_CACHE_HOME", Env.CacheDir, []string{".cache"}},
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		for _, dir := range dirs {
			for _, test := range tests {
				t.Run(goos+"/"+dir.variable+"/"+test.name, func(t *testing.T) {
					env := testEnv(t, goos, map[string]string{dir.variable: test.value})
					want := filepath.Join(append([]string{env.Home}, dir.fallback...)...)
					if test.want != nil {
						want = test.want(env.Home)
					}
					if got := dir.get(env); got != filepath.Join(want, "saki") {
						t.Fatalf("%s = %q, want %q", dir.variable, got, filepath.Join(want, "saki"))
					}
				})
			}
		}
	}
}

func TestRuntimeDir(t *testing.T) {
	abs := t.TempDir()
	if dir, ok := testEnv(t, "linux", map[string]string{"XDG_RUNTIME_DIR": abs}).RuntimeDir(); !ok || dir != filepath.Join(abs, "saki") {
		t.Fatalf("absolute XDG_RUNTIME_DIR = %q, %v", dir, ok)
	}
	for _, value := range []string{"", "run/user/1000"} {
		if dir, ok := testEnv(t, "linux", map[string]string{"XDG_RUNTIME_DIR": value}).RuntimeDir(); ok {
			t.Fatalf("XDG_RUNTIME_DIR=%q gave %q", value, dir)
		}
	}
}

func TestSystemUsesHomeFromEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	env, err := System()
	if err != nil {
		t.Fatal(err)
	}
	if env.Home != home {
		t.Fatalf("home = %q, want %q", env.Home, home)
	}
}

func TestLegacyCacheRoots(t *testing.T) {
	abs := t.TempDir()
	local := filepath.Join(abs, "AppData", "Local")
	tests := []struct {
		name string
		goos string
		vars map[string]string
		want func(home string) []string
	}{
		{"windows", "windows", map[string]string{"LOCALAPPDATA": local}, func(string) []string {
			return []string{filepath.Join(local, "saki"), filepath.Join(local, "Saki")}
		}},
		{"windows without LOCALAPPDATA", "windows", nil, func(string) []string { return nil }},
		{"darwin", "darwin", map[string]string{"XDG_CACHE_HOME": abs}, func(home string) []string {
			return []string{filepath.Join(home, "Library", "Caches", "saki")}
		}},
		{"linux default", "linux", nil, func(home string) []string { return []string{filepath.Join(home, ".cache", "saki")} }},
		{"linux absolute", "linux", map[string]string{"XDG_CACHE_HOME": abs}, func(string) []string { return []string{filepath.Join(abs, "saki")} }},
		{"linux relative", "linux", map[string]string{"XDG_CACHE_HOME": "rel"}, func(string) []string { return nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			env := testEnv(t, test.goos, test.vars)
			got, want := env.LegacyCacheRoots(), test.want(env.Home)
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatalf("roots = %q, want %q", got, want)
			}
		})
	}
}

func TestResolveConfig(t *testing.T) {
	for _, test := range []struct {
		name       string
		legacy     bool
		current    bool
		wantLegacy bool
	}{
		{"neither", false, false, false},
		{"legacy only", true, false, true},
		{"new only", false, true, false},
		{"both", true, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			env := testEnv(t, "linux", nil)
			legacy := filepath.Join(env.Home, ".saki", "config.json")
			current := filepath.Join(env.Home, ".config", "saki", "config.json")
			if test.legacy {
				touch(t, legacy)
			}
			if test.current {
				touch(t, current)
			}
			item := ResolveConfig(env)
			want := current
			if test.wantLegacy {
				want = legacy
			}
			if item.Path != want || item.Legacy != test.wantLegacy || item.New != current {
				t.Fatalf("item = %+v, want path %q legacy %v", item, want, test.wantLegacy)
			}
			if (item.Notice() != "") != test.wantLegacy {
				t.Fatalf("notice = %q", item.Notice())
			}
		})
	}
}

func TestResolveConfigIgnoresRelativeXDGConfigHome(t *testing.T) {
	env := testEnv(t, "linux", map[string]string{"XDG_CONFIG_HOME": "relative"})
	touch(t, filepath.Join(env.Home, ".config", "saki", "config.json"))
	if item := ResolveConfig(env); item.Path != filepath.Join(env.Home, ".config", "saki", "config.json") || item.Legacy {
		t.Fatalf("item = %+v", item)
	}
}

func TestNoticeFormat(t *testing.T) {
	env := testEnv(t, "linux", nil)
	legacy := filepath.Join(env.Home, ".saki", "config.json")
	touch(t, legacy)
	want := `saki: using the config from the old location ` + legacy + `; run "saki migrate" to move it to ` + filepath.Join(env.Home, ".config", "saki", "config.json")
	if got := ResolveConfig(env).Notice(); got != want {
		t.Fatalf("notice = %q\nwant     %q", got, want)
	}
}

func TestResolveStorageResolvesEachItemIndependently(t *testing.T) {
	env := testEnv(t, "darwin", nil)
	legacyRoot := filepath.Join(env.Home, "Library", "Caches", "saki")
	touch(t, filepath.Join(legacyRoot, "audio", "ns", "a.audio"))
	touch(t, filepath.Join(legacyRoot, "offline", "ns", "a.stream"))
	touch(t, filepath.Join(legacyRoot, "catalog", "ns", "library.db"))
	touch(t, filepath.Join(env.Home, ".local", "share", "saki", "catalog", "ns", "library.db"))

	storage := ResolveStorage(env, "")
	checks := []struct {
		item   Item
		path   string
		legacy bool
	}{
		{storage.Audio, filepath.Join(legacyRoot, "audio"), true},
		{storage.Covers, filepath.Join(env.Home, ".cache", "saki", "covers"), false},
		{storage.Catalog, filepath.Join(env.Home, ".local", "share", "saki", "catalog"), false},
		{storage.Offline, filepath.Join(legacyRoot, "offline"), true},
	}
	for _, check := range checks {
		if check.item.Path != check.path || check.item.Legacy != check.legacy {
			t.Fatalf("%s = %+v, want %q legacy %v", check.item.What, check.item, check.path, check.legacy)
		}
	}
	if notices := storage.Notices(); len(notices) != 2 {
		t.Fatalf("notices = %q, want two", notices)
	}
}

func TestEmptyNewDirectoryDoesNotHideLegacy(t *testing.T) {
	env := testEnv(t, "windows", map[string]string{"LOCALAPPDATA": filepath.Join(t.TempDir(), "Local")})
	legacyAudio := filepath.Join(env.getenv("LOCALAPPDATA"), "saki", "audio")
	touch(t, filepath.Join(legacyAudio, "ns", "a.audio"))
	// A newer run created the directory tree but wrote no file.
	if err := os.MkdirAll(filepath.Join(env.CacheDir(), "audio", "ns"), 0o700); err != nil {
		t.Fatal(err)
	}
	if item := ResolveStorage(env, "").Audio; item.Path != legacyAudio || !item.Legacy {
		t.Fatalf("audio = %+v, want legacy %q", item, legacyAudio)
	}
}

func TestResolveStorageWithNothingUsesNewLocationsWithoutCreatingThem(t *testing.T) {
	env := testEnv(t, "darwin", nil)
	storage := ResolveStorage(env, "")
	for _, item := range storage.Items() {
		if item.Legacy || item.Path != item.New {
			t.Fatalf("%s = %+v", item.What, item)
		}
	}
	entries, err := os.ReadDir(env.Home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("resolving created %v (%v)", entries, err)
	}
}

func TestLinuxCacheRootDoublesAsNewCacheDirectory(t *testing.T) {
	env := testEnv(t, "linux", nil)
	root := filepath.Join(env.Home, ".cache", "saki")
	touch(t, filepath.Join(root, "audio", "ns", "a.audio"))
	touch(t, filepath.Join(root, "catalog", "ns", "library.db"))
	storage := ResolveStorage(env, "")
	if storage.Audio.Legacy || storage.Audio.Path != filepath.Join(root, "audio") {
		t.Fatalf("audio = %+v", storage.Audio)
	}
	if !storage.Catalog.Legacy || storage.Catalog.New != filepath.Join(env.Home, ".local", "share", "saki", "catalog") {
		t.Fatalf("catalog = %+v", storage.Catalog)
	}
}

func TestCacheDirOverride(t *testing.T) {
	env := testEnv(t, "darwin", nil)
	custom := filepath.Join(env.Home, "Music", "saki")
	touch(t, filepath.Join(env.Home, "Library", "Caches", "saki", "audio", "a"))
	storage := ResolveStorage(env, custom)
	for _, item := range storage.Items() {
		if !item.Override || item.Legacy || filepath.Dir(item.Path) != custom {
			t.Fatalf("%s = %+v", item.What, item)
		}
	}
	if len(storage.Notices()) != 0 {
		t.Fatalf("notices = %q", storage.Notices())
	}

	storage = ResolveStorage(env, filepath.Join(env.Home, "Library", "Caches", "saki"))
	if storage.CacheDirNotice == "" || len(storage.Notices()) != 1 {
		t.Fatalf("a cacheDir naming the old default gave no notice: %+v", storage)
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join(t.TempDir(), "offline")
	if rest, ok := Within("linux", root, filepath.Join(root, "ns", "a.stream")); !ok || rest != filepath.Join("ns", "a.stream") {
		t.Fatalf("inside = %q, %v", rest, ok)
	}
	if _, ok := Within("linux", root, root+"-other"); ok {
		t.Fatal("a sibling with the same prefix counted as inside")
	}
	if _, ok := Within("windows", strings.ToUpper(root), filepath.Join(root, "x")); !ok {
		t.Fatal("Windows comparison is case-sensitive")
	}
}
