package ui

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/models"
)

type endpointEditorMode int

const (
	endpointEditorListMode endpointEditorMode = iota
	endpointEditorFormMode
)

type endpointEditor struct {
	*tview.Box

	app       *App
	content   *tview.Flex
	list      *tview.List
	form      *tview.Form
	endpoints []models.Endpoint
	draft     models.Endpoint
	selected  int
	formIndex int
	mode      endpointEditorMode
	message   string
	anchor    settingsRect
	panelRect settingsRect
	close     func()
	save      func([]models.Endpoint) error
}

func newEndpointEditor(app *App, endpoints []models.Endpoint, anchor settingsRect, save func([]models.Endpoint) error, close func()) *endpointEditor {
	editor := &endpointEditor{
		Box:       tview.NewBox(),
		app:       app,
		content:   tview.NewFlex().SetDirection(tview.FlexRow),
		endpoints: cloneEndpoints(endpoints),
		anchor:    anchor,
		close:     close,
		save:      save,
	}
	editor.SetBorder(true).SetBorderColor(uiBorder).SetTitleColor(uiTitle).SetBackgroundColor(uiBackground)
	setPlainTitle(editor, "Edit Endpoints")
	editor.list = tview.NewList().ShowSecondaryText(true)
	styleList(editor.list)
	editor.list.SetBorder(false)
	editor.list.SetSelectedFunc(func(index int, _ string, _ string, _ rune) {
		if index < len(editor.endpoints) {
			editor.openForm(index)
		}
	})
	editor.showList()
	return editor
}

func (p *endpointEditor) Draw(screen tcell.Screen) {
	x, y, width, height := p.GetRect()
	if width <= 0 || height <= 0 {
		return
	}
	anchor := p.anchor
	if anchor.width <= 0 || anchor.height <= 0 {
		anchor = settingsRect{x: x, y: y, width: width, height: height}
	}
	if anchor.x < x {
		anchor.x = x
	}
	if anchor.y < y {
		anchor.y = y
	}
	if anchor.x+anchor.width > x+width {
		anchor.width = x + width - anchor.x
	}
	if anchor.y+anchor.height > y+height {
		anchor.height = y + height - anchor.y
	}
	preferredWidth := 84
	if anchor.width < preferredWidth+4 {
		preferredWidth = anchor.width
	}
	panelWidth := overlayPanelSize(anchor.width, preferredWidth)
	preferredHeight := max(12, min(24, len(p.endpoints)*2+8))
	panelHeight := overlayPanelSize(anchor.height, preferredHeight)
	panelX := anchor.x + max(0, (anchor.width-panelWidth)/2)
	panelY := anchor.y + max(0, (anchor.height-panelHeight)/3)
	p.panelRect = settingsRect{x: panelX, y: panelY, width: panelWidth, height: panelHeight}

	box := tview.NewBox().SetBorder(true).SetBorderColor(uiBorder).SetTitleColor(uiTitle).SetBackgroundColor(uiBackground)
	setPlainTitle(box, "Edit Endpoints")
	box.SetRect(panelX, panelY, panelWidth, panelHeight)
	box.Draw(screen)
	contentX := panelX + 1
	contentY := panelY + 1
	contentWidth := max(1, panelWidth-2)
	contentHeight := max(1, panelHeight-2)
	p.content.SetRect(contentX, contentY, contentWidth, contentHeight)
	p.content.Draw(screen)
}

func (p *endpointEditor) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return p.WrapInputHandler(func(event *tcell.EventKey, setFocus func(tview.Primitive)) {
		if event == nil {
			return
		}
		if p.mode == endpointEditorFormMode {
			switch event.Key() {
			case tcell.KeyF2:
				p.commitDraft()
				return
			case tcell.KeyEscape:
				p.showList()
				return
			}
			if p.form != nil {
				if handler := p.form.InputHandler(); handler != nil {
					handler(event, setFocus)
				}
			}
			return
		}

		switch event.Key() {
		case tcell.KeyEscape:
			p.closeEditor()
			return
		case tcell.KeyF2:
			p.saveChanges()
			return
		case tcell.KeyRune:
			switch event.Rune() {
			case 'a', 'A':
				p.openForm(-1)
				return
			case 'd', 'D':
				p.deleteSelected()
				return
			case '[':
				p.moveSelected(-1)
				return
			case ']':
				p.moveSelected(1)
				return
			}
		}
		if p.list != nil {
			if handler := p.list.InputHandler(); handler != nil {
				handler(event, setFocus)
			}
		}
	})
}

func (p *endpointEditor) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return p.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		if event == nil {
			return true, p
		}
		if p.mode == endpointEditorFormMode && p.form != nil {
			if handler := p.form.MouseHandler(); handler != nil {
				handler(action, event, setFocus)
			}
			return true, p.form
		}
		if p.list != nil && p.list.InRect(event.Position()) {
			if handler := p.list.MouseHandler(); handler != nil {
				handler(action, event, setFocus)
			}
			return true, p.list
		}
		return true, p
	})
}

func (p *endpointEditor) showList() {
	p.showListMessage("")
}

func (p *endpointEditor) showListMessage(message string) {
	p.mode = endpointEditorListMode
	p.form = nil
	p.message = message
	p.refreshList()
	p.content.Clear()
	p.content.AddItem(p.list, 0, 1, true)
	helpText := endpointEditorHelpText()
	if p.message != "" {
		helpText = p.message
	}
	help := tview.NewTextView().SetText(helpText).SetTextColor(uiMuted).SetTextAlign(tview.AlignCenter)
	help.SetWrap(false)
	p.content.AddItem(help, 2, 0, false)
	if p.app != nil && p.app.app != nil {
		p.app.app.SetFocus(p)
	}
}

func (p *endpointEditor) refreshList() {
	if p.list == nil {
		return
	}
	current := p.selected
	p.list.Clear()
	for _, endpoint := range p.endpoints {
		name := endpointNameForEditor(endpoint)
		state := "Enabled"
		if !endpoint.Enabled {
			state = "Disabled"
		}
		p.list.AddItem(name+"  "+state, strings.TrimSpace(endpoint.URL), 0, nil)
	}
	if len(p.endpoints) == 0 {
		p.list.AddItem("No endpoints configured", "Press A to add an endpoint", 0, nil)
	}
	p.selected = settingsClampInt(current, 0, len(p.endpoints)-1)
	p.list.SetCurrentItem(p.selected)
}

func (p *endpointEditor) openForm(index int) {
	p.mode = endpointEditorFormMode
	p.formIndex = index
	p.message = ""
	p.draft = models.Endpoint{Enabled: true}
	if index >= 0 && index < len(p.endpoints) {
		p.draft = p.endpoints[index]
	}
	p.draft.Name = endpointNameForEditor(p.draft)
	p.draft.URL = strings.TrimSpace(p.draft.URL)

	form := tview.NewForm()
	form.AddInputField("Name", p.draft.Name, 40, nil, func(value string) {
		p.draft.Name = value
	})
	form.AddInputField("URL", p.draft.URL, 56, nil, func(value string) {
		p.draft.URL = value
	})
	form.AddDropDown("Enabled", onOffOptions, boolOption(p.draft.Enabled), func(_ string, selected int) {
		p.draft.Enabled = selected == 1
	})
	form.AddButton("Save", func() { p.commitDraft() })
	form.AddButton("Cancel", func() { p.showList() })
	form.SetCancelFunc(func() { p.showList() })
	styleForm(form)
	form.SetItemPadding(1)
	form.SetBorder(true)
	form.SetBorderColor(uiBorder)
	form.SetTitleColor(uiTitle)
	title := "Edit Endpoint"
	if index < 0 {
		title = "Add Endpoint"
	}
	setPlainTitle(form, title)
	p.form = form
	p.content.Clear()
	p.content.AddItem(form, 0, 1, true)
	if p.app != nil && p.app.app != nil {
		p.app.app.SetFocus(form)
	}
}

func (p *endpointEditor) Focus(delegate func(tview.Primitive)) {
	if p.mode == endpointEditorFormMode && p.form != nil {
		p.form.Focus(delegate)
		return
	}
	if p.list != nil {
		p.list.Focus(delegate)
		return
	}
	p.Box.Focus(delegate)
}

func (p *endpointEditor) HasFocus() bool {
	if p.mode == endpointEditorFormMode && p.form != nil {
		return p.form.HasFocus()
	}
	if p.list != nil {
		return p.list.HasFocus()
	}
	return p.Box.HasFocus()
}

func (p *endpointEditor) commitDraft() {
	endpoint := p.draft
	endpoint.Name = strings.TrimSpace(endpoint.Name)
	endpoint.URL = strings.TrimRight(strings.TrimSpace(endpoint.URL), "/")
	if err := validateEndpointEditorEntry(endpoint, p.formIndex, p.endpoints); err != nil {
		if p.form != nil {
			setPlainTitle(p.form, "Invalid Endpoint: "+err.Error())
		}
		return
	}
	if p.formIndex < 0 {
		p.endpoints = append(p.endpoints, endpoint)
		p.selected = len(p.endpoints) - 1
	} else {
		p.endpoints[p.formIndex] = endpoint
		p.selected = p.formIndex
	}
	p.showList()
}

func (p *endpointEditor) deleteSelected() {
	if p.selected < 0 || p.selected >= len(p.endpoints) {
		return
	}
	p.message = ""
	p.endpoints = append(p.endpoints[:p.selected], p.endpoints[p.selected+1:]...)
	if p.selected >= len(p.endpoints) {
		p.selected = len(p.endpoints) - 1
	}
	p.refreshList()
}

func (p *endpointEditor) moveSelected(delta int) {
	target := p.selected + delta
	if p.selected < 0 || p.selected >= len(p.endpoints) || target < 0 || target >= len(p.endpoints) {
		return
	}
	p.message = ""
	p.endpoints[p.selected], p.endpoints[target] = p.endpoints[target], p.endpoints[p.selected]
	p.selected = target
	p.refreshList()
}

func (p *endpointEditor) saveChanges() {
	if err := validateEndpointEditorList(p.endpoints); err != nil {
		p.showListMessage(err.Error())
		return
	}
	if p.save != nil {
		if err := p.save(cloneEndpoints(p.endpoints)); err != nil {
			p.showListMessage(err.Error())
			return
		}
	}
	p.closeEditor()
}

func (p *endpointEditor) closeEditor() {
	if p.close != nil {
		p.close()
	}
}

func endpointEditorHelpText() string {
	return "Enter edit  A add  D delete  [/] reorder  F2 save  Esc cancel"
}

func endpointNameForEditor(endpoint models.Endpoint) string {
	if name := strings.TrimSpace(endpoint.Name); name != "" {
		return name
	}
	if display := strings.TrimSpace(endpointDisplayName(endpoint)); display != "" && display != "endpoint" {
		return display
	}
	return "Endpoint"
}

func validateEndpointEditorEntry(endpoint models.Endpoint, index int, endpoints []models.Endpoint) error {
	if strings.TrimSpace(endpoint.Name) == "" {
		return errors.New("name must not be blank")
	}
	if strings.TrimSpace(endpoint.URL) == "" {
		return errors.New("URL must not be blank")
	}
	parsed, err := url.Parse(endpoint.URL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("URL must include a scheme and host")
	}
	normalized := normalizedEndpointURL(endpoint.URL)
	for otherIndex, other := range endpoints {
		if otherIndex != index && normalizedEndpointURL(other.URL) == normalized {
			return errors.New("URL is already used by another endpoint")
		}
	}
	return nil
}

func validateEndpointEditorList(endpoints []models.Endpoint) error {
	for index, endpoint := range endpoints {
		if err := validateEndpointEditorEntry(endpoint, index, endpoints); err != nil {
			return fmt.Errorf("endpoint %d: %w", index+1, err)
		}
	}
	return nil
}

func endpointSummary(endpoints []models.Endpoint) string {
	if len(endpoints) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		part := endpointNameForEditor(endpoint)
		if !endpoint.Enabled {
			part += " (disabled)"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "; ")
}
