package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/models"
)

const (
	nowPlayingWideMinWidth  = 72
	nowPlayingWideMinHeight = 14
	nowPlayingQueueRadius   = 2
)

type nowPlayingView struct {
	*tview.Box

	state      models.CurrentState
	cover      *coverPreview
	coverRect  settingsRect
	infoRect   settingsRect
	queueRect  settingsRect
	stacked    bool
	lastRows   []string
	lastStatus string
}

type queueContextRow struct {
	text  string
	color tcell.Color
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
	v.state = state
	if v.cover != nil {
		v.cover.SetState(state)
	}
}

func (v *nowPlayingView) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	v.coverRect = settingsRect{}
	v.infoRect = settingsRect{}
	v.queueRect = settingsRect{}
	v.lastRows = nil
	v.lastStatus = ""
	if width <= 0 || height <= 0 {
		return
	}
	if v.state.CurrentTrack == nil {
		v.drawEmpty(screen, x, y, width, height)
		return
	}
	if width >= nowPlayingWideMinWidth && height >= nowPlayingWideMinHeight {
		v.stacked = false
		v.drawWide(screen, x, y, width, height)
		return
	}
	v.stacked = true
	v.drawStacked(screen, x, y, width, height)
}

func (v *nowPlayingView) drawEmpty(screen tcell.Screen, x, y, width, height int) {
	rows := []string{
		"No track loaded",
		"Start playback from an album, playlist, or search result.",
		"Controls: Space Play/Pause | C-b Prev | C-n Next | C-q Quit",
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

func (v *nowPlayingView) drawWide(screen tcell.Screen, x, y, width, height int) {
	coverWidth := min(max(22, width/3), width/2-2)
	if coverWidth < 18 {
		v.drawStacked(screen, x, y, width, height)
		return
	}
	infoX := x + coverWidth + 2
	infoWidth := width - coverWidth - 2
	v.coverRect = settingsRect{x: x, y: y, width: coverWidth, height: height}
	v.infoRect = settingsRect{x: infoX, y: y, width: infoWidth, height: height}
	v.drawCover(screen, v.coverRect)
	v.drawInfo(screen, v.infoRect, true)
}

func (v *nowPlayingView) drawStacked(screen tcell.Screen, x, y, width, height int) {
	infoHeight := min(height, 9)
	v.infoRect = settingsRect{x: x, y: y, width: width, height: infoHeight}
	v.drawInfo(screen, v.infoRect, false)
	if height <= infoHeight+3 {
		return
	}

	remainingY := y + infoHeight + 1
	remainingHeight := height - infoHeight - 1
	coverHeight := min(max(5, remainingHeight/2), remainingHeight)
	if coverHeight >= 5 {
		v.coverRect = settingsRect{x: x, y: remainingY, width: width, height: coverHeight}
		v.drawCover(screen, v.coverRect)
		remainingY += coverHeight + 1
		remainingHeight = y + height - remainingY
	}
	if remainingHeight > 0 {
		v.queueRect = settingsRect{x: x, y: remainingY, width: width, height: remainingHeight}
		v.drawQueueContext(screen, v.queueRect)
	}
}

func (v *nowPlayingView) drawCover(screen tcell.Screen, rect settingsRect) {
	if v.cover == nil || rect.width <= 0 || rect.height <= 0 {
		return
	}
	v.cover.SetRect(rect.x, rect.y, rect.width, rect.height)
	v.cover.Draw(screen)
}

func (v *nowPlayingView) drawInfo(screen tcell.Screen, rect settingsRect, includeQueue bool) {
	if rect.width <= 0 || rect.height <= 0 || v.state.CurrentTrack == nil {
		return
	}
	track := v.state.CurrentTrack
	row := rect.y
	endY := rect.y + rect.height

	row = v.printLabelValue(screen, rect.x, row, rect.width, "Title", track.Title, uiText)
	row = v.printLabelValue(screen, rect.x, row, rect.width, "Artist", track.Artist, uiText)
	row = v.printLabelValue(screen, rect.x, row, rect.width, "Album", track.Album, uiText)
	if row < endY {
		row++
	}
	if row < endY {
		tview.Print(screen, playingProgressLine(v.state.Position, float64(track.Duration), rect.width), rect.x, row, rect.width, tview.AlignLeft, uiText)
		v.lastRows = append(v.lastRows, playingProgressLine(v.state.Position, float64(track.Duration), rect.width))
		row++
	}
	if row < endY {
		status := nowPlayingStatusLine(v.state, rect.width)
		v.lastStatus = status
		tview.Print(screen, status, rect.x, row, rect.width, tview.AlignLeft, uiAccent)
		row++
	}
	if row < endY {
		tview.Print(screen, nowPlayingControlsLine(rect.width), rect.x, row, rect.width, tview.AlignLeft, uiMuted)
		row++
	}
	if row < endY {
		tview.Print(screen, nowPlayingSeekLine(rect.width), rect.x, row, rect.width, tview.AlignLeft, uiMuted)
		row++
	}
	if v.state.LastError != "" && row < endY {
		tview.Print(screen, "Error: "+v.state.LastError, rect.x, row, rect.width, tview.AlignLeft, uiDanger)
		row++
	}
	if !includeQueue || row >= endY-1 {
		return
	}
	row++
	v.queueRect = settingsRect{x: rect.x, y: row, width: rect.width, height: endY - row}
	v.drawQueueContext(screen, v.queueRect)
}

func (v *nowPlayingView) printLabelValue(screen tcell.Screen, x, y, width int, label, value string, valueColor tcell.Color) int {
	if width <= 0 {
		return y + 1
	}
	if value = strings.TrimSpace(value); value == "" {
		value = "Unknown"
	}
	prefix := label + ": "
	drawSegmentsClipped(screen, x, y, []textSegment{
		{text: prefix, color: uiLabel},
		{text: value, color: valueColor},
	}, width)
	v.lastRows = append(v.lastRows, prefix+value)
	return y + 1
}

func (v *nowPlayingView) drawQueueContext(screen tcell.Screen, rect settingsRect) {
	if rect.width <= 0 || rect.height <= 0 {
		return
	}
	rows := queueContextRows(v.state, rect.height-1)
	tview.Print(screen, "Queue Context", rect.x, rect.y, rect.width, tview.AlignLeft, uiTitle)
	for i, row := range rows {
		y := rect.y + 1 + i
		if y >= rect.y+rect.height {
			return
		}
		tview.Print(screen, row.text, rect.x, y, rect.width, tview.AlignLeft, row.color)
		v.lastRows = append(v.lastRows, row.text)
	}
}

func nowPlayingStatusLine(state models.CurrentState, width int) string {
	stream := playingLeftText(state, width)
	if stream == "" {
		stream = "Stream Ready"
	}
	return fmt.Sprintf("%s  %s  Repeat %s  Shuffle %s  %s",
		stateString(state),
		stream,
		state.RepeatStatus,
		boolText(state.Shuffled),
		volumeStatusText(state.Volume),
	)
}

func nowPlayingControlsLine(width int) string {
	line := "Controls: Space Play/Pause | C-b Prev | C-n Next | C-t Repeat | C-h Shuffle"
	if width < len(line) {
		return "Controls: Space Play/Pause | C-b Prev | C-n Next"
	}
	return line
}

func nowPlayingSeekLine(width int) string {
	line := "Seek/Volume: C-Left/C-Right Seek 10s | C-i/k Volume | Esc Back"
	if width < len(line) {
		return "Seek: C-Left/C-Right | Volume: C-i/k | Esc Back"
	}
	return line
}

func queueContextRows(state models.CurrentState, maxRows int) []queueContextRow {
	if maxRows <= 0 {
		return nil
	}
	entries := state.CurrentPlaylist.Entries
	if len(entries) == 0 {
		return []queueContextRow{{text: "Queue is empty", color: uiMuted}}
	}
	index := state.CurrentTrackIndex
	if index < 0 || index >= len(entries) {
		index = 0
	}
	start := max(0, index-nowPlayingQueueRadius)
	end := min(len(entries), index+nowPlayingQueueRadius+1)
	for end-start > maxRows {
		if index-start > end-index-1 {
			start++
		} else {
			end--
		}
	}

	rows := make([]queueContextRow, 0, end-start)
	for i := start; i < end; i++ {
		song := entries[i]
		prefix := "  "
		color := uiText
		if i == index {
			prefix = "> "
			color = uiAccent
		}
		rows = append(rows, queueContextRow{
			text:  fmt.Sprintf("%s%02d. %s - %s [%s]", prefix, i+1, nowPlayingFallbackText(song.Artist), nowPlayingFallbackText(song.Title), models.SecondsAsMMSS(song.Duration)),
			color: color,
		})
	}
	return rows
}

func nowPlayingFallbackText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unknown"
	}
	return value
}
