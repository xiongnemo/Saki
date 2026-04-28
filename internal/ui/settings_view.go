package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/models"
	"github.com/xiongnemo/saki/internal/subsonic"
	"github.com/xiongnemo/saki/internal/version"
)

const (
	settingsProbeDelay   = 500 * time.Millisecond
	settingsProbeTimeout = 5 * time.Second
	systemMinPingHeight  = 3
	systemMaxPingHeight  = 8
	systemEditPageName   = "system-edit"
)

type systemTab int

const (
	systemTabAbout systemTab = iota
	systemTabSettings
)

var systemTabLabels = []string{"About", "Settings"}

type settingsRect struct {
	x, y, width, height int
}

func (r settingsRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

type settingRow struct {
	label  string
	value  string
	action func()
}

type kvRow struct {
	label string
	value string
}

type settingsView struct {
	*tview.Box

	app      *App
	content  *tview.TextView
	status   *tview.TextView
	cfg      models.Config
	settings models.Settings

	activeTab        systemTab
	tabRects         []settingsRect
	settingsRows     []settingRow
	selectedRow      int
	contentRect      settingsRect
	settingsListRect settingsRect
	saveButtonRect   settingsRect
	statusRect       settingsRect
	saveFocused      bool

	probeMu    sync.Mutex
	probeSeq   int
	probeTimer *time.Timer

	saveConfig func(models.Config) error
}

func newSettingsView(app *App, cfg models.Config) *settingsView {
	view := &settingsView{
		Box:       tview.NewBox(),
		app:       app,
		cfg:       cfg,
		settings:  cfg.Settings,
		activeTab: systemTabSettings,
		content:   tview.NewTextView(),
		status:    tview.NewTextView(),
	}
	view.saveConfig = func(models.Config) error { return nil }
	if app != nil {
		view.saveConfig = func(cfg models.Config) error {
			return app.store.Save(cfg)
		}
	}
	view.Box.SetBorder(true)
	view.Box.SetBackgroundColor(uiBackground)
	view.Box.SetBorderColor(uiBorder)
	view.Box.SetTitleColor(uiTitle)
	setViewTitle(view.Box, "System")

	view.content.SetDynamicColors(true)
	view.content.SetWrap(true)
	view.content.SetScrollable(true)
	view.content.SetTextColor(uiText)
	view.content.SetBackgroundColor(uiBackground)

	view.status.SetDynamicColors(true)
	view.status.SetWrap(false)
	view.status.SetScrollable(true)
	view.status.SetTextColor(uiText)
	view.status.SetBackgroundColor(uiBackground)
	view.status.SetBorder(true)
	view.status.SetBorderColor(uiBorder)
	view.status.SetTitleColor(uiTitle)
	setPlainTitle(view.status, " Endpoint Ping ")

	view.refreshSettingsList()
	view.setStatusText(view.statusText("", view.cfg.Account.Endpoints, nil, false, "Waiting to ping."))
	return view
}

func (v *settingsView) refreshSettingsList() {
	current := v.selectedRow
	v.settingsRows = v.settingsRows[:0]
	v.settingsRows = append(v.settingsRows, settingRow{label: "Endpoints (; separated)", value: endpointsToText(v.cfg.Account.Endpoints), action: func() {
		v.openEndpointPopup()
	}})
	v.settingsRows = append(v.settingsRows, settingRow{label: "Audio backend", value: fallbackText(v.settings.AudioBackend, "auto"), action: func() {
		v.openChoicePopup("Audio backend", audioBackendLabels, audioBackendOption(v.settings.AudioBackend), func(index int) error {
			if index < 0 || index >= len(audioBackendValues) {
				return errors.New("invalid audio backend")
			}
			v.settings.AudioBackend = audioBackendValues[index]
			v.refreshSettingsList()
			return nil
		})
	}})
	v.settingsRows = append(v.settingsRows, settingRow{label: "MPV path", value: fallbackText(v.settings.MPVPath, "PATH lookup"), action: func() {
		v.openTextPopup("MPV path", v.settings.MPVPath, func(value string) error {
			v.settings.MPVPath = strings.TrimSpace(value)
			v.refreshSettingsList()
			return nil
		})
	}})
	v.settingsRows = append(v.settingsRows, settingRow{label: "Cache dir", value: fallbackText(v.settings.CacheDir, "default"), action: func() {
		v.openTextPopup("Cache dir", v.settings.CacheDir, func(value string) error {
			v.settings.CacheDir = strings.TrimSpace(value)
			v.refreshSettingsList()
			return nil
		})
	}})
	cacheMaxMB := v.settings.AudioCacheMaxBytes / 1024 / 1024
	if cacheMaxMB <= 0 {
		cacheMaxMB = 2048
	}
	v.settingsRows = append(v.settingsRows, settingRow{label: "Audio cache MB", value: strconv.FormatInt(cacheMaxMB, 10), action: func() {
		v.openTextPopup("Audio cache MB", strconv.FormatInt(cacheMaxMB, 10), func(value string) error {
			mb, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || mb <= 0 {
				return errors.New("audio cache MB must be a positive integer")
			}
			v.settings.AudioCacheMaxBytes = mb * 1024 * 1024
			v.refreshSettingsList()
			return nil
		})
	}})
	v.settingsRows = append(v.settingsRows, settingRow{label: "Health interval sec", value: strconv.Itoa(v.settings.HealthCheckIntervalSeconds), action: func() {
		v.openTextPopup("Health interval sec", strconv.Itoa(v.settings.HealthCheckIntervalSeconds), func(value string) error {
			seconds, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || seconds <= 0 {
				return errors.New("health interval must be a positive integer")
			}
			v.settings.HealthCheckIntervalSeconds = seconds
			v.refreshSettingsList()
			return nil
		})
	}})
	v.settingsRows = append(v.settingsRows, settingRow{label: "Switch threshold", value: fmt.Sprintf("%.2f", v.settings.EndpointSwitchThreshold), action: func() {
		v.openTextPopup("Switch threshold", fmt.Sprintf("%.2f", v.settings.EndpointSwitchThreshold), func(value string) error {
			threshold, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || threshold <= 0 || threshold >= 1 {
				return errors.New("switch threshold must be greater than 0 and less than 1")
			}
			v.settings.EndpointSwitchThreshold = threshold
			v.refreshSettingsList()
			return nil
		})
	}})
	v.settingsRows = append(v.settingsRows, settingRow{label: "Prefetch", value: onOffOptions[boolOption(v.settings.EnablePrefetch)], action: func() {
		v.openChoicePopup("Prefetch", onOffOptions, boolOption(v.settings.EnablePrefetch), func(index int) error {
			v.settings.EnablePrefetch = index == 1
			v.refreshSettingsList()
			return nil
		})
	}})
	v.settingsRows = append(v.settingsRows, settingRow{label: "Bundled mpv", value: onOffOptions[boolOption(v.settings.UseBundledMPV)], action: func() {
		v.openChoicePopup("Bundled mpv", onOffOptions, boolOption(v.settings.UseBundledMPV), func(index int) error {
			v.settings.UseBundledMPV = index == 1
			v.refreshSettingsList()
			return nil
		})
	}})
	v.selectedRow = settingsClampInt(current, 0, len(v.settingsRows)-1)
}

func (v *settingsView) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}

	v.layout(x, y, width, height)
	v.drawTabs(screen)

	if v.contentRect.width > 0 && v.contentRect.height > 0 {
		switch v.activeTab {
		case systemTabSettings:
			v.drawSettingsRows(screen)
			v.drawSaveButton(screen)
		default:
			v.drawAbout(screen)
		}
	}
	if v.statusRect.width > 0 && v.statusRect.height > 0 {
		v.status.SetRect(v.statusRect.x, v.statusRect.y, v.statusRect.width, v.statusRect.height)
		v.status.Draw(screen)
	}
}

func (v *settingsView) layout(x, y, width, height int) {
	v.tabRects = v.tabRects[:0]
	pingHeight := v.pingHeight(height)
	tabHeight := 1
	contentY := y + tabHeight
	if height > 2 {
		contentY++
	}
	contentBottom := y + height
	if pingHeight > 0 {
		contentBottom -= pingHeight + 1
		v.statusRect = settingsRect{x: x, y: contentBottom + 1, width: width, height: pingHeight}
	} else {
		v.statusRect = settingsRect{}
	}
	contentHeight := contentBottom - contentY
	if contentHeight < 0 {
		contentHeight = 0
	}
	v.contentRect = settingsRect{x: x, y: contentY, width: width, height: contentHeight}
	if v.activeTab == systemTabSettings {
		v.settingsListRect = v.contentRect
		v.saveButtonRect = settingsRect{}
		if contentHeight > 1 {
			buttonText := " Save "
			buttonWidth := len(buttonText)
			v.saveButtonRect = settingsRect{x: x, y: contentY + contentHeight - 1, width: buttonWidth, height: 1}
			listHeight := contentHeight - 2
			if listHeight < 1 {
				listHeight = 1
			}
			v.settingsListRect = settingsRect{x: x, y: contentY, width: width, height: listHeight}
		}
	} else {
		v.settingsListRect = settingsRect{}
		v.saveButtonRect = settingsRect{}
	}
}

func (v *settingsView) pingHeight(totalHeight int) int {
	if totalHeight < 10 {
		return 0
	}
	if totalHeight < 16 {
		return systemMinPingHeight
	}
	height := len(v.cfg.Account.Endpoints) + 2
	return settingsClampInt(height, systemMinPingHeight+1, systemMaxPingHeight)
}

func (v *settingsView) drawTabs(screen tcell.Screen) {
	x, y, width, _ := v.GetInnerRect()
	col := x
	for index, label := range systemTabLabels {
		text := " " + label + " "
		tabWidth := len(text)
		if col+tabWidth > x+width {
			tabWidth = x + width - col
		}
		if tabWidth <= 0 {
			break
		}
		style := tcell.StyleDefault.Foreground(uiTitle).Background(uiBackground)
		if systemTab(index) == v.activeTab {
			style = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiAccent).Bold(true)
		}
		v.tabRects = append(v.tabRects, settingsRect{x: col, y: y, width: tabWidth, height: 1})
		drawPlainText(screen, col, y, text[:tabWidth], style)
		col += tabWidth + 1
	}
}

func drawPlainText(screen tcell.Screen, x, y int, text string, style tcell.Style) {
	for i, r := range text {
		screen.SetContent(x+i, y, r, nil, style)
	}
}

func drawStyledText(screen tcell.Screen, x, y, width int, text string, style tcell.Style) {
	if width <= 0 {
		return
	}
	col := 0
	for _, r := range text {
		if col >= width {
			break
		}
		screen.SetContent(x+col, y, r, nil, style)
		col++
	}
	for col < width {
		screen.SetContent(x+col, y, ' ', nil, style)
		col++
	}
}

func fillLine(screen tcell.Screen, x, y, width int, style tcell.Style) {
	for col := 0; col < width; col++ {
		screen.SetContent(x+col, y, ' ', nil, style)
	}
}

func (v *settingsView) drawSettingsRows(screen tcell.Screen) {
	rect := v.settingsListRect
	if rect.width <= 0 || rect.height <= 0 {
		return
	}
	labelWidth := settingLabelWidth(v.settingsRows, rect.width)
	for rowIndex, row := range v.settingsRows {
		if rowIndex >= rect.height {
			break
		}
		y := rect.y + rowIndex
		selected := rowIndex == v.selectedRow && !v.saveFocused
		background := uiBackground
		labelColor := uiLabel
		valueColor := uiText
		if selected {
			background = uiAccent
			labelColor = tcell.ColorBlack
			valueColor = tcell.ColorBlack
			fillLine(screen, rect.x, y, rect.width, tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiAccent).Bold(true))
		}
		labelStyle := tcell.StyleDefault.Foreground(labelColor).Background(background)
		valueStyle := tcell.StyleDefault.Foreground(valueColor).Background(background)
		if selected {
			labelStyle = labelStyle.Bold(true)
			valueStyle = valueStyle.Bold(true)
		}
		drawStyledText(screen, rect.x, y, labelWidth, row.label, labelStyle)
		value := strings.TrimSpace(row.value)
		if value == "" {
			value = "(empty)"
		}
		drawStyledText(screen, rect.x+labelWidth, y, rect.width-labelWidth, value, valueStyle)
	}
}

func settingLabelWidth(rows []settingRow, maxWidth int) int {
	width := 0
	for _, row := range rows {
		if labelWidth := len([]rune(row.label)); labelWidth > width {
			width = labelWidth
		}
	}
	width += 2
	if maxWidth <= 0 {
		return width
	}
	if width >= maxWidth {
		return maxWidth
	}
	return width
}

func (v *settingsView) drawSaveButton(screen tcell.Screen) {
	if v.saveButtonRect.width <= 0 || v.saveButtonRect.height <= 0 {
		return
	}
	style := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiAccent)
	if v.saveFocused {
		style = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiTitle).Bold(true)
	}
	drawPlainText(screen, v.saveButtonRect.x, v.saveButtonRect.y, " Save ", style)
}

func (v *settingsView) Focus(delegate func(p tview.Primitive)) {
	v.Box.Focus(delegate)
	if v.app != nil {
		v.app.focusTarget = appFocusContent
		v.app.contentFocus = v
	}
}

func (v *settingsView) HasFocus() bool {
	return v.Box.HasFocus()
}

func (v *settingsView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return v.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if v.handleTabShortcut(event, setFocus) {
			return
		}
		if v.activeTab == systemTabSettings {
			if v.saveFocused {
				v.handleSaveButtonKey(event, setFocus)
				return
			}
			v.handleSettingsKey(event, setFocus)
			return
		}
		if handler := v.content.InputHandler(); handler != nil {
			handler(event, setFocus)
		}
	})
}

func (v *settingsView) handleSettingsKey(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	switch event.Key() {
	case tcell.KeyTab:
		v.focusSaveButton(setFocus)
	case tcell.KeyUp:
		v.moveSettingsSelection(-1, setFocus)
	case tcell.KeyDown:
		if v.selectedRow >= len(v.settingsRows)-1 {
			v.focusSaveButton(setFocus)
		} else {
			v.moveSettingsSelection(1, setFocus)
		}
	case tcell.KeyHome:
		v.selectedRow = 0
	case tcell.KeyEnd:
		v.selectedRow = max(0, len(v.settingsRows)-1)
	case tcell.KeyEnter:
		if v.selectedRow >= 0 && v.selectedRow < len(v.settingsRows) && v.settingsRows[v.selectedRow].action != nil {
			v.settingsRows[v.selectedRow].action()
		}
	}
}

func (v *settingsView) handleSaveButtonKey(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	switch event.Key() {
	case tcell.KeyEnter:
		v.save()
	case tcell.KeyBacktab, tcell.KeyUp:
		v.focusSettingsList(setFocus)
	case tcell.KeyTab, tcell.KeyDown:
		v.focusSettingsList(setFocus)
	}
}

func (v *settingsView) handleTabShortcut(event *tcell.EventKey, setFocus func(p tview.Primitive)) bool {
	switch event.Key() {
	case tcell.KeyLeft:
		v.setActiveTab(v.activeTab-1, setFocus)
		return true
	case tcell.KeyRight:
		v.setActiveTab(v.activeTab+1, setFocus)
		return true
	case tcell.KeyRune:
		switch event.Rune() {
		case '[':
			v.setActiveTab(v.activeTab-1, setFocus)
			return true
		case ']':
			v.setActiveTab(v.activeTab+1, setFocus)
			return true
		case '1':
			v.setActiveTab(systemTabAbout, setFocus)
			return true
		case '2':
			v.setActiveTab(systemTabSettings, setFocus)
			return true
		}
	}
	return false
}

func (v *settingsView) setActiveTab(tab systemTab, setFocus func(tview.Primitive)) {
	last := systemTab(len(systemTabLabels) - 1)
	if tab < systemTabAbout {
		tab = last
	}
	if tab > last {
		tab = systemTabAbout
	}
	v.activeTab = tab
	if tab == systemTabSettings {
		setFocus(v)
		return
	}
	setFocus(v)
}

func (v *settingsView) focusSettingsList(setFocus func(tview.Primitive)) {
	v.saveFocused = false
	setFocus(v)
}

func (v *settingsView) focusSaveButton(setFocus func(tview.Primitive)) {
	v.saveFocused = true
	setFocus(v)
}

func (v *settingsView) PasteHandler() func(pastedText string, setFocus func(p tview.Primitive)) {
	return v.WrapPasteHandler(func(_ string, _ func(p tview.Primitive)) {})
}

func (v *settingsView) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return v.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		x, y := event.Position()
		if !v.InRect(x, y) {
			return false, nil
		}
		wrappedSetFocus := func(p tview.Primitive) {
			if v.app != nil {
				v.app.focusTarget = appFocusContent
			}
			setFocus(p)
		}
		if action == tview.MouseLeftDown {
			for index, rect := range v.tabRects {
				if rect.contains(x, y) {
					v.setActiveTab(systemTab(index), wrappedSetFocus)
					return true, nil
				}
			}
		}

		if v.activeTab == systemTabSettings && v.settingsListRect.contains(x, y) {
			switch action {
			case tview.MouseScrollUp:
				v.saveFocused = false
				v.moveSettingsSelection(-1, wrappedSetFocus)
				return true, nil
			case tview.MouseScrollDown:
				v.saveFocused = false
				v.moveSettingsSelection(1, wrappedSetFocus)
				return true, nil
			case tview.MouseLeftDown, tview.MouseLeftClick:
				v.saveFocused = false
				v.selectSettingsRow(y, wrappedSetFocus)
				return true, nil
			}
		}
		if v.activeTab == systemTabSettings && v.saveButtonRect.contains(x, y) {
			switch action {
			case tview.MouseLeftDown:
				v.focusSaveButton(wrappedSetFocus)
				return true, nil
			case tview.MouseLeftClick:
				v.focusSaveButton(wrappedSetFocus)
				v.save()
				return true, nil
			}
		}
		if v.activeTab == systemTabAbout && v.contentRect.contains(x, y) {
			if handler := v.content.MouseHandler(); handler != nil {
				return handler(action, event, wrappedSetFocus)
			}
		}
		if action == tview.MouseLeftDown {
			v.setActiveTab(v.activeTab, wrappedSetFocus)
			return true, nil
		}
		return false, nil
	})
}

func (v *settingsView) moveSettingsSelection(delta int, setFocus func(tview.Primitive)) {
	if len(v.settingsRows) == 0 {
		return
	}
	v.selectedRow += delta
	if v.selectedRow < 0 {
		v.selectedRow = 0
	}
	if v.selectedRow >= len(v.settingsRows) {
		v.selectedRow = len(v.settingsRows) - 1
	}
	v.saveFocused = false
	setFocus(v)
}

func (v *settingsView) selectSettingsRow(y int, setFocus func(tview.Primitive)) {
	index := y - v.settingsListRect.y
	if index < 0 || index >= len(v.settingsRows) {
		return
	}
	v.selectedRow = index
	v.saveFocused = false
	setFocus(v)
}

type systemTextPopup struct {
	*tview.Box

	title      string
	label      string
	text       *tview.TextArea
	anchor     settingsRect
	popupRect  settingsRect
	textRect   settingsRect
	saveRect   settingsRect
	cancelRect settingsRect
	message    string

	accept func(string) error
	cancel func()
	close  func()
}

func newSystemTextPopup(title, label, value string, anchor settingsRect, accept func(string) error, live func(string), cancel func(), close func()) *systemTextPopup {
	text := tview.NewTextArea().
		SetText(value, true).
		SetWrap(true)
	text.SetBorder(true)
	text.SetBackgroundColor(uiField)
	text.SetBorderColor(uiBorder)
	text.SetTitleColor(uiTitle)
	text.SetTextStyle(tcell.StyleDefault.Foreground(uiText).Background(uiField))
	text.SetLabelStyle(tcell.StyleDefault.Foreground(uiLabel).Background(uiBackground))
	if live != nil {
		text.SetChangedFunc(func() {
			live(text.GetText())
		})
	}
	return &systemTextPopup{
		Box:    tview.NewBox(),
		title:  title,
		label:  label,
		text:   text,
		anchor: anchor,
		accept: accept,
		cancel: cancel,
		close:  close,
	}
}

func (p *systemTextPopup) Draw(screen tcell.Screen) {
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

	popupWidth := settingsClampInt(anchor.width-4, 48, 100)
	if popupWidth > anchor.width {
		popupWidth = anchor.width
	}
	popupHeight := settingsClampInt(anchor.height-2, 8, 16)
	if popupHeight > anchor.height {
		popupHeight = anchor.height
	}
	popupX := anchor.x + max(0, (anchor.width-popupWidth)/2)
	popupY := anchor.y + max(0, (anchor.height-popupHeight)/2)
	p.popupRect = settingsRect{x: popupX, y: popupY, width: popupWidth, height: popupHeight}

	box := tview.NewBox().SetBorder(true).SetBorderColor(uiBorder).SetTitleColor(uiTitle).SetBackgroundColor(uiBackground)
	setPlainTitle(box, p.title)
	box.SetRect(popupX, popupY, popupWidth, popupHeight)
	box.Draw(screen)

	innerX := popupX + 2
	innerWidth := max(1, popupWidth-4)
	drawStyledText(screen, innerX, popupY+1, innerWidth, p.label, tcell.StyleDefault.Foreground(uiLabel).Background(uiBackground))

	textHeight := max(1, popupHeight-6)
	p.textRect = settingsRect{x: innerX, y: popupY + 2, width: innerWidth, height: textHeight}
	p.text.SetRect(p.textRect.x, p.textRect.y, p.textRect.width, p.textRect.height)
	p.text.Draw(screen)

	buttonY := popupY + popupHeight - 2
	p.saveRect = settingsRect{x: innerX, y: buttonY, width: 8, height: 1}
	p.cancelRect = settingsRect{x: innerX + 10, y: buttonY, width: 10, height: 1}
	drawStyledText(screen, p.saveRect.x, p.saveRect.y, p.saveRect.width, " Save ", tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiAccent).Bold(true))
	drawStyledText(screen, p.cancelRect.x, p.cancelRect.y, p.cancelRect.width, " Cancel ", tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiAccent))
	help := "Ctrl+S save | Esc close"
	if p.message != "" {
		help = p.message
	}
	drawStyledText(screen, innerX+22, buttonY, max(0, innerWidth-22), help, tcell.StyleDefault.Foreground(uiMuted).Background(uiBackground))
}

func (p *systemTextPopup) Focus(delegate func(p tview.Primitive)) {
	p.text.Focus(delegate)
}

func (p *systemTextPopup) HasFocus() bool {
	return p.text.HasFocus() || p.Box.HasFocus()
}

func (p *systemTextPopup) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return p.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if event.Key() == tcell.KeyCtrlS {
			p.save()
			return
		}
		if handler := p.text.InputHandler(); handler != nil {
			handler(event, setFocus)
		}
	})
}

func (p *systemTextPopup) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
	return p.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		x, y := event.Position()
		if p.saveRect.contains(x, y) && action == tview.MouseLeftClick {
			p.save()
			return true, nil
		}
		if p.cancelRect.contains(x, y) && action == tview.MouseLeftClick {
			p.cancelAndClose()
			return true, nil
		}
		if p.textRect.contains(x, y) {
			if handler := p.text.MouseHandler(); handler != nil {
				handler(action, event, setFocus)
			}
			return true, nil
		}
		return true, nil
	})
}

func (p *systemTextPopup) save() {
	if p.accept != nil {
		if err := p.accept(p.text.GetText()); err != nil {
			p.message = "Invalid value: " + err.Error()
			return
		}
	}
	if p.close != nil {
		p.close()
	}
}

func (p *systemTextPopup) cancelAndClose() {
	if p.cancel != nil {
		p.cancel()
	}
	if p.close != nil {
		p.close()
	}
}

func (v *settingsView) openEndpointPopup() {
	original := endpointsToText(v.cfg.Account.Endpoints)
	v.openInputPopup("Edit Endpoints", "Endpoints (; separated)", original, 72, func(value string) error {
		v.cfg.Account.Endpoints = parseEndpoints(value)
		v.refreshSettingsList()
		v.startProbeNow("")
		return nil
	}, func(value string) {
		v.cfg.Account.Endpoints = parseEndpoints(value)
		v.refreshSettingsList()
		v.startProbeAfterDelay("")
	}, func() {
		v.cfg.Account.Endpoints = parseEndpoints(original)
		v.refreshSettingsList()
		v.startProbeNow("")
	})
}

func (v *settingsView) openTextPopup(label, value string, accept func(string) error) {
	v.openInputPopup("Edit "+label, label, value, 64, accept, nil, nil)
}

func (v *settingsView) openInputPopup(title, label, value string, fieldWidth int, accept func(string) error, live func(string), cancel func()) {
	if v.app == nil || v.app.pages == nil {
		return
	}
	anchor := v.popupAnchor()
	popup := newSystemTextPopup(title, label, value, anchor, accept, live, cancel, func() {
		v.closePopup()
	})
	v.showPopup(popup, cancel)
}

func (v *settingsView) openChoicePopup(label string, options []string, current int, accept func(int) error) {
	if v.app == nil || v.app.pages == nil {
		return
	}
	selected := current
	form := tview.NewForm()
	form.AddDropDown(label, options, current, func(_ string, index int) {
		selected = index
	})
	form.AddButton("OK", func() {
		if accept != nil {
			if err := accept(selected); err != nil {
				v.setStatusText(v.statusText("Invalid value: "+err.Error(), v.cfg.Account.Endpoints, nil, false, ""))
				return
			}
		}
		v.closePopup()
	})
	form.AddButton("Cancel", func() {
		v.closePopup()
	})
	form.SetCancelFunc(func() {
		v.closePopup()
	})
	styleForm(form)
	form.SetItemPadding(0)
	form.SetBorder(true)
	form.SetBorderColor(uiBorder)
	form.SetTitleColor(uiTitle)
	setPlainTitle(form, "Choose "+label)
	form.SetFocus(0)
	v.showPopup(center(form, 52, 7), nil)
}

func (v *settingsView) popupAnchor() settingsRect {
	anchor := v.contentRect
	if anchor.width <= 0 || anchor.height <= 0 {
		x, y, width, height := v.GetInnerRect()
		anchor = settingsRect{x: x, y: y, width: width, height: height}
	}
	return anchor
}

func (v *settingsView) showPopup(item tview.Primitive, cancel func()) {
	v.app.pages.RemovePage(systemEditPageName)
	v.app.systemPopup = item
	v.app.systemPopupCancel = func() {
		if cancel != nil {
			cancel()
		}
		v.closePopup()
	}
	v.app.pages.AddPage(systemEditPageName, item, true, true)
	if v.app.app != nil {
		v.app.app.SetFocus(item)
	}
}

func (v *settingsView) closePopup() {
	if v.app != nil && v.app.pages != nil {
		v.app.pages.RemovePage(systemEditPageName)
		v.app.systemPopup = nil
		v.app.systemPopupCancel = nil
	}
	if v.app != nil && v.app.app != nil {
		v.saveFocused = false
		v.app.focusTarget = appFocusContent
		v.app.contentFocus = v
		v.app.app.SetFocus(v)
	}
}

func (v *settingsView) save() {
	v.cfg.Settings = v.settings
	cfg := v.cfg
	endpoints := enabledEndpoints(cfg.Account.Endpoints)
	if len(endpoints) > 1 && v.app != nil && v.app.client != nil {
		v.setStatusText(v.statusText("Validating endpoints before save...", cfg.Account.Endpoints, nil, true, ""))
		go v.validateAndSave(cfg, endpoints)
		return
	}
	v.finishSave(cfg, "Saved")
}

func (v *settingsView) validateAndSave(cfg models.Config, endpoints []models.Endpoint) {
	baseCtx := context.Background()
	if v.app != nil && v.app.ctx != nil {
		baseCtx = v.app.ctx
	}
	ctx, cancel := context.WithTimeout(baseCtx, settingsProbeTimeout)
	identities := v.app.client.ProbeEndpointIdentities(ctx, endpoints)
	cancel()
	err := validateEndpointIdentities(identities)
	v.queueUpdate(func() {
		if err != nil {
			v.setStatusText(v.identityStatusText("Save failed: "+err.Error(), identities))
			return
		}
		v.finishSave(cfg, "Saved")
	})
}

func validateEndpointIdentities(identities []subsonic.EndpointIdentity) error {
	if len(identities) <= 1 {
		return nil
	}
	var fingerprint string
	for _, identity := range identities {
		if identity.Err != nil {
			return fmt.Errorf("%s: %w", identity.Endpoint.URL, identity.Err)
		}
		if fingerprint == "" {
			fingerprint = identity.Fingerprint
			continue
		}
		if identity.Fingerprint != fingerprint {
			return errors.New("Endpoints appear to point to different libraries")
		}
	}
	return nil
}

func (v *settingsView) finishSave(cfg models.Config, message string) {
	if v.app == nil {
		return
	}
	cfg = v.app.apply(cfg)
	v.cfg = cfg
	v.settings = cfg.Settings
	v.app.cfg = cfg
	v.refreshSettingsList()
	if err := v.saveConfig(cfg); err != nil {
		v.setStatusText(v.statusText("Save failed: "+err.Error(), cfg.Account.Endpoints, nil, false, ""))
		v.app.modal("Save failed", err.Error())
		return
	}
	v.startProbeNow(message)
}

func (v *settingsView) startProbeAfterDelay(message string) {
	v.startProbe(message, settingsProbeDelay)
}

func (v *settingsView) startProbeNow(message string) {
	v.startProbe(message, 0)
}

func (v *settingsView) startProbe(message string, delay time.Duration) {
	endpoints := cloneEndpoints(v.cfg.Account.Endpoints)
	if v.app == nil || v.app.client == nil {
		v.setStatusText(v.statusText(message, endpoints, nil, false, "Endpoint ping unavailable."))
		return
	}

	v.probeMu.Lock()
	v.probeSeq++
	seq := v.probeSeq
	if v.probeTimer != nil {
		v.probeTimer.Stop()
		v.probeTimer = nil
	}
	if len(endpoints) == 0 {
		v.probeMu.Unlock()
		v.setStatusText(v.statusText(message, endpoints, nil, false, "No endpoints configured."))
		return
	}
	if delay > 0 {
		v.probeTimer = time.AfterFunc(delay, func() {
			v.runProbe(seq, message, endpoints)
		})
	} else {
		go v.runProbe(seq, message, endpoints)
	}
	v.probeMu.Unlock()

	v.setStatusText(v.statusText(message, endpoints, nil, true, ""))
}

func (v *settingsView) runProbe(seq int, message string, endpoints []models.Endpoint) {
	baseCtx := context.Background()
	if v.app != nil && v.app.ctx != nil {
		baseCtx = v.app.ctx
	}
	ctx, cancel := context.WithTimeout(baseCtx, settingsProbeTimeout)
	probes := v.app.client.ProbeEndpoints(ctx, endpoints)
	cancel()

	v.queueUpdate(func() {
		if !v.acceptProbe(seq) {
			return
		}
		v.setStatusText(v.statusText(message, endpoints, probes, false, ""))
	})
}

func (v *settingsView) acceptProbe(seq int) bool {
	v.probeMu.Lock()
	defer v.probeMu.Unlock()
	return seq == v.probeSeq
}

func (v *settingsView) queueUpdate(fn func()) {
	if v.app != nil && v.app.app != nil {
		v.app.app.QueueUpdateDraw(fn)
		return
	}
	fn()
}

func (v *settingsView) setStatusText(text string) {
	v.status.SetText(text)
}

func (v *settingsView) statusText(message string, endpoints []models.Endpoint, probes []subsonic.EndpointProbe, checking bool, fallback string) string {
	var builder strings.Builder
	if fallback != "" {
		if message != "" {
			builder.WriteString("[::b]")
			builder.WriteString(tview.Escape(message))
			builder.WriteString("[-] ")
		}
		builder.WriteString(tview.Escape(fallback))
		return strings.TrimRight(builder.String(), "\n")
	}
	if len(endpoints) == 0 {
		if message != "" {
			builder.WriteString("[::b]")
			builder.WriteString(tview.Escape(message))
			builder.WriteString("[-] ")
		}
		builder.WriteString("No endpoints configured.")
		return strings.TrimRight(builder.String(), "\n")
	}
	if checking {
		if message != "" {
			builder.WriteString("[::b]")
			builder.WriteString(tview.Escape(message))
			builder.WriteString("[-] ")
		}
		builder.WriteString("Checking endpoints...")
		return strings.TrimRight(builder.String(), "\n")
	}

	activeURL := ""
	if v.app != nil && v.app.client != nil {
		activeURL = strings.TrimRight(v.app.client.ActiveEndpoint().URL, "/")
	}
	if len(probes) == 0 {
		if message != "" {
			builder.WriteString("[::b]")
			builder.WriteString(tview.Escape(message))
			builder.WriteString("[-]")
		}
		return strings.TrimRight(builder.String(), "\n")
	}
	for _, probe := range probes {
		if message != "" {
			builder.WriteString("[::b]")
			builder.WriteString(tview.Escape(message))
			builder.WriteString("[-] | ")
			message = ""
		}
		mark := " "
		if activeURL != "" && strings.TrimRight(probe.Endpoint.URL, "/") == activeURL {
			mark = "*"
		}
		builder.WriteString(mark)
		builder.WriteByte(' ')
		if probe.Err == nil {
			builder.WriteString("[green]OK[-] ")
			builder.WriteString(probe.Latency.Round(time.Millisecond).String())
		} else {
			builder.WriteString("[red]ERR[-] ")
			builder.WriteString(tview.Escape(shortEndpointError(probe.Err)))
		}
		builder.WriteByte(' ')
		builder.WriteString(tview.Escape(probe.Endpoint.URL))
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (v *settingsView) identityStatusText(message string, identities []subsonic.EndpointIdentity) string {
	var builder strings.Builder
	if message != "" {
		builder.WriteString("[red]")
		builder.WriteString(tview.Escape(message))
		builder.WriteString("[-]\n")
	}
	for _, identity := range identities {
		if identity.Err != nil {
			builder.WriteString("  [red]ERR[-] ")
			builder.WriteString(tview.Escape(shortEndpointError(identity.Err)))
		} else {
			builder.WriteString("  [green]OK[-] artists=")
			builder.WriteString(strconv.Itoa(identity.ArtistCount))
		}
		builder.WriteByte(' ')
		builder.WriteString(tview.Escape(identity.Endpoint.URL))
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (v *settingsView) aboutText() string {
	var builder strings.Builder
	for _, line := range v.aboutIntroLines() {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	builder.WriteByte('\n')
	builder.WriteString(formatKVSection("Resolved Config", v.resolvedConfigRows()))
	builder.WriteString("\n\n")
	builder.WriteString(formatKVSection("Media Support", v.mediaSupportRows()))
	return strings.TrimRight(builder.String(), "\n")
}

func (v *settingsView) aboutIntroLines() []string {
	return []string{
		fmt.Sprintf("This is Saki %s (Subsonic Audio Klient for Individuals).", version.String()),
		"Made with ♪&♥ by Nemo Xiong.",
		"Repository: https://github.com/xiongnemo/Saki",
	}
}

func (v *settingsView) resolvedConfigRows() []kvRow {
	configPath := "Unavailable"
	if v.app != nil {
		if path, err := v.app.store.Path(); err == nil {
			configPath = path
		}
	}
	activeEndpoint := "Not connected"
	if v.app != nil && v.app.client != nil {
		if active := v.app.client.ActiveEndpoint(); active.URL != "" {
			activeEndpoint = active.URL
		}
	}
	return []kvRow{
		{label: "Config path", value: configPath},
		{label: "Username", value: fallbackText(v.cfg.Account.Username, "Not configured")},
		{label: "Active endpoint", value: activeEndpoint},
		{label: "Endpoints", value: fmt.Sprintf("%d enabled / %d total", len(enabledEndpoints(v.cfg.Account.Endpoints)), len(v.cfg.Account.Endpoints))},
		{label: "Configured backend", value: fallbackText(v.cfg.Settings.AudioBackend, "auto")},
		{label: "MPV path", value: fallbackText(v.cfg.Settings.MPVPath, "PATH lookup")},
		{label: "Bundled mpv", value: onOffOptions[boolOption(v.cfg.Settings.UseBundledMPV)]},
		{label: "Cache dir", value: resolvedAudioCacheDir(v.cfg.Settings)},
		{label: "Audio cache limit", value: formatBytes(v.cfg.Settings.AudioCacheMaxBytes)},
		{label: "Health interval", value: fmt.Sprintf("%ds", v.cfg.Settings.HealthCheckIntervalSeconds)},
		{label: "OS/Arch", value: runtime.GOOS + "/" + runtime.GOARCH},
	}
}

func (v *settingsView) mediaSupportRows() []kvRow {
	activeCover := "Cell"
	if v.app != nil && v.app.cover != nil {
		activeCover = v.app.cover.ActiveRendererLabel()
	}
	return []kvRow{
		{label: "Supported backends", value: "auto, miniaudio, mpv"},
		{label: "Active backend", value: v.activeAudioBackend()},
		{label: "Configured decoders", value: "MP3, WAV PCM/float, FLAC, ALAC/M4A"},
		{label: "Streaming support", value: "HTTP Range, local proxy cache, mpv fallback"},
		{label: "Audio output", value: "miniaudio default device, signed 16-bit PCM"},
		{label: "Supported cover renderers", value: supportedCoverRenderersLabel()},
		{label: "Active cover renderer", value: activeCover},
	}
}

func (v *settingsView) activeAudioBackend() string {
	if v.app == nil || v.app.player == nil {
		return "None"
	}
	return backendLabel(v.app.player.ActiveBackend())
}

func backendLabel(backend string) string {
	switch strings.ToLower(strings.TrimSpace(backend)) {
	case "miniaudio":
		return "miniaudio"
	case "mpv":
		return "mpv"
	case "custom":
		return "custom"
	default:
		return "None"
	}
}

func (v *settingsView) drawAbout(screen tcell.Screen) {
	rect := v.contentRect
	if rect.width <= 0 || rect.height <= 0 {
		return
	}
	y := rect.y
	for _, line := range v.aboutIntroLines() {
		if y >= rect.y+rect.height {
			return
		}
		tview.Print(screen, line, rect.x, y, rect.width, tview.AlignLeft, uiText)
		y++
	}
	y++
	if y >= rect.y+rect.height {
		return
	}
	availableHeight := rect.y + rect.height - y
	if availableHeight <= 0 {
		return
	}
	if rect.width >= 96 && availableHeight >= 5 {
		gap := 2
		leftWidth := (rect.width - gap) / 2
		rightWidth := rect.width - gap - leftWidth
		left := settingsRect{x: rect.x, y: y, width: leftWidth, height: availableHeight}
		right := settingsRect{x: rect.x + leftWidth + gap, y: y, width: rightWidth, height: availableHeight}
		drawKVPanel(screen, left, "Resolved Config", v.resolvedConfigRows())
		drawKVPanel(screen, right, "Media Support", v.mediaSupportRows())
		return
	}
	topHeight := availableHeight / 2
	if topHeight < 4 {
		topHeight = availableHeight
	}
	drawKVPanel(screen, settingsRect{x: rect.x, y: y, width: rect.width, height: topHeight}, "Resolved Config", v.resolvedConfigRows())
	bottomY := y + topHeight + 1
	bottomHeight := rect.y + rect.height - bottomY
	if bottomHeight > 0 {
		drawKVPanel(screen, settingsRect{x: rect.x, y: bottomY, width: rect.width, height: bottomHeight}, "Media Support", v.mediaSupportRows())
	}
}

func formatKVSection(title string, rows []kvRow) string {
	var builder strings.Builder
	builder.WriteString(title)
	builder.WriteByte('\n')
	labelWidth := kvLabelWidth(rows, 0)
	for _, row := range rows {
		builder.WriteString(row.label)
		if padding := labelWidth - len([]rune(row.label)); padding > 0 {
			builder.WriteString(strings.Repeat(" ", padding))
		}
		builder.WriteString(tview.Escape(row.value))
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
}

func drawKVPanel(screen tcell.Screen, rect settingsRect, title string, rows []kvRow) {
	if rect.width <= 0 || rect.height <= 0 {
		return
	}
	box := tview.NewBox().SetBorder(true).SetBorderColor(uiBorder).SetTitleColor(uiTitle).SetBackgroundColor(uiBackground)
	setPlainTitle(box, title)
	box.SetRect(rect.x, rect.y, rect.width, rect.height)
	box.Draw(screen)

	innerX := rect.x + 1
	innerY := rect.y + 1
	innerWidth := rect.width - 2
	innerHeight := rect.height - 2
	if innerWidth <= 0 || innerHeight <= 0 {
		return
	}
	labelWidth := kvLabelWidth(rows, innerWidth)
	for index, row := range rows {
		if index >= innerHeight {
			break
		}
		y := innerY + index
		drawStyledText(screen, innerX, y, labelWidth, row.label, tcell.StyleDefault.Foreground(uiLabel).Background(uiBackground))
		drawStyledText(screen, innerX+labelWidth, y, innerWidth-labelWidth, row.value, tcell.StyleDefault.Foreground(uiText).Background(uiBackground))
	}
}

func kvLabelWidth(rows []kvRow, maxWidth int) int {
	width := 0
	for _, row := range rows {
		if labelWidth := len([]rune(row.label)); labelWidth > width {
			width = labelWidth
		}
	}
	width += 2
	if maxWidth > 0 && width >= maxWidth {
		return maxWidth
	}
	return width
}

func resolvedAudioCacheDir(settings models.Settings) string {
	cacheDir := settings.CacheDir
	if cacheDir == "" {
		if userCache, err := os.UserCacheDir(); err == nil {
			cacheDir = filepath.Join(userCache, "saki")
		} else {
			cacheDir = filepath.Join(os.TempDir(), "saki")
		}
	}
	return filepath.Join(cacheDir, "audio")
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatBytes(bytes int64) string {
	if bytes <= 0 {
		return "0 B"
	}
	const mib = 1024 * 1024
	const gib = 1024 * mib
	if bytes%gib == 0 {
		return fmt.Sprintf("%d GiB", bytes/gib)
	}
	if bytes >= gib {
		return fmt.Sprintf("%.1f GiB", float64(bytes)/gib)
	}
	if bytes%mib == 0 {
		return fmt.Sprintf("%d MiB", bytes/mib)
	}
	if bytes >= mib {
		return fmt.Sprintf("%.1f MiB", float64(bytes)/mib)
	}
	return fmt.Sprintf("%d B", bytes)
}

func enabledEndpoints(endpoints []models.Endpoint) []models.Endpoint {
	enabled := make([]models.Endpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.Enabled && strings.TrimSpace(endpoint.URL) != "" {
			enabled = append(enabled, endpoint)
		}
	}
	return enabled
}

func shortEndpointError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ReplaceAll(err.Error(), "\n", " ")
	if len(text) > 72 {
		return text[:69] + "..."
	}
	return text
}

func cloneEndpoints(endpoints []models.Endpoint) []models.Endpoint {
	cloned := make([]models.Endpoint, len(endpoints))
	copy(cloned, endpoints)
	return cloned
}

func settingsClampInt(value, minValue, maxValue int) int {
	if maxValue < minValue {
		return minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
