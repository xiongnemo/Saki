package audio

import (
	"context"

	"github.com/xiongnemo/saki/internal/models"
)

type EventType int

const (
	EventPosition EventType = iota
	EventCompleted
	EventError
	EventBuffering
	EventBufferProgress
	EventFormat
)

type LoadRequest struct {
	URI             string
	TrackID         string
	DurationSeconds float64
}

type Event struct {
	Type            EventType
	Position        float64
	Duration        float64
	Err             error
	Buffering       bool
	BufferedSeconds float64
	BufferedBytes   int64
	TotalBytes      int64
	AudioInfo       models.AudioInfo
}

type Backend interface {
	Load(ctx context.Context, request LoadRequest) error
	Play() error
	Pause() error
	Stop() error
	Seek(position float64) error
	SetVolume(volume float64) error
	Position() float64
	Duration() float64
	IsPlaying() bool
	IsPaused() bool
	Events() <-chan Event
	Close() error
}
