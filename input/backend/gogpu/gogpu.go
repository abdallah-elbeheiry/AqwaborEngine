// Package gogpu adapts the gogpu application event model to the input system.
//
// It polls the gogpu input state each frame and emits raw edges (key/mouse
// down/up) plus pointer movement, which the input.Manager turns into
// higher-level Action events. Key and button codes are passed through
// directly because the input package mirrors gogpu's numeric encoding.
package gogpu

import (
	"github.com/gogpu/gogpu"
	gogpuinput "github.com/gogpu/gogpu/input"

	"github.com/abdallah-elbeheiry/AqwaborEngine/input"
)

// Pin our key/button encodings to gogpu. If gogpu inserts, removes or reorders
// a key, these compile-time assertions fail instead of silently shifting every
// binding by a constant offset.
const (
	_ = uint(input.KeyUnknown) - uint(gogpuinput.KeyUnknown)
	_ = uint(input.KeyCount) - uint(gogpuinput.KeyCount)
	_ = uint(input.MouseButtonCount) - uint(gogpuinput.MouseButtonCount)
)

// Backend is an input.Backend backed by a running *gogpu.App.
type Backend struct {
	app *gogpu.App

	prevKeys  map[gogpuinput.Key]bool
	prevMouse map[gogpuinput.MouseButton]bool
	lastX     float32
	lastY     float32
}

// NewBackend creates a Backend that reads input from the given App.
func NewBackend(app *gogpu.App) *Backend {
	return &Backend{
		app:       app,
		prevKeys:  make(map[gogpuinput.Key]bool),
		prevMouse: make(map[gogpuinput.MouseButton]bool),
	}
}

// Poll reads the current gogpu input state and returns the edges since the
// previous Poll. It is safe to call when the App has not started yet (it
// simply returns no events).
func (b *Backend) Poll() []input.Event {
	if b.app == nil {
		return nil
	}
	st := b.app.Input()
	if st == nil {
		return nil
	}

	kb := st.Keyboard()
	var events []input.Event
	for k := range gogpuinput.KeyCount {
		cur := kb.Pressed(k)
		if cur == b.prevKeys[k] {
			continue
		}
		b.prevKeys[k] = cur
		if cur {
			events = append(events, input.Event{Kind: input.EventKeyDown, Key: input.Key(k)})
		} else {
			events = append(events, input.Event{Kind: input.EventKeyUp, Key: input.Key(k)})
		}
	}

	ms := st.Mouse()
	for mb := range gogpuinput.MouseButtonCount {
		cur := ms.Pressed(mb)
		if cur == b.prevMouse[mb] {
			continue
		}
		b.prevMouse[mb] = cur
		x, y := ms.Position()
		if cur {
			events = append(events, input.Event{Kind: input.EventMouseDown, Button: input.MouseButton(mb), X: float64(x), Y: float64(y)})
		} else {
			events = append(events, input.Event{Kind: input.EventMouseUp, Button: input.MouseButton(mb), X: float64(x), Y: float64(y)})
		}
	}

	x, y := ms.Position()
	if x != b.lastX || y != b.lastY {
		b.lastX, b.lastY = x, y
		events = append(events, input.Event{Kind: input.EventMouseMove, X: float64(x), Y: float64(y)})
	}
	return events
}
