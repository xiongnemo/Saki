package streamproxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiongnemo/saki/internal/cachepaths"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/subsonic"
)

func TestProxyStreamsAndCommitsCompletedCache(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/stream" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer upstream.Close()

	client := subsonic.NewClient(upstream.Client())
	client.Configure(configForEndpoint(upstream.URL, t.TempDir()))

	proxy := New(client, models.Settings{CacheDir: t.TempDir(), AudioCacheMaxBytes: 1024 * 1024})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())

	resp, err := http.Get(proxy.TrackURL("song-1"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "0123456789" {
		t.Fatalf("unexpected body %q", body)
	}
	if _, err := os.Stat(proxy.cachePath("song-1")); err != nil {
		t.Fatalf("expected completed cache file: %v", err)
	}
}

func TestProxyStreamsFallbackUnderServingEndpointNamespace(t *testing.T) {
	// Given
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/stream" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("fallback-audio"))
	}))
	defer fallback.Close()
	cacheDir := t.TempDir()
	client := subsonic.NewClient(primary.Client())
	client.Configure(configForEndpointPair(primary.URL, fallback.URL, cacheDir))
	proxy := New(client, models.Settings{CacheDir: cacheDir, AudioCacheMaxBytes: 1024 * 1024})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())

	// When
	resp, err := http.Get(proxy.TrackURL("song-1"))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Then
	if string(body) != "fallback-audio" {
		t.Fatalf("unexpected body %q", body)
	}
	primaryPath := cachepaths.NewScope(cacheDir, "nemo", primary.URL, "").AudioPath("song-1")
	fallbackPath := cachepaths.NewScope(cacheDir, "nemo", fallback.URL, "").AudioPath("song-1")
	data, err := os.ReadFile(fallbackPath)
	if err != nil {
		t.Fatalf("expected fallback namespace cache file: %v", err)
	}
	if string(data) != "fallback-audio" {
		t.Fatalf("fallback cache body = %q, want fallback-audio", data)
	}
	if _, err := os.Stat(primaryPath); !os.IsNotExist(err) {
		t.Fatalf("primary namespace should not contain fallback bytes, stat err=%v", err)
	}
}

func TestEnsureCachedFallbackUnderServingEndpointNamespace(t *testing.T) {
	// Given
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/stream" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("fallback-audio"))
	}))
	defer fallback.Close()
	cacheDir := t.TempDir()
	client := subsonic.NewClient(primary.Client())
	client.Configure(configForEndpointPair(primary.URL, fallback.URL, cacheDir))
	proxy := New(client, models.Settings{CacheDir: cacheDir, AudioCacheMaxBytes: 1024 * 1024})
	primaryPath := cachepaths.NewScope(cacheDir, "nemo", primary.URL, "").AudioPath("song-1")
	fallbackPath := cachepaths.NewScope(cacheDir, "nemo", fallback.URL, "").AudioPath("song-1")

	// When
	path, err := proxy.EnsureCached(context.Background(), "song-1")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if path != fallbackPath {
		t.Fatalf("cache path = %s, want fallback path %s", path, fallbackPath)
	}
	data, err := os.ReadFile(fallbackPath)
	if err != nil {
		t.Fatalf("expected fallback namespace cache file: %v", err)
	}
	if string(data) != "fallback-audio" {
		t.Fatalf("fallback cache body = %q, want fallback-audio", data)
	}
	if _, err := os.Stat(primaryPath); !os.IsNotExist(err) {
		t.Fatalf("primary namespace should not contain fallback bytes, stat err=%v", err)
	}
}

func TestProxyServesRangeFromCompletedCache(t *testing.T) {
	cacheDir := t.TempDir()
	proxy := New(subsonic.NewClient(nil), models.Settings{CacheDir: filepath.Dir(cacheDir), AudioCacheMaxBytes: 1024 * 1024})
	proxy.settings.CacheDir = filepath.Dir(cacheDir)
	audioDir := proxy.audioDir()
	if err := os.MkdirAll(audioDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(proxy.cachePath("song-1"), []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())

	req, err := http.NewRequest(http.MethodGet, proxy.TrackURL("song-1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", resp.StatusCode)
	}
	if string(body) != "2345" {
		t.Fatalf("unexpected range body %q", body)
	}
}

func TestProxyCachesFullFileDuringRangePlayback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/stream" {
			t.Fatalf("unexpected upstream path %s", r.URL.Path)
		}
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 2-5/10")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("2345"))
			return
		}
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer upstream.Close()

	client := subsonic.NewClient(upstream.Client())
	client.Configure(configForEndpoint(upstream.URL, t.TempDir()))

	proxy := New(client, models.Settings{CacheDir: t.TempDir(), AudioCacheMaxBytes: 1024 * 1024})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer proxy.Close(context.Background())

	req, err := http.NewRequest(http.MethodGet, proxy.TrackURL("song-1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("expected 206, got %d", resp.StatusCode)
	}
	if string(body) != "2345" {
		t.Fatalf("unexpected range body %q", body)
	}

	cachePath := proxy.cachePath("song-1")
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(cachePath)
		if err == nil && string(data) == "0123456789" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected completed cache file, read err=%v data=%q", err, data)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProxyPrunesLeastRecentlyUsedFiles(t *testing.T) {
	proxy := New(subsonic.NewClient(nil), models.Settings{CacheDir: t.TempDir(), AudioCacheMaxBytes: 5})
	audioDir := proxy.audioDir()
	if err := os.MkdirAll(audioDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(audioDir, "old.audio")
	newPath := filepath.Join(audioDir, "new.audio")
	if err := os.WriteFile(oldPath, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("67890"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(oldPath, now.Add(-time.Minute), now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, now, now); err != nil {
		t.Fatal(err)
	}
	if err := proxy.prune(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old file to be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected new file to remain: %v", err)
	}
}

func configForEndpoint(rawURL string, cacheDir string) models.Config {
	return models.Config{
		Account: models.Account{
			Username: "nemo",
			Password: "secret",
			Endpoints: []models.Endpoint{{
				Name:    "Test",
				URL:     rawURL,
				Enabled: true,
			}},
		},
		Settings: models.Settings{
			CacheDir:                   cacheDir,
			AudioCacheMaxBytes:         1024 * 1024,
			HealthCheckIntervalSeconds: 5,
			EndpointTimeoutSeconds:     2,
			EndpointSwitchThreshold:    0.30,
		},
	}
}

func configForEndpointPair(primaryURL string, fallbackURL string, cacheDir string) models.Config {
	cfg := configForEndpoint(primaryURL, cacheDir)
	cfg.Account.Endpoints = append(cfg.Account.Endpoints, models.Endpoint{Name: "Fallback", URL: fallbackURL, Enabled: true})
	return cfg
}
