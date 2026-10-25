package offline

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/xiongnemo/saki/internal/models"
)

type Catalog interface {
	GetArtists(context.Context) ([]models.Artist, error)
	GetArtist(context.Context, string) (models.Artist, error)
	GetAllAlbums(context.Context) ([]models.Album, error)
	GetAlbum(context.Context, string) (models.Album, error)
	GetPlaylists(context.Context) ([]models.Playlist, error)
	GetPlaylist(context.Context, string) (models.Playlist, error)
	GetSong(context.Context, string) (models.Song, error)
	GetSimilarSongs(context.Context, string) ([]models.Song, error)
	Search(context.Context, string, int) (models.SearchResult, error)
}

type OfflineCatalog struct{ Store *Store }

func (c OfflineCatalog) GetArtists(ctx context.Context) ([]models.Artist, error) {
	rows, err := c.Store.DB().QueryContext(ctx, "SELECT id,name,album_count FROM artists ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var artists []models.Artist
	for rows.Next() {
		var artist models.Artist
		if err := rows.Scan(&artist.ID, &artist.Name, &artist.AlbumCount); err != nil {
			return nil, err
		}
		artists = append(artists, artist)
	}
	return artists, rows.Err()
}

func (c OfflineCatalog) GetArtist(ctx context.Context, id string) (models.Artist, error) {
	var artist models.Artist
	if err := c.Store.DB().QueryRowContext(ctx, "SELECT id,name,album_count FROM artists WHERE id=?", id).Scan(&artist.ID, &artist.Name, &artist.AlbumCount); err != nil {
		return artist, err
	}
	rows, err := c.Store.DB().QueryContext(ctx, "SELECT id,name,artist,artist_id,song_count,duration,year,cover_art FROM albums WHERE artist_id=? ORDER BY year,name COLLATE NOCASE", id)
	if err != nil {
		return artist, err
	}
	defer rows.Close()
	for rows.Next() {
		var album models.Album
		if err := rows.Scan(&album.ID, &album.Name, &album.Artist, &album.ArtistID, &album.SongCount, &album.Duration, &album.Year, &album.CoverArt); err != nil {
			return artist, err
		}
		artist.Albums = append(artist.Albums, album)
	}
	return artist, rows.Err()
}

func (c OfflineCatalog) GetAllAlbums(ctx context.Context) ([]models.Album, error) {
	rows, err := c.Store.DB().QueryContext(ctx, "SELECT id,name,artist,artist_id,song_count,duration,year,cover_art FROM albums ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAlbums(rows)
}

func scanAlbums(rows *sql.Rows) ([]models.Album, error) {
	var albums []models.Album
	for rows.Next() {
		var album models.Album
		if err := rows.Scan(&album.ID, &album.Name, &album.Artist, &album.ArtistID, &album.SongCount, &album.Duration, &album.Year, &album.CoverArt); err != nil {
			return nil, err
		}
		albums = append(albums, album)
	}
	return albums, rows.Err()
}

func (c OfflineCatalog) GetAlbum(ctx context.Context, id string) (models.Album, error) {
	var album models.Album
	if err := c.Store.DB().QueryRowContext(ctx, "SELECT id,name,artist,artist_id,song_count,duration,year,cover_art FROM albums WHERE id=?", id).Scan(&album.ID, &album.Name, &album.Artist, &album.ArtistID, &album.SongCount, &album.Duration, &album.Year, &album.CoverArt); err != nil {
		return album, err
	}
	songs, err := c.Store.SongIDs(ctx, "JOIN album_songs ON album_songs.song_id=songs.id WHERE album_songs.album_id=? ORDER BY album_songs.position", id)
	album.Songs = songs
	return album, err
}

func (c OfflineCatalog) GetPlaylists(ctx context.Context) ([]models.Playlist, error) {
	rows, err := c.Store.DB().QueryContext(ctx, "SELECT id,name,comment,owner,public,song_count,duration,cover_art,created FROM playlists ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var playlists []models.Playlist
	for rows.Next() {
		var playlist models.Playlist
		var public int
		if err := rows.Scan(&playlist.ID, &playlist.Name, &playlist.Comment, &playlist.Owner, &public, &playlist.SongCount, &playlist.Duration, &playlist.CoverArt, &playlist.Created); err != nil {
			return nil, err
		}
		playlist.Public = public != 0
		playlists = append(playlists, playlist)
	}
	return playlists, rows.Err()
}

func (c OfflineCatalog) GetPlaylist(ctx context.Context, id string) (models.Playlist, error) {
	var playlist models.Playlist
	var public int
	if err := c.Store.DB().QueryRowContext(ctx, "SELECT id,name,comment,owner,public,song_count,duration,cover_art,created FROM playlists WHERE id=?", id).Scan(&playlist.ID, &playlist.Name, &playlist.Comment, &playlist.Owner, &public, &playlist.SongCount, &playlist.Duration, &playlist.CoverArt, &playlist.Created); err != nil {
		return playlist, err
	}
	playlist.Public = public != 0
	songs, err := c.Store.SongIDs(ctx, "JOIN playlist_songs ON playlist_songs.song_id=songs.id WHERE playlist_songs.playlist_id=? ORDER BY playlist_songs.position", id)
	playlist.Entries = songs
	return playlist, err
}

func (c OfflineCatalog) GetSong(ctx context.Context, id string) (models.Song, error) {
	return c.Store.Song(ctx, id)
}

func (c OfflineCatalog) GetSimilarSongs(ctx context.Context, id string) ([]models.Song, error) {
	base, err := c.GetSong(ctx, id)
	if err != nil {
		return nil, err
	}
	return c.Store.SongIDs(ctx, "WHERE artist=? AND id<>? ORDER BY title COLLATE NOCASE LIMIT 50", base.Artist, id)
}

func (c OfflineCatalog) Search(ctx context.Context, query string, count int) (models.SearchResult, error) {
	query = "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	if count <= 0 {
		count = 100
	}
	var result models.SearchResult
	rows, err := c.Store.DB().QueryContext(ctx, "SELECT id,name,album_count FROM artists WHERE lower(name) LIKE ? ORDER BY name COLLATE NOCASE LIMIT ?", query, count)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var artist models.Artist
		if err := rows.Scan(&artist.ID, &artist.Name, &artist.AlbumCount); err != nil {
			rows.Close()
			return result, err
		}
		result.Artists = append(result.Artists, artist)
	}
	rows.Close()
	rows, err = c.Store.DB().QueryContext(ctx, "SELECT id,name,artist,artist_id,song_count,duration,year,cover_art FROM albums WHERE lower(name) LIKE ? OR lower(artist) LIKE ? ORDER BY name COLLATE NOCASE LIMIT ?", query, query, count)
	if err != nil {
		return result, err
	}
	result.Albums, err = scanAlbums(rows)
	rows.Close()
	if err != nil {
		return result, err
	}
	result.Songs, err = c.Store.SongIDs(ctx, "WHERE lower(title) LIKE ? OR lower(artist) LIKE ? OR lower(album) LIKE ? ORDER BY title COLLATE NOCASE LIMIT ?", query, query, query, count)
	return result, err
}

func (c OfflineCatalog) Empty() (bool, error) {
	var count int
	err := c.Store.DB().QueryRow("SELECT COUNT(*) FROM songs").Scan(&count)
	return count == 0, err
}

func missing(err error, kind, id string) error {
	if err == sql.ErrNoRows {
		return fmt.Errorf("offline %s %q not found", kind, id)
	}
	return err
}
