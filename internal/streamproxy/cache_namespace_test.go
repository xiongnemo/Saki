package streamproxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/subsonic"
)

func TestProxyCachePathUsesVerifiedLibraryFingerprintNamespace(t *testing.T) {
	// Given
	cacheDir := t.TempDir()
	client := subsonic.NewClient(nil)
	cfg := configForEndpoint("https://one.example", cacheDir)
	cfg.Account.LibraryFingerprint = "verified-library"
	client.Configure(cfg)
	proxy := New(client, models.Settings{CacheDir: cacheDir, AudioCacheMaxBytes: 1024 * 1024})

	// When
	firstPath := proxy.cachePath("song-1")
	cfg.Account.Endpoints = []models.Endpoint{{Name: "two", URL: "https://two.example", Enabled: true}}
	client.Configure(cfg)
	secondPath := proxy.cachePath("song-1")

	// Then
	if firstPath != secondPath {
		t.Fatalf("verified namespace changed across endpoints:\nfirst:  %s\nsecond: %s", firstPath, secondPath)
	}
	if !strings.Contains(firstPath, "audio") {
		t.Fatalf("audio cache path missing audio root: %s", firstPath)
	}
}

func TestProxyCachePathFallsBackToUnverifiedEndpointNamespace(t *testing.T) {
	// Given
	cacheDir := t.TempDir()
	client := subsonic.NewClient(nil)
	cfg := configForEndpoint("https://one.example", cacheDir)
	client.Configure(cfg)
	proxy := New(client, models.Settings{CacheDir: cacheDir, AudioCacheMaxBytes: 1024 * 1024})

	// When
	firstPath := proxy.cachePath("song-1")
	cfg.Account.Endpoints = []models.Endpoint{{Name: "two", URL: "https://two.example", Enabled: true}}
	client.Configure(cfg)
	secondPath := proxy.cachePath("song-1")

	// Then
	if firstPath == secondPath {
		t.Fatalf("unverified endpoint namespaces should differ, both were %s", firstPath)
	}
}

func TestProxyPruneUsesCurrentLibraryFingerprintNamespace(t *testing.T) {
	// Given
	cacheDir := t.TempDir()
	client := subsonic.NewClient(nil)
	cfg := configForEndpoint("https://one.example", cacheDir)
	cfg.Account.LibraryFingerprint = "library-a"
	client.Configure(cfg)
	proxy := New(client, models.Settings{CacheDir: cacheDir, AudioCacheMaxBytes: 5})
	otherNamespacePath := proxy.cachePath("other-namespace")
	if err := os.WriteFile(otherNamespacePath, []byte("abcde"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Account.LibraryFingerprint = "library-b"
	client.Configure(cfg)
	oldPath := proxy.cachePath("old-current")
	newPath := proxy.cachePath("new-current")
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
	if err := os.Chtimes(otherNamespacePath, now.Add(-2*time.Minute), now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, now, now); err != nil {
		t.Fatal(err)
	}

	// When
	if err := proxy.prune(); err != nil {
		t.Fatal(err)
	}

	// Then
	if filepath.Dir(oldPath) != filepath.Dir(newPath) {
		t.Fatalf("test setup paths should share current namespace: %s vs %s", oldPath, newPath)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("expected old current-namespace file to be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(otherNamespacePath); !os.IsNotExist(err) {
		t.Fatalf("expected oldest cross-namespace file to be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected new current-namespace file to remain: %v", err)
	}
}
