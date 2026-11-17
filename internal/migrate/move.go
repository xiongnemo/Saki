package migrate

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// move moves src to dst without overwriting anything and reports whether src
// is gone afterwards (in a dry run: whether it would be). A missing
// destination is created by renaming, or by copying when the rename fails,
// typically across volumes. Directories merge entry by entry. A file that
// exists on both sides is removed from src when the content is identical and
// reported as a conflict, with both copies kept, when it is not.
func (m *migrator) move(src, dst string) bool {
	srcInfo, err := os.Lstat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		m.fail(src, err)
		return false
	}
	dstInfo, err := os.Lstat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return m.moveNew(src, dst, srcInfo)
	}
	if err != nil {
		m.fail(dst, err)
		return false
	}
	switch {
	case srcInfo.IsDir() && dstInfo.IsDir():
		return m.mergeDir(src, dst)
	case srcInfo.Mode().IsRegular() && dstInfo.Mode().IsRegular():
		same, err := sameContent(src, dst)
		if err != nil {
			m.fail(src, err)
			return false
		}
		if !same {
			m.conflict(src, dst)
			return false
		}
		return m.removeDuplicate(src, dst)
	case srcInfo.Mode()&fs.ModeSymlink != 0 && dstInfo.Mode()&fs.ModeSymlink != 0:
		if symlinkTarget(src) != symlinkTarget(dst) {
			m.conflict(src, dst)
			return false
		}
		return m.removeDuplicate(src, dst)
	default:
		m.conflict(src, dst)
		return false
	}
}

func (m *migrator) removeDuplicate(src, dst string) bool {
	m.act("remove", "%s (the same content is already at %s)", src, dst)
	if m.dryRun {
		return true
	}
	if err := os.Remove(src); err != nil {
		m.fail(src, err)
		return false
	}
	return true
}

func (m *migrator) moveNew(src, dst string, info fs.FileInfo) bool {
	if info.IsDir() && hasOrphanJournal(src) {
		// Renaming the whole directory would carry the orphan along; merge
		// entry by entry so that it stays behind.
		if !m.dryRun {
			if err := os.MkdirAll(dst, info.Mode().Perm()|0o700); err != nil {
				m.fail(dst, err)
				return false
			}
		}
		return m.mergeDir(src, dst)
	}
	if m.dryRun {
		m.act("move", "%s -> %s", src, dst)
		return true
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		m.fail(dst, err)
		return false
	}
	if info.IsDir() {
		if err := m.rename(src, dst); err == nil {
			m.act("move", "%s -> %s", src, dst)
			return true
		}
		// The rename failed, usually because dst is on another volume: copy.
		if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
			m.fail(dst, err)
			return false
		}
		return m.mergeDir(src, dst)
	}
	how, err := m.transfer(src, dst, info)
	if err != nil {
		m.fail(src, err)
		return false
	}
	m.act("move", "%s -> %s%s", src, dst, how)
	return true
}

// transfer moves one file or symlink to a dst that does not exist: a rename,
// or when that fails a verified copy followed by removing src. A symlink is
// recreated with an absolute target, so that it still points at the same
// file from its new directory. It returns a note on how the file moved.
func (m *migrator) transfer(src, dst string, info fs.FileInfo) (string, error) {
	if info.Mode()&fs.ModeSymlink != 0 {
		target := symlinkTarget(src)
		if target == "" {
			return "", errors.New("cannot read symlink")
		}
		if err := os.Symlink(target, dst); err != nil {
			return "", err
		}
		if err := os.Remove(src); err != nil {
			return "", err
		}
		return " (symlink to " + target + ")", nil
	}
	if err := m.rename(src, dst); err == nil {
		return "", nil
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	if err := m.copy(src, dst, info); err != nil {
		return "", err
	}
	if err := os.Remove(src); err != nil {
		return "", err
	}
	return " (copied and verified)", nil
}

func symlinkTarget(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return ""
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}

// sqliteSidecars are files SQLite keeps next to a database. A database and
// its sidecars move together or not at all: pairing a write-ahead log with a
// different database corrupts it.
var sqliteSidecars = []string{"-wal", "-shm", "-journal"}

func groupKey(name string) string {
	for _, suffix := range sqliteSidecars {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

func (m *migrator) mergeDir(src, dst string) bool {
	entries, err := os.ReadDir(src)
	if err != nil {
		m.fail(src, err)
		return false
	}
	var keys []string
	groups := make(map[string][]string)
	for _, entry := range entries {
		key := groupKey(entry.Name())
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], entry.Name())
	}
	gone := true
	for _, key := range keys {
		names := groups[key]
		if len(names) == 1 && names[0] == key {
			if !m.move(filepath.Join(src, key), filepath.Join(dst, key)) {
				gone = false
			}
			continue
		}
		if !m.moveGroup(src, dst, key, names) {
			gone = false
		}
	}
	if gone && !m.dryRun {
		if err := os.Remove(src); err != nil {
			m.fail(src, err)
			return false
		}
	}
	return gone
}

// moveGroup moves a database and its sidecars, all of them or none. When any
// of them exists at dst, the group is only removed from src if dst holds
// exactly the same files. Sidecars whose database is not in src stay put: a
// journal moved next to some other library.db would be replayed onto it.
func (m *migrator) moveGroup(src, dst, key string, names []string) bool {
	if !contains(names, key) {
		for _, name := range names {
			m.skip(filepath.Join(src, name), "SQLite file without its database "+key+" beside it; left in place so that it is never applied to another database")
		}
		return false
	}
	var atDst []string
	for _, name := range append([]string{key}, suffixed(key)...) {
		if _, err := os.Lstat(filepath.Join(dst, name)); err == nil {
			atDst = append(atDst, name)
		}
	}
	if len(atDst) == 0 {
		return m.moveGroupTogether(src, dst, key, names)
	}
	same := len(atDst) == len(names)
	for _, name := range names {
		if !same {
			break
		}
		equal, err := sameContent(filepath.Join(src, name), filepath.Join(dst, name))
		same = err == nil && equal
	}
	if !same {
		m.conflict(filepath.Join(src, key), filepath.Join(dst, key))
		return false
	}
	gone := true
	for _, name := range names {
		if !m.removeDuplicate(filepath.Join(src, name), filepath.Join(dst, name)) {
			gone = false
		}
	}
	return gone
}

// moveGroupTogether moves a database and its sidecars to a dst that holds none
// of them. If one fails, the ones already moved go back, so that the database
// never sits apart from its write-ahead log.
func (m *migrator) moveGroupTogether(src, dst, key string, names []string) bool {
	failed := false
	for _, name := range names {
		if !m.move(filepath.Join(src, name), filepath.Join(dst, name)) {
			failed = true
			break
		}
	}
	if !failed {
		return true
	}
	database := filepath.Join(src, key)
	if stranded := m.restoreGroup(src, dst, names); len(stranded) > 0 {
		m.fail(database, fmt.Errorf("a database and its -wal/-shm files must stay together, and moving them back failed; the files are now at: %s; put them back together in %s by hand before starting Saki", strings.Join(stranded, "; "), src))
	} else {
		m.fail(database, errors.New("a database and its -wal/-shm files must stay together, so the files already moved were moved back"))
	}
	return false
}

// restoreGroup moves every member of a half-moved group from dst back to src.
// dst held none of them before, so whatever is there came from src. It
// returns where each file is if any could not be moved back.
func (m *migrator) restoreGroup(src, dst string, names []string) []string {
	restored := true
	for _, name := range names {
		from, to := filepath.Join(dst, name), filepath.Join(src, name)
		info, err := os.Lstat(from)
		if err != nil {
			continue // never arrived
		}
		if _, err := os.Lstat(to); err == nil {
			// Copied, but the original could not be removed: drop the copy.
			if same, err := sameContent(to, from); err == nil && same && os.Remove(from) == nil {
				continue
			}
			restored = false
			continue
		}
		if _, err := m.transfer(from, to, info); err != nil {
			m.line("error", "%s: moving it back to %s: %v", from, to, err)
			restored = false
			continue
		}
		m.line("undo", "%s -> %s", from, to)
	}
	if restored {
		return nil
	}
	var stranded []string
	for _, name := range names {
		for _, dir := range []string{src, dst} {
			if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
				stranded = append(stranded, filepath.Join(dir, name))
			}
		}
	}
	return stranded
}

// hasOrphanJournal reports whether a directory below root holds a SQLite
// sidecar without its database beside it.
func hasOrphanJournal(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil
		}
		present := make(map[string]bool, len(entries))
		for _, e := range entries {
			present[e.Name()] = true
		}
		for _, e := range entries {
			if key := groupKey(e.Name()); key != e.Name() && !present[key] {
				found = true
				return fs.SkipAll
			}
		}
		return nil
	})
	return found
}

func suffixed(key string) []string {
	names := make([]string, 0, len(sqliteSidecars))
	for _, suffix := range sqliteSidecars {
		names = append(names, key+suffix)
	}
	return names
}

// copyVerified copies src to a new file dst, keeping mode and modification
// time, and checks size and SHA-256 of the copy. A failed copy is removed.
func copyVerified(src, dst string, info fs.FileInfo) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(out, hash), in)
	if syncErr := out.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
		return err
	}
	copied, err := os.Stat(dst)
	if err != nil {
		return err
	}
	if written != info.Size() || copied.Size() != info.Size() {
		return fmt.Errorf("copy of %s has %d bytes, want %d", src, copied.Size(), info.Size())
	}
	sum, err := fileSHA256(dst)
	if err != nil {
		return err
	}
	if !bytes.Equal(sum, hash.Sum(nil)) {
		return fmt.Errorf("copy of %s does not match its SHA-256", src)
	}
	return nil
}

func sameContent(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, err
	}
	if !leftInfo.Mode().IsRegular() || !rightInfo.Mode().IsRegular() || leftInfo.Size() != rightInfo.Size() {
		return false, nil
	}
	leftSum, err := fileSHA256(left)
	if err != nil {
		return false, err
	}
	rightSum, err := fileSHA256(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(leftSum, rightSum), nil
}

func fileSHA256(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}
