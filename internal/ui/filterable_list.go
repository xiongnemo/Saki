package ui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type filterableListEntry struct {
	main      string
	secondary string
	shortcut  rune
	selected  func()
}

type filterableList struct {
	*tview.Flex

	app          *App
	input        *tview.InputField
	list         *tview.List
	entries      []filterableListEntry
	filtered     []int
	filtering    bool
	beforeFilter int
	extraCapture func(*tcell.EventKey) *tcell.EventKey
}

func (a *App) newFilterableList(target appFocusTarget) *filterableList {
	list := a.newList(target)
	input := tview.NewInputField().
		SetLabel("/ ").
		SetFieldWidth(0)
	input.SetFieldBackgroundColor(uiField)
	input.SetFieldTextColor(uiText)
	input.SetLabelColor(uiAccent)
	input.SetPlaceholderTextColor(uiMuted)

	view := &filterableList{
		Flex:  tview.NewFlex().SetDirection(tview.FlexRow),
		app:   a,
		input: input,
		list:  list,
	}
	view.Flex.AddItem(input, 0, 0, false)
	view.Flex.AddItem(list, 0, 1, true)

	input.SetChangedFunc(func(text string) {
		view.applyFilter(text)
	})
	input.SetDoneFunc(func(key tcell.Key) {
		switch key {
		case tcell.KeyEnter:
			view.confirmFilter()
		case tcell.KeyEscape:
			view.cancelFilter()
		}
	})
	list.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyRune && event.Rune() == '/' {
			view.startFilter()
			return nil
		}
		if view.extraCapture != nil {
			return view.extraCapture(event)
		}
		return event
	})
	return view
}

func (f *filterableList) AddItem(mainText, secondaryText string, shortcut rune, selected func()) *filterableList {
	f.entries = append(f.entries, filterableListEntry{
		main:      mainText,
		secondary: secondaryText,
		shortcut:  shortcut,
		selected:  selected,
	})
	f.render("")
	return f
}

func (f *filterableList) SetListInputCapture(capture func(*tcell.EventKey) *tcell.EventKey) *filterableList {
	f.extraCapture = capture
	return f
}

func (f *filterableList) SetCurrentItem(index int) *filterableList {
	setListCurrentItem(f.list, index)
	return f
}

func (f *filterableList) GetCurrentItem() int {
	return f.list.GetCurrentItem()
}

func (f *filterableList) startFilter() {
	f.filtering = true
	f.beforeFilter = f.originalCurrentItem()
	f.input.SetText("")
	f.ResizeItem(f.input, 1, 0)
	f.app.app.SetFocus(f.input)
	f.render("")
}

func (f *filterableList) applyFilter(query string) {
	if !f.filtering {
		return
	}
	f.render(query)
}

func (f *filterableList) confirmFilter() {
	if !f.filtering {
		return
	}
	original := f.originalCurrentItem()
	f.stopFilter()
	f.render("")
	setListCurrentItem(f.list, original)
	f.app.app.SetFocus(f.list)
}

func (f *filterableList) cancelFilter() {
	if !f.filtering {
		return
	}
	original := f.beforeFilter
	f.stopFilter()
	f.render("")
	setListCurrentItem(f.list, original)
	f.app.app.SetFocus(f.list)
}

func (f *filterableList) stopFilter() {
	f.filtering = false
	f.input.SetText("")
	f.ResizeItem(f.input, 0, 0)
}

func (f *filterableList) originalCurrentItem() int {
	current := f.list.GetCurrentItem()
	if current >= 0 && current < len(f.filtered) {
		return f.filtered[current]
	}
	return current
}

func (f *filterableList) render(query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	currentOriginal := f.originalCurrentItem()
	f.list.Clear()
	f.filtered = f.filtered[:0]
	for i, entry := range f.entries {
		if query != "" {
			haystack := strings.ToLower(entry.main + "\n" + entry.secondary)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		originalIndex := i
		entry := entry
		f.filtered = append(f.filtered, originalIndex)
		f.list.AddItem(entry.main, entry.secondary, entry.shortcut, entry.selected)
	}
	if len(f.filtered) == 0 {
		return
	}
	target := 0
	for filteredIndex, originalIndex := range f.filtered {
		if originalIndex == currentOriginal {
			target = filteredIndex
			break
		}
	}
	setListCurrentItem(f.list, target)
}
