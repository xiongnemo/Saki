package subsonic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiongnemo/saki/internal/models"
)

func TestMetadataRequestFallsBackWhenActiveEndpointTimesOut(t *testing.T) {
	// Given
	var firstHits atomic.Int32
	var secondHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		<-r.Context().Done()
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		writeArtistsResponse(t, w, "artist-fallback")
	}))
	defer second.Close()
	client := NewClient(first.Client())
	client.Configure(configForEndpoints(first.URL, second.URL, 1, 30))
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	// When
	artists, err := client.GetArtists(ctx)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 1 || artists[0].ID != "artist-fallback" {
		t.Fatalf("unexpected artists: %#v", artists)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 {
		t.Fatalf("requests first=%d second=%d, want 1 each", firstHits.Load(), secondHits.Load())
	}
}

func TestOpenStreamFallsBackOnHeaderTimeoutAndPreservesBodyReads(t *testing.T) {
	// Given
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/stream" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(1200 * time.Millisecond)
		_, _ = w.Write([]byte("slow-audio"))
	}))
	defer second.Close()
	client := NewClient(first.Client())
	client.Configure(configForEndpoints(first.URL, second.URL, 1, 30))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// When
	resp, err := client.OpenStream(ctx, "song-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "slow-audio" {
		t.Fatalf("body = %q, want slow-audio", body)
	}
}

func TestOpenStreamCustomTransportHeaderTimeoutDoesNotCancelBodyReads(t *testing.T) {
	// Given
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       &slowContextBody{ctx: req.Context(), delay: 1200 * time.Millisecond, data: []byte("custom-body")},
			Request:    req,
		}, nil
	})
	client := NewClient(&http.Client{Transport: transport})
	client.Configure(configForEndpoints("https://one.example", "", 1, 30))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// When
	resp, err := client.OpenStream(ctx, "song-1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "custom-body" {
		t.Fatalf("body = %q, want custom-body", body)
	}
}

func TestGetCoverArtFallsBackAndUpdatesEndpointHealth(t *testing.T) {
	// Given
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/getCoverArt" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte("cover-bytes"))
	}))
	defer second.Close()
	client := NewClient(first.Client())
	client.Configure(configForEndpoints(first.URL, second.URL, 1, 30))

	// When
	data, err := client.GetCoverArt(context.Background(), "cover-1")

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cover-bytes" {
		t.Fatalf("cover art = %q, want cover-bytes", data)
	}
	statuses := client.EndpointStatuses()
	if statuses[0].Failures == 0 || statuses[1].Successes == 0 {
		t.Fatalf("fallback did not update health: %#v", statuses)
	}
}

func TestStartHealthChecksUsesUpdatedIntervalAfterConfigure(t *testing.T) {
	// Given
	pings := make(chan time.Time, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/ping.view" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		select {
		case pings <- time.Now():
		default:
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"subsonic-response": map[string]any{"status": "ok"}})
	}))
	defer server.Close()
	client := NewClient(server.Client())
	cfg := configForEndpoints(server.URL, "", 1, 30)
	client.Configure(cfg)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client.StartHealthChecks(ctx)
	select {
	case <-pings:
	case <-time.After(time.Second):
		t.Fatal("initial health check did not run")
	}

	// When
	cfg.Settings.HealthCheckIntervalSeconds = 1
	client.Configure(cfg)

	// Then
	select {
	case <-pings:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("health check interval did not update after Configure")
	}
}

func TestEndpointStatusesExposeCircuitStateAndOpenEndpointsAreSkipped(t *testing.T) {
	// Given
	var firstHits atomic.Int32
	var secondHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		writeArtistsResponse(t, w, "artist-first")
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		writeArtistsResponse(t, w, "artist-second")
	}))
	defer second.Close()
	client := NewClient(first.Client())
	client.Configure(configForEndpoints(first.URL, second.URL, 1, 30))
	client.markEndpointResult(first.URL, 0, errors.New("dial timeout"))
	client.markEndpointResult(first.URL, 0, errors.New("dial timeout"))
	client.mu.Lock()
	client.active = 0
	client.mu.Unlock()

	// When
	artists, err := client.GetArtists(context.Background())

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 1 || artists[0].ID != "artist-second" {
		t.Fatalf("unexpected artists: %#v", artists)
	}
	if firstHits.Load() != 0 || secondHits.Load() != 1 {
		t.Fatalf("requests first=%d second=%d, want first skipped and second used", firstHits.Load(), secondHits.Load())
	}
	statuses := client.EndpointStatuses()
	if statuses[0].CircuitState != "open" {
		t.Fatalf("first circuit state = %q, want open", statuses[0].CircuitState)
	}
	if statuses[0].LastError == "" || !strings.Contains(statuses[0].LastFailoverReason, "dial timeout") {
		t.Fatalf("status missing failure detail: %#v", statuses[0])
	}

	// Given all endpoints are open, real requests are allowed to try one best-effort.
	client.markEndpointResult(second.URL, 0, errors.New("dial timeout"))
	client.markEndpointResult(second.URL, 0, errors.New("dial timeout"))
	firstHits.Store(0)
	secondHits.Store(0)
	client.mu.Lock()
	client.active = 0
	client.mu.Unlock()

	// When
	artists, err = client.GetArtists(context.Background())

	// Then
	if err != nil {
		t.Fatal(err)
	}
	if len(artists) != 1 || artists[0].ID != "artist-first" {
		t.Fatalf("unexpected best-effort artists: %#v", artists)
	}
	if firstHits.Load() != 1 {
		t.Fatalf("best-effort did not try open active endpoint, first hits=%d", firstHits.Load())
	}
}

func writeArtistsResponse(t *testing.T, w http.ResponseWriter, artistID string) {
	t.Helper()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"subsonic-response": map[string]any{
			"status": "ok",
			"artists": map[string]any{
				"index": []any{map[string]any{
					"name": "A",
					"artist": []any{map[string]any{
						"id": artistID, "name": "Artist", "albumCount": 1,
					}},
				}},
			},
		},
	})
}

func configForEndpoints(firstURL string, secondURL string, timeoutSeconds int, intervalSeconds int) models.Config {
	endpoints := []models.Endpoint{{Name: "first", URL: firstURL, Enabled: true}}
	if secondURL != "" {
		endpoints = append(endpoints, models.Endpoint{Name: "second", URL: secondURL, Enabled: true})
	}
	return models.Config{
		Account: models.Account{
			Username:  "nemo",
			Password:  "secret",
			Endpoints: endpoints,
		},
		Settings: models.Settings{
			HealthCheckIntervalSeconds: intervalSeconds,
			EndpointTimeoutSeconds:     timeoutSeconds,
			EndpointSwitchThreshold:    0.30,
		},
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type slowContextBody struct {
	ctx   context.Context
	delay time.Duration
	data  []byte
	done  bool
}

func (b *slowContextBody) Close() error {
	return nil
}

func (b *slowContextBody) Read(p []byte) (int, error) {
	if b.done {
		return 0, io.EOF
	}
	select {
	case <-time.After(b.delay):
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	}
	n := copy(p, b.data)
	b.done = true
	return n, nil
}
