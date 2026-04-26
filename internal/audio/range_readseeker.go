package audio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const httpRangeWindowBytes int64 = 1 << 20

type httpRangeReadSeeker struct {
	ctx    context.Context
	cancel context.CancelFunc
	client *http.Client
	uri    string

	size       int64
	pos        int64
	windowSize int64
	bufStart   int64
	buf        []byte
}

func newHTTPRangeReadSeeker(ctx context.Context, uri string, windowSize int64) (*httpRangeReadSeeker, error) {
	if windowSize <= 0 {
		windowSize = httpRangeWindowBytes
	}
	childCtx, cancel := context.WithCancel(ctx)
	reader := &httpRangeReadSeeker{
		ctx:        childCtx,
		cancel:     cancel,
		client:     http.DefaultClient,
		uri:        uri,
		windowSize: windowSize,
		bufStart:   -1,
	}
	if err := reader.probe(); err != nil {
		cancel()
		return nil, err
	}
	return reader, nil
}

func (r *httpRangeReadSeeker) probe() error {
	resp, err := r.getRange(0, 0)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		return ErrStreamSeekRequiresCache
	}
	start, _, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		return err
	}
	if start != 0 || total <= 0 {
		return ErrStreamSeekRequiresCache
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	r.size = total
	r.bufStart = 0
	r.buf = data
	return nil
}

func (r *httpRangeReadSeeker) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.pos >= r.size {
		return 0, io.EOF
	}

	total := 0
	for len(p) > 0 && r.pos < r.size {
		if !r.posInBuffer() {
			if err := r.fetchWindow(r.pos); err != nil {
				if total > 0 && errors.Is(err, io.EOF) {
					return total, nil
				}
				return total, err
			}
		}
		offset := int(r.pos - r.bufStart)
		if offset < 0 || offset >= len(r.buf) {
			if total > 0 {
				return total, nil
			}
			return total, io.ErrUnexpectedEOF
		}
		n := copy(p, r.buf[offset:])
		if n == 0 {
			if total > 0 {
				return total, nil
			}
			return total, io.ErrUnexpectedEOF
		}
		r.pos += int64(n)
		total += n
		p = p[n:]
	}
	return total, nil
}

func (r *httpRangeReadSeeker) Seek(offset int64, whence int) (int64, error) {
	var next int64
	switch whence {
	case io.SeekStart:
		next = offset
	case io.SeekCurrent:
		next = r.pos + offset
	case io.SeekEnd:
		next = r.size + offset
	default:
		return r.pos, errors.New("invalid seek whence")
	}
	if next < 0 {
		return r.pos, errors.New("negative seek position")
	}
	r.pos = next
	return r.pos, nil
}

func (r *httpRangeReadSeeker) Close() error {
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}

func (r *httpRangeReadSeeker) posInBuffer() bool {
	return r.bufStart >= 0 && r.pos >= r.bufStart && r.pos < r.bufStart+int64(len(r.buf))
}

func (r *httpRangeReadSeeker) fetchWindow(start int64) error {
	if start >= r.size {
		r.bufStart = start
		r.buf = nil
		return io.EOF
	}
	end := start + r.windowSize - 1
	if end >= r.size {
		end = r.size - 1
	}
	resp, err := r.getRange(start, end)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return io.EOF
	}
	if resp.StatusCode != http.StatusPartialContent {
		return ErrStreamSeekRequiresCache
	}
	rangeStart, rangeEnd, total, err := parseContentRange(resp.Header.Get("Content-Range"))
	if err != nil {
		return err
	}
	if rangeStart != start || rangeEnd < rangeStart || total != r.size {
		return fmt.Errorf("unexpected content range %q", resp.Header.Get("Content-Range"))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if len(data) == 0 && start < r.size {
		return io.ErrUnexpectedEOF
	}
	maxBytes := rangeEnd - rangeStart + 1
	if int64(len(data)) > maxBytes {
		data = data[:maxBytes]
	}
	r.bufStart = rangeStart
	r.buf = data
	return nil
}

func (r *httpRangeReadSeeker) getRange(start, end int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || (resp.StatusCode >= 300 && resp.StatusCode != http.StatusRequestedRangeNotSatisfiable) {
		resp.Body.Close()
		return nil, fmt.Errorf("range stream returned HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

func parseContentRange(value string) (int64, int64, int64, error) {
	value = strings.TrimSpace(value)
	const prefix = "bytes "
	if !strings.HasPrefix(value, prefix) {
		return 0, 0, 0, fmt.Errorf("invalid content range %q", value)
	}
	parts := strings.Split(strings.TrimPrefix(value, prefix), "/")
	if len(parts) != 2 || parts[1] == "" || parts[1] == "*" {
		return 0, 0, 0, fmt.Errorf("invalid content range %q", value)
	}
	rangeParts := strings.Split(parts[0], "-")
	if len(rangeParts) != 2 {
		return 0, 0, 0, fmt.Errorf("invalid content range %q", value)
	}
	start, err := strconv.ParseInt(rangeParts[0], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid content range %q", value)
	}
	end, err := strconv.ParseInt(rangeParts[1], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid content range %q", value)
	}
	total, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid content range %q", value)
	}
	if start < 0 || end < start || total <= end {
		return 0, 0, 0, fmt.Errorf("invalid content range %q", value)
	}
	return start, end, total, nil
}
