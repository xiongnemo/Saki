package ui

import (
	"image"
	"math"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const minQueuePanelHeight = 5

type queueSidePanel struct {
	*tview.Box

	queue *tview.List
	cover *coverPreview
}

func newQueueSidePanel(queue *tview.List, cover *coverPreview) *queueSidePanel {
	return &queueSidePanel{
		Box:   tview.NewBox(),
		queue: queue,
		cover: cover,
	}
}

func (p *queueSidePanel) Draw(screen tcell.Screen) {
	x, y, width, height := p.GetRect()
	if width <= 0 || height <= 0 {
		return
	}
	coverWidth, coverHeight := 0, 0
	if p.cover != nil {
		coverWidth, coverHeight = p.cover.PreferredSize(width, max(0, height-minQueuePanelHeight), screenCellRatio(screen))
	}
	queueHeight := height - coverHeight
	if queueHeight < 0 {
		queueHeight = 0
	}
	if p.queue != nil && queueHeight > 0 {
		p.queue.SetRect(x, y, width, queueHeight)
		p.queue.Draw(screen)
	}
	if p.cover != nil && coverWidth > 0 && coverHeight > 0 {
		coverX := x + (width-coverWidth)/2
		p.cover.SetRect(coverX, y+queueHeight, coverWidth, coverHeight)
		p.cover.Draw(screen)
	} else if p.cover != nil {
		p.cover.unlockInline(screen)
	}
}

func (p *queueSidePanel) InputHandler() func(*tcell.EventKey, func(tview.Primitive)) {
	return nil
}

func (p *queueSidePanel) Focus(delegate func(tview.Primitive)) {
	if p.queue != nil && delegate != nil {
		delegate(p.queue)
	}
}

func (p *queueSidePanel) HasFocus() bool {
	return p.queue != nil && p.queue.HasFocus()
}

func (p *queueSidePanel) Blur() {}

func (p *queueSidePanel) MouseHandler() func(tview.MouseAction, *tcell.EventMouse, func(tview.Primitive)) (bool, tview.Primitive) {
	return p.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(tview.Primitive)) (bool, tview.Primitive) {
		if event == nil || !p.InRect(event.Position()) {
			return false, nil
		}
		if p.queue != nil && p.queue.InRect(event.Position()) {
			if handler := p.queue.MouseHandler(); handler != nil {
				return handler(action, event, setFocus)
			}
		}
		return false, nil
	})
}

func (p *queueSidePanel) PasteHandler() func(string, func(tview.Primitive)) {
	return nil
}

func (v *coverPreview) PreferredSize(maxWidth, maxHeight int, cellRatio float64) (int, int) {
	if maxWidth <= 0 || maxHeight <= 0 || v == nil || v.renderer == coverRendererOff {
		return 0, 0
	}
	if v.state.CurrentTrack == nil {
		return maxWidth, min(maxHeight, 5)
	}
	img := v.currentImage()
	if img == nil {
		return maxWidth, min(maxHeight, 5)
	}
	return coverPanelSize(img.Bounds(), maxWidth, maxHeight, cellRatio)
}

func coverPanelSize(bounds image.Rectangle, maxWidth, maxHeight int, cellRatio float64) (int, int) {
	if maxWidth <= 0 || maxHeight <= 0 || bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return 0, 0
	}
	if cellRatio <= 0 {
		cellRatio = defaultCoverCellRatio
	}
	innerMaxWidth := maxWidth - 2
	innerMaxHeight := maxHeight - 2
	if innerMaxWidth <= 0 || innerMaxHeight <= 0 {
		return min(maxWidth, 2), min(maxHeight, 2)
	}

	imageAspect := float64(bounds.Dx()) / float64(bounds.Dy())
	innerWidth := innerMaxWidth
	innerHeight := int(math.Round(float64(innerWidth) * cellRatio / imageAspect))
	if innerHeight < 1 {
		innerHeight = 1
	}

	if innerHeight > innerMaxHeight {
		innerHeight = innerMaxHeight
		innerWidth = int(math.Round(float64(innerHeight) * imageAspect / cellRatio))
		if innerWidth < 1 {
			innerWidth = 1
		}
		if innerWidth > innerMaxWidth {
			innerWidth = innerMaxWidth
		}
	}

	return innerWidth + 2, innerHeight + 2
}

func screenCellRatio(screen tcell.Screen) float64 {
	tty, ok := screen.Tty()
	if !ok || tty == nil {
		return defaultCoverCellRatio
	}
	cellWidth, cellHeight := terminalCellDimensions(tty)
	if cellWidth <= 0 || cellHeight <= 0 {
		return defaultCoverCellRatio
	}
	return float64(cellWidth) / float64(cellHeight)
}
