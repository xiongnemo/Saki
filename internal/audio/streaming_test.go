package audio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStreamingPCMSourceStartsBeforeFullHTTPBody(t *testing.T) {
	const sampleRate = 8000
	wav := testWAV(t, 4, sampleRate)
	initialBytes := 44 + int(float64(sampleRate*2)*2.1)

	initialServed := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var releaseOnce sync.Once
	releaseResponse := func() {
		releaseOnce.Do(func() { close(release) })
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(wav)))
		_, _ = w.Write(wav[:initialBytes])
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(initialServed)
		<-release
		_, _ = w.Write(wav[initialBytes:])
		close(done)
	}))
	defer func() {
		releaseResponse()
		server.Close()
	}()

	source, err := newStreamingPCMSource(context.Background(), LoadRequest{
		URI:             server.URL,
		DurationSeconds: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	select {
	case <-initialServed:
	case <-time.After(time.Second):
		t.Fatal("server did not send initial bytes")
	}
	eventuallyAudio(t, func() bool { return source.BufferedSeconds() >= 2 })

	select {
	case <-done:
		t.Fatal("source waited for the full HTTP body before becoming readable")
	default:
	}

	buf := make([]byte, sampleRate/10*2)
	n, err := source.Read(buf)
	if n == 0 || errors.Is(err, errBuffering) {
		t.Fatalf("read = %d, %v; want buffered PCM", n, err)
	}

	releaseResponse()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not finish after release")
	}
}

func TestStreamingWAVSourceRangeSeek(t *testing.T) {
	const sampleRate = 8000
	wav := testWAV(t, 4, sampleRate)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
			start, err := parseTestRangeStart(rangeHeader)
			if err != nil || start >= len(wav) {
				http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(wav)-1, len(wav)))
			w.Header().Set("Content-Length", strconv.Itoa(len(wav)-start))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(wav[start:])
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(wav)))
		_, _ = w.Write(wav)
	}))
	defer server.Close()

	source, err := newStreamingPCMSource(context.Background(), LoadRequest{
		URI:             server.URL,
		DurationSeconds: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	if err := source.SeekFrame(uint64(sampleRate * 2)); err != nil {
		t.Fatal(err)
	}
	eventuallyAudio(t, func() bool { return source.BufferedSeconds() > 0 })

	buf := make([]byte, sampleRate/10*2)
	n, err := source.Read(buf)
	if n == 0 || errors.Is(err, errBuffering) {
		t.Fatalf("read after seek = %d, %v; want PCM", n, err)
	}
	if position := source.Position(); position < 2 {
		t.Fatalf("position after seek/read = %.3f, want >= 2", position)
	}
}

func TestStreamPCMBufferCapacity(t *testing.T) {
	if got := streamPCMBufferCapacity(8000, 2, 4); got != 64000 {
		t.Fatalf("duration capacity = %d, want 64000", got)
	}
	if got := streamPCMBufferCapacity(8000, 2, 0); got != 1920000 {
		t.Fatalf("fallback capacity = %d, want 1920000", got)
	}
	if got := streamPCMBufferCapacity(8000, 2, -1); got != 1920000 {
		t.Fatalf("negative duration capacity = %d, want fallback", got)
	}
	if got := streamPCMBufferCapacity(0, 2, 4); got != 1 {
		t.Fatalf("invalid format capacity = %d, want 1", got)
	}
	if got := streamPCMBufferCapacity(8000, 2, 0.00001); got != 2 {
		t.Fatalf("minimum capacity = %d, want one frame", got)
	}
}

func TestStreamPCMDecoderDefersALACToRangeSource(t *testing.T) {
	stream := "\x00\x00\x00\x18ftypM4A "
	_, _, _, err := newStreamPCMDecoder(bufio.NewReader(strings.NewReader(stream)), 0)
	if !errors.Is(err, errALACStreamRequiresRange) {
		t.Fatalf("error = %v", err)
	}
}

func TestOpenPCMSourceStreamsHTTPALACWithRange(t *testing.T) {
	m4a := testALACM4A(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(m4a)
			return
		}
		writeTestRange(t, w, m4a, r.Header.Get("Range"))
	}))
	defer server.Close()

	source, err := openPCMSource(context.Background(), LoadRequest{
		URI:             server.URL,
		DurationSeconds: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()

	streaming, ok := source.(*streamingPCMSource)
	if !ok {
		t.Fatalf("source type = %T, want *streamingPCMSource", source)
	}
	streaming.mu.RLock()
	kind := streaming.kind
	streaming.mu.RUnlock()
	if kind != "alac" {
		t.Fatalf("stream kind = %q, want alac", kind)
	}

	readPCMFromSource(t, source)
	if err := source.SeekFrame(uint64(source.SampleRate() / 20)); err != nil {
		t.Fatal(err)
	}
	readPCMFromSource(t, source)
}

func TestOpenPCMSourceHTTPALACRequiresRange(t *testing.T) {
	m4a := testALACM4A(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(m4a)
	}))
	defer server.Close()

	_, err := openPCMSource(context.Background(), LoadRequest{
		URI:             server.URL,
		DurationSeconds: 1,
	})
	if !errors.Is(err, ErrStreamSeekRequiresCache) {
		t.Fatalf("error = %v, want ErrStreamSeekRequiresCache", err)
	}
}

func testWAV(t *testing.T, seconds int, sampleRate int) []byte {
	t.Helper()

	dataBytes := seconds * sampleRate * 2
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataBytes))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataBytes))
	for i := 0; i < seconds*sampleRate; i++ {
		_ = binary.Write(&buf, binary.LittleEndian, int16(i%1024))
	}
	return buf.Bytes()
}

func testALACM4A(t *testing.T) []byte {
	t.Helper()
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
		"-i", "sine=frequency=440:duration=0.25:sample_rate=44100",
		"-c:a", "alac",
		m4aPath,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(m4aPath)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readPCMFromSource(t *testing.T, source pcmSource) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		n, err := source.Read(buf)
		if n > 0 {
			return
		}
		if err != nil && !errors.Is(err, errBuffering) && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for decoded PCM")
}

func parseTestRangeStart(header string) (int, error) {
	value := strings.TrimPrefix(header, "bytes=")
	value = strings.TrimSuffix(value, "-")
	return strconv.Atoi(value)
}

func eventuallyAudio(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("condition not met before timeout")
		case <-tick.C:
			if condition() {
				return
			}
		}
	}
}
