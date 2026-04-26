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

func TestProxyServesRangeFromCompletedCache(t *testing.T) {
	cacheDir := t.TempDir()
	proxy := New(subsonic.NewClient(nil), models.Settings{CacheDir: filepath.Dir(cacheDir), AudioCacheMaxBytes: 1024 * 1024})
	proxy.cacheDir = cacheDir
	if err := os.MkdirAll(proxy.cacheDir, 0o700); err != nil {
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
	if err := os.MkdirAll(proxy.cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(proxy.cacheDir, "old.audio")
	newPath := filepath.Join(proxy.cacheDir, "new.audio")
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
