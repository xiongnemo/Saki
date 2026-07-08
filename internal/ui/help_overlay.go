package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const helpOverlayPageName = "help-overlay"

type helpOverlay struct {
	*tview.Box

	actions []uiAction
	close   func()
}

func newHelpOverlay(actions []uiAction, close func()) *helpOverlay {
	view := &helpOverlay{
		Box:     tview.NewBox(),
		actions: actions,
		close:   close,
	}
	view.SetBorder(false)
	view.SetBackgroundColor(uiBackground)
	return view
}

func (h *helpOverlay) Draw(screen tcell.Screen) {
	h.Box.DrawForSubclass(screen, h)
	x, y, width, height := h.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}
	panelWidth := overlayPanelSize(width, 78)
	panelHeight := overlayPanelSize(height, max(12, len(h.actions)+4))
	panelX := x + max(0, (width-panelWidth)/2)
	panelY := y + max(0, (height-panelHeight)/2)

	box := tview.NewBox().SetBorder(true).SetBorderColor(uiBorder).SetTitleColor(uiTitle).SetBackgroundColor(uiBackground)
	setPlainTitle(box, "Help")
	box.SetRect(panelX, panelY, panelWidth, panelHeight)
	box.Draw(screen)

	innerX := panelX + 2
	innerY := panelY + 1
	innerWidth := max(1, panelWidth-4)
	drawStyledText(screen, innerX, innerY, innerWidth, "Global commands", tcell.StyleDefault.Foreground(uiTitle).Background(uiBackground).Bold(true))

	row := innerY + 1
	for _, action := range h.actions {
		if row >= panelY+panelHeight-2 {
			break
		}
		binding := strings.Join(action.bindings, ", ")
		line := action.group + "  " + action.title
		if binding != "" {
			line = action.group + "  " + binding + "  " + action.title
		}
		drawStyledText(screen, innerX, row, innerWidth, line, tcell.StyleDefault.Foreground(uiText).Background(uiBackground))
		row++
	}

	hint := "Esc / Backspace / ? / ？ close"
	drawStyledText(screen, innerX, panelY+panelHeight-2, innerWidth, hint, tcell.StyleDefault.Foreground(uiMuted).Background(uiBackground))
}

func (h *helpOverlay) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return h.WrapInputHandler(func(event *tcell.EventKey, _ func(tview.Primitive)) {
		if isHelpCloseKey(event) && h.close != nil {
			h.close()
		}
	})
}

func (h *helpOverlay) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return h.WrapMouseHandler(func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
		return true, h
	})
}

func overlayPanelSize(available, preferred int) int {
	if available <= 0 {
		return 0
	}
	if available <= preferred+4 {
		return available
	}
	return preferred
}

func (a *App) handleOverlayTrigger(event *tcell.EventKey) bool {
	if event == nil || event.Key() != tcell.KeyRune || acceptsTextInput(a.app.GetFocus()) {
		return false
	}
	switch event.Rune() {
	case '?', '？':
		a.showHelpOverlay()
	case ':', '：':
		a.showCommandPalette()
	default:
		return false
	}
	return true
}

func (a *App) handleOpenOverlayKey(event *tcell.EventKey) bool {
	if a.helpOverlay != nil && a.pages != nil && a.pages.HasPage(helpOverlayPageName) {
		if isHelpCloseKey(event) && a.helpOverlay.close != nil {
			a.helpOverlay.close()
		}
		return true
	}
	return false
}

func (a *App) handleOverlayMouse(event *tcell.EventMouse, action tview.MouseAction) (bool, *tcell.EventMouse) {
	if a.helpOverlay != nil && a.pages != nil && a.pages.HasPage(helpOverlayPageName) {
		if handler := a.helpOverlay.MouseHandler(); handler != nil {
			_, _ = handler(action, event, func(p tview.Primitive) {
				a.app.SetFocus(p)
			})
		}
		return true, nil
	}
	if a.commandPalette != nil && a.pages != nil && a.pages.HasPage(commandPalettePageName) {
		if preservesMouseClickSynthesis(action) {
			return true, event
		}
		if handler := a.commandPalette.MouseHandler(); handler != nil {
			_, _ = handler(action, event, func(p tview.Primitive) {
				a.app.SetFocus(p)
			})
		}
		return true, nil
	}
	return false, event
}

func isHelpCloseKey(event *tcell.EventKey) bool {
	if event == nil {
		return false
	}
	switch event.Key() {
	case tcell.KeyEscape, tcell.KeyBackspace, tcell.KeyBackspace2:
		return true
	case tcell.KeyRune:
		return event.Rune() == '?' || event.Rune() == '？'
	default:
		return false
	}
}

func (a *App) showHelpOverlay() {
	if a.pages == nil || a.app == nil {
		return
	}
	if a.pages.HasPage(helpOverlayPageName) {
		return
	}
	previousFocus := a.app.GetFocus()
	overlay := newHelpOverlay(globalActions(), func() {
		a.closeHelpOverlay(previousFocus)
	})
	a.helpOverlay = overlay
	a.pages.AddPage(helpOverlayPageName, overlay, true, true)
	a.app.SetFocus(overlay)
}

func (a *App) closeHelpOverlay(previousFocus tview.Primitive) {
	if a.pages != nil {
		a.pages.RemovePage(helpOverlayPageName)
	}
	a.helpOverlay = nil
	a.restoreOverlayFocus(previousFocus)
}
