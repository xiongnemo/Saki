// Package migrate implements `saki migrate`: it moves the files older releases
// kept in ~/.saki and os.UserCacheDir()/saki to the XDG layout described in
// package paths.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiongnemo/saki/internal/config"
	"github.com/xiongnemo/saki/internal/instancelock"
	"github.com/xiongnemo/saki/internal/offline"
	"github.com/xiongnemo/saki/internal/paths"
)

// smtcDLLName is the Windows SMTC shim older releases extracted to
// %LOCALAPPDATA%\Saki\smtc.
const smtcDLLName = "saki_smtc.dll"

// Options configures a migration.
type Options struct {
	Env    paths.Env
	DryRun bool
	Out    io.Writer
	// Rename defaults to os.Rename. Tests replace it to exercise the copy
	// that runs when a rename fails across volumes.
	Rename func(oldpath, newpath string) error
}

// ErrRunning means another Saki holds the instance lock.
var ErrRunning = errors.New("another Saki is running; quit it and run saki migrate again")

// ConflictError reports files that exist at both locations with different
// content. Both copies are kept.
type ConflictError struct{ Count int }

func (e *ConflictError) Error() string {
	noun := "conflicts"
	if e.Count == 1 {
		noun = "conflict"
	}
	return fmt.Sprintf("%d %s: both copies were kept; compare them, delete the one you do not want and run saki migrate again", e.Count, noun)
}

// FailureError reports files that could not be moved.
type FailureError struct{ Count int }

func (e *FailureError) Error() string {
	return fmt.Sprintf("%d item(s) could not be migrated; see the errors above", e.Count)
}

type migrator struct {
	env       paths.Env
	goos      string
	dryRun    bool
	out       io.Writer
	rename    func(string, string) error
	actions   int
	conflicts int
	failures  int
}

func (m *migrator) line(verb, format string, args ...any) {
	fmt.Fprintf(m.out, "%-9s %s\n", verb, fmt.Sprintf(format, args...))
}

func (m *migrator) act(verb, format string, args ...any) {
	m.actions++
	m.line(verb, format, args...)
}

func (m *migrator) skip(path, reason string) { m.line("skip", "%s: %s", path, reason) }

func (m *migrator) conflict(src, dst string) {
	m.conflicts++
	m.line("conflict", "%s and %s both exist with different content; kept both", src, dst)
}

func (m *migrator) fail(path string, err error) {
	m.failures++
	m.line("error", "%s: %v", path, err)
}

// Run migrates, or with DryRun prints what it would do. It returns a
// *ConflictError or *FailureError when something was left behind.
func Run(ctx context.Context, opts Options) error {
	m := &migrator{env: opts.Env, goos: opts.Env.OS(), dryRun: opts.DryRun, out: opts.Out, rename: opts.Rename}
	if m.out == nil {
		m.out = io.Discard
	}
	if m.rename == nil {
		m.rename = os.Rename
	}
	env := opts.Env

	if m.dryRun {
		fmt.Fprintln(m.out, "Dry run: nothing is changed.")
		if busy, err := instancelock.Probe(env.LockPath()); err == nil && busy {
			m.line("note", "another Saki is running; saki migrate will refuse to run until it quits")
		}
	} else {
		lock, err := instancelock.Exclusive(env.LockPath())
		if errors.Is(err, instancelock.ErrLocked) {
			return ErrRunning
		}
		if err != nil {
			return fmt.Errorf("take the instance lock: %w", err)
		}
		defer lock.Close()
	}

	legacyRoots := existingDirs(env.LegacyCacheRoots())
	leftovers := make(map[string]bool)

	// The old SMTC shim goes first: a running older Saki keeps it loaded, so
	// failing to delete it means that Saki would still be writing the files.
	for _, root := range legacyRoots {
		if paths.SamePath(m.goos, root, env.CacheDir()) {
			continue // the current shim, if XDG_CACHE_HOME names the old base
		}
		gone, err := m.deleteSMTC(root)
		if err != nil {
			return fmt.Errorf("delete the old SMTC shim in %s: %w; quit every running Saki and run saki migrate again", root, err)
		}
		if !gone {
			leftovers[root] = true
		}
	}

	// config.json
	legacyConfig := filepath.Join(env.LegacyConfigDir(), paths.ConfigFile)
	newConfig := filepath.Join(env.ConfigDir(), paths.ConfigFile)
	configPath := paths.ResolveConfig(env).Path // where Saki will read it afterwards
	if _, err := os.Lstat(legacyConfig); err == nil {
		if m.move(legacyConfig, newConfig) {
			configPath = newConfig
			m.tightenConfig(newConfig)
		} else {
			leftovers[env.LegacyConfigDir()] = true
		}
	}
	m.cleanup(env.LegacyConfigDir(), []string{paths.ConfigFile}, leftovers[env.LegacyConfigDir()])

	// settings.cacheDir decides where the caches, catalog and downloads are.
	// A dry run moved nothing, so read the config where it is now.
	readPath := paths.ResolveConfig(env).Path
	cfg, err := config.NewStoreAt(readPath).Load()
	if err != nil {
		return fmt.Errorf("read %s: %w", readPath, err)
	}
	cacheDir := strings.TrimSpace(cfg.Settings.CacheDir)
	sources := legacyRoots
	resetCacheDir := false
	if cacheDir != "" {
		sources = nil
		for _, root := range env.LegacyCacheRoots() {
			if paths.SamePath(m.goos, root, cacheDir) {
				sources = existingDirs([]string{cacheDir})
				resetCacheDir = true
				break
			}
		}
		if !resetCacheDir {
			m.skip(cacheDir, "settings.cacheDir is set, so Saki keeps its caches, catalog and downloads there")
			m.noteCacheDirInsideLegacy(cacheDir, legacyRoots)
			for _, root := range legacyRoots {
				if paths.SamePath(m.goos, root, env.CacheDir()) {
					continue
				}
				if hasEntriesBesides(root, paths.SMTCDirName) {
					m.skip(root, "not used while settings.cacheDir is set; left in place")
					continue
				}
				m.cleanup(root, []string{paths.SMTCDirName}, leftovers[root])
			}
		}
	}

	type relocation struct{ from, to string }
	var offlineMoves []relocation
	for _, root := range sources {
		targets := []relocation{
			{filepath.Join(root, paths.AudioDirName), filepath.Join(env.CacheDir(), paths.AudioDirName)},
			{filepath.Join(root, paths.CoversDirName), filepath.Join(env.CacheDir(), paths.CoversDirName)},
			{filepath.Join(root, paths.CatalogDirName), filepath.Join(env.DataDir(), paths.CatalogDirName)},
			{filepath.Join(root, paths.OfflineDirName), filepath.Join(env.DataDir(), paths.OfflineDirName)},
		}
		for _, target := range targets {
			if paths.SamePath(m.goos, target.from, target.to) {
				continue
			}
			if _, err := os.Lstat(target.from); err != nil {
				continue
			}
			if !m.move(target.from, target.to) {
				leftovers[root] = true
			}
			if filepath.Base(target.to) == paths.OfflineDirName {
				offlineMoves = append(offlineMoves, target)
			}
		}
		// On Linux the old cache root is also the new cache directory.
		if !paths.SamePath(m.goos, root, env.CacheDir()) {
			m.cleanup(root, []string{paths.AudioDirName, paths.CoversDirName, paths.CatalogDirName, paths.OfflineDirName, paths.SMTCDirName}, leftovers[root])
		}
	}

	// The catalog databases record absolute download paths.
	for _, move := range offlineMoves {
		m.rewriteDownloads(ctx, sources, move.from, move.to)
	}

	if resetCacheDir {
		m.act("rewrite", "%s: settings.cacheDir %s -> default", configPath, cacheDir)
		if !m.dryRun {
			cfg.Settings.CacheDir = ""
			if err := config.NewStoreAt(configPath).Save(cfg); err != nil {
				m.fail(configPath, err)
			}
		}
	}

	switch {
	case m.failures > 0:
		return &FailureError{Count: m.failures}
	case m.conflicts > 0:
		return &ConflictError{Count: m.conflicts}
	case m.actions == 0:
		fmt.Fprintln(m.out, "Nothing to migrate.")
	case m.dryRun:
		fmt.Fprintln(m.out, "Run saki migrate without --dry-run to do this.")
	default:
		fmt.Fprintln(m.out, "Done.")
	}
	return nil
}

// deleteSMTC deletes the SMTC shim an older release extracted below root. It
// is regenerated in the new cache directory on start and nothing registered
// with Windows points at it. It reports whether root/smtc is gone.
func (m *migrator) deleteSMTC(root string) (bool, error) {
	dir := filepath.Join(root, paths.SMTCDirName)
	dll := filepath.Join(dir, smtcDLLName)
	if _, err := os.Lstat(dll); err == nil {
		m.act("delete", "%s (extracted helper; Saki extracts it again to %s)", dll, m.env.SMTCDir())
		if !m.dryRun {
			if err := os.Remove(dll); err != nil {
				return false, err
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		m.fail(dir, err)
		return false, nil
	}
	gone := true
	for _, entry := range entries {
		if entry.Name() == smtcDLLName {
			continue
		}
		m.skip(filepath.Join(dir, entry.Name()), "not written by Saki; left in place")
		gone = false
	}
	if gone && !m.dryRun {
		if err := os.Remove(dir); err != nil {
			m.fail(dir, err)
			return false, nil
		}
	}
	return gone, nil
}

// cleanup lists what is left in an old directory Saki owns and removes the
// directory when nothing is. known names entries handled elsewhere.
func (m *migrator) cleanup(dir string, known []string, leftovers bool) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		m.fail(dir, err)
		return
	}
	empty := !leftovers
	for _, entry := range entries {
		if contains(known, entry.Name()) {
			// Moved or deleted above; still listed only in a dry run.
			continue
		}
		m.skip(filepath.Join(dir, entry.Name()), "not written by Saki; left in place")
		empty = false
	}
	if !empty {
		return
	}
	m.act("rmdir", "%s (empty)", dir)
	if m.dryRun {
		return
	}
	if err := os.Remove(dir); err != nil {
		m.fail(dir, err)
	}
}

func (m *migrator) rewriteDownloads(ctx context.Context, sources []string, from, to string) {
	catalog := filepath.Join(m.env.DataDir(), paths.CatalogDirName)
	if m.dryRun {
		// The databases have not moved yet; opening them could create WAL
		// files, so only say what will happen.
		var dbs []string
		for _, root := range append([]string{filepath.Dir(catalog)}, sources...) {
			found, _ := filepath.Glob(filepath.Join(root, paths.CatalogDirName, "*", "library.db"))
			dbs = append(dbs, found...)
		}
		if len(dbs) > 0 {
			m.act("rewrite", "download paths in the offline catalog databases: %s -> %s", from, to)
		}
		return
	}
	dbs, _ := filepath.Glob(filepath.Join(catalog, "*", "library.db"))
	for _, root := range sources {
		// Databases left behind by a conflict still name the moved files.
		found, _ := filepath.Glob(filepath.Join(root, paths.CatalogDirName, "*", "library.db"))
		dbs = append(dbs, found...)
	}
	for _, db := range dbs {
		count, err := offline.RewriteDownloadRoot(ctx, db, from, to, m.goos)
		if err != nil {
			m.fail(db, err)
			continue
		}
		if count > 0 {
			m.act("rewrite", "%s: %d download path(s) %s -> %s", db, count, from, to)
		}
	}
}

// tightenConfig keeps the moved config.json private: it holds the password.
func (m *migrator) tightenConfig(path string) {
	if m.dryRun || m.goos == "windows" {
		return
	}
	for _, target := range []struct {
		path string
		mode fs.FileMode
	}{{filepath.Dir(path), 0o700}, {path, 0o600}} {
		info, err := os.Lstat(target.path)
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || info.Mode().Perm()&^target.mode == 0 {
			continue
		}
		if err := os.Chmod(target.path, target.mode); err != nil {
			m.fail(target.path, err)
			continue
		}
		m.act("chmod", "%s %04o", target.path, target.mode)
	}
}

func (m *migrator) noteCacheDirInsideLegacy(cacheDir string, legacyRoots []string) {
	for _, owned := range append([]string{m.env.LegacyConfigDir()}, legacyRoots...) {
		if paths.SamePath(m.goos, owned, m.env.CacheDir()) {
			continue
		}
		if _, inside := paths.Within(m.goos, owned, cacheDir); inside {
			m.line("note", "settings.cacheDir %s is inside the old directory %s; move it elsewhere yourself and set Cache dir in System > Settings to the new place", cacheDir, owned)
			return
		}
	}
}

// existingDirs keeps the directories that exist, dropping duplicates such as
// %LOCALAPPDATA%\saki and %LOCALAPPDATA%\Saki on a case-insensitive volume.
func existingDirs(dirs []string) []string {
	var kept []string
	var infos []fs.FileInfo
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		duplicate := false
		for _, seen := range infos {
			if os.SameFile(seen, info) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			kept = append(kept, dir)
			infos = append(infos, info)
		}
	}
	return kept
}

func hasEntriesBesides(dir, name string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() != name {
			return true
		}
	}
	return false
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
