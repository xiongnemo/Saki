package ui

import "github.com/rivo/tview"

func newStatusLine() *tview.TextView {
	status := tview.NewTextView().SetDynamicColors(true)
	status.SetScrollable(false)
	status.SetWrap(false)
	status.SetWordWrap(false)
	status.SetTextColor(uiText)
	status.SetBackgroundColor(uiBackground)
	return status
}
