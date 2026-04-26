package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/models"
)

func TestProgressBar(t *testing.T) {
	if got := progressBar(5, 10, 10); got != "|#####-----|" {
		t.Fatalf("progressBar = %q", got)
	}
	if got := progressBar(15, 10, 10); got != "|##########|" {
		t.Fatalf("clamped progressBar = %q", got)
	}
	if got := progressBar(1, 0, 4); got != "|----|" {
		t.Fatalf("unknown duration progressBar = %q", got)
	}
}

func TestBufferLabel(t *testing.T) {
	state := models.CurrentState{
		BufferedPercent:    42,
		BufferPercentKnown: true,
		BufferedSeconds:    8.25,
	}
	if got := bufferLabel(state); got != "Ready  Download 42%" {
		t.Fatalf("bufferLabel = %q", got)
	}

	state.Buffering = true
	state.BufferPercentKnown = false
	if got := bufferLabel(state); got != "Buffering" {
		t.Fatalf("unknown buffering label = %q", got)
	}

	state.Buffering = false
	state.CacheReady = true
	if got := bufferLabel(state); got != "Ready  Cached" {
		t.Fatalf("cache-ready label = %q", got)
	}
}

func TestViewTitle(t *testing.T) {
	if got := viewTitle("Artists", "EA", "Albums", "The Sims"); got != "Saki :: Artists :: EA :: Albums :: The Sims" {
		t.Fatalf("viewTitle = %q", got)
	}
	if got := viewTitle("", "Queue"); got != "Saki :: Queue" {
		t.Fatalf("viewTitle skips blanks = %q", got)
	}
	box := tview.NewBox()
	setPlainTitle(box, " Queue ")
	if got := box.GetTitle(); got != " Queue " {
		t.Fatalf("plain title = %q", got)
	}
	playing := newPlayingView()
	playing.SetState(models.CurrentState{Playing: true})
	if got := playing.GetTitle(); got != " Playing " {
		t.Fatalf("playing title = %q", got)
	}
	setViewTitle(box, "Artists")
	if got := box.GetTitle(); got != " Saki :: Artists " {
		t.Fatalf("view title with padding = %q", got)
	}
}

func TestApplyListFocusStyleDrawsFocusedAndUnfocused(t *testing.T) {
	list := tview.NewList().ShowSecondaryText(false)
	list.AddItem("alpha", "", 0, nil)
	list.SetHighlightFullLine(true)

	_, focusedBG, _ := selectedCellStyle(t, list, true).Decompose()
	if focusedBG != uiAccent {
		t.Fatalf("focused selected background = %v, want %v", focusedBG, uiAccent)
	}

	_, unfocusedBG, _ := selectedCellStyle(t, list, false).Decompose()
	if unfocusedBG != uiField {
		t.Fatalf("unfocused selected background = %v, want %v", unfocusedBG, uiField)
	}
}

func TestFocusTraversalTogglesContentAndQueue(t *testing.T) {
	app := &App{
		app:         tview.NewApplication(),
		content:     tview.NewPages(),
		queue:       tview.NewList(),
		focusTarget: appFocusContent,
	}
	content := tview.NewTextView()
	app.contentFocus = content
	app.app.SetFocus(content)

	if !app.handleFocusTraversal(false) || app.app.GetFocus() != app.queue {
		t.Fatalf("first tab focus = %T, want queue", app.app.GetFocus())
	}
	if !app.handleFocusTraversal(false) || app.app.GetFocus() != content {
		t.Fatalf("second tab focus = %T, want content", app.app.GetFocus())
	}
}

func TestFocusTraversalPreservesContentSubFocus(t *testing.T) {
	app := &App{
		app:         tview.NewApplication(),
		content:     tview.NewPages(),
		queue:       tview.NewList(),
		focusTarget: appFocusContent,
	}
	first := tview.NewList()
	second := tview.NewList()
	app.contentFocus = first
	app.app.SetFocus(first)
	usedSubFocus := false
	app.contentTabHandler = func(bool) bool {
		usedSubFocus = true
		app.contentFocus = second
		app.app.SetFocus(second)
		return true
	}

	if !app.handleFocusTraversal(false) || !usedSubFocus || app.app.GetFocus() != second {
		t.Fatalf("search subfocus was not preserved")
	}
	app.contentTabHandler = nil
	if !app.handleFocusTraversal(false) || app.app.GetFocus() != app.queue {
		t.Fatalf("tab after subfocus = %T, want queue", app.app.GetFocus())
	}
	if !app.handleFocusTraversal(false) || app.app.GetFocus() != second {
		t.Fatalf("queue tab returned to %T, want remembered subfocus", app.app.GetFocus())
	}
}

func TestFocusTraversalUsesContentReturnFromQueue(t *testing.T) {
	app := &App{
		app:         tview.NewApplication(),
		content:     tview.NewPages(),
		queue:       tview.NewList(),
		focusTarget: appFocusQueue,
	}
	first := tview.NewList()
	second := tview.NewList()
	app.contentFocus = second
	app.app.SetFocus(app.queue)
	app.contentReturn = func(back bool) bool {
		if back {
			app.contentFocus = second
			app.app.SetFocus(second)
		} else {
			app.contentFocus = first
			app.app.SetFocus(first)
		}
		app.focusTarget = appFocusContent
		return true
	}

	if !app.handleFocusTraversal(false) || app.app.GetFocus() != first {
		t.Fatalf("forward return focus = %T, want first search pane", app.app.GetFocus())
	}
	app.focusTarget = appFocusQueue
	app.app.SetFocus(app.queue)
	if !app.handleFocusTraversal(true) || app.app.GetFocus() != second {
		t.Fatalf("backward return focus = %T, want last search pane", app.app.GetFocus())
	}
}

func TestSearchTabCycleReturnsFromQueueToFirstAvailablePane(t *testing.T) {
	app := &App{
		app:     tview.NewApplication(),
		content: tview.NewPages(),
		queue:   tview.NewList(),
	}
	result := models.SearchResult{
		Albums: []models.Album{{ID: "album-1", Artist: "Artist", Name: "Album"}},
		Songs:  []models.Song{{ID: "song-1", Artist: "Artist", Album: "Album", Title: "Song"}},
	}

	app.renderSearchResults("query", result, searchPaneAlbums, searchSelections{})
	albumList := app.contentFocus
	if albumList == nil {
		t.Fatal("expected initial album focus")
	}
	if !app.handleFocusTraversal(false) {
		t.Fatal("expected tab from album to song to be handled")
	}
	songList := app.contentFocus
	if songList == nil || songList == albumList {
		t.Fatalf("song focus = %T, want different focused pane", songList)
	}
	if !app.handleFocusTraversal(false) || app.app.GetFocus() != app.queue {
		t.Fatalf("expected tab from song to queue, got %T", app.app.GetFocus())
	}
	if !app.handleFocusTraversal(false) || app.app.GetFocus() != albumList {
		t.Fatalf("expected tab from queue to return to album, got %T", app.app.GetFocus())
	}
	if !app.handleFocusTraversal(false) || app.app.GetFocus() != songList {
		t.Fatalf("expected tab from album to go back to song, got %T", app.app.GetFocus())
	}
}

func TestTextInputGetsBackspaceAndSpace(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	input := tview.NewInputField()
	app.app.SetFocus(input)

	backspace := tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone)
	if got := app.handleGlobalKey(backspace); got != backspace {
		t.Fatal("backspace in input should pass through")
	}
	space := tcell.NewEventKey(tcell.KeyRune, ' ', tcell.ModNone)
	if got := app.handleGlobalKey(space); got != space {
		t.Fatal("space in input should pass through")
	}
}

func TestEscapeGoesBackFromForm(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	called := false
	app.history = []func(){func() { called = true }, func() {}}
	app.app.SetFocus(tview.NewForm())

	if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Fatal("escape should be handled globally")
	}
	if !called {
		t.Fatal("expected escape to go back")
	}
}

func TestSettingsStartsOnAudioBackend(t *testing.T) {
	app := &App{
		app:     tview.NewApplication(),
		content: tview.NewPages(),
		cfg: models.Config{
			Account: models.Account{Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}}},
			Settings: models.Settings{
				AudioBackend:               "miniaudio",
				AudioCacheMaxBytes:         2048 * 1024 * 1024,
				HealthCheckIntervalSeconds: 5,
				EndpointSwitchThreshold:    0.3,
			},
		},
	}
	app.showSettings(false)
	form, ok := app.contentFocus.(*tview.Form)
	if !ok {
		t.Fatalf("content focus = %T, want form", app.contentFocus)
	}
	form.Focus(func(p tview.Primitive) {
		app.app.SetFocus(p)
	})
	if focus, ok := app.app.GetFocus().(*tview.DropDown); !ok || focus.GetLabel() != "Audio backend" {
		t.Fatalf("settings focus = %T %[1]v, want Audio backend dropdown", app.app.GetFocus())
	}
}

func TestHasLoginProfile(t *testing.T) {
	cfg := models.Config{Account: models.Account{
		Username: "nemo",
		Password: "secret",
		Endpoints: []models.Endpoint{{
			Name:    "server",
			URL:     "https://music.example",
			Enabled: true,
		}},
	}}
	if !hasLoginProfile(cfg) {
		t.Fatal("expected username/password/enabled endpoint to be a login profile")
	}
	cfg.Account.Password = ""
	if hasLoginProfile(cfg) {
		t.Fatal("expected missing password/token to block auto login")
	}
	cfg.Account.Token = "token"
	cfg.Account.Salt = "salt"
	if !hasLoginProfile(cfg) {
		t.Fatal("expected token/salt profile to auto login")
	}
	cfg.Account.Endpoints[0].Enabled = false
	if hasLoginProfile(cfg) {
		t.Fatal("expected disabled endpoints to block auto login")
	}
}

func TestRenderStatePreservesQueueSelectionWhileFocused(t *testing.T) {
	app := &App{
		app:    tview.NewApplication(),
		queue:  tview.NewList(),
		status: newPlayingView(),
	}
	app.queue.AddItem("old one", "", 0, nil)
	app.queue.AddItem("old two", "", 0, nil)
	app.queue.AddItem("old three", "", 0, nil)
	app.queue.SetCurrentItem(2)
	app.app.SetFocus(app.queue)

	state := models.CurrentState{
		CurrentTrackIndex: 0,
		CurrentPlaylist: models.Playlist{Entries: []models.Song{
			{Title: "one", Duration: 60},
			{Title: "two", Duration: 60},
			{Title: "three", Duration: 60},
		}},
	}
	app.renderState(state)
	if got := app.queue.GetCurrentItem(); got != 2 {
		t.Fatalf("focused queue selection = %d, want 2", got)
	}

	app.app.SetFocus(tview.NewTextView())
	state.CurrentTrackIndex = 1
	app.renderState(state)
	if got := app.queue.GetCurrentItem(); got != 1 {
		t.Fatalf("unfocused queue selection = %d, want current track", got)
	}
}

func TestQueueSelectedFuncSurvivesRenderState(t *testing.T) {
	app := &App{
		app:    tview.NewApplication(),
		queue:  tview.NewList(),
		status: newPlayingView(),
	}
	called := -1
	app.queue.SetSelectedFunc(func(index int, _ string, _ string, _ rune) {
		called = index
	})
	app.renderState(models.CurrentState{
		CurrentPlaylist: models.Playlist{Entries: []models.Song{
			{Title: "one", Duration: 60},
			{Title: "two", Duration: 60},
		}},
	})
	handler := app.queue.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), nil)
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), nil)
	if called != 1 {
		t.Fatalf("queue selected func called with %d, want 1", called)
	}
}

func TestQueueFocusedKeysDispatchToList(t *testing.T) {
	app := &App{
		app:         tview.NewApplication(),
		queue:       tview.NewList(),
		focusTarget: appFocusQueue,
	}
	app.queue.AddItem("one", "", 0, nil)
	app.queue.AddItem("two", "", 0, nil)
	if !app.handleQueueKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)) {
		t.Fatal("expected queue key to be handled")
	}
	if got := app.queue.GetCurrentItem(); got != 1 {
		t.Fatalf("queue current item = %d, want 1", got)
	}
}

func TestQueuePanelPlacesCoverBelowQueueWithoutTakingFocus(t *testing.T) {
	app := &App{
		queue: tview.NewList(),
		cover: newCoverPreviewWithRenderer(coverRendererCell),
	}
	app.cover.SetState(models.CurrentState{CurrentTrack: &models.Song{ID: "song", Artist: "Artist", Album: "Album", Title: "Title"}})
	panel := app.queuePanel()
	panel.SetRect(0, 0, 36, 30)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(36, 30)
	panel.Draw(screen)

	_, queueY, _, queueHeight := app.queue.GetRect()
	coverX, coverY, coverWidth, coverHeight := app.cover.GetRect()
	if queueY != 0 || queueHeight != 11 {
		t.Fatalf("queue rect y/height = %d/%d, want 0/11", queueY, queueHeight)
	}
	if coverX != 0 || coverY != 11 || coverWidth != 36 || coverHeight != 19 {
		t.Fatalf("cover rect = %d,%d %dx%d, want 0,11 36x19", coverX, coverY, coverWidth, coverHeight)
	}

	panel.SetRect(0, 0, 46, 30)
	screen.SetSize(46, 30)
	panel.Draw(screen)
	_, _, _, queueHeight = app.queue.GetRect()
	coverX, coverY, coverWidth, coverHeight = app.cover.GetRect()
	if queueHeight != 6 {
		t.Fatalf("resized queue height = %d, want 6", queueHeight)
	}
	if coverX != 0 || coverY != 6 || coverWidth != 46 || coverHeight != 24 {
		t.Fatalf("resized cover rect = %d,%d %dx%d, want 0,6 46x24", coverX, coverY, coverWidth, coverHeight)
	}
}

func TestQueuePanelMouseHandlerIsSafeForFlexDispatch(t *testing.T) {
	queue := tview.NewList()
	queue.AddItem("one", "", 0, nil)
	panel := newQueueSidePanel(queue, newCoverPreviewWithRenderer(coverRendererCell))
	root := tview.NewFlex().AddItem(panel, 20, 0, false)
	root.SetRect(0, 0, 20, 10)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(20, 10)
	root.Draw(screen)

	handler := root.MouseHandler()
	if handler == nil {
		t.Fatal("expected root mouse handler")
	}
	handler(tview.MouseMove, tcell.NewEventMouse(1, 1, tcell.ButtonNone, tcell.ModNone), func(tview.Primitive) {})
	handler(tview.MouseMove, tcell.NewEventMouse(1, 8, tcell.ButtonNone, tcell.ModNone), func(tview.Primitive) {})
}

func TestRenderStateUpdatesCoverPreview(t *testing.T) {
	app := &App{
		app:    tview.NewApplication(),
		queue:  tview.NewList(),
		status: newPlayingView(),
		cover:  newCoverPreviewWithRenderer(coverRendererCell),
	}
	track := &models.Song{ID: "song", Artist: "Artist", Album: "Album", Title: "Title", Duration: 60}
	app.renderState(models.CurrentState{
		CurrentTrack:      track,
		CurrentTrackIndex: 0,
		CurrentPlaylist: models.Playlist{Entries: []models.Song{
			*track,
		}},
	})
	if app.cover.state.CurrentTrack != track {
		t.Fatalf("cover current track = %p, want %p", app.cover.state.CurrentTrack, track)
	}
}

func TestFilterableListFiltersAndConfirmsOriginalItem(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	list := app.newFilterableList(appFocusContent)
	list.AddItem("Alpha", "", 0, nil)
	list.AddItem("Beta", "", 0, nil)
	list.AddItem("Gamma", "", 0, nil)
	list.SetCurrentItem(2)

	handler := list.list.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone), nil)
	list.input.SetText("be")
	if got := list.list.GetItemCount(); got != 1 {
		t.Fatalf("filtered item count = %d, want 1", got)
	}
	if got := list.list.GetCurrentItem(); got != 0 {
		t.Fatalf("filtered current item = %d, want 0", got)
	}

	inputHandler := list.input.InputHandler()
	inputHandler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), nil)
	if got := list.list.GetItemCount(); got != 3 {
		t.Fatalf("confirmed item count = %d, want 3", got)
	}
	if got := list.GetCurrentItem(); got != 1 {
		t.Fatalf("confirmed original item = %d, want 1", got)
	}
}

func TestListMouseSingleClickFocusesAndSelectsOnly(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true)
	list.SetRect(0, 0, 20, 5)
	called := 0
	list.AddItem("one", "", 0, func() { called++ })
	list.AddItem("two", "", 0, func() { called++ })
	app.contentMouseLists = []*tview.List{list}

	event := tcell.NewEventMouse(1, 2, tcell.ButtonNone, tcell.ModNone)
	nextEvent, _ := app.handleMouseCapture(event, tview.MouseLeftClick)
	if nextEvent != nil {
		t.Fatal("single click should be consumed before tview list activates the item")
	}
	if app.app.GetFocus() != list {
		t.Fatalf("focus = %T, want clicked list", app.app.GetFocus())
	}
	if got := list.GetCurrentItem(); got != 1 {
		t.Fatalf("current item = %d, want 1", got)
	}
	if called != 0 {
		t.Fatalf("single click activated item %d times, want 0", called)
	}
}

func TestListMouseDoubleClickRunsEnterBehavior(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true)
	list.SetRect(0, 0, 20, 5)
	called := -1
	list.AddItem("one", "", 0, nil)
	list.AddItem("two", "", 0, nil)
	list.SetSelectedFunc(func(index int, _ string, _ string, _ rune) {
		called = index
	})
	app.contentMouseLists = []*tview.List{list}

	event := tcell.NewEventMouse(1, 2, tcell.ButtonNone, tcell.ModNone)
	nextEvent, _ := app.handleMouseCapture(event, tview.MouseLeftDoubleClick)
	if nextEvent != nil {
		t.Fatal("double click should be consumed after dispatching Enter behavior")
	}
	if called != 1 {
		t.Fatalf("selected index = %d, want 1", called)
	}
}

func TestPlayingProgressLineUsesAvailableWidth(t *testing.T) {
	line := playingProgressLine(5, 10, 40)
	if len(line) != 40 {
		t.Fatalf("progress line length = %d, want 40: %q", len(line), line)
	}
	if got := playingProgressLine(15, 10, 24); got != "Progress |#|  00:15 / 00:10" {
		t.Fatalf("clamped narrow progress line = %q", got)
	}
	if got := playingProgressLine(1, 0, 28); got != "Progress |--|  00:01 / --:--" {
		t.Fatalf("unknown duration progress line = %q", got)
	}
}

func TestPlayingStatusLabelsUseStableWidths(t *testing.T) {
	if len(statusBoolText(true)) != len(statusBoolText(false)) {
		t.Fatalf("shuffle label widths differ: %q vs %q", statusBoolText(true), statusBoolText(false))
	}
	if got := statusBoolText(true); got != "On " {
		t.Fatalf("true status label = %q, want padded On", got)
	}
	if volumeStatusWidth(1) != volumeStatusWidth(0.95) || volumeStatusWidth(1) != volumeStatusWidth(0) {
		t.Fatalf("volume status widths differ: 100=%d 95=%d 0=%d", volumeStatusWidth(1), volumeStatusWidth(0.95), volumeStatusWidth(0))
	}
	if got := volumeStatusText(0.95); !strings.HasSuffix(got, "  95%") {
		t.Fatalf("volume 95 text = %q, want fixed-width percentage", got)
	}
}

func TestControlsHelpMentionsViewSearch(t *testing.T) {
	if !strings.Contains(controlsHelpText, "/ Search View") {
		t.Fatalf("controls help missing view search shortcut: %q", controlsHelpText)
	}
}

func TestSearchFocusIndex(t *testing.T) {
	targets := []searchFocusTarget{
		{pane: searchPaneAlbums, list: tview.NewList()},
		{pane: searchPaneSongs, list: tview.NewList()},
	}
	if got := searchFocusIndex(targets, searchPaneSongs); got != 1 {
		t.Fatalf("searchFocusIndex = %d, want 1", got)
	}
	if got := searchFocusIndex(targets, searchPaneArtists); got != 0 {
		t.Fatalf("missing pane fallback = %d, want 0", got)
	}
}

func TestSetListCurrentItemClamps(t *testing.T) {
	list := tview.NewList()
	list.AddItem("a", "", 0, nil)
	list.AddItem("b", "", 0, nil)

	setListCurrentItem(list, 10)
	if got := list.GetCurrentItem(); got != 1 {
		t.Fatalf("clamped high index = %d, want 1", got)
	}
	setListCurrentItem(list, -10)
	if got := list.GetCurrentItem(); got != 0 {
		t.Fatalf("clamped low index = %d, want 0", got)
	}
}

func selectedCellStyle(t *testing.T, list *tview.List, focused bool) tcell.Style {
	t.Helper()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	screen.SetSize(20, 3)
	applyListFocusStyle(list, focused)
	list.SetRect(0, 0, 20, 3)
	list.Draw(screen)
	_, _, style, _ := screen.GetContent(0, 0)
	return style
}
