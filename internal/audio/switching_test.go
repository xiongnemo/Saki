package audio

import (
	"context"
	"testing"

	"github.com/xiongnemo/saki/internal/models"
)

type fakeSwitchBackend struct {
	events chan Event
	loads  int
	stops  int
}

func newFakeSwitchBackend() *fakeSwitchBackend {
	return &fakeSwitchBackend{events: make(chan Event)}
}

func (f *fakeSwitchBackend) Load(context.Context, LoadRequest) error {
	f.loads++
	return nil
}

func (f *fakeSwitchBackend) Play() error             { return nil }
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
