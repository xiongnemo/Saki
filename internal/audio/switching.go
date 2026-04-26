package audio

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/xiongnemo/saki/internal/models"
)

type SwitchingBackend struct {
	settings  models.Settings
	miniaudio *MiniAudioBackend
	mpv       *MPVBackend
	active    Backend
	events    chan Event
}

func NewBackend(settings models.Settings) *SwitchingBackend {
	backend := &SwitchingBackend{
		settings:  settings,
		miniaudio: NewMiniAudioBackend(),
		mpv:       NewMPVBackend(settings),
		events:    make(chan Event, 128),
	}
	go backend.forward(backend.miniaudio.Events())
	go backend.forward(backend.mpv.Events())
	return backend
}

func (b *SwitchingBackend) SetSettings(settings models.Settings) {
	b.settings = settings
	b.mpv.SetSettings(settings)
}

func (b *SwitchingBackend) Load(ctx context.Context, request LoadRequest) error {
	mode := normalizeBackendMode(b.settings.AudioBackend)
	switch mode {
	case "miniaudio":
		if err := b.miniaudio.Load(ctx, request); err != nil {
			return err
		}
		b.active = b.miniaudio
		return nil
	case "mpv":
		if err := b.mpv.Load(ctx, request); err != nil {
			return err
		}
		b.active = b.mpv
		return nil
	default:
		if err := b.miniaudio.Load(ctx, request); err == nil {
			b.active = b.miniaudio
			return nil
		} else {
			if mpvErr := b.mpv.Load(ctx, request); mpvErr == nil {
				b.active = b.mpv
				return nil
			} else {
				return fmt.Errorf("miniaudio failed: %v; mpv fallback failed: %w", err, mpvErr)
			}
		}
	}
}

func (b *SwitchingBackend) Play() error {
	if b.active == nil {
		return errors.New("no active audio backend")
	}
	return b.active.Play()
}

func (b *SwitchingBackend) Pause() error {
	if b.active == nil {
		return nil
	}
	return b.active.Pause()
}

func (b *SwitchingBackend) Stop() error {
	if b.active == nil {
		return nil
	}
	return b.active.Stop()
}

func (b *SwitchingBackend) Seek(position float64) error {
	if b.active == nil {
		return nil
	}
	return b.active.Seek(position)
}

func (b *SwitchingBackend) SetVolume(volume float64) error {
	if b.active != nil {
		if err := b.active.SetVolume(volume); err != nil {
			return err
		}
	}
	_ = b.miniaudio.SetVolume(volume)
	return b.mpv.SetVolume(volume)
}

func (b *SwitchingBackend) Position() float64 {
	if b.active == nil {
		return 0
	}
	return b.active.Position()
}

func (b *SwitchingBackend) Duration() float64 {
	if b.active == nil {
		return 0
	}
	return b.active.Duration()
}

func (b *SwitchingBackend) IsPlaying() bool {
	return b.active != nil && b.active.IsPlaying()
}

func (b *SwitchingBackend) IsPaused() bool {
	return b.active != nil && b.active.IsPaused()
}

func (b *SwitchingBackend) Events() <-chan Event {
	return b.events
}

func (b *SwitchingBackend) Close() error {
	minErr := b.miniaudio.Close()
	mpvErr := b.mpv.Close()
	if minErr != nil {
		return minErr
	}
	return mpvErr
}

func (b *SwitchingBackend) forward(events <-chan Event) {
	for event := range events {
		select {
		case b.events <- event:
		default:
		}
	}
}

func normalizeBackendMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "miniaudio", "mpv":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "auto"
	}
}
