package audio

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hajimehoshi/go-mp3"
	"github.com/mewkiz/flac"
)

const (
	initialBufferSeconds      = 2.0
	resumeBufferSeconds       = 3.0
	rebufferSeconds           = 0.5
	streamRingFallbackSeconds = 120
)

var (
	ErrStreamSeekRequiresCache = errors.New("stream seek is unavailable until the cached file is ready")
	errBuffering               = errors.New("audio is buffering")
	errStreamReplaced          = errors.New("audio stream replaced")
)

type bufferedPCMSource interface {
	BufferedSeconds() float64
	BufferedBytes() int64
	TotalBytes() int64
	IsBuffering() bool
}

type streamPCMDecoder interface {
	io.Reader
	Channels() uint32
	SampleRate() uint32
	Duration() float64
}

type streamingPCMSource struct {
	parent     context.Context
	request    LoadRequest
	httpClient *http.Client

	mu      sync.RWMutex
	cancel  context.CancelFunc
	body    io.Closer
	ring    *pcmRingBuffer
	kind    string
	wavInfo *wavStreamInfo

	channels     uint32
	sampleRate   uint32
	duration     float64
	lengthFrames uint64
	frameBytes   int

	positionFrames atomic.Uint64
	compressedRead atomic.Int64
	totalBytes     atomic.Int64
	buffering      atomic.Bool
	started        atomic.Bool
	everStarted    atomic.Bool
	closed         atomic.Bool
	sequence       atomic.Uint64
}

func newStreamingPCMSource(ctx context.Context, request LoadRequest) (*streamingPCMSource, error) {
	if request.URI == "" {
		return nil, errors.New("empty stream URI")
	}
	source := &streamingPCMSource{
		parent:     ctx,
		request:    request,
		httpClient: http.DefaultClient,
	}
	if err := source.openStream(0, 0, nil); err != nil {
		return nil, err
	}
	return source, nil
}

func (s *streamingPCMSource) openStream(byteOffset int64, startFrame uint64, rawWAV *wavStreamInfo) error {
	ctx, cancel := context.WithCancel(s.parent)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.request.URI, nil)
	if err != nil {
		cancel()
		return err
	}
	if byteOffset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", byteOffset))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		cancel()
		return err
	}
	if byteOffset > 0 && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		cancel()
		return errors.New("stream server does not support byte range seek")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		cancel()
		return fmt.Errorf("stream returned HTTP %d", resp.StatusCode)
	}

	counter := &countingReadCloser{ReadCloser: resp.Body, counter: &s.compressedRead}
	s.compressedRead.Store(0)
	if total := responseTotalBytes(resp); total >= 0 {
		s.totalBytes.Store(total)
	}

	var decoder streamPCMDecoder
	kind := ""
	wavInfo := rawWAV
	if rawWAV != nil {
		decoder = newWAVDataStreamDecoder(counter, rawWAV, startFrame)
		kind = "wav"
	} else {
		reader := bufio.NewReaderSize(counter, 32*1024)
		decoder, kind, wavInfo, err = newStreamPCMDecoder(reader, s.request.DurationSeconds)
		if err != nil {
			counter.Close()
			cancel()
			return err
		}
	}

	channels := decoder.Channels()
	sampleRate := decoder.SampleRate()
	frameBytes := int(channels) * 2
	if channels == 0 || sampleRate == 0 || frameBytes == 0 {
		counter.Close()
		cancel()
		return errors.New("stream decoder returned invalid audio format")
	}
	duration := decoder.Duration()
	if duration <= 0 {
		duration = s.request.DurationSeconds
	}
	var lengthFrames uint64
	if duration > 0 {
		lengthFrames = uint64(duration * float64(sampleRate))
	}
	ring := newPCMRingBuffer(streamPCMBufferCapacity(sampleRate, frameBytes, duration))
	seq := s.sequence.Add(1)

	s.mu.Lock()
	oldCancel := s.cancel
	oldBody := s.body
	oldRing := s.ring
	s.cancel = cancel
	s.body = counter
	s.ring = ring
	s.kind = kind
	s.wavInfo = wavInfo
	s.channels = channels
	s.sampleRate = sampleRate
	s.duration = duration
	s.lengthFrames = lengthFrames
	s.frameBytes = frameBytes
	s.positionFrames.Store(startFrame)
	s.started.Store(false)
	s.buffering.Store(true)
	s.mu.Unlock()

	if oldCancel != nil {
		oldCancel()
	}
	if oldBody != nil {
		_ = oldBody.Close()
	}
	if oldRing != nil {
		oldRing.CloseWithError(errStreamReplaced)
	}

	go s.decodeLoop(seq, counter, decoder, ring)
	return nil
}

func (s *streamingPCMSource) decodeLoop(seq uint64, body io.Closer, decoder streamPCMDecoder, ring *pcmRingBuffer) {
	defer body.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := decoder.Read(buf)
		if n > 0 {
			if _, writeErr := ring.Write(buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				ring.CloseWithError(nil)
			} else {
				ring.CloseWithError(err)
			}
			return
		}
		if s.closed.Load() || seq != s.sequence.Load() {
			return
		}
	}
}

func (s *streamingPCMSource) Read(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, io.EOF
	}
	ring, frameBytes := s.currentRing()
	if ring == nil || frameBytes == 0 {
		s.buffering.Store(true)
		return 0, errBuffering
	}

	if s.shouldBuffer(ring, frameBytes) {
		return 0, errBuffering
	}

	n, err := ring.ReadAvailable(p)
	if n > 0 {
		s.positionFrames.Add(uint64(n / frameBytes))
		if !ring.Closed() && s.bufferedSecondsFor(ring, frameBytes) < rebufferSeconds {
			s.started.Store(false)
			s.buffering.Store(true)
		} else {
			s.buffering.Store(false)
		}
		return n, nil
	}
	if errors.Is(err, errStreamReplaced) {
		s.buffering.Store(true)
		return 0, errBuffering
	}
	if err != nil {
		return 0, err
	}

	s.started.Store(false)
	s.buffering.Store(true)
	return 0, errBuffering
}

func (s *streamingPCMSource) shouldBuffer(ring *pcmRingBuffer, frameBytes int) bool {
	closed := ring.Closed()
	buffered := s.bufferedSecondsFor(ring, frameBytes)
	if !s.started.Load() {
		threshold := initialBufferSeconds
		if s.everStarted.Load() || s.positionFrames.Load() > 0 {
			threshold = resumeBufferSeconds
		}
		if buffered < threshold && !closed {
			s.buffering.Store(true)
			return true
		}
		s.started.Store(true)
		s.everStarted.Store(true)
		s.buffering.Store(false)
		return false
	}
	if buffered < rebufferSeconds && !closed {
		s.started.Store(false)
		s.buffering.Store(true)
		return true
	}
	s.buffering.Store(false)
	return false
}

func (s *streamingPCMSource) SeekFrame(frame uint64) error {
	s.mu.RLock()
	kind := s.kind
	info := s.wavInfo
	lengthFrames := s.lengthFrames
	s.mu.RUnlock()

	if lengthFrames > 0 && frame > lengthFrames {
		frame = lengthFrames
	}
	if frame == 0 {
		s.everStarted.Store(false)
		return s.openStream(0, 0, nil)
	}
	if kind != "wav" || info == nil {
		return ErrStreamSeekRequiresCache
	}
	byteOffset := info.dataStart + int64(frame)*int64(info.format.blockAlign)
	return s.openStream(byteOffset, frame, info)
}

func (s *streamingPCMSource) Close() error {
	if s.closed.Swap(true) {
		return nil
	}
	s.mu.Lock()
	cancel := s.cancel
	body := s.body
	ring := s.ring
	s.cancel = nil
	s.body = nil
	s.ring = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if body != nil {
		_ = body.Close()
	}
	if ring != nil {
		ring.CloseWithError(errRingClosed)
	}
	return nil
}

func (s *streamingPCMSource) Channels() uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.channels
}

func (s *streamingPCMSource) SampleRate() uint32 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sampleRate
}

func (s *streamingPCMSource) LengthFrames() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lengthFrames
}

func (s *streamingPCMSource) Position() float64 {
	s.mu.RLock()
	sampleRate := s.sampleRate
	s.mu.RUnlock()
	if sampleRate == 0 {
		return 0
	}
	return float64(s.positionFrames.Load()) / float64(sampleRate)
}

func (s *streamingPCMSource) Duration() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.duration
}

func (s *streamingPCMSource) BufferedSeconds() float64 {
	ring, frameBytes := s.currentRing()
	return s.bufferedSecondsFor(ring, frameBytes)
}

func (s *streamingPCMSource) BufferedBytes() int64 {
	return s.compressedRead.Load()
}

func (s *streamingPCMSource) TotalBytes() int64 {
	return s.totalBytes.Load()
}

func (s *streamingPCMSource) IsBuffering() bool {
	return s.buffering.Load()
}

func (s *streamingPCMSource) currentRing() (*pcmRingBuffer, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ring, s.frameBytes
}

func (s *streamingPCMSource) bufferedSecondsFor(ring *pcmRingBuffer, frameBytes int) float64 {
	if ring == nil || frameBytes == 0 {
		return 0
	}
	s.mu.RLock()
	sampleRate := s.sampleRate
	s.mu.RUnlock()
	if sampleRate == 0 {
		return 0
	}
	return float64(ring.Available()/frameBytes) / float64(sampleRate)
}

func streamPCMBufferCapacity(sampleRate uint32, frameBytes int, duration float64) int {
	if sampleRate == 0 || frameBytes <= 0 {
		return 1
	}
	seconds := duration
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		seconds = streamRingFallbackSeconds
	}
	capacity := seconds * float64(sampleRate) * float64(frameBytes)
	minCapacity := float64(frameBytes)
	if capacity < minCapacity {
		capacity = minCapacity
	}
	maxInt := int(^uint(0) >> 1)
	if capacity > float64(maxInt) {
		return maxInt
	}
	return int(capacity + 0.5)
}

func newStreamPCMDecoder(r *bufio.Reader, fallbackDuration float64) (streamPCMDecoder, string, *wavStreamInfo, error) {
	header, err := r.Peek(12)
	if err != nil {
		return nil, "", nil, err
	}
	switch {
	case string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE":
		decoder, info, err := newWAVStreamDecoder(r, fallbackDuration)
		return decoder, "wav", info, err
	case string(header[:4]) == "fLaC":
		decoder, err := newFLACStreamDecoder(r, fallbackDuration)
		return decoder, "flac", nil, err
	default:
		decoder, err := newMP3StreamDecoder(r, fallbackDuration)
		return decoder, "mp3", nil, err
	}
}

type countingReadCloser struct {
	io.ReadCloser
	counter *atomic.Int64
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.counter.Add(int64(n))
	}
	return n, err
}

func responseTotalBytes(resp *http.Response) int64 {
	if contentRange := resp.Header.Get("Content-Range"); contentRange != "" {
		if slash := strings.LastIndex(contentRange, "/"); slash >= 0 && slash+1 < len(contentRange) {
			if total, err := strconv.ParseInt(strings.TrimSpace(contentRange[slash+1:]), 10, 64); err == nil {
				return total
			}
		}
	}
	return resp.ContentLength
}

type mp3StreamDecoder struct {
	decoder    *mp3.Decoder
	posBytes   int64
	sampleRate uint32
	duration   float64
}

func newMP3StreamDecoder(r io.Reader, fallbackDuration float64) (*mp3StreamDecoder, error) {
	decoder, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, err
	}
	duration := fallbackDuration
	if duration <= 0 && decoder.Length() > 0 && decoder.SampleRate() > 0 {
		duration = float64(decoder.Length()/4) / float64(decoder.SampleRate())
	}
	return &mp3StreamDecoder{
		decoder:    decoder,
		sampleRate: uint32(decoder.SampleRate()),
		duration:   duration,
	}, nil
}

func (d *mp3StreamDecoder) Read(p []byte) (int, error) {
	n, err := d.decoder.Read(p)
	d.posBytes += int64(n)
	return n, err
}

func (d *mp3StreamDecoder) Channels() uint32   { return 2 }
func (d *mp3StreamDecoder) SampleRate() uint32 { return d.sampleRate }
func (d *mp3StreamDecoder) Duration() float64  { return d.duration }

type wavStreamInfo struct {
	format       wavFormat
	dataStart    int64
	dataSize     uint32
	lengthFrames uint64
	duration     float64
}

type wavStreamDecoder struct {
	r            io.Reader
	info         *wavStreamInfo
	posFrames    uint64
	inputScratch []byte
}

func newWAVStreamDecoder(r io.Reader, fallbackDuration float64) (*wavStreamDecoder, *wavStreamInfo, error) {
	format, dataStart, dataSize, err := parseWAVHeaderReader(r)
	if err != nil {
		return nil, nil, err
	}
	if format.numChannels == 0 || format.sampleRate == 0 || format.blockAlign == 0 {
		return nil, nil, errors.New("invalid WAV format")
	}
	lengthFrames := uint64(dataSize) / uint64(format.blockAlign)
	duration := fallbackDuration
	if duration <= 0 {
		duration = float64(lengthFrames) / float64(format.sampleRate)
	}
	info := &wavStreamInfo{
		format:       format,
		dataStart:    dataStart,
		dataSize:     dataSize,
		lengthFrames: lengthFrames,
		duration:     duration,
	}
	return &wavStreamDecoder{r: r, info: info}, info, nil
}

func newWAVDataStreamDecoder(r io.Reader, info *wavStreamInfo, startFrame uint64) *wavStreamDecoder {
	return &wavStreamDecoder{
		r:         r,
		info:      info,
		posFrames: startFrame,
	}
}

func parseWAVHeaderReader(r io.Reader) (wavFormat, int64, uint32, error) {
	counter := &logicalCountingReader{r: r}
	var format wavFormat
	header := make([]byte, 12)
	if _, err := io.ReadFull(counter, header); err != nil {
		return format, 0, 0, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return format, 0, 0, errors.New("not a WAV file")
	}
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(counter, chunkHeader); err != nil {
			return format, 0, 0, err
		}
		id := string(chunkHeader[:4])
		size := binary.LittleEndian.Uint32(chunkHeader[4:])
		chunkDataStart := counter.n
		switch id {
		case "fmt ":
			if size < 16 {
				return format, 0, 0, errors.New("invalid WAV fmt chunk")
			}
			if size > 1<<20 {
				return format, 0, 0, errors.New("WAV fmt chunk is too large")
			}
			data := make([]byte, size)
			if _, err := io.ReadFull(counter, data); err != nil {
				return format, 0, 0, err
			}
			format = wavFormat{
				audioFormat:   binary.LittleEndian.Uint16(data[0:2]),
				numChannels:   binary.LittleEndian.Uint16(data[2:4]),
				sampleRate:    binary.LittleEndian.Uint32(data[4:8]),
				byteRate:      binary.LittleEndian.Uint32(data[8:12]),
				blockAlign:    binary.LittleEndian.Uint16(data[12:14]),
				bitsPerSample: binary.LittleEndian.Uint16(data[14:16]),
			}
		case "data":
			if format.numChannels == 0 {
				return format, 0, 0, errors.New("WAV data chunk found before fmt chunk")
			}
			return format, chunkDataStart, size, nil
		default:
			if _, err := io.CopyN(io.Discard, counter, int64(size)); err != nil {
				return format, 0, 0, err
			}
		}
		if size%2 == 1 {
			if _, err := io.CopyN(io.Discard, counter, 1); err != nil {
				return format, 0, 0, err
			}
		}
	}
}

type logicalCountingReader struct {
	r io.Reader
	n int64
}

func (r *logicalCountingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func (d *wavStreamDecoder) Read(p []byte) (int, error) {
	channels := int(d.info.format.numChannels)
	outputFrameBytes := channels * 2
	if outputFrameBytes == 0 {
		return 0, io.EOF
	}
	framesRequested := len(p) / outputFrameBytes
	framesLeft := int(d.info.lengthFrames - d.posFrames)
	if framesLeft <= 0 {
		return 0, io.EOF
	}
	if framesRequested > framesLeft {
		framesRequested = framesLeft
	}
	inputBytes := framesRequested * int(d.info.format.blockAlign)
	if cap(d.inputScratch) < inputBytes {
		d.inputScratch = make([]byte, inputBytes)
	}
	input := d.inputScratch[:inputBytes]
	n, err := io.ReadFull(d.r, input)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	framesRead := n / int(d.info.format.blockAlign)
	outBytes := framesRead * outputFrameBytes
	convertWAVToS16(p[:outBytes], input[:framesRead*int(d.info.format.blockAlign)], d.info.format)
	d.posFrames += uint64(framesRead)
	if framesRead == 0 && err != nil {
		return 0, err
	}
	if framesRead < framesRequested && err == nil {
		err = io.EOF
	}
	return outBytes, err
}

func (d *wavStreamDecoder) Channels() uint32   { return uint32(d.info.format.numChannels) }
func (d *wavStreamDecoder) SampleRate() uint32 { return d.info.format.sampleRate }
func (d *wavStreamDecoder) Duration() float64  { return d.info.duration }

type flacStreamDecoder struct {
	stream       *flac.Stream
	posFrames    uint64
	lengthFrames uint64
	sampleRate   uint32
	channels     uint32
	bits         uint8
	buffer       []byte
	duration     float64
}

func newFLACStreamDecoder(r io.Reader, fallbackDuration float64) (*flacStreamDecoder, error) {
	stream, err := flac.New(r)
	if err != nil {
		return nil, err
	}
	duration := fallbackDuration
	if duration <= 0 && stream.Info.SampleRate > 0 && stream.Info.NSamples > 0 {
		duration = float64(stream.Info.NSamples) / float64(stream.Info.SampleRate)
	}
	return &flacStreamDecoder{
		stream:       stream,
		lengthFrames: stream.Info.NSamples,
		sampleRate:   stream.Info.SampleRate,
		channels:     uint32(stream.Info.NChannels),
		bits:         stream.Info.BitsPerSample,
		duration:     duration,
	}, nil
}

func (d *flacStreamDecoder) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if len(d.buffer) == 0 {
			if err := d.readFrame(); err != nil {
				if total > 0 && errors.Is(err, io.EOF) {
					return total, nil
				}
				return total, err
			}
		}
		n := copy(p[total:], d.buffer)
		d.buffer = d.buffer[n:]
		total += n
	}
	return total, nil
}

func (d *flacStreamDecoder) readFrame() error {
	frame, err := d.stream.ParseNext()
	if err != nil {
		return err
	}
	channels := int(d.channels)
	blockSize := int(frame.BlockSize)
	if len(frame.Subframes) < channels {
		return fmt.Errorf("FLAC frame has %d subframes, expected %d", len(frame.Subframes), channels)
	}
	data := make([]byte, blockSize*channels*2)
	out := 0
	for i := 0; i < blockSize; i++ {
		for ch := 0; ch < channels; ch++ {
			sample := int32(0)
			if i < len(frame.Subframes[ch].Samples) {
				sample = frame.Subframes[ch].Samples[i]
			}
			sample16 := scaleIntToS16(sample, d.bits)
			binary.LittleEndian.PutUint16(data[out:], uint16(sample16))
			out += 2
		}
	}
	d.buffer = data
	d.posFrames += uint64(blockSize)
	return nil
}

func (d *flacStreamDecoder) Channels() uint32   { return d.channels }
func (d *flacStreamDecoder) SampleRate() uint32 { return d.sampleRate }
func (d *flacStreamDecoder) Duration() float64  { return d.duration }
