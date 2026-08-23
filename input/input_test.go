package input

import "testing"

// stubBackend is a minimal Backend used to drive the Manager deterministically
// in tests without a real window.
type stubBackend struct {
	q []Event
}

func (s *stubBackend) Poll() []Event {
	q := s.q
	s.q = nil
	return q
}

func (s *stubBackend) push(e Event) { s.q = append(s.q, e) }

func newTestManager() (*Manager, *stubBackend) {
	b := &stubBackend{}
	return NewManager(b), b
}

func TestActionCreatedOnce(t *testing.T) {
	m, _ := newTestManager()
	a := m.Action("jump")
	b := m.Action("jump")
	if a != b {
		t.Fatal("Action should be created once per name")
	}
	if a.Name() != "jump" {
		t.Fatalf("unexpected name %q", a.Name())
	}
}

func TestPressedReleased(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("jump")
	m.BindKey(a, KeySpace)
	pressed, released := 0, 0
	a.OnPressed(func(Context) { pressed++ })
	a.OnReleased(func(Context) { released++ })

	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	if pressed != 1 || released != 0 {
		t.Fatalf("press: got pressed=%d released=%d", pressed, released)
	}
	if !m.IsDown(a) {
		t.Fatal("IsDown should be true")
	}

	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0)
	if pressed != 1 || released != 1 {
		t.Fatalf("release: got pressed=%d released=%d", pressed, released)
	}
	if m.IsDown(a) {
		t.Fatal("IsDown should be false after release")
	}
}

func TestHoldFiresContinuously(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("charge")
	m.BindKey(a, KeySpace)
	holds := 0
	a.OnHold(0.4, func(Context) { holds++ })

	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0) // down registered at t=0
	if holds != 0 {
		t.Fatal("hold should not fire before threshold")
	}
	m.Update(0.2) // t=0.2, < 0.4
	if holds != 0 {
		t.Fatal("hold should not fire before threshold (2)")
	}
	m.Update(0.3) // t=0.5, >= 0.4
	if holds != 1 {
		t.Fatalf("expected hold to fire once, got %d", holds)
	}
	m.Update(0.1) // t=0.6, still held
	if holds != 2 {
		t.Fatalf("expected hold to fire every frame while held, got %d", holds)
	}

	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0)
	if holds != 2 {
		t.Fatalf("hold should stop after release, got %d", holds)
	}
}

func TestTapVsHold(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("tap")
	m.BindKey(a, KeySpace)
	taps, holds := 0, 0
	a.OnTap(func(Context) { taps++ })
	a.OnHold(0.4, func(Context) { holds++ })

	// Quick tap.
	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0.05)
	if taps != 1 || holds != 0 {
		t.Fatalf("quick tap: taps=%d holds=%d", taps, holds)
	}

	// Long hold (no tap on release, hold already fired).
	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	m.Update(0.5) // hold fires
	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0)
	if taps != 1 || holds != 1 {
		t.Fatalf("long hold should not tap: taps=%d holds=%d", taps, holds)
	}
}

func TestDoubleTap(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("dt")
	m.BindKey(a, KeySpace)
	doubles := 0
	a.OnDoubleTap(func(Context) { doubles++ })

	// A short press+release (duration 0.02s) counts as a tap.
	tap := func() {
		b.push(Event{Kind: EventKeyDown, Key: KeySpace})
		m.Update(0.01)
		b.push(Event{Kind: EventKeyUp, Key: KeySpace})
		m.Update(0.01)
	}
	// Idle time used to break the double-tap window.
	gap := func(d float64) { m.Update(d) }

	tap()
	if doubles != 0 {
		t.Fatal("single tap should not double")
	}
	tap() // within window
	if doubles != 1 {
		t.Fatalf("expected double tap, got %d", doubles)
	}

	// Outside the window: reset.
	gap(1.0)
	tap()
	if doubles != 1 {
		t.Fatal("tap after gap should not double")
	}
	tap()
	if doubles != 2 {
		t.Fatalf("expected second double tap, got %d", doubles)
	}
}

func TestMultiTap(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("mt")
	m.BindKey(a, KeySpace)
	triples := 0
	a.OnMultiTap(3, func(Context) { triples++ })

	tap := func() {
		b.push(Event{Kind: EventKeyDown, Key: KeySpace})
		m.Update(0.01)
		b.push(Event{Kind: EventKeyUp, Key: KeySpace})
		m.Update(0.01)
	}
	gap := func(d float64) { m.Update(d) }

	tap()
	tap()
	if triples != 0 {
		t.Fatal("two taps should not triple")
	}
	tap()
	if triples != 1 {
		t.Fatalf("expected triple tap, got %d", triples)
	}

	gap(1.0)
	tap()
	tap()
	if triples != 1 {
		t.Fatal("tap after gap should reset count")
	}
	tap()
	if triples != 2 {
		t.Fatalf("expected second triple tap, got %d", triples)
	}
}

func TestToggle(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("t")
	m.BindKey(a, KeySpace)
	var states []bool
	a.OnToggle(func(active bool, _ Context) { states = append(states, active) })

	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0)

	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0)

	if len(states) != 2 || states[0] != true || states[1] != false {
		t.Fatalf("toggle states = %v", states)
	}
	if a.IsActive() != false {
		t.Fatal("toggle should be off after two presses")
	}
}

func TestCombo(t *testing.T) {
	m, b := newTestManager()
	c := m.Combo("stealth", KeyX, KeyV)
	pressed := 0
	c.OnPressed(func(Context) { pressed++ })

	b.push(Event{Kind: EventKeyDown, Key: KeyX})
	m.Update(0)
	if pressed != 0 {
		t.Fatal("combo should not fire on first key")
	}

	b.push(Event{Kind: EventKeyDown, Key: KeyV})
	m.Update(0)
	if pressed != 1 {
		t.Fatalf("combo should fire when all keys down, got %d", pressed)
	}

	b.push(Event{Kind: EventKeyUp, Key: KeyX})
	m.Update(0)
	if pressed != 1 {
		t.Fatal("combo should not re-fire on partial release")
	}
	b.push(Event{Kind: EventKeyUp, Key: KeyV})
	m.Update(0)

	// Re-press both for a second fire.
	b.push(Event{Kind: EventKeyDown, Key: KeyX})
	m.Update(0)
	b.push(Event{Kind: EventKeyDown, Key: KeyV})
	m.Update(0)
	if pressed != 2 {
		t.Fatalf("combo should fire again, got %d", pressed)
	}
}

func TestEnableDisable(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("e")
	m.BindKey(a, KeySpace)
	pressed := 0
	a.OnPressed(func(Context) { pressed++ })

	a.Disable()
	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0)
	if pressed != 0 {
		t.Fatal("disabled action should not deliver")
	}
	if a.Enabled() {
		t.Fatal("Enabled() should reflect disabled state")
	}

	a.Enable()
	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	if pressed != 1 {
		t.Fatalf("enabled action should deliver, got %d", pressed)
	}
}

func TestUnbindKeepsCallbacks(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("u")
	pressed := 0
	a.OnPressed(func(Context) { pressed++ })

	m.BindKey(a, KeySpace)
	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	if pressed != 1 {
		t.Fatal("bound action should fire")
	}

	a.Unbind()
	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	if pressed != 1 {
		t.Fatal("unbound action should not fire")
	}

	// Rebind: callbacks preserved.
	m.Rebind(a, KeyW)
	b.push(Event{Kind: EventKeyDown, Key: KeyW})
	m.Update(0)
	if pressed != 2 {
		t.Fatalf("rebound action with same callbacks should fire, got %d", pressed)
	}
	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	if pressed != 2 {
		t.Fatal("old binding should be gone after rebind")
	}
}

func TestMouseClickCarriesPosition(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("click")
	m.BindMouseButton(a, MouseButtonLeft)
	var gotX, gotY float64
	a.OnPressed(func(c Context) {
		gotX, gotY = c.MousePosition()
	})

	b.push(Event{Kind: EventMouseDown, Button: MouseButtonLeft, X: 123, Y: 456})
	m.Update(0)
	if gotX != 123 || gotY != 456 {
		t.Fatalf("click position = (%v,%v)", gotX, gotY)
	}
	if mx, my := m.MousePosition(); mx != 123 || my != 456 {
		t.Fatalf("manager mouse position = (%v,%v)", mx, my)
	}

	b.push(Event{Kind: EventMouseUp, Button: MouseButtonLeft, X: 123, Y: 456})
	m.Update(0)
}

func TestMouseDrag(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("drag")
	m.BindMouseButton(a, MouseButtonRight)
	var totalDX, totalDY float64
	a.OnDrag(func(dx, dy float64, _ Context) {
		totalDX += dx
		totalDY += dy
	})

	b.push(Event{Kind: EventMouseDown, Button: MouseButtonRight, X: 0, Y: 0})
	m.Update(0)

	b.push(Event{Kind: EventMouseMove, X: 10, Y: 5})
	m.Update(0)
	if totalDX != 10 || totalDY != 5 {
		t.Fatalf("drag delta = (%v,%v)", totalDX, totalDY)
	}

	b.push(Event{Kind: EventMouseMove, X: 10, Y: 5})
	m.Update(0) // no movement
	if totalDX != 10 || totalDY != 5 {
		t.Fatal("no movement should not accumulate drag")
	}

	b.push(Event{Kind: EventMouseUp, Button: MouseButtonRight, X: 10, Y: 5})
	m.Update(0)
	b.push(Event{Kind: EventMouseMove, X: 99, Y: 99})
	m.Update(0) // not down, no drag
	if totalDX != 10 || totalDY != 5 {
		t.Fatal("drag should stop after release")
	}
}

func TestCallbacksSurviveDisableAcrossHold(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("h")
	m.BindKey(a, KeySpace)
	holds := 0
	a.OnHold(0.1, func(Context) { holds++ })

	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	a.Disable()
	m.Update(0.2) // held but disabled -> no hold
	if holds != 0 {
		t.Fatal("disabled action should not fire hold")
	}
	a.Enable()
	m.Update(0.2)
	if holds != 1 {
		t.Fatalf("re-enabled held action should fire hold, got %d", holds)
	}
}

// OnHold must honour each handler's own threshold, not the max.
func TestOnHoldPerHandlerThreshold(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("h")
	m.BindKey(a, KeySpace)
	hold01, hold10 := 0, 0
	a.OnHold(0.1, func(Context) { hold01++ })
	a.OnHold(1.0, func(Context) { hold10++ })

	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	m.Update(0.2) // held 0.2s
	m.Update(0.2) // held 0.4s -> still < 1.0
	if hold01 == 0 {
		t.Fatal("OnHold(0.1) should have fired by 0.4s")
	}
	if hold10 != 0 {
		t.Fatalf("OnHold(1.0) should not fire at 0.4s, got %d", hold10)
	}
}

// a long press must not count as a tap even with a large OnHold set.
func TestLongPressNotTap(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("h")
	m.BindKey(a, KeySpace)
	taps, holds := 0, 0
	a.OnTap(func(Context) { taps++ })
	a.OnHold(2.0, func(Context) { holds++ })

	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	m.Update(0.75) // held 0.75s
	m.Update(0.75) // held 1.5s: > tapMax(0.22), < hold(2.0)
	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0)
	if taps != 0 {
		t.Fatalf("1.5s press must not be a tap, got %d", taps)
	}
}

// OnHold is frame-rate independent when scaled by ctx.Dt().
func TestHoldFrameRateIndependent(t *testing.T) {
	run := func(step float64) float64 {
		m, b := newTestManager()
		a := m.Action("h")
		m.BindKey(a, KeySpace)
		charge := 0.0
		a.OnHold(0.0, func(c Context) { charge += 10.0 * c.Dt() })

		b.push(Event{Kind: EventKeyDown, Key: KeySpace})
		// Hold for an exact total of 0.5s so the per-frame dt sum is identical
		// at every frame rate.
		n := int(0.5 / step)
		for range n {
			m.Update(step)
		}
		return charge
	}
	// Total held time is exactly 0.5s at every frame rate, so accumulation
	// scaled by ctx.Dt() yields the same charge regardless of fps.
	c30 := run(1.0 / 30.0)   // 15 frames * 1/30 * 10 == 5.0
	c240 := run(1.0 / 240.0) // 120 frames * 1/240 * 10 == 5.0
	if c30 < 4.9 || c30 > 5.1 {
		t.Fatalf("fps=30 charge out of range: %v", c30)
	}
	if c240 < 4.9 || c240 > 5.1 {
		t.Fatalf("fps=240 charge out of range: %v", c240)
	}
	if (c30-c240) > 1e-9 || (c240-c30) > 1e-9 {
		t.Fatalf("frame-rate dependent: c30=%v c240=%v", c30, c240)
	}
}

// double-tap re-triggers; six taps => three double-taps.
func TestDoubleTapContinuous(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("dt")
	m.BindKey(a, KeySpace)
	doubles := 0
	a.OnDoubleTap(func(Context) { doubles++ })

	tap := func() {
		b.push(Event{Kind: EventKeyDown, Key: KeySpace})
		m.Update(0.01)
		b.push(Event{Kind: EventKeyUp, Key: KeySpace})
		m.Update(0.01)
	}
	for range 6 {
		tap()
	}
	if doubles != 3 {
		t.Fatalf("six taps should yield three double-taps, got %d", doubles)
	}
}

// Rebind while held must not synthesize a release/tap.
func TestRebindWhileHeldNoFakeRelease(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("r")
	m.BindKey(a, KeyA)
	pressed, released, taps := 0, 0, 0
	a.OnPressed(func(Context) { pressed++ })
	a.OnReleased(func(Context) { released++ })
	a.OnTap(func(Context) { taps++ })

	b.push(Event{Kind: EventKeyDown, Key: KeyA})
	m.Update(0) // press A
	m.Rebind(a, KeyB)
	m.Update(0) // no synthetic release
	if pressed != 1 || released != 0 || taps != 0 {
		t.Fatalf("rebind while held: pressed=%d released=%d taps=%d", pressed, released, taps)
	}
}

// a press while disabled still advances the toggle state.
func TestDisabledAdvancesToggle(t *testing.T) {
	m, b := newTestManager()
	a := m.Action("t")
	m.BindKey(a, KeySpace)
	calls := 0
	a.OnToggle(func(active bool, _ Context) { calls++ })

	a.Disable()
	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0)

	if calls != 0 {
		t.Fatalf("disabled action should not deliver toggle callback, got %d", calls)
	}
	if !a.IsActive() {
		t.Fatal("toggle state should advance even while disabled")
	}
}

// Drain records derived events into a replayable stream.
func TestDrainRecordsEvents(t *testing.T) {
	m, b := newTestManager()
	m.SetRecording(true)
	a := m.Action("d")
	m.BindKey(a, KeySpace)
	a.OnPressed(func(Context) {})
	a.OnTap(func(Context) {})

	b.push(Event{Kind: EventKeyDown, Key: KeySpace})
	m.Update(0)
	b.push(Event{Kind: EventKeyUp, Key: KeySpace})
	m.Update(0.05)

	events := m.Drain()
	// Drain records every derived event, not just those with callbacks:
	// press (intrinsic), toggle (state flip), release (intrinsic), tap.
	if len(events) != 4 {
		t.Fatalf("expected 4 recorded events, got %d", len(events))
	}
	want := []ActionEventType{EventTypePressed, EventTypeToggle, EventTypeReleased, EventTypeTap}
	for i, w := range want {
		if events[i].Type != w {
			t.Fatalf("event %d = %v, want %v", i, events[i].Type, w)
		}
	}
	if events[0].Action != a {
		t.Fatal("recorded event should reference the Action")
	}

	// Drain clears the buffer.
	if len(m.Drain()) != 0 {
		t.Fatal("Drain should clear recorded events")
	}
}
