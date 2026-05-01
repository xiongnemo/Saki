package audio

import (
	"context"
	"errors"
	"testing"

	"github.com/xiongnemo/saki/internal/models"
)

type fakeSwitchBackend struct {
	events   chan Event
	loadErr  error
	playErr  error
	loads    int
	plays    int
	stops    int
	requests []LoadRequest
}

func newFakeSwitchBackend() *fakeSwitchBackend {
	return &fakeSwitchBackend{events: make(chan Event)}
}

func (f *fakeSwitchBackend) Load(_ context.Context, request LoadRequest) error {
	if f.loadErr != nil {
		return f.loadErr
	}
	f.loads++
	f.requests = append(f.requests, request)
	return nil
}

func (f *fakeSwitchBackend) Play() error             { f.plays++; return f.playErr }
func (f *fakeSwitchBackend) Pause() error            { return nil }
func (f *fakeSwitchBackend) Stop() error             { f.stops++; return nil }
func (f *fakeSwitchBackend) Seek(float64) error      { return nil }
func (f *fakeSwitchBackend) SetVolume(float64) error { return nil }
func (f *fakeSwitchBackend) Position() float64       { return 0 }
func (f *fakeSwitchBackend) Duration() float64       { return 0 }
func (f *fakeSwitchBackend) IsPlaying() bool         { return false }
func (f *fakeSwitchBackend) IsPaused() bool          { return false }
func (f *fakeSwitchBackend) Events() <-chan Event    { return f.events }
func (f *fakeSwitchBackend) Close() error            { return nil }

func TestSetSettingsStopsActiveBackendOnModeChange(t *testing.T) {
	active := newFakeSwitchBackend()
	backend := &SwitchingBackend{
		settings: models.Settings{AudioBackend: "mpv"},
		mpv:      NewMPVBackend(models.Settings{AudioBackend: "mpv"}),
		active:   active,
	}

	backend.SetSettings(models.Settings{AudioBackend: "miniaudio"})

	if active.stops != 1 {
		t.Fatalf("active stops = %d, want 1", active.stops)
	}
	if backend.active != nil {
		t.Fatal("expected active backend to be cleared")
	}
}

func TestLoadWithStopsPreviousBackendBeforeSwitching(t *testing.T) {
	previous := newFakeSwitchBackend()
	next := newFakeSwitchBackend()
	backend := &SwitchingBackend{active: previous}

	if err := backend.loadWith(context.Background(), next, LoadRequest{TrackID: "s1"}); err != nil {
		t.Fatal(err)
	}

	if previous.stops != 1 {
		t.Fatalf("previous stops = %d, want 1", previous.stops)
	}
	if next.loads != 1 {
		t.Fatalf("next loads = %d, want 1", next.loads)
	}
	if backend.active != next {
		t.Fatal("expected next backend to become active")
	}
}

func TestActiveBackendReportsConcreteBackend(t *testing.T) {
	backend := &SwitchingBackend{
		miniaudio: NewMiniAudioBackend(),
		mpv:       NewMPVBackend(models.Settings{}),
	}

	if got := backend.ActiveBackend(); got != "none" {
		t.Fatalf("inactive backend = %q, want none", got)
	}
	backend.active = backend.miniaudio
	if got := backend.ActiveBackend(); got != "miniaudio" {
		t.Fatalf("miniaudio backend = %q", got)
	}
	backend.active = backend.mpv
	if got := backend.ActiveBackend(); got != "mpv" {
		t.Fatalf("mpv backend = %q", got)
	}
	backend.active = newFakeSwitchBackend()
	if got := backend.ActiveBackend(); got != "custom" {
		t.Fatalf("custom backend = %q", got)
	}
}

func TestAutoPlayFallsBackToMPVAfterMiniaudioPlayError(t *testing.T) {
	miniaudio := newFakeSwitchBackend()
	miniaudio.playErr = errors.New("miniaudio start failed")
	mpv := newFakeSwitchBackend()
	backend := &SwitchingBackend{
		settings:  models.Settings{AudioBackend: "auto"},
		miniaudio: miniaudio,
		mpv:       mpv,
	}
	request := LoadRequest{URI: "http://music.example/song.mp3", TrackID: "song"}

	if err := backend.Load(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if backend.active != miniaudio {
		t.Fatal("auto load should start with miniaudio")
	}
	if err := backend.Play(); err != nil {
		t.Fatalf("Play fallback error = %v", err)
	}
	if miniaudio.plays != 1 {
		t.Fatalf("miniaudio plays = %d, want 1", miniaudio.plays)
	}
	if mpv.loads != 1 || mpv.plays != 1 {
		t.Fatalf("mpv loads/plays = %d/%d, want 1/1", mpv.loads, mpv.plays)
	}
	if backend.active != mpv {
		t.Fatal("mpv should become active after fallback")
	}
}

func TestExplicitMiniaudioPlayErrorDoesNotFallback(t *testing.T) {
	miniaudio := newFakeSwitchBackend()
	miniaudio.playErr = errors.New("miniaudio start failed")
	mpv := newFakeSwitchBackend()
	backend := &SwitchingBackend{
		settings:  models.Settings{AudioBackend: "miniaudio"},
		miniaudio: miniaudio,
		mpv:       mpv,
	}

	if err := backend.Load(context.Background(), LoadRequest{URI: "http://music.example/song.mp3"}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Play(); err == nil {
		t.Fatal("Play should return the explicit miniaudio error")
	}
	if mpv.loads != 0 || mpv.plays != 0 {
		t.Fatalf("explicit miniaudio should not fallback to mpv, got loads/plays %d/%d", mpv.loads, mpv.plays)
	}
}
