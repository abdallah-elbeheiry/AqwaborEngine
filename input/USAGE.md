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

- `OnHold` fires **every frame** the Action has been held past its threshold.
- `OnTap` fires on release when the press was shorter than the effective tap
  window — the largest registered `OnHold` threshold, or `0.22s` by default —
  so a short press is a tap and a long press is a hold with no dead zone.
- `OnDoubleTap` / `OnMultiTap` require taps within `0.30s` of each other.
- `OnToggle` flips an internal state on every press edge.
- Disabled Actions keep their input state up to date, so re-enabling never
  emits a stale press.
