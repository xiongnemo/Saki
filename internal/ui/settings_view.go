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

type settingsView struct {
	*tview.Box

	app          *App
	settingsList *tview.List
	content      *tview.TextView
	status       *tview.TextView
	cfg          models.Config
	settings     models.Settings

	activeTab        systemTab
	tabRects         []settingsRect
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

	view.settingsList = view.buildSettingsList()
	view.refreshSettingsList()
	view.setStatusText(view.statusText("", view.cfg.Account.Endpoints, nil, false, "Waiting to ping."))
	return view
}

func (v *settingsView) buildSettingsList() *tview.List {
	list := tview.NewList().ShowSecondaryText(false)
	styleList(list)
	list.SetFocusFunc(func() {
		applyListFocusStyle(list, true)
		if v.app != nil {
			v.app.focusTarget = appFocusContent
			v.app.contentFocus = list
		}
	})
	list.SetBlurFunc(func() {
		applyListFocusStyle(list, false)
	})
	return list
}

func (v *settingsView) refreshSettingsList() {
	if v.settingsList == nil {
		return
	}
	current := v.settingsList.GetCurrentItem()
	v.settingsList.Clear()
	v.settingsList.AddItem(settingListLine("Endpoints (; separated)", endpointsToText(v.cfg.Account.Endpoints)), "", 0, func() {
		v.openEndpointPopup()
	})
	v.settingsList.AddItem(settingListLine("Audio backend", fallbackText(v.settings.AudioBackend, "auto")), "", 0, func() {
		v.openChoicePopup("Audio backend", audioBackendLabels, audioBackendOption(v.settings.AudioBackend), func(index int) error {
			if index < 0 || index >= len(audioBackendValues) {
				return errors.New("invalid audio backend")
			}
			v.settings.AudioBackend = audioBackendValues[index]
			v.refreshSettingsList()
			return nil
		})
	})
	v.settingsList.AddItem(settingListLine("MPV path", fallbackText(v.settings.MPVPath, "PATH lookup")), "", 0, func() {
		v.openTextPopup("MPV path", v.settings.MPVPath, func(value string) error {
			v.settings.MPVPath = strings.TrimSpace(value)
			v.refreshSettingsList()
			return nil
		})
	})
	v.settingsList.AddItem(settingListLine("Cache dir", fallbackText(v.settings.CacheDir, "default")), "", 0, func() {
		v.openTextPopup("Cache dir", v.settings.CacheDir, func(value string) error {
			v.settings.CacheDir = strings.TrimSpace(value)
			v.refreshSettingsList()
			return nil
		})
	})
	cacheMaxMB := v.settings.AudioCacheMaxBytes / 1024 / 1024
	if cacheMaxMB <= 0 {
		cacheMaxMB = 2048
	}
	v.settingsList.AddItem(settingListLine("Audio cache MB", strconv.FormatInt(cacheMaxMB, 10)), "", 0, func() {
		v.openTextPopup("Audio cache MB", strconv.FormatInt(cacheMaxMB, 10), func(value string) error {
			mb, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || mb <= 0 {
				return errors.New("audio cache MB must be a positive integer")
			}
			v.settings.AudioCacheMaxBytes = mb * 1024 * 1024
			v.refreshSettingsList()
			return nil
		})
	})
	v.settingsList.AddItem(settingListLine("Health interval sec", strconv.Itoa(v.settings.HealthCheckIntervalSeconds)), "", 0, func() {
		v.openTextPopup("Health interval sec", strconv.Itoa(v.settings.HealthCheckIntervalSeconds), func(value string) error {
			seconds, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || seconds <= 0 {
				return errors.New("health interval must be a positive integer")
			}
			v.settings.HealthCheckIntervalSeconds = seconds
			v.refreshSettingsList()
			return nil
		})
	})
	v.settingsList.AddItem(settingListLine("Switch threshold", fmt.Sprintf("%.2f", v.settings.EndpointSwitchThreshold)), "", 0, func() {
		v.openTextPopup("Switch threshold", fmt.Sprintf("%.2f", v.settings.EndpointSwitchThreshold), func(value string) error {
			threshold, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err != nil || threshold <= 0 || threshold >= 1 {
				return errors.New("switch threshold must be greater than 0 and less than 1")
			}
			v.settings.EndpointSwitchThreshold = threshold
			v.refreshSettingsList()
			return nil
		})
	})
	v.settingsList.AddItem(settingListLine("Prefetch", onOffOptions[boolOption(v.settings.EnablePrefetch)]), "", 0, func() {
		v.openChoicePopup("Prefetch", onOffOptions, boolOption(v.settings.EnablePrefetch), func(index int) error {
			v.settings.EnablePrefetch = index == 1
			v.refreshSettingsList()
			return nil
		})
	})
	v.settingsList.AddItem(settingListLine("Bundled mpv", onOffOptions[boolOption(v.settings.UseBundledMPV)]), "", 0, func() {
		v.openChoicePopup("Bundled mpv", onOffOptions, boolOption(v.settings.UseBundledMPV), func(index int) error {
			v.settings.UseBundledMPV = index == 1
			v.refreshSettingsList()
			return nil
		})
	})
	setListCurrentItem(v.settingsList, current)
}

func settingListLine(label, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "(empty)"
	}
	return label + "  " + value
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
			v.settingsList.SetRect(v.settingsListRect.x, v.settingsListRect.y, v.settingsListRect.width, v.settingsListRect.height)
			v.settingsList.Draw(screen)
			v.drawSaveButton(screen)
		default:
			v.content.SetText(v.aboutText())
			v.content.SetRect(v.contentRect.x, v.contentRect.y, v.contentRect.width, v.contentRect.height)
			v.content.Draw(screen)
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
	if v.activeTab == systemTabSettings {
		if v.saveFocused {
			v.Box.Focus(delegate)
			return
		}
		v.settingsList.Focus(delegate)
		return
	}
	v.Box.Focus(delegate)
}

func (v *settingsView) HasFocus() bool {
	return v.settingsList.HasFocus() || v.Box.HasFocus()
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
			if v.handleSettingsListKey(event, setFocus) {
				return
			}
			if handler := v.settingsList.InputHandler(); handler != nil {
				handler(event, setFocus)
			}
			return
		}
		if handler := v.content.InputHandler(); handler != nil {
			handler(event, setFocus)
		}
	})
}

func (v *settingsView) handleSettingsListKey(event *tcell.EventKey, setFocus func(p tview.Primitive)) bool {
	switch event.Key() {
	case tcell.KeyTab:
		v.focusSaveButton(setFocus)
		return true
	case tcell.KeyDown:
		if v.settingsList.GetItemCount() > 0 && v.settingsList.GetCurrentItem() >= v.settingsList.GetItemCount()-1 {
			v.focusSaveButton(setFocus)
			return true
		}
	}
	return false
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
		if v.saveFocused {
			v.Box.Focus(setFocus)
		} else {
			v.settingsList.Focus(setFocus)
		}
		return
	}
	setFocus(v)
}

func (v *settingsView) focusSettingsList(setFocus func(tview.Primitive)) {
	v.saveFocused = false
	applyListFocusStyle(v.settingsList, true)
	v.settingsList.Focus(setFocus)
}

func (v *settingsView) focusSaveButton(setFocus func(tview.Primitive)) {
	v.saveFocused = true
	applyListFocusStyle(v.settingsList, false)
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
	if v.settingsList.GetItemCount() == 0 {
		return
	}
	setListCurrentItem(v.settingsList, v.settingsList.GetCurrentItem()+delta)
	v.settingsList.Focus(setFocus)
}

func (v *settingsView) selectSettingsRow(y int, setFocus func(tview.Primitive)) {
	index := y - v.settingsListRect.y
	if index < 0 || index >= v.settingsList.GetItemCount() {
		return
	}
	v.settingsList.SetCurrentItem(index)
	v.settingsList.Focus(setFocus)
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
	form := tview.NewForm()
	current := value
	form.AddInputField(label, value, fieldWidth, nil, func(text string) {
		current = text
		if live != nil {
			live(text)
		}
	})
	form.AddButton("OK", func() {
		if accept != nil {
			if err := accept(current); err != nil {
				v.setStatusText(v.statusText("Invalid value: "+err.Error(), v.cfg.Account.Endpoints, nil, false, ""))
				return
			}
		}
		v.closePopup()
	})
	form.AddButton("Cancel", func() {
		if cancel != nil {
			cancel()
		}
		v.closePopup()
	})
	form.SetCancelFunc(func() {
		if cancel != nil {
			cancel()
		}
		v.closePopup()
	})
	styleForm(form)
	form.SetItemPadding(0)
	form.SetBorder(true)
	form.SetBorderColor(uiBorder)
	form.SetTitleColor(uiTitle)
	setPlainTitle(form, title)
	form.SetFocus(0)
	width := settingsClampInt(fieldWidth+26, 40, 100)
	v.showPopup(form, width, 7, cancel)
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
	v.showPopup(form, 52, 7, nil)
}

func (v *settingsView) showPopup(item tview.Primitive, width, height int, cancel func()) {
	v.app.pages.RemovePage(systemEditPageName)
	v.app.systemPopupCancel = func() {
		if cancel != nil {
			cancel()
		}
		v.closePopup()
	}
	v.app.pages.AddPage(systemEditPageName, center(item, width, height), true, true)
	if v.app.app != nil {
		v.app.app.SetFocus(item)
	}
}

func (v *settingsView) closePopup() {
	if v.app != nil && v.app.pages != nil {
		v.app.pages.RemovePage(systemEditPageName)
		v.app.systemPopupCancel = nil
	}
	if v.app != nil && v.app.app != nil {
		v.saveFocused = false
		v.app.focusTarget = appFocusContent
		v.app.contentFocus = v.settingsList
		v.app.app.SetFocus(v.settingsList)
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
	return fmt.Sprintf("This is Saki %s (Subsonic Audio Klient for Individuals).\nMade with 🎵&❤ by Nemo Xiong.\nRepository: https://github.com/xiongnemo/Saki\n\nProperties\n%s", version.String(), v.propertiesText())
}

func (v *settingsView) propertiesText() string {
	var builder strings.Builder
	writeProperty := func(label, value string) {
		builder.WriteString("[#8ee3ff]")
		builder.WriteString(label)
		builder.WriteString("[-] ")
		builder.WriteString(tview.Escape(value))
		builder.WriteByte('\n')
	}

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
	writeProperty("Config path", configPath)
	writeProperty("Username", fallbackText(v.cfg.Account.Username, "Not configured"))
	writeProperty("Active endpoint", activeEndpoint)
	writeProperty("Endpoints", fmt.Sprintf("%d enabled / %d total", len(enabledEndpoints(v.cfg.Account.Endpoints)), len(v.cfg.Account.Endpoints)))
	writeProperty("Audio backend", fallbackText(v.cfg.Settings.AudioBackend, "auto"))
	writeProperty("MPV path", fallbackText(v.cfg.Settings.MPVPath, "PATH lookup"))
	writeProperty("Bundled mpv", onOffOptions[boolOption(v.cfg.Settings.UseBundledMPV)])
	writeProperty("Cache dir", resolvedAudioCacheDir(v.cfg.Settings))
	writeProperty("Audio cache limit", formatBytes(v.cfg.Settings.AudioCacheMaxBytes))
	writeProperty("Health interval", fmt.Sprintf("%ds", v.cfg.Settings.HealthCheckIntervalSeconds))
	writeProperty("OS/Arch", runtime.GOOS+"/"+runtime.GOARCH)
	writeProperty("Input decoders", "MP3, WAV PCM/float, FLAC, ALAC/M4A")
	writeProperty("Streaming support", "HTTP Range, local proxy cache, mpv fallback")
	writeProperty("Audio output", "miniaudio default device, signed 16-bit PCM")
	writeProperty("Cover renderers", "Kitty, iTerm2, Sixel, cell fallback")
	writeProperty("Cover mode", fallbackText(os.Getenv("SAKI_COVER_RENDERER"), "auto"))
	return strings.TrimRight(builder.String(), "\n")
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
