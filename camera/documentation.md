# camera — 2D View Transform

Minimal, widget-independent 2D view transform. Maps between world coordinates
and local viewport coordinates.

## Public API

### Construction

```go
cam := camera.NewCamera()
```

### Coordinate conversion

```go
local := cam.WorldToLocal(world, vp)   // world → viewport pixels
world := cam.LocalToWorld(local, vp)   // viewport pixels → world
```

### Pan / zoom

```go
cam.Pan(delta)                                    // delta in local viewport pixels
cam.ZoomAt(factor, cursorLocal, vp)               // zoom keeping cursor point fixed
cam.SetZoom(z)                                     // clamped to [minZoom, maxZoom]
cam.SetZoomLimits(min, max)
```

### Fit / clamp

```go
cam.Fit(worldSize, vp)         // center + best-fit zoom
cam.ClampToBounds(worldSize, vp)  // keep visible region inside world
```

## ECS Integration

### Camera2D Component

```go
camera.MustRegisterECS(w)
```

Registers the `Camera2D` component type. Spawn a camera entity:

```go
camEntity := w.Create()
ecs.MustAdd[camera.Camera2D](w, camEntity, camera.Camera2D{
    X: 640, Y: 360,
    Zoom:    1,
    MinZoom: 0.1,
    MaxZoom: 10,
    Active:  1,
})
```

### ViewProjFrom

```go
vpMat := camera.ViewProjFrom(camComp, viewW, viewH)
```

Builds a column-major 4x4 orthographic view-projection matrix from a `Camera2D`
component. Returns `[16]float32` suitable for `render.GPU.SetCamera`.

### ClampZoom

```go
camera.ClampZoom(&camComp)
```

Clamps `Zoom` to `[MinZoom, MaxZoom]`. Call after manual zoom mutations.

### Typical frame loop

```go
cam, _ := ecs.Get[camera.Camera2D](w, camEntity)
vpMat := camera.ViewProjFrom(*cam, 1280, 720)
render.DrawWorld(gfx, batch, dc, clear, vpMat, 1280, 720, bounds, 64)
```
