package ui

type uiActionID string

const (
	actionArtists        uiActionID = "artists"
	actionAlbums         uiActionID = "albums"
	actionPlaylists      uiActionID = "playlists"
	actionSearch         uiActionID = "search"
	actionNowPlaying     uiActionID = "now-playing"
	actionSystem         uiActionID = "system"
	actionHelp           uiActionID = "help"
	actionCommandPalette uiActionID = "command-palette"
	actionPlayPause      uiActionID = "play-pause"
	actionPrevious       uiActionID = "previous"
	actionNext           uiActionID = "next"
	actionRepeat         uiActionID = "repeat"
	actionShuffle        uiActionID = "shuffle"
	actionSeekBack       uiActionID = "seek-back"
	actionSeekForward    uiActionID = "seek-forward"
	actionVolumeDown     uiActionID = "volume-down"
	actionVolumeUp       uiActionID = "volume-up"
	actionFineVolumeDown uiActionID = "fine-volume-down"
	actionFineVolumeUp   uiActionID = "fine-volume-up"
	actionQuit           uiActionID = "quit"
)

type uiAction struct {
	id       uiActionID
	title    string
	group    string
	bindings []string
	run      func(*App)
}

func globalActions() []uiAction {
	return []uiAction{
		{id: actionArtists, title: "Artists", group: "Navigation", bindings: []string{"1"}, run: func(a *App) { a.showArtists(true) }},
		{id: actionAlbums, title: "Albums", group: "Navigation", bindings: []string{"2"}, run: func(a *App) { a.showAlbums(true) }},
		{id: actionPlaylists, title: "Playlists", group: "Navigation", bindings: []string{"3"}, run: func(a *App) { a.showPlaylists(true) }},
		{id: actionSearch, title: "Search", group: "Navigation", bindings: []string{"4"}, run: func(a *App) { a.showSearch(true) }},
		{id: actionNowPlaying, title: "Now Playing", group: "Navigation", bindings: []string{"5"}, run: func(a *App) { a.showNowPlaying(true) }},
		{id: actionSystem, title: "System", group: "Navigation", bindings: []string{"6"}, run: func(a *App) { a.showSettings(true) }},
		{id: actionHelp, title: "Help", group: "System", bindings: []string{"?", "？"}, run: func(a *App) { a.showHelpOverlay() }},
		{id: actionCommandPalette, title: "Command Palette", group: "System", bindings: []string{":", "："}, run: func(a *App) { a.showCommandPalette() }},
		{id: actionPlayPause, title: "Play/Pause", group: "Playback", bindings: []string{"Space"}, run: func(a *App) {
			if a.player != nil {
				a.runPlaybackCommand(func() { a.player.PlayPause() })
			}
		}},
		{id: actionPrevious, title: "Previous Track", group: "Playback", bindings: []string{";"}, run: func(a *App) {
			if a.player != nil {
				a.runPlaybackCommand(func() { a.player.Previous() })
			}
		}},
		{id: actionNext, title: "Next Track", group: "Playback", bindings: []string{"'"}, run: func(a *App) {
			if a.player != nil {
				a.runPlaybackCommand(func() { a.player.Next() })
			}
		}},
		{id: actionRepeat, title: "Toggle Repeat", group: "Playback", bindings: []string{"r"}, run: func(a *App) {
			if a.player != nil {
				a.player.ToggleRepeat()
			}
		}},
		{id: actionShuffle, title: "Shuffle", group: "Playback", bindings: []string{"s"}, run: func(a *App) {
			if a.player != nil {
				a.player.Shuffle()
			}
		}},
		{id: actionSeekBack, title: "Seek Back 10s", group: "Playback", bindings: []string{","}, run: func(a *App) {
			if a.player != nil {
				a.runPlaybackCommand(func() { a.player.Seek(-10, true) })
			}
		}},
		{id: actionSeekForward, title: "Seek Forward 10s", group: "Playback", bindings: []string{"."}, run: func(a *App) {
			if a.player != nil {
				a.runPlaybackCommand(func() { a.player.Seek(10, true) })
			}
		}},
		{id: actionVolumeDown, title: "Volume Down 5%", group: "Playback", bindings: []string{"-"}, run: func(a *App) {
			if a.player != nil {
				a.player.SetVolume(-5, true)
			}
		}},
		{id: actionVolumeUp, title: "Volume Up 5%", group: "Playback", bindings: []string{"="}, run: func(a *App) {
			if a.player != nil {
				a.player.SetVolume(5, true)
			}
		}},
		{id: actionFineVolumeDown, title: "Volume Down 1%", group: "Playback", bindings: []string{"["}, run: func(a *App) {
			if a.player != nil {
				a.player.SetVolume(-1, true)
			}
		}},
		{id: actionFineVolumeUp, title: "Volume Up 1%", group: "Playback", bindings: []string{"]"}, run: func(a *App) {
			if a.player != nil {
				a.player.SetVolume(1, true)
			}
		}},
		{id: actionQuit, title: "Quit", group: "Quit", bindings: []string{"q"}, run: func(a *App) { a.stop() }},
	}
}

func paletteActions() []uiAction {
	actions := globalActions()
	filtered := make([]uiAction, 0, len(actions)-1)
	for _, action := range actions {
		if action.id != actionCommandPalette {
			filtered = append(filtered, action)
		}
	}
	return filtered
}
