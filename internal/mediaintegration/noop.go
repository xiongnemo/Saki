//go:build !windows

package mediaintegration

import "github.com/xiongnemo/saki/internal/models"

type noopIntegration struct{}

func New() MediaIntegration {
	return noopIntegration{}
}

func (noopIntegration) Init() error {
	return nil
}

func (noopIntegration) UpdateNowPlaying(models.Song) error {
	return nil
}

func (noopIntegration) SetPlaybackState(models.PlaybackState) error {
	return nil
}

func (noopIntegration) PollCommand() Command {
	return CommandNone
}

func (noopIntegration) Close() error {
	return nil
}
