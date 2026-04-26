package audio

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestPCMRingBufferReadWriteAndEOF(t *testing.T) {
	ring := newPCMRingBuffer(8)
	if n, err := ring.Write([]byte("abcdef")); err != nil || n != 6 {
		t.Fatalf("write = %d, %v", n, err)
	}
	if ring.Available() != 6 {
		t.Fatalf("available = %d, want 6", ring.Available())
	}

	buf := make([]byte, 4)
	if n, err := ring.ReadAvailable(buf); err != nil || n != 4 {
		t.Fatalf("read = %d, %v", n, err)
	}
	if string(buf) != "abcd" {
		t.Fatalf("read bytes = %q", buf)
	}

	if n, err := ring.Write([]byte("ghij")); err != nil || n != 4 {
		t.Fatalf("wrap write = %d, %v", n, err)
	}
	out := make([]byte, 6)
	if n, err := ring.ReadAvailable(out); err != nil || n != 6 {
		t.Fatalf("wrap read = %d, %v", n, err)
	}
	if string(out) != "efghij" {
		t.Fatalf("wrap read bytes = %q", out)
	}

	ring.CloseWithError(nil)
	if n, err := ring.ReadAvailable(out); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("closed read = %d, %v; want EOF", n, err)
	}
}

func TestPCMRingBufferCloseUnblocksWriter(t *testing.T) {
	ring := newPCMRingBuffer(4)
	done := make(chan error, 1)
	go func() {
		_, err := ring.Write([]byte("abcdef"))
		done <- err
	}()

	deadline := time.After(time.Second)
	for ring.Available() < 4 {
		select {
		case <-deadline:
			t.Fatal("writer did not fill ring")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	ring.CloseWithError(errRingClosed)
	select {
	case err := <-done:
		if !errors.Is(err, errRingClosed) {
			t.Fatalf("writer err = %v, want errRingClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("writer did not unblock")
	}
}
