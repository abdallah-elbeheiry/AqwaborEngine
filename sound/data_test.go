package sound

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"testing"
)

func openTestMP3() (io.Reader, error) {
	for _, p := range []string{
		"../cmd/aqwabor/song-example.mp3",
		"../cmd/audio-only/song-example.mp3",
	} {
		f, err := os.Open(p)
		if err == nil {
			return f, nil
		}
	}
	return nil, os.ErrNotExist
}

// makeStereoWAV builds an in-memory 16-bit stereo WAV sine wave at the given
// rate (1 second long). It is local to this file so it does not clash with the
// mono helper of the same name in sound_test.go.
func makeStereoWAV(rate int) []byte {
	const seconds = 1
	const ch = 2
	n := rate * seconds * ch
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+4*n))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1)) // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(ch))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(rate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(rate*ch*2)) // byte rate
	_ = binary.Write(&buf, binary.LittleEndian, uint16(ch*2))      // block align
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))        // bits/sample
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4*n))

	for i := 0; i < n; i++ {
		s := math.Sin(2 * math.Pi * 440 * float64(i) / float64(rate))
		v := int16(s * 30000)
		_ = binary.Write(&buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}

func TestMixerProducesAudio(t *testing.T) {
	rate := 44100
	src, err := newSource(makeStereoWAV(rate), formatWAV, false)
	if err != nil {
		t.Fatalf("newSource: %v", err)
	}
	v := newVoice(src)
	m := newMixer()
	m.AddVoice(v)

	out := make([]float32, rate) // 1 second of mono-interleaved samples
	peak := float32(0)
	for i := 0; i < 4; i++ {
		m.Mix(out)
		for _, s := range out {
			a := s
			if a < 0 {
				a = -a
			}
			if a > peak {
				peak = a
			}
		}
	}
	t.Logf("peak mixed amplitude = %v", peak)
	if peak == 0 {
		t.Fatal("mixer produced only silence (data path broken)")
	}
}

// TestMP3Source verifies an MP3 clip yields non-zero float samples.
func TestMP3Source(t *testing.T) {
	f, err := openTestMP3()
	if err != nil {
		t.Skipf("no test mp3: %v", err)
	}
	if c, ok := f.(io.Closer); ok {
		defer c.Close()
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read mp3: %v", err)
	}
	src, err := newSource(data, formatMP3, false)
	if err != nil {
		t.Fatalf("newSource mp3: %v", err)
	}
	buf := make([]byte, 4096)
	total := 0
	nonzero := 0
	for {
		n, rerr := src.Read(buf)
		total += n
		for i := 0; i+3 < n; i += 4 {
			fv := math.Float32frombits(binary.LittleEndian.Uint32(buf[i:]))
			if fv != 0 {
				nonzero++
			}
		}
		if rerr != nil {
			break
		}
	}
	t.Logf("mp3 read %d bytes, %d nonzero floats", total, nonzero)
	if total == 0 || nonzero == 0 {
		t.Fatal("mp3 source produced no audio data")
	}
}
