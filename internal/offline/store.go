package offline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/xiongnemo/saki/internal/cachepaths"
	"github.com/xiongnemo/saki/internal/models"
)

type Store struct {
	db   *sql.DB
	path string
	root string
}

type Download struct {
	SongID         string
	Variant        string
	Status         string
	Pinned         bool
	Path           string
	Bytes          int64
	ContentType    string
	SHA256         string
	SourceEndpoint string
	Error          string
	Attempts       int
	UpdatedAt      time.Time
	CompletedAt    time.Time
}

func Open(root, account, endpoint, fingerprint string) (*Store, error) {
	scope := cachepaths.NewScope(root, account, endpoint, fingerprint)
	if err := os.MkdirAll(scope.CatalogDir(), 0o700); err != nil {
		return nil, err
	}
	db, err := openDB(scope.CatalogPath())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, path: scope.CatalogPath(), root: scope.OfflineDir()}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) DownloadRoot() string { return s.root }

// Rebind switches the catalog to the namespace selected by the current
// account, endpoint, fingerprint, and cache directory. Existing namespaces
// remain on disk and can be selected again if the user restores those values.
func (s *Store) Rebind(root, account, endpoint, fingerprint string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("offline store is closed")
	}
	scope := cachepaths.NewScope(root, account, endpoint, fingerprint)
	if scope.CatalogPath() == s.path {
		s.root = scope.OfflineDir()
		return nil
	}
	if err := os.MkdirAll(scope.CatalogDir(), 0o700); err != nil {
		return err
	}
	db, err := openDB(scope.CatalogPath())
	if err != nil {
		return err
	}
	rebound := &Store{db: db, path: scope.CatalogPath(), root: scope.OfflineDir()}
	if err := rebound.migrate(context.Background()); err != nil {
		_ = db.Close()
		return err
	}
	old := s.db
	s.db = db
	s.path = rebound.path
	s.root = rebound.root
	return old.Close()
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS artists (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  album_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS albums (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  artist TEXT NOT NULL,
  artist_id TEXT NOT NULL,
  song_count INTEGER NOT NULL DEFAULT 0,
  duration INTEGER NOT NULL DEFAULT 0,
  year INTEGER NOT NULL DEFAULT 0,
  cover_art TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS songs (
  id TEXT PRIMARY KEY,
  parent TEXT NOT NULL DEFAULT '',
  track INTEGER NOT NULL DEFAULT 0,
  title TEXT NOT NULL,
  artist TEXT NOT NULL DEFAULT '',
  album TEXT NOT NULL DEFAULT '',
  album_id TEXT NOT NULL DEFAULT '',
  duration INTEGER NOT NULL DEFAULT 0,
  cover_art TEXT NOT NULL DEFAULT '',
  suffix TEXT NOT NULL DEFAULT '',
  content_type TEXT NOT NULL DEFAULT '',
  transcoded_suffix TEXT NOT NULL DEFAULT '',
  transcoded_content_type TEXT NOT NULL DEFAULT '',
  bit_rate INTEGER NOT NULL DEFAULT 0,
  bit_depth INTEGER NOT NULL DEFAULT 0,
  sampling_rate INTEGER NOT NULL DEFAULT 0,
  channel_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS album_songs (
  album_id TEXT NOT NULL,
  song_id TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (album_id, position),
  FOREIGN KEY (album_id) REFERENCES albums(id) ON DELETE CASCADE,
  FOREIGN KEY (song_id) REFERENCES songs(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS playlists (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  comment TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL DEFAULT '',
  public INTEGER NOT NULL DEFAULT 0,
  song_count INTEGER NOT NULL DEFAULT 0,
  duration INTEGER NOT NULL DEFAULT 0,
  cover_art TEXT NOT NULL DEFAULT '',
  created TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS playlist_songs (
  playlist_id TEXT NOT NULL,
  song_id TEXT NOT NULL,
  position INTEGER NOT NULL,
  PRIMARY KEY (playlist_id, position),
  FOREIGN KEY (playlist_id) REFERENCES playlists(id) ON DELETE CASCADE,
  FOREIGN KEY (song_id) REFERENCES songs(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS downloads (
  song_id TEXT NOT NULL,
  variant TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued',
  pinned INTEGER NOT NULL DEFAULT 0,
  path TEXT NOT NULL DEFAULT '',
  bytes INTEGER NOT NULL DEFAULT 0,
  content_type TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  source_endpoint TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT '',
  completed_at TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (song_id, variant)
);
CREATE TABLE IF NOT EXISTS sync_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
PRAGMA user_version = 1;
`)
	return err
}

func (s *Store) ReplaceSnapshot(ctx context.Context, artists []models.Artist, albums []models.Album, playlists []models.Playlist) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error { _ = tx.Rollback(); return err }
	for _, query := range []string{
		"DELETE FROM album_songs", "DELETE FROM playlist_songs", "DELETE FROM artists", "DELETE FROM albums", "DELETE FROM songs", "DELETE FROM playlists",
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return rollback(err)
		}
	}
	artistStmt, err := tx.PrepareContext(ctx, "INSERT INTO artists(id,name,album_count) VALUES(?,?,?)")
	if err != nil {
		return rollback(err)
	}
	for _, artist := range artists {
		if _, err := artistStmt.ExecContext(ctx, artist.ID, artist.Name, artist.AlbumCount); err != nil {
			_ = artistStmt.Close()
			return rollback(err)
		}
	}
	_ = artistStmt.Close()
	if err := insertAlbums(ctx, tx, albums); err != nil {
		return rollback(err)
	}
	if err := insertPlaylists(ctx, tx, playlists); err != nil {
		return rollback(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_state(key,value) VALUES('last_success',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return rollback(err)
	}
	return tx.Commit()
}

func insertAlbums(ctx context.Context, tx *sql.Tx, albums []models.Album) error {
	for _, album := range albums {
		if _, err := tx.ExecContext(ctx, `INSERT INTO albums(id,name,artist,artist_id,song_count,duration,year,cover_art) VALUES(?,?,?,?,?,?,?,?)`, album.ID, album.Name, album.Artist, album.ArtistID, album.SongCount, album.Duration, album.Year, album.CoverArt); err != nil {
			return err
		}
		for position, song := range album.Songs {
			if err := insertSong(ctx, tx, song); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO album_songs(album_id,song_id,position) VALUES(?,?,?)`, album.ID, song.ID, position); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertPlaylists(ctx context.Context, tx *sql.Tx, playlists []models.Playlist) error {
	for _, playlist := range playlists {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlists(id,name,comment,owner,public,song_count,duration,cover_art,created) VALUES(?,?,?,?,?,?,?,?,?)`, playlist.ID, playlist.Name, playlist.Comment, playlist.Owner, playlist.Public, playlist.SongCount, playlist.Duration, playlist.CoverArt, playlist.Created); err != nil {
			return err
		}
		for position, song := range playlist.Entries {
			if err := insertSong(ctx, tx, song); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_songs(playlist_id,song_id,position) VALUES(?,?,?)`, playlist.ID, song.ID, position); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertSong(ctx context.Context, tx *sql.Tx, song models.Song) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO songs(id,parent,track,title,artist,album,album_id,duration,cover_art,suffix,content_type,transcoded_suffix,transcoded_content_type,bit_rate,bit_depth,sampling_rate,channel_count) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET parent=excluded.parent,track=excluded.track,title=excluded.title,artist=excluded.artist,album=excluded.album,album_id=excluded.album_id,duration=excluded.duration,cover_art=excluded.cover_art,suffix=excluded.suffix,content_type=excluded.content_type,transcoded_suffix=excluded.transcoded_suffix,transcoded_content_type=excluded.transcoded_content_type,bit_rate=excluded.bit_rate,bit_depth=excluded.bit_depth,sampling_rate=excluded.sampling_rate,channel_count=excluded.channel_count`, song.ID, song.Parent, song.Track, song.Title, song.Artist, song.Album, song.AlbumID, song.Duration, song.CoverArt, song.Suffix, song.ContentType, song.TranscodedSuffix, song.TranscodedContentType, song.BitRateKbps, song.BitDepth, song.SamplingRate, song.ChannelCount)
	return err
}

func (s *Store) LastSuccess(ctx context.Context) (time.Time, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM sync_state WHERE key='last_success'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse last sync time: %w", err)
	}
	return parsed, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func scanSong(scanner interface{ Scan(...any) error }) (models.Song, error) {
	var song models.Song
	err := scanner.Scan(&song.ID, &song.Parent, &song.Track, &song.Title, &song.Artist, &song.Album, &song.AlbumID, &song.Duration, &song.CoverArt, &song.Suffix, &song.ContentType, &song.TranscodedSuffix, &song.TranscodedContentType, &song.BitRateKbps, &song.BitDepth, &song.SamplingRate, &song.ChannelCount)
	return song, err
}

const songColumns = "id,parent,track,title,artist,album,album_id,duration,cover_art,suffix,content_type,transcoded_suffix,transcoded_content_type,bit_rate,bit_depth,sampling_rate,channel_count"
const songColumnsQualified = "songs.id,songs.parent,songs.track,songs.title,songs.artist,songs.album,songs.album_id,songs.duration,songs.cover_art,songs.suffix,songs.content_type,songs.transcoded_suffix,songs.transcoded_content_type,songs.bit_rate,songs.bit_depth,songs.sampling_rate,songs.channel_count"

func (s *Store) Song(ctx context.Context, id string) (models.Song, error) {
	return scanSong(s.db.QueryRowContext(ctx, "SELECT "+songColumns+" FROM songs WHERE id=?", id))
}

func (s *Store) UpsertDownload(ctx context.Context, download Download) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO downloads(song_id,variant,status,pinned,path,bytes,content_type,sha256,source_endpoint,error,attempts,updated_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(song_id,variant) DO UPDATE SET status=excluded.status,pinned=excluded.pinned,path=excluded.path,bytes=excluded.bytes,content_type=excluded.content_type,sha256=excluded.sha256,source_endpoint=excluded.source_endpoint,error=excluded.error,attempts=excluded.attempts,updated_at=excluded.updated_at,completed_at=excluded.completed_at`, download.SongID, download.Variant, download.Status, boolInt(download.Pinned), download.Path, download.Bytes, download.ContentType, download.SHA256, download.SourceEndpoint, download.Error, download.Attempts, formatTime(download.UpdatedAt), formatTime(download.CompletedAt))
	return err
}

func (s *Store) Download(ctx context.Context, songID, variant string) (Download, error) {
	var d Download
	var pinned int
	var updated, completed string
	err := s.db.QueryRowContext(ctx, "SELECT song_id,variant,status,pinned,path,bytes,content_type,sha256,source_endpoint,error,attempts,updated_at,completed_at FROM downloads WHERE song_id=? AND variant=?", songID, variant).Scan(&d.SongID, &d.Variant, &d.Status, &pinned, &d.Path, &d.Bytes, &d.ContentType, &d.SHA256, &d.SourceEndpoint, &d.Error, &d.Attempts, &updated, &completed)
	d.Pinned = pinned != 0
	d.UpdatedAt = parseTime(updated)
	d.CompletedAt = parseTime(completed)
	return d, err
}

func (s *Store) CompletedPath(ctx context.Context, songID, variant string) (string, bool) {
	d, err := s.Download(ctx, songID, variant)
	if err != nil || d.Status != "complete" || d.Path == "" {
		return "", false
	}
	if info, err := os.Stat(d.Path); err == nil && info.Size() > 0 {
		return d.Path, true
	}
	return "", false
}

func (s *Store) RemoveDownload(ctx context.Context, songID, variant string) error {
	d, err := s.Download(ctx, songID, variant)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if d.Path != "" {
		_ = os.Remove(d.Path)
		_ = os.Remove(d.Path + ".partial")
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM downloads WHERE song_id=? AND variant=?", songID, variant)
	return err
}

func (s *Store) DownloadedBytes(ctx context.Context) (int64, error) {
	var bytes int64
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(bytes),0) FROM downloads WHERE status='complete'").Scan(&bytes)
	return bytes, err
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, _ := time.Parse(time.RFC3339, value)
	return parsed
}

func (s *Store) SongIDs(ctx context.Context, query string, args ...any) ([]models.Song, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+songColumnsQualified+" FROM songs "+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var songs []models.Song
	for rows.Next() {
		song, err := scanSong(rows)
		if err != nil {
			return nil, err
		}
		songs = append(songs, song)
	}
	return songs, rows.Err()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
