package sound

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
)

// float32Reader adapts a pullFunc into an io.Reader that emits interleaved
// float32 PCM in little-endian byte layout. It is consumed by the real device
// backends (oto/pulse), which pull from it on their own audio threads. Closing
// the reader yields EOF so the underlying device stops cleanly.
type float32Reader struct {
	pull    pullFunc
	format  byte // device sample format; set by backends that require it (e.g. pulse)
	closeCh chan struct{}
	once    sync.Once
	buf     []float32
}

func newFloat32Reader(pull pullFunc) *float32Reader {
	return &float32Reader{pull: pull, closeCh: make(chan struct{})}
}

// Format returns the sample format this reader produces. It is part of the
// contract some backends (e.g. pulse) require to configure their stream; its
// value is set by those backends when they create the reader. Backends that
// configure the format themselves (e.g. oto) ignore it.
func (r *float32Reader) Format() byte { return r.format }

// Read fills p with float32-LE bytes. p must be a multiple of 4 bytes; any
// trailing partial sample is dropped.
func (r *float32Reader) Read(p []byte) (int, error) {
	select {
	case <-r.closeCh:
		return 0, io.EOF
	default:
	}

	nFloats := len(p) / 4
	if nFloats == 0 {
		return 0, nil
	}
	if cap(r.buf) < nFloats {
		r.buf = make([]float32, nFloats)
	}
	fbuf := r.buf[:nFloats]

	r.pull(fbuf)

	for i := range nFloats {
		binary.LittleEndian.PutUint32(p[i*4:], math.Float32bits(fbuf[i]))
	}
	return nFloats * 4, nil
}

func (r *float32Reader) close() {
	r.once.Do(func() { close(r.closeCh) })
}
