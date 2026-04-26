package audio

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"

	alac "github.com/mycophonic/saprobe-alac"
	"github.com/xiongnemo/saki/internal/models"
)

const alacDecodeBufferBytes = 32 * 1024

type alacSource struct {
	*alacPCM
	file *os.File
}

type alacStreamDecoder struct {
	*alacPCM
}

type alacPCM struct {
	dec              *alac.Decoder
	format           alac.PCMFormat
	sourceFrameBytes int
	outputFrameBytes int
	lengthFrames     uint64
	posBytes         int64
	raw              []byte
	converted        []byte
	pending          []byte
	pendingErr       error
}

func newALACSource(file *os.File, _ string) (*alacSource, error) {
	decoder, err := alac.NewDecoder(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	pcm, err := newALACPCM(decoder)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &alacSource{alacPCM: pcm, file: file}, nil
}

func newALACStreamDecoder(reader io.ReadSeeker) (*alacStreamDecoder, error) {
	decoder, err := alac.NewDecoder(reader)
	if err != nil {
		return nil, err
	}
	pcm, err := newALACPCM(decoder)
	if err != nil {
		return nil, err
	}
	return &alacStreamDecoder{alacPCM: pcm}, nil
}

func newALACPCM(decoder *alac.Decoder) (*alacPCM, error) {
	format := decoder.Format()
	sourceSampleBytes := alacBytesPerSample(format.BitDepth)
	if format.SampleRate <= 0 || format.Channels <= 0 || sourceSampleBytes == 0 {
		return nil, errors.New("invalid ALAC format")
	}

	sourceFrameBytes := format.Channels * sourceSampleBytes
	outputFrameBytes := format.Channels * 2
	rawSize := alacDecodeBufferBytes - alacDecodeBufferBytes%sourceFrameBytes
	if rawSize < sourceFrameBytes {
		rawSize = sourceFrameBytes
	}

	duration := decoder.Duration()
	var lengthFrames uint64
	if duration > 0 {
		lengthFrames = uint64(duration.Seconds()*float64(format.SampleRate) + 0.5)
	}

	return &alacPCM{
		dec:              decoder,
		format:           format,
		sourceFrameBytes: sourceFrameBytes,
		outputFrameBytes: outputFrameBytes,
		lengthFrames:     lengthFrames,
		raw:              make([]byte, rawSize),
		converted:        make([]byte, 0, rawSize/sourceFrameBytes*outputFrameBytes),
	}, nil
}

func (s *alacPCM) Read(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		if len(s.pending) > 0 {
			n := copy(p, s.pending)
			s.pending = s.pending[n:]
			s.posBytes += int64(n)
			total += n
			p = p[n:]
			continue
		}
		if s.pendingErr != nil {
			err := s.pendingErr
			s.pendingErr = nil
			if total > 0 {
				return total, nil
			}
			return 0, err
		}
		if err := s.fill(); err != nil {
			if total > 0 && errors.Is(err, io.EOF) {
				return total, nil
			}
			return total, err
		}
	}
	return total, nil
}

func (s *alacPCM) fill() error {
	n, err := s.dec.Read(s.raw)
	if n <= 0 {
		return err
	}
	frames := n / s.sourceFrameBytes
	if frames <= 0 {
		return errors.New("ALAC decoder returned a partial frame")
	}
	outBytes := frames * s.outputFrameBytes
	if cap(s.converted) < outBytes {
		s.converted = make([]byte, outBytes)
	}
	s.converted = s.converted[:outBytes]
	convertALACPCMToS16(s.converted, s.raw[:frames*s.sourceFrameBytes], s.format.BitDepth, s.format.Channels)
	s.pending = s.converted
	s.pendingErr = err
	return nil
}

func (s *alacPCM) SeekFrame(frame uint64) error {
	_, err := s.seekFrame(frame)
	return err
}

func (s *alacPCM) seekFrame(frame uint64) (uint64, error) {
	if s.lengthFrames > 0 && frame > s.lengthFrames {
		frame = s.lengthFrames
	}
	target := time.Duration(float64(frame) / float64(s.format.SampleRate) * float64(time.Second))
	actual, err := s.dec.Seek(target)
	if err != nil {
		return 0, err
	}
	actualFrames := uint64(actual.Seconds()*float64(s.format.SampleRate) + 0.5)
	s.posBytes = int64(actualFrames) * int64(s.outputFrameBytes)
	s.pending = nil
	s.pendingErr = nil
	return actualFrames, nil
}

func (s *alacSource) Close() error { return s.file.Close() }

func (s *alacStreamDecoder) SeekFrame(frame uint64) (uint64, error) {
	return s.seekFrame(frame)
}

func (s *alacPCM) Channels() uint32 { return uint32(s.format.Channels) }

func (s *alacPCM) SampleRate() uint32 { return uint32(s.format.SampleRate) }

func (s *alacPCM) LengthFrames() uint64 { return s.lengthFrames }

func (s *alacPCM) Position() float64 {
	if s.format.SampleRate <= 0 || s.outputFrameBytes <= 0 {
		return 0
	}
	frames := s.posBytes / int64(s.outputFrameBytes)
	return float64(frames) / float64(s.format.SampleRate)
}

func (s *alacPCM) Duration() float64 {
	if s.format.SampleRate <= 0 || s.lengthFrames == 0 {
		return 0
	}
	return float64(s.lengthFrames) / float64(s.format.SampleRate)
}

func (s *alacPCM) AudioInfo() models.AudioInfo {
	return models.AudioInfo{
		Codec:      "ALAC",
		BitDepth:   s.format.BitDepth,
		SampleRate: s.format.SampleRate,
		Channels:   s.format.Channels,
	}
}

func (s *alacSource) AudioInfo() models.AudioInfo {
	info := s.alacPCM.AudioInfo()
	info.BitRateKbps = fileBitRateKbps(s.file, s.Duration())
	return info
}

func isMP4Header(header []byte) bool {
	return len(header) >= 8 && string(header[4:8]) == "ftyp"
}

func alacBytesPerSample(bitDepth int) int {
	switch bitDepth {
	case 16:
		return 2
	case 20, 24:
		return 3
	case 32:
		return 4
	default:
		return 0
	}
}

func convertALACPCMToS16(dst, src []byte, bitDepth int, channels int) {
	sourceSampleBytes := alacBytesPerSample(bitDepth)
	if sourceSampleBytes == 0 || channels <= 0 {
		return
	}
	sourceFrameBytes := sourceSampleBytes * channels
	outputFrameBytes := channels * 2
	frames := len(src) / sourceFrameBytes
	for frame := 0; frame < frames; frame++ {
		srcFrame := src[frame*sourceFrameBytes:]
		dstFrame := dst[frame*outputFrameBytes:]
		for channel := 0; channel < channels; channel++ {
			srcSample := srcFrame[channel*sourceSampleBytes:]
			var sample int16
			switch bitDepth {
			case 16:
				sample = int16(binary.LittleEndian.Uint16(srcSample))
			case 20, 24:
				v := int32(srcSample[0]) | int32(srcSample[1])<<8 | int32(srcSample[2])<<16
				if v&0x800000 != 0 {
					v |= ^0xffffff
				}
				sample = int16(v >> 8)
			case 32:
				sample = int16(int32(binary.LittleEndian.Uint32(srcSample)) >> 16)
			}
			binary.LittleEndian.PutUint16(dstFrame[channel*2:], uint16(sample))
		}
	}
}
