package subsonic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/xiongnemo/saki/internal/models"
)

func TestGetArtistsUsesTokenAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/getArtists" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		query := r.URL.Query()
		if query.Get("u") != "nemo" {
			t.Fatalf("unexpected username %q", query.Get("u"))
		}
		if query.Get("p") != "" {
			t.Fatalf("plaintext password should not be sent with token auth")
		}
		if query.Get("t") == "" || query.Get("s") == "" {
			t.Fatalf("expected token and salt auth params")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"subsonic-response": map[string]any{
				"status": "ok",
				"artists": map[string]any{
					"index": []any{
						map[string]any{
							"name": "A",
							"artist": []any{
								map[string]any{"id": "ar-1", "name": "Artist", "albumCount": 2},
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.Configure(configForURL(server.URL))

	artists, err := client.GetArtists(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 1 || artists[0].ID != "ar-1" || artists[0].AlbumCount != 2 {
		t.Fatalf("unexpected artists: %#v", artists)
	}
}

func TestGetAllAlbumsPaginatesUntilShortBatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/getAlbumList2" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		requests++
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		albums := make([]any, 0)
		limit := 500
		if offset >= 500 {
			limit = 1
		}
		for i := 0; i < limit; i++ {
			albums = append(albums, map[string]any{
				"id":        strconv.Itoa(offset + i),
				"name":      "Album",
				"artist":    "Artist",
				"songCount": 1,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"subsonic-response": map[string]any{
				"status": "ok",
				"albumList2": map[string]any{
					"album": albums,
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.Configure(configForURL(server.URL))

	albums, err := client.GetAllAlbums(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(albums) != 501 {
		t.Fatalf("expected 501 albums, got %d", len(albums))
	}
	if requests != 2 {
		t.Fatalf("expected 2 requests, got %d", requests)
	}
}

func TestOpenStreamFallsBackToSecondEndpoint(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer first.Close()

	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/stream" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("audio"))
	}))
	defer second.Close()

	client := NewClient(second.Client())
	client.Configure(models.Config{
		Account: models.Account{
			Username: "nemo",
			Password: "secret",
			Endpoints: []models.Endpoint{
				{Name: "first", URL: first.URL, Enabled: true},
				{Name: "second", URL: second.URL, Enabled: true},
			},
		},
		Settings: models.Settings{EndpointSwitchThreshold: 0.30, EndpointTimeoutSeconds: 2},
	})

	resp, err := client.OpenStream(context.Background(), "song-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "audio" {
		t.Fatalf("unexpected body %q", body)
	}
	if client.ActiveEndpoint().URL != second.URL {
		t.Fatalf("expected active endpoint to switch to second, got %#v", client.ActiveEndpoint())
	}
}

func TestHealthCheckSwitchesToConsistentlyFasterEndpoint(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(30 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok"}})
	}))
	defer slow.Close()

	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok"}})
	}))
	defer fast.Close()

	client := NewClient(slow.Client())
	client.Configure(models.Config{
		Account: models.Account{
			Username: "nemo",
			Password: "secret",
			Endpoints: []models.Endpoint{
				{Name: "slow", URL: slow.URL, Enabled: true},
				{Name: "fast", URL: fast.URL, Enabled: true},
			},
		},
		Settings: models.Settings{EndpointSwitchThreshold: 0.30, EndpointTimeoutSeconds: 2},
	})

	client.checkAllEndpoints(context.Background())
	client.checkAllEndpoints(context.Background())

	if client.ActiveEndpoint().URL != fast.URL {
		t.Fatalf("expected active endpoint to switch to fast, got %#v", client.ActiveEndpoint())
	}
}

func configForURL(rawURL string) models.Config {
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
			HealthCheckIntervalSeconds: 5,
			EndpointTimeoutSeconds:     2,
			EndpointSwitchThreshold:    0.30,
		},
	}
}
