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
vpMat := camera.ViewProj(*c, vpW, vpH)
```

It returns a `[16]float32` column-major 4x4 for `render.GPU.SetCamera`.

World Y runs down the screen, the way a cell grid reads and the way
`WorldToLocal` treats it, and clip Y runs up, so the matrix's Y scale is
negative. The camera's own position lands at clip 0 on both axes; anything else
is a bug.

A world whose Y runs up, or whose coordinates carry a scale divisor, needs its
own matrix. `examples/mapdemo` builds one: degrees times a scale, latitude
running up, so the scale stays positive and the translation is negated.

### A frame

```go
c, _ := camComp.Get(camE)

c.Pan(dx, dy)                    // input writes into Camera
c.ZoomAt(factor, mx, my, vpW, vpH)
camera.ClampZoom(c)

gfx.Begin(dc, render.Clear{R: 0.04, G: 0.05, B: 0.08, A: 1})
gfx.SetCamera(camera.ViewProj(*c, vpW, vpH), vpW, vpH)
scene.Draw(view)                 // view is the world rectangle the camera covers
gfx.End()
```
