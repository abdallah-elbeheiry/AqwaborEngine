package sound

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"
)

const (
	wavFormatPCM   = 1
	wavFormatFloat = 3
)

// wavDecoder reads WAV data and produces interleaved float32 PCM samples via
// the io.Reader interface. It supports PCM 8/16/24/32-bit integer and IEEE
// 32-bit float formats.
type wavDecoder struct {
	data          []byte
	pos           int
	sampleRate    int
	channels      int
	bitsPerSample int
	audioFormat   int
	dataStart     int
	dataEnd       int
}

// decodeWAV parses a WAV byte slice and returns a decoder producing interleaved
// float32 PCM through its Read method.
func decodeWAV(data []byte) (*wavDecoder, error) {
	if len(data) < 12 {
		return nil, errors.New("audio: WAV data too short for RIFF header")
	}
	if string(data[0:4]) != "RIFF" {
		return nil, errors.New("audio: missing RIFF header")
	}
	if string(data[8:12]) != "WAVE" {
		return nil, errors.New("audio: missing WAVE format identifier")
	}

	d := &wavDecoder{data: data}

	var foundFmt, foundData bool
	offset := 12
	for offset+8 <= len(data) {
		chunkID := string(data[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		chunkDataStart := offset + 8

		cs := chunkSize
		if chunkDataStart+cs > len(data) {
			cs = len(data) - chunkDataStart
		}

		switch chunkID {
		case "fmt ":
			if err := d.parseFmtChunk(data[chunkDataStart : chunkDataStart+cs]); err != nil {
				return nil, err
			}
			foundFmt = true
		case "data":
			d.dataStart = chunkDataStart
			d.dataEnd = chunkDataStart + cs
			d.pos = 0
			foundData = true
		}

		offset = chunkDataStart + chunkSize
		if chunkSize%2 != 0 {
			offset++
		}
	}

	if !foundFmt {
		return nil, errors.New("audio: missing fmt chunk")
	}
	if !foundData {
		return nil, errors.New("audio: missing data chunk")
	}
	return d, nil
}

func (d *wavDecoder) parseFmtChunk(chunk []byte) error {
	if len(chunk) < 16 {
		return errors.New("audio: fmt chunk too short")
	}
	d.audioFormat = int(binary.LittleEndian.Uint16(chunk[0:2]))
	d.channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
	d.sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
	d.bitsPerSample = int(binary.LittleEndian.Uint16(chunk[14:16]))

	switch d.audioFormat {
	case wavFormatPCM:
		switch d.bitsPerSample {
		case 8, 16, 24, 32:
		default:
			return fmt.Errorf("audio: unsupported PCM bit depth %d", d.bitsPerSample)
		}
	case wavFormatFloat:
		if d.bitsPerSample != 32 {
			return fmt.Errorf("audio: float WAV must be 32-bit, got %d", d.bitsPerSample)
		}
	default:
		return fmt.Errorf("audio: unsupported audio format %d (expected PCM=1 or Float=3)", d.audioFormat)
	}
	if d.channels < 1 {
		return errors.New("audio: channel count must be at least 1")
	}
	if d.sampleRate < 1 {
		return errors.New("audio: sample rate must be at least 1")
	}
	return nil
}

// Read produces interleaved float32 PCM samples. len(p) must be a multiple of
// 4; each 4 bytes in p receive one float32 sample in little-endian format.
func (d *wavDecoder) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	bytesPerInputSample := d.bitsPerSample / 8
	bytesPerOutputSample := 4 // float32

	written := 0
	for written+bytesPerOutputSample <= len(p) {
		absPos := d.dataStart + d.pos
		if absPos+bytesPerInputSample > d.dataEnd {
			return written, io.EOF
		}
		sample := d.convertSample(d.data[absPos : absPos+bytesPerInputSample])
		binary.LittleEndian.PutUint32(p[written:], math.Float32bits(sample))
		d.pos += bytesPerInputSample
		written += bytesPerOutputSample
	}
	return written, nil
}

func (d *wavDecoder) convertSample(raw []byte) float32 {
	switch {
	case d.audioFormat == wavFormatFloat && d.bitsPerSample == 32:
		bits := binary.LittleEndian.Uint32(raw)
		return math.Float32frombits(bits)

	case d.audioFormat == wavFormatPCM && d.bitsPerSample == 8:
		return (float32(raw[0]) - 128.0) / 128.0

	case d.audioFormat == wavFormatPCM && d.bitsPerSample == 16:
		v := int16(binary.LittleEndian.Uint16(raw))
		return float32(v) / 32768.0

	case d.audioFormat == wavFormatPCM && d.bitsPerSample == 24:
		v := int32(raw[0]) | int32(raw[1])<<8 | int32(raw[2])<<16
		if v&0x800000 != 0 {
			v |= ^0xFFFFFF
		}
		return float32(v) / 8388608.0

	case d.audioFormat == wavFormatPCM && d.bitsPerSample == 32:
		v := int32(binary.LittleEndian.Uint32(raw))
		return float32(v) / 2147483648.0

	default:
		return 0
	}
}

// SampleRate returns the WAV file's sample rate in Hz.
func (d *wavDecoder) SampleRate() int { return d.sampleRate }

// Channels returns the number of audio channels.
func (d *wavDecoder) Channels() int { return d.channels }

// Duration returns the total duration of the audio data.
func (d *wavDecoder) Duration() time.Duration {
	totalBytes := d.dataEnd - d.dataStart
	bytesPerSample := d.bitsPerSample / 8
	if bytesPerSample == 0 || d.channels == 0 || d.sampleRate == 0 {
		return 0
	}
	totalFrames := totalBytes / (bytesPerSample * d.channels)
	return time.Duration(totalFrames) * time.Second / time.Duration(d.sampleRate)
}

// Reset rewinds the decoder to the beginning of the audio data.
func (d *wavDecoder) Reset() {
	d.pos = 0
}
