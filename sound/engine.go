package sound

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
)

// voice is one playback instance in the mixer. It pulls interleaved float32
// PCM (little-endian) from a source reader and exposes a per-voice volume.
//
// A voice only ever advances its source while it is playing (not paused, not
// stopped, not finished). Looping sources restart at EOF; one-shot sources
// mark themselves done and are reaped by the mixer.
type voice struct {
	mu sync.Mutex

	src io.Reader
	vol float32

	playing bool
	paused  bool
	done    bool

	rbuf []byte // scratch for reading float32-LE bytes from src
}

func newVoice(src io.Reader) *voice {
	return &voice{src: src, vol: 1, playing: true}
}

func (v *voice) Play() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.done {
		v.playing = true
		v.paused = false
	}
}

func (v *voice) Pause() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.playing && !v.done {
		v.paused = true
	}
}

func (v *voice) Resume() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.playing && v.paused && !v.done {
		v.paused = false
	}
}

func (v *voice) Stop() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.playing = false
	v.paused = false
	v.done = true
}

func (v *voice) SetVolume(f float32) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if f < 0 {
		f = 0
	} else if f > 1 {
		f = 1
	}
	v.vol = f
}

func (v *voice) Volume() float32 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.vol
}

// readSamples fills dst with float32 samples read from src. Returns the number
// of samples written; marks the voice done on EOF. Caller must NOT hold v.mu.
func (v *voice) readSamples(dst []float32) int {
	need := len(dst) * 4
	v.mu.Lock()
	if v.done || v.src == nil {
		v.mu.Unlock()
		return 0
	}
	if len(v.rbuf) < need {
		v.rbuf = make([]byte, need)
	}
	src := v.src
	v.mu.Unlock()

	n, err := io.ReadFull(src, v.rbuf[:need])
	samples := n / 4
	for i := 0; i < samples; i++ {
		bits := binary.LittleEndian.Uint32(v.rbuf[i*4:])
		dst[i] = math.Float32frombits(bits)
	}

	if err != nil {
		v.mu.Lock()
		v.done = true
		v.playing = false
		v.mu.Unlock()
	}
	return samples
}

// mixer sums all active voices, applying a master volume, and clamps to [-1,1].
type mixer struct {
	mu      sync.Mutex
	voices  []*voice
	master  float64
	scratch []float32 // per-voice accumulation buffer
}

func newMixer() *mixer {
	return &mixer{master: 1}
}

func (m *mixer) AddVoice(v *voice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.voices = append(m.voices, v)
}

func (m *mixer) RemoveVoice(v *voice) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.voices {
		if s == v {
			m.voices = append(m.voices[:i], m.voices[i+1:]...)
			return
		}
	}
}

func (m *mixer) SetMasterVolume(f float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f < 0 {
		f = 0
	} else if f > 1 {
		f = 1
	}
	m.master = f
}

func (m *mixer) MasterVolume() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.master
}

// Mix fills out with the mixed, master-scaled output. Each active voice is
// accumulated additively into its own scratch region so voices do not clobber
// one another, then the sum is scaled by the master volume and clamped.
func (m *mixer) Mix(out []float32) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range out {
		out[i] = 0
	}

	if cap(m.scratch) < len(out) {
		m.scratch = make([]float32, len(out))
	}
	master := float32(m.master)

	// Build the survivor list in place (reusing the backing array). Voices that
	// are still playing are kept; finished one-shot voices are dropped so the
	// mixer does not grow without bound across many Play/Stop cycles. Mix holds
	// m.mu for the whole call, so no voice is added/removed concurrently here.
	alive := m.voices[:0]
	for _, v := range m.voices {
		v.mu.Lock()
		finished := v.done
		if !v.playing || v.paused {
			v.mu.Unlock()
			alive = append(alive, v)
			continue
		}
		vol := v.vol
		v.mu.Unlock()

		scratch := m.scratch[:len(out)]
		n := v.readSamples(scratch)
		for i := 0; i < n; i++ {
			out[i] += scratch[i] * vol
		}
		if !finished {
			alive = append(alive, v)
		}
	}
	m.voices = alive

	for i := range out {
		s := out[i] * master
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		out[i] = s
	}
}

// engine owns the mixer and the OS output device. The device pulls mixed
// float32 frames through the mixer (no extra goroutine needed for real
// devices; the null device runs its own pull loop to keep voices advancing).
type engine struct {
	sr  int
	ch  int
	mix *mixer
	dev outputDevice
}

func newEngine(sr, ch int, silent bool) (*engine, error) {
	m := newMixer()
	dev, err := newOutputDevice(sr, ch, m.Mix, silent)
	if err != nil {
		return nil, err
	}
	return &engine{sr: sr, ch: ch, mix: m, dev: dev}, nil
}

func (e *engine) NewPlayer(src io.Reader) *voice {
	v := newVoice(src)
	e.mix.AddVoice(v)
	return v
}

func (e *engine) SetMasterVolume(f float64) { e.mix.SetMasterVolume(f) }
func (e *engine) MasterVolume() float64     { return e.mix.MasterVolume() }

func (e *engine) RemoveVoice(v *voice) { e.mix.RemoveVoice(v) }

func (e *engine) isNull() bool { return e.dev.isNull() }

func (e *engine) Close() error {
	return e.dev.Close()
}

// outputDevice consumes mixed float32 frames. Real devices pull frames
// themselves (via the pull callback passed to newOutputDevice); the null
// device discards them while still advancing voices.
type outputDevice interface {
	Close() error
	isNull() bool
}

// pullFunc is invoked by a device to obtain the next block of interleaved
// float32 samples. It must fill out entirely (the mixer always produces a full
// block) and never returns EOF, so playback continues until the device closes.
type pullFunc func(out []float32)

// newOutputDevice builds the appropriate output device. When silent is true it
// always returns the null device. Otherwise it attempts the platform's real
// backend and falls back to the null device if no audio device is available.
func newOutputDevice(sr, ch int, pull pullFunc, silent bool) (outputDevice, error) {
	if silent {
		return newNullDevice(sr, ch, pull), nil
	}
	dev, err := openRealDevice(sr, ch, pull)
	if err != nil {
		log.Warn("real audio device unavailable, using null driver",
			"err", err, "sampleRate", sr, "channels", ch)
		return newNullDevice(sr, ch, pull), nil
	}
	return dev, nil
}
