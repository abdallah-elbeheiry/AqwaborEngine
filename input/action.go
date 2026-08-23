package input

// bindingKind distinguishes how a Binding maps hardware input to an Action.
type bindingKind uint8

const (
	bindKey bindingKind = iota
	bindMouse
	bindCombo
)

// Binding maps hardware input to an Action.
//
//   - bindKey:    the Action is active while key is down.
//   - bindMouse:  the Action is active while the button is down.
//   - bindCombo:  the Action is active only while ALL combo keys are down.
//
// An Action with multiple bindings is active if ANY binding is active.
type Binding struct {
	kind   bindingKind
	key    Key
	button MouseButton
	combo  []Key
}

// currentDown reports whether the Action should currently be considered
// pressed, based on its bindings and the live Manager state.
func (a *Action) currentDown(m *Manager) bool {
	for _, b := range a.bindings {
		switch b.kind {
		case bindKey:
			if m.keysDown[b.key] {
				return true
			}
		case bindMouse:
			if b.button < MouseButtonCount && m.mouseDown[b.button] {
				return true
			}
		case bindCombo:
			allDown := true
			for _, k := range b.combo {
				if !m.keysDown[k] {
					allDown = false
					break
				}
			}
			if allDown {
				return true
			}
		}
	}
	return false
}

// holdHandler pairs a hold threshold with its callback.
type holdHandler struct {
	threshold float64
	fn        func(Context)
}

// Action is the single user-facing input object. The user binds hardware to
// it, attaches callbacks to it, and controls it directly via Enable,
// Disable and Unbind.
type Action struct {
	name    string
	mgr     *Manager
	enabled bool

	bindings []Binding

	onPress   []func(Context)
	onRelease []func(Context)
	onHold    []holdHandler
	onTap     []func(Context)
	onToggle  []func(bool, Context)
	onDrag    []func(float64, float64, Context)

	// Live state.
	down      bool
	downTime  float64
	active    bool // toggle state
	lastDragX float64
	lastDragY float64
}

// Name returns the Action's identifier.
func (a *Action) Name() string { return a.name }

// Enabled reports whether the Action is currently delivering events.
func (a *Action) Enabled() bool { return a.enabled }

// Enable resumes event delivery. Callbacks and bindings are preserved.
func (a *Action) Enable() { a.enabled = true }

// Disable stops event delivery but keeps callbacks and bindings. Input state
// for the Action stays up to date so re-enabling does not emit a stale press.
func (a *Action) Disable() { a.enabled = false }

// Unbind clears all hardware bindings. Callbacks and live state (including
// the pressed flag) are reset, so no release is synthesized for a key that
// was physically still held.
func (a *Action) Unbind() {
	a.bindings = nil
	a.down = false
}

// IsDown reports whether the Action is currently pressed.
func (a *Action) IsDown() bool { return a.down }

// IsActive reports the current toggle state.
func (a *Action) IsActive() bool { return a.active }

// OnPressed registers a callback fired once on the press edge.
func (a *Action) OnPressed(fn func(Context)) { a.onPress = append(a.onPress, fn) }

// OnReleased registers a callback fired once on the release edge.
func (a *Action) OnReleased(fn func(Context)) { a.onRelease = append(a.onRelease, fn) }

// OnHold registers a callback fired every frame the Action has been held
// longer than its own threshold (seconds). Each handler is gated on its own
// threshold, so OnHold(0.1) and OnHold(1.0) fire independently. Because the
// callback fires every frame, scale accumulations by ctx.Dt() to stay
// frame-rate independent.
func (a *Action) OnHold(threshold float64, fn func(Context)) {
	a.onHold = append(a.onHold, holdHandler{threshold, fn})
}

// OnTap registers a callback fired on a quick press+release, where "quick"
// means shorter than defaultTapMax (0.22s). The tap window is fixed and does
// not depend on any OnHold threshold.
func (a *Action) OnTap(fn func(Context)) { a.onTap = append(a.onTap, fn) }

// OnToggle registers a callback fired on every press edge, carrying the new
// toggle state (true after an odd number of presses).
func (a *Action) OnToggle(fn func(bool, Context)) { a.onToggle = append(a.onToggle, fn) }

// OnDrag registers a callback fired whenever the pointer moves while the
// Action is held. dx, dy are the per-frame movement deltas.
func (a *Action) OnDrag(fn func(float64, float64, Context)) { a.onDrag = append(a.onDrag, fn) }

// process updates the Action's state from the live Manager state and
// dispatches any derived events. It is called once per frame per Action.
func (a *Action) process(m *Manager) {
	down := a.currentDown(m)

	if down != a.down {
		a.down = down
		if down {
			a.downTime = m.clock
			a.lastDragX, a.lastDragY = m.mouseX, m.mouseY
			// Toggle state advances regardless of enabled, so re-enabling
			// after a press while disabled yields a consistent logical state.
			a.active = !a.active
			if a.enabled {
				m.ctx.action = a
				m.record(EventTypePressed, false, 0, 0)
				log.Debug("action pressed", "action", a.name, "toggle", a.active, "press_cbs", len(a.onPress), "toggle_cbs", len(a.onToggle))
				for _, fn := range a.onPress {
					fn(m.ctx)
				}
				m.record(EventTypeToggle, a.active, 0, 0)
				for _, fn := range a.onToggle {
					fn(a.active, m.ctx)
				}
			}
		} else {
			duration := m.clock - a.downTime
			if a.enabled {
				m.ctx.action = a
				m.record(EventTypeReleased, false, 0, 0)
				log.Debug("action released", "action", a.name, "held_s", duration)
				for _, fn := range a.onRelease {
					fn(m.ctx)
				}
				a.handleTap(duration, m)
			}
		}
	}

	if a.down && a.enabled {
		m.ctx.action = a
		for _, h := range a.onHold {
			if m.clock-a.downTime >= h.threshold {
				m.record(EventTypeHold, false, 0, 0)
				log.Debug("action hold", "action", a.name, "threshold_s", h.threshold, "held_s", m.clock-a.downTime)
				h.fn(m.ctx)
			}
		}
		if len(a.onDrag) > 0 {
			dx := m.mouseX - a.lastDragX
			dy := m.mouseY - a.lastDragY
			a.lastDragX, a.lastDragY = m.mouseX, m.mouseY
			if dx != 0 || dy != 0 {
				m.ctx.action = a
				m.record(EventTypeDrag, false, dx, dy)
				log.Debug("action drag", "action", a.name, "dx", dx, "dy", dy)
				for _, fn := range a.onDrag {
					fn(dx, dy, m.ctx)
				}
			}
		}
	}
}

// handleTap fires the tap callback for a completed press whose duration
// qualifies as a tap.
func (a *Action) handleTap(duration float64, m *Manager) {
	if duration > defaultTapMax {
		return
	}

	m.record(EventTypeTap, false, 0, 0)
	for _, fn := range a.onTap {
		fn(m.ctx)
	}
}
