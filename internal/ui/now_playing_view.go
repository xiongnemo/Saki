package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/models"
)

const (
	nowPlayingSideMinWidth  = 88
	nowPlayingSideMinHeight = 20
	nowPlayingTopMinWidth   = 54
	nowPlayingTopMinHeight  = 26
	nowPlayingButtonHeight  = 1
	nowPlayingButtonGap     = 2
	nowPlayingButtonRows    = 3
)

type nowPlayingLayout int

const (
	nowPlayingLayoutHint nowPlayingLayout = iota
	nowPlayingLayoutSideCover
	nowPlayingLayoutTopCover
)

type nowPlayingAction int

const (
	nowPlayingActionPrevious nowPlayingAction = iota
	nowPlayingActionPlayPause
	nowPlayingActionNext
	nowPlayingActionRepeat
	nowPlayingActionShuffle
)

type nowPlayingButton struct {
	label  string
	action nowPlayingAction
	rect   settingsRect
}

type nowPlayingView struct {
	*tview.Box

	state      models.CurrentState
	cover      *coverPreview
	coverRect  settingsRect
	infoRect   settingsRect
	statusRect settingsRect
	layout     nowPlayingLayout
	lastRows   []string
	lastStatus string
	buttons    []nowPlayingButton

	metadataOffset int
	metadataText   string
	metadataWidth  int
	trackKey       string

	onKey    func(*tcell.EventKey) *tcell.EventKey
	onAction func(nowPlayingAction)
}

func newNowPlayingView() *nowPlayingView {
	view := &nowPlayingView{
		Box:   tview.NewBox(),
		cover: newCoverPreviewWithRenderer(coverRendererCell),
	}
	view.SetBorder(true)
	setViewTitle(view, "Now Playing")
	return view
}

func (v *nowPlayingView) SetState(state models.CurrentState) {
	nextKey := coverTrackKey(state.CurrentTrack)
	if nextKey != v.trackKey {
		v.trackKey = nextKey
		v.metadataOffset = 0
	}
	v.state = state
	if v.cover != nil {
		v.cover.SetState(state)
	}
}

func (v *nowPlayingView) AdvanceMetadataScroll() bool {
	metadataLen := len([]rune(v.metadataText))
	if v.metadataWidth <= 0 || metadataLen <= v.metadataWidth {
		return false
	}
	v.metadataOffset = (v.metadataOffset + 1) % (metadataLen + 3)
	return true
}

func (v *nowPlayingView) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return v.WrapInputHandler(func(event *tcell.EventKey, _ func(tview.Primitive)) {
		if v.onKey != nil {
			v.onKey(event)
		}
	})
}

func (v *nowPlayingView) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return v.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, _ func(tview.Primitive)) (bool, tview.Primitive) {
		if event == nil {
			return true, nil
		}
		if action != tview.MouseLeftClick && action != tview.MouseLeftDoubleClick {
			return true, nil
		}
		x, y := event.Position()
		for _, button := range v.buttons {
			if button.rect.contains(x, y) {
				if v.onAction != nil {
					v.onAction(button.action)
				}
				return true, v
			}
		}
		return true, nil
	})
}

func (v *nowPlayingView) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	v.coverRect = settingsRect{}
	v.infoRect = settingsRect{}
	v.statusRect = settingsRect{}
	v.lastRows = nil
	v.lastStatus = ""
	v.buttons = nil
	v.metadataText = ""
	v.metadataWidth = 0
	if width <= 0 || height <= 0 {
		return
	}
	if v.state.CurrentTrack == nil {
		v.layout = nowPlayingLayoutHint
		v.drawEmpty(screen, x, y, width, height)
		return
	}

	v.layout = chooseNowPlayingLayout(width, height)
	switch v.layout {
	case nowPlayingLayoutSideCover:
		v.drawSideCover(screen, x, y, width, height)
	case nowPlayingLayoutTopCover:
		v.drawTopCover(screen, x, y, width, height)
	default:
		v.drawRatioHint(screen, x, y, width, height)
	}
}

func chooseNowPlayingLayout(width, height int) nowPlayingLayout {
	if width <= 0 || height <= 0 {
		return nowPlayingLayoutHint
	}
	ratio := float64(width) / float64(height)
	if width >= nowPlayingSideMinWidth && height >= nowPlayingSideMinHeight && ratio >= 2.8 {
		return nowPlayingLayoutSideCover
	}
	if width >= nowPlayingTopMinWidth && height >= nowPlayingTopMinHeight && ratio >= 1.1 && ratio <= 2.8 {
		return nowPlayingLayoutTopCover
	}
	return nowPlayingLayoutHint
}

func (v *nowPlayingView) drawEmpty(screen tcell.Screen, x, y, width, height int) {
	rows := []string{
		"No track loaded",
		"Start playback from an album, playlist, or search result.",
		"Press Esc to return.",
	}
	startY := y + max(0, (height-len(rows))/2)
	for i, row := range rows {
		if startY+i >= y+height {
			return
		}
		color := uiMuted
		if i == 0 {
			color = uiText
		}
		tview.Print(screen, row, x, startY+i, width, tview.AlignCenter, color)
	}
	v.lastRows = rows
}

func (v *nowPlayingView) drawRatioHint(screen tcell.Screen, x, y, width, height int) {
	rows := []string{
		"Now Playing needs a different terminal shape.",
		fmt.Sprintf("Try at least %dx%d for side-cover or %dx%d for top-cover layout.", nowPlayingSideMinWidth, nowPlayingSideMinHeight, nowPlayingTopMinWidth, nowPlayingTopMinHeight),
		"Resize the terminal or press Esc to return.",
	}
	startY := y + max(0, (height-len(rows))/2)
	for i, row := range rows {
		if startY+i >= y+height {
			return
		}
		color := uiMuted
		if i == 0 {
			color = uiTitle
		}
		tview.Print(screen, row, x, startY+i, width, tview.AlignCenter, color)
	}
	v.lastRows = rows
}

func (v *nowPlayingView) drawSideCover(screen tcell.Screen, x, y, width, height int) {
	coverWidth := min(width/2-4, max(28, height*2))
	coverHeight := height - 2
	coverY := y + 1
	v.coverRect = settingsRect{x: x + 2, y: coverY, width: coverWidth, height: coverHeight}
	v.drawCover(screen, v.coverRect)

	infoX := v.coverRect.x + v.coverRect.width + 4
	infoWidth := x + width - infoX - 2
	v.infoRect = settingsRect{x: infoX, y: y + 3, width: infoWidth, height: height - 7}
	v.drawTextBlock(screen, v.infoRect, tview.AlignLeft)

	status := nowPlayingStreamAudioLine(v.state, width)
	v.lastStatus = status
	v.statusRect = settingsRect{x: infoX, y: y + height - 2, width: infoWidth, height: 1}
	tview.Print(screen, status, v.statusRect.x, v.statusRect.y, v.statusRect.width, tview.AlignLeft, uiMuted)
}

func (v *nowPlayingView) drawTopCover(screen tcell.Screen, x, y, width, height int) {
	coverHeight := min(max(10, height/2), height-13)
	coverWidth := width - 8
	if coverWidth < 1 {
		coverWidth = width
	}
	coverX := x + (width-coverWidth)/2
	v.coverRect = settingsRect{x: coverX, y: y + 1, width: coverWidth, height: coverHeight}
	v.drawCover(screen, v.coverRect)

	infoY := v.coverRect.y + v.coverRect.height + 1
	infoWidth := min(width-4, coverWidth)
	infoX := x + (width-infoWidth)/2
	infoHeight := y + height - infoY - 2
	v.infoRect = settingsRect{x: infoX, y: infoY, width: infoWidth, height: infoHeight}
	v.drawTextBlock(screen, v.infoRect, tview.AlignCenter)

	status := nowPlayingStreamAudioLine(v.state, width)
	v.lastStatus = status
	v.statusRect = settingsRect{x: x, y: y + height - 1, width: width, height: 1}
	tview.Print(screen, status, v.statusRect.x, v.statusRect.y, v.statusRect.width, tview.AlignCenter, uiMuted)
}

func (v *nowPlayingView) drawCover(screen tcell.Screen, rect settingsRect) {
	if v.cover == nil || rect.width <= 0 || rect.height <= 0 {
		return
	}
	v.cover.SetRect(rect.x, rect.y, rect.width, rect.height)
	v.cover.Draw(screen)
}

func (v *nowPlayingView) drawTextBlock(screen tcell.Screen, rect settingsRect, align int) {
	if rect.width <= 0 || rect.height <= 0 || v.state.CurrentTrack == nil {
		return
	}
	track := v.state.CurrentTrack
	row := rect.y
	endY := rect.y + rect.height

	if row < endY {
		title := fallbackNowPlayingText(track.Title)
		printStyled(screen, title, rect.x, row, rect.width, align, tcell.StyleDefault.Foreground(uiTitle).Background(uiBackground).Bold(true))
		v.lastRows = append(v.lastRows, "Title: "+title)
		row += 2
	}
	if row < endY {
		metadata := metadataLine(track)
		v.metadataText = metadata
		v.metadataWidth = rect.width
		rendered := scrollingText(metadata, rect.width, v.metadataOffset)
		tview.Print(screen, rendered, rect.x, row, rect.width, align, uiText)
		v.lastRows = append(v.lastRows, "Metadata: "+rendered)
		row += 2
	}
	if row < endY {
		progress := playingProgressLine(v.state.Position, float64(track.Duration), rect.width)
		tview.Print(screen, progress, rect.x, row, rect.width, align, uiText)
		v.lastRows = append(v.lastRows, progress)
		row += 2
	}
	if row+nowPlayingButtonRows <= endY {
		v.drawButtons(screen, rect.x, row, rect.width, align)
		row += nowPlayingButtonRows + 1
	}
	if v.state.LastError != "" && row < endY {
		tview.Print(screen, "Error: "+v.state.LastError, rect.x, row, rect.width, align, uiDanger)
	}
}

func (v *nowPlayingView) drawButtons(screen tcell.Screen, x, y, width, _ int) {
	topRow, bottomRow := nowPlayingButtonRowsForWidth(v.state, width)
	v.drawButtonRow(screen, x, y, width, topRow)
	v.drawButtonRow(screen, x, y+2, width, bottomRow)
}

func (v *nowPlayingView) drawButtonRow(screen tcell.Screen, x, y, width int, buttons []nowPlayingButton) {
	totalWidth := buttonRowWidth(buttons)
	startX := x
	if totalWidth < width {
		startX = x + (width-totalWidth)/2
	}
	cursor := startX
	for i, button := range buttons {
		if i > 0 {
			cursor += nowPlayingButtonGap
		}
		buttonWidth := nowPlayingButtonWidth(button.label)
		button.rect = settingsRect{x: cursor, y: y, width: buttonWidth, height: nowPlayingButtonHeight}
		color := uiTitle
		if button.action == nowPlayingActionRepeat || button.action == nowPlayingActionShuffle {
			color = uiAccent
		}
		drawNowPlayingButton(screen, button.rect, button.label, color)
		v.buttons = append(v.buttons, button)
		v.lastRows = append(v.lastRows, button.label)
		cursor += buttonWidth
	}
}

func nowPlayingButtonRowsForWidth(state models.CurrentState, width int) ([]nowPlayingButton, []nowPlayingButton) {
	playLabel := "Play"
	if state.Playing {
		playLabel = "Pause"
	}
	topRow := []nowPlayingButton{
		{label: "Prev", action: nowPlayingActionPrevious},
		{label: playLabel, action: nowPlayingActionPlayPause},
		{label: "Next", action: nowPlayingActionNext},
	}
	bottomRow := []nowPlayingButton{
		{label: "Repeat: " + state.RepeatStatus.String(), action: nowPlayingActionRepeat},
		{label: "Shuffle: " + boolText(state.Shuffled), action: nowPlayingActionShuffle},
	}
	if buttonRowWidth(topRow) <= width && buttonRowWidth(bottomRow) <= width {
		return topRow, bottomRow
	}
	return topRow, []nowPlayingButton{
		{label: "Rep:" + state.RepeatStatus.String(), action: nowPlayingActionRepeat},
		{label: "Shuf:" + boolText(state.Shuffled), action: nowPlayingActionShuffle},
	}
}

func buttonRowWidth(buttons []nowPlayingButton) int {
	width := 0
	for i, button := range buttons {
		if i > 0 {
			width += nowPlayingButtonGap
		}
		width += nowPlayingButtonWidth(button.label)
	}
	return width
}

func nowPlayingButtonWidth(label string) int {
	return len([]rune(label)) + 2
}

func drawNowPlayingButton(screen tcell.Screen, rect settingsRect, label string, color tcell.Color) {
	if screen == nil || rect.width <= 0 || rect.height <= 0 {
		return
	}
	style := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(color)
	text := " " + label + " "
	drawPlainText(screen, rect.x, rect.y, text, style)
}

func metadataLine(track *models.Song) string {
	if track == nil {
		return "Unknown - Unknown"
	}
	return fallbackNowPlayingText(track.Artist) + " - " + fallbackNowPlayingText(track.Album)
}

func fallbackNowPlayingText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	return value
}

func scrollingText(text string, width, offset int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	padded := []rune(text + "   ")
	if len(padded) == 0 {
		return ""
	}
	offset = offset % len(padded)
	doubled := append(append([]rune{}, padded...), padded...)
	if offset+width > len(doubled) {
		doubled = append(doubled, padded...)
	}
	return string(doubled[offset : offset+width])
}

func nowPlayingStreamAudioLine(state models.CurrentState, width int) string {
	status := playingLeftText(state, width)
	if status == "" {
		return "Stream Ready"
	}
	return status
}

func printStyled(screen tcell.Screen, text string, x, y, width, align int, style tcell.Style) {
	if screen == nil || width <= 0 {
		return
	}
	runes := []rune(text)
	if len(runes) > width {
		runes = runes[:width]
	}
	start := x
	if align == tview.AlignCenter && len(runes) < width {
		start = x + (width-len(runes))/2
	}
	for i, r := range runes {
		if start+i >= x+width {
			return
		}
		screen.SetContent(start+i, y, r, nil, style)
	}
}
