package offline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type OpenVariant func(context.Context, string, string, string) (*http.Response, string, error)

type DownloadManager struct {
	store    *Store
	root     string
	maxBytes int64
	open     OpenVariant
	sem      chan struct{}
	mu       sync.Mutex
	active   map[string]struct{}
}

func NewDownloadManager(store *Store, root string, maxBytes int64, concurrency int, open OpenVariant) *DownloadManager {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > 4 {
		concurrency = 4
	}
	return &DownloadManager{store: store, root: root, maxBytes: maxBytes, open: open, sem: make(chan struct{}, concurrency), active: make(map[string]struct{})}
}

func (m *DownloadManager) SetMaxBytes(maxBytes int64) {
	m.mu.Lock()
	m.maxBytes = maxBytes
	m.mu.Unlock()
}

func (m *DownloadManager) SetStore(store *Store, root string) {
	m.mu.Lock()
	m.store = store
	m.root = root
	m.mu.Unlock()
}

func (m *DownloadManager) Enqueue(ctx context.Context, songID, variant string, pinned bool) error {
	if variant != "raw" {
		variant = "stream"
	}
	key := songID + "\x00" + variant
	m.mu.Lock()
	if _, ok := m.active[key]; ok {
		m.mu.Unlock()
		return nil
	}
	m.active[key] = struct{}{}
	m.mu.Unlock()
	go func() {
		defer func() { m.mu.Lock(); delete(m.active, key); m.mu.Unlock() }()
		m.sem <- struct{}{}
		defer func() { <-m.sem }()
		_ = m.Download(ctx, songID, variant, pinned)
	}()
	return nil
}

func (m *DownloadManager) Download(ctx context.Context, songID, variant string, pinned bool) error {
	m.mu.Lock()
	store, root, open := m.store, m.root, m.open
	m.mu.Unlock()
	if store == nil || open == nil {
		return fmt.Errorf("download manager is not configured")
	}
	if variant != "raw" {
		variant = "stream"
	}
	now := time.Now().UTC()
	previous, _ := store.Download(ctx, songID, variant)
	previous.Status = "downloading"
	previous.Pinned = pinned
	previous.Attempts++
	previous.Error = ""
	previous.UpdatedAt = now
	previous.SongID = songID
	previous.Variant = variant
	if err := store.UpsertDownload(ctx, previous); err != nil {
		return err
	}
	path := filePath(root, songID, variant)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	partial := path + ".partial"
	resumeBytes := int64(0)
	if info, statErr := os.Stat(partial); statErr == nil && info.Size() > 0 {
		resumeBytes = info.Size()
	}
	rangeHeader := ""
	if resumeBytes > 0 {
		rangeHeader = fmt.Sprintf("bytes=%d-", resumeBytes)
	}
	resp, endpoint, err := open(ctx, songID, variant, rangeHeader)
	if err != nil {
		previous.Status = "failed"
		previous.Error = err.Error()
		previous.UpdatedAt = time.Now().UTC()
		_ = store.UpsertDownload(ctx, previous)
		return err
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	if (resp.StatusCode < 200 || resp.StatusCode >= 300) && !(resumeBytes > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable) {
		err = fmt.Errorf("download returned HTTP %d", resp.StatusCode)
		previous.Status = "failed"
		previous.Error = err.Error()
		previous.UpdatedAt = time.Now().UTC()
		_ = store.UpsertDownload(ctx, previous)
		return err
	}
	if resumeBytes > 0 && resp.StatusCode != http.StatusPartialContent {
		resumeBytes = 0
		_ = resp.Body.Close()
		resp, endpoint, err = open(ctx, songID, variant, "")
		if err != nil {
			previous.Status = "failed"
			previous.Error = err.Error()
			previous.UpdatedAt = time.Now().UTC()
			_ = store.UpsertDownload(ctx, previous)
			return err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			err = fmt.Errorf("download returned HTTP %d", resp.StatusCode)
			previous.Status = "failed"
			previous.Error = err.Error()
			previous.UpdatedAt = time.Now().UTC()
			_ = store.UpsertDownload(ctx, previous)
			return err
		}
	}
	fileFlags := os.O_CREATE | os.O_WRONLY
	if resumeBytes > 0 {
		fileFlags |= os.O_APPEND
	} else {
		fileFlags |= os.O_TRUNC
	}
	file, err := os.OpenFile(partial, fileFlags, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	if resumeBytes > 0 {
		existing, hashErr := os.Open(partial)
		if hashErr != nil {
			_ = file.Close()
			return hashErr
		}
		_, hashErr = io.CopyN(hash, existing, resumeBytes)
		_ = existing.Close()
		if hashErr != nil {
			_ = file.Close()
			return hashErr
		}
	}
	newBytes, copyErr := io.Copy(io.MultiWriter(file, hash), resp.Body)
	bytes := resumeBytes + newBytes
	closeErr := file.Close()
	if copyErr != nil {
		previous.Status = "failed"
		previous.Error = copyErr.Error()
		previous.UpdatedAt = time.Now().UTC()
		_ = store.UpsertDownload(ctx, previous)
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	m.mu.Lock()
	maxBytes := m.maxBytes
	m.mu.Unlock()
	if maxBytes > 0 {
		used, quotaErr := store.DownloadedBytes(ctx)
		if quotaErr != nil {
			_ = os.Remove(partial)
			return quotaErr
		}
		oldBytes := previous.Bytes
		if used-oldBytes+bytes > maxBytes {
			_ = os.Remove(partial)
			err = fmt.Errorf("offline storage limit exceeded")
			previous.Status = "failed"
			previous.Error = err.Error()
			previous.UpdatedAt = time.Now().UTC()
			_ = store.UpsertDownload(ctx, previous)
			return err
		}
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(partial, path); err != nil {
		_ = os.Remove(partial)
		return err
	}
	completed := time.Now().UTC()
	record := Download{SongID: songID, Variant: variant, Status: "complete", Pinned: pinned, Path: path, Bytes: bytes, ContentType: resp.Header.Get("Content-Type"), SHA256: hex.EncodeToString(hash.Sum(nil)), SourceEndpoint: endpoint, Attempts: previous.Attempts, UpdatedAt: completed, CompletedAt: completed}
	return store.UpsertDownload(ctx, record)
}

func (m *DownloadManager) Path(ctx context.Context, songID, variant string) (string, bool) {
	m.mu.Lock()
	store := m.store
	m.mu.Unlock()
	if store == nil {
		return "", false
	}
	return store.CompletedPath(ctx, songID, variant)
}

func (m *DownloadManager) filePath(songID, variant string) string {
	m.mu.Lock()
	root := m.root
	m.mu.Unlock()
	return filePath(root, songID, variant)
}

func filePath(root, songID, variant string) string {
	name := fmt.Sprintf("%x.%s", sha256.Sum256([]byte(songID)), variant)
	return filepath.Join(root, name)
}

func (m *DownloadManager) HasAny(ctx context.Context, songID string) bool {
	for _, variant := range []string{"stream", "raw"} {
		if _, ok := m.Path(ctx, songID, variant); ok {
			return true
		}
	}
	return false
}

func IsLocalPath(path string) bool {
	return !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://")
}
