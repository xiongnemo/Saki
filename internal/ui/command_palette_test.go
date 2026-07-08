package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestCommandPaletteOpensFromHalfAndFullWidthColon(t *testing.T) {
	for _, r := range []rune{':', '：'} {
		app, previousFocus := newOverlayTestApp()

		if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)); got != nil {
			t.Fatalf("%q should open Command Palette and be consumed, got %v", r, got)
		}
		if !app.pages.HasPage(commandPalettePageName) {
			t.Fatalf("%q did not add Command Palette page", r)
		}
		if app.app.GetFocus() == previousFocus {
			t.Fatalf("%q did not move focus to Command Palette", r)
		}
		if app.commandPalette == nil || app.app.GetFocus() != app.commandPalette.input {
			t.Fatalf("focus after %q = %T, want palette input", r, app.app.GetFocus())
		}
	}
}

func TestCommandPaletteCloseBackspaceSemantics(t *testing.T) {
	app, previousFocus := newOverlayTestApp()
	app.showCommandPalette()
	palette := app.commandPalette
	if palette == nil {
		t.Fatal("expected Command Palette")
	}
	handler := palette.input.InputHandler()

	handler(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone), func(p tview.Primitive) { app.app.SetFocus(p) })
	if app.pages.HasPage(commandPalettePageName) {
		t.Fatal("Command Palette should close on Escape")
	}
	if app.app.GetFocus() != previousFocus {
		t.Fatalf("focus after Escape = %T, want previous %T", app.app.GetFocus(), previousFocus)
	}

	app.showCommandPalette()
	palette = app.commandPalette
	palette.input.SetText("now")
	handler = palette.input.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone), func(p tview.Primitive) { app.app.SetFocus(p) })
	if !app.pages.HasPage(commandPalettePageName) {
		t.Fatal("Command Palette should stay open when Backspace edits a non-empty query")
	}
	if got := palette.input.GetText(); got != "no" {
		t.Fatalf("Backspace edited query to %q, want no", got)
	}

	palette.input.SetText("")
	handler(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone), func(p tview.Primitive) { app.app.SetFocus(p) })
	if app.pages.HasPage(commandPalettePageName) {
		t.Fatal("Command Palette should close on Backspace when query is empty")
	}
}

func TestCommandPaletteInputNavigationMovesSelection(t *testing.T) {
	app, _ := newOverlayTestApp()
	app.showCommandPalette()
	palette := app.commandPalette
	if palette == nil {
		t.Fatal("expected Command Palette")
	}
	handler := palette.input.InputHandler()

	handler(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone), func(p tview.Primitive) { app.app.SetFocus(p) })
	if got := palette.list.GetCurrentItem(); got != 1 {
		t.Fatalf("palette selection after Down = %d, want 1", got)
	}
	handler(tcell.NewEventKey(tcell.KeyHome, 0, tcell.ModNone), func(p tview.Primitive) { app.app.SetFocus(p) })
	if got := palette.list.GetCurrentItem(); got != 0 {
		t.Fatalf("palette selection after Home = %d, want 0", got)
	}
}

func TestCommandPaletteFiltersAndExecutesNowPlaying(t *testing.T) {
	app, _ := newOverlayTestApp()
	app.showCommandPalette()
	palette := app.commandPalette
	if palette == nil {
		t.Fatal("expected Command Palette")
	}

	palette.input.SetText("now playing")
	if got := palette.list.GetItemCount(); got != 1 {
		t.Fatalf("filtered palette item count = %d, want Now Playing only", got)
	}
	handler := palette.input.InputHandler()
	handler(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone), func(p tview.Primitive) { app.app.SetFocus(p) })

	if app.pages.HasPage(commandPalettePageName) {
		t.Fatal("Command Palette should close before executing the action")
	}
	if !app.pages.HasPage(nowPlayingPageName) {
		t.Fatal("Now Playing action did not open the overlay")
	}
	if _, ok := app.app.GetFocus().(*nowPlayingView); !ok {
		t.Fatalf("focus after Now Playing action = %T, want nowPlayingView", app.app.GetFocus())
	}
}

func TestCommandPaletteRegistryFiltersMetadataAndExcludesItself(t *testing.T) {
	action := uiAction{title: "Example", group: "System", bindings: []string{"？"}}
	for _, query := range []string{"example", "SYSTEM", "？"} {
		if !actionMatchesQuery(action, strings.ToLower(query)) {
			t.Fatalf("query %q should match action metadata", query)
		}
	}
	for _, action := range paletteActions() {
		if action.id == actionCommandPalette {
			t.Fatal("Command Palette action should be excluded from palette results")
		}
	}
}

func TestCommandPaletteDrawsFilteredActionContent(t *testing.T) {
	app, _ := newOverlayTestApp()
	app.showCommandPalette()
	palette := app.commandPalette
	if palette == nil {
		t.Fatal("expected Command Palette")
	}
	palette.input.SetText("now")
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	screen.SetSize(100, 26)
	palette.SetRect(0, 0, 100, 26)
	palette.Draw(screen)

	text := overlayScreenText(screen, 100, 26)
	if !strings.Contains(text, "Command Palette") || !strings.Contains(text, "Now Playing") {
		t.Fatalf("Command Palette render missing title/action:\n%s", text)
	}
	if strings.Contains(text, "Artists") {
		t.Fatalf("Command Palette render should not include unfiltered Artists action:\n%s", text)
	}
}

func TestCommandPaletteMouseCapturePreservesClickSynthesisAndExecutesItem(t *testing.T) {
	app, _ := newOverlayTestApp()
	app.showCommandPalette()
	palette := app.commandPalette
	if palette == nil {
		t.Fatal("expected Command Palette")
	}
	palette.input.SetText("now playing")
	palette.SetRect(0, 0, 100, 26)
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(100, 26)
	palette.Draw(screen)
	if got := palette.list.GetItemCount(); got != 1 {
		t.Fatalf("filtered palette item count = %d, want Now Playing only", got)
	}
	palette.list.SetCurrentItem(0)
	listX, listY, _, _ := palette.list.GetRect()

	event := tcell.NewEventMouse(listX+1, listY, tcell.ButtonNone, tcell.ModNone)
	for _, action := range []tview.MouseAction{tview.MouseMove, tview.MouseLeftDown, tview.MouseLeftUp} {
		nextEvent, _ := app.handleMouseCapture(event, action)
		if nextEvent == nil {
			t.Fatalf("%v should pass through so tview can synthesize MouseLeftClick", action)
		}
	}
	nextEvent, _ := app.handleMouseCapture(event, tview.MouseLeftClick)
	if nextEvent != nil {
		t.Fatal("synthesized MouseLeftClick should be consumed by Command Palette")
	}
	if app.pages.HasPage(commandPalettePageName) {
		t.Fatal("Command Palette should close after clicked action executes")
	}
}
