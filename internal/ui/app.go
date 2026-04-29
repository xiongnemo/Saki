package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/config"
	"github.com/xiongnemo/saki/internal/mediaintegration"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/player"
	"github.com/xiongnemo/saki/internal/subsonic"
)

var (
	uiBackground = tcell.ColorBlack
	uiPanel      = tcell.GetColor("#101820")
	uiField      = tcell.GetColor("#16202a")
	uiBorder     = tcell.GetColor("#6ea8fe")
	uiTitle      = tcell.GetColor("#cde8ff")
	uiLabel      = tcell.GetColor("#8ee3ff")
	uiText       = tcell.ColorWhite
	uiMuted      = tcell.GetColor("#9aa8b4")
	uiAccent     = tcell.GetColor("#00d1d1")
	uiDanger     = tcell.GetColor("#ff5c75")
)

var (
	onOffOptions       = []string{"Off", "On"}
	audioBackendLabels = []string{"Auto", "miniaudio", "mpv"}
	audioBackendValues = []string{"auto", "miniaudio", "mpv"}
)

const (
	playingPanelHeight       = 5
	nowPlayingPageName       = "now-playing"
	controlsViewHelpText     = "C-a Artists | C-l Albums | C-p Playlists | C-r Search | C-o Playing | / Search View | C-s System"
	controlsPlaybackHelpText = "Space Play/Pause | C-b Prev | C-n Next | C-t Repeat | C-h Shuffle | C-i/k Volume | C-Left/Right Seek | C-q Quit"
	controlsHelpText         = controlsViewHelpText + "\n" + controlsPlaybackHelpText
)

type appFocusTarget int

const (
	appFocusContent appFocusTarget = iota
	appFocusQueue
)

type App struct {
	ctx    context.Context
	cancel context.CancelFunc

	store       config.Store
	cfg         models.Config
	client      *subsonic.Client
	player      *player.Service
	media       mediaintegration.MediaIntegration
	applyConfig func(models.Config) models.Config

	app     *tview.Application
	pages   *tview.Pages
	content *tview.Pages
	queue   *tview.List
	cover   *coverPreview
	status  *playingView
	playing *nowPlayingView
	help    *tview.TextView

	currentState models.CurrentState

	focusTarget        appFocusTarget
	contentFocus       tview.Primitive
	contentTabHandler  func(back bool) bool
	contentReturn      func(back bool) bool
	contentOwnsTab     bool
	contentMouseLists  []*tview.List
	playingReturnFocus tview.Primitive
	lastClickList      *tview.List
	lastClickIndex     int
	lastClickAt        time.Time
	systemPopup        tview.Primitive
	systemPopupCancel  func()

	historyMu sync.Mutex
	history   []func()
}

type searchPane int

const (
	searchPaneArtists searchPane = iota
	searchPaneAlbums
	searchPaneSongs
)

type searchSelections struct {
	artists int
	albums  int
	songs   int
}

type searchFocusTarget struct {
	pane searchPane
	list *tview.List
}

func New(ctx context.Context, cancel context.CancelFunc, store config.Store, cfg models.Config, client *subsonic.Client, player *player.Service, media mediaintegration.MediaIntegration, applyConfig func(models.Config) models.Config) *App {
	configureTheme()
	app := &App{
		ctx:         ctx,
		cancel:      cancel,
		store:       store,
		cfg:         cfg,
		client:      client,
		player:      player,
		media:       media,
		applyConfig: applyConfig,
		app:         tview.NewApplication(),
		pages:       tview.NewPages(),
	}
	app.app.SetMouseCapture(app.handleMouseCapture)
	return app
}

func (a *App) Run() error {
	if hasLoginProfile(a.cfg) {
		a.login(a.cfg)
	} else {
		a.showLogin()
	}
	go a.consumePlayerUpdates()
	return a.app.SetRoot(a.pages, true).EnableMouse(true).Run()
}

func (a *App) showLogin() {
	cfg := a.cfg
	account := cfg.Account
	endpointsText := endpointsToText(account.Endpoints)
	authMode := 0
	if account.UsePlaintext {
		authMode = 1
	}
	form := tview.NewForm().
		AddInputField("Endpoints", endpointsText, 72, nil, func(value string) {
			account.Endpoints = parseEndpoints(value)
		}).
		AddInputField("Username", account.Username, 48, nil, func(value string) { account.Username = strings.TrimSpace(value) }).
		AddPasswordField("Password", account.Password, 48, '*', func(value string) { account.Password = value }).
		AddDropDown("Password mode", []string{"Token/salt", "Plaintext"}, authMode, func(_ string, index int) {
			account.UsePlaintext = index == 1
		}).
		AddButton("Login", func() {
			cfg.Account = account
			a.login(cfg)
		}).
		AddButton("Quit", func() { a.stop() })

	form.SetBorder(true)
	setViewTitle(form)
	styleForm(form)
	if field, ok := form.GetFormItemByLabel("Endpoints").(*tview.InputField); ok {
		field.SetPlaceholder("https://server-a; https://server-b")
	}

	box := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(nil, 0, 1, false).
		AddItem(form, 13, 1, true).
		AddItem(nil, 0, 1, false)

	a.pages.AddAndSwitchToPage("login", center(box, 104, 17), true)
}

func (a *App) login(cfg models.Config) {
	cfg = config.WithDefaults(cfg)
	if len(cfg.Account.Endpoints) == 0 || cfg.Account.Username == "" || cfg.Account.Password == "" {
		a.modal("Missing login details", "Server URL, username, and password are required.")
		return
	}

	a.pages.AddAndSwitchToPage("login-loading", centeredText("Login", "Connecting to server..."), true)
	go func() {
		configured := a.apply(cfg)
		if _, err := a.client.GetArtists(a.ctx); err != nil {
			a.app.QueueUpdateDraw(func() {
				a.pages.RemovePage("login-loading")
				a.cfg = cfg
				a.showLogin()
				a.modal("Login failed", err.Error())
			})
			return
		}
		a.cfg = configured
		_ = a.store.Save(configured)
		a.app.QueueUpdateDraw(func() {
			a.pages.RemovePage("login-loading")
			a.showMain()
		})
	}()
}

func (a *App) showMain() {
	a.content = tview.NewPages()
	a.contentFocus = nil
	a.contentTabHandler = nil
	a.focusTarget = appFocusContent
	a.queue = a.newList(appFocusQueue)
	a.queue.SetBorder(true)
	setPlainTitle(a.queue, "Queue")
	a.queue.SetSelectedFunc(func(index int, _ string, _ string, _ rune) {
		a.player.SkipTo(index)
	})
	a.cover = newCoverPreview()

	a.status = newPlayingView()

	a.help = tview.NewTextView().SetDynamicColors(true)
	a.help.SetScrollable(false)
	a.help.SetBorder(true)
	setPlainTitle(a.help, "Controls")
	a.help.SetText(controlsHelpText)

	left := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.content, 0, 1, true).
		AddItem(a.status, playingPanelHeight, 0, false).
		AddItem(a.help, 4, 0, false)
	right := a.queuePanel()

	root := tview.NewFlex().
		AddItem(left, 0, 1, true).
		AddItem(right, 36, 0, false)

	root.SetInputCapture(a.handleGlobalKey)
	a.pages.AddAndSwitchToPage("main", root, true)
	a.showArtists(true)
}

func (a *App) queuePanel() tview.Primitive {
	return newQueueSidePanel(a.queue, a.cover)
}

func (a *App) handleGlobalKey(event *tcell.EventKey) *tcell.EventKey {
	if a.hasSystemEditPopup() {
		switch event.Key() {
		case tcell.KeyCtrlQ:
			a.stop()
			return nil
		case tcell.KeyEscape:
			a.closeSystemEditPopup()
			return nil
		default:
			if a.systemPopup != nil {
				if handler := a.systemPopup.InputHandler(); handler != nil {
					handler(event, func(p tview.Primitive) {
						a.app.SetFocus(p)
					})
				}
			}
			return nil
		}
	}

	if a.hasNowPlayingOverlay() {
		if a.handleNowPlayingKey(event) {
			return nil
		}
		return nil
	}

	textInputFocused := acceptsTextInput(a.app.GetFocus())
	switch event.Key() {
	case tcell.KeyCtrlQ:
		a.stop()
		return nil
	case tcell.KeyTab:
		if a.contentOwnsTab && a.focusTarget == appFocusContent {
			return event
		}
		if a.handleFocusTraversal(false) {
			return nil
		}
	case tcell.KeyBacktab:
		if a.contentOwnsTab && a.focusTarget == appFocusContent {
			return event
		}
		if a.handleFocusTraversal(true) {
			return nil
		}
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyHome, tcell.KeyEnd, tcell.KeyPgUp, tcell.KeyPgDn, tcell.KeyEnter:
		if a.handleQueueKey(event) {
			return nil
		}
	case tcell.KeyCtrlA:
		a.showArtists(true)
		return nil
	case tcell.KeyCtrlL:
		a.showAlbums(true)
		return nil
	case tcell.KeyCtrlP:
		a.showPlaylists(true)
		return nil
	case tcell.KeyCtrlR:
		a.showSearch(true)
		return nil
	case tcell.KeyCtrlO:
		a.showNowPlaying(true)
		return nil
	case tcell.KeyCtrlS:
		a.showSettings(true)
		return nil
	case tcell.KeyCtrlN:
		a.player.Next()
		return nil
	case tcell.KeyCtrlB:
		a.player.Previous()
		return nil
	case tcell.KeyCtrlT:
		a.player.ToggleRepeat()
		return nil
	case tcell.KeyCtrlH:
		a.player.Shuffle()
		return nil
	case tcell.KeyCtrlI:
		a.player.SetVolume(5, true)
		return nil
	case tcell.KeyCtrlK:
		a.player.SetVolume(-5, true)
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if textInputFocused {
			return event
		}
		a.goBack()
		return nil
	case tcell.KeyEscape:
		a.goBack()
		return nil
	case tcell.KeyRight:
		if a.handleQueueKey(event) {
			return nil
		}
		if event.Modifiers()&tcell.ModCtrl != 0 {
			a.player.Seek(10, true)
			return nil
		}
	case tcell.KeyLeft:
		if a.handleQueueKey(event) {
			return nil
		}
		if event.Modifiers()&tcell.ModCtrl != 0 {
			a.player.Seek(-10, true)
			return nil
		}
	case tcell.KeyRune:
		if event.Rune() == ' ' {
			if textInputFocused {
				return event
			}
			a.player.PlayPause()
			return nil
		}
	}
	return event
}

func (a *App) handleNowPlayingKey(event *tcell.EventKey) bool {
	if event == nil {
		return true
	}
	switch event.Key() {
	case tcell.KeyCtrlQ:
		a.stop()
	case tcell.KeyEscape, tcell.KeyBackspace, tcell.KeyBackspace2:
		a.closeNowPlaying()
	case tcell.KeyCtrlO:
		a.closeNowPlaying()
	case tcell.KeyCtrlN:
		if a.player != nil {
			a.player.Next()
		}
	case tcell.KeyCtrlB:
		if a.player != nil {
			a.player.Previous()
		}
	case tcell.KeyCtrlT:
		if a.player != nil {
			a.player.ToggleRepeat()
		}
	case tcell.KeyCtrlH:
		if a.player != nil {
			a.player.Shuffle()
		}
	case tcell.KeyCtrlI:
		if a.player != nil {
			a.player.SetVolume(5, true)
		}
	case tcell.KeyCtrlK:
		if a.player != nil {
			a.player.SetVolume(-5, true)
		}
	case tcell.KeyRight:
		if event.Modifiers()&tcell.ModCtrl != 0 && a.player != nil {
			a.player.Seek(10, true)
		}
	case tcell.KeyLeft:
		if event.Modifiers()&tcell.ModCtrl != 0 && a.player != nil {
			a.player.Seek(-10, true)
		}
	case tcell.KeyRune:
		if event.Rune() == ' ' && a.player != nil {
			a.player.PlayPause()
		}
	}
	return true
}

func (a *App) handleQueueKey(event *tcell.EventKey) bool {
	if a.queue == nil || a.focusTarget != appFocusQueue {
		return false
	}
	if event.Modifiers()&tcell.ModCtrl != 0 {
		return false
	}
	if handler := a.queue.InputHandler(); handler != nil {
		handler(event, func(p tview.Primitive) {
			a.app.SetFocus(p)
		})
		return true
	}
	return false
}

func (a *App) handleFocusTraversal(back bool) bool {
	if a.content == nil || a.queue == nil {
		return false
	}
	if a.app.GetFocus() == a.queue || a.focusTarget == appFocusQueue {
		if a.contentReturn != nil && a.contentReturn(back) {
			return true
		}
		a.focusContent()
		return true
	}
	if a.contentTabHandler != nil && a.contentTabHandler(back) {
		return true
	}
	a.focusQueue()
	return true
}

func (a *App) focusContent() {
	a.focusTarget = appFocusContent
	focus := a.contentFocus
	if focus == nil {
		focus = a.content
	}
	a.app.SetFocus(focus)
}

func (a *App) focusQueue() {
	a.focusTarget = appFocusQueue
	a.app.SetFocus(a.queue)
}

func (a *App) newList(target appFocusTarget) *tview.List {
	list := tview.NewList().ShowSecondaryText(false)
	styleList(list)
	list.SetFocusFunc(func() {
		applyListFocusStyle(list, true)
		a.focusTarget = target
		if target == appFocusContent {
			a.contentFocus = list
		}
	})
	list.SetBlurFunc(func() {
		applyListFocusStyle(list, false)
	})
	return list
}

func (a *App) handleMouseCapture(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if event == nil {
		return nil, action
	}
	if a.hasSystemEditPopup() {
		if a.systemPopup != nil {
			if handler := a.systemPopup.MouseHandler(); handler != nil {
				_, _ = handler(action, event, func(p tview.Primitive) {
					a.app.SetFocus(p)
				})
			}
		}
		return nil, action
	}
	if a.hasNowPlayingOverlay() {
		return nil, action
	}
	list := a.listAt(event.Position())
	if list != nil {
		switch action {
		case tview.MouseLeftClick:
			if index := a.focusAndSelectListItem(list, event); index >= 0 {
				a.lastClickList = list
				a.lastClickIndex = index
				a.lastClickAt = time.Now()
			}
			return nil, action
		case tview.MouseLeftDoubleClick:
			index := a.focusAndSelectListItem(list, event)
			if index >= 0 && a.confirmedDoubleClick(list, index) {
				if handler := list.InputHandler(); handler != nil {
					handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) {
						a.app.SetFocus(p)
					})
				}
				a.lastClickList = nil
				a.lastClickAt = time.Time{}
			}
			return nil, action
		case tview.MouseScrollUp:
			a.scrollList(list, -1)
			return nil, action
		case tview.MouseScrollDown:
			a.scrollList(list, 1)
			return nil, action
		}
		return event, action
	}
	if a.passivePanelAt(event.Position()) && consumesPassivePanelMouse(action) {
		return nil, action
	}
	return event, action
}

func (a *App) hasSystemEditPopup() bool {
	return a.pages != nil && a.pages.HasPage(systemEditPageName)
}

func (a *App) closeSystemEditPopup() {
	if a.systemPopupCancel != nil {
		cancel := a.systemPopupCancel
		a.systemPopupCancel = nil
		cancel()
		return
	}
	if a.pages != nil {
		a.pages.RemovePage(systemEditPageName)
	}
	a.systemPopup = nil
	a.focusContent()
}

func (a *App) listAt(x, y int) *tview.List {
	if a.queue != nil && a.queue.InRect(x, y) {
		return a.queue
	}
	for _, list := range a.contentMouseLists {
		if list != nil && list.InRect(x, y) {
			return list
		}
	}
	return nil
}

func (a *App) passivePanelAt(x, y int) bool {
	if a.status != nil && a.status.InRect(x, y) {
		return true
	}
	if a.help != nil && a.help.InRect(x, y) {
		return true
	}
	return false
}

func consumesPassivePanelMouse(action tview.MouseAction) bool {
	switch action {
	case tview.MouseLeftDown, tview.MouseLeftUp, tview.MouseLeftClick, tview.MouseLeftDoubleClick,
		tview.MouseScrollUp, tview.MouseScrollDown, tview.MouseScrollLeft, tview.MouseScrollRight:
		return true
	default:
		return false
	}
}

func (a *App) focusAndSelectListItem(list *tview.List, event *tcell.EventMouse) int {
	a.app.SetFocus(list)
	x, y := event.Position()
	if index := listItemAt(list, x, y); index >= 0 {
		list.SetCurrentItem(index)
		return index
	}
	return -1
}

func (a *App) confirmedDoubleClick(list *tview.List, index int) bool {
	return a.lastClickList == list &&
		a.lastClickIndex == index &&
		!a.lastClickAt.IsZero() &&
		time.Since(a.lastClickAt) <= tview.DoubleClickInterval
}

func (a *App) scrollList(list *tview.List, delta int) {
	if list == nil || list.GetItemCount() == 0 || delta == 0 {
		return
	}
	a.app.SetFocus(list)
	offset, horizontal := list.GetOffset()
	_, _, _, height := list.GetInnerRect()
	if height <= 0 {
		height = 1
	}
	maxOffset := list.GetItemCount() - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	nextOffset := offset + delta
	if nextOffset < 0 {
		nextOffset = 0
	}
	if nextOffset > maxOffset {
		nextOffset = maxOffset
	}

	current := list.GetCurrentItem()
	row := current - offset
	if row < 0 {
		row = 0
	}
	if row >= height {
		row = height - 1
	}
	nextCurrent := nextOffset + row
	if nextCurrent < 0 {
		nextCurrent = 0
	}
	if nextCurrent >= list.GetItemCount() {
		nextCurrent = list.GetItemCount() - 1
	}

	list.SetOffset(nextOffset, horizontal)
	list.SetCurrentItem(nextCurrent)
}

func listItemAt(list *tview.List, x, y int) int {
	if list == nil || list.GetItemCount() == 0 {
		return -1
	}
	innerX, innerY, innerWidth, innerHeight := list.GetInnerRect()
	if x < innerX || x >= innerX+innerWidth || y < innerY || y >= innerY+innerHeight {
		return -1
	}
	itemOffset, _ := list.GetOffset()
	index := itemOffset + y - innerY
	if index < 0 || index >= list.GetItemCount() {
		return -1
	}
	return index
}

func acceptsTextInput(focus tview.Primitive) bool {
	switch focus.(type) {
	case *tview.InputField, *tview.TextArea:
		return true
	default:
		return false
	}
}

func (a *App) showArtists(push bool) {
	a.showArtistsAt(push, 0)
}

func (a *App) showArtistsAt(push bool, selected int) {
	if push {
		a.pushHistory(func() { a.showArtistsAt(false, selected) })
	}
	a.setContent("Artists", centeredText("Artists", "Loading artists..."))
	go func() {
		artists, err := a.client.GetArtists(a.ctx)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.showError("Artists", err)
				return
			}
			list := a.newFilterableList(appFocusContent)
			list.SetBorder(true)
			setViewTitle(list, "Artists")
			for _, artist := range artists {
				artist := artist
				list.AddItem(fmt.Sprintf("%s  [%d albums]", artist.Name, artist.AlbumCount), "", 0, func() {
					index := list.GetCurrentItem()
					a.replaceHistoryTop(func() { a.showArtistsAt(false, index) })
					a.showArtist(artist.ID, true)
				})
			}
			list.SetCurrentItem(selected)
			a.setContent("Artists", list)
		})
	}()
}

func (a *App) showArtist(id string, push bool) {
	a.showArtistAt(id, push, 0)
}

func (a *App) showArtistAt(id string, push bool, selected int) {
	if push {
		a.pushHistory(func() { a.showArtistAt(id, false, selected) })
	}
	a.setContent("Artist", centeredText("Artist", "Loading artist..."))
	go func() {
		artist, err := a.client.GetArtist(a.ctx, id)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.showError("Artist", err)
				return
			}
			list := a.newFilterableList(appFocusContent)
			list.SetBorder(true)
			setViewTitle(list, "Artists", artist.Name)
			for _, album := range artist.Albums {
				album := album
				list.AddItem(fmt.Sprintf("(%04d) %s", album.Year, album.Name), "", 0, func() {
					index := list.GetCurrentItem()
					a.replaceHistoryTop(func() { a.showArtistAt(id, false, index) })
					a.showAlbum(album.ID, true)
				})
			}
			list.SetCurrentItem(selected)
			a.setContent("Artist", list)
		})
	}()
}

func (a *App) showAlbums(push bool) {
	a.showAlbumsAt(push, 0)
}

func (a *App) showAlbumsAt(push bool, selected int) {
	if push {
		a.pushHistory(func() { a.showAlbumsAt(false, selected) })
	}
	a.setContent("Albums", centeredText("Albums", "Loading albums..."))
	go func() {
		albums, err := a.client.GetAllAlbums(a.ctx)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.showError("Albums", err)
				return
			}
			list := a.newFilterableList(appFocusContent)
			list.SetBorder(true)
			setViewTitle(list, "Albums")
			for _, album := range albums {
				album := album
				list.AddItem(fmt.Sprintf("%s :: %s", album.Artist, album.Name), "", 0, func() {
					index := list.GetCurrentItem()
					a.replaceHistoryTop(func() { a.showAlbumsAt(false, index) })
					a.showAlbum(album.ID, true)
				})
			}
			list.SetCurrentItem(selected)
			a.setContent("Albums", list)
		})
	}()
}

func (a *App) showAlbum(id string, push bool) {
	a.showAlbumAt(id, push, 0)
}

func (a *App) showAlbumAt(id string, push bool, selected int) {
	if push {
		a.pushHistory(func() { a.showAlbumAt(id, false, selected) })
	}
	a.setContent("Album", centeredText("Album", "Loading album..."))
	go func() {
		album, err := a.client.GetAlbum(a.ctx, id)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.showError("Album", err)
				return
			}
			list := a.newFilterableList(appFocusContent)
			list.SetBorder(true)
			setViewTitle(list, "Artists", album.Artist, "Albums", album.Name)
			for i, song := range album.Songs {
				i, song := i, song
				list.AddItem(fmt.Sprintf("%02d - %s [%s]", song.Track, song.Title, models.SecondsAsMMSS(song.Duration)), "", 0, func() {
					go a.report(a.player.PlayAlbum(a.ctx, album.ID, i))
				})
			}
			list.SetListInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				if event.Key() == tcell.KeyCtrlM {
					index := list.GetCurrentItem()
					if index >= 0 && index < len(album.Songs) {
						a.player.AddToCurrentPlaylist(album.Songs[index])
						return nil
					}
				}
				return event
			})
			list.SetCurrentItem(selected)
			a.setContent("Album", list)
		})
	}()
}

func (a *App) showPlaylists(push bool) {
	a.showPlaylistsAt(push, 0)
}

func (a *App) showPlaylistsAt(push bool, selected int) {
	if push {
		a.pushHistory(func() { a.showPlaylistsAt(false, selected) })
	}
	a.setContent("Playlists", centeredText("Playlists", "Loading playlists..."))
	go func() {
		playlists, err := a.client.GetPlaylists(a.ctx)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.showError("Playlists", err)
				return
			}
			list := a.newFilterableList(appFocusContent)
			list.SetBorder(true)
			setViewTitle(list, "Playlists")
			for _, playlist := range playlists {
				playlist := playlist
				list.AddItem(fmt.Sprintf("%s :: %s [%s]", playlist.Name, playlist.Owner, models.SecondsAsMMSS(playlist.Duration)), "", 0, func() {
					index := list.GetCurrentItem()
					a.replaceHistoryTop(func() { a.showPlaylistsAt(false, index) })
					a.showPlaylist(playlist.ID, true)
				})
			}
			list.SetCurrentItem(selected)
			a.setContent("Playlists", list)
		})
	}()
}

func (a *App) showPlaylist(id string, push bool) {
	a.showPlaylistAt(id, push, 0)
}

func (a *App) showPlaylistAt(id string, push bool, selected int) {
	if push {
		a.pushHistory(func() { a.showPlaylistAt(id, false, selected) })
	}
	a.setContent("Playlist", centeredText("Playlist", "Loading playlist..."))
	go func() {
		playlist, err := a.client.GetPlaylist(a.ctx, id)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.showError("Playlist", err)
				return
			}
			list := a.newFilterableList(appFocusContent)
			list.SetBorder(true)
			setViewTitle(list, "Playlists", playlist.Name)
			for i, song := range playlist.Entries {
				i, song := i, song
				list.AddItem(fmt.Sprintf("%s :: %s [%s]", song.Title, song.Artist, models.SecondsAsMMSS(song.Duration)), "", 0, func() {
					go a.report(a.player.PlayPlaylist(a.ctx, playlist.ID, i))
				})
			}
			list.SetListInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
				if event.Key() == tcell.KeyCtrlM {
					index := list.GetCurrentItem()
					if index >= 0 && index < len(playlist.Entries) {
						a.player.AddToCurrentPlaylist(playlist.Entries[index])
						return nil
					}
				}
				return event
			})
			list.SetCurrentItem(selected)
			a.setContent("Playlist", list)
		})
	}()
}

func (a *App) showSearch(push bool) {
	if push {
		a.pushHistory(func() { a.showSearch(false) })
	}

	query := ""
	input := tview.NewInputField().
		SetLabel("Search: ").
		SetFieldWidth(0).
		SetChangedFunc(func(text string) { query = text })
	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter && strings.TrimSpace(query) != "" {
			a.runSearch(strings.TrimSpace(query))
		}
	})

	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(input, 3, 0, true).
		AddItem(centeredText("Search", "Type a query and press Enter."), 0, 1, false)
	root.SetBorder(true)
	setViewTitle(root, "Search")
	a.setContentWithFocus("Search", root, input)
}

func (a *App) runSearch(query string) {
	a.setContent("Search", centeredText("Search", "Searching..."))
	go func() {
		result, err := a.client.Search(a.ctx, query, 100)
		a.app.QueueUpdateDraw(func() {
			if err != nil {
				a.showError("Search", err)
				return
			}
			selections := searchSelections{}
			a.replaceHistoryTop(func() { a.renderSearchResults(query, result, searchPaneArtists, selections) })
			a.renderSearchResults(query, result, searchPaneArtists, selections)
		})
	}()
}

func (a *App) renderSearchResults(query string, result models.SearchResult, focusPane searchPane, selections searchSelections) {
	var artistsList, albumsList, songsList *tview.List
	var focusTargets []searchFocusTarget
	selectedPane := focusPane

	saveSearchState := func() {
		currentSelections := currentSearchSelections(artistsList, albumsList, songsList)
		a.replaceHistoryTop(func() { a.renderSearchResults(query, result, selectedPane, currentSelections) })
	}

	artistsItem := a.searchListOrEmpty("Artists", fmt.Sprintf("No artists found for %q", query), len(result.Artists), func(list *tview.List) {
		artistsList = list
		for _, artist := range result.Artists {
			artist := artist
			list.AddItem(artist.Name, "", 0, func() {
				saveSearchState()
				a.showArtist(artist.ID, true)
			})
		}
		setListCurrentItem(list, selections.artists)
		focusTargets = append(focusTargets, searchFocusTarget{pane: searchPaneArtists, list: list})
	})

	albumsItem := a.searchListOrEmpty("Albums", fmt.Sprintf("No albums found for %q", query), len(result.Albums), func(list *tview.List) {
		albumsList = list
		for _, album := range result.Albums {
			album := album
			list.AddItem(album.Artist+" :: "+album.Name, "", 0, func() {
				saveSearchState()
				a.showAlbum(album.ID, true)
			})
		}
		setListCurrentItem(list, selections.albums)
		focusTargets = append(focusTargets, searchFocusTarget{pane: searchPaneAlbums, list: list})
	})

	songsItem := a.searchListOrEmpty("Songs", fmt.Sprintf("No songs found for %q", query), len(result.Songs), func(list *tview.List) {
		songsList = list
		for _, song := range result.Songs {
			song := song
			list.AddItem(song.Artist+" :: "+song.Album+" :: "+song.Title, "", 0, func() {
				go a.report(a.player.PlayRadio(a.ctx, song.ID))
			})
		}
		setListCurrentItem(list, selections.songs)
		focusTargets = append(focusTargets, searchFocusTarget{pane: searchPaneSongs, list: list})
	})

	focusIndex := searchFocusIndex(focusTargets, focusPane)
	if len(focusTargets) > 0 {
		selectedPane = focusTargets[focusIndex].pane
	}
	updateSearchFocusStyles(focusTargets, selectedPane)

	top := tview.NewFlex().
		AddItem(artistsItem, 0, 1, len(result.Artists) > 0).
		AddItem(albumsItem, 0, 1, len(result.Albums) > 0)
	root := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(top, 0, 1, len(focusTargets) > 0).
		AddItem(songsItem, 0, 1, len(result.Songs) > 0)
	root.SetBorder(true)
	setViewTitle(root, "Search", "Results", query)

	setSearchFocus := func(index int) {
		if len(focusTargets) == 0 {
			return
		}
		if index < 0 {
			index = 0
		}
		if index >= len(focusTargets) {
			index = len(focusTargets) - 1
		}
		focusIndex = index
		selectedPane = focusTargets[focusIndex].pane
		updateSearchFocusStyles(focusTargets, selectedPane)
		a.contentFocus = focusTargets[focusIndex].list
		a.focusTarget = appFocusContent
		a.app.SetFocus(focusTargets[focusIndex].list)
		saveSearchState()
	}

	focus := tview.Primitive(root)
	if len(focusTargets) > 0 {
		focus = focusTargets[focusIndex].list
	}
	a.setContentWithFocus("Search", root, focus)
	a.contentMouseLists = searchMouseLists(focusTargets)
	a.contentReturn = func(back bool) bool {
		if len(focusTargets) == 0 {
			return false
		}
		if back {
			setSearchFocus(len(focusTargets) - 1)
		} else {
			setSearchFocus(0)
		}
		return true
	}
	a.contentTabHandler = func(back bool) bool {
		if len(focusTargets) <= 1 {
			return false
		}
		if back {
			if focusIndex == 0 {
				return false
			}
			setSearchFocus(focusIndex - 1)
			return true
		}
		if focusIndex == len(focusTargets)-1 {
			return false
		}
		setSearchFocus(focusIndex + 1)
		return true
	}
}

func (a *App) showNowPlaying(push bool) {
	view := newNowPlayingView()
	view.onKey = a.handleGlobalKey
	state := a.currentState
	if state.CurrentTrack == nil && a.player != nil {
		state = a.player.State()
	}
	view.SetState(state)
	a.playing = view
	if a.app != nil {
		a.playingReturnFocus = a.app.GetFocus()
	}
	if a.pages != nil {
		a.pages.AddPage(nowPlayingPageName, view, true, true)
	}
	if a.app != nil {
		a.app.SetFocus(view)
	}
	_ = push
}

func (a *App) hasNowPlayingOverlay() bool {
	return a.pages != nil && a.pages.HasPage(nowPlayingPageName)
}

func (a *App) closeNowPlaying() {
	if a.pages != nil {
		a.pages.RemovePage(nowPlayingPageName)
	}
	a.playing = nil
	if a.app == nil {
		return
	}
	if a.playingReturnFocus != nil {
		a.app.SetFocus(a.playingReturnFocus)
		a.playingReturnFocus = nil
		return
	}
	if a.content != nil {
		a.focusContent()
	}
}

func searchMouseLists(targets []searchFocusTarget) []*tview.List {
	lists := make([]*tview.List, 0, len(targets))
	for _, target := range targets {
		if target.list != nil {
			lists = append(lists, target.list)
		}
	}
	return lists
}

func (a *App) searchListOrEmpty(title, emptyText string, count int, fill func(*tview.List)) tview.Primitive {
	if count == 0 {
		return centeredText(title, emptyText)
	}
	list := a.newList(appFocusContent)
	list.SetBorder(true)
	setViewTitle(list, "Search", title)
	fill(list)
	return list
}

func (a *App) showSettings(push bool) {
	if push {
		a.pushHistory(func() { a.showSettings(false) })
	}

	view := newSettingsView(a, a.cfg)
	a.setContentWithFocus("System", view, view)
	a.contentOwnsTab = true
	view.startProbeNow("")
}

func (a *App) consumePlayerUpdates() {
	for {
		select {
		case state := <-a.player.Updates():
			a.app.QueueUpdateDraw(func() { a.renderState(state) })
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *App) renderState(state models.CurrentState) {
	a.currentState = state
	if a.playing != nil {
		a.playing.SetState(state)
	}
	if a.status == nil || a.queue == nil {
		return
	}

	a.status.SetState(state)
	if a.cover != nil {
		a.cover.SetState(state)
	}

	queueFocused := a.app.GetFocus() == a.queue || a.focusTarget == appFocusQueue
	queueSelection := a.queue.GetCurrentItem()
	a.queue.Clear()
	for i, song := range state.CurrentPlaylist.Entries {
		prefix := "  "
		if i == state.CurrentTrackIndex {
			prefix = "> "
		}
		a.queue.AddItem(prefix+song.Title+" ["+models.SecondsAsMMSS(song.Duration)+"]", "", 0, nil)
	}
	if queueFocused {
		setListCurrentItem(a.queue, queueSelection)
	} else if state.CurrentTrackIndex >= 0 && state.CurrentTrackIndex < a.queue.GetItemCount() {
		a.queue.SetCurrentItem(state.CurrentTrackIndex)
	}
}

func (a *App) setContent(name string, item tview.Primitive) {
	a.setContentWithFocus(name, item, item)
}

func (a *App) setContentWithFocus(name string, item tview.Primitive, focus tview.Primitive) {
	a.content.RemovePage("content")
	a.content.AddAndSwitchToPage("content", item, true)
	a.contentTabHandler = nil
	a.contentReturn = nil
	a.contentOwnsTab = false
	a.contentMouseLists = listsFromPrimitive(item)
	if focus == nil {
		focus = item
	}
	a.contentFocus = focus
	a.focusTarget = appFocusContent
	a.app.SetFocus(focus)
}

func (a *App) showError(title string, err error) {
	a.setContent(title, centeredText(title, err.Error()))
}

func (a *App) report(err error) {
	if err == nil {
		return
	}
	a.app.QueueUpdateDraw(func() {
		if a.status != nil {
			a.status.SetMessage("[red]" + err.Error() + "[-]")
		}
	})
}

func (a *App) apply(cfg models.Config) models.Config {
	cfg = config.WithDefaults(cfg)
	if a.applyConfig != nil {
		cfg = a.applyConfig(cfg)
	} else {
		cfg = a.client.Configure(cfg)
	}
	return cfg
}

func (a *App) modal(title, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			a.pages.RemovePage("modal")
		})
	a.pages.AddPage("modal", modal, true, true)
	_ = title
}

func (a *App) pushHistory(fn func()) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	a.history = append(a.history, fn)
}

func (a *App) replaceHistoryTop(fn func()) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	if len(a.history) == 0 {
		a.history = append(a.history, fn)
		return
	}
	a.history[len(a.history)-1] = fn
}

func (a *App) goBack() {
	a.historyMu.Lock()
	if len(a.history) <= 1 {
		a.historyMu.Unlock()
		return
	}
	a.history = a.history[:len(a.history)-1]
	fn := a.history[len(a.history)-1]
	a.historyMu.Unlock()
	fn()
}

func (a *App) stop() {
	a.cancel()
	a.app.Stop()
}

func setListCurrentItem(list *tview.List, index int) {
	if list == nil || list.GetItemCount() == 0 {
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= list.GetItemCount() {
		index = list.GetItemCount() - 1
	}
	list.SetCurrentItem(index)
}

func currentSearchSelections(artists, albums, songs *tview.List) searchSelections {
	return searchSelections{
		artists: currentListItem(artists),
		albums:  currentListItem(albums),
		songs:   currentListItem(songs),
	}
}

func currentListItem(list *tview.List) int {
	if list == nil || list.GetItemCount() == 0 {
		return 0
	}
	return list.GetCurrentItem()
}

func searchFocusIndex(targets []searchFocusTarget, pane searchPane) int {
	for i, target := range targets {
		if target.pane == pane {
			return i
		}
	}
	return 0
}

func updateSearchFocusStyles(targets []searchFocusTarget, focused searchPane) {
	for _, target := range targets {
		applyListFocusStyle(target.list, target.pane == focused)
	}
}

func listsFromPrimitive(item tview.Primitive) []*tview.List {
	if list, ok := item.(*tview.List); ok {
		return []*tview.List{list}
	}
	if list, ok := item.(*filterableList); ok {
		return []*tview.List{list.list}
	}
	return nil
}

func centeredText(title, text string) tview.Primitive {
	view := tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignCenter).
		SetText(text)
	view.SetBorder(true)
	setViewTitle(view, title)
	return view
}

func configureTheme() {
	tview.Styles = tview.Theme{
		PrimitiveBackgroundColor:    uiBackground,
		ContrastBackgroundColor:     uiField,
		MoreContrastBackgroundColor: uiAccent,
		BorderColor:                 uiBorder,
		TitleColor:                  uiTitle,
		GraphicsColor:               uiBorder,
		PrimaryTextColor:            uiText,
		SecondaryTextColor:          uiLabel,
		TertiaryTextColor:           uiMuted,
		InverseTextColor:            tcell.ColorBlack,
		ContrastSecondaryTextColor:  uiTitle,
	}
}

func styleForm(form *tview.Form) {
	form.SetBackgroundColor(uiBackground)
	form.SetBorderColor(uiBorder)
	form.SetTitleColor(uiTitle)
	form.SetItemPadding(1)
	form.SetLabelColor(uiLabel)
	form.SetFieldBackgroundColor(uiField)
	form.SetFieldTextColor(uiText)
	form.SetButtonBackgroundColor(uiAccent)
	form.SetButtonTextColor(tcell.ColorBlack)
	form.SetButtonActivatedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiTitle).Bold(true))
	for i := 0; i < form.GetFormItemCount(); i++ {
		switch item := form.GetFormItem(i).(type) {
		case *tview.InputField:
			item.SetLabelColor(uiLabel)
			item.SetFieldBackgroundColor(uiField)
			item.SetFieldTextColor(uiText)
			item.SetPlaceholderTextColor(uiMuted)
		case *tview.DropDown:
			item.SetLabelColor(uiLabel)
			item.SetFieldBackgroundColor(uiField)
			item.SetFieldTextColor(uiText)
			item.SetFocusedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiAccent).Bold(true))
			item.SetListStyles(
				tcell.StyleDefault.Foreground(uiText).Background(uiPanel),
				tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiAccent).Bold(true),
			)
		}
	}
}

func center(item tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(item, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}

func stateString(state models.CurrentState) string {
	if state.Buffering {
		return "Buffering"
	}
	if state.Playing {
		return "Playing"
	}
	if state.Stopped {
		return "Stopped"
	}
	return "Paused"
}

func progressBar(position, duration float64, width int) string {
	if width <= 0 {
		return "||"
	}
	fraction := 0.0
	if duration > 0 {
		fraction = position / duration
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction*float64(width) + 0.5)
	return "|" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "|"
}

func secondsLabel(seconds float64) string {
	if seconds < 0 {
		return "--:--"
	}
	return models.SecondsAsMMSS(int(seconds))
}

func durationLabel(seconds float64) string {
	if seconds <= 0 {
		return "--:--"
	}
	return secondsLabel(seconds)
}

func bufferLabel(state models.CurrentState) string {
	prefix := "Ready"
	if state.Buffering {
		prefix = "Buffering"
	}
	if state.CacheReady {
		return fmt.Sprintf("%s  Cached", prefix)
	}
	if state.BufferPercentKnown {
		return fmt.Sprintf("%s  Download %.0f%%", prefix, state.BufferedPercent)
	}
	return prefix
}

func boolText(v bool) string {
	if v {
		return "On"
	}
	return "Off"
}

func boolOption(v bool) int {
	if v {
		return 1
	}
	return 0
}

func audioBackendOption(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "miniaudio":
		return 1
	case "mpv":
		return 2
	default:
		return 0
	}
}

func endpointsToText(endpoints []models.Endpoint) string {
	parts := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint.URL) != "" {
			parts = append(parts, endpoint.URL)
		}
	}
	return strings.Join(parts, "; ")
}

func parseEndpoints(value string) []models.Endpoint {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == '\n' || r == ','
	})
	endpoints := make([]models.Endpoint, 0, len(fields))
	for i, field := range fields {
		url := strings.TrimRight(strings.TrimSpace(field), "/")
		if url == "" {
			continue
		}
		endpoints = append(endpoints, models.Endpoint{
			Name:    "Endpoint " + strconv.Itoa(i+1),
			URL:     url,
			Enabled: true,
		})
	}
	return endpoints
}

func hasLoginProfile(cfg models.Config) bool {
	account := cfg.Account
	if strings.TrimSpace(account.Username) == "" {
		return false
	}
	if account.UsePlaintext {
		if account.Password == "" {
			return false
		}
	} else if account.Password == "" && (account.Token == "" || account.Salt == "") {
		return false
	}
	for _, endpoint := range account.Endpoints {
		if endpoint.Enabled && strings.TrimSpace(endpoint.URL) != "" {
			return true
		}
	}
	return false
}
