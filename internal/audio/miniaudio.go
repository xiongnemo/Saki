package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
	"github.com/hajimehoshi/go-mp3"
	"github.com/mewkiz/flac"
	"github.com/xiongnemo/saki/internal/models"
)

const (
	maxS16 = 1<<15 - 1
	minS16 = -1 << 15
)

type MiniAudioBackend struct {
	mu sync.Mutex

	ctx    *malgo.AllocatedContext
	device *malgo.Device
	source pcmSource

	events    chan Event
	loaded    bool
	paused    bool
	completed bool
	position  float64
	duration  float64
	volume    float64

	lastBuffering      bool
	lastBufferingKnown bool
	lastBufferProgress time.Time
	lastPositionEvent  time.Time
}

func NewMiniAudioBackend() *MiniAudioBackend {
	return &MiniAudioBackend{
		events: make(chan Event, 64),
		paused: true,
		volume: 1,
	}
}

func (b *MiniAudioBackend) Load(ctx context.Context, request LoadRequest) error {
	source, err := openPCMSource(ctx, request)
	if err != nil {
		return err
	}
	audioInfo := sourceAudioInfo(source)

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.closeLocked(); err != nil {
		_ = source.Close()
		return err
	}
	if b.ctx == nil {
		audioCtx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
		if err != nil {
			_ = source.Close()
			return err
		}
		b.ctx = audioCtx
	}

	config := malgo.DefaultDeviceConfig(malgo.Playback)
	config.Playback.Format = malgo.FormatS16
	config.Playback.Channels = source.Channels()
	config.SampleRate = source.SampleRate()
	config.Alsa.NoMMap = 1

	callbacks := malgo.DeviceCallbacks{
		Data: b.onSamples,
	}
	device, err := malgo.InitDevice(b.ctx.Context, config, callbacks)
	if err != nil {
		_ = source.Close()
		return err
	}

	b.device = device
	b.source = source
	b.loaded = true
	b.paused = true
	b.completed = false
	b.position = 0
	b.duration = source.Duration()
	b.lastBuffering = false
	b.lastBufferingKnown = false
	b.lastBufferProgress = time.Time{}
	b.lastPositionEvent = time.Time{}
	b.emitLocked(Event{Type: EventFormat, AudioInfo: audioInfo})
	return nil
}

func (b *MiniAudioBackend) Play() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.device == nil || b.source == nil {
		return errors.New("miniaudio has no loaded track")
	}
	if b.completed {
		if err := b.source.SeekFrame(0); err != nil {
			return err
		}
		b.completed = false
	}
	if err := b.device.Start(); err != nil {
		return err
	}
	b.loaded = true
	b.paused = false
	return nil
}

func (b *MiniAudioBackend) Pause() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.device != nil {
		if err := b.device.Stop(); err != nil {
			return err
		}
	}
	b.paused = true
	return nil
}

func (b *MiniAudioBackend) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.device != nil {
		if err := b.device.Stop(); err != nil {
			return err
		}
	}
	if b.source != nil {
		_ = b.source.SeekFrame(0)
	}
	b.position = 0
	b.paused = true
	b.loaded = b.source != nil
	b.completed = false
	return nil
}

func (b *MiniAudioBackend) Seek(position float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.source == nil {
		return nil
	}
	if position < 0 {
		position = 0
	}
	if position > 1 {
		position = 1
	}
	frame := uint64(position * float64(b.source.LengthFrames()))
	if err := b.source.SeekFrame(frame); err != nil {
		return err
	}
	b.position = b.source.Position()
	b.completed = false
	b.lastPositionEvent = time.Time{}
	b.emitLocked(Event{Type: EventPosition, Position: b.position, Duration: b.duration})
	return nil
}

func (b *MiniAudioBackend) SetVolume(volume float64) error {
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	b.mu.Lock()
	b.volume = volume
	b.mu.Unlock()
	return nil
}

func (b *MiniAudioBackend) Position() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.position
}

func (b *MiniAudioBackend) Duration() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.duration
}

func (b *MiniAudioBackend) IsPlaying() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loaded && !b.paused
}

func (b *MiniAudioBackend) IsPaused() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.loaded && b.paused
}

func (b *MiniAudioBackend) Events() <-chan Event {
	return b.events
}

func (b *MiniAudioBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.closeLocked(); err != nil {
		return err
	}
	if b.ctx != nil {
		if err := b.ctx.Uninit(); err != nil {
			return err
		}
		b.ctx.Free()
		b.ctx = nil
	}
	return nil
}

func (b *MiniAudioBackend) closeLocked() error {
	if b.device != nil {
		b.device.Uninit()
		b.device = nil
	}
	if b.source != nil {
		_ = b.source.Close()
		b.source = nil
	}
	b.loaded = false
	b.paused = true
	b.completed = false
	b.position = 0
	b.duration = 0
	b.lastBuffering = false
	b.lastBufferingKnown = false
	b.lastBufferProgress = time.Time{}
	b.lastPositionEvent = time.Time{}
	return nil
}

func (b *MiniAudioBackend) onSamples(output, _ []byte, _ uint32) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range output {
		output[i] = 0
	}
	if b.paused || b.source == nil || b.completed {
		return
	}

	n, err := b.source.Read(output)
	if n > 0 && b.volume != 1 {
		scaleS16(output[:n], b.volume)
	}
	b.position = b.source.Position()
	if b.shouldEmitPositionLocked(false) {
		b.emitLocked(Event{Type: EventPosition, Position: b.position, Duration: b.duration})
	}
	b.reportBufferLocked(false)
	if errors.Is(err, errBuffering) {
		return
	}
	if errors.Is(err, io.EOF) {
		b.completed = true
		b.loaded = false
		b.paused = true
		position := b.position
		duration := b.duration
		device := b.device
		go func() {
			if device != nil {
				_ = device.Stop()
			}
			b.emit(Event{Type: EventCompleted, Position: position, Duration: duration})
		}()
	} else if err != nil {
		b.completed = true
		b.loaded = false
		b.paused = true
		b.emitLocked(Event{Type: EventError, Position: b.position, Duration: b.duration, Err: err})
	}
}

func (b *MiniAudioBackend) shouldEmitPositionLocked(force bool) bool {
	now := time.Now()
	if force || now.Sub(b.lastPositionEvent) >= 250*time.Millisecond {
		b.lastPositionEvent = now
		return true
	}
	return false
}

func (b *MiniAudioBackend) reportBufferLocked(force bool) {
	source, ok := b.source.(bufferedPCMSource)
	if !ok {
		if b.lastBufferingKnown && b.lastBuffering {
			b.emitLocked(Event{Type: EventBuffering, Position: b.position, Duration: b.duration, Buffering: false})
		}
		b.lastBufferingKnown = false
		b.lastBuffering = false
		return
	}

	buffering := source.IsBuffering()
	event := Event{
		Position:        b.position,
		Duration:        b.duration,
		Buffering:       buffering,
		BufferedSeconds: source.BufferedSeconds(),
		BufferedBytes:   source.BufferedBytes(),
		TotalBytes:      source.TotalBytes(),
	}
	if !b.lastBufferingKnown || buffering != b.lastBuffering {
		event.Type = EventBuffering
		b.emitLocked(event)
		b.lastBufferingKnown = true
		b.lastBuffering = buffering
	}

	now := time.Now()
	if force || now.Sub(b.lastBufferProgress) >= 250*time.Millisecond {
		event.Type = EventBufferProgress
		b.emitLocked(event)
		b.lastBufferProgress = now
	}
}

func (b *MiniAudioBackend) emit(event Event) {
	select {
	case b.events <- event:
	default:
	}
}

func (b *MiniAudioBackend) emitLocked(event Event) {
	select {
	case b.events <- event:
	default:
	}
}

func scaleS16(data []byte, volume float64) {
	for i := 0; i+1 < len(data); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(data[i:]))
		scaled := int(float64(sample) * volume)
		if scaled > maxS16 {
			scaled = maxS16
		}
		if scaled < minS16 {
			scaled = minS16
		}
		binary.LittleEndian.PutUint16(data[i:], uint16(int16(scaled)))
	}
}

type pcmSource interface {
	Read([]byte) (int, error)
	SeekFrame(uint64) error
	Close() error
	Channels() uint32
	SampleRate() uint32
	LengthFrames() uint64
	Position() float64
	Duration() float64
}

type audioInfoProvider interface {
	AudioInfo() models.AudioInfo
}

func sourceAudioInfo(source pcmSource) models.AudioInfo {
	info := models.AudioInfo{}
	if provider, ok := source.(audioInfoProvider); ok {
		info = provider.AudioInfo()
	}
	if info.SampleRate == 0 {
		info.SampleRate = int(source.SampleRate())
	}
	if info.Channels == 0 {
		info.Channels = int(source.Channels())
	}
	return info
}

func openPCMSource(ctx context.Context, request LoadRequest) (pcmSource, error) {
	path := request.URI
	if parsed, err := url.Parse(request.URI); err == nil && parsed.Scheme == "file" {
		path = localPathFromFileURL(parsed)
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		source, err := newStreamingPCMSource(ctx, request)
		if err == nil {
			return source, nil
		}
		if errors.Is(err, errALACStreamRequiresRange) {
			return newALACStreamingPCMSource(ctx, request)
		}
		return nil, err
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		_ = file.Close()
		return nil, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}

	switch {
	case string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE":
		return newWAVSource(file, path)
	case string(header[:4]) == "fLaC":
		return newFLACSource(file, path)
	case isMP4Header(header):
		return newALACSource(file, path)
	default:
		return newMP3Source(file, path)
	}
}

func localPathFromFileURL(parsed *url.URL) string {
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		path = parsed.Path
	}
	if runtime.GOOS == "windows" && strings.HasPrefix(path, "/") && len(path) >= 3 && path[2] == ':' {
		path = path[1:]
	}
	if parsed.Host != "" {
		path = `\\` + parsed.Host + filepath.FromSlash(path)
	}
	return filepath.FromSlash(path)
}

type mp3Source struct {
	file         *os.File
	decoder      *mp3.Decoder
	path         string
	posBytes     int64
	lengthFrames uint64
	sampleRate   uint32
}

func newMP3Source(file *os.File, path string) (*mp3Source, error) {
	decoder, err := mp3.NewDecoder(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	length := decoder.Length()
	var lengthFrames uint64
	if length > 0 {
		lengthFrames = uint64(length / 4)
	}
	return &mp3Source{
		file:         file,
		decoder:      decoder,
		path:         path,
		lengthFrames: lengthFrames,
		sampleRate:   uint32(decoder.SampleRate()),
	}, nil
}

func (s *mp3Source) Read(p []byte) (int, error) {
	n, err := s.decoder.Read(p)
	s.posBytes += int64(n)
	return n, err
}

func (s *mp3Source) SeekFrame(frame uint64) error {
	pos, err := s.decoder.Seek(int64(frame*4), io.SeekStart)
	s.posBytes = pos
	return err
}

func (s *mp3Source) Close() error { return s.file.Close() }
func (s *mp3Source) Channels() uint32 {
	return 2
}
func (s *mp3Source) SampleRate() uint32 { return s.sampleRate }
func (s *mp3Source) LengthFrames() uint64 {
	return s.lengthFrames
}
func (s *mp3Source) Position() float64 {
	if s.sampleRate == 0 {
		return 0
	}
	return float64(s.posBytes/4) / float64(s.sampleRate)
}
func (s *mp3Source) Duration() float64 {
	if s.sampleRate == 0 || s.lengthFrames == 0 {
		return 0
	}
	return float64(s.lengthFrames) / float64(s.sampleRate)
}

func (s *mp3Source) AudioInfo() models.AudioInfo {
	return models.AudioInfo{
		Codec:       "MP3",
		SampleRate:  int(s.sampleRate),
		Channels:    2,
		BitRateKbps: fileBitRateKbps(s.file, s.Duration()),
	}
}

type wavSource struct {
	file          *os.File
	format        wavFormat
	dataStart     int64
	dataSize      uint32
	posFrames     uint64
	lengthFrames  uint64
	inputScratch  []byte
	outputScratch []byte
}

type wavFormat struct {
	audioFormat   uint16
	numChannels   uint16
	sampleRate    uint32
	byteRate      uint32
	blockAlign    uint16
	bitsPerSample uint16
}

func newWAVSource(file *os.File, _ string) (*wavSource, error) {
	format, dataStart, dataSize, err := parseWAVHeader(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if format.numChannels == 0 || format.sampleRate == 0 || format.blockAlign == 0 {
		_ = file.Close()
		return nil, errors.New("invalid WAV format")
	}
	if _, err := file.Seek(dataStart, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &wavSource{
		file:         file,
		format:       format,
		dataStart:    dataStart,
		dataSize:     dataSize,
		lengthFrames: uint64(dataSize) / uint64(format.blockAlign),
	}, nil
}

func parseWAVHeader(file *os.File) (wavFormat, int64, uint32, error) {
	var format wavFormat
	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		return format, 0, 0, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return format, 0, 0, errors.New("not a WAV file")
	}
	for {
		chunkHeader := make([]byte, 8)
		if _, err := io.ReadFull(file, chunkHeader); err != nil {
			return format, 0, 0, err
		}
		id := string(chunkHeader[:4])
		size := binary.LittleEndian.Uint32(chunkHeader[4:])
		chunkDataStart, _ := file.Seek(0, io.SeekCurrent)
		switch id {
		case "fmt ":
			if size < 16 {
				return format, 0, 0, errors.New("invalid WAV fmt chunk")
			}
			data := make([]byte, size)
			if _, err := io.ReadFull(file, data); err != nil {
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
			if _, err := file.Seek(int64(size), io.SeekCurrent); err != nil {
				return format, 0, 0, err
			}
		}
		if size%2 == 1 {
			if _, err := file.Seek(1, io.SeekCurrent); err != nil {
				return format, 0, 0, err
			}
		}
	}
}

func (s *wavSource) Read(p []byte) (int, error) {
	channels := int(s.format.numChannels)
	outputFrameBytes := channels * 2
	if outputFrameBytes == 0 {
		return 0, io.EOF
	}
	framesRequested := len(p) / outputFrameBytes
	framesLeft := int(s.lengthFrames - s.posFrames)
	if framesLeft <= 0 {
		return 0, io.EOF
	}
	if framesRequested > framesLeft {
		framesRequested = framesLeft
	}
	inputBytes := framesRequested * int(s.format.blockAlign)
	if cap(s.inputScratch) < inputBytes {
		s.inputScratch = make([]byte, inputBytes)
	}
	input := s.inputScratch[:inputBytes]
	n, err := io.ReadFull(s.file, input)
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	framesRead := n / int(s.format.blockAlign)
	outBytes := framesRead * outputFrameBytes
	convertWAVToS16(p[:outBytes], input[:framesRead*int(s.format.blockAlign)], s.format)
	s.posFrames += uint64(framesRead)
	if framesRead == 0 && err != nil {
		return 0, err
	}
	if framesRead < framesRequested && err == nil {
		err = io.EOF
	}
	return outBytes, err
}

func convertWAVToS16(dst, src []byte, format wavFormat) {
	channels := int(format.numChannels)
	inFrameBytes := int(format.blockAlign)
	bps := int(format.bitsPerSample)
	inSampleBytes := bps / 8
	out := 0
	for frameStart := 0; frameStart+inFrameBytes <= len(src); frameStart += inFrameBytes {
		for channel := 0; channel < channels; channel++ {
			sampleStart := frameStart + channel*inSampleBytes
			var sample int16
			switch format.audioFormat {
			case 3:
				if bps == 32 && sampleStart+4 <= len(src) {
					f := math.Float32frombits(binary.LittleEndian.Uint32(src[sampleStart:]))
					if f > 1 {
						f = 1
					}
					if f < -1 {
						f = -1
					}
					sample = int16(f * maxS16)
				}
			default:
				switch bps {
				case 8:
					sample = int16((int(src[sampleStart]) - 128) << 8)
				case 16:
					sample = int16(binary.LittleEndian.Uint16(src[sampleStart:]))
				case 24:
					v := int32(src[sampleStart]) | int32(src[sampleStart+1])<<8 | int32(src[sampleStart+2])<<16
					if v&0x800000 != 0 {
						v |= ^0xffffff
					}
					sample = int16(v >> 8)
				case 32:
					sample = int16(int32(binary.LittleEndian.Uint32(src[sampleStart:])) >> 16)
				}
			}
			binary.LittleEndian.PutUint16(dst[out:], uint16(sample))
			out += 2
		}
	}
}

func (s *wavSource) SeekFrame(frame uint64) error {
	if frame > s.lengthFrames {
		frame = s.lengthFrames
	}
	_, err := s.file.Seek(s.dataStart+int64(frame)*int64(s.format.blockAlign), io.SeekStart)
	if err == nil {
		s.posFrames = frame
	}
	return err
}

func (s *wavSource) Close() error       { return s.file.Close() }
func (s *wavSource) Channels() uint32   { return uint32(s.format.numChannels) }
func (s *wavSource) SampleRate() uint32 { return s.format.sampleRate }
func (s *wavSource) LengthFrames() uint64 {
	return s.lengthFrames
}
func (s *wavSource) Position() float64 {
	return float64(s.posFrames) / float64(s.format.sampleRate)
}
func (s *wavSource) Duration() float64 {
	return float64(s.lengthFrames) / float64(s.format.sampleRate)
}

func (s *wavSource) AudioInfo() models.AudioInfo {
	bitRateKbps := 0
	if s.format.byteRate > 0 {
		bitRateKbps = int((uint64(s.format.byteRate)*8 + 500) / 1000)
	}
	return models.AudioInfo{
		Codec:       "WAV",
		BitDepth:    int(s.format.bitsPerSample),
		SampleRate:  int(s.format.sampleRate),
		BitRateKbps: bitRateKbps,
		Channels:    int(s.format.numChannels),
	}
}

type flacSource struct {
	file         *os.File
	stream       *flac.Stream
	posFrames    uint64
	lengthFrames uint64
	sampleRate   uint32
	channels     uint32
	bits         uint8
	buffer       []byte
}

func newFLACSource(file *os.File, _ string) (*flacSource, error) {
	stream, err := flac.NewSeek(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &flacSource{
		file:         file,
		stream:       stream,
		lengthFrames: stream.Info.NSamples,
		sampleRate:   stream.Info.SampleRate,
		channels:     uint32(stream.Info.NChannels),
		bits:         stream.Info.BitsPerSample,
	}, nil
}

func (s *flacSource) Read(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		if len(s.buffer) == 0 {
			if err := s.readFrame(); err != nil {
				if total > 0 && errors.Is(err, io.EOF) {
					return total, nil
				}
				return total, err
			}
		}
		n := copy(p[total:], s.buffer)
		s.buffer = s.buffer[n:]
		total += n
	}
	return total, nil
}

func (s *flacSource) readFrame() error {
	frame, err := s.stream.ParseNext()
	if err != nil {
		return err
	}
	channels := int(s.channels)
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
			sample16 := scaleIntToS16(sample, s.bits)
			binary.LittleEndian.PutUint16(data[out:], uint16(sample16))
			out += 2
		}
	}
	s.buffer = data
	s.posFrames += uint64(blockSize)
	return nil
}

func scaleIntToS16(sample int32, bits uint8) int16 {
	if bits > 16 {
		sample >>= bits - 16
	} else if bits < 16 {
		sample <<= 16 - bits
	}
	if sample > maxS16 {
		sample = maxS16
	}
	if sample < minS16 {
		sample = minS16
	}
	return int16(sample)
}

func (s *flacSource) SeekFrame(frame uint64) error {
	if frame > s.lengthFrames {
		frame = s.lengthFrames
	}
	actual, err := s.stream.Seek(frame)
	if err == nil {
		s.posFrames = actual
		s.buffer = nil
	}
	return err
}

func (s *flacSource) Close() error       { return s.file.Close() }
func (s *flacSource) Channels() uint32   { return s.channels }
func (s *flacSource) SampleRate() uint32 { return s.sampleRate }
func (s *flacSource) LengthFrames() uint64 {
	return s.lengthFrames
}
func (s *flacSource) Position() float64 {
	return float64(s.posFrames) / float64(s.sampleRate)
}
func (s *flacSource) Duration() float64 {
	return float64(s.lengthFrames) / float64(s.sampleRate)
}

func (s *flacSource) AudioInfo() models.AudioInfo {
	return models.AudioInfo{
		Codec:       "FLAC",
		BitDepth:    int(s.bits),
		SampleRate:  int(s.sampleRate),
		BitRateKbps: fileBitRateKbps(s.file, s.Duration()),
		Channels:    int(s.channels),
	}
}

func fileBitRateKbps(file *os.File, duration float64) int {
	if file == nil {
		return 0
	}
	stat, err := file.Stat()
	if err != nil {
		return 0
	}
	return averageBitRateKbps(stat.Size(), duration)
}

func averageBitRateKbps(bytes int64, duration float64) int {
	if bytes <= 0 || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0
	}
	return int(math.Round(float64(bytes) * 8 / duration / 1000))
}
