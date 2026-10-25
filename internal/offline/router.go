package offline

import (
	"context"
	"errors"
	"sync"

	"github.com/xiongnemo/saki/internal/models"
)

type Mode string

const (
	ModeOnline  Mode = "online"
	ModeOffline Mode = "offline"
)

type Router struct {
	online  Catalog
	offline Catalog
	store   *Store

	mu   sync.RWMutex
	mode Mode
	auto bool
}

func NewRouter(online Catalog, store *Store, configuredMode string) *Router {
	mode := ModeOnline
	auto := true
	if configuredMode == models.ConnectionModeOffline {
		mode = ModeOffline
		auto = false
	}
	return &Router{online: online, offline: OfflineCatalog{Store: store}, store: store, mode: mode, auto: auto}
}

func (r *Router) Mode() Mode      { r.mu.RLock(); defer r.mu.RUnlock(); return r.mode }
func (r *Router) IsOffline() bool { return r.Mode() == ModeOffline }
func (r *Router) SetOffline()     { r.mu.Lock(); r.mode = ModeOffline; r.mu.Unlock() }
func (r *Router) SetOnline()      { r.mu.Lock(); r.mode = ModeOnline; r.auto = true; r.mu.Unlock() }
func (r *Router) SetConfiguredMode(configuredMode string) {
	r.mu.Lock()
	if configuredMode == models.ConnectionModeOffline {
		r.mode = ModeOffline
		r.auto = false
	} else {
		r.mode = ModeOnline
		r.auto = true
	}
	r.mu.Unlock()
}
func (r *Router) SetStore(store *Store) {
	r.mu.Lock()
	r.store = store
	r.offline = OfflineCatalog{Store: store}
	r.mu.Unlock()
}
func (r *Router) Store() *Store {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.store
}

func (r *Router) offlineCatalog() OfflineCatalog { return OfflineCatalog{Store: r.Store()} }

func (r *Router) HasOfflineData(ctx context.Context) bool {
	store := r.Store()
	if store == nil {
		return false
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM artists) + (SELECT COUNT(*) FROM albums) + (SELECT COUNT(*) FROM songs) + (SELECT COUNT(*) FROM playlists)").Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func run[T any](r *Router, ctx context.Context, online func() (T, error), offline func() (T, error)) (T, error) {
	r.mu.RLock()
	mode, auto := r.mode, r.auto
	r.mu.RUnlock()
	if mode == ModeOffline {
		return offline()
	}
	value, err := online()
	if err == nil {
		return value, nil
	}
	if auto && r.HasOfflineData(ctx) {
		r.SetOffline()
		return offline()
	}
	return value, err
}

func (r *Router) GetArtists(ctx context.Context) ([]models.Artist, error) {
	return run(r, ctx, func() ([]models.Artist, error) { return r.online.GetArtists(ctx) }, func() ([]models.Artist, error) { return r.offlineCatalog().GetArtists(ctx) })
}
func (r *Router) GetArtist(ctx context.Context, id string) (models.Artist, error) {
	return run(r, ctx, func() (models.Artist, error) { return r.online.GetArtist(ctx, id) }, func() (models.Artist, error) { return r.offlineCatalog().GetArtist(ctx, id) })
}
func (r *Router) GetAllAlbums(ctx context.Context) ([]models.Album, error) {
	return run(r, ctx, func() ([]models.Album, error) { return r.online.GetAllAlbums(ctx) }, func() ([]models.Album, error) { return r.offlineCatalog().GetAllAlbums(ctx) })
}
func (r *Router) GetAlbum(ctx context.Context, id string) (models.Album, error) {
	return run(r, ctx, func() (models.Album, error) { return r.online.GetAlbum(ctx, id) }, func() (models.Album, error) { return r.offlineCatalog().GetAlbum(ctx, id) })
}
func (r *Router) GetPlaylists(ctx context.Context) ([]models.Playlist, error) {
	return run(r, ctx, func() ([]models.Playlist, error) { return r.online.GetPlaylists(ctx) }, func() ([]models.Playlist, error) { return r.offlineCatalog().GetPlaylists(ctx) })
}
func (r *Router) GetPlaylist(ctx context.Context, id string) (models.Playlist, error) {
	return run(r, ctx, func() (models.Playlist, error) { return r.online.GetPlaylist(ctx, id) }, func() (models.Playlist, error) { return r.offlineCatalog().GetPlaylist(ctx, id) })
}
func (r *Router) GetSong(ctx context.Context, id string) (models.Song, error) {
	return run(r, ctx, func() (models.Song, error) { return r.online.GetSong(ctx, id) }, func() (models.Song, error) { return r.offlineCatalog().GetSong(ctx, id) })
}
func (r *Router) GetSimilarSongs(ctx context.Context, id string) ([]models.Song, error) {
	return run(r, ctx, func() ([]models.Song, error) { return r.online.GetSimilarSongs(ctx, id) }, func() ([]models.Song, error) { return r.offlineCatalog().GetSimilarSongs(ctx, id) })
}
func (r *Router) Search(ctx context.Context, query string, count int) (models.SearchResult, error) {
	return run(r, ctx, func() (models.SearchResult, error) { return r.online.Search(ctx, query, count) }, func() (models.SearchResult, error) { return r.offlineCatalog().Search(ctx, query, count) })
}

func (r *Router) RetryOnline(ctx context.Context) error {
	if r.online == nil {
		return errors.New("online catalog is unavailable")
	}
	if _, err := r.online.GetArtists(ctx); err != nil {
		return err
	}
	r.SetOnline()
	return nil
}
