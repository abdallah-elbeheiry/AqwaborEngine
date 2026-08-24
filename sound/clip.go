package sound

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/go-mp3"
)

// Clip is a cached, decoded audio asset owned by a Context. It is created via
// Context.LoadAudio or Context.LoadAudioFile and is not itself audible; play
// it by creating one or more Players.
//
// A Clip holds a default volume ("clipVolume") applied to every new Player.
// Clips are reference-counted by the active Players they spawn; after the
// owning Context is closed, Clip methods fail with ErrClosed.
type Clip struct {
	ctx    *Context
	data   []byte
	format formatKind
	id     string

	// metadata captured at load time (best-effort, for logging/diagnostics).
	sampleRate int
	channels   int
	duration   time.Duration

	mu      sync.Mutex
	volume  float64
	players map[*Player]struct{}
}

// probe decodes the container header and returns stream metadata. It also
// validates the format, so a non-nil error means the clip cannot be played.
func probe(data []byte, f formatKind) (sr, ch int, dur time.Duration, err error) {
	switch f {
	case formatWAV:
		d, e := decodeWAV(data)
		if e != nil {
			return 0, 0, 0, e
		}
		return d.SampleRate(), d.Channels(), d.Duration(), nil
	case formatMP3:
		d, e := mp3.NewDecoder(bytes.NewReader(data))
		if e != nil {
			return 0, 0, 0, e
		}
		sr = d.SampleRate()
		ch = 2 // go-mp3 always outputs 2-channel (stereo) PCM
		dur = mp3Duration(data)
		return sr, ch, dur, nil
	default:
		return 0, 0, 0, nil
	}
}

// LoadAudio decodes and caches the given audio bytes. Identical bytes yield
// the same *Clip (keyed by SHA-256), so repeated loads are cheap.
func (c *Context) LoadAudio(data []byte) (*Clip, error) {
	format, ok := detectFormat(data)
	if !ok {
		log.Error("unsupported audio format", "bytes", len(data))
		return nil, fmt.Errorf("%w: could not detect container", ErrUnsupportedFormat)
	}
	key := sha256Hex(data)
	return c.buildAndRegister(c.clipsData, key, "data:"+key[:16], data, format)
}

// LoadAudioFile reads and decodes the audio file at path, caching by the
// normalised path. The same normalised path yields the same *Clip.
func (c *Context) LoadAudioFile(path string) (*Clip, error) {
	norm := filepath.Clean(path)

	data, err := os.ReadFile(norm)
	if err != nil {
		log.Error("read audio file failed", "err", err, "path", norm)
		return nil, fmt.Errorf("sound: read file %q: %w", norm, err)
	}

	format, ok := detectFormat(data)
	if !ok {
		// Fall back to extension so a valid file with an unusual/ID3-less
		// header still decodes.
		switch strings.ToLower(filepath.Ext(norm)) {
		case ".wav":
			format, ok = formatWAV, true
		case ".mp3":
			format, ok = formatMP3, true
		}
	}
	if !ok {
		log.Error("unsupported audio format", "path", norm)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, norm)
	}
	return c.buildAndRegister(c.clipsFile, norm, "file:"+norm, data, format)
}

// buildAndRegister probes, constructs and caches a Clip under key in the given
// cache map. It is safe to call concurrently and tolerates the Context having
// been closed (or the clip having appeared in the cache) since the initial
// lookup.
func (c *Context) buildAndRegister(cache map[string]*Clip, key, id string, data []byte, format formatKind) (*Clip, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	if cl, ok := cache[key]; ok {
		c.mu.Unlock()
		log.Debug("clip cache hit",
			"id", cl.id, "format", cl.format.String(),
			"sampleRate", cl.sampleRate, "channels", cl.channels, "duration", cl.duration)
		return cl, nil
	}
	c.mu.Unlock()

	sr, ch, dur, err := probe(data, format)
	if err != nil {
		log.Error("decode failed", "err", err, "format", format.String(), "id", id)
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedFormat, err)
	}

	cl := &Clip{
		ctx:        c,
		data:       data,
		format:     format,
		id:         id,
		sampleRate: sr,
		channels:   ch,
		duration:   dur,
		volume:     1.0,
		players:    make(map[*Player]struct{}),
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	if existing, ok := cache[key]; ok {
		c.mu.Unlock()
		return existing, nil
	}
	cache[key] = cl
	c.mu.Unlock()

	log.Debug("clip loaded",
		"id", cl.id, "format", format.String(),
		"sampleRate", sr, "channels", ch, "duration", dur,
		"bytes", len(data), "cache", "miss")
	return cl, nil
}

// SetVolume sets the clip's default gain, clamped to [0, 1]. It is applied to
// every Player created afterwards and re-applied to currently active Players
// of this clip.
func (c *Clip) SetVolume(v float64) {
	c.mu.Lock()
	if c.ctx.isClosed() {
		c.mu.Unlock()
		return
	}
	v = clamp01(v)
	c.volume = v
	players := make([]*Player, 0, len(c.players))
	for p := range c.players {
		players = append(players, p)
	}
	c.mu.Unlock()

	log.Debug("clip volume changed", "id", c.id, "value", v, "activePlayers", len(players))

	for _, p := range players {
		p.applyEffective()
	}
}

// Volume returns the clip's default gain.
func (c *Clip) Volume() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.volume
}

// SampleRate returns the clip's sample rate in Hz (0 if unknown).
func (c *Clip) SampleRate() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sampleRate
}

// Channels returns the clip's channel count (0 if unknown).
func (c *Clip) Channels() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.channels
}

// Duration returns the clip's playback duration (0 if unknown).
func (c *Clip) Duration() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.duration
}

// isClosed reports whether the owning Context is closed.
func (c *Context) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// registerPlayer adds p to this clip's active set.
func (c *Clip) registerPlayer(p *Player) {
	c.mu.Lock()
	if c.players != nil {
		c.players[p] = struct{}{}
	}
	c.mu.Unlock()
}

// removePlayer drops p from this clip's active set.
func (c *Clip) removePlayer(p *Player) {
	c.mu.Lock()
	if c.players != nil {
		delete(c.players, p)
	}
	c.mu.Unlock()
}

// volumeValue returns the clip default gain under lock.
func (c *Clip) volumeValue() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.volume
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
