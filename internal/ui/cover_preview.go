package ui

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strconv"
	"strings"

	"github.com/BourgeoisBear/rasterm"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/xiongnemo/saki/internal/models"
)

const (
	coverRendererEnv      = "SAKI_COVER_RENDERER"
	defaultCoverCellRatio = 0.5
)

type coverRenderer string

const (
	coverRendererAuto  coverRenderer = "auto"
	coverRendererCell  coverRenderer = "cell"
	coverRendererKitty coverRenderer = "kitty"
	coverRendererIterm coverRenderer = "iterm"
	coverRendererSixel coverRenderer = "sixel"
	coverRendererOff   coverRenderer = "off"
)

type coverPreview struct {
	*tview.Box

	renderer coverRenderer
	actual   coverRenderer
	state    models.CurrentState

	trackKey string
	imageKey string
	image    image.Image

	lastInline inlineCoverState
}

type inlineCoverState struct {
	active    bool
	signature string
	x         int
	y         int
	width     int
	height    int
}

type coverTerminal interface {
	LockRegion(x, y, width, height int, lock bool)
	Tty() (tcell.Tty, bool)
}

type inlineCoverOptions struct {
	CellWidth  int
	CellHeight int
}

func newCoverPreview() *coverPreview {
	return newCoverPreviewWithRenderer(configuredCoverRenderer())
}

func newCoverPreviewWithRenderer(renderer coverRenderer) *coverPreview {
	view := &coverPreview{
		Box:      tview.NewBox(),
		renderer: renderer,
	}
	view.SetBorder(true)
	setPlainTitle(view, "Cover")
	return view
}

func configuredCoverRenderer() coverRenderer {
	return selectCoverRenderer(os.Getenv(coverRendererEnv), coverEnv())
}

func coverEnv() map[string]string {
	keys := []string{"KITTY_WINDOW_ID", "LC_TERMINAL", "TERM", "TERM_PROGRAM", "TMUX", "WT_SESSION"}
	env := make(map[string]string, len(keys))
	for _, key := range keys {
		env[key] = os.Getenv(key)
	}
	return env
}

func selectCoverRenderer(requested string, env map[string]string) coverRenderer {
	switch coverRenderer(strings.ToLower(strings.TrimSpace(requested))) {
	case coverRendererOff:
		return coverRendererOff
	case coverRendererCell:
		return coverRendererCell
	case coverRendererKitty:
		return coverRendererKitty
	case coverRendererIterm:
		return coverRendererIterm
	case coverRendererSixel:
		return coverRendererSixel
	}

	if isTmuxEnv(env) {
		return coverRendererCell
	}
	if isKittyEnv(env) {
		return coverRendererKitty
	}
	if isItermEnv(env) {
		return coverRendererIterm
	}
	if isSixelEnv(env) {
		return coverRendererSixel
	}
	return coverRendererCell
}

func isTmuxEnv(env map[string]string) bool {
	return envValue(env, "TMUX") != ""
}

func isKittyEnv(env map[string]string) bool {
	term := envValue(env, "TERM")
	program := envValue(env, "TERM_PROGRAM")
	return envValue(env, "KITTY_WINDOW_ID") != "" ||
		term == "xterm-kitty" ||
		term == "xterm-ghostty" ||
		program == "kitty" ||
		program == "ghostty"
}

func isItermEnv(env map[string]string) bool {
	term := envValue(env, "TERM")
	program := envValue(env, "TERM_PROGRAM")
	lcTerminal := envValue(env, "LC_TERMINAL")
	return lcTerminal == "iterm2" ||
		strings.Contains(program, "iterm") ||
		program == "wezterm" ||
		program == "rio" ||
		term == "mintty"
}

func isSixelEnv(env map[string]string) bool {
	term := envValue(env, "TERM")
	program := envValue(env, "TERM_PROGRAM")
	return envValue(env, "WT_SESSION") != "" ||
		strings.Contains(term, "sixel") ||
		strings.Contains(term, "mlterm") ||
		program == "rio"
}

func envValue(env map[string]string, key string) string {
	if env == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(env[key]))
}

func (v *coverPreview) SetState(state models.CurrentState) {
	nextKey := coverTrackKey(state.CurrentTrack)
	if nextKey != v.trackKey {
		v.trackKey = nextKey
		v.imageKey = ""
		v.image = nil
	}
	v.state = state
}

func (v *coverPreview) Draw(screen tcell.Screen) {
	v.Box.DrawForSubclass(screen, v)
	x, y, width, height := v.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}
	if v.renderer == coverRendererOff {
		v.actual = coverRendererOff
		v.unlockInline(screen)
		return
	}
	if v.state.CurrentTrack == nil {
		v.actual = v.renderer
		v.unlockInline(screen)
		tview.Print(screen, "No cover", x, y+height/2, width, tview.AlignCenter, uiMuted)
		return
	}

	img := v.currentImage()
	if img == nil {
		v.actual = v.renderer
		v.unlockInline(screen)
		tview.Print(screen, "No cover", x, y+height/2, width, tview.AlignCenter, uiMuted)
		return
	}

	if v.renderer != coverRendererCell && v.drawInline(screen, img, v.imageKey, x, y, width, height) {
		v.actual = v.renderer
		return
	}

	v.unlockInline(screen)
	v.actual = coverRendererCell
	drawCellCover(screen, img, x, y, width, height, defaultCoverCellRatio)
}

func supportedCoverRenderersLabel() string {
	return "Kitty, iTerm2, Sixel, Cell"
}

func (v *coverPreview) ActiveRendererLabel() string {
	if v == nil {
		return coverRendererLabel(configuredCoverRenderer())
	}
	if v.actual != "" {
		return coverRendererLabel(v.actual)
	}
	return coverRendererLabel(v.renderer)
}

func coverRendererLabel(renderer coverRenderer) string {
	switch renderer {
	case coverRendererKitty:
		return "Kitty"
	case coverRendererIterm:
		return "iTerm2"
	case coverRendererSixel:
		return "Sixel"
	case coverRendererOff:
		return "Off"
	default:
		return "Cell"
	}
}

func (v *coverPreview) currentImage() image.Image {
	if v.image != nil {
		return v.image
	}
	img, key := coverImageForTrack(v.state.CurrentTrack)
	v.image = img
	if key != "" {
		v.imageKey = key
	}
	return img
}

func (v *coverPreview) drawInline(screen coverTerminal, img image.Image, imageKey string, x, y, width, height int) bool {
	tty, ok := screen.Tty()
	if !ok || tty == nil {
		return false
	}
	cellWidth, cellHeight := terminalCellDimensions(tty)
	if v.renderer == coverRendererSixel && (cellWidth <= 0 || cellHeight <= 0) {
		return false
	}
	cellRatio := defaultCoverCellRatio
	if cellWidth > 0 && cellHeight > 0 {
		cellRatio = float64(cellWidth) / float64(cellHeight)
	}
	drawX, drawY, drawWidth, drawHeight := fillCoverCells(x, y, width, height, cellRatio)
	if drawWidth <= 0 || drawHeight <= 0 {
		return false
	}

	signature := fmt.Sprintf("%s|%s|%d,%d,%d,%d", v.renderer, imageKey, drawX, drawY, drawWidth, drawHeight)
	if v.lastInline.active && v.lastInline.signature == signature {
		screen.LockRegion(drawX, drawY, drawWidth, drawHeight, true)
		v.actual = v.renderer
		return true
	}

	v.clearInline(tty)
	if v.lastInline.active {
		screen.LockRegion(v.lastInline.x, v.lastInline.y, v.lastInline.width, v.lastInline.height, false)
	}
	screen.LockRegion(drawX, drawY, drawWidth, drawHeight, true)
	err := writeInlineCover(tty, v.renderer, img, drawX, drawY, drawWidth, drawHeight, inlineCoverOptions{
		CellWidth:  cellWidth,
		CellHeight: cellHeight,
	})
	if err != nil {
		screen.LockRegion(drawX, drawY, drawWidth, drawHeight, false)
		v.lastInline = inlineCoverState{}
		v.actual = coverRendererCell
		return false
	}
	v.lastInline = inlineCoverState{
		active:    true,
		signature: signature,
		x:         drawX,
		y:         drawY,
		width:     drawWidth,
		height:    drawHeight,
	}
	v.actual = v.renderer
	return true
}

func (v *coverPreview) unlockInline(screen coverTerminal) {
	if !v.lastInline.active {
		return
	}
	if tty, ok := screen.Tty(); ok && tty != nil {
		v.clearInline(tty)
	}
	screen.LockRegion(v.lastInline.x, v.lastInline.y, v.lastInline.width, v.lastInline.height, false)
	v.lastInline = inlineCoverState{}
}

func (v *coverPreview) clearInline(tty tcell.Tty) {
	if !v.lastInline.active || tty == nil {
		return
	}
	for row := 0; row < v.lastInline.height; row++ {
		writeCursorMove(tty, v.lastInline.x, v.lastInline.y+row)
		_, _ = fmt.Fprint(tty, strings.Repeat(" ", v.lastInline.width))
	}
}

func terminalCellDimensions(tty tcell.Tty) (int, int) {
	if tty == nil {
		return 0, 0
	}
	ws, err := tty.WindowSize()
	if err != nil {
		return 0, 0
	}
	return ws.CellDimensions()
}

func writeInlineCover(out interface {
	Write([]byte) (int, error)
}, renderer coverRenderer, img image.Image, x, y, width, height int, opts inlineCoverOptions) error {
	if out == nil || img == nil || width <= 0 || height <= 0 {
		return fmt.Errorf("invalid inline cover request")
	}
	writeCursorMove(out, x, y)
	switch renderer {
	case coverRendererKitty:
		return rasterm.KittyWriteImage(out, img, rasterm.KittyImgOpts{
			DstCols: uint32(width),
			DstRows: uint32(height),
			ImageId: coverImageID(img.Bounds(), width, height),
		})
	case coverRendererIterm:
		return rasterm.ItermWriteImageWithOptions(out, img, rasterm.ItermImgOpts{
			Width:             strconv.Itoa(width),
			Height:            strconv.Itoa(height),
			DisplayInline:     true,
			IgnoreAspectRatio: true,
		})
	case coverRendererSixel:
		if opts.CellWidth <= 0 || opts.CellHeight <= 0 {
			return fmt.Errorf("sixel renderer needs terminal cell dimensions")
		}
		resized := resizeImageNearest(img, width*opts.CellWidth, height*opts.CellHeight)
		paletted := image.NewPaletted(resized.Bounds(), palette.Plan9)
		draw.FloydSteinberg.Draw(paletted, paletted.Bounds(), resized, resized.Bounds().Min)
		return rasterm.SixelWriteImage(out, paletted)
	default:
		return fmt.Errorf("renderer %q is not an inline renderer", renderer)
	}
}

func writeCursorMove(out interface {
	Write([]byte) (int, error)
}, x, y int) {
	_, _ = fmt.Fprintf(out, "\x1b[%d;%dH", y+1, x+1)
}

func coverImageID(bounds image.Rectangle, width, height int) uint32 {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%d:%d:%d:%d", bounds.Dx(), bounds.Dy(), width, height)
	id := h.Sum32()
	if id == 0 {
		id = 1
	}
	return id
}

func coverTrackKey(track *models.Song) string {
	if track == nil {
		return ""
	}
	return strings.Join([]string{track.ID, track.Artist, track.Album, track.Title, track.Image, track.CoverArt}, "\x00")
}

func coverImageForTrack(track *models.Song) (image.Image, string) {
	if track == nil {
		return nil, ""
	}
	if path := localCoverPath(track.Image); path != "" {
		if img, err := decodeImageFile(path); err == nil {
			return img, "file:" + path
		}
	}
	key := "placeholder:" + coverTrackKey(track)
	return placeholderCover(key, 128), key
}

func localCoverPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	lower := strings.ToLower(path)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return ""
	}
	return path
}

func decodeImageFile(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	return img, err
}

func placeholderCover(seed string, size int) image.Image {
	if size < 8 {
		size = 8
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	sum := h.Sum32()
	c1 := hashColor(sum)
	c2 := hashColor(sum>>8 | sum<<24)
	c3 := hashColor(sum>>16 | sum<<16)
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	cx := int(sum%uint32(size/2)) + size/4
	cy := int((sum>>7)%uint32(size/2)) + size/4
	radius := size/5 + int((sum>>13)%uint32(size/4))
	stripe := 7 + int(sum%9)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			t := float64(x+y) / float64(size*2)
			c := blendRGBA(c1, c2, t)
			if ((x-y)+size*2)%stripe < stripe/2 {
				c = blendRGBA(c, c3, 0.35)
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy < radius*radius {
				c = blendRGBA(c, c3, 0.55)
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func hashColor(sum uint32) color.RGBA {
	r := uint8(70 + sum%170)
	g := uint8(70 + (sum>>8)%170)
	b := uint8(70 + (sum>>16)%170)
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func blendRGBA(a, b color.RGBA, t float64) color.RGBA {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return color.RGBA{
		R: uint8(float64(a.R)*(1-t) + float64(b.R)*t),
		G: uint8(float64(a.G)*(1-t) + float64(b.G)*t),
		B: uint8(float64(a.B)*(1-t) + float64(b.B)*t),
		A: 255,
	}
}

func drawCellCover(screen tcell.Screen, img image.Image, x, y, width, height int, cellRatio float64) {
	if screen == nil || img == nil || width <= 0 || height <= 0 {
		return
	}
	drawX, drawY, drawWidth, drawHeight := fillCoverCells(x, y, width, height, cellRatio)
	if drawWidth <= 0 || drawHeight <= 0 {
		return
	}
	pixelWidth := drawWidth
	pixelHeight := drawHeight * 2
	for row := 0; row < drawHeight; row++ {
		for col := 0; col < drawWidth; col++ {
			top := sampleImage(img, col, row*2, pixelWidth, pixelHeight)
			bottom := sampleImage(img, col, row*2+1, pixelWidth, pixelHeight)
			style := tcell.StyleDefault.Foreground(tcellColor(top)).Background(tcellColor(bottom))
			screen.SetContent(drawX+col, drawY+row, '▀', nil, style)
		}
	}
}

func fillCoverCells(x, y, width, height int, cellRatio float64) (int, int, int, int) {
	if width <= 0 || height <= 0 {
		return 0, 0, 0, 0
	}
	if cellRatio <= 0 {
		cellRatio = defaultCoverCellRatio
	}
	return x, y, width, height
}

func sampleImage(img image.Image, x, y, targetWidth, targetHeight int) color.RGBA {
	bounds := img.Bounds()
	if targetWidth <= 0 || targetHeight <= 0 || bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return color.RGBA{A: 255}
	}
	srcX := bounds.Min.X + int(float64(x)*float64(bounds.Dx())/float64(targetWidth))
	srcY := bounds.Min.Y + int(float64(y)*float64(bounds.Dy())/float64(targetHeight))
	if srcX >= bounds.Max.X {
		srcX = bounds.Max.X - 1
	}
	if srcY >= bounds.Max.Y {
		srcY = bounds.Max.Y - 1
	}
	r, g, b, a := img.At(srcX, srcY).RGBA()
	if a == 0 {
		return color.RGBA{A: 255}
	}
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
}

func tcellColor(c color.RGBA) tcell.Color {
	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}

func resizeImageNearest(src image.Image, width, height int) image.Image {
	if src == nil || width <= 0 || height <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := src.Bounds()
	for y := 0; y < height; y++ {
		srcY := bounds.Min.Y + int(float64(y)*float64(bounds.Dy())/float64(height))
		if srcY >= bounds.Max.Y {
			srcY = bounds.Max.Y - 1
		}
		for x := 0; x < width; x++ {
			srcX := bounds.Min.X + int(float64(x)*float64(bounds.Dx())/float64(width))
			if srcX >= bounds.Max.X {
				srcX = bounds.Max.X - 1
			}
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}
