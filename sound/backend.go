package sound

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"time"

	"github.com/hajimehoshi/go-mp3"
)

// formatKind identifies a decoded audio container.
type formatKind int

const (
	formatWAV formatKind = iota
	formatMP3
)

func (f formatKind) String() string {
	switch f {
	case formatWAV:
		return "wav"
	case formatMP3:
		return "mp3"
	default:
		return "unknown"
	}
}

// detectFormat inspects the leading bytes of data to choose a decoder.
// WAV is identified by its RIFF/WAVE header; MP3 by an ID3v2 tag or an MPEG
// frame sync word (0xFF followed by 0b111xxxxx).
func detectFormat(data []byte) (formatKind, bool) {
	if len(data) >= 12 &&
		bytes.Equal(data[0:4], []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte("WAVE")) {
		return formatWAV, true
	}
	if len(data) >= 3 && bytes.Equal(data[0:3], []byte("ID3")) {
		return formatMP3, true
	}
	if len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0 {
		return formatMP3, true
	}
	return 0, false
}

// validateDecode performs a cheap up-front decode so format/codec errors
// surface at Load time rather than at first Play.
func validateDecode(data []byte, f formatKind) error {
	switch f {
	case formatWAV:
		_, err := decodeWAV(data)
		return err
	case formatMP3:
		_, err := mp3.NewDecoder(bytes.NewReader(data))
		return err
	default:
		return nil
	}
}

// pcmSource adapts a decoded clip into an io.Reader that produces interleaved
// float32 PCM samples encoded as little-endian bytes (the layout consumed by
// the oto player). When loop is true it restarts the stream at EOF instead
// of ending.
type pcmSource struct {
	data   []byte
	format formatKind
	loop   bool

	wdec *wavDecoder
	mdec *mp3.Decoder
	mtmp []byte
}

func newSource(data []byte, f formatKind, loop bool) (*pcmSource, error) {
	s := &pcmSource{data: data, format: f, loop: loop}
	if err := s.reset(); err != nil {
		return nil, err
	}
	return s, nil
}

// reset rebuilds the underlying decoder from the start of the clip.
func (s *pcmSource) reset() error {
	switch s.format {
	case formatWAV:
		d, err := decodeWAV(s.data)
		if err != nil {
			return err
		}
		s.wdec = d
	case formatMP3:
		d, err := mp3.NewDecoder(bytes.NewReader(s.data))
		if err != nil {
			return err
		}
		s.mdec = d
	}
	return nil
}

func (s *pcmSource) Read(p []byte) (int, error) {
	switch s.format {
	case formatWAV:
		return s.readWAV(p)
	case formatMP3:
		return s.readMP3(p)
	default:
		return 0, io.EOF
	}
}

func (s *pcmSource) readWAV(p []byte) (int, error) {
	n, err := s.wdec.Read(p)
	if err == io.EOF && s.loop {
		s.wdec.Reset()
		return s.wdec.Read(p)
	}
	return n, err
}

// readMP3 pulls 16-bit signed little-endian PCM from go-mp3 and converts each
// sample to a float32 (divided by 32768) in little-endian byte layout.
func (s *pcmSource) readMP3(p []byte) (int, error) {
	need := len(p) / 2 // int16 bytes required for len(p) float32 bytes
	if cap(s.mtmp) < need {
		s.mtmp = make([]byte, need)
	}
	buf := s.mtmp[:need]

	total := 0
	for total < need {
		n, err := s.mdec.Read(buf[total:])
		total += n
		if err == io.EOF {
			if s.loop {
				if e := s.reset(); e != nil {
					return 0, e
				}
				continue
			}
			break
		}
		if err != nil {
			break
		}
	}

	if total == 0 {
		return 0, io.EOF
	}

	samples := total / 2
	for i := range samples {
		v := int16(binary.LittleEndian.Uint16(s.mtmp[i*2:]))
		f := float32(v) / 32768.0
		binary.LittleEndian.PutUint32(p[i*4:], math.Float32bits(f))
	}

	if total < need {
		return samples * 4, io.EOF
	}
	return samples * 4, nil
}

// mp3Duration returns the playback duration of an MP3 by walking its frame
// headers (summing per-frame sample counts). It does not decode audio, so it
// is cheap and works for VBR. Returns 0 if the stream cannot be parsed.
func mp3Duration(data []byte) time.Duration {
	off := 0
	if len(data) >= 10 && bytes.Equal(data[0:3], []byte("ID3")) {
		off = 10 + id3Size(data[6:10])
	}

	var (
		totalSamples int64
		sampleRate   int
	)
	for off+4 <= len(data) {
		if data[off] != 0xFF || data[off+1]&0xE0 != 0xE0 {
			off++
			continue
		}
		b1, b2 := data[off+1], data[off+2]
		version := (b1 >> 3) & 0x03
		layer := (b1 >> 1) & 0x03
		brIdx := (b2 >> 4) & 0x0F
		srIdx := (b2 >> 2) & 0x03
		padding := (b2 >> 1) & 0x01

		if layer != 0x01 { // layer III only (what go-mp3 emits)
			return 0
		}

		var samplesPerFrame int
		switch version {
		case 0x03: // MPEG1
			sampleRate = []int{44100, 48000, 32000}[srIdx]
			samplesPerFrame = 1152
		case 0x02: // MPEG2
			sampleRate = []int{22050, 24000, 16000}[srIdx]
			samplesPerFrame = 576
		case 0x00: // MPEG2.5
			sampleRate = []int{11025, 12000, 8000}[srIdx]
			samplesPerFrame = 576
		default:
			return 0
		}

		bitrate := mp3Bitrate(int(version), int(brIdx)) // kbps
		if sampleRate == 0 || bitrate == 0 {
			return 0
		}

		frameLen := (samplesPerFrame/8*bitrate*1000)/sampleRate + int(padding)
		if frameLen <= 0 {
			return 0
		}

		totalSamples += int64(samplesPerFrame)
		off += frameLen
	}

	if totalSamples == 0 || sampleRate == 0 {
		return 0
	}
	return time.Duration(totalSamples) * time.Second / time.Duration(sampleRate)
}

// id3Size decodes a 28-bit syncsafe integer (ID3v2 size field).
func id3Size(b []byte) int {
	if len(b) < 4 {
		return 0
	}
	return ((int(b[0]) & 0x7F) << 21) | ((int(b[1]) & 0x7F) << 14) |
		((int(b[2]) & 0x7F) << 7) | (int(b[3]) & 0x7F)
}

// mp3Bitrate returns the frame bitrate in kbps for layer III.
func mp3Bitrate(version, idx int) int {
	if idx == 0 || idx == 15 {
		return 0
	}
	if version == 0x03 { // MPEG1
		return []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}[idx]
	}
	// MPEG2 / MPEG2.5
	return []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}[idx]
}
