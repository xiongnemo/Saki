package player

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiongnemo/saki/internal/cachepaths"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/subsonic"
)

func TestCoverCacheUsesLibraryFingerprintNamespace(t *testing.T) {
	// Given
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/getCoverArt" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("cover"))
	}))
	defer server.Close()
	client := subsonic.NewClient(server.Client())
	cfg := coverCacheConfig(server.URL, "library-a")
	client.Configure(cfg)
	service := New(client, newFakeAudio(), newFakeMedia())
	defer service.Close()
	cacheRoot := t.TempDir()
	service.SetCacheRoot(cacheRoot)
	track := models.Song{ID: "song-1", CoverArt: "cover-1"}

	// When
	firstPath, err := service.cacheCoverArt(context.Background(), track)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Account.LibraryFingerprint = "library-b"
	client.Configure(cfg)
	secondPath, err := service.cacheCoverArt(context.Background(), track)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatalf("cover cache paths should differ across library fingerprints, both were %s", firstPath)
	}
	if filepath.Dir(filepath.Dir(firstPath)) != filepath.Join(cacheRoot, "covers") {
		t.Fatalf("cover cache path should live under namespaced covers root, got %s", firstPath)
	}
}

func TestCoverCacheUsesServingEndpointNamespaceAfterFallback(t *testing.T) {
	// Given
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/getCoverArt" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("fallback-cover"))
	}))
	defer fallback.Close()
	cacheRoot := t.TempDir()
	client := subsonic.NewClient(primary.Client())
	client.Configure(coverCacheFallbackConfig(primary.URL, fallback.URL))
	service := New(client, newFakeAudio(), newFakeMedia())
	defer service.Close()
	service.SetCacheRoot(cacheRoot)
	track := models.Song{ID: "song-1", CoverArt: "cover-1"}
	primaryPath := cachepaths.NewScope(cacheRoot, "nemo", primary.URL, "").CoverPath("cover-1")
	fallbackPath := cachepaths.NewScope(cacheRoot, "nemo", fallback.URL, "").CoverPath("cover-1")

	// When
	path, err := service.cacheCoverArt(context.Background(), track)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if path != fallbackPath {
		t.Fatalf("cover path = %s, want fallback path %s", path, fallbackPath)
	}
	data, err := os.ReadFile(fallbackPath)
	if err != nil {
		t.Fatalf("expected fallback namespace cover file: %v", err)
	}
	if string(data) != "fallback-cover" {
		t.Fatalf("fallback cover body = %q, want fallback-cover", data)
	}
	if _, err := os.Stat(primaryPath); !os.IsNotExist(err) {
		t.Fatalf("primary namespace should not contain fallback cover, stat err=%v", err)
	}
}

func coverCacheConfig(rawURL string, fingerprint string) models.Config {
	return models.Config{
		Account: models.Account{
			Username:           "nemo",
			Password:           "secret",
			LibraryFingerprint: fingerprint,
			Endpoints: []models.Endpoint{{
				Name:    "Test",
				URL:     rawURL,
				Enabled: true,
			}},
		},
		Settings: models.Settings{
			HealthCheckIntervalSeconds: 5,
			EndpointTimeoutSeconds:     2,
			EndpointSwitchThreshold:    0.30,
		},
	}
}

func coverCacheFallbackConfig(primaryURL string, fallbackURL string) models.Config {
	cfg := coverCacheConfig(primaryURL, "")
	cfg.Account.Endpoints = append(cfg.Account.Endpoints, models.Endpoint{Name: "Fallback", URL: fallbackURL, Enabled: true})
	return cfg
}
