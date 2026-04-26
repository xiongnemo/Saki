package audio

import (
	"errors"
	"io"
	"sync"
)

var errRingClosed = errors.New("PCM ring buffer closed")

type pcmRingBuffer struct {
	mu      sync.Mutex
	notFull *sync.Cond
	buf     []byte
	read    int
	write   int
	used    int
	closed  bool
	err     error
}

func newPCMRingBuffer(capacity int) *pcmRingBuffer {
	if capacity < 1 {
		capacity = 1
	}
	r := &pcmRingBuffer{buf: make([]byte, capacity)}
	r.notFull = sync.NewCond(&r.mu)
	return r
}

func (r *pcmRingBuffer) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		r.mu.Lock()
		for r.used == len(r.buf) && !r.closed {
			r.notFull.Wait()
		}
		if r.closed {
			err := r.err
			r.mu.Unlock()
			if err == nil {
				err = errRingClosed
			}
			return written, err
		}

		free := len(r.buf) - r.used
		n := free
		if n > len(p) {
			n = len(p)
		}
		if end := len(r.buf) - r.write; n > end {
			copy(r.buf[r.write:], p[:end])
			copy(r.buf, p[end:n])
			r.write = n - end
		} else {
			copy(r.buf[r.write:r.write+n], p[:n])
			r.write = (r.write + n) % len(r.buf)
		}
		r.used += n
		r.mu.Unlock()

		written += n
		p = p[n:]
	}
	return written, nil
}

func (r *pcmRingBuffer) ReadAvailable(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.used == 0 {
		if r.closed {
			if r.err != nil {
				return 0, r.err
			}
			return 0, io.EOF
		}
		return 0, nil
	}

	n := r.used
	if n > len(p) {
		n = len(p)
	}
	if end := len(r.buf) - r.read; n > end {
		copy(p, r.buf[r.read:])
		copy(p[end:n], r.buf[:n-end])
		r.read = n - end
	} else {
		copy(p, r.buf[r.read:r.read+n])
		r.read = (r.read + n) % len(r.buf)
	}
	r.used -= n
	r.notFull.Signal()
	return n, nil
}

func (r *pcmRingBuffer) Available() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.used
}

func (r *pcmRingBuffer) Closed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *pcmRingBuffer) CloseWithError(err error) {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		r.err = err
		r.notFull.Broadcast()
	}
	r.mu.Unlock()
}
