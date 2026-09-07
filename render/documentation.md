# render — GPU Submission Layer

Single-path GPU API for game/world content. All drawing goes through the `GPU`
facade; game code never touches `wgpu.Buffer`, bind groups, or WGSL directly.

## Public API

### Construction

```go
gfx := render.New(dp)  // dp = win.DeviceProvider()
```

### Frame lifecycle

```go
gfx.Begin(dc, render.Clear{R: 0.05, G: 0.05, B: 0.1, A: 1})
gfx.SetCamera(viewProj, viewportW, viewportH)
// ... draw calls ...
gfx.End()
```

`SetCamera` writes a single shared camera uniform used by sprite, stroke,
and map pipelines.

### Sprites / instances (primary path)

```go
batch := gfx.Sprites(capacity)

// Write instance data.
batch.Set(index, render.InstanceData{
    Position: [2]float32{x, y},
    Scale:    [2]float32{w, h},
    Color:    [4]float32{r, g, b, a},
})
batch.SetAll(sprites) // dense write, one GPU upload

// Draw (CPU → GPU, all instances visible).
gfx.DrawSprites(batch)
```

### GPU compute culling + indirect draw

```go
// Draw only instances visible within viewBounds.
gfx.DrawSpritesCulled(batch, render.ViewBounds{
    MinX: -640, MinY: -360,
    MaxX:  640, MaxY:  360,
})
```

The cull pass runs as a compute shader on the GPU:
1. Reads all instances from the batch's InstanceBuffer.
2. Tests each against the camera frustum + optional world AABB.
3. Compacts visible survivors into an output buffer.
4. Draws with `DrawIndexedIndirect` using the GPU-written instance count.

When instances are off-screen, fewer are drawn than submitted.

### Strokes (polylines: map outlines, paths, debug)

```go
strokes := render.NewStrokeBuffer(gfx.Device(), capacity)
strokes.WriteAll(segments)
gfx.DrawStrokes(strokes)
```

Stroke width is in **screen pixels** (constant under zoom), resolved in the
stroke vertex shader.

### Map fills (static geometry)

Map fills are drawn through the Renderer directly (not through the GPU
facade's public API). The map pipeline shares the same camera uniform
as sprite and stroke pipelines, updated by `GPU.SetCamera`.

### Accessors

```go
gfx.Stats()    // render.FrameStats{DrawCalls, Instances, Triangles}
```

### Cleanup

```go
gfx.Release()
```

## Types

### SpriteBatch

Convenience wrapper around InstanceBuffer. Created via `gfx.Sprites(cap)`.
Provides `Set`, `SetAll`, `Count`, `Reset`. Hides raw mesh/buffer management.

### InstanceData (64 bytes)

Per-instance data layout. Stride matches WGSL storage alignment (64 bytes).

| Offset | Size | Field    | Description          |
|--------|------|----------|----------------------|
| 0      | 8    | Position | vec2 world position   |
| 8      | 8    | Scale    | vec2 size in pixels   |
| 16     | 4    | Rotation | radians               |
| 32     | 16   | Color    | vec4 RGBA             |
| 48     | 8    | UVOffset | vec2 texture offset   |
| 56     | 4    | Layer    | draw order layer      |

### Mesh

Shared immutable geometry (vertex + index buffers). Created internally by
`GPU.Sprites()`. Not part of the public demo API.

### StrokeSegment (72 bytes)

Per-segment data for GPU-expanded polylines. The vertex shader expands each
segment into a screen-space quad with miter joins.

## Shaders (5 files)

| File              | Purpose                                      |
|-------------------|----------------------------------------------|
| `cull.wgsl`       | Compute: frustum/AABB cull + compact         |
| `instanced.wgsl`  | Sprite/instance vertex: mesh + transform     |
| `stroke.wgsl`     | Stroke vertex: screen-space expansion        |
| `map.wgsl`        | Map fill vertex: int32 positions + camera    |
| `fragment.wgsl`   | Shared fragment passthrough for all pipelines |

## ECS Integration

The render package owns plain-old-data components and extract/draw helpers.
No GPU pointers live in components — the ECS enforces PoD registration.

### Components

| Component  | Fields                                    | Purpose                    |
|------------|-------------------------------------------|----------------------------|
| `Transform`| `X, Y, Rot, SX, SY float32`             | 2D world-space pose        |
| `Color`    | `R, G, B, A float32`                     | RGBA colour                |
| `Sprite`   | `Layer float32, UVX, UVY float32, Flags uint32` | Draw order + metadata |
| `ClearColor`| `R, G, B, A float32`                    | Per-frame background       |

### Registration

```go
render.MustRegisterECS(w)  // or render.RegisterECS(w) for error return
```

Registers all four component types. Call once at startup before spawning.

### Spawning

```go
e := render.SpawnSprite(w,
    render.Transform{X: 100, Y: 200, SX: 48, SY: 48},
    render.Color{R: 1, G: 0, B: 0, A: 1},
    render.Sprite{Layer: 0},
)
```

### Extracting to batch

```go
batch := gfx.Sprites(1024)  // persistent, created once
render.ExtractSprites(w, batch)  // fills batch from all Transform+Sprite entities
```

`ExtractSprites` iterates all entities with `Transform` + `Sprite` (optionally
`Color`; defaults to white). Zero-scale defaults to 1.

### Drawing

```go
render.DrawWorld(gfx, batch, dc, clear, viewProj, viewW, viewH, bounds, 64)
```

Full-frame helper: `SetCamera` → `Begin` → draw (with GPU cull if
`batch.Count() >= cullThreshold`) → `End`.

### Sharing colours

Use `ecs.Create[Color]` + `ecs.Attach[Color]` to share one colour instance
across many sprite entities — no per-frame alloc.
