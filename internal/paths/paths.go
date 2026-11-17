// Package paths says where Saki keeps its per-user files.
//
// Saki uses the XDG Base Directory layout under the home directory on every
// OS, Windows and macOS included:
//
//	config  $XDG_CONFIG_HOME/saki  (~/.config/saki)       config.json
//	data    $XDG_DATA_HOME/saki    (~/.local/share/saki)  offline catalog and downloads
//	state   $XDG_STATE_HOME/saki   (~/.local/state/saki)  instance lock
//	cache   $XDG_CACHE_HOME/saki   (~/.cache/saki)        audio and cover caches, SMTC shim
//
// An XDG variable counts only when it holds an absolute path; the XDG spec
// calls a relative value invalid. Releases before this layout kept the config
// in ~/.saki and everything else in os.UserCacheDir()/saki. Each item resolves
// on its own: the new location when the item exists there, otherwise the old
// location when it exists there, otherwise the new location.
package paths

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// App is the directory name under each XDG base directory.
const App = "saki"

// ConfigFile is the name of Saki's config file.
const ConfigFile = "config.json"

// SMTCDirName is the cache subdirectory the Windows SMTC shim is extracted to.
const SMTCDirName = "smtc"

// LockFile is the name of the instance lock in the state directory.
const LockFile = "saki.lock"

// Env is what path resolution depends on: the OS, the home directory and the
// environment. Tests build one by hand instead of touching the real home.
type Env struct {
	GOOS   string
	Home   string
	Getenv func(string) string
}

// System returns the Env of the running process.
func System() (Env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Env{}, fmt.Errorf("locate home directory: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return Env{}, errors.New("locate home directory: empty path")
	}
	return Env{GOOS: runtime.GOOS, Home: home, Getenv: os.Getenv}, nil
}

func (e Env) getenv(key string) string {
	if e.Getenv == nil {
		return ""
	}
	return e.Getenv(key)
}

// OS is GOOS, or the running OS when GOOS is empty.
func (e Env) OS() string { return e.goos() }

func (e Env) goos() string {
	if e.GOOS == "" {
		return runtime.GOOS
	}
	return e.GOOS
}

// absEnv returns $key when it is set to an absolute path.
func (e Env) absEnv(key string) (string, bool) {
	value := e.getenv(key)
	if value == "" || !filepath.IsAbs(value) {
		return "", false
	}
	return filepath.Clean(value), true
}

func (e Env) base(key string, fallback ...string) string {
	if dir, ok := e.absEnv(key); ok {
		return dir
	}
	return filepath.Join(append([]string{e.Home}, fallback...)...)
}

// ConfigDir is $XDG_CONFIG_HOME/saki, by default ~/.config/saki.
func (e Env) ConfigDir() string { return filepath.Join(e.base("XDG_CONFIG_HOME", ".config"), App) }

// DataDir is $XDG_DATA_HOME/saki, by default ~/.local/share/saki.
func (e Env) DataDir() string { return filepath.Join(e.base("XDG_DATA_HOME", ".local", "share"), App) }

// StateDir is $XDG_STATE_HOME/saki, by default ~/.local/state/saki.
func (e Env) StateDir() string {
	return filepath.Join(e.base("XDG_STATE_HOME", ".local", "state"), App)
}

// CacheDir is $XDG_CACHE_HOME/saki, by default ~/.cache/saki.
func (e Env) CacheDir() string { return filepath.Join(e.base("XDG_CACHE_HOME", ".cache"), App) }

// RuntimeDir is $XDG_RUNTIME_DIR/saki when XDG_RUNTIME_DIR is absolute.
func (e Env) RuntimeDir() (string, bool) {
	dir, ok := e.absEnv("XDG_RUNTIME_DIR")
	if !ok {
		return "", false
	}
	return filepath.Join(dir, App), true
}

// LockPath is the instance lock file in the state directory.
func (e Env) LockPath() string { return filepath.Join(e.StateDir(), LockFile) }

// SMTCDir is where the Windows SMTC shim is extracted.
func (e Env) SMTCDir() string { return filepath.Join(e.CacheDir(), SMTCDirName) }

// LegacyConfigDir is ~/.saki, where every release before the XDG layout kept
// config.json.
func (e Env) LegacyConfigDir() string { return filepath.Join(e.Home, ".saki") }

// LegacyCacheRoots are the directories released builds used as the cache root,
// os.UserCacheDir()/saki: %LOCALAPPDATA%\saki on Windows (the SMTC shim went to
// %LOCALAPPDATA%\Saki, the same directory unless the volume is case-sensitive),
// ~/Library/Caches/saki on macOS and $XDG_CACHE_HOME/saki or ~/.cache/saki
// elsewhere. On Linux the last one is also the new cache directory.
func (e Env) LegacyCacheRoots() []string {
	switch e.goos() {
	case "windows":
		localAppData := e.getenv("LOCALAPPDATA")
		if localAppData == "" {
			return nil
		}
		return []string{filepath.Join(localAppData, App), filepath.Join(localAppData, "Saki")}
	case "darwin", "ios":
		return []string{filepath.Join(e.Home, "Library", "Caches", App)}
	case "plan9":
		return []string{filepath.Join(e.Home, "lib", "cache", App)}
	default:
		// Go's os.UserCacheDir rejects a relative $XDG_CACHE_HOME, and released
		// builds then fell back to the temporary directory, which is not a home
		// location and is left alone.
		if value := e.getenv("XDG_CACHE_HOME"); value != "" {
			if !filepath.IsAbs(value) {
				return nil
			}
			return []string{filepath.Join(filepath.Clean(value), App)}
		}
		return []string{filepath.Join(e.Home, ".cache", App)}
	}
}

// Item is one resolved file or directory.
type Item struct {
	// What names the item in messages, such as "config" or "audio cache".
	What string
	// Path is where Saki uses the item this run.
	Path string
	// New is the item's location in the XDG layout.
	New string
	// Legacy reports that Path is an old location.
	Legacy bool
	// Override reports that Path comes from an explicit setting.
	Override bool
}

// Notice is the stderr line for an item found at an old location, or "".
func (item Item) Notice() string {
	if !item.Legacy {
		return ""
	}
	return fmt.Sprintf("%s: using the %s from the old location %s; run \"%s migrate\" to move it to %s", App, item.What, item.Path, App, item.New)
}

// ResolveConfig resolves config.json.
func ResolveConfig(e Env) Item { return ResolveConfigFile(e, ConfigFile) }

// ResolveConfigFile resolves a file in the config directory.
func ResolveConfigFile(e Env, name string) Item {
	return resolve(e.goos(), "config", filepath.Join(e.ConfigDir(), name),
		[]string{filepath.Join(e.LegacyConfigDir(), name)}, isFile)
}

// Storage holds the directories that settings.cacheDir can override. Each one
// contains a subdirectory per account namespace.
type Storage struct {
	Audio   Item
	Covers  Item
	Catalog Item
	Offline Item
	// CacheDirNotice is set when settings.cacheDir names an old default root.
	CacheDirNotice string
}

// Items lists the storage items in a fixed order.
func (s Storage) Items() []Item { return []Item{s.Audio, s.Covers, s.Catalog, s.Offline} }

// Notices are the stderr lines for items found at old locations.
func (s Storage) Notices() []string {
	var notices []string
	if s.CacheDirNotice != "" {
		notices = append(notices, s.CacheDirNotice)
	}
	for _, item := range s.Items() {
		if notice := item.Notice(); notice != "" {
			notices = append(notices, notice)
		}
	}
	return notices
}

// Storage subdirectory names, shared by the new layout, the old cache root and
// a settings.cacheDir override.
const (
	AudioDirName   = "audio"
	CoversDirName  = "covers"
	CatalogDirName = "catalog"
	OfflineDirName = "offline"
)

// ResolveStorage resolves the cache, catalog and download directories. A
// non-empty cacheDir is the user's override and keeps all four under it, as
// every release did.
func ResolveStorage(e Env, cacheDir string) Storage {
	if strings.TrimSpace(cacheDir) != "" {
		storage := Storage{
			Audio:   override("audio cache", filepath.Join(cacheDir, AudioDirName)),
			Covers:  override("cover art cache", filepath.Join(cacheDir, CoversDirName)),
			Catalog: override("offline catalog", filepath.Join(cacheDir, CatalogDirName)),
			Offline: override("offline downloads", filepath.Join(cacheDir, OfflineDirName)),
		}
		for _, root := range e.LegacyCacheRoots() {
			if SamePath(e.goos(), root, cacheDir) {
				storage.CacheDirNotice = fmt.Sprintf("%s: settings.cacheDir is set to the old default %s; run \"%s migrate\" to move its contents to %s and %s", App, cacheDir, App, e.CacheDir(), e.DataDir())
				break
			}
		}
		return storage
	}
	legacy := func(name string) []string {
		var paths []string
		for _, root := range e.LegacyCacheRoots() {
			paths = append(paths, filepath.Join(root, name))
		}
		return paths
	}
	goos := e.goos()
	return Storage{
		Audio:   resolve(goos, "audio cache", filepath.Join(e.CacheDir(), AudioDirName), legacy(AudioDirName), hasFile),
		Covers:  resolve(goos, "cover art cache", filepath.Join(e.CacheDir(), CoversDirName), legacy(CoversDirName), hasFile),
		Catalog: resolve(goos, "offline catalog", filepath.Join(e.DataDir(), CatalogDirName), legacy(CatalogDirName), hasFile),
		Offline: resolve(goos, "offline downloads", filepath.Join(e.DataDir(), OfflineDirName), legacy(OfflineDirName), hasFile),
	}
}

func override(what, path string) Item {
	return Item{What: what, Path: path, New: path, Override: true}
}

func resolve(goos, what, newPath string, legacy []string, exists func(string) bool) Item {
	item := Item{What: what, Path: newPath, New: newPath}
	if exists(newPath) {
		return item
	}
	for _, old := range legacy {
		if SamePath(goos, old, newPath) {
			continue
		}
		if exists(old) {
			item.Path = old
			item.Legacy = true
			return item
		}
	}
	return item
}

// SamePath compares two cleaned paths, ignoring case on Windows and macOS,
// whose default filesystems are case-insensitive.
func SamePath(goos, left, right string) bool {
	return equalPath(goos, filepath.Clean(left), filepath.Clean(right))
}

func equalPath(goos, left, right string) bool {
	if goos == "windows" || goos == "darwin" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

// Within reports whether path is root or lies below it, and returns the
// remainder relative to root.
func Within(goos, root, path string) (string, bool) {
	root, path = filepath.Clean(root), filepath.Clean(path)
	if equalPath(goos, root, path) {
		return "", true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if len(path) <= len(prefix) || !equalPath(goos, path[:len(prefix)], prefix) {
		return "", false
	}
	return path[len(prefix):], true
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

var errFound = errors.New("found")

// hasFile reports whether dir holds at least one regular file. Empty
// directories a newer run created do not count as the item existing.
func hasFile(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	err = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.Type().IsRegular() || entry.Type()&fs.ModeSymlink != 0 {
			return errFound
		}
		return nil
	})
	return errors.Is(err, errFound)
}
