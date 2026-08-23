// Package input provides a high-level, ergonomic input system.
//
// The mental model is tiny:
//
//	Create an Action -> Bind hardware -> Attach logic -> Enable/Disable/Unbind as needed.
//
// The user only ever touches *Action values. There is no separate
// Subscription object: enabling, disabling, unbinding and rebinding all
// happen directly on the Action (or on the Manager for bindings). Callbacks
// stay alive across enable/disable and rebind operations.
//
// Raw input arrives through a Backend (e.g. gogpu or headless). The Manager
// maintains input state (key/button state, mouse position, timers) and
// derives higher-level events (hold, tap, toggle, combo, drag)
// from it before dispatching to the bound Actions.
//
// All event consumption is single-threaded: call Manager.Update once per
// frame from your main loop.
package input

import (
	"fmt"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// log is the input subsystem logger, pre-tagged with its component.
var log = logx.With("component", "input")

// EventKind enumerates the kinds of raw events a Backend can emit.
type EventKind uint8

const (
	// EventKeyDown is emitted on a key press edge.
	EventKeyDown EventKind = iota
	// EventKeyUp is emitted on a key release edge.
	EventKeyUp
	// EventMouseDown is emitted on a mouse button press edge.
	EventMouseDown
	// EventMouseUp is emitted on a mouse button release edge.
	EventMouseUp
	// EventMouseMove is emitted when the pointer changes position.
	EventMouseMove
)

// Event is a single raw input event produced by a Backend.
type Event struct {
	Kind   EventKind
	Key    Key
	Button MouseButton
	X, Y   float64 // valid for mouse events
}

// ActionEventType enumerates the derived events recorded by Manager.Drain.
type ActionEventType uint8

const (
	EventTypePressed ActionEventType = iota
	EventTypeReleased
	EventTypeHold
	EventTypeTap
	EventTypeToggle
	EventTypeDrag
)

// ActionEvent is a recorded, replayable derived event. It mirrors what the
// callbacks see, but as data instead of function calls, so the game can
// convert input into a command stream bound to simulation ticks. Use
// Manager.SetRecording(true) and Manager.Drain() to collect them.
type ActionEvent struct {
	Type   ActionEventType
	Action *Action
	Now    float64 // Manager clock (seconds) at dispatch
	Dt     float64 // frame delta (seconds) that produced this event
	Active bool    // toggle state, for EventTypeToggle
	DX, DY float64 // movement delta, for EventTypeDrag
}

// Backend supplies raw input events to the Manager.
//
// Implementations must be safe to call from the same goroutine that calls
// Manager.Update. Poll is called once per frame and should return the events
// that occurred since the previous call (an empty slice is fine).
type Backend interface {
	Poll() []Event
}

// Context is passed to every callback. It is a value type and is reused
// across dispatches within a single frame, so callbacks must consume it
// synchronously (they always do).
type Context struct {
	mgr    *Manager
	action *Action
	now    float64
	dt     float64
}

// MousePosition returns the current pointer position in window coordinates.
func (c Context) MousePosition() (x, y float64) { return c.mgr.mouseX, c.mgr.mouseY }

// Action returns the Action that triggered the current callback.
func (c Context) Action() *Action { return c.action }

// Now returns the Manager clock (seconds) at the time of dispatch.
func (c Context) Now() float64 { return c.now }

// Dt returns the frame delta (seconds) that produced the current dispatch.
//
// OnHold fires once per frame, so any accumulation must scale by Dt to be
// frame-rate independent: charge += rate*ctx.Dt().
func (c Context) Dt() float64 { return c.dt }

// Default tuning constants for derived tap behaviour.
const (
	// defaultTapMax is the longest press duration (seconds) that still
	// counts as a tap when no OnHold threshold is registered.
	defaultTapMax = 0.22
)

// Manager owns all Actions and drives event processing.
type Manager struct {
	backend Backend

	actions []*Action
	byName  map[string]*Action

	clock float64

	keysDown  map[Key]bool
	mouseDown [MouseButtonCount]bool
	mouseX    float64
	mouseY    float64

	ctx Context

	recording bool
	events    []ActionEvent
}

// NewManager creates a Manager fed by the given Backend.
func NewManager(backend Backend) *Manager {
	m := &Manager{
		backend:  backend,
		byName:   make(map[string]*Action),
		keysDown: make(map[Key]bool),
	}
	m.ctx.mgr = m
	log.Debug("input manager created", "backend", backendName(backend))
	return m
}

// backendName returns a short, stable identifier for the backend type.
func backendName(b Backend) string {
	switch b.(type) {
	case nil:
		return "none"
	default:
		return fmt.Sprintf("%T", b)
	}
}

// Action returns the named Action, creating it on first use.
func (m *Manager) Action(name string) *Action {
	if a, ok := m.byName[name]; ok {
		return a
	}
	a := &Action{name: name, mgr: m, enabled: true}
	m.actions = append(m.actions, a)
	m.byName[name] = a
	return a
}

// BindKey adds a keyboard binding to an Action. Bindings accumulate; an
// Action fires when any of its bindings is active.
func (m *Manager) BindKey(a *Action, k Key) {
	a.bindings = append(a.bindings, Binding{kind: bindKey, key: k})
	log.Debug("bound key", "action", a.name, "key", k, "bindings", len(a.bindings))
}

// BindMouseButton adds a mouse button binding to an Action.
func (m *Manager) BindMouseButton(a *Action, b MouseButton) {
	a.bindings = append(a.bindings, Binding{kind: bindMouse, button: b})
	log.Debug("bound mouse button", "action", a.name, "button", b, "bindings", len(a.bindings))
}

// Combo returns (creating if needed) an Action that fires only when all the
// given keys are held simultaneously. The combo is treated as a single
// binding on the returned Action.
func (m *Manager) Combo(name string, keys ...Key) *Action {
	a := m.Action(name)
	a.bindings = append(a.bindings, Binding{kind: bindCombo, combo: keys})
	log.Debug("bound combo", "action", name, "keys", keys)
	return a
}

// Rebind clears an Action's hardware bindings and binds a single key.
// Callbacks and live state are preserved (the pressed flag is reset, so no
// release is synthesized for a key that was physically still held).
func (m *Manager) Rebind(a *Action, k Key) {
	log.Debug("rebind", "action", a.name, "key", k)
	a.Unbind()
	m.BindKey(a, k)
}

// IsDown reports whether the Action is currently considered pressed, based
// on its bindings and the live input state. Disabled Actions still report
// accurate state.
func (m *Manager) IsDown(a *Action) bool { return a.down }

// MousePosition returns the current pointer position.
func (m *Manager) MousePosition() (x, y float64) { return m.mouseX, m.mouseY }

// Clock returns the internal clock (seconds) advanced by Update.
func (m *Manager) Clock() float64 { return m.clock }

// SetRecording enables or disables recording of derived events. When enabled,
// every dispatched derived event is appended to an internal buffer that
// Drain returns. Recording is off by default so the default callback path
// stays allocation-free.
func (m *Manager) SetRecording(on bool) { m.recording = on }

// Drain returns and clears the derived events recorded since the last call.
// Use it to build a deterministic command stream for the simulation; attach
// callbacks on top for non-simulation concerns (camera, UI).
func (m *Manager) Drain() []ActionEvent {
	e := m.events
	m.events = nil
	return e
}

// record appends a derived event to the buffer when recording is enabled.
func (m *Manager) record(t ActionEventType, active bool, dx, dy float64) {
	if !m.recording {
		return
	}
	m.events = append(m.events, ActionEvent{
		Type:   t,
		Action: m.ctx.action,
		Now:    m.clock,
		Dt:     m.ctx.dt,
		Active: active,
		DX:     dx,
		DY:     dy,
	})
}

// Update advances the simulation by dt seconds, pumps the Backend for new
// raw events, and dispatches derived events to enabled Actions.
//
// Call this once per frame from your main loop (single-threaded).
func (m *Manager) Update(dt float64) {
	if dt < 0 {
		dt = 0
	}
	m.clock += dt
	m.ctx.now = m.clock
	m.ctx.dt = dt

	eventCount := 0
	if m.backend != nil {
		for _, ev := range m.backend.Poll() {
			m.applyEvent(ev)
			eventCount++
		}
	}

	for _, a := range m.actions {
		a.process(m)
	}
	log.Trace("input frame", "clock", m.clock, "dt", dt, "raw_events", eventCount, "actions", len(m.actions))
}

func (m *Manager) applyEvent(ev Event) {
	switch ev.Kind {
	case EventKeyDown:
		m.keysDown[ev.Key] = true
		log.Trace("key down", "key", ev.Key)
	case EventKeyUp:
		m.keysDown[ev.Key] = false
		log.Trace("key up", "key", ev.Key)
	case EventMouseDown:
		if ev.Button < MouseButtonCount {
			m.mouseDown[ev.Button] = true
		}
		m.mouseX, m.mouseY = ev.X, ev.Y
		log.Trace("mouse down", "button", ev.Button, "x", ev.X, "y", ev.Y)
	case EventMouseUp:
		if ev.Button < MouseButtonCount {
			m.mouseDown[ev.Button] = false
		}
		m.mouseX, m.mouseY = ev.X, ev.Y
		log.Trace("mouse up", "button", ev.Button, "x", ev.X, "y", ev.Y)
	case EventMouseMove:
		m.mouseX, m.mouseY = ev.X, ev.Y
		log.Trace("mouse move", "x", ev.X, "y", ev.Y)
	}
}
