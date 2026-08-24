package sound

import (
	"fmt"
	"sync"
)

// Player is a single playback instance of a Clip. Multiple Players can play
// the same Clip at once (overlapping sound effects); each has its own volume
// and lifecycle.
type Player struct {
	ctx  *Context
	clip *Clip

	// voice is the mixer voice that actually pulls PCM for this Player.
	voice *voice

	id   string
	loop bool

	mu     sync.Mutex
	volume float64
}

// Play starts a one-shot playback of the clip and returns the Player.
func (c *Clip) Play() (*Player, error) {
	return c.play(false)
}

// PlayLoop starts a looping playback of the clip and returns the Player. Stop
// it to end playback.
func (c *Clip) PlayLoop() (*Player, error) {
	return c.play(true)
}

func (c *Clip) play(loop bool) (*Player, error) {
	if c.ctx.isClosed() {
		return nil, ErrClosed
	}

	src, err := newSource(c.data, c.format, loop)
	if err != nil {
		log.Error("create player source failed", "err", err, "id", c.id, "format", c.format.String())
		return nil, fmt.Errorf("sound: create player: %w", err)
	}

	bp := c.ctx.backend.NewPlayer(src)
	p := &Player{
		ctx:    c.ctx,
		clip:   c,
		voice:  bp,
		id:     fmt.Sprintf("p%d", playerSeq.Add(1)),
		loop:   loop,
		volume: 1.0,
	}

	c.ctx.registerPlayer(p)
	c.registerPlayer(p)

	p.applyEffective()

	log.Debug("play",
		"player", p.id, "clip", c.id, "format", c.format.String(),
		"loop", loop, "effectiveVolume", p.EffectiveVolume(),
		"sampleRate", c.sampleRate, "channels", c.channels)
	return p, nil
}

// SetVolume sets this instance's gain, clamped to [0, 1], and re-applies the
// effective volume immediately.
func (p *Player) SetVolume(v float64) {
	p.mu.Lock()
	v = clamp01(v)
	p.volume = v
	p.mu.Unlock()

	eff := p.applyEffective()
	log.Debug("player volume changed",
		"player", p.id, "clip", p.clip.id, "instance", v, "effective", eff)
}

// Volume returns this instance's gain.
func (p *Player) Volume() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

// Stop ends playback permanently. The Player cannot be restarted.
func (p *Player) Stop() {
	p.voice.Stop()
	p.ctx.removePlayer(p)
	p.clip.removePlayer(p)
	log.Debug("stop", "player", p.id, "clip", p.clip.id, "loop", p.loop)
}

// Pause suspends playback without losing position.
func (p *Player) Pause() {
	p.voice.Pause()
	log.Debug("pause", "player", p.id, "clip", p.clip.id, "loop", p.loop)
}

// Resume continues playback after a Pause.
func (p *Player) Resume() {
	p.voice.Resume()
	log.Debug("resume", "player", p.id, "clip", p.clip.id, "loop", p.loop)
}

// applyEffective folds the clip and player gains into the voice's own volume
// (master is applied by the mixer) and returns the effective volume actually
// heard by the listener (master * clip * player, each clamped to [0, 1]).
func (p *Player) applyEffective() float64 {
	p.mu.Lock()
	v := p.volume
	p.mu.Unlock()

	clipVol := p.clip.volumeValue()
	voiceVol := float32(clamp01(clipVol) * clamp01(v))
	p.voice.SetVolume(voiceVol)
	return computeEffective(p.ctx.masterVolume(), clipVol, v)
}

// EffectiveVolume returns the gain actually heard for this Player:
// master * clip * player, each clamped to [0, 1].
func (p *Player) EffectiveVolume() float64 {
	p.mu.Lock()
	v := p.volume
	p.mu.Unlock()
	return computeEffective(p.ctx.masterVolume(), p.clip.volumeValue(), v)
}

// computeEffective is the pure volume formula, unit-testable without a device.
func computeEffective(master, clip, player float64) float64 {
	return clamp01(master) * clamp01(clip) * clamp01(player)
}
