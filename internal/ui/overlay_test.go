package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestTextInputsPassThroughHelpAndPaletteTriggers(t *testing.T) {
	app := &App{app: tview.NewApplication(), pages: tview.NewPages()}

	for _, focus := range []tview.Primitive{tview.NewInputField(), tview.NewTextArea()} {
		app.app.SetFocus(focus)
		for _, r := range []rune{'?', '？', ':', '：'} {
			event := tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
			if got := app.handleGlobalKey(event); got != event {
				t.Fatalf("%q in %T should pass through, got %v", r, focus, got)
			}
			if app.pages.HasPage(helpOverlayPageName) || app.pages.HasPage(commandPalettePageName) {
				t.Fatalf("%q in %T opened an overlay", r, focus)
			}
		}
	}
}

func TestSystemEditPopupBlocksHelpAndPaletteTriggers(t *testing.T) {
	app, _ := newOverlayTestApp()
	popup := tview.NewInputField()
	app.systemPopup = popup
	app.pages.AddPage(systemEditPageName, popup, true, true)
	app.app.SetFocus(popup)

	for _, r := range []rune{'?', '？', ':', '：'} {
		if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)); got != nil {
			t.Fatalf("%q should be consumed by System popup, got %v", r, got)
		}
		if app.pages.HasPage(helpOverlayPageName) || app.pages.HasPage(commandPalettePageName) {
			t.Fatalf("%q opened an overlay above System popup", r)
		}
	}
}

func TestNowPlayingOverlayAllowsHelpAndPaletteAboveIt(t *testing.T) {
	app, _ := newOverlayTestApp()
	app.showNowPlaying(false)
	nowPlaying := app.app.GetFocus()
	if _, ok := nowPlaying.(*nowPlayingView); !ok {
		t.Fatalf("focus after opening Now Playing = %T, want nowPlayingView", nowPlaying)
	}

	if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone)); got != nil {
		t.Fatalf("? over Now Playing should open Help, got %v", got)
	}
	if !app.pages.HasPage(helpOverlayPageName) || !app.pages.HasPage(nowPlayingPageName) {
		t.Fatal("Help should open above Now Playing without closing it")
	}
	app.helpOverlay.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(p tview.Primitive) { app.app.SetFocus(p) })
	if app.app.GetFocus() != nowPlaying {
		t.Fatalf("focus after Help over Now Playing = %T, want Now Playing", app.app.GetFocus())
	}

	if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, ':', tcell.ModNone)); got != nil {
		t.Fatalf(": over Now Playing should open Command Palette, got %v", got)
	}
	if !app.pages.HasPage(commandPalettePageName) || !app.pages.HasPage(nowPlayingPageName) {
		t.Fatal("Command Palette should open above Now Playing without closing it")
	}
	app.commandPalette.input.InputHandler()(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(p tview.Primitive) { app.app.SetFocus(p) })
	if app.app.GetFocus() != nowPlaying {
		t.Fatalf("focus after Command Palette over Now Playing = %T, want Now Playing", app.app.GetFocus())
	}
}

func TestHelpAndCommandPaletteMouseDoesNotLeakToUnderlyingLists(t *testing.T) {
	for _, openOverlay := range []func(*App){
		func(app *App) { app.showHelpOverlay() },
		func(app *App) { app.showCommandPalette() },
	} {
		app, _ := newOverlayTestApp()
		underlying := tview.NewList().ShowSecondaryText(false)
		underlying.SetBorder(true)
		underlying.SetRect(0, 0, 20, 5)
		underlying.AddItem("one", "", 0, nil)
		underlying.AddItem("two", "", 0, nil)
		underlying.SetCurrentItem(0)
		app.contentMouseLists = []*tview.List{underlying}
		openOverlay(app)

		event := tcell.NewEventMouse(1, 2, tcell.ButtonNone, tcell.ModNone)
		nextEvent, _ := app.handleMouseCapture(event, tview.MouseLeftClick)

		if nextEvent != nil {
			t.Fatalf("%T mouse should be consumed", app.app.GetFocus())
		}
		if got := underlying.GetCurrentItem(); got != 0 {
			t.Fatalf("underlying list selection = %d, want unchanged", got)
		}
	}
}

func newOverlayTestApp() (*App, tview.Primitive) {
	previousFocus := tview.NewTextView()
	app := &App{
		cancel:  func() {},
		app:     tview.NewApplication(),
		pages:   tview.NewPages(),
		content: tview.NewPages(),
		queue:   tview.NewList(),
	}
	app.pages.AddPage("main", tview.NewTextView(), true, true)
	app.content.AddPage("content", previousFocus, true, true)
	app.contentFocus = previousFocus
	app.app.SetFocus(previousFocus)
	return app, previousFocus
}

func overlayScreenText(screen tcell.SimulationScreen, width, height int) string {
	rows := make([]string, 0, height)
	for row := range height {
		rows = append(rows, strings.TrimRight(overlayScreenRowText(screen, row, width), " "))
	}
	return strings.Join(rows, "\n")
}

func overlayScreenRowText(screen tcell.SimulationScreen, row, width int) string {
	var b strings.Builder
	for x := range width {
		text, _, _ := screen.Get(x, row)
		if text == "" {
			text = " "
		}
		b.WriteString(text)
	}
	return b.String()
}
