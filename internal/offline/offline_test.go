package offline

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiongnemo/saki/internal/models"
)

func sampleSnapshot() ([]models.Artist, []models.Album, []models.Playlist) {
	song := models.Song{ID: "song-1", Title: "Track One", Artist: "Artist One", Album: "Album One", AlbumID: "album-1", Duration: 120, Suffix: "mp3", ContentType: "audio/mpeg"}
	artist := models.Artist{ID: "artist-1", Name: "Artist One", AlbumCount: 1}
	album := models.Album{ID: "album-1", Name: "Album One", Artist: "Artist One", ArtistID: "artist-1", SongCount: 1, Songs: []models.Song{song}}
	playlist := models.Playlist{ID: "playlist-1", Name: "Favorites", SongCount: 1, Entries: []models.Song{song}}
	return []models.Artist{artist}, []models.Album{album}, []models.Playlist{playlist}
}

func TestStoreSnapshotAndOfflineCatalog(t *testing.T) {
	store, err := Open(t.TempDir(), "user", "https://music.example", "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	artists, albums, playlists := sampleSnapshot()
	if err := store.ReplaceSnapshot(context.Background(), artists, albums, playlists); err != nil {
		t.Fatal(err)
	}
	catalog := OfflineCatalog{Store: store}
	gotArtists, err := catalog.GetArtists(context.Background())
	if err != nil || len(gotArtists) != 1 {
		t.Fatalf("artists = %#v, err=%v", gotArtists, err)
	}
	gotAlbum, err := catalog.GetAlbum(context.Background(), "album-1")
	if err != nil || len(gotAlbum.Songs) != 1 || gotAlbum.Songs[0].ID != "song-1" {
		t.Fatalf("album = %#v, err=%v", gotAlbum, err)
	}
	gotPlaylist, err := catalog.GetPlaylist(context.Background(), "playlist-1")
	if err != nil || len(gotPlaylist.Entries) != 1 {
		t.Fatalf("playlist = %#v, err=%v", gotPlaylist, err)
	}
	last, err := store.LastSuccess(context.Background())
	if err != nil || last.IsZero() {
		t.Fatalf("last success = %v, err=%v", last, err)
	}
}

func TestStoreRebindChangesCatalogNamespace(t *testing.T) {
	root := t.TempDir()
	store, err := Open(root, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	artists, albums, playlists := sampleSnapshot()
	if err := store.ReplaceSnapshot(context.Background(), artists, albums, playlists); err != nil {
		t.Fatal(err)
	}
	oldPath := store.Path()
	if err := store.Rebind(root, "user", "https://music.example", "fingerprint"); err != nil {
		t.Fatal(err)
	}
	if store.Path() == oldPath {
		t.Fatal("catalog path did not change")
	}
	if _, err := (OfflineCatalog{Store: store}).GetArtists(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRouterFallsBackToOfflineCatalog(t *testing.T) {
	store, err := Open(t.TempDir(), "user", "https://music.example", "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	artists, albums, playlists := sampleSnapshot()
	if err := store.ReplaceSnapshot(context.Background(), artists, albums, playlists); err != nil {
		t.Fatal(err)
	}
	onlineErr := &failingCatalog{}
	router := NewRouter(onlineErr, store, models.ConnectionModeAuto)
	got, err := router.GetArtists(context.Background())
	if err != nil || len(got) != 1 || !router.IsOffline() {
		t.Fatalf("artists=%#v err=%v mode=%s", got, err, router.Mode())
	}
}

type failingCatalog struct{}

func (failingCatalog) GetArtists(context.Context) ([]models.Artist, error) {
	return nil, io.ErrUnexpectedEOF
}
func (failingCatalog) GetArtist(context.Context, string) (models.Artist, error) {
	return models.Artist{}, io.ErrUnexpectedEOF
}
func (failingCatalog) GetAllAlbums(context.Context) ([]models.Album, error) {
	return nil, io.ErrUnexpectedEOF
}
func (failingCatalog) GetAlbum(context.Context, string) (models.Album, error) {
	return models.Album{}, io.ErrUnexpectedEOF
}
func (failingCatalog) GetPlaylists(context.Context) ([]models.Playlist, error) {
	return nil, io.ErrUnexpectedEOF
}
func (failingCatalog) GetPlaylist(context.Context, string) (models.Playlist, error) {
	return models.Playlist{}, io.ErrUnexpectedEOF
}
func (failingCatalog) GetSong(context.Context, string) (models.Song, error) {
	return models.Song{}, io.ErrUnexpectedEOF
}
func (failingCatalog) GetSimilarSongs(context.Context, string) ([]models.Song, error) {
	return nil, io.ErrUnexpectedEOF
}
func (failingCatalog) Search(context.Context, string, int) (models.SearchResult, error) {
	return models.SearchResult{}, io.ErrUnexpectedEOF
}

func TestDownloadManagerPersistsAtomicFile(t *testing.T) {
	store, err := Open(t.TempDir(), "user", "https://music.example", "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio bytes"))
	}))
	defer server.Close()
	manager := NewDownloadManager(store, filepath.Join(t.TempDir(), "offline"), 0, 2, func(ctx context.Context, _, _, _ string) (*http.Response, string, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if err != nil {
			return nil, "", err
		}
		resp, err := http.DefaultClient.Do(req)
		return resp, server.URL, err
	})
	if err := manager.Download(context.Background(), "song-1", models.DownloadVariantStream, true); err != nil {
		t.Fatal(err)
	}
	path, ok := manager.Path(context.Background(), "song-1", models.DownloadVariantStream)
	if !ok {
		t.Fatal("download path not available")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "audio bytes" {
		t.Fatalf("download = %q, err=%v", data, err)
	}
	if _, err := os.Stat(path + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial file still exists: %v", err)
	}
}

func TestDownloadManagerResumesPartialFile(t *testing.T) {
	store, err := Open(t.TempDir(), "user", "https://music.example", "fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const payload = "audio bytes"
	root := filepath.Join(t.TempDir(), "offline")
	manager := NewDownloadManager(store, root, 0, 1, func(ctx context.Context, _, _, rangeHeader string) (*http.Response, string, error) {
		status := http.StatusOK
		body := payload
		if rangeHeader == "bytes=5-" {
			status = http.StatusPartialContent
			body = payload[5:]
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, "test", nil
	})
	partial := manager.filePath("song-2", models.DownloadVariantStream) + ".partial"
	if err := os.MkdirAll(filepath.Dir(partial), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, []byte(payload[:5]), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manager.Download(context.Background(), "song-2", models.DownloadVariantStream, true); err != nil {
		t.Fatal(err)
	}
	path, ok := manager.Path(context.Background(), "song-2", models.DownloadVariantStream)
	if !ok {
		t.Fatal("resumed download path not available")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != payload {
		t.Fatalf("resumed data = %q, err=%v", data, err)
	}
}
