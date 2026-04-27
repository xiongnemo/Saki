package subsonic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func TestMapSongPreservesAudioMetadata(t *testing.T) {
	song := mapSong(songJSON{
		ID:           "song-1",
		Title:        "Song",
		Suffix:       "flac",
		ContentType:  "audio/flac",
		BitRateKbps:  880,
		BitDepth:     16,
		SamplingRate: 44100,
		ChannelCount: 2,
	})

	if song.Suffix != "flac" || song.BitRateKbps != 880 || song.BitDepth != 16 || song.SamplingRate != 44100 || song.ChannelCount != 2 {
		t.Fatalf("song metadata not mapped: %#v", song)
	}
	info := song.AudioInfo()
	if info.Codec != "FLAC" || info.BitRateKbps != 880 || info.BitDepth != 16 || info.SampleRate != 44100 || info.Channels != 2 {
		t.Fatalf("song audio info = %#v", info)
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

func TestProbeEndpointsReportsLatencyWithoutMutatingHealthState(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/rest/ping.view" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok"}})
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.Configure(configForURL(server.URL))
	before := client.EndpointStatuses()

	results := client.ProbeEndpoints(context.Background(), []models.Endpoint{{Name: "Test", URL: server.URL, Enabled: true}})
	if len(results) != 1 {
		t.Fatalf("expected one probe, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("probe err = %v", results[0].Err)
	}
	if results[0].Latency <= 0 {
		t.Fatalf("expected positive latency, got %v", results[0].Latency)
	}
	if requests != 1 {
		t.Fatalf("expected one request, got %d", requests)
	}

	after := client.EndpointStatuses()
	if before[0].Failures != after[0].Failures || before[0].Successes != after[0].Successes || before[0].EWMA != after[0].EWMA {
		t.Fatalf("probe mutated health state: before=%#v after=%#v", before[0], after[0])
	}
}

func TestProbeEndpointsReportsHTTPAndSubsonicErrors(t *testing.T) {
	httpErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer httpErr.Close()
	apiErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{
			"status": "failed",
			"error":  map[string]any{"code": 40, "message": "bad auth"},
		}})
	}))
	defer apiErr.Close()

	client := NewClient(httpErr.Client())
	client.Configure(configForURL(httpErr.URL))
	results := client.ProbeEndpoints(context.Background(), []models.Endpoint{
		{Name: "http", URL: httpErr.URL, Enabled: true},
		{Name: "api", URL: apiErr.URL, Enabled: true},
	})
	if len(results) != 2 {
		t.Fatalf("expected two probes, got %d", len(results))
	}
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "HTTP 502") {
		t.Fatalf("HTTP error probe err = %v", results[0].Err)
	}
	if results[1].Err == nil || !strings.Contains(results[1].Err.Error(), "bad auth") {
		t.Fatalf("Subsonic error probe err = %v", results[1].Err)
	}
}

func TestProbeEndpointsRespectsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.Configure(configForURL(server.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := client.ProbeEndpoints(ctx, []models.Endpoint{{Name: "Test", URL: server.URL, Enabled: true}})
	if len(results) != 1 {
		t.Fatalf("expected one probe, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected canceled context error")
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
