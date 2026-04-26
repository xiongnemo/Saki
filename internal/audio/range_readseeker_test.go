package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestHTTPRangeReadSeekerReadSeekAndReuse(t *testing.T) {
	data := []byte("abcdefghijklmnopqrstuvwxyz")
	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		mu.Lock()
		ranges = append(ranges, rangeHeader)
		mu.Unlock()
		writeTestRange(t, w, data, rangeHeader)
	}))
	defer server.Close()

	reader, err := newHTTPRangeReadSeeker(context.Background(), server.URL, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	buf := make([]byte, 6)
	n, err := reader.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(buf[:n]); got != "abcdef" {
		t.Fatalf("first read = %q", got)
	}

	if pos, err := reader.Seek(10, io.SeekStart); err != nil || pos != 10 {
		t.Fatalf("seek start = %d, %v", pos, err)
	}
	buf = make([]byte, 2)
	if n, err = reader.Read(buf); err != nil || string(buf[:n]) != "kl" {
		t.Fatalf("seeked read = %q, %v", string(buf[:n]), err)
	}
	if pos, err := reader.Seek(11, io.SeekStart); err != nil || pos != 11 {
		t.Fatalf("seek reused window = %d, %v", pos, err)
	}
	if n, err = reader.Read(buf); err != nil || string(buf[:n]) != "lm" {
		t.Fatalf("reused read = %q, %v", string(buf[:n]), err)
	}
	if pos, err := reader.Seek(-3, io.SeekEnd); err != nil || pos != 23 {
		t.Fatalf("seek end = %d, %v", pos, err)
	}
	buf = make([]byte, 8)
	if n, err = reader.Read(buf); err != nil || string(buf[:n]) != "xyz" {
		t.Fatalf("end read = %q, %v", string(buf[:n]), err)
	}

	mu.Lock()
	defer mu.Unlock()
	count := 0
	for _, rangeHeader := range ranges {
		if rangeHeader == "bytes=10-13" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("range window bytes=10-13 requested %d times; ranges=%v", count, ranges)
	}
}

func TestHTTPRangeReadSeekerRejectsNoRangeSupport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not a partial response"))
	}))
	defer server.Close()

	_, err := newHTTPRangeReadSeeker(context.Background(), server.URL, 4)
	if !errors.Is(err, ErrStreamSeekRequiresCache) {
		t.Fatalf("error = %v, want ErrStreamSeekRequiresCache", err)
	}
}

func TestHTTPRangeReadSeekerRejectsUnknownTotal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-0/*")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()

	if _, err := newHTTPRangeReadSeeker(context.Background(), server.URL, 4); err == nil {
		t.Fatal("expected unknown total size to fail")
	}
}

func TestHTTPRangeReadSeekerRejectsMissingContentRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()

	if _, err := newHTTPRangeReadSeeker(context.Background(), server.URL, 4); err == nil {
		t.Fatal("expected missing Content-Range to fail")
	}
}

func TestHTTPRangeReadSeekerHandlesRangeNotSatisfiable(t *testing.T) {
	data := []byte("abc")
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			writeTestRange(t, w, data, r.Header.Get("Range"))
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(data)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer server.Close()

	reader, err := newHTTPRangeReadSeeker(context.Background(), server.URL, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	buf := make([]byte, 2)
	n, err := reader.Read(buf)
	if err != nil || n != 1 || string(buf[:n]) != "a" {
		t.Fatalf("first read = %d %q, %v", n, string(buf[:n]), err)
	}
	n, err = reader.Read(buf)
	if !errors.Is(err, io.EOF) || n != 0 {
		t.Fatalf("second read = %d, %v; want EOF", n, err)
	}
}

func writeTestRange(t *testing.T, w http.ResponseWriter, data []byte, rangeHeader string) {
	t.Helper()
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		t.Fatalf("missing range header %q", rangeHeader)
	}
	parts := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
	if len(parts) != 2 {
		t.Fatalf("invalid range header %q", rangeHeader)
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	end := len(data) - 1
	if parts[1] != "" {
		end, err = strconv.Atoi(parts[1])
		if err != nil {
			t.Fatal(err)
		}
	}
	if start >= len(data) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(data)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if end >= len(data) {
		end = len(data) - 1
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
	w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(data[start : end+1])
}
