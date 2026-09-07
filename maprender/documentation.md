# maprender — Map Data to GPU

Triangulates ring fills from `mapdata` and builds GPU-expanded stroke
centerlines. Draws through `render.Renderer` (fills via dedicated map
pipeline, strokes via stroke pipeline).

## Public API

### Construction

```go
rend := maprender.NewRenderer(world, refZoom, ren)
```

- `world` — loaded `*mapdata.World`
- `refZoom` — `float32` reference zoom for stroke width calculation
- `ren` — `*render.Renderer` (set later via `SetRenderer` if GPU not yet init)

### GPU init

```go
rend.SetRenderer(gfx.Renderer())  // builds GPU mesh + strokes; must be called once
rend.SetViewport(vp)
```

### Drawing

```go
rend.Draw()  // records fills + strokes into existing render pass
```

The caller owns `Begin`/`End`. `Draw` only records draw calls.

## ECS Integration

### MapScene Component

```go
maprender.MustRegisterECS(w)
```

Registers the `MapScene` component (clear colour, world scale, active flag).

```go
mapE := w.Create()
ecs.MustAdd[maprender.MapScene](w, mapE, maprender.MapScene{
    ClearR: world.Background.R,
    ClearG: world.Background.G,
    ClearB: world.Background.B,
    ClearA: world.Background.A,
    WorldScale: float32(world.Scale),
    Active: 1,
})
```

### Bind / Unbind

Heavy resources (renderer, mesh, strokes) live outside ECS in the `Renderer`.
`Bind` associates an entity with its renderer:

```go
maprender.Bind(mapE, rend)
maprender.Unbind(mapE)  // cleanup
```

### DrawECS

```go
maprender.DrawECS(w, mapE)
```

Reads `MapScene` from ECS, looks up bound renderer, calls `Draw()`. The caller
must own the render pass (`Begin`/`End`).

### Typical world demo frame

```go
cam, _ := ecs.Get[camera.Camera](w, camE)
scene, _ := ecs.Get[maprender.MapScene](w, mapE)

vpMat := render.ViewProjMap(*cam, vp.Width, vp.Height, scene.WorldScale)
gfx.SetCamera(vpMat, vp.Width, vp.Height)
gfx.Begin(dc, render.Clear{R: scene.ClearR, G: scene.ClearG, B: scene.ClearB, A: scene.ClearA})
maprender.DrawECS(w, mapE)
gfx.End()
```
