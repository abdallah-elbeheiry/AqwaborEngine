//go:build linux && !otoaudio

package sound

import (
	"github.com/jfreymuth/pulse"
	"github.com/jfreymuth/pulse/proto"
)

// pulseDevice is the real audio backend for Linux. It is pure-Go (no CGO) and
// therefore links cleanly alongside the wgpu graphics stack, whose goffi loader
// otherwise collides at link time with oto's cgo/ALSA backend (both declare
// //go:cgo_import_dynamic for the same libc symbols — a Go toolchain limitation).
// PulseAudio must be running (or emulated via PipeWire's pulse compatibility) for
// this to produce sound; if it is unavailable, openRealDevice returns an error
// and the engine falls back to the null (silent) device.
type pulseDevice struct {
	client *pulse.Client
	stream *pulse.PlaybackStream
	reader *float32Reader
}

func openRealDevice(sr, ch int, pull pullFunc) (outputDevice, error) {
	client, err := pulse.NewClient()
	if err != nil {
		return nil, err
	}

	r := newFloat32Reader(pull)
	r.format = proto.FormatFloat32LE

	opts := []pulse.PlaybackOption{
		pulse.PlaybackSampleRate(sr),
		pulse.PlaybackBufferSize(sr * ch / 10), // ~100ms
		pulse.PlaybackMediaName("AqwaborEngine"),
	}
	if ch == 1 {
		opts = append(opts, pulse.PlaybackMono)
	} else {
		opts = append(opts, pulse.PlaybackStereo)
	}

	stream, err := client.NewPlayback(r, opts...)
	if err != nil {
		client.Close()
		return nil, err
	}
	stream.Start()

	return &pulseDevice{client: client, stream: stream, reader: r}, nil
}

func (d *pulseDevice) Close() error {
	d.reader.close()
	if d.stream != nil {
		d.stream.Close()
	}
	if d.client != nil {
		d.client.Close()
	}
	return nil
}

func (d *pulseDevice) isNull() bool { return false }
