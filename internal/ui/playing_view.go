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
		fmt.Sprintf("%s  Vol %d%%  Repeat %s  Shuffle %s%s", playingLeftText(state, width), int(state.Volume*100), state.RepeatStatus, boolText(state.Shuffled), errorText),
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
	rightWidth := volumeStatusWidth(state.Volume)
	leftLimit := width
	if rightWidth+2 < width {
		leftLimit = width - rightWidth - 2
	}
	leftSegments := playingLeftSegments(state, leftLimit)
	leftWidth := segmentsWidth(leftSegments)
	drawSegmentsClipped(screen, x, y, leftSegments, leftLimit)

	centerSegments := []textSegment{
		{text: "Repeat ", color: uiMuted},
		{text: state.RepeatStatus.String(), color: uiAccent},
		{text: "  Shuffle ", color: uiMuted},
		{text: statusBoolText(state.Shuffled), color: uiAccent},
	}
	centerWidth := segmentsWidth(centerSegments)
	centerX := x + (width-centerWidth)/2
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

func playingLeftSegments(state models.CurrentState, maxWidth int) []textSegment {
	leftState := "Ready"
	stateColor := uiAccent
	if state.Buffering {
		leftState = "Buffering"
		stateColor = uiDanger
	}
	leftSegments := []textSegment{
		{text: "Stream ", color: uiMuted},
		{text: leftState, color: stateColor},
	}
	switch {
	case state.CacheReady:
		leftSegments = append(leftSegments,
			textSegment{text: "  Cached", color: uiAccent},
		)
	case state.BufferPercentKnown:
		leftSegments = append(leftSegments,
			textSegment{text: "  Download ", color: uiText},
			textSegment{text: fmt.Sprintf("%.0f%%", state.BufferedPercent), color: uiText},
		)
	}

	remaining := maxWidth - segmentsWidth(leftSegments) - 2
	if label := audioInfoLabel(state.AudioInfo, remaining); label != "" {
		leftSegments = append(leftSegments,
			textSegment{text: "  ", color: uiText},
			textSegment{text: label, color: uiAccent},
		)
	}
	return leftSegments
}

func playingLeftText(state models.CurrentState, width int) string {
	segments := playingLeftSegments(state, width)
	var builder strings.Builder
	for _, segment := range segments {
		builder.WriteString(segment.text)
	}
	return builder.String()
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

func drawSegmentsClipped(screen tcell.Screen, x, y int, segments []textSegment, maxWidth int) {
	if maxWidth <= 0 {
		return
	}
	used := 0
	for _, segment := range segments {
		if segment.text == "" {
			continue
		}
		remaining := maxWidth - used
		if remaining <= 0 {
			return
		}
		text := segment.text
		if len(text) > remaining {
			text = text[:remaining]
		}
		tview.Print(screen, text, x+used, y, len(text), tview.AlignLeft, segment.color)
		used += len(text)
	}
}

func audioInfoLabel(info models.AudioInfo, maxWidth int) string {
	if maxWidth <= 0 || info.Empty() {
		return ""
	}
	codec := models.NormalizeAudioCodec(info.Codec)
	rateDepth := audioRateDepthLabel(info)
	bitRate := audioBitRateLabel(info.BitRateKbps)
	candidates := make([]string, 0, 4)
	switch {
	case codec != "" && rateDepth != "" && bitRate != "":
		candidates = append(candidates, codec+" "+rateDepth+" "+bitRate)
	case codec != "" && rateDepth != "":
		candidates = append(candidates, codec+" "+rateDepth)
	case codec != "" && bitRate != "":
		candidates = append(candidates, codec+" "+bitRate)
	case rateDepth != "" && bitRate != "":
		candidates = append(candidates, rateDepth+" "+bitRate)
	}
	if codec != "" && rateDepth != "" {
		candidates = append(candidates, codec+" "+rateDepth)
	}
	if codec != "" && bitRate != "" {
		candidates = append(candidates, codec+" "+bitRate)
	}
	if codec != "" {
		candidates = append(candidates, codec)
	}
	if rateDepth != "" {
		candidates = append(candidates, rateDepth)
	}
	if bitRate != "" {
		candidates = append(candidates, bitRate)
	}
	for _, candidate := range candidates {
		if candidate != "" && len(candidate) <= maxWidth {
			return candidate
		}
	}
	return ""
}

func audioRateDepthLabel(info models.AudioInfo) string {
	rate := audioSampleRateLabel(info.SampleRate)
	switch {
	case info.BitDepth > 0 && rate != "":
		return fmt.Sprintf("%s/%d", rate, info.BitDepth)
	case info.BitDepth > 0:
		return fmt.Sprintf("%dbit", info.BitDepth)
	case rate != "":
		return rate + "kHz"
	default:
		return ""
	}
}

func audioSampleRateLabel(sampleRate int) string {
	if sampleRate <= 0 {
		return ""
	}
	if sampleRate%1000 == 0 {
		return fmt.Sprintf("%d", sampleRate/1000)
	}
	value := float64(sampleRate) / 1000
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", value), "0"), ".")
}

func audioBitRateLabel(bitRateKbps int) string {
	if bitRateKbps <= 0 {
		return ""
	}
	return fmt.Sprintf("%dk", bitRateKbps)
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
