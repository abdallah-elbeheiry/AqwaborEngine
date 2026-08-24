//go:build !linux || otoaudio

package sound

import (
	"time"

	"github.com/ebitengine/oto/v3"
)

// otoDevice is the real audio backend for Windows and macOS, built on the oto
// v3 library. oto pulls float32-LE frames from our reader on its own audio thread.
// (On Linux the pure-Go PulseAudio backend is used instead, because oto's cgo/ALSA
// backend cannot be linked into the same binary as the wgpu graphics stack under
// the current Go toolchain — see device_pulse_linux.go.)
type otoDevice struct {
	ctx    *oto.Context
	player *oto.Player
	reader *float32Reader
}

func openRealDevice(sr, ch int, pull pullFunc) (outputDevice, error) {
	octx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sr,
		ChannelCount: ch,
		Format:       oto.FormatFloat32LE,
	})
	if err != nil {
		return nil, err
	}

	// Wait briefly for the context to become usable; oto will not call Read
	// until it is ready, but a stalled server should not hang startup.
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
	}

	r := newFloat32Reader(pull)
	p := octx.NewPlayer(r)
	p.Play()

	return &otoDevice{ctx: octx, player: p, reader: r}, nil
}

func (d *otoDevice) Close() error {
	d.reader.close()
	if d.player != nil {
		_ = d.player.Close()
	}
	if d.ctx != nil {
		_ = d.ctx.Suspend()
	}
	return nil
}

func (d *otoDevice) isNull() bool { return false }
