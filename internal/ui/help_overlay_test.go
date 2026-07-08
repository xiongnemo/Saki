package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestHelpOverlayOpensFromHalfAndFullWidthQuestionMark(t *testing.T) {
	for _, r := range []rune{'?', '？'} {
		app, previousFocus := newOverlayTestApp()

		if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)); got != nil {
			t.Fatalf("%q should open Help and be consumed, got %v", r, got)
		}
		if !app.pages.HasPage(helpOverlayPageName) {
			t.Fatalf("%q did not add Help page", r)
		}
		if app.app.GetFocus() == previousFocus {
			t.Fatalf("%q did not move focus to Help", r)
		}
		if _, ok := app.app.GetFocus().(*helpOverlay); !ok {
			t.Fatalf("focus after %q = %T, want helpOverlay", r, app.app.GetFocus())
		}
	}
}

func TestHelpOverlayClosesAndRestoresFocus(t *testing.T) {
	for _, event := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyBackspace, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, '?', tcell.ModNone),
		tcell.NewEventKey(tcell.KeyRune, '？', tcell.ModNone),
	} {
		app, previousFocus := newOverlayTestApp()
		app.showHelpOverlay()
		overlay := app.helpOverlay
		if overlay == nil {
			t.Fatal("expected Help overlay")
		}

		handler := overlay.InputHandler()
		handler(event, func(p tview.Primitive) { app.app.SetFocus(p) })

		if app.pages.HasPage(helpOverlayPageName) {
			t.Fatalf("Help page still exists after %v", event)
		}
		if app.app.GetFocus() != previousFocus {
			t.Fatalf("focus after Help close = %T, want previous %T", app.app.GetFocus(), previousFocus)
		}
	}
}

func TestHelpOverlayGlobalCaptureClosesAndBlocksUnderlyingShortcuts(t *testing.T) {
	for _, r := range []rune{'?', '？'} {
		app, previousFocus := newOverlayTestApp()
		app.showHelpOverlay()

		for _, blocked := range []rune{'1', ':', '：'} {
			if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, blocked, tcell.ModNone)); got != nil {
				t.Fatalf("Help should consume %q while open, got %v", blocked, got)
			}
			if app.app.GetFocus() != app.helpOverlay {
				t.Fatalf("%q changed focus to %T", blocked, app.app.GetFocus())
			}
			if app.pages.HasPage(commandPalettePageName) {
				t.Fatalf("%q opened Command Palette above Help", blocked)
			}
		}

		if got := app.handleGlobalKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)); got != nil {
			t.Fatalf("%q should close Help through global capture and be consumed, got %v", r, got)
		}
		if app.pages.HasPage(helpOverlayPageName) {
			t.Fatalf("Help page still exists after global %q", r)
		}
		if app.app.GetFocus() != previousFocus {
			t.Fatalf("focus after global %q = %T, want previous %T", r, app.app.GetFocus(), previousFocus)
		}
	}
}

func TestHelpOverlayDrawsActionRegistryContent(t *testing.T) {
	app, _ := newOverlayTestApp()
	app.showHelpOverlay()
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	screen.SetSize(100, 30)
	app.helpOverlay.SetRect(0, 0, 100, 30)
	app.helpOverlay.Draw(screen)

	text := overlayScreenText(screen, 100, 30)
	for _, want := range []string{"Help", "Global commands", "Navigation  5  Now Playing", "System  ?, ？   Help", "Playback  Space  Play/Pause", "Quit  q  Quit", "Esc / Backspace / ? / ？  close"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Help overlay missing %q:\n%s", want, text)
		}
	}
}

func TestDrawStyledTextPreservesWideRuneColumns(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()

	drawStyledText(screen, 0, 0, 24, "System  ?, ？  Help", tcell.StyleDefault)

	if text, _, _ := screen.Get(14, 0); text == "H" {
		t.Fatal("wide question mark was treated as one terminal cell")
	}
	if text, _, _ := screen.Get(15, 0); text != "H" {
		t.Fatalf("cell 15 = %q, want H after wide question mark", text)
	}
}
