package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/xiongnemo/saki/internal/models"
)

func TestSelectCoverRenderer(t *testing.T) {
	tests := []struct {
		name      string
		requested string
		env       map[string]string
		want      coverRenderer
	}{
		{name: "explicit off", requested: "off", want: coverRendererOff},
		{name: "explicit cell", requested: "cell", want: coverRendererCell},
		{name: "kitty", env: map[string]string{"KITTY_WINDOW_ID": "1"}, want: coverRendererKitty},
		{name: "iterm", env: map[string]string{"LC_TERMINAL": "iTerm2"}, want: coverRendererIterm},
		{name: "wezterm iterm", env: map[string]string{"TERM_PROGRAM": "wezterm"}, want: coverRendererIterm},
		{name: "windows terminal sixel", env: map[string]string{"WT_SESSION": "abc"}, want: coverRendererSixel},
		{name: "tmux fallback", env: map[string]string{"KITTY_WINDOW_ID": "1", "TMUX": "/tmp/tmux"}, want: coverRendererCell},
		{name: "unknown fallback", env: map[string]string{"TERM": "xterm-256color"}, want: coverRendererCell},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectCoverRenderer(tt.requested, tt.env); got != tt.want {
				t.Fatalf("selectCoverRenderer() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCoverImageForTrackDecodesLocalImageAndFallsBack(t *testing.T) {
	path := writeTestPNG(t)
	img, key := coverImageForTrack(&models.Song{ID: "song", Image: path})
	if img == nil {
		t.Fatal("expected decoded cover image")
	}
	if !strings.HasPrefix(key, "file:") {
		t.Fatalf("decoded image key = %q, want file key", key)
	}

	img, key = coverImageForTrack(&models.Song{ID: "missing", Artist: "A", Album: "B", Title: "T", Image: filepath.Join(t.TempDir(), "missing.jpg")})
	if img == nil {
		t.Fatal("expected placeholder cover image")
	}
	if !strings.HasPrefix(key, "placeholder:") {
		t.Fatalf("placeholder image key = %q", key)
	}

	img, _ = coverImageForTrack(&models.Song{ID: "url", Image: "https://music.example/cover.jpg"})
	if img == nil {
		t.Fatal("URL image should use placeholder instead of failing")
	}
}

func TestDrawCellCoverUsesColorBlocksAndSmallSizes(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(12, 8)

	drawCellCover(screen, testImage(), 0, 0, 12, 8, defaultCoverCellRatio)
	ch, _, style, _ := screen.GetContent(2, 1)
	if ch != '▀' {
		t.Fatalf("cell cover rune = %q, want upper half block", ch)
	}
	fg, bg, _ := style.Decompose()
	if fg == tcell.ColorDefault || bg == tcell.ColorDefault {
		t.Fatalf("cell cover style = %v, want truecolor foreground/background", style)
	}

	drawCellCover(screen, testImage(), 0, 0, 1, 1, defaultCoverCellRatio)
}

func TestCoverPanelSizeUsesAspectRatioAndConstraints(t *testing.T) {
	square := image.Rect(0, 0, 100, 100)
	width, height := coverPanelSize(square, 36, 30, defaultCoverCellRatio)
	if width != 36 || height != 19 {
		t.Fatalf("width-driven cover size = %dx%d, want 36x19", width, height)
	}

	width, height = coverPanelSize(square, 36, 15, defaultCoverCellRatio)
	if width != 28 || height != 15 {
		t.Fatalf("height-constrained cover size = %dx%d, want 28x15", width, height)
	}

	wide := image.Rect(0, 0, 200, 100)
	width, height = coverPanelSize(wide, 42, 30, defaultCoverCellRatio)
	if width != 42 || height != 12 {
		t.Fatalf("wide cover size = %dx%d, want 42x12", width, height)
	}
}

func TestCoverPreviewDrawsPlaceholderForMissingCover(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(16, 8)

	view := newCoverPreviewWithRenderer(coverRendererCell)
	view.SetRect(0, 0, 16, 8)
	view.SetState(models.CurrentState{CurrentTrack: &models.Song{ID: "song", Artist: "Artist", Album: "Album", Title: "Title"}})
	view.Draw(screen)

	foundBlock := false
	for y := 1; y < 7; y++ {
		for x := 1; x < 15; x++ {
			ch, _, _, _ := screen.GetContent(x, y)
			if ch == '▀' {
				foundBlock = true
			}
		}
	}
	if !foundBlock {
		t.Fatal("expected placeholder cover blocks to be drawn")
	}
}

func TestCoverPreviewActiveRendererLabelReportsFallback(t *testing.T) {
	screen := tcell.NewSimulationScreen("")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(16, 8)

	view := newCoverPreviewWithRenderer(coverRendererSixel)
	if got := view.ActiveRendererLabel(); got != "Sixel" {
		t.Fatalf("pending renderer label = %q, want Sixel", got)
	}
	view.SetRect(0, 0, 16, 8)
	view.SetState(models.CurrentState{CurrentTrack: &models.Song{ID: "song", Artist: "Artist", Album: "Album", Title: "Title"}})
	view.Draw(screen)
	if got := view.ActiveRendererLabel(); got != "Cell" {
		t.Fatalf("fallback renderer label = %q, want Cell", got)
	}

	view = newCoverPreviewWithRenderer(coverRendererOff)
	view.SetRect(0, 0, 16, 8)
	view.SetState(models.CurrentState{CurrentTrack: &models.Song{ID: "song"}})
	view.Draw(screen)
	if got := view.ActiveRendererLabel(); got != "Off" {
		t.Fatalf("off renderer label = %q, want Off", got)
	}
}

func TestWriteInlineCoverProtocols(t *testing.T) {
	tests := []struct {
		name     string
		renderer coverRenderer
		want     string
		opts     inlineCoverOptions
	}{
		{name: "kitty", renderer: coverRendererKitty, want: "\x1b_G"},
		{name: "iterm", renderer: coverRendererIterm, want: "\x1b]1337;File="},
		{name: "sixel", renderer: coverRendererSixel, want: "\x1bP0;1q", opts: inlineCoverOptions{CellWidth: 8, CellHeight: 16}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeInlineCover(&buf, tt.renderer, testImage(), 1, 2, 4, 3, tt.opts); err != nil {
				t.Fatal(err)
			}
			got := buf.String()
			if !strings.Contains(got, "\x1b[3;2H") {
				t.Fatalf("inline output missing cursor move: %#v", got[:min(len(got), 32)])
			}
			if !strings.Contains(got, tt.want) {
				t.Fatalf("inline output missing protocol marker %q", tt.want)
			}
		})
	}
}

func TestDrawInlineLocksRegionAndCachesWrite(t *testing.T) {
	tty := &fakeCoverTty{ws: tcell.WindowSize{Width: 80, Height: 24, PixelWidth: 640, PixelHeight: 384}}
	screen := &fakeCoverTerminal{tty: tty}
	view := newCoverPreviewWithRenderer(coverRendererKitty)

	if !view.drawInline(screen, testImage(), "image-key", 0, 0, 20, 10) {
		t.Fatal("expected inline cover draw to succeed")
	}
	if len(screen.locks) == 0 || !screen.locks[len(screen.locks)-1].lock {
		t.Fatal("expected inline draw to lock its region")
	}
	if !strings.Contains(tty.buf.String(), "\x1b_G") {
		t.Fatal("expected kitty image output")
	}
	if got := view.ActiveRendererLabel(); got != "Kitty" {
		t.Fatalf("active inline renderer = %q, want Kitty", got)
	}

	firstLen := tty.buf.Len()
	if !view.drawInline(screen, testImage(), "image-key", 0, 0, 20, 10) {
		t.Fatal("expected cached inline cover draw to succeed")
	}
	if tty.buf.Len() != firstLen {
		t.Fatal("cached inline draw should not rewrite image bytes")
	}
}

func writeTestPNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cover.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, testImage()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(20 + x*20), G: uint8(30 + y*20), B: uint8(200 - x*10), A: 255})
		}
	}
	return img
}

type fakeCoverTerminal struct {
	tty   *fakeCoverTty
	locks []coverLockCall
}

type coverLockCall struct {
	x      int
	y      int
	width  int
	height int
	lock   bool
}

func (f *fakeCoverTerminal) LockRegion(x, y, width, height int, lock bool) {
	f.locks = append(f.locks, coverLockCall{x: x, y: y, width: width, height: height, lock: lock})
}

func (f *fakeCoverTerminal) Tty() (tcell.Tty, bool) {
	return f.tty, f.tty != nil
}

type fakeCoverTty struct {
	buf bytes.Buffer
	ws  tcell.WindowSize
}

func (f *fakeCoverTty) Start() error                          { return nil }
func (f *fakeCoverTty) Stop() error                           { return nil }
func (f *fakeCoverTty) Drain() error                          { return nil }
func (f *fakeCoverTty) NotifyResize(func())                   {}
func (f *fakeCoverTty) WindowSize() (tcell.WindowSize, error) { return f.ws, nil }
func (f *fakeCoverTty) Read([]byte) (int, error)              { return 0, io.EOF }
func (f *fakeCoverTty) Write(p []byte) (int, error)           { return f.buf.Write(p) }
func (f *fakeCoverTty) Close() error                          { return nil }
