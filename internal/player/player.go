package player

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xiongnemo/saki/internal/audio"
	"github.com/xiongnemo/saki/internal/mediaintegration"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/subsonic"
)

type Service struct {
	ctx    context.Context
	cancel context.CancelFunc

	client    *subsonic.Client
	audio     audio.Backend
	media     mediaintegration.MediaIntegration
	streamURL func(string) string
	source    func(context.Context, string) (string, error)
	cacheRoot string

	mu               sync.RWMutex
	playlist         models.Playlist
	originalQueue    []models.Song
	currentTrack     *models.Song
	loadedTrackID    string
	repeatStatus     models.RepeatStatus
	shuffled         bool
	volume           float64
	buffering        bool
	bufferedSec      float64
	bufferedBytes    int64
	totalBytes       int64
	bufferedPct      float64
	bufferPctKnown   bool
	lastError        string
	lastProgressEmit time.Time

	updates chan models.CurrentState
}

var errCachedSourceUnavailable = errors.New("cached source is not available")

func New(client *subsonic.Client, backend audio.Backend, media mediaintegration.MediaIntegration) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	service := &Service{
		ctx:       ctx,
		cancel:    cancel,
		client:    client,
		audio:     backend,
		media:     media,
		streamURL: client.SongURI,
		source: func(_ context.Context, id string) (string, error) {
			return client.SongURI(id), nil
		},
		volume:  1,
		updates: make(chan models.CurrentState, 32),
	}
	go service.consumeAudioEvents()
	go service.consumeMediaCommands()
	return service
}

func (s *Service) SetStreamURLResolver(resolver func(string) string) {
	if resolver == nil {
		resolver = s.client.SongURI
	}
	s.mu.Lock()
	s.streamURL = resolver
	s.source = func(_ context.Context, id string) (string, error) {
		return resolver(id), nil
	}
	s.mu.Unlock()
}

func (s *Service) SetStreamSourceResolver(resolver func(context.Context, string) (string, error)) {
	if resolver == nil {
		resolver = func(_ context.Context, id string) (string, error) {
			return s.client.SongURI(id), nil
		}
	}
	s.mu.Lock()
	s.source = resolver
	s.mu.Unlock()
}

func (s *Service) SetCacheRoot(cacheRoot string) {
	s.mu.Lock()
	s.cacheRoot = cacheRoot
	s.mu.Unlock()
}

func (s *Service) Updates() <-chan models.CurrentState {
	return s.updates
}

func (s *Service) State() models.CurrentState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stateLocked()
}

func (s *Service) PlayPause() {
	if s.audio.IsPlaying() {
		s.Pause()
		return
	}
	s.Play()
}

func (s *Service) Play() {
	s.mu.Lock()
	needsLoad := s.currentTrack == nil && len(s.playlist.Entries) > 0
	if needsLoad {
		track := s.playlist.Entries[0]
		s.currentTrack = &track
	}
	trackID := ""
	if s.currentTrack != nil {
		trackID = s.currentTrack.ID
	}
	loaded := trackID != "" && trackID == s.loadedTrackID
	s.mu.Unlock()

	if trackID == "" {
		return
	}
	if !loaded {
		if err := s.loadCurrentAndPlay(); err != nil {
			s.emit()
		}
		return
	}
	_ = s.audio.Play()
	_ = s.media.SetPlaybackState(models.PlaybackPlaying)
	s.emit()
}

func (s *Service) Pause() {
	_ = s.audio.Pause()
	_ = s.media.SetPlaybackState(models.PlaybackPaused)
	s.emit()
}

func (s *Service) Stop() {
	_ = s.audio.Stop()
	_ = s.media.SetPlaybackState(models.PlaybackStopped)
	s.emit()
}

func (s *Service) Next() {
	s.mu.Lock()
	nextIndex, ok := s.nextIndexLocked(false)
	if !ok {
		s.mu.Unlock()
		return
	}
	track := s.playlist.Entries[nextIndex]
	s.currentTrack = &track
	s.loadedTrackID = ""
	s.mu.Unlock()
	_ = s.loadCurrentAndPlay()
}

func (s *Service) Previous() {
	s.mu.Lock()
	if s.currentTrack == nil || len(s.playlist.Entries) == 0 {
		s.mu.Unlock()
		return
	}
	index := s.currentIndexLocked()
	if index <= 0 {
		if s.repeatStatus != models.RepeatAll {
			s.mu.Unlock()
			return
		}
		index = len(s.playlist.Entries) - 1
	} else {
		index--
	}
	track := s.playlist.Entries[index]
	s.currentTrack = &track
	s.loadedTrackID = ""
	s.mu.Unlock()
	_ = s.loadCurrentAndPlay()
}

func (s *Service) SkipTo(index int) {
	s.mu.Lock()
	if index < 0 || index >= len(s.playlist.Entries) {
		s.mu.Unlock()
		return
	}
	track := s.playlist.Entries[index]
	s.currentTrack = &track
	s.loadedTrackID = ""
	s.mu.Unlock()
	_ = s.loadCurrentAndPlay()
}

func (s *Service) Seek(seconds float64, relative bool) {
	state := s.State()
	duration := s.audio.Duration()
	if duration <= 0 {
		if state.CurrentTrack != nil {
			duration = float64(state.CurrentTrack.Duration)
		}
	}
	if duration <= 0 {
		return
	}

	target := seconds
	if relative {
		target = s.audio.Position() + seconds
	}
	if target < 0 {
		target = 0
	}
	if target > duration {
		target = duration
	}
	if err := s.audio.Seek(target / duration); err != nil {
		if errors.Is(err, audio.ErrStreamSeekRequiresCache) {
			wasPlaying := state.Playing
			if reloadErr := s.reloadCachedCurrentForSeek(target, duration, wasPlaying); reloadErr == nil {
				s.emit()
				return
			} else if !errors.Is(reloadErr, errCachedSourceUnavailable) {
				err = reloadErr
			}
		}
		s.setLastError(err.Error())
	}
	s.emit()
}

func (s *Service) reloadCachedCurrentForSeek(target, duration float64, resume bool) error {
	s.mu.RLock()
	if s.currentTrack == nil {
		s.mu.RUnlock()
		return errCachedSourceUnavailable
	}
	track := *s.currentTrack
	source := s.source
	s.mu.RUnlock()

	if source == nil {
		return errCachedSourceUnavailable
	}
	playSource, err := source(s.ctx, track.ID)
	if err != nil {
		return err
	}
	if !isLocalAudioSource(playSource) {
		return errCachedSourceUnavailable
	}

	request := audio.LoadRequest{
		URI:             playSource,
		TrackID:         track.ID,
		DurationSeconds: float64(track.Duration),
	}
	if err := s.audio.Load(s.ctx, request); err != nil {
		return err
	}
	if actualDuration := s.audio.Duration(); actualDuration > 0 {
		duration = actualDuration
	}
	if duration <= 0 {
		return errors.New("cannot seek because track duration is unknown")
	}
	if target < 0 {
		target = 0
	}
	if target > duration {
		target = duration
	}
	if err := s.audio.Seek(target / duration); err != nil {
		return err
	}
	if resume {
		if err := s.audio.Play(); err != nil {
			return err
		}
		_ = s.media.SetPlaybackState(models.PlaybackPlaying)
	} else {
		_ = s.audio.Pause()
		_ = s.media.SetPlaybackState(models.PlaybackPaused)
	}

	s.mu.Lock()
	s.buffering = false
	s.bufferedSec = duration
	s.bufferedBytes = 1
	s.totalBytes = 1
	s.bufferedPct = 100
	s.bufferPctKnown = true
	s.lastError = ""
	s.loadedTrackID = track.ID
	s.currentTrack = &track
	s.mu.Unlock()
	return nil
}

func (s *Service) SetVolume(volume int, relative bool) {
	current := s.State().Volume
	next := float64(volume) / 100
	if relative {
		next = current + float64(volume)/100
	}
	if next < 0 {
		next = 0
	}
	if next > 1 {
		next = 1
	}
	_ = s.audio.SetVolume(next)
	s.mu.Lock()
	s.volume = next
	s.mu.Unlock()
	s.emit()
}

func (s *Service) ToggleRepeat() {
	s.mu.Lock()
	s.repeatStatus = s.repeatStatus.Next()
	s.mu.Unlock()
	s.emit()
}

func (s *Service) Shuffle() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.playlist.Entries) == 0 {
		return
	}
	if s.shuffled {
		s.playlist.Entries = slices.Clone(s.originalQueue)
		s.shuffled = false
		s.emitLocked()
		return
	}

	currentID := ""
	if s.currentTrack != nil {
		currentID = s.currentTrack.ID
	}
	rand.Shuffle(len(s.playlist.Entries), func(i, j int) {
		s.playlist.Entries[i], s.playlist.Entries[j] = s.playlist.Entries[j], s.playlist.Entries[i]
	})
	if currentID != "" {
		index := s.currentIndexLocked()
		if index > 0 {
			s.playlist.Entries[0], s.playlist.Entries[index] = s.playlist.Entries[index], s.playlist.Entries[0]
		}
	}
	s.shuffled = true
	s.emitLocked()
}

func (s *Service) AddToCurrentPlaylist(song models.Song) {
	s.mu.Lock()
	s.playlist.Entries = append(s.playlist.Entries, song)
	s.originalQueue = append(s.originalQueue, song)
	s.mu.Unlock()
	s.emit()
}

func (s *Service) PlayAlbum(ctx context.Context, albumID string, track int) error {
	album, err := s.client.GetAlbum(ctx, albumID)
	if err != nil {
		return err
	}
	playlist := models.Playlist{
		Name:      album.Name,
		Comment:   "by " + album.Artist,
		Owner:     s.client.ActiveAccount().Username,
		SongCount: len(album.Songs),
		Duration:  totalDuration(album.Songs),
		Entries:   album.Songs,
	}
	return s.loadPlaylistAt(playlist, track)
}

func (s *Service) PlayPlaylist(ctx context.Context, playlistID string, track int) error {
	playlist, err := s.client.GetPlaylist(ctx, playlistID)
	if err != nil {
		return err
	}
	return s.loadPlaylistAt(playlist, track)
}

func (s *Service) PlayRadio(ctx context.Context, songID string) error {
	songs, err := s.client.GetSimilarSongs(ctx, songID)
	if err != nil {
		return err
	}
	baseSong, err := s.client.GetSong(ctx, songID)
	if err != nil {
		return err
	}
	songs = append([]models.Song{baseSong}, songs...)
	playlist := models.Playlist{
		Name:      fmt.Sprintf("Radio based on %s", baseSong.Title),
		Comment:   fmt.Sprintf("by %s from %s", baseSong.Artist, baseSong.Album),
		Owner:     s.client.ActiveAccount().Username,
		SongCount: len(songs),
		Duration:  totalDuration(songs),
		Entries:   songs,
	}
	return s.loadPlaylistAt(playlist, 0)
}

func (s *Service) Close() error {
	s.cancel()
	if s.media != nil {
		_ = s.media.Close()
	}
	return s.audio.Close()
}

func (s *Service) loadPlaylistAt(playlist models.Playlist, track int) error {
	if len(playlist.Entries) == 0 {
		return nil
	}
	if track < 0 || track >= len(playlist.Entries) {
		track = 0
	}
	for i := range playlist.Entries {
		playlist.Entries[i].Image = s.client.CoverArtURI(playlist.Entries[i].AlbumID)
	}

	s.mu.Lock()
	s.playlist = playlist
	s.originalQueue = slices.Clone(playlist.Entries)
	current := playlist.Entries[track]
	s.currentTrack = &current
	s.loadedTrackID = ""
	s.shuffled = false
	s.mu.Unlock()

	return s.loadCurrentAndPlay()
}

func (s *Service) loadCurrentAndPlay() error {
	s.mu.RLock()
	if s.currentTrack == nil {
		s.mu.RUnlock()
		return nil
	}
	track := *s.currentTrack
	s.mu.RUnlock()

	if coverPath, err := s.cacheCoverArt(s.ctx, track); err == nil {
		track.Image = coverPath
	}

	s.mu.RLock()
	source := s.source
	s.mu.RUnlock()
	playSource, err := source(s.ctx, track.ID)
	if err != nil {
		return err
	}
	request := audio.LoadRequest{
		URI:             playSource,
		TrackID:         track.ID,
		DurationSeconds: float64(track.Duration),
	}
	if err := s.audio.Load(s.ctx, request); err != nil {
		return err
	}
	isLocal := isLocalAudioSource(playSource)
	s.mu.Lock()
	s.buffering = false
	s.bufferedSec = 0
	s.bufferedBytes = 0
	s.totalBytes = 0
	s.bufferedPct = 0
	s.bufferPctKnown = false
	s.lastProgressEmit = time.Time{}
	if isLocal {
		s.bufferedSec = float64(track.Duration)
		s.bufferedBytes = 1
		s.totalBytes = 1
		s.bufferedPct = 100
		s.bufferPctKnown = true
	}
	s.lastError = ""
	s.mu.Unlock()
	if err := s.audio.Play(); err != nil {
		return err
	}

	s.mu.Lock()
	s.loadedTrackID = track.ID
	s.currentTrack = &track
	s.mu.Unlock()

	_ = s.media.UpdateNowPlaying(track)
	_ = s.media.SetPlaybackState(models.PlaybackPlaying)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.client.Scrobble(ctx, track.ID)
	}()
	s.emit()
	return nil
}

func (s *Service) cacheCoverArt(ctx context.Context, song models.Song) (string, error) {
	if song.CoverArt == "" {
		return "", nil
	}
	s.mu.RLock()
	cacheRoot := s.cacheRoot
	s.mu.RUnlock()
	if cacheRoot == "" {
		var err error
		cacheRoot, err = os.UserCacheDir()
		if err != nil {
			cacheRoot = os.TempDir()
		}
		cacheRoot = filepath.Join(cacheRoot, "saki")
	}
	dir := filepath.Join(cacheRoot, "covers")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, song.CoverArt+".jpg")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	data, err := s.client.GetCoverArt(ctx, song.CoverArt)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Service) consumeAudioEvents() {
	for {
		select {
		case event := <-s.audio.Events():
			switch event.Type {
			case audio.EventCompleted:
				s.handlePlaybackCompleted()
			case audio.EventError:
				if event.Err != nil {
					s.setLastError(event.Err.Error())
				}
				s.emit()
			case audio.EventPosition:
				s.emitProgress(false)
			case audio.EventBuffering:
				s.updateBufferState(event)
				s.emitProgress(true)
			case audio.EventBufferProgress:
				s.updateBufferState(event)
				s.emitProgress(false)
			default:
				s.emit()
			}
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Service) emitProgress(force bool) {
	s.mu.Lock()
	now := time.Now()
	if !force && now.Sub(s.lastProgressEmit) < 250*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.lastProgressEmit = now
	s.mu.Unlock()
	s.emit()
}

func (s *Service) setLastError(message string) {
	s.mu.Lock()
	s.lastError = message
	s.mu.Unlock()
}

func (s *Service) updateBufferState(event audio.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffering = event.Buffering
	if event.BufferedSeconds >= 0 {
		s.bufferedSec = event.BufferedSeconds
	}
	if event.BufferedBytes >= 0 {
		s.bufferedBytes = event.BufferedBytes
	}
	if event.TotalBytes >= 0 {
		s.totalBytes = event.TotalBytes
	}
	if percent, ok := bufferPercent(s.bufferedBytes, s.totalBytes); ok {
		s.bufferedPct = percent
		s.bufferPctKnown = true
	} else {
		s.bufferedPct = 0
		s.bufferPctKnown = false
	}
}

func (s *Service) consumeMediaCommands() {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			switch s.media.PollCommand() {
			case mediaintegration.CommandPlay:
				s.Play()
			case mediaintegration.CommandPause:
				s.Pause()
			case mediaintegration.CommandStop:
				s.Stop()
			case mediaintegration.CommandNext:
				s.Next()
			case mediaintegration.CommandPrevious:
				s.Previous()
			}
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *Service) handlePlaybackCompleted() {
	s.mu.Lock()
	nextIndex, ok := s.nextIndexLocked(true)
	if !ok {
		s.mu.Unlock()
		s.Stop()
		return
	}
	track := s.playlist.Entries[nextIndex]
	s.currentTrack = &track
	s.loadedTrackID = ""
	s.mu.Unlock()
	_ = s.loadCurrentAndPlay()
}

func (s *Service) nextIndexLocked(autoAdvance bool) (int, bool) {
	if s.currentTrack == nil || len(s.playlist.Entries) == 0 {
		return 0, false
	}
	index := s.currentIndexLocked()
	if autoAdvance && s.repeatStatus == models.RepeatOne {
		return index, true
	}
	if index < len(s.playlist.Entries)-1 {
		return index + 1, true
	}
	if s.repeatStatus == models.RepeatAll {
		return 0, true
	}
	return 0, false
}

func (s *Service) currentIndexLocked() int {
	if s.currentTrack == nil {
		return -1
	}
	for i, song := range s.playlist.Entries {
		if song.ID == s.currentTrack.ID {
			return i
		}
	}
	return -1
}

func (s *Service) stateLocked() models.CurrentState {
	index := s.currentIndexLocked()
	var current *models.Song
	if s.currentTrack != nil {
		track := *s.currentTrack
		current = &track
	}
	return models.CurrentState{
		CurrentTrack:       current,
		Position:           s.audio.Position(),
		Playing:            s.audio.IsPlaying(),
		Stopped:            !s.audio.IsPlaying() && !s.audio.IsPaused(),
		Buffering:          s.buffering,
		BufferedSeconds:    s.bufferedSec,
		BufferedBytes:      s.bufferedBytes,
		TotalBytes:         s.totalBytes,
		BufferedPercent:    s.bufferedPct,
		BufferPercentKnown: s.bufferPctKnown,
		LastError:          s.lastError,
		CurrentPlaylist:    s.playlist,
		CurrentTrackIndex:  index,
		RepeatStatus:       s.repeatStatus,
		Shuffled:           s.shuffled,
		Volume:             s.volume,
	}
}

func isLocalAudioSource(uri string) bool {
	uri = strings.ToLower(strings.TrimSpace(uri))
	return uri != "" && !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://")
}

func bufferPercent(bufferedBytes, totalBytes int64) (float64, bool) {
	if totalBytes <= 0 {
		return 0, false
	}
	percent := float64(bufferedBytes) / float64(totalBytes) * 100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return percent, true
}

func (s *Service) emit() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.emitLocked()
}

func (s *Service) emitLocked() {
	state := s.stateLocked()
	select {
	case s.updates <- state:
	default:
	}
}

func totalDuration(songs []models.Song) int {
	total := 0
	for _, song := range songs {
		total += song.Duration
	}
	return total
}
