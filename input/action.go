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

// multiTapHandler pairs a tap count with its callback.
type multiTapHandler struct {
	n  int
	fn func(Context)
}

// Action is the single user-facing input object. The user binds hardware to
// it, attaches callbacks to it, and controls it directly via Enable,
// Disable and Unbind.
type Action struct {
	name    string
	mgr     *Manager
	enabled bool

	bindings []Binding

	onPress     []func(Context)
	onRelease   []func(Context)
	onHold      []holdHandler
	onTap       []func(Context)
	onToggle    []func(bool, Context)
	onDoubleTap []func(Context)
	onMultiTap  []multiTapHandler
	onDrag      []func(float64, float64, Context)

	// Derived-event tuning.
	holdThreshold float64

	// Live state.
	down        bool
	downTime    float64
	active      bool // toggle state
	lastTapTime float64
	tapCount    int
	lastDragX   float64
	lastDragY   float64
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

// Unbind clears all hardware bindings. Callbacks are preserved.
func (a *Action) Unbind() { a.bindings = nil }

// IsDown reports whether the Action is currently pressed.
func (a *Action) IsDown() bool { return a.down }

// IsActive reports the current toggle state.
func (a *Action) IsActive() bool { return a.active }

// OnPressed registers a callback fired once on the press edge.
func (a *Action) OnPressed(fn func(Context)) { a.onPress = append(a.onPress, fn) }

// OnReleased registers a callback fired once on the release edge.
func (a *Action) OnReleased(fn func(Context)) { a.onRelease = append(a.onRelease, fn) }

// OnHold registers a callback fired every frame the Action has been held
// longer than threshold (seconds). It fires continuously while held.
func (a *Action) OnHold(threshold float64, fn func(Context)) {
	a.onHold = append(a.onHold, holdHandler{threshold, fn})
	if threshold > a.holdThreshold {
		a.holdThreshold = threshold
	}
}

// OnTap registers a callback fired on a quick press+release (shorter than
// the effective tap window: the largest OnHold threshold, or defaultTapMax).
func (a *Action) OnTap(fn func(Context)) { a.onTap = append(a.onTap, fn) }

// OnToggle registers a callback fired on every press edge, carrying the new
// toggle state (true after an odd number of presses).
func (a *Action) OnToggle(fn func(bool, Context)) { a.onToggle = append(a.onToggle, fn) }

// OnDoubleTap registers a callback fired when two taps occur within
// defaultDoubleTapWindow of each other.
func (a *Action) OnDoubleTap(fn func(Context)) { a.onDoubleTap = append(a.onDoubleTap, fn) }

// OnMultiTap registers a callback fired when n taps occur within
// defaultDoubleTapWindow of each other (n >= 2).
func (a *Action) OnMultiTap(n int, fn func(Context)) {
	if n < 2 {
		n = 2
	}
	a.onMultiTap = append(a.onMultiTap, multiTapHandler{n, fn})
}

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
			if a.enabled {
				m.ctx.action = a
				for _, fn := range a.onPress {
					fn(m.ctx)
				}
				a.active = !a.active
				for _, fn := range a.onToggle {
					fn(a.active, m.ctx)
				}
			}
		} else {
			duration := m.clock - a.downTime
			if a.enabled {
				m.ctx.action = a
				for _, fn := range a.onRelease {
					fn(m.ctx)
				}
				a.handleTap(duration, m)
			}
		}
	}

	if a.down && a.enabled {
		if a.holdThreshold > 0 && m.clock-a.downTime >= a.holdThreshold {
			m.ctx.action = a
			for _, h := range a.onHold {
				h.fn(m.ctx)
			}
		}
		if len(a.onDrag) > 0 {
			dx := m.mouseX - a.lastDragX
			dy := m.mouseY - a.lastDragY
			a.lastDragX, a.lastDragY = m.mouseX, m.mouseY
			if dx != 0 || dy != 0 {
				m.ctx.action = a
				for _, fn := range a.onDrag {
					fn(dx, dy, m.ctx)
				}
			}
		}
	}
}

// handleTap fires tap/double-tap/multi-tap callbacks for a completed press
// whose duration qualifies as a tap.
func (a *Action) handleTap(duration float64, m *Manager) {
	tapMax := a.holdThreshold
	if tapMax <= 0 {
		tapMax = defaultTapMax
	}
	if duration > tapMax {
		return
	}

	for _, fn := range a.onTap {
		fn(m.ctx)
	}

	if m.clock-a.lastTapTime <= defaultDoubleTapWindow {
		a.tapCount++
	} else {
		a.tapCount = 1
	}
	a.lastTapTime = m.clock

	if a.tapCount == 2 {
		for _, fn := range a.onDoubleTap {
			fn(m.ctx)
		}
	}
	for _, h := range a.onMultiTap {
		if a.tapCount == h.n {
			h.fn(m.ctx)
		}
	}
}
