package ui

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/models"
)

type playingView struct {
	*tview.Box

	state   models.CurrentState
	message string
}

func newPlayingView() *playingView {
	view := &playingView{Box: tview.NewBox()}
	view.SetBorder(true)
	setPlainTitle(view, "Playing")
	return view
}

func (v *playingView) SetState(state models.CurrentState) {
	v.state = state
	v.message = ""
	setPlainTitle(v, "Playing")
}

func (v *playingView) SetMessage(message string) {
	v.message = message
}

func (v *playingView) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}
	if v.message != "" {
		tview.Print(screen, v.message, x, y, width, tview.AlignLeft, uiDanger)
		return
	}
	if v.state.CurrentTrack == nil {
		tview.Print(screen, "No track loaded", x, y, width, tview.AlignLeft, uiText)
		return
	}
	track := v.state.CurrentTrack
	tview.Print(screen, fmt.Sprintf("%s :: %s :: %s", track.Artist, track.Album, track.Title), x, y, width, tview.AlignLeft, uiText)
	if height > 1 {
		tview.Print(screen, playingProgressLine(v.state.Position, float64(track.Duration), width), x, y+1, width, tview.AlignLeft, uiText)
	}
	if height > 2 {
		drawPlayingStatus(screen, x, y+2, width, v.state)
	}
}

func playingRows(state models.CurrentState, message string, width int) []string {
	rows := make([]string, 0, 4)
	if message != "" {
		rows = append(rows, message)
	}
	if state.CurrentTrack == nil {
		return append(rows, "No track loaded")
	}

	track := state.CurrentTrack
	duration := float64(track.Duration)
	errorText := ""
	if state.LastError != "" {
		errorText = "  [red]" + state.LastError + "[-]"
	}
	rows = append(rows,
		fmt.Sprintf("%s :: %s :: %s", track.Artist, track.Album, track.Title),
		playingProgressLine(state.Position, duration, width),
		fmt.Sprintf("%s  Vol %d%%  Repeat %s  Shuffle %s%s", bufferLabel(state), int(state.Volume*100), state.RepeatStatus, boolText(state.Shuffled), errorText),
	)
	return rows
}

func playingProgressLine(position, duration float64, width int) string {
	timeText := fmt.Sprintf("%s / %s", secondsLabel(position), durationLabel(duration))
	label := "Progress "
	fillWidth := width - len(label) - len(timeText) - 4
	if fillWidth < 1 {
		fillWidth = 1
	}
	return label + progressBar(position, duration, fillWidth) + "  " + timeText
}

func drawPlayingStatus(screen tcell.Screen, x, y, width int, state models.CurrentState) {
	if width <= 0 {
		return
	}
	leftPrefix := "Stream "
	leftState := "Ready"
	stateColor := uiAccent
	if state.Buffering {
		leftState = "Buffering"
		stateColor = uiDanger
	}
	download := "--%"
	if state.BufferPercentKnown {
		download = fmt.Sprintf("%.0f%%", state.BufferedPercent)
	}
	leftSuffix := "  Download " + download

	leftWidth := len(leftPrefix) + len(leftState) + len(leftSuffix)
	tview.Print(screen, leftPrefix, x, y, width, tview.AlignLeft, uiMuted)
	tview.Print(screen, leftState, x+len(leftPrefix), y, max(0, width-len(leftPrefix)), tview.AlignLeft, stateColor)
	tview.Print(screen, leftSuffix, x+len(leftPrefix)+len(leftState), y, max(0, width-len(leftPrefix)-len(leftState)), tview.AlignLeft, uiText)

	centerSegments := []textSegment{
		{text: "Repeat ", color: uiMuted},
		{text: state.RepeatStatus.String(), color: uiAccent},
		{text: "  Shuffle ", color: uiMuted},
		{text: statusBoolText(state.Shuffled), color: uiAccent},
	}
	centerWidth := segmentsWidth(centerSegments)
	centerX := x + (width-centerWidth)/2
	rightWidth := volumeStatusWidth(state.Volume)
	if centerX < x+leftWidth+2 {
		centerX = x + leftWidth + 2
	}
	if centerX+centerWidth <= x+width-rightWidth-2 {
		drawSegments(screen, centerX, y, centerSegments)
	}

	drawVolumeStatus(screen, x, y, width, state.Volume)
	if state.LastError != "" && width > leftWidth+2 {
		errorX := x + leftWidth + 2
		maxWidth := x + width - rightWidth - 2 - errorX
		if maxWidth > 0 {
			tview.Print(screen, state.LastError, errorX, y, maxWidth, tview.AlignLeft, uiDanger)
		}
	}
}

type textSegment struct {
	text  string
	color tcell.Color
}

func segmentsWidth(segments []textSegment) int {
	width := 0
	for _, segment := range segments {
		width += len(segment.text)
	}
	return width
}

func drawSegments(screen tcell.Screen, x, y int, segments []textSegment) {
	for _, segment := range segments {
		if segment.text == "" {
			continue
		}
		tview.Print(screen, segment.text, x, y, len(segment.text), tview.AlignLeft, segment.color)
		x += len(segment.text)
	}
}

func volumeStatusWidth(volume float64) int {
	return len(volumeStatusText(volume))
}

func volumeStatusText(volume float64) string {
	return fmt.Sprintf("Vol %s %3d%%", volumeBar(volume, 10), int(clamp01(volume)*100))
}

func drawVolumeStatus(screen tcell.Screen, x, y, width int, volume float64) {
	text := volumeStatusText(volume)
	start := x + width - len(text)
	if start < x {
		start = x
	}
	tview.Print(screen, text, start, y, width-(start-x), tview.AlignLeft, uiText)

	barStart := start + len("Vol ")
	if barStart < x || barStart >= x+width {
		return
	}
	fraction := clamp01(volume)
	filled := int(fraction*10 + 0.5)
	styleBorder := tcell.StyleDefault.Foreground(uiTitle).Background(uiBackground)
	styleFilled := tcell.StyleDefault.Foreground(uiAccent).Background(uiBackground)
	styleEmpty := tcell.StyleDefault.Foreground(uiMuted).Background(uiBackground)
	screen.SetContent(barStart, y, '|', nil, styleBorder)
	for i := 0; i < 10 && barStart+1+i < x+width; i++ {
		style := styleEmpty
		if i < filled {
			style = styleFilled
		}
		ch := '-'
		if i < filled {
			ch = '#'
		}
		screen.SetContent(barStart+1+i, y, ch, nil, style)
	}
	if barStart+11 < x+width {
		screen.SetContent(barStart+11, y, '|', nil, styleBorder)
	}
}

func volumeBar(volume float64, width int) string {
	if width <= 0 {
		return "||"
	}
	filled := int(clamp01(volume)*float64(width) + 0.5)
	return "|" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "|"
}

func statusBoolText(v bool) string {
	return fmt.Sprintf("%-3s", boolText(v))
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
