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
