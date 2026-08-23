// Package headless provides an injectable Backend for the input system.
//
// It is intended for tests, headless servers and LLM-driven playtesting: the
// same Action logic that runs against real hardware can be driven by injecting
// synthetic events, and consumed deterministically with a fixed timestep.
package headless

import (
	"sync"

	"github.com/abdallah-elbeheiry/AqwaborEngine/input"
)

// Backend is an input.Backend whose events are injected programmatically.
type Backend struct {
	mu    sync.Mutex
	queue []input.Event
}

// New creates an empty headless Backend.
func New() *Backend { return &Backend{} }

func (b *Backend) push(e input.Event) {
	b.mu.Lock()
	b.queue = append(b.queue, e)
	b.mu.Unlock()
}

// Poll returns and clears the events injected since the last call.
func (b *Backend) Poll() []input.Event {
	b.mu.Lock()
	q := b.queue
	b.queue = nil
	b.mu.Unlock()
	return q
}

// KeyDown injects a key press edge.
func (b *Backend) KeyDown(k input.Key) { b.push(input.Event{Kind: input.EventKeyDown, Key: k}) }

// KeyUp injects a key release edge.
func (b *Backend) KeyUp(k input.Key) { b.push(input.Event{Kind: input.EventKeyUp, Key: k}) }

// MouseMove injects a pointer movement to (x, y).
func (b *Backend) MouseMove(x, y float64) {
	b.push(input.Event{Kind: input.EventMouseMove, X: x, Y: y})
}

// MouseDown injects a mouse button press at (x, y).
func (b *Backend) MouseDown(btn input.MouseButton, x, y float64) {
	b.push(input.Event{Kind: input.EventMouseDown, Button: btn, X: x, Y: y})
}

// MouseUp injects a mouse button release at (x, y).
func (b *Backend) MouseUp(btn input.MouseButton, x, y float64) {
	b.push(input.Event{Kind: input.EventMouseUp, Button: btn, X: x, Y: y})
}
