package mediaintegration

import "github.com/xiongnemo/saki/internal/models"

type Command int

const (
	CommandNone Command = iota
	CommandPlay
	CommandPause
	CommandStop
	CommandNext
	CommandPrevious
)

type MediaIntegration interface {
	Init() error
	UpdateNowPlaying(song models.Song) error
	SetPlaybackState(state models.PlaybackState) error
	PollCommand() Command
	Close() error
}
