package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xiongnemo/saki/internal/config"
	"github.com/xiongnemo/saki/internal/instancelock"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/offline"
	"github.com/xiongnemo/saki/internal/paths"
)

// fixture is a throwaway home. It simulates Windows, whose old cache root
// %LOCALAPPDATA%\saki differs from the new one on every host; the Linux case,
// where they coincide, has its own test.
type fixture struct {
	env        paths.Env
	home       string
	legacyRoot string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	home := t.TempDir()
	local := filepath.Join(home, "AppData", "Local")
	env := paths.Env{GOOS: "windows", Home: home, Getenv: func(key string) string {
		if key == "LOCALAPPDATA" {
			return local
		}
		return ""
	}}
	return fixture{env: env, home: home, legacyRoot: filepath.Join(local, "saki")}
}

func (f fixture) run(t *testing.T, dryRun bool) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), Options{Env: f.env, DryRun: dryRun, Out: &out})
	return out.String(), err
}

func (f fixture) legacyConfig() string { return filepath.Join(f.home, ".saki", "config.json") }
func (f fixture) newConfig() string    { return filepath.Join(f.env.ConfigDir(), "config.json") }
func (f fixture) newCache(parts ...string) string {
	return filepath.Join(append([]string{f.env.CacheDir()}, parts...)...)
}
func (f fixture) newData(parts ...string) string {
	return filepath.Join(append([]string{f.env.DataDir()}, parts...)...)
}
func (f fixture) old(parts ...string) string {
	return filepath.Join(append([]string{f.legacyRoot}, parts...)...)
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// populate writes what a v1.0.0 install on Windows leaves behind, including a
// catalog with one completed download.
func (f fixture) populate(t *testing.T) (namespace string) {
	t.Helper()
	write(t, f.legacyConfig(), `{"account":{"username":"nemo"},"settings":{}}`)
	write(t, f.old("covers", "ns", "c.jpg"), "cover")
	write(t, f.old("audio", "ns", "a.audio"), "audio")
	write(t, f.old("smtc", "saki_smtc.dll"), "dll")
	store, err := offline.Open(f.legacyRoot, "nemo", "https://music.example", "fp")
	if err != nil {
		t.Fatal(err)
	}
	download := filepath.Join(store.DownloadRoot(), "song.stream")
	write(t, download, "song")
	if err := store.UpsertDownload(context.Background(), offline.Download{SongID: "s1", Variant: "stream", Status: "complete", Path: download, Bytes: 4}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return filepath.Base(store.DownloadRoot())
}

type entry struct {
	mode    fs.FileMode
	modTime time.Time
	content string
}

func snapshot(t *testing.T, root string) map[string]entry {
	t.Helper()
	tree := make(map[string]entry)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// Directory times are left out: NTFS updates them lazily after the
		// fixture's own writes. Added or removed entries still show up.
		e := entry{mode: info.Mode()}
		if info.Mode().IsRegular() {
			e.modTime = info.ModTime()
			e.content = read(t, path)
		}
		tree[path] = e
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestDryRunTouchesNothing(t *testing.T) {
	f := newFixture(t)
	f.populate(t)
	write(t, f.old("notes.txt"), "mine")
	before := snapshot(t, f.home)

	out, err := f.run(t, true)
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	after := snapshot(t, f.home)
	if len(before) != len(after) {
		t.Fatalf("dry run changed the tree: %d entries before, %d after\n%s", len(before), len(after), out)
	}
	for path, want := range before {
		if got, ok := after[path]; !ok || got != want {
			t.Fatalf("dry run changed %s: %+v -> %+v\n%s", path, want, got, out)
		}
	}
	for _, want := range []string{
		"Dry run",
		"move      " + f.legacyConfig() + " -> " + f.newConfig(),
		"move      " + f.old("audio") + " -> " + f.newCache("audio"),
		"move      " + f.old("covers") + " -> " + f.newCache("covers"),
		"move      " + f.old("catalog") + " -> " + f.newData("catalog"),
		"move      " + f.old("offline") + " -> " + f.newData("offline"),
		"delete    " + f.old("smtc", "saki_smtc.dll"),
		"rewrite   download paths in the offline catalog databases: " + f.old("offline") + " -> " + f.newData("offline"),
		"skip      " + f.old("notes.txt") + ": not written by Saki",
		"rmdir     " + filepath.Join(f.home, ".saki"),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("plan lacks %q:\n%s", want, out)
		}
	}
}

func TestMovesEverythingAndRewritesDownloadPaths(t *testing.T) {
	f := newFixture(t)
	namespace := f.populate(t)

	out, err := f.run(t, false)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	for path, want := range map[string]string{
		f.newConfig():                                  `{"account":{"username":"nemo"},"settings":{}}`,
		f.newCache("audio", "ns", "a.audio"):           "audio",
		f.newCache("covers", "ns", "c.jpg"):            "cover",
		f.newData("offline", namespace, "song.stream"): "song",
	} {
		if got := read(t, path); got != want {
			t.Fatalf("%s = %q, want %q", path, got, want)
		}
	}
	for _, gone := range []string{filepath.Join(f.home, ".saki"), f.legacyRoot, f.newCache("smtc", "saki_smtc.dll")} {
		if exists(gone) {
			t.Fatalf("%s still exists\n%s", gone, out)
		}
	}

	db, err := sql.Open("sqlite", f.newData("catalog", namespace, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var path string
	if err := db.QueryRow("SELECT path FROM downloads WHERE song_id='s1'").Scan(&path); err != nil {
		t.Fatal(err)
	}
	if want := f.newData("offline", namespace, "song.stream"); path != want {
		t.Fatalf("download path = %q, want %q", path, want)
	}
	if !strings.Contains(out, "1 download path(s)") {
		t.Fatalf("rewrite not reported:\n%s", out)
	}
}

func TestSecondRunHasNothingToDo(t *testing.T) {
	f := newFixture(t)
	f.populate(t)
	if out, err := f.run(t, false); err != nil {
		t.Fatalf("first run: %v\n%s", err, out)
	}
	for _, dryRun := range []bool{false, true} {
		out, err := f.run(t, dryRun)
		if err != nil || !strings.Contains(out, "Nothing to migrate.") {
			t.Fatalf("dry=%v second run: %v\n%s", dryRun, err, out)
		}
	}
}

func TestFreshInstallHasNothingToDo(t *testing.T) {
	f := newFixture(t)
	out, err := f.run(t, true)
	if err != nil || !strings.Contains(out, "Nothing to migrate.") {
		t.Fatalf("fresh dry run: %v\n%s", err, out)
	}
	if entries, _ := os.ReadDir(f.home); len(entries) != 0 {
		t.Fatalf("dry run on a fresh home created %v", entries)
	}
}

func TestMergesIntoExistingDirectory(t *testing.T) {
	f := newFixture(t)
	write(t, f.old("audio", "ns", "old.audio"), "old")
	write(t, f.newCache("audio", "ns", "new.audio"), "new")

	out, err := f.run(t, false)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	if read(t, f.newCache("audio", "ns", "old.audio")) != "old" || read(t, f.newCache("audio", "ns", "new.audio")) != "new" {
		t.Fatalf("merge lost a file\n%s", out)
	}
	if exists(f.legacyRoot) {
		t.Fatalf("emptied old root remains\n%s", out)
	}
}

func TestConflictKeepsBothCopiesAndCarriesOn(t *testing.T) {
	f := newFixture(t)
	write(t, f.legacyConfig(), `{"account":{"username":"old"}}`)
	write(t, f.newConfig(), `{"account":{"username":"new"}}`)
	write(t, f.old("covers", "ns", "c.jpg"), "cover")

	out, err := f.run(t, false)
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Count != 1 {
		t.Fatalf("err = %v, want one conflict\n%s", err, out)
	}
	if read(t, f.legacyConfig()) != `{"account":{"username":"old"}}` || read(t, f.newConfig()) != `{"account":{"username":"new"}}` {
		t.Fatal("a conflicting file was overwritten")
	}
	if read(t, f.newCache("covers", "ns", "c.jpg")) != "cover" {
		t.Fatalf("migrate stopped at the conflict\n%s", out)
	}
	if !strings.Contains(out, "conflict  "+f.legacyConfig()) {
		t.Fatalf("conflict not reported:\n%s", out)
	}
}

func TestIdenticalLegacyCopyIsRemoved(t *testing.T) {
	f := newFixture(t)
	write(t, f.legacyConfig(), `{"account":{}}`)
	write(t, f.newConfig(), `{"account":{}}`)
	write(t, f.old("audio", "ns", "a.audio"), "audio")
	write(t, f.newCache("audio", "ns", "a.audio"), "audio")

	out, err := f.run(t, false)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	if exists(f.legacyConfig()) || exists(f.legacyRoot) {
		t.Fatalf("identical legacy copies remain\n%s", out)
	}
	if read(t, f.newConfig()) != `{"account":{}}` {
		t.Fatal("new copy changed")
	}
}

func TestResetsCacheDirNamingTheOldDefault(t *testing.T) {
	f := newFixture(t)
	cfg := models.Config{Account: models.Account{Username: "nemo"}, Settings: models.Settings{CacheDir: f.legacyRoot}}
	if err := config.NewStoreAt(f.legacyConfig()).Save(cfg); err != nil {
		t.Fatal(err)
	}
	write(t, f.old("audio", "ns", "a.audio"), "audio")
	write(t, f.old("catalog", "ns", "library.db"), "db")

	out, err := f.run(t, false)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	loaded, err := config.NewStoreAt(f.newConfig()).Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Settings.CacheDir != "" || loaded.Account.Username != "nemo" {
		t.Fatalf("config after migrate = %+v", loaded)
	}
	if read(t, f.newCache("audio", "ns", "a.audio")) != "audio" || read(t, f.newData("catalog", "ns", "library.db")) != "db" {
		t.Fatalf("cache contents not moved\n%s", out)
	}
}

func TestCustomCacheDirIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	custom := filepath.Join(f.home, "Music", "saki")
	if err := config.NewStoreAt(f.newConfig()).Save(models.Config{Settings: models.Settings{CacheDir: custom}}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(custom, "audio", "ns", "a.audio"), "audio")
	write(t, f.old("audio", "ns", "orphan.audio"), "orphan")

	out, err := f.run(t, false)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	if read(t, filepath.Join(custom, "audio", "ns", "a.audio")) != "audio" || read(t, f.old("audio", "ns", "orphan.audio")) != "orphan" {
		t.Fatalf("files moved despite settings.cacheDir\n%s", out)
	}
	if !strings.Contains(out, "skip      "+custom) || !strings.Contains(out, "skip      "+f.legacyRoot) {
		t.Fatalf("skips not reported:\n%s", out)
	}
}

func TestCopiesAndVerifiesWhenRenameFails(t *testing.T) {
	f := newFixture(t)
	write(t, f.old("covers", "ns", "c.jpg"), "cover")
	write(t, f.old("covers", "ns", "deeper", "d.jpg"), "deep")
	stamp := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(f.old("covers", "ns", "c.jpg"), stamp, stamp); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	crossDevice := func(oldpath, newpath string) error {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errors.New("invalid cross-device link")}
	}
	if err := Run(context.Background(), Options{Env: f.env, Out: &out, Rename: crossDevice}); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	copied := f.newCache("covers", "ns", "c.jpg")
	if read(t, copied) != "cover" || read(t, f.newCache("covers", "ns", "deeper", "d.jpg")) != "deep" {
		t.Fatalf("copy incomplete\n%s", out.String())
	}
	info, err := os.Stat(copied)
	if err != nil || !info.ModTime().Equal(stamp) {
		t.Fatalf("mtime = %v, want %v (%v)", info.ModTime(), stamp, err)
	}
	if exists(f.legacyRoot) {
		t.Fatalf("sources kept after a verified copy\n%s", out.String())
	}
	if !strings.Contains(out.String(), "copied and verified") {
		t.Fatalf("copy not reported:\n%s", out.String())
	}
}

func TestRefusesWhileAnotherSakiRuns(t *testing.T) {
	if !instancelock.Supported {
		t.Skip("no file locking on this platform")
	}
	f := newFixture(t)
	write(t, f.legacyConfig(), "{}")
	lock, err := instancelock.Shared(f.env.LockPath())
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	if _, err := f.run(t, false); !errors.Is(err, ErrRunning) {
		t.Fatalf("err = %v, want ErrRunning", err)
	}
	if !exists(f.legacyConfig()) || exists(f.newConfig()) {
		t.Fatal("migrate moved files while Saki was running")
	}
	out, err := f.run(t, true)
	if err != nil || !strings.Contains(out, "another Saki is running") {
		t.Fatalf("dry run while running: %v\n%s", err, out)
	}
}

func TestLinuxMovesOnlyTheDataOutOfTheCache(t *testing.T) {
	home := t.TempDir()
	env := paths.Env{GOOS: "linux", Home: home, Getenv: func(string) string { return "" }}
	cache := filepath.Join(home, ".cache", "saki")
	write(t, filepath.Join(cache, "audio", "ns", "a.audio"), "audio")
	store, err := offline.Open(cache, "nemo", "https://music.example", "")
	if err != nil {
		t.Fatal(err)
	}
	namespace := filepath.Base(store.DownloadRoot())
	write(t, filepath.Join(store.DownloadRoot(), "x.stream"), "song")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := Run(context.Background(), Options{Env: env, Out: &out}); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	data := filepath.Join(home, ".local", "share", "saki")
	if read(t, filepath.Join(cache, "audio", "ns", "a.audio")) != "audio" ||
		!exists(filepath.Join(data, "catalog", namespace, "library.db")) ||
		read(t, filepath.Join(data, "offline", namespace, "x.stream")) != "song" {
		t.Fatalf("unexpected layout\n%s", out.String())
	}
	if strings.Contains(out.String(), "rmdir") || strings.Contains(out.String(), "error") {
		t.Fatalf("touched the new cache directory:\n%s", out.String())
	}
}

func TestDatabaseAndSidecarsMoveTogether(t *testing.T) {
	f := newFixture(t)
	write(t, f.old("catalog", "ns", "library.db"), "old db")
	write(t, f.old("catalog", "ns", "library.db-wal"), "old wal")
	write(t, f.newData("catalog", "ns", "library.db"), "new db")

	out, err := f.run(t, false)
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want a conflict\n%s", err, out)
	}
	if exists(f.newData("catalog", "ns", "library.db-wal")) {
		t.Fatal("an old write-ahead log was paired with a different database")
	}
	if read(t, f.old("catalog", "ns", "library.db-wal")) != "old wal" {
		t.Fatal("old write-ahead log lost")
	}
}

func TestSkipsUnknownFilesAndKeepsTheirDirectory(t *testing.T) {
	f := newFixture(t)
	write(t, f.legacyConfig(), "{}")
	write(t, filepath.Join(f.home, ".saki", "backup.json"), "mine")

	out, err := f.run(t, false)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	if read(t, filepath.Join(f.home, ".saki", "backup.json")) != "mine" || !exists(f.newConfig()) {
		t.Fatalf("unexpected result\n%s", out)
	}
	if !strings.Contains(out, "skip      "+filepath.Join(f.home, ".saki", "backup.json")) {
		t.Fatalf("skip not reported:\n%s", out)
	}
}

func TestRelativeSymlinkStillResolvesAfterMove(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	f := newFixture(t)
	const linked = `{"account":{"username":"linked"}}`
	write(t, filepath.Join(f.home, "dotfiles", "saki.json"), linked)
	if err := os.MkdirAll(filepath.Dir(f.legacyConfig()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "dotfiles", "saki.json"), f.legacyConfig()); err != nil {
		t.Fatal(err)
	}
	out, err := f.run(t, false)
	if err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	if read(t, f.newConfig()) != linked {
		t.Fatalf("symlink broken after move\n%s", out)
	}
	if info, err := os.Lstat(f.newConfig()); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("moved config is no longer a symlink: %v", err)
	}
}

func TestTightensConfigPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions")
	}
	home := t.TempDir()
	env := paths.Env{GOOS: runtime.GOOS, Home: home, Getenv: func(string) string { return "" }}
	legacy := filepath.Join(home, ".saki", "config.json")
	write(t, legacy, "{}")
	if err := os.Chmod(legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), Options{Env: env, Out: &out}); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out.String())
	}
	info, err := os.Stat(filepath.Join(env.ConfigDir(), "config.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v (%v), want 0600", info.Mode().Perm(), err)
	}
}
