---
title: Camera
tags: [engine, aqwabor, camera]
---

# camera — 2D View Transform

Minimal, widget-independent 2D view transform. `Camera` is the single source
of truth for view state — position, zoom, and limits. It is an ECS component
with exported PoD fields.

## Public API

### Construction / Registration

```go
camera.MustRegisterECS(w)  // register Camera component type
```

Spawn a camera entity:

```go
camE := w.Create()
camComp := camera.MustRegisterECS(w)
camE := w.Create()
camComp.Set(camE, camera.Camera{
    X: 180, Y: 90,
    MinZoom: 0.01,
    MaxZoom: 1000,
    Active:  1,
})
c, _ := camComp.Get(camE)
c.Fit(360, 180, 1280, 720)  // center + best-fit zoom
```

### Pan / zoom

```go
c.Pan(dx, dy)                                            // delta in local viewport pixels
c.ZoomAt(factor, cursorX, cursorY, vpW, vpH)             // zoom keeping cursor point fixed
camera.ClampZoom(c)                                      // clamp Zoom to [MinZoom, MaxZoom]
```

### Fit / clamp

```go
c.Fit(worldW, worldH, vpW, vpH)                // center + best-fit zoom
c.ClampToBounds(worldW, worldH, vpW, vpH)      // keep visible region inside world
```

### Coordinate conversion

```go
lx, ly := c.WorldToLocal(wx, wy, vpW, vpH)     // world → viewport pixels
wx, wy := c.LocalToWorld(lx, ly, vpW, vpH)     // viewport pixels → world
```

### View-projection

```go
vpMat := camera.ViewProj(*c, vpW, vpH)                   // sprites (no world scale)
vpMat := render.ViewProjMap(*c, vpW, vpH, worldScale)     // map (with world scale)
```

Both return `[16]float32` column-major 4x4 for `render.GPU.SetCamera`.

### Typical world demo frame

```go
c, _ := camComp.Get(camE)
scene, _ := scenes.Component().Get(mapE)

c.Pan(dx, dy)                    // input writes into Camera
c.ZoomAt(factor, mx, my, vpW, vpH)
camera.ClampZoom(c)

vpMat := render.ViewProjMap(*c, vpW, vpH, scene.WorldScale)
gfx.SetCamera(vpMat, vpW, vpH)
gfx.Begin(dc, render.Clear{R: scene.ClearR, ...})
scenes.Draw(mapE)
gfx.End()
```
