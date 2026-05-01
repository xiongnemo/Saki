package audio

import (
	"errors"
	"io"
	"testing"
)

func TestReadPCMFullFillsShortReads(t *testing.T) {
	chunks := [][]byte{
		[]byte("ab"),
		[]byte("cd"),
		[]byte("ef"),
	}
	calls := 0
	dst := make([]byte, 6)
	n, err := readPCMFull(dst, func(p []byte) (int, error) {
		if calls >= len(chunks) {
			return 0, io.EOF
		}
		n := copy(p, chunks[calls])
		calls++
		return n, nil
	})
	if err != nil {
		t.Fatalf("readPCMFull error = %v", err)
	}
	if n != len(dst) || string(dst) != "abcdef" {
		t.Fatalf("readPCMFull = %d %q, want full buffer", n, dst)
	}
	if calls != 3 {
		t.Fatalf("read calls = %d, want 3", calls)
	}
}

func TestReadPCMFullReturnsPartialBeforeEOF(t *testing.T) {
	calls := 0
	dst := make([]byte, 6)
	n, err := readPCMFull(dst, func(p []byte) (int, error) {
		calls++
		if calls == 1 {
			return copy(p, "abc"), io.EOF
		}
		return 0, errors.New("unexpected extra read")
	})
	if err != nil {
		t.Fatalf("readPCMFull error = %v", err)
	}
	if n != 3 || string(dst[:n]) != "abc" {
		t.Fatalf("readPCMFull partial = %d %q, want abc", n, dst[:n])
	}
}

func TestMiniAudioDeviceLifecycleDoesNotHoldBackendLock(t *testing.T) {
	backend := NewMiniAudioBackend()
	source := &fakePCMSource{
		channels:   2,
		sampleRate: 44100,
		duration:   1,
		length:     44100,
	}
	device := &fakeMiniAudioDevice{}
	device.start = func() error {
		assertMiniAudioLockAvailable(t, backend)
		backend.onSamples(make([]byte, 8), nil, 2)
		return nil
	}
	device.stop = func() error {
		assertMiniAudioLockAvailable(t, backend)
		return nil
	}
	device.uninit = func() {
		assertMiniAudioLockAvailable(t, backend)
	}

	backend.mu.Lock()
	backend.device = device
	backend.source = source
	backend.loaded = true
	backend.paused = true
	backend.mu.Unlock()

	if err := backend.Play(); err != nil {
		t.Fatalf("Play error = %v", err)
	}
	if device.starts != 1 {
		t.Fatalf("device starts = %d, want 1", device.starts)
	}
	if source.reads == 0 {
		t.Fatal("Start should allow the sample callback to read source data")
	}
	if !backend.IsPlaying() {
		t.Fatal("backend should be playing after successful Start")
	}

	if err := backend.Pause(); err != nil {
		t.Fatalf("Pause error = %v", err)
	}
	if err := backend.Stop(); err != nil {
		t.Fatalf("Stop error = %v", err)
	}
	if device.stops != 2 {
		t.Fatalf("device stops = %d, want pause and stop calls", device.stops)
	}
	if source.seeks == 0 {
		t.Fatal("Stop should seek the source back to the beginning")
	}
	if err := backend.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}
	if device.uninits != 1 {
		t.Fatalf("device uninits = %d, want 1", device.uninits)
	}
	if !source.closed {
		t.Fatal("Close should close the source")
	}
}

func assertMiniAudioLockAvailable(t *testing.T, backend *MiniAudioBackend) {
	t.Helper()
	if !backend.mu.TryLock() {
		t.Fatal("MiniAudioBackend.mu is held during a device lifecycle call")
	}
	backend.mu.Unlock()
}

type fakeMiniAudioDevice struct {
	start  func() error
	stop   func() error
	uninit func()

	starts  int
	stops   int
	uninits int
}

func (f *fakeMiniAudioDevice) Start() error {
	f.starts++
	if f.start != nil {
		return f.start()
	}
	return nil
}

func (f *fakeMiniAudioDevice) Stop() error {
	f.stops++
	if f.stop != nil {
		return f.stop()
	}
	return nil
}

func (f *fakeMiniAudioDevice) Uninit() {
	f.uninits++
	if f.uninit != nil {
		f.uninit()
	}
}

type fakePCMSource struct {
	channels   uint32
	sampleRate uint32
	length     uint64
	duration   float64
	position   float64

	reads  int
	seeks  int
	closed bool
}

func (f *fakePCMSource) Read(p []byte) (int, error) {
	f.reads++
	for i := range p {
		p[i] = byte(i)
	}
	f.position += float64(len(p)) / float64(f.channels*2) / float64(f.sampleRate)
	return len(p), nil
}

func (f *fakePCMSource) SeekFrame(frame uint64) error {
	f.seeks++
	if f.sampleRate > 0 {
		f.position = float64(frame) / float64(f.sampleRate)
	}
	return nil
}

func (f *fakePCMSource) Close() error {
	f.closed = true
	return nil
}

func (f *fakePCMSource) Channels() uint32     { return f.channels }
func (f *fakePCMSource) SampleRate() uint32   { return f.sampleRate }
func (f *fakePCMSource) LengthFrames() uint64 { return f.length }
func (f *fakePCMSource) Position() float64    { return f.position }
func (f *fakePCMSource) Duration() float64    { return f.duration }
