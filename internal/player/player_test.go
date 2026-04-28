package player

import (
	"context"
	"testing"
	"time"

	"github.com/xiongnemo/saki/internal/audio"
	"github.com/xiongnemo/saki/internal/mediaintegration"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/subsonic"
)

func TestRepeatCycleAndVolumeClamp(t *testing.T) {
	backend := newFakeAudio()
	service := New(subsonic.NewClient(nil), backend, newFakeMedia())
	defer service.Close()

	service.ToggleRepeat()
	if service.State().RepeatStatus != models.RepeatOne {
		t.Fatalf("expected repeat one")
	}
	service.ToggleRepeat()
	if service.State().RepeatStatus != models.RepeatAll {
		t.Fatalf("expected repeat all")
	}
	service.ToggleRepeat()
	if service.State().RepeatStatus != models.RepeatNone {
		t.Fatalf("expected repeat off")
	}

	service.SetVolume(200, false)
	if backend.volume != 1 {
		t.Fatalf("expected volume clamped to 1, got %f", backend.volume)
	}
	service.SetVolume(-200, false)
	if backend.volume != 0 {
		t.Fatalf("expected volume clamped to 0, got %f", backend.volume)
	}
}

func TestMediaCommandPauseAndStop(t *testing.T) {
	backend := newFakeAudio()
	backend.playing = true
	media := newFakeMedia()
	service := New(subsonic.NewClient(nil), backend, media)
	defer service.Close()

	media.push(mediaintegration.CommandPause)
	eventually(t, func() bool { return backend.paused && media.lastState == models.PlaybackPaused })

	backend.playing = true
	backend.paused = false
	media.push(mediaintegration.CommandStop)
	eventually(t, func() bool { return !backend.playing && !backend.paused && media.lastState == models.PlaybackStopped })
}

func TestAddToCurrentPlaylistEmitsState(t *testing.T) {
	service := New(subsonic.NewClient(nil), newFakeAudio(), newFakeMedia())
	defer service.Close()

	service.AddToCurrentPlaylist(models.Song{ID: "s1", Title: "Song"})

	select {
	case state := <-service.Updates():
		if len(state.CurrentPlaylist.Entries) != 1 || state.CurrentPlaylist.Entries[0].ID != "s1" {
			t.Fatalf("unexpected state: %#v", state)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for state update")
	}
}

func TestBufferPercent(t *testing.T) {
	percent, ok := bufferPercent(25, 100)
	if !ok || percent != 25 {
		t.Fatalf("bufferPercent = %.1f, %v; want 25 true", percent, ok)
	}
	percent, ok = bufferPercent(150, 100)
	if !ok || percent != 100 {
		t.Fatalf("clamped bufferPercent = %.1f, %v; want 100 true", percent, ok)
	}
	if _, ok := bufferPercent(1, 0); ok {
		t.Fatal("expected unknown percent when total is zero")
	}
}

func TestStateReportsCacheReadyForCurrentTrack(t *testing.T) {
	service := New(subsonic.NewClient(nil), newFakeAudio(), newFakeMedia())
	defer service.Close()

	track := models.Song{ID: "s1", Title: "Song"}
	service.mu.Lock()
	service.currentTrack = &track
	service.mu.Unlock()
	service.SetCacheReadyResolver(func(id string) bool {
		return id == "s1"
	})

	if !service.State().CacheReady {
		t.Fatal("expected current track cache to be reported ready")
	}
}

func TestAudioFormatEventMergesWithSongMetadata(t *testing.T) {
	backend := newFakeAudio()
	service := New(subsonic.NewClient(nil), backend, newFakeMedia())
	defer service.Close()

	track := models.Song{
		ID:           "s1",
		Title:        "Song",
		Suffix:       "m4a",
		BitRateKbps:  921,
		SamplingRate: 44100,
	}
	service.mu.Lock()
	service.currentTrack = &track
	service.audioInfo = track.AudioInfo()
	service.mu.Unlock()

	backend.events <- audio.Event{
		Type: audio.EventFormat,
		AudioInfo: models.AudioInfo{
			Codec:      "ALAC",
			BitDepth:   24,
			SampleRate: 48000,
			Channels:   2,
		},
	}

	eventually(t, func() bool {
		info := service.State().AudioInfo
		return info.Codec == "ALAC" &&
			info.BitDepth == 24 &&
			info.SampleRate == 48000 &&
			info.Channels == 2 &&
			info.BitRateKbps == 921
	})
}

func TestActiveBackendUsesReporterWhenAvailable(t *testing.T) {
	backend := &fakeAudioWithActive{fakeAudio: newFakeAudio(), active: "mpv"}
	service := New(subsonic.NewClient(nil), backend, newFakeMedia())
	defer service.Close()

	if got := service.ActiveBackend(); got != "mpv" {
		t.Fatalf("active backend = %q, want mpv", got)
	}

	custom := New(subsonic.NewClient(nil), newFakeAudio(), newFakeMedia())
	defer custom.Close()
	if got := custom.ActiveBackend(); got != "custom" {
		t.Fatalf("custom backend = %q, want custom", got)
	}
}

func TestSeekReloadsCachedSourceAfterStreamingSeekUnavailable(t *testing.T) {
	backend := newFakeAudio()
	backend.playing = true
	backend.seekErr = audio.ErrStreamSeekRequiresCache
	backend.failSeekOnce = true
	service := New(subsonic.NewClient(nil), backend, newFakeMedia())
	defer service.Close()

	track := models.Song{ID: "s1", Title: "Song", Duration: 120}
	service.mu.Lock()
	service.currentTrack = &track
	service.loadedTrackID = track.ID
	service.mu.Unlock()
	service.SetStreamSourceResolver(func(context.Context, string) (string, error) {
		return `C:\cache\song.mp3`, nil
	})

	service.Seek(30, false)

	if len(backend.loadRequests) != 1 {
		t.Fatalf("load requests = %d, want 1", len(backend.loadRequests))
	}
	if got := backend.loadRequests[0].URI; got != `C:\cache\song.mp3` {
		t.Fatalf("reload URI = %q", got)
	}
	if backend.position != 30 {
		t.Fatalf("position = %.1f, want 30", backend.position)
	}
	if !backend.playing {
		t.Fatal("expected playback to resume after cached seek reload")
	}
	state := service.State()
	if state.LastError != "" {
		t.Fatalf("last error = %q, want empty", state.LastError)
	}
	if !state.BufferPercentKnown || state.BufferedPercent != 100 || state.BufferedSeconds != 120 {
		t.Fatalf("unexpected buffer state: %#v", state)
	}
	if !state.CacheReady {
		t.Fatalf("expected cache ready after cached seek reload: %#v", state)
	}
}

func TestSeekKeepsStreamCacheErrorWhenCachedSourceUnavailable(t *testing.T) {
	backend := newFakeAudio()
	backend.seekErr = audio.ErrStreamSeekRequiresCache
	service := New(subsonic.NewClient(nil), backend, newFakeMedia())
	defer service.Close()

	track := models.Song{ID: "s1", Title: "Song", Duration: 120}
	service.mu.Lock()
	service.currentTrack = &track
	service.loadedTrackID = track.ID
	service.mu.Unlock()
	service.SetStreamSourceResolver(func(context.Context, string) (string, error) {
		return "http://127.0.0.1/stream/s1", nil
	})

	service.Seek(30, false)

	if len(backend.loadRequests) != 0 {
		t.Fatalf("load requests = %d, want 0", len(backend.loadRequests))
	}
	if got := service.State().LastError; got != audio.ErrStreamSeekRequiresCache.Error() {
		t.Fatalf("last error = %q", got)
	}
}

type fakeAudio struct {
	events       chan audio.Event
	playing      bool
	paused       bool
	volume       float64
	position     float64
	duration     float64
	seekErr      error
	failSeekOnce bool
	loadRequests []audio.LoadRequest
}

func newFakeAudio() *fakeAudio {
	return &fakeAudio{events: make(chan audio.Event, 4), volume: 1, duration: 120}
}

func (f *fakeAudio) Load(_ context.Context, request audio.LoadRequest) error {
	f.loadRequests = append(f.loadRequests, request)
	f.playing = false
	f.paused = true
	f.position = 0
	return nil
}
func (f *fakeAudio) Play() error {
	f.playing = true
	f.paused = false
	return nil
}
func (f *fakeAudio) Pause() error {
	f.playing = false
	f.paused = true
	return nil
}
func (f *fakeAudio) Stop() error {
	f.playing = false
	f.paused = false
	f.position = 0
	return nil
}
func (f *fakeAudio) Seek(position float64) error {
	if f.seekErr != nil {
		err := f.seekErr
		if f.failSeekOnce {
			f.seekErr = nil
			f.failSeekOnce = false
		}
		return err
	}
	f.position = position * f.duration
	return nil
}
func (f *fakeAudio) SetVolume(volume float64) error {
	f.volume = volume
	return nil
}
func (f *fakeAudio) Position() float64          { return f.position }
func (f *fakeAudio) Duration() float64          { return f.duration }
func (f *fakeAudio) IsPlaying() bool            { return f.playing }
func (f *fakeAudio) IsPaused() bool             { return f.paused }
func (f *fakeAudio) Events() <-chan audio.Event { return f.events }
func (f *fakeAudio) Close() error               { return nil }

type fakeAudioWithActive struct {
	*fakeAudio
	active string
}

func (f *fakeAudioWithActive) ActiveBackend() string {
	return f.active
}

type fakeMedia struct {
	commands  chan mediaintegration.Command
	lastState models.PlaybackState
}

func newFakeMedia() *fakeMedia {
	return &fakeMedia{commands: make(chan mediaintegration.Command, 8)}
}

func (f *fakeMedia) Init() error { return nil }
func (f *fakeMedia) UpdateNowPlaying(models.Song) error {
	return nil
}
func (f *fakeMedia) SetPlaybackState(state models.PlaybackState) error {
	f.lastState = state
	return nil
}
func (f *fakeMedia) PollCommand() mediaintegration.Command {
	select {
	case command := <-f.commands:
		return command
	default:
		return mediaintegration.CommandNone
	}
}
func (f *fakeMedia) Close() error { return nil }
func (f *fakeMedia) push(command mediaintegration.Command) {
	f.commands <- command
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-deadline:
			t.Fatal("condition not met before timeout")
		case <-tick.C:
			if condition() {
				return
			}
		}
	}
}
