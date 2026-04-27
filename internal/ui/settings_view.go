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
)

var settingsPreferredFieldWidths = map[string]int{
	"Endpoints (; separated)": 72,
	"Audio backend":           10,
	"MPV path":                72,
	"Cache dir":               72,
	"Audio cache MB":          12,
	"Health interval sec":     8,
	"Switch threshold":        8,
	"Prefetch":                3,
	"Bundled mpv":             3,
}

type systemTab int

const (
	systemTabAbout systemTab = iota
	systemTabSettings
	systemTabProperties
)

var systemTabLabels = []string{"About", "Settings", "Properties"}

type settingsRect struct {
	x, y, width, height int
}

func (r settingsRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

type settingsView struct {
	*tview.Box

	app      *App
	form     *tview.Form
	content  *tview.TextView
	status   *tview.TextView
	cfg      models.Config
	settings models.Settings

	activeTab   systemTab
	tabRects    []settingsRect
	contentRect settingsRect
	formRect    settingsRect
	statusRect  settingsRect

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

	view.form = view.buildForm()
	view.setStatusText(view.statusText("", view.cfg.Account.Endpoints, nil, false, "Waiting to ping."))
	return view
}

func (v *settingsView) buildForm() *tview.Form {
	cacheMaxMB := v.settings.AudioCacheMaxBytes / 1024 / 1024
	if cacheMaxMB <= 0 {
		cacheMaxMB = 2048
	}

	form := tview.NewForm().
		AddInputField("Endpoints (; separated)", endpointsToText(v.cfg.Account.Endpoints), settingsPreferredFieldWidths["Endpoints (; separated)"], nil, func(value string) {
			v.cfg.Account.Endpoints = parseEndpoints(value)
			v.startProbeAfterDelay("")
		}).
		AddDropDown("Audio backend", audioBackendLabels, audioBackendOption(v.settings.AudioBackend), func(_ string, index int) {
			if index >= 0 && index < len(audioBackendValues) {
				v.settings.AudioBackend = audioBackendValues[index]
			}
		}).
		AddInputField("MPV path", v.settings.MPVPath, settingsPreferredFieldWidths["MPV path"], nil, func(value string) {
			v.settings.MPVPath = strings.TrimSpace(value)
		}).
		AddInputField("Cache dir", v.settings.CacheDir, settingsPreferredFieldWidths["Cache dir"], nil, func(value string) {
			v.settings.CacheDir = strings.TrimSpace(value)
		}).
		AddInputField("Audio cache MB", strconv.FormatInt(cacheMaxMB, 10), settingsPreferredFieldWidths["Audio cache MB"], tview.InputFieldInteger, func(value string) {
			if mb, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && mb > 0 {
				v.settings.AudioCacheMaxBytes = mb * 1024 * 1024
			}
		}).
		AddInputField("Health interval sec", strconv.Itoa(v.settings.HealthCheckIntervalSeconds), settingsPreferredFieldWidths["Health interval sec"], tview.InputFieldInteger, func(value string) {
			if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds > 0 {
				v.settings.HealthCheckIntervalSeconds = seconds
			}
		}).
		AddInputField("Switch threshold", fmt.Sprintf("%.2f", v.settings.EndpointSwitchThreshold), settingsPreferredFieldWidths["Switch threshold"], nil, func(value string) {
			if threshold, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && threshold > 0 && threshold < 1 {
				v.settings.EndpointSwitchThreshold = threshold
			}
		}).
		AddDropDown("Prefetch", onOffOptions, boolOption(v.settings.EnablePrefetch), func(_ string, index int) {
			v.settings.EnablePrefetch = index == 1
		}).
		AddDropDown("Bundled mpv", onOffOptions, boolOption(v.settings.UseBundledMPV), func(_ string, index int) {
			v.settings.UseBundledMPV = index == 1
		}).
		AddButton("Save", func() {
			v.save()
		}).
		AddButton("Back", func() {
			v.app.goBack()
		})

	form.SetCancelFunc(func() {
		v.app.goBack()
	})
	form.SetFocus(form.GetFormItemIndex("Audio backend"))
	styleForm(form)
	form.SetBorder(false)
	form.SetItemPadding(0)
	return form
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
			v.resizeFormFields(v.contentRect.width)
			v.form.SetRect(v.contentRect.x, v.contentRect.y, v.contentRect.width, v.contentRect.height)
			v.formRect = v.contentRect
			v.form.Draw(screen)
		case systemTabProperties:
			v.content.SetText(v.propertiesText())
			v.content.SetRect(v.contentRect.x, v.contentRect.y, v.contentRect.width, v.contentRect.height)
			v.content.Draw(screen)
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
		v.formRect = v.contentRect
	} else {
		v.formRect = settingsRect{}
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

func (v *settingsView) resizeFormFields(width int) {
	labelWidth := settingsFormLabelWidth(v.form)
	fieldWidth := width - labelWidth
	if fieldWidth < 1 {
		fieldWidth = 1
	}

	for i := 0; i < v.form.GetFormItemCount(); i++ {
		item := v.form.GetFormItem(i)
		preferred := settingsPreferredFieldWidths[item.GetLabel()]
		if preferred <= 0 {
			preferred = fieldWidth
		}
		resized := settingsClampInt(preferred, 1, fieldWidth)
		switch item := item.(type) {
		case *tview.InputField:
			item.SetFieldWidth(resized)
		case *tview.DropDown:
			item.SetFieldWidth(resized)
		case *tview.TextView:
			item.SetSize(1, resized)
		}
	}
}

func settingsFormLabelWidth(form *tview.Form) int {
	width := 0
	for i := 0; i < form.GetFormItemCount(); i++ {
		labelWidth := tview.TaggedStringWidth(form.GetFormItem(i).GetLabel())
		if labelWidth > width {
			width = labelWidth
		}
	}
	return width + 1
}

func (v *settingsView) Focus(delegate func(p tview.Primitive)) {
	if v.activeTab == systemTabSettings {
		v.form.Focus(delegate)
		return
	}
	v.Box.Focus(delegate)
}

func (v *settingsView) HasFocus() bool {
	return v.form.HasFocus() || v.Box.HasFocus()
}

func (v *settingsView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return v.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if v.handleTabShortcut(event, setFocus) {
			return
		}
		if v.activeTab == systemTabSettings {
			if handler := v.form.InputHandler(); handler != nil {
				handler(event, setFocus)
			}
		}
	})
}

func (v *settingsView) handleTabShortcut(event *tcell.EventKey, setFocus func(p tview.Primitive)) bool {
	if v.activeTab == systemTabSettings && v.app != nil && v.app.app != nil && acceptsTextInput(v.app.app.GetFocus()) {
		return false
	}
	if event.Key() != tcell.KeyRune {
		return false
	}
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
	case '3':
		v.setActiveTab(systemTabProperties, setFocus)
		return true
	}
	return false
}

func (v *settingsView) setActiveTab(tab systemTab, setFocus func(tview.Primitive)) {
	if tab < systemTabAbout {
		tab = systemTabProperties
	}
	if tab > systemTabProperties {
		tab = systemTabAbout
	}
	v.activeTab = tab
	if tab == systemTabSettings {
		v.form.Focus(setFocus)
		return
	}
	setFocus(v)
}

func (v *settingsView) PasteHandler() func(pastedText string, setFocus func(p tview.Primitive)) {
	return v.WrapPasteHandler(func(pastedText string, setFocus func(p tview.Primitive)) {
		if v.activeTab != systemTabSettings {
			return
		}
		if handler := v.form.PasteHandler(); handler != nil {
			handler(pastedText, setFocus)
		}
	})
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

		switch action {
		case tview.MouseScrollUp:
			if v.activeTab == systemTabSettings && v.formRect.contains(x, y) {
				v.moveFormFocus(-1, wrappedSetFocus)
				return true, nil
			}
		case tview.MouseScrollDown:
			if v.activeTab == systemTabSettings && v.formRect.contains(x, y) {
				v.moveFormFocus(1, wrappedSetFocus)
				return true, nil
			}
		}

		if v.activeTab == systemTabSettings && v.formRect.contains(x, y) {
			if handler := v.form.MouseHandler(); handler != nil {
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

func (v *settingsView) moveFormFocus(delta int, setFocus func(tview.Primitive)) {
	count := v.form.GetFormItemCount() + v.form.GetButtonCount()
	if count == 0 {
		return
	}
	current := v.currentFormFocus()
	if current < 0 {
		current = v.form.GetFormItemIndex("Audio backend")
	}
	if current < 0 {
		current = 0
	}
	next := settingsClampInt(current+delta, 0, count-1)
	v.form.SetFocus(next)
	v.form.Focus(setFocus)
}

func (v *settingsView) currentFormFocus() int {
	for i := 0; i < v.form.GetFormItemCount(); i++ {
		if v.form.GetFormItem(i).HasFocus() {
			return i
		}
	}
	for i := 0; i < v.form.GetButtonCount(); i++ {
		if v.form.GetButton(i).HasFocus() {
			return v.form.GetFormItemCount() + i
		}
	}
	return -1
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
	cfg = v.app.apply(cfg)
	v.cfg = cfg
	v.settings = cfg.Settings
	v.app.cfg = cfg
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
	if message != "" {
		builder.WriteString("[::b]")
		builder.WriteString(tview.Escape(message))
		builder.WriteString("[-]\n")
	}
	if fallback != "" {
		builder.WriteString(tview.Escape(fallback))
		return strings.TrimRight(builder.String(), "\n")
	}
	if len(endpoints) == 0 {
		builder.WriteString("No endpoints configured.")
		return strings.TrimRight(builder.String(), "\n")
	}
	if checking {
		builder.WriteString("Checking endpoints...")
		return strings.TrimRight(builder.String(), "\n")
	}

	activeURL := ""
	if v.app != nil && v.app.client != nil {
		activeURL = strings.TrimRight(v.app.client.ActiveEndpoint().URL, "/")
	}
	for _, probe := range probes {
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
	endpoint := "Not connected"
	if v.app != nil && v.app.client != nil {
		if active := v.app.client.ActiveEndpoint(); active.URL != "" {
			endpoint = active.URL
		}
	}
	return fmt.Sprintf("This is Saki %s\nMade with music by Nemo Xiong\nRepository: https://github.com/xiongnemo/Saki\n\nConnected endpoint:\n%s", version.String(), endpoint)
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

	builder.WriteByte('\n')
	state := models.CurrentState{}
	if v.app != nil && v.app.player != nil {
		state = v.app.player.State()
	}
	if state.CurrentTrack == nil {
		builder.WriteString("No track loaded")
		return strings.TrimRight(builder.String(), "\n")
	}
	writeProperty("Current track", state.CurrentTrack.Artist+" :: "+state.CurrentTrack.Album+" :: "+state.CurrentTrack.Title)
	if label := audioInfoLabel(state.AudioInfo, 80); label != "" {
		writeProperty("Audio input", label)
	} else {
		writeProperty("Audio input", "Unknown")
	}
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
