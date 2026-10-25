package offline

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/xiongnemo/saki/internal/models"
)

type Progress struct {
	Completed int
	Total     int
	Stage     string
}
type ProgressFunc func(Progress)

func Sync(ctx context.Context, remote Catalog, store *Store, progress ProgressFunc) error {
	if remote == nil || store == nil {
		return fmt.Errorf("offline sync requires online catalog and store")
	}
	artists, err := remote.GetArtists(ctx)
	if err != nil {
		return fmt.Errorf("sync artists: %w", err)
	}
	albums, err := remote.GetAllAlbums(ctx)
	if err != nil {
		return fmt.Errorf("sync albums: %w", err)
	}
	playlists, err := remote.GetPlaylists(ctx)
	if err != nil {
		return fmt.Errorf("sync playlists: %w", err)
	}
	total := len(albums) + len(playlists)
	var completed atomic.Int32
	if progress != nil {
		progress(Progress{Total: total, Stage: "loading details"})
	}
	detailedAlbums := make([]models.Album, len(albums))
	detailedPlaylists := make([]models.Playlist, len(playlists))
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := 2
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				var detailErr error
				if index < len(albums) {
					detailedAlbums[index], detailErr = remote.GetAlbum(ctx, albums[index].ID)
				} else {
					p := index - len(albums)
					detailedPlaylists[p], detailErr = remote.GetPlaylist(ctx, playlists[p].ID)
				}
				if detailErr != nil {
					continue
				}
				current := int(completed.Add(1))
				if progress != nil {
					progress(Progress{Completed: current, Total: total, Stage: "loading details"})
				}
			}
		}()
	}
	for i := 0; i < total; i++ {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	for i, album := range detailedAlbums {
		if album.ID == "" {
			return fmt.Errorf("sync album %q failed", albums[i].ID)
		}
	}
	for i, playlist := range detailedPlaylists {
		if playlist.ID == "" {
			return fmt.Errorf("sync playlist %q failed", playlists[i].ID)
		}
	}
	if err := store.ReplaceSnapshot(ctx, artists, detailedAlbums, detailedPlaylists); err != nil {
		return fmt.Errorf("commit catalog: %w", err)
	}
	if progress != nil {
		progress(Progress{Completed: total, Total: total, Stage: "complete"})
	}
	return nil
}
