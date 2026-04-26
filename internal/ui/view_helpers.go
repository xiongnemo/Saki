package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type titledPrimitive interface {
	SetTitle(string) *tview.Box
	SetTitleAlign(int) *tview.Box
}

func viewTitle(parts ...string) string {
	clean := []string{"Saki"}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			clean = append(clean, part)
		}
	}
	return strings.Join(clean, " :: ")
}

func setViewTitle(view titledPrimitive, parts ...string) {
	view.SetTitle(" " + viewTitle(parts...) + " ")
	view.SetTitleAlign(tview.AlignLeft)
}

func setPlainTitle(view titledPrimitive, title string) {
	view.SetTitle(" " + strings.TrimSpace(title) + " ")
	view.SetTitleAlign(tview.AlignLeft)
}

func styleList(list *tview.List) {
	list.SetMainTextStyle(tcell.StyleDefault.Foreground(uiText).Background(uiBackground))
	list.SetSecondaryTextStyle(tcell.StyleDefault.Foreground(uiMuted).Background(uiBackground))
	list.SetShortcutStyle(tcell.StyleDefault.Foreground(uiLabel).Background(uiBackground))
	list.SetHighlightFullLine(true)
	applyListFocusStyle(list, false)
}

func applyListFocusStyle(list *tview.List, focused bool) {
	if focused {
		list.SetSelectedStyle(tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(uiAccent).Bold(true))
		return
	}
	list.SetSelectedStyle(tcell.StyleDefault.Foreground(uiTitle).Background(uiField))
}
