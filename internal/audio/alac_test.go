package audio

import (
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsMP4Header(t *testing.T) {
	if !isMP4Header([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}) {
		t.Fatal("expected ftyp header to be recognized")
	}
	if isMP4Header([]byte("RIFFxxxxWAVE")) {
		t.Fatal("did not expect WAV header to be recognized as MP4")
	}
}

func TestConvertALACPCMToS16(t *testing.T) {
	t.Run("16-bit", func(t *testing.T) {
		src := []byte{0x00, 0x80, 0xff, 0x7f}
		dst := make([]byte, 4)
		convertALACPCMToS16(dst, src, 16, 2)
		if got := int16(binary.LittleEndian.Uint16(dst[0:2])); got != -32768 {
			t.Fatalf("left = %d, want -32768", got)
		}
		if got := int16(binary.LittleEndian.Uint16(dst[2:4])); got != 32767 {
			t.Fatalf("right = %d, want 32767", got)
		}
	})

	t.Run("24-bit", func(t *testing.T) {
		src := []byte{0x00, 0x00, 0x80, 0x00, 0xff, 0x7f}
		dst := make([]byte, 4)
		convertALACPCMToS16(dst, src, 24, 2)
		if got := int16(binary.LittleEndian.Uint16(dst[0:2])); got != -32768 {
			t.Fatalf("left = %d, want -32768", got)
		}
		if got := int16(binary.LittleEndian.Uint16(dst[2:4])); got != 32767 {
			t.Fatalf("right = %d, want 32767", got)
		}
	})

	t.Run("20-bit", func(t *testing.T) {
		src := []byte{0x00, 0x00, 0x80, 0xf0, 0xff, 0x7f}
		dst := make([]byte, 4)
		convertALACPCMToS16(dst, src, 20, 2)
		if got := int16(binary.LittleEndian.Uint16(dst[0:2])); got != -32768 {
			t.Fatalf("left = %d, want -32768", got)
		}
		if got := int16(binary.LittleEndian.Uint16(dst[2:4])); got != 32767 {
			t.Fatalf("right = %d, want 32767", got)
		}
	})

	t.Run("32-bit", func(t *testing.T) {
		src := []byte{0x00, 0x00, 0x00, 0x80, 0x00, 0x00, 0xff, 0x7f}
		dst := make([]byte, 4)
		convertALACPCMToS16(dst, src, 32, 2)
		if got := int16(binary.LittleEndian.Uint16(dst[0:2])); got != -32768 {
			t.Fatalf("left = %d, want -32768", got)
		}
		if got := int16(binary.LittleEndian.Uint16(dst[2:4])); got != 32767 {
			t.Fatalf("right = %d, want 32767", got)
		}
	})
}

func TestALACSourceWithFFmpegFixture(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found")
	}

	dir := t.TempDir()
	m4aPath := filepath.Join(dir, "tone.m4a")
	cmd := exec.Command(ffmpeg,
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-f", "lavfi",
		"-i", "sine=frequency=440:duration=0.1:sample_rate=44100",
		"-c:a", "alac",
		m4aPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg failed: %v\n%s", err, output)
	}

	file, err := os.Open(m4aPath)
	if err != nil {
		t.Fatal(err)
	}
	source, err := newALACSource(file, m4aPath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	if source.SampleRate() != 44100 {
		t.Fatalf("sample rate = %d, want 44100", source.SampleRate())
	}
	if source.Channels() == 0 {
		t.Fatal("expected at least one channel")
	}
	if source.Duration() <= 0 {
		t.Fatal("expected positive duration")
	}

	buf := make([]byte, int(source.Channels())*2*512)
	n, err := source.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected decoded PCM bytes")
	}
	if source.Position() <= 0 {
		t.Fatal("expected position to advance")
	}
	if err := source.SeekFrame(0); err != nil {
		t.Fatal(err)
	}
	if source.Position() != 0 {
		t.Fatalf("position after seek = %f, want 0", source.Position())
	}
}
