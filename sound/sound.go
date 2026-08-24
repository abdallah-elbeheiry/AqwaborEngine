// Package sound provides a small, engine-style audio API with a custom mixer
// and pure-Go (no CGO) output backends.
//
// # Audio model
//
// The API follows a three-tier ownership model:
//
//   - Context: the process-level audio device owner. Created once via New.
//     Holds the master volume and the clip cache. Closing it tears down the
//     device and invalidates every Clip/Player it created.
//   - Clip: a cached, decoded audio asset owned by a Context. Loading the
//     same bytes (or the same file path) returns the same Clip. A Clip is
//     not itself audible; it is played by creating one or more Players.
//   - Player: a single playback instance. Multiple Players can play the same
//     Clip concurrently (overlapping sound effects). Each has its own volume
//     and can be stopped, paused and resumed independently.
//
// # Volume
//
// The effective gain applied to a Player is:
//
//	effective = masterVolume * clipVolume * playerVolume
//
// where each factor is clamped to [0, 1]. A master volume of 0 produces
// silence regardless of the other factors. The master gain is applied by the
// mixer; the per-voice gain folds in the clip and player factors.
//
// Formats (v1)
//
// WAV is fully supported by the built-in decoder. MP3 is supported through the
// pure-Go go-mp3 decoder. Other formats are rejected with ErrUnsupportedFormat.
//
// # Backends
//
// The output backend is selected per platform: oto/v3 on Windows and macOS, and
// the pure-Go jfreymuth/pulse on Linux. Pulse is the Linux default because
// oto's cgo/ALSA backend cannot be linked into the same binary as the wgpu
// graphics stack (both declare //go:cgo_import_dynamic for libc symbols — Go
// issue #50295). A build that does not link wgpu can use oto on Linux via the
// "otoaudio" build tag. Whenever the real device is unavailable (or WithSilent
// is set) a null driver is used that advances playback without producing sound.
//
// Concurrency / lifecycle
//
// Context, Clip and Player are safe for concurrent use. After Context.Close,
// Clip and Player methods fail cleanly with ErrClosed.
package sound

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// log is the sound subsystem logger, pre-tagged with its component.
var log = logx.With("component", "sound")

// playerSeq assigns a monotonically increasing id to every Player for log
// correlation (Play/Stop/Pause reference the same id).
var playerSeq atomic.Uint64

// Package-level errors.
var (
	// ErrUnsupportedFormat is returned when LoadAudio/LoadAudioFile cannot
	// recognise (or decode) the supplied bytes.
	ErrUnsupportedFormat = errors.New("unsupported audio format")

	// ErrNotSupported is returned by operations the active backend cannot
	// perform. The current backends support pause/resume, so it is currently
	// unused, but it is part of the public surface for forward compatibility.
	ErrNotSupported = errors.New("sound: operation not supported")

	// ErrClosed is returned by Clip/Player methods after the owning Context
	// has been closed.
	ErrClosed = errors.New("sound: context closed")
)

// Context owns the audio device, the master volume and the clip cache.
// Typically one Context exists per application.
type Context struct {
	mu sync.Mutex

	backend *engine

	closed bool

	clipsFile map[string]*Clip
	clipsData map[string]*Clip

	players map[*Player]struct{}
}

// Option configures a Context during construction.
type Option func(*config)

type config struct {
	silent     bool
	sampleRate int
	channels   int
	volume     float64
	hasVolume  bool
}

// WithSilent forces the null (no-output) driver. This is ideal for tests and
// headless environments where no audio device is available, and guarantees
// playback never touches real hardware.
func WithSilent(silent bool) Option {
	return func(c *config) { c.silent = silent }
}

// WithSampleRate sets the audio sample rate in Hz (default 44100).
func WithSampleRate(rate int) Option {
	return func(c *config) { c.sampleRate = rate }
}

// WithChannels sets the number of output channels (default 2 = stereo).
func WithChannels(ch int) Option {
	return func(c *config) { c.channels = ch }
}

// WithVolume sets the initial master volume, clamped to [0, 1] (default 1).
func WithVolume(v float64) Option {
	return func(c *config) { c.volume = v; c.hasVolume = true }
}

// New opens the audio device and returns a ready Context. If the device cannot
// be opened the error is returned (the null fallback is used when no real
// device is available, so this rarely fails).
func New(opts ...Option) (*Context, error) {
	cfg := config{}
	for _, o := range opts {
		o(&cfg)
	}

	sr := cfg.sampleRate
	if sr <= 0 {
		sr = 44100
	}
	ch := cfg.channels
	if ch <= 0 {
		ch = 2
	}

	backend, err := newEngine(sr, ch, cfg.silent)
	if err != nil {
		log.Error("open audio engine failed", "err", err)
		return nil, fmt.Errorf("sound: open context: %w", err)
	}
	if cfg.hasVolume {
		backend.SetMasterVolume(cfg.volume)
	}

	c := &Context{
		backend:   backend,
		clipsFile: make(map[string]*Clip),
		clipsData: make(map[string]*Clip),
		players:   make(map[*Player]struct{}),
	}

	log.Debug("context opened",
		"silent", cfg.silent || backend.isNull(),
		"sampleRate", sr,
		"channels", ch,
		"master", backend.MasterVolume())

	return c, nil
}

// Close shuts down the device, stops every active Player and clears the clip
// cache. It is safe to call more than once.
func (c *Context) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true

	players := make([]*Player, 0, len(c.players))
	for p := range c.players {
		players = append(players, p)
	}
	clips := len(c.clipsFile) + len(c.clipsData)
	c.clipsFile = nil
	c.clipsData = nil
	c.players = nil
	c.mu.Unlock()

	for _, p := range players {
		p.voice.Stop()
	}

	log.Debug("context closing",
		"players", len(players), "cachedClips", clips, "master", c.backend.MasterVolume())

	err := c.backend.Close()
	if err != nil {
		log.Error("close device failed", "err", err)
		return err
	}
	log.Debug("context closed", "playersStopped", len(players))
	return nil
}

// SetMasterVolume sets the master gain, clamped to [0, 1]. The mixer applies it
// to every active Player automatically, so no per-Player re-application is
// needed.
func (c *Context) SetMasterVolume(v float64) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	v = clamp01(v)
	c.backend.SetMasterVolume(v)
	log.Debug("master volume changed", "value", v)
}

// MasterVolume returns the current master gain.
func (c *Context) MasterVolume() float64 {
	return c.backend.MasterVolume()
}

// masterVolume returns the current master gain under lock.
func (c *Context) masterVolume() float64 {
	return c.backend.MasterVolume()
}

// registerPlayer tracks an active Player for master-volume updates.
func (c *Context) registerPlayer(p *Player) {
	c.mu.Lock()
	if c.players != nil {
		c.players[p] = struct{}{}
	}
	c.mu.Unlock()
}

// removePlayer drops a Player from the active set.
func (c *Context) removePlayer(p *Player) {
	c.mu.Lock()
	if c.players != nil {
		delete(c.players, p)
	}
	c.mu.Unlock()
}

// clamp01 clamps v to the inclusive range [0, 1].
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
