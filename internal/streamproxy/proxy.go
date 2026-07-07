package streamproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiongnemo/saki/internal/cachepaths"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/subsonic"
)

type Proxy struct {
	client   *subsonic.Client
	settings models.Settings

	mu        sync.Mutex
	inflight  map[string]*sync.Mutex
	cacheJobs map[string]struct{}
	server    *http.Server
	listener  net.Listener
	baseURL   string
	maxBytes  int64
	ctx       context.Context
}

func New(client *subsonic.Client, settings models.Settings) *Proxy {
	maxBytes := settings.AudioCacheMaxBytes
	if maxBytes <= 0 {
		maxBytes = 2 * 1024 * 1024 * 1024
	}
	return &Proxy{
		client:    client,
		settings:  settings,
		inflight:  make(map[string]*sync.Mutex),
		cacheJobs: make(map[string]struct{}),
		maxBytes:  maxBytes,
	}
}

func (p *Proxy) Start(ctx context.Context) error {
	if err := os.MkdirAll(p.audioDir(), 0o700); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	p.listener = listener
	p.baseURL = "http://" + listener.Addr().String()
	p.mu.Lock()
	p.ctx = ctx
	p.mu.Unlock()
	mux := http.NewServeMux()
	mux.HandleFunc("/stream/", p.handleStream)
	p.server = &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = p.server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := p.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "stream proxy stopped: %v\n", err)
		}
	}()
	return nil
}

func (p *Proxy) Close(ctx context.Context) error {
	if p.server == nil {
		return nil
	}
	return p.server.Shutdown(ctx)
}

func (p *Proxy) TrackURL(id string) string {
	return p.baseURL + "/stream/" + id
}

func (p *Proxy) CachedPath(id string) (string, bool) {
	cachePath := p.cachePath(id)
	if fileInfo, err := os.Stat(cachePath); err == nil && fileInfo.Size() > 0 {
		_ = os.Chtimes(cachePath, time.Now(), time.Now())
		return cachePath, true
	}
	return "", false
}

func (p *Proxy) EnsureCached(ctx context.Context, id string) (string, error) {
	if cachePath, ok := p.CachedPath(id); ok {
		return cachePath, nil
	}

	lock := p.lockFor(id)
	lock.Lock()
	defer lock.Unlock()

	if cachePath, ok := p.CachedPath(id); ok {
		return cachePath, nil
	}

	resp, endpoint, err := p.client.OpenStreamWithEndpoint(ctx, id, "")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("stream upstream returned HTTP %d", resp.StatusCode)
	}
	cachePath := p.cachePathForEndpoint(id, endpoint)

	partialPath := cachePath + ".partial"
	partial, err := os.Create(partialPath)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(partial, resp.Body)
	closeErr := partial.Close()
	if copyErr != nil {
		_ = os.Remove(partialPath)
		return "", copyErr
	}
	if closeErr != nil {
		_ = os.Remove(partialPath)
		return "", closeErr
	}
	if err := os.Rename(partialPath, cachePath); err != nil {
		_ = os.Remove(partialPath)
		return "", err
	}
	_ = p.prune()
	return cachePath, nil
}

func (p *Proxy) UpdateSettings(settings models.Settings) error {
	maxBytes := settings.AudioCacheMaxBytes
	if maxBytes <= 0 {
		maxBytes = 2 * 1024 * 1024 * 1024
	}

	p.mu.Lock()
	p.settings = settings
	p.maxBytes = maxBytes
	p.mu.Unlock()

	return os.MkdirAll(p.audioDir(), 0o700)
}

func (p *Proxy) handleStream(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/stream/")
	if id == "" {
		http.Error(w, "missing track id", http.StatusBadRequest)
		return
	}

	cachePath := p.cachePath(id)
	if fileInfo, err := os.Stat(cachePath); err == nil && fileInfo.Size() > 0 {
		_ = os.Chtimes(cachePath, time.Now(), time.Now())
		http.ServeFile(w, r, cachePath)
		return
	}

	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		p.ensureCachedAsync(id)
		p.proxyRange(w, r, id, rangeHeader)
		return
	}

	p.proxyAndCache(w, r, id)
}

func (p *Proxy) proxyRange(w http.ResponseWriter, r *http.Request, id string, rangeHeader string) {
	resp, err := p.client.OpenStream(r.Context(), id, rangeHeader)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *Proxy) ensureCachedAsync(id string) {
	cachePath := p.cachePath(id)
	if fileInfo, err := os.Stat(cachePath); err == nil && fileInfo.Size() > 0 {
		_ = os.Chtimes(cachePath, time.Now(), time.Now())
		return
	}

	p.mu.Lock()
	if _, ok := p.cacheJobs[id]; ok {
		p.mu.Unlock()
		return
	}
	p.cacheJobs[id] = struct{}{}
	ctx := p.ctx
	p.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.cacheJobs, id)
			p.mu.Unlock()
		}()
		_, _ = p.EnsureCached(ctx, id)
	}()
}

func (p *Proxy) proxyAndCache(w http.ResponseWriter, r *http.Request, id string) {
	lock := p.lockFor(id)
	lock.Lock()
	defer lock.Unlock()

	if cachePath, ok := p.CachedPath(id); ok {
		http.ServeFile(w, r, cachePath)
		return
	}

	resp, endpoint, err := p.client.OpenStreamWithEndpoint(r.Context(), id, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	cachePath := p.cachePathForEndpoint(id, endpoint)

	partialPath := cachePath + ".partial"
	partial, err := os.Create(partialPath)
	if err != nil {
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, copyErr := io.Copy(io.MultiWriter(w, partial), resp.Body)
	closeErr := partial.Close()
	if copyErr == nil && closeErr == nil {
		if err := os.Rename(partialPath, cachePath); err == nil {
			_ = p.prune()
			return
		}
	}
	_ = os.Remove(partialPath)
}

func (p *Proxy) lockFor(id string) *sync.Mutex {
	p.mu.Lock()
	defer p.mu.Unlock()
	lock := p.inflight[id]
	if lock == nil {
		lock = &sync.Mutex{}
		p.inflight[id] = lock
	}
	return lock
}

func (p *Proxy) cachePath(id string) string {
	path := p.cacheScope().AudioPath(id)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	return path
}

func (p *Proxy) cachePathForEndpoint(id string, endpoint models.Endpoint) string {
	path := p.cacheScopeForEndpoint(endpoint).AudioPath(id)
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	return path
}

func (p *Proxy) audioDir() string {
	return p.cacheScope().AudioDir()
}

func (p *Proxy) cacheScope() cachepaths.Scope {
	p.mu.Lock()
	root := p.settings.CacheDir
	p.mu.Unlock()
	return cacheScope(p.client, root)
}

func (p *Proxy) cacheScopeForEndpoint(endpoint models.Endpoint) cachepaths.Scope {
	p.mu.Lock()
	root := p.settings.CacheDir
	p.mu.Unlock()
	return cacheScopeForEndpoint(p.client, root, endpoint)
}

func cacheScope(client *subsonic.Client, root string) cachepaths.Scope {
	if client == nil {
		return cachepaths.NewScope(root, "", "", "")
	}
	account := client.ActiveAccount()
	endpoint := client.ActiveEndpoint()
	return cachepaths.NewScope(root, account.Username, endpoint.URL, account.LibraryFingerprint)
}

func cacheScopeForEndpoint(client *subsonic.Client, root string, endpoint models.Endpoint) cachepaths.Scope {
	if client == nil {
		return cachepaths.NewScope(root, "", endpoint.URL, "")
	}
	account := client.ActiveAccount()
	return cachepaths.NewScope(root, account.Username, endpoint.URL, account.LibraryFingerprint)
}

func (p *Proxy) prune() error {
	files, err := cachedAudioFiles(p.cacheScope().AudioRoot())
	if err != nil {
		return err
	}
	var total int64
	for _, file := range files {
		total += file.size
	}
	if total <= p.maxBytes {
		return nil
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].modTime.Equal(files[j].modTime) {
			return files[i].path < files[j].path
		}
		return files[i].modTime.Before(files[j].modTime)
	})
	for _, file := range files {
		if total <= p.maxBytes {
			break
		}
		if err := os.Remove(file.path); err == nil {
			total -= file.size
		}
	}
	return nil
}

type cacheFile struct {
	path    string
	size    int64
	modTime time.Time
}

func cachedAudioFiles(root string) ([]cacheFile, error) {
	var files []cacheFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".partial") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		files = append(files, cacheFile{path: path, size: info.Size(), modTime: info.ModTime()})
		return nil
	})
	return files, err
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
