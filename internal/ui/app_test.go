package ui

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/subsonic"
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

func TestSettingsStartsOnList(t *testing.T) {
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
	_, item := app.content.GetFrontPage()
	view, ok := item.(*settingsView)
	if !ok {
		t.Fatalf("settings page = %T, want settingsView", item)
	}
	if got := view.GetTitle(); got != " Saki :: System " {
		t.Fatalf("system title = %q", got)
	}
	if view.activeTab != systemTabSettings {
		t.Fatalf("initial tab = %v, want settings", view.activeTab)
	}
	focus, ok := app.contentFocus.(*settingsView)
	if !ok {
		t.Fatalf("content focus = %T, want settings view", app.contentFocus)
	}
	focus.Focus(func(p tview.Primitive) {
		app.app.SetFocus(p)
	})
	if app.app.GetFocus() != focus {
		t.Fatalf("settings focus = %T, want settings view", app.app.GetFocus())
	}
	if len(view.settingsRows) == 0 || !strings.Contains(view.settingsRows[0].label, "Endpoints") {
		t.Fatalf("first settings row = %+v, want Endpoints", view.settingsRows)
	}
}

func TestSettingsKeepsTabInsideSystem(t *testing.T) {
	app := &App{
		app:     tview.NewApplication(),
		content: tview.NewPages(),
		queue:   tview.NewList(),
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

	tab := tcell.NewEventKey(tcell.KeyTab, 0, tcell.ModNone)
	if got := app.handleGlobalKey(tab); got != tab {
		t.Fatal("settings should pass tab to the form")
	}
	if app.app.GetFocus() == app.queue || app.focusTarget != appFocusContent {
		t.Fatalf("settings tab moved focus to queue: focus=%T target=%v", app.app.GetFocus(), app.focusTarget)
	}
}

func TestSettingsListAndPingStayInsideSystem(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	view := newSettingsView(app, models.Config{
		Account: models.Account{Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}}},
		Settings: models.Settings{
			AudioBackend:               "miniaudio",
			AudioCacheMaxBytes:         2048 * 1024 * 1024,
			HealthCheckIntervalSeconds: 5,
			EndpointSwitchThreshold:    0.3,
		},
	})

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	screen.SetSize(56, 14)
	view.SetRect(0, 0, 56, 14)
	view.Draw(screen)

	if view.settingsListRect.width > view.contentRect.width {
		t.Fatalf("settings list width = %d, content width = %d", view.settingsListRect.width, view.contentRect.width)
	}
	if view.settingsListRect.x < view.contentRect.x || view.settingsListRect.x+view.settingsListRect.width > view.contentRect.x+view.contentRect.width {
		t.Fatalf("settings list overflows content: list=%+v content=%+v", view.settingsListRect, view.contentRect)
	}

	screen.SetSize(160, 20)
	view.SetRect(0, 0, 160, 20)
	view.Draw(screen)
	if view.statusRect.width != view.contentRect.width {
		t.Fatalf("ping width = %d, content width = %d", view.statusRect.width, view.contentRect.width)
	}
	if view.statusRect.y <= view.contentRect.y {
		t.Fatalf("ping should be below content: content=%+v ping=%+v", view.contentRect, view.statusRect)
	}
}

func TestSettingsRowsAlignLabelsAndUseThemeColor(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	view := newSettingsView(app, models.Config{
		Account: models.Account{Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}}},
		Settings: models.Settings{
			AudioBackend:               "miniaudio",
			AudioCacheMaxBytes:         2048 * 1024 * 1024,
			HealthCheckIntervalSeconds: 5,
			EndpointSwitchThreshold:    0.3,
		},
	})
	view.selectedRow = 1

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(96, 22)
	view.SetRect(0, 0, 96, 22)
	view.Draw(screen)

	labelX := view.settingsListRect.x
	labelY := view.settingsListRect.y
	ch, _, style, _ := screen.GetContent(labelX, labelY)
	if ch != 'E' {
		t.Fatalf("first label starts with %q, want E", ch)
	}
	fg, _, _ := style.Decompose()
	if fg != uiLabel {
		t.Fatalf("first label foreground = %v, want %v", fg, uiLabel)
	}

	valueX := view.settingsListRect.x + settingLabelWidth(view.settingsRows, view.settingsListRect.width)
	ch, _, _, _ = screen.GetContent(valueX, labelY)
	if ch != 'h' {
		t.Fatalf("first value starts at x=%d with %q, want h", valueX, ch)
	}
}

func TestSystemAboutIncludesProperties(t *testing.T) {
	client := subsonic.NewClient(nil)
	cfg := models.Config{
		Account: models.Account{
			Username:  "nemo",
			Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}},
		},
		Settings: models.Settings{
			AudioBackend:               "miniaudio",
			AudioCacheMaxBytes:         2048 * 1024 * 1024,
			HealthCheckIntervalSeconds: 5,
			EndpointSwitchThreshold:    0.3,
		},
	}
	client.Configure(cfg)
	app := &App{app: tview.NewApplication(), client: client, cfg: cfg}
	view := newSettingsView(app, cfg)

	view.setActiveTab(systemTabAbout, func(p tview.Primitive) { app.app.SetFocus(p) })
	about := view.aboutText()
	for _, want := range []string{"This is Saki", "Subsonic Audio Klient for Individuals", "running on", runtime.GOOS + "/" + runtime.GOARCH, "♪&♥", "Nemo Xiong", "https://github.com/xiongnemo/Saki", "https://music.example", "Resolved Config", "Media Support", "Config path", "Username", "Active backend", "Configured decoders", "Audio output"} {
		if !strings.Contains(about, want) {
			t.Fatalf("about text missing %q: %q", want, about)
		}
	}
	for _, unwanted := range []string{"Connected endpoint:", "Current track", "Audio input", "No track loaded", "Configured backend"} {
		if strings.Contains(about, unwanted) {
			t.Fatalf("about text contains removed field %q: %q", unwanted, about)
		}
	}
}

func TestSystemTabShortcutsSwitchTabs(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	view := newSettingsView(app, models.Config{})
	handler := view.InputHandler()
	if handler == nil {
		t.Fatal("expected input handler")
	}
	setFocus := func(p tview.Primitive) { app.app.SetFocus(p) }

	handler(tcell.NewEventKey(tcell.KeyRune, '1', tcell.ModNone), setFocus)
	if view.activeTab != systemTabAbout {
		t.Fatalf("tab after 1 = %v, want about", view.activeTab)
	}
	handler(tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone), setFocus)
	if view.activeTab != systemTabSettings {
		t.Fatalf("tab after right = %v, want settings", view.activeTab)
	}
	handler(tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone), setFocus)
	if view.activeTab != systemTabAbout {
		t.Fatalf("tab after left = %v, want about", view.activeTab)
	}
	handler(tcell.NewEventKey(tcell.KeyRune, '2', tcell.ModNone), setFocus)
	if view.activeTab != systemTabSettings {
		t.Fatalf("tab after 2 = %v, want settings", view.activeTab)
	}
}

func TestSettingsEnterOpensEditPopup(t *testing.T) {
	app := &App{
		app:   tview.NewApplication(),
		pages: tview.NewPages(),
		cfg: models.Config{
			Account: models.Account{Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}}},
		},
	}
	view := newSettingsView(app, app.cfg)
	app.pages.AddPage("main", view, true, true)
	app.app.SetFocus(view)

	handler := view.InputHandler()
	if handler == nil {
		t.Fatal("expected settings input handler")
	}
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) {
		app.app.SetFocus(p)
	})

	name, _ := app.pages.GetFrontPage()
	if name != systemEditPageName {
		t.Fatalf("front page = %q, want edit popup", name)
	}
}

func TestSystemPopupBlocksGlobalNavigationAndEscapeCloses(t *testing.T) {
	app := &App{
		app:   tview.NewApplication(),
		pages: tview.NewPages(),
		queue: tview.NewList(),
		cfg: models.Config{
			Account: models.Account{Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}}},
		},
	}
	app.queue.AddItem("one", "", 0, nil)
	app.queue.AddItem("two", "", 0, nil)
	app.queue.SetCurrentItem(0)
	view := newSettingsView(app, app.cfg)
	app.contentFocus = view
	app.focusTarget = appFocusQueue
	app.pages.AddPage("main", view, true, true)
	view.openEndpointPopup()

	if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)); got != nil {
		t.Fatalf("popup key should be consumed, got %v", got)
	}
	if current := app.queue.GetCurrentItem(); current != 0 {
		t.Fatalf("queue moved behind popup to %d", current)
	}

	app.queue.SetRect(0, 0, 20, 5)
	event, _ := app.handleMouseCapture(tcell.NewEventMouse(1, 2, tcell.ButtonNone, tcell.ModNone), tview.MouseScrollDown)
	if event != nil {
		t.Fatalf("popup mouse should be consumed, got %v", event)
	}
	if current := app.queue.GetCurrentItem(); current != 0 {
		t.Fatalf("queue moved behind popup mouse to %d", current)
	}

	if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Fatalf("escape should close popup, got %v", got)
	}
	if app.pages.HasPage(systemEditPageName) {
		t.Fatal("popup page still exists after escape")
	}
	if app.app.GetFocus() != view {
		t.Fatalf("focus after popup close = %T, want settings view", app.app.GetFocus())
	}
}

func TestSystemTextPopupEscapeClosesItself(t *testing.T) {
	app := &App{
		app:   tview.NewApplication(),
		pages: tview.NewPages(),
		cfg: models.Config{
			Account: models.Account{Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}}},
		},
	}
	view := newSettingsView(app, app.cfg)
	app.pages.AddPage("main", view, true, true)
	view.openEndpointPopup()

	handler := app.systemPopup.InputHandler()
	if handler == nil {
		t.Fatal("expected popup input handler")
	}
	handler(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(p tview.Primitive) {
		app.app.SetFocus(p)
	})

	if app.pages.HasPage(systemEditPageName) {
		t.Fatal("popup page still exists after direct escape")
	}
	if app.app.GetFocus() != view {
		t.Fatalf("focus after direct popup escape = %T, want settings view", app.app.GetFocus())
	}
}

func TestSystemTextPopupUsesContentAnchorAndMultilineEditor(t *testing.T) {
	app := &App{
		app:   tview.NewApplication(),
		pages: tview.NewPages(),
		cfg: models.Config{
			Account: models.Account{Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}}},
		},
	}
	view := newSettingsView(app, app.cfg)
	app.pages.AddPage("main", view, true, true)

	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(120, 32)
	view.SetRect(0, 0, 80, 28)
	view.Draw(screen)
	view.openEndpointPopup()

	popup, ok := app.systemPopup.(*systemTextPopup)
	if !ok {
		t.Fatalf("system popup = %T, want text popup", app.systemPopup)
	}
	popup.SetRect(0, 0, 120, 32)
	popup.Draw(screen)

	if popup.popupRect.x < view.contentRect.x ||
		popup.popupRect.y < view.contentRect.y ||
		popup.popupRect.x+popup.popupRect.width > view.contentRect.x+view.contentRect.width ||
		popup.popupRect.y+popup.popupRect.height > view.contentRect.y+view.contentRect.height {
		t.Fatalf("popup rect %+v should stay inside content %+v", popup.popupRect, view.contentRect)
	}
	if popup.textRect.height <= 1 {
		t.Fatalf("text popup height = %d, want multiline editor", popup.textRect.height)
	}
}

func TestValidateEndpointIdentitiesRejectsDifferentLibraries(t *testing.T) {
	err := validateEndpointIdentities([]subsonic.EndpointIdentity{
		{Endpoint: models.Endpoint{URL: "https://one.example"}, Fingerprint: "one"},
		{Endpoint: models.Endpoint{URL: "https://two.example"}, Fingerprint: "two"},
	})
	if err == nil || !strings.Contains(err.Error(), "different libraries") {
		t.Fatalf("validation err = %v", err)
	}
	if err := validateEndpointIdentities([]subsonic.EndpointIdentity{
		{Endpoint: models.Endpoint{URL: "https://one.example"}, Fingerprint: "same"},
		{Endpoint: models.Endpoint{URL: "https://two.example"}, Fingerprint: "same"},
	}); err != nil {
		t.Fatalf("same library validation err = %v", err)
	}
	if err := validateEndpointIdentities([]subsonic.EndpointIdentity{
		{Endpoint: models.Endpoint{URL: "https://one.example"}, Err: context.Canceled},
		{Endpoint: models.Endpoint{URL: "https://two.example"}, Fingerprint: "same"},
	}); err == nil || !strings.Contains(err.Error(), "https://one.example") {
		t.Fatalf("endpoint error validation err = %v", err)
	}
}

func TestSettingsSaveStaysOnSettingsView(t *testing.T) {
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
		applyConfig: func(cfg models.Config) models.Config { return cfg },
	}
	app.showSettings(false)
	_, item := app.content.GetFrontPage()
	view, ok := item.(*settingsView)
	if !ok {
		t.Fatalf("settings page = %T, want settingsView", item)
	}
	view.saveConfig = func(models.Config) error { return nil }

	view.save()
	_, after := app.content.GetFrontPage()
	if after != view {
		t.Fatalf("save replaced settings page with %T", after)
	}
	if !strings.Contains(view.status.GetText(true), "Saved") {
		t.Fatalf("save status = %q, want Saved", view.status.GetText(true))
	}
}

func TestSettingsProbeStatusRendersResults(t *testing.T) {
	client := subsonic.NewClient(nil)
	cfg := models.Config{
		Account: models.Account{Endpoints: []models.Endpoint{{URL: "https://music.example", Enabled: true}}},
	}
	client.Configure(cfg)
	app := &App{app: tview.NewApplication(), client: client}
	view := newSettingsView(app, cfg)

	text := view.statusText("", cfg.Account.Endpoints, []subsonic.EndpointProbe{{
		Endpoint: cfg.Account.Endpoints[0],
		Latency:  12 * time.Millisecond,
	}}, false, "")
	if !strings.Contains(text, "OK") || !strings.Contains(text, "12ms") || !strings.Contains(text, "https://music.example") {
		t.Fatalf("probe status text = %q", text)
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

func TestNowPlayingShortcutShowsPage(t *testing.T) {
	track := &models.Song{ID: "song", Artist: "Artist", Album: "Album", Title: "Title", Duration: 180}
	previousFocus := tview.NewTextView()
	app := &App{
		app:   tview.NewApplication(),
		pages: tview.NewPages(),
		currentState: models.CurrentState{
			CurrentTrack:      track,
			CurrentTrackIndex: 0,
			CurrentPlaylist:   models.Playlist{Entries: []models.Song{*track}},
			Volume:            0.75,
		},
	}
	app.app.SetFocus(previousFocus)

	if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyCtrlO, 0, tcell.ModNone)); got != nil {
		t.Fatalf("Ctrl+O should be handled, got %v", got)
	}
	name, item := app.pages.GetFrontPage()
	if name != nowPlayingPageName {
		t.Fatalf("front page = %q, want %q", name, nowPlayingPageName)
	}
	view, ok := item.(*nowPlayingView)
	if !ok {
		t.Fatalf("overlay page = %T, want nowPlayingView", item)
	}
	if app.app.GetFocus() != view || app.playing != view {
		t.Fatalf("now playing focus/view not installed: focus=%T playing=%T", app.app.GetFocus(), app.playing)
	}
	if view.state.CurrentTrack == nil || view.state.CurrentTrack.Title != "Title" {
		t.Fatalf("now playing track = %+v, want Title", view.state.CurrentTrack)
	}

	if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)); got != nil {
		t.Fatalf("escape should be handled, got %v", got)
	}
	if app.pages.HasPage(nowPlayingPageName) {
		t.Fatal("now playing overlay should close on escape")
	}
	if app.app.GetFocus() != previousFocus {
		t.Fatalf("focus after close = %T, want previous focus", app.app.GetFocus())
	}
}

func TestRenderStateUpdatesNowPlayingView(t *testing.T) {
	app := &App{
		app:     tview.NewApplication(),
		queue:   tview.NewList(),
		status:  newPlayingView(),
		playing: newNowPlayingView(),
	}
	track := &models.Song{ID: "song", Artist: "Artist", Album: "Album", Title: "New Title", Duration: 60}

	app.renderState(models.CurrentState{
		CurrentTrack:      track,
		CurrentTrackIndex: 0,
		CurrentPlaylist:   models.Playlist{Entries: []models.Song{*track}},
		Volume:            1,
	})

	if app.currentState.CurrentTrack != track {
		t.Fatalf("app current state track = %p, want %p", app.currentState.CurrentTrack, track)
	}
	if app.playing.state.CurrentTrack != track {
		t.Fatalf("now playing state track = %p, want %p", app.playing.state.CurrentTrack, track)
	}
}

func TestNowPlayingViewResponsiveLayouts(t *testing.T) {
	state := models.CurrentState{
		CurrentTrack: &models.Song{ID: "song-2", Artist: "Artist", Album: "Album", Title: "Title", Duration: 240},
		Position:     42,
		Playing:      true,
		Volume:       0.65,
		AudioInfo:    models.AudioInfo{Codec: "flac", BitDepth: 24, SampleRate: 48000},
		CurrentPlaylist: models.Playlist{Entries: []models.Song{
			{ID: "song-1", Artist: "Artist", Title: "Previous", Duration: 120},
			{ID: "song-2", Artist: "Artist", Title: "Title", Duration: 240},
			{ID: "song-3", Artist: "Artist", Title: "Next", Duration: 180},
		}},
		CurrentTrackIndex: 1,
	}

	wide := newNowPlayingView()
	wide.SetState(state)
	wideScreen := tcell.NewSimulationScreen("")
	if err := wideScreen.Init(); err != nil {
		t.Fatal(err)
	}
	defer wideScreen.Fini()
	wideScreen.SetSize(100, 28)
	wide.SetRect(0, 0, 100, 28)
	wide.Draw(wideScreen)
	if wide.stacked {
		t.Fatal("wide layout should not stack")
	}
	if wide.coverRect.width == 0 || wide.infoRect.width == 0 {
		t.Fatalf("wide layout missing cover/info: cover=%+v info=%+v", wide.coverRect, wide.infoRect)
	}
	if !strings.Contains(strings.Join(wide.lastRows, "\n"), "Title: Title") {
		t.Fatalf("wide rows missing track title: %q", wide.lastRows)
	}
	if strings.Contains(strings.Join(wide.lastRows, "\n"), "Queue Context") {
		t.Fatalf("now playing rows should not include queue context: %q", wide.lastRows)
	}

	narrow := newNowPlayingView()
	narrow.SetState(state)
	narrowScreen := tcell.NewSimulationScreen("")
	if err := narrowScreen.Init(); err != nil {
		t.Fatal(err)
	}
	defer narrowScreen.Fini()
	narrowScreen.SetSize(50, 20)
	narrow.SetRect(0, 0, 50, 20)
	narrow.Draw(narrowScreen)
	if !narrow.stacked {
		t.Fatal("narrow layout should stack")
	}
	if narrow.coverRect.y <= narrow.infoRect.y {
		t.Fatalf("narrow cover should be below info: cover=%+v info=%+v", narrow.coverRect, narrow.infoRect)
	}
}

func TestNowPlayingInputHandlerRoutesGlobalKeys(t *testing.T) {
	app := &App{
		app:     tview.NewApplication(),
		pages:   tview.NewPages(),
		playing: newNowPlayingView(),
	}
	app.playing.onKey = app.handleGlobalKey
	app.pages.AddPage(nowPlayingPageName, app.playing, true, true)
	handler := app.playing.InputHandler()
	if handler == nil {
		t.Fatal("expected now playing input handler")
	}

	handler(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone), func(tview.Primitive) {})
	if app.pages.HasPage(nowPlayingPageName) {
		t.Fatal("now playing overlay should close on backspace")
	}
}

func TestFilterableListFiltersAndActivatesOriginalItem(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	list := app.newFilterableList(appFocusContent)
	called := -1
	list.AddItem("Alpha", "", 0, func() { called = list.GetCurrentItem() })
	list.AddItem("Beta", "", 0, func() { called = list.GetCurrentItem() })
	list.AddItem("Gamma", "", 0, func() { called = list.GetCurrentItem() })
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
	if called != 1 {
		t.Fatalf("selected action called with %d, want original index 1", called)
	}
}

func TestFilterableListInputArrowsMoveFilteredSelection(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	list := app.newFilterableList(appFocusContent)
	list.AddItem("Alpha", "", 0, nil)
	list.AddItem("Beta", "", 0, nil)
	list.AddItem("Gamma", "", 0, nil)
	list.SetCurrentItem(0)

	handler := list.list.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyRune, '/', tcell.ModNone), nil)
	list.input.SetText("a")

	inputHandler := list.input.InputHandler()
	inputHandler(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), nil)
	if got := list.list.GetCurrentItem(); got != 1 {
		t.Fatalf("filtered down current item = %d, want 1", got)
	}
	inputHandler(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone), nil)
	if got := list.list.GetCurrentItem(); got != 0 {
		t.Fatalf("filtered up current item = %d, want 0", got)
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
	nextEvent, _ := app.handleMouseCapture(event, tview.MouseLeftClick)
	if nextEvent != nil {
		t.Fatal("single click should be consumed")
	}
	if called != -1 {
		t.Fatalf("single click selected index = %d, want no activation", called)
	}
	nextEvent, _ = app.handleMouseCapture(event, tview.MouseLeftDoubleClick)
	if nextEvent != nil {
		t.Fatal("double click should be consumed after dispatching Enter behavior")
	}
	if called != 1 {
		t.Fatalf("selected index = %d, want 1", called)
	}
}

func TestListMouseDoubleClickRequiresSamePriorItem(t *testing.T) {
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

	first := tcell.NewEventMouse(1, 1, tcell.ButtonNone, tcell.ModNone)
	second := tcell.NewEventMouse(1, 2, tcell.ButtonNone, tcell.ModNone)
	app.handleMouseCapture(first, tview.MouseLeftClick)
	nextEvent, _ := app.handleMouseCapture(second, tview.MouseLeftDoubleClick)
	if nextEvent != nil {
		t.Fatal("double click should be consumed")
	}
	if called != -1 {
		t.Fatalf("different-item double click activated index %d", called)
	}
	if got := list.GetCurrentItem(); got != 1 {
		t.Fatalf("current item = %d, want selected second item", got)
	}
}

func TestListMouseDoubleClickWithoutPriorClickDoesNotRunEnter(t *testing.T) {
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
		t.Fatal("double click should be consumed")
	}
	if called != -1 {
		t.Fatalf("double click without prior click activated index %d", called)
	}
	if got := list.GetCurrentItem(); got != 1 {
		t.Fatalf("current item = %d, want selected second item", got)
	}
}

func TestListMouseScrollMovesSelectionWithViewport(t *testing.T) {
	app := &App{app: tview.NewApplication()}
	list := tview.NewList().ShowSecondaryText(false)
	list.SetBorder(true)
	list.SetRect(0, 0, 20, 5)
	for i := 0; i < 10; i++ {
		list.AddItem("item", "", 0, nil)
	}
	app.contentMouseLists = []*tview.List{list}

	event := tcell.NewEventMouse(1, 2, tcell.ButtonNone, tcell.ModNone)
	nextEvent, _ := app.handleMouseCapture(event, tview.MouseScrollDown)
	if nextEvent != nil {
		t.Fatal("scroll should be consumed")
	}
	if offset, _ := list.GetOffset(); offset != 1 {
		t.Fatalf("offset after scroll down = %d, want 1", offset)
	}
	if got := list.GetCurrentItem(); got != 1 {
		t.Fatalf("current item after scroll down = %d, want 1", got)
	}
	app.handleMouseCapture(event, tview.MouseScrollUp)
	if offset, _ := list.GetOffset(); offset != 0 {
		t.Fatalf("offset after scroll up = %d, want 0", offset)
	}
	if got := list.GetCurrentItem(); got != 0 {
		t.Fatalf("current item after scroll up = %d, want 0", got)
	}
}

func TestPassivePanelsDoNotTakeMouseFocus(t *testing.T) {
	initial := tview.NewList()
	app := &App{
		app:    tview.NewApplication(),
		status: newPlayingView(),
		help:   tview.NewTextView(),
	}
	app.status.SetRect(0, 5, 20, 3)
	app.help.SetRect(0, 8, 20, 3)
	app.app.SetFocus(initial)

	event := tcell.NewEventMouse(1, 9, tcell.ButtonNone, tcell.ModNone)
	nextEvent, _ := app.handleMouseCapture(event, tview.MouseLeftDown)
	if nextEvent != nil {
		t.Fatal("passive panel mouse down should be consumed")
	}
	if app.app.GetFocus() != initial {
		t.Fatalf("focus = %T, want initial list", app.app.GetFocus())
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

func TestAudioInfoLabelUsesResponsiveFallbacks(t *testing.T) {
	info := models.AudioInfo{Codec: "flac", BitDepth: 16, SampleRate: 44100, BitRateKbps: 880, Channels: 2}
	if got := audioInfoLabel(info, 17); got != "FLAC 44.1/16 880k" {
		t.Fatalf("full audio info label = %q", got)
	}
	if got := audioInfoLabel(info, 12); got != "FLAC 44.1/16" {
		t.Fatalf("medium audio info label = %q", got)
	}
	if got := audioInfoLabel(info, 4); got != "FLAC" {
		t.Fatalf("small audio info label = %q", got)
	}
	if got := audioInfoLabel(info, 3); got != "" {
		t.Fatalf("too-small audio info label = %q", got)
	}
}

func TestPlayingLeftTextIncludesAudioInfo(t *testing.T) {
	state := models.CurrentState{
		CacheReady: true,
		AudioInfo:  models.AudioInfo{Codec: "ALAC", BitDepth: 24, SampleRate: 48000, BitRateKbps: 921},
	}
	if got := playingLeftText(state, 80); got != "Stream Ready  Cached  ALAC 48/24 921k" {
		t.Fatalf("playing left text = %q", got)
	}
	if got := playingLeftText(state, len("Stream Ready  Cached  ALAC")-1); got != "Stream Ready  Cached" {
		t.Fatalf("narrow playing left text = %q", got)
	}
}

func TestControlsHelpMentionsViewSearch(t *testing.T) {
	if !strings.Contains(controlsHelpText, "/ Search View") {
		t.Fatalf("controls help missing view search shortcut: %q", controlsHelpText)
	}
	if !strings.Contains(controlsViewHelpText, "C-o Playing") {
		t.Fatalf("controls help missing now playing shortcut: %q", controlsViewHelpText)
	}
	if !strings.Contains(controlsHelpText, "\n") {
		t.Fatalf("controls help should be split into two lines: %q", controlsHelpText)
	}
	if !strings.Contains(controlsViewHelpText, "C-a Artists") || strings.Contains(controlsViewHelpText, "Play/Pause") {
		t.Fatalf("view controls line is not view-specific: %q", controlsViewHelpText)
	}
	if !strings.Contains(controlsPlaybackHelpText, "Play/Pause") || strings.Contains(controlsPlaybackHelpText, "Artists") {
		t.Fatalf("playback controls line is not playback-specific: %q", controlsPlaybackHelpText)
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
