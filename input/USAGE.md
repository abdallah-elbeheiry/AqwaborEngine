# Input System — Usage Guide

A high-level, ergonomic input system for AqwaborEngine. You only ever work
with **Actions**: create one, bind hardware to it, attach logic to it, and
control it directly. There is no separate subscription object.

## Mental model

```
Create Action  ->  Bind hardware  ->  Attach logic  ->  Enable / Disable / Unbind
```

- `Enable()` / `Disable()` stop or resume event delivery, but keep callbacks
  and bindings.
- `Unbind()` clears hardware bindings but keeps callbacks.
- `Rebind()` swaps bindings without destroying callbacks.
- Mouse position is always current and readable from `Context`.
- Behaviour (hold, tap, toggle, double-tap, combo, drag) lives inside the
  system — you never reimplement it per Action.

## Wire it up

```go
import (
    "aqwabor/input"
    "aqwabor/input/backend/gogpu"
    "aqwabor/input/backend/headless"
)

// Real game: feed the manager from a running gogpu App.
in := input.NewManager(gogpu.NewBackend(app))

// Tests / LLM playtesting: feed it synthetic events.
in := input.NewManager(headless.New())
```

Call `in.Update(dt)` once per frame from your main loop (single-threaded).

## Create and bind

```go
jump := in.Action("jump")
in.BindKey(jump, input.KeySpace)

menu := in.Action("open_menu")
in.BindKey(menu, input.KeyEscape)

selectAction := in.Action("select")
in.BindMouseButton(selectAction, input.MouseButtonLeft)

drag := in.Action("camera_drag")
in.BindMouseButton(drag, input.MouseButtonRight)

// A combo fires only when all keys are held together.
stealth := in.Combo("stealth_attack", input.KeyX, input.KeyV)
```

## Attach logic

```go
jump.OnPressed(func(ctx input.Context) {
    player.Jump()
})

jump.OnReleased(func(ctx input.Context) {
    player.StopJump()
})

jump.OnHold(0.4, func(ctx input.Context) {
    player.StartCharging()
})

jump.OnTap(func(ctx input.Context) {
    player.QuickStep()
})

jump.OnToggle(func(active bool, ctx input.Context) {
    player.SetCrouch(active)
})

jump.OnDoubleTap(func(ctx input.Context) {
    player.Dash()
})

jump.OnMultiTap(3, func(ctx input.Context) {
    player.SpinAttack()
})

selectAction.OnPressed(func(ctx input.Context) {
    x, y := ctx.MousePosition()
    world.Click(x, y)
})

drag.OnDrag(func(dx, dy float64, ctx input.Context) {
    camera.Pan(dx, dy)
})
```

## Control the Action (no subscription object)

```go
jump.Disable()   // stops delivery; callbacks + bindings remain
jump.Enable()    // resumes

jump.Unbind()    // clears hardware bindings; callbacks remain
in.Rebind(jump, input.KeyW) // new binding, callbacks unchanged
```

## Queries

```go
if in.IsDown(jump) {
    // jump is currently held
}

x, y := in.MousePosition() // always current
```

## Headless injection (tests & playtesting)

```go
backend := headless.New()
in := input.NewManager(backend)

backend.KeyDown(input.KeySpace)
in.Update(0.0)
backend.KeyUp(input.KeySpace)
in.Update(0.05) // a quick tap -> OnTap fires
```

Because timing is driven by the `dt` you pass to `Update`, headless scenarios
are fully deterministic.

## Notes on behaviour

- `OnHold` fires **every frame** the Action has been held past **its own**
  threshold. Each `OnHold` handler is gated on its own threshold, so
  `OnHold(0.1)` and `OnHold(1.0)` fire independently — the 1.0s handler does
  not raise the gate for the 0.1s handler.
- Because `OnHold` fires every frame, scale any accumulation by the frame
  delta so it is frame-rate independent:

  ```go
  charge := 0.0
  jump.OnHold(0.4, func(ctx input.Context) {
      charge += rate * ctx.Dt() // same total regardless of fps
  })
  ```

- `OnTap` fires on release when the press was shorter than the fixed tap
  window of `0.22s`. The tap window does **not** depend on any `OnHold`
  threshold, so a 1.5s press with an `OnHold(2.0)` is neither a tap nor a
  hold — a long press is simply not a tap.
- `OnDoubleTap` / `OnMultiTap` re-trigger: six quick taps yield three
  double-taps, and three quick taps yield one triple-tap. The tap count resets
  after each tap-level event.
- `OnToggle` advances its internal state on every press edge. A press while
  the Action is **disabled** still advances the toggle state; only the
  callback delivery is suppressed, so re-enabling yields a consistent logical
  state.
- `Unbind` and `Rebind` reset the pressed flag without synthesizing a release,
  so rebinding while a key is physically held does not emit a fake
  `OnReleased`/`OnTap`.
- `Combo` bindings do not currently consume the underlying keys, so a plain
  Action bound to one of the combo keys still fires alongside the combo.
  Add a consume/priority model before using modifier combos in a GUI.

## Command stream (Drain) vs callbacks

Callbacks are the ergonomic path for camera and UI. For the simulation,
prefer a deterministic command stream:

```go
in.SetRecording(true)

// ... in your fixed-step update:
in.Update(simDt) // simDt from the scheduler, not wall-clock frame time
for _, e := range in.Drain() {
    world.Dispatch(e) // bind events to simulation ticks
}
```

`Manager.Drain` returns and clears the derived events recorded since the last
call (`EventTypePressed`, `Released`, `Hold`, `Tap`, `Toggle`, `DoubleTap`,
`MultiTap`, `Drag`), each carrying the `Action`, `Now`, `Dt`, and any
per-event data (`Active`, `DX`/`DY`, `TapCount`). Recording is off by default
so the plain callback path stays allocation-free.

## Timing, determinism and replay

All derived events (tap, hold, double-tap) are timing based, so `Update` must
be driven by the **fixed simulation dt** (e.g. the scheduler's tick interval),
not real wall-clock frame time. The Manager keeps its own clock advanced by
that `dt`; if you feed it the scheduler's sim dt, the two clocks stay in sync
and replays are bit-for-bit reproducible.

Because timing is driven by the `dt` you pass to `Update`, headless scenarios
are fully deterministic.
