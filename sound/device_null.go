package sound

import "sync"

// nullDevice is a silent output device. It runs a goroutine that pulls mixed
// frames on a fixed cadence so voices advance (and loops restart) without ever
// touching real hardware. It is used for tests, headless environments, and as
// the fallback when no real audio device is available.
type nullDevice struct {
	sr   int
	ch   int
	pull pullFunc

	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func newNullDevice(sr, ch int, pull pullFunc) *nullDevice {
	d := &nullDevice{
		sr:   sr,
		ch:   ch,
		pull: pull,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go d.loop()
	return d
}

func (d *nullDevice) loop() {
	defer close(d.done)
	// ~100ms block at the device sample rate.
	block := d.sr * d.ch / 10
	if block < 1 {
		block = 1
	}
	buf := make([]float32, block)
	for {
		select {
		case <-d.stop:
			return
		default:
			d.pull(buf)
		}
	}
}

func (d *nullDevice) Close() error {
	d.once.Do(func() { close(d.stop) })
	<-d.done
	return nil
}

func (d *nullDevice) isNull() bool { return true }
