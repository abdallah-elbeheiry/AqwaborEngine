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

Components remain PoD only (no GPU pointers). A small system or demo
function queries transforms and writes into render batches:

```go
batch := gfx.Sprites(1024)
i := 0
query.ForEach(func(e ecs.Entity) {
    t := ecs.Get[Transform](e)
    s := ecs.Get[SpriteStyle](e)
    batch.Set(i, render.InstanceData{
        Position: t.Position,
        Scale:    s.Size,
        Color:    s.Color,
    })
    i++
})
batch.SetCount(i)
gfx.DrawSprites(batch)
```
