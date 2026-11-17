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
	if m.dryRun {
		m.act("move", "%s -> %s", src, dst)
		return true
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		m.fail(dst, err)
		return false
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return m.moveSymlink(src, dst)
	}
	if err := m.rename(src, dst); err == nil {
		m.act("move", "%s -> %s", src, dst)
		return true
	}
	// The rename failed, usually because dst is on another volume: copy.
	if info.IsDir() {
		if err := os.Mkdir(dst, info.Mode().Perm()); err != nil {
			m.fail(dst, err)
			return false
		}
		return m.mergeDir(src, dst)
	}
	if !info.Mode().IsRegular() {
		m.skip(src, "not a regular file")
		return false
	}
	if err := copyVerified(src, dst, info); err != nil {
		m.fail(src, err)
		return false
	}
	if err := os.Remove(src); err != nil {
		m.fail(src, err)
		return false
	}
	m.act("move", "%s -> %s (copied and verified)", src, dst)
	return true
}

// moveSymlink recreates a symlink at dst. A relative target is made absolute
// so that the link still points at the same file from its new directory.
func (m *migrator) moveSymlink(src, dst string) bool {
	target := symlinkTarget(src)
	if target == "" {
		m.fail(src, errors.New("cannot read symlink"))
		return false
	}
	if err := os.Symlink(target, dst); err != nil {
		m.fail(dst, err)
		return false
	}
	if err := os.Remove(src); err != nil {
		m.fail(src, err)
		return false
	}
	m.act("move", "%s -> %s (symlink to %s)", src, dst, target)
	return true
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

// moveGroup moves a database and its sidecars. When any of them exists at
// dst, the group is only removed from src if dst holds exactly the same files.
func (m *migrator) moveGroup(src, dst, key string, names []string) bool {
	var atDst []string
	for _, name := range append([]string{key}, suffixed(key)...) {
		if _, err := os.Lstat(filepath.Join(dst, name)); err == nil {
			atDst = append(atDst, name)
		}
	}
	if len(atDst) == 0 {
		gone := true
		for _, name := range names {
			if !m.move(filepath.Join(src, name), filepath.Join(dst, name)) {
				gone = false
			}
		}
		return gone
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
