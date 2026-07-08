package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const commandPalettePageName = "command-palette"

type commandPalette struct {
	*tview.Flex

	input    *tview.InputField
	list     *tview.List
	actions  []uiAction
	filtered []uiAction
	app      *App
	close    func()
}

func newCommandPalette(app *App, actions []uiAction, close func()) *commandPalette {
	input := tview.NewInputField().
		SetLabel(": ").
		SetFieldWidth(0)
	input.SetFieldBackgroundColor(uiField)
	input.SetFieldTextColor(uiText)
	input.SetLabelColor(uiAccent)
	input.SetPlaceholder("Type a command")
	input.SetPlaceholderTextColor(uiMuted)

	list := tview.NewList().ShowSecondaryText(true)
	styleList(list)
	list.SetBorder(false)

	palette := &commandPalette{
		Flex:    tview.NewFlex().SetDirection(tview.FlexRow),
		input:   input,
		list:    list,
		actions: actions,
		app:     app,
		close:   close,
	}
	palette.SetBorder(true).SetBorderColor(uiBorder).SetTitleColor(uiTitle).SetBackgroundColor(uiBackground)
	palette.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		return palette.handleInputCapture(event)
	})
	setViewTitle(palette, "Command Palette")
	palette.AddItem(input, 3, 0, true)
	palette.AddItem(list, 0, 1, false)

	input.SetChangedFunc(func(text string) {
		palette.applyFilter(text)
	})
	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			palette.executeSelected()
		}
	})
	input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		return palette.handleInputCapture(event)
	})
	list.SetSelectedFunc(func(index int, _ string, _ string, _ rune) {
		palette.execute(index)
	})
	palette.applyFilter("")
	return palette
}

func (p *commandPalette) Draw(screen tcell.Screen) {
	x, y, width, height := p.GetRect()
	if width <= 0 || height <= 0 {
		return
	}
	panelWidth := overlayPanelSize(width, 82)
	panelHeight := overlayPanelSize(height, 16)
	panelX := x + max(0, (width-panelWidth)/2)
	panelY := y + max(0, (height-panelHeight)/3)
	p.Flex.SetRect(panelX, panelY, panelWidth, panelHeight)
	p.Flex.Draw(screen)
}

func (p *commandPalette) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return p.WrapInputHandler(func(event *tcell.EventKey, setFocus func(tview.Primitive)) {
		if handler := p.input.InputHandler(); handler != nil {
			handler(event, setFocus)
		}
	})
}

func (p *commandPalette) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return p.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		if event == nil {
			return true, p
		}
		if p.input.InRect(event.Position()) {
			if handler := p.input.MouseHandler(); handler != nil {
				handler(action, event, setFocus)
			}
			return true, p.input
		}
		if p.list.InRect(event.Position()) {
			if handler := p.list.MouseHandler(); handler != nil {
				handler(action, event, setFocus)
			}
			return true, p.list
		}
		return true, p
	})
}

func (p *commandPalette) handleInputCapture(event *tcell.EventKey) *tcell.EventKey {
	if event == nil {
		return nil
	}
	switch event.Key() {
	case tcell.KeyEscape:
		p.closePalette()
		return nil
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if p.input.GetText() == "" {
			p.closePalette()
			return nil
		}
	case tcell.KeyUp, tcell.KeyDown, tcell.KeyHome, tcell.KeyEnd, tcell.KeyPgUp, tcell.KeyPgDn:
		p.routeListKey(event)
		return nil
	}
	return event
}

func (p *commandPalette) routeListKey(event *tcell.EventKey) {
	if p.list == nil || p.list.GetItemCount() == 0 {
		return
	}
	if handler := p.list.InputHandler(); handler != nil {
		handler(event, func(tview.Primitive) {})
	}
}

func (p *commandPalette) applyFilter(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	p.list.Clear()
	p.filtered = p.filtered[:0]
	for _, action := range p.actions {
		if query != "" && !actionMatchesQuery(action, query) {
			continue
		}
		action := action
		p.filtered = append(p.filtered, action)
		p.list.AddItem(action.title, action.group+"  "+strings.Join(action.bindings, ", "), 0, nil)
	}
	if p.list.GetItemCount() > 0 {
		p.list.SetCurrentItem(0)
	}
}

func actionMatchesQuery(action uiAction, query string) bool {
	haystack := strings.ToLower(action.title + "\n" + action.group + "\n" + strings.Join(action.bindings, "\n"))
	return strings.Contains(haystack, query)
}

func (p *commandPalette) executeSelected() {
	p.execute(p.list.GetCurrentItem())
}

func (p *commandPalette) execute(index int) {
	if index < 0 || index >= len(p.filtered) {
		return
	}
	action := p.filtered[index]
	p.closePalette()
	if action.run != nil {
		action.run(p.app)
	}
}

func (p *commandPalette) closePalette() {
	if p.close != nil {
		p.close()
	}
}

func (a *App) showCommandPalette() {
	if a.pages == nil || a.app == nil {
		return
	}
	if a.pages.HasPage(commandPalettePageName) {
		return
	}
	previousFocus := a.app.GetFocus()
	palette := newCommandPalette(a, paletteActions(), func() {
		a.closeCommandPalette(previousFocus)
	})
	a.commandPalette = palette
	a.pages.AddPage(commandPalettePageName, palette, true, true)
	a.app.SetFocus(palette.input)
}

func (a *App) closeCommandPalette(previousFocus tview.Primitive) {
	if a.pages != nil {
		a.pages.RemovePage(commandPalettePageName)
	}
	a.commandPalette = nil
	a.restoreOverlayFocus(previousFocus)
}

func (a *App) restoreOverlayFocus(previousFocus tview.Primitive) {
	if a.app == nil {
		return
	}
	if previousFocus != nil {
		a.app.SetFocus(previousFocus)
		return
	}
	if a.content != nil {
		a.focusContent()
	}
}
