package ui

import (
	"context"
	"fmt"
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
	settingsWideThreshold  = 120
	settingsStatusMinWidth = 32
	settingsFormMaxWidth   = 96
	settingsProbeDelay     = 500 * time.Millisecond
	settingsProbeTimeout   = 5 * time.Second
)

var settingsPreferredFieldWidths = map[string]int{
	"Version":                 72,
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
	status   *tview.TextView
	cfg      models.Config
	settings models.Settings

	formRect   settingsRect
	statusRect settingsRect

	probeMu    sync.Mutex
	probeSeq   int
	probeTimer *time.Timer

	saveConfig func(models.Config) error
}

func newSettingsView(app *App, cfg models.Config) *settingsView {
	view := &settingsView{
		Box:      tview.NewBox(),
		app:      app,
		cfg:      cfg,
		settings: cfg.Settings,
		status:   tview.NewTextView(),
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
	setViewTitle(view.Box, "Settings")

	view.status.SetDynamicColors(true)
	view.status.SetWrap(true)
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
		AddTextView("Version", version.String(), settingsPreferredFieldWidths["Version"], 1, false, false).
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
	if v.formRect.width > 0 && v.formRect.height > 0 {
		v.resizeFormFields(v.formRect.width)
		v.form.SetRect(v.formRect.x, v.formRect.y, v.formRect.width, v.formRect.height)
		v.form.Draw(screen)
	}
	if v.statusRect.width > 0 && v.statusRect.height > 0 {
		v.status.SetRect(v.statusRect.x, v.statusRect.y, v.statusRect.width, v.statusRect.height)
		v.status.Draw(screen)
	}
}

func (v *settingsView) layout(x, y, width, height int) {
	if width >= settingsWideThreshold {
		formWidth := width - settingsStatusMinWidth - 2
		if formWidth > settingsFormMaxWidth {
			formWidth = settingsFormMaxWidth
		}
		if formWidth < 1 {
			formWidth = 1
		}
		statusWidth := width - formWidth - 2
		if statusWidth < 1 {
			statusWidth = 1
		}
		v.formRect = settingsRect{x: x, y: y, width: formWidth, height: height}
		v.statusRect = settingsRect{x: x + formWidth + 2, y: y, width: statusWidth, height: height}
		return
	}

	if height >= 18 {
		statusHeight := settingsClampInt(height/3, 5, 9)
		formHeight := height - statusHeight - 1
		v.formRect = settingsRect{x: x, y: y, width: width, height: formHeight}
		v.statusRect = settingsRect{x: x, y: y + formHeight + 1, width: width, height: statusHeight}
		return
	}
	if height >= 12 {
		statusHeight := 3
		formHeight := height - statusHeight - 1
		v.formRect = settingsRect{x: x, y: y, width: width, height: formHeight}
		v.statusRect = settingsRect{x: x, y: y + formHeight + 1, width: width, height: statusHeight}
		return
	}
	v.formRect = settingsRect{x: x, y: y, width: width, height: height}
	v.statusRect = settingsRect{}
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
	v.form.Focus(delegate)
}

func (v *settingsView) HasFocus() bool {
	return v.form.HasFocus() || v.Box.HasFocus()
}

func (v *settingsView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return v.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if handler := v.form.InputHandler(); handler != nil {
			handler(event, setFocus)
		}
	})
}

func (v *settingsView) PasteHandler() func(pastedText string, setFocus func(p tview.Primitive)) {
	return v.WrapPasteHandler(func(pastedText string, setFocus func(p tview.Primitive)) {
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

		switch action {
		case tview.MouseScrollUp:
			if v.formRect.contains(x, y) {
				v.moveFormFocus(-1, wrappedSetFocus)
				return true, nil
			}
		case tview.MouseScrollDown:
			if v.formRect.contains(x, y) {
				v.moveFormFocus(1, wrappedSetFocus)
				return true, nil
			}
		}

		if v.formRect.contains(x, y) {
			if handler := v.form.MouseHandler(); handler != nil {
				return handler(action, event, wrappedSetFocus)
			}
		}
		if action == tview.MouseLeftDown {
			v.form.Focus(wrappedSetFocus)
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
	cfg := v.app.apply(v.cfg)
	v.cfg = cfg
	v.settings = cfg.Settings
	v.app.cfg = cfg
	if err := v.saveConfig(cfg); err != nil {
		v.app.modal("Save failed", err.Error())
		return
	}
	v.startProbeNow("Saved")
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
		builder.WriteString("[-]\n\n")
	}
	if fallback != "" {
		builder.WriteString(tview.Escape(fallback))
		return builder.String()
	}
	if len(endpoints) == 0 {
		builder.WriteString("No endpoints configured.")
		return builder.String()
	}
	if checking {
		builder.WriteString("Checking endpoints...\n")
		for _, endpoint := range endpoints {
			builder.WriteString("  ")
			builder.WriteString(tview.Escape(endpoint.URL))
			builder.WriteByte('\n')
		}
		return builder.String()
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
		builder.WriteByte('\n')
		builder.WriteString("  ")
		builder.WriteString(tview.Escape(probe.Endpoint.URL))
		builder.WriteByte('\n')
	}
	return strings.TrimRight(builder.String(), "\n")
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
