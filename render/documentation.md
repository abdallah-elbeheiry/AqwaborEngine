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

### Sprites / instances (primary path)

```go
// Create a mesh (shared unit quad).
quad := render.NewUnitQuad(gfx.Device(), gfx.Queue())

// Create an instance buffer.
instances := render.NewInstanceBuffer(gfx.Device(), capacity)

// Write instance data.
instances.WriteAll([]render.InstanceData{{
    Position: [2]float32{x, y},
    Scale:    [2]float32{w, h},
    Color:    [4]float32{r, g, b, a},
}})

// Draw (CPU → GPU, all instances visible).
gfx.DrawInstanced(quad, instances)
```

### GPU compute culling + indirect draw

```go
// Draw only instances visible within viewBounds.
// The CullPipeline is managed internally; game code does not touch it.
gfx.DrawSpritesCulled(quad, instances, render.ViewBounds{
    MinX: -640, MinY: -360,
    MaxX:  640, MaxY:  360,
})
```

The cull pass runs as a compute shader on the GPU:
1. Reads all instances from the InstanceBuffer.
2. Tests each against the camera frustum + optional world AABB.
3. Compacts visible survivors into an output buffer.
4. Draws with `DrawIndexedIndirect` using the GPU-written instance count.

When instances are off-screen, fewer are drawn than submitted. Stats
(`gfx.Stats().Instances`) reflect the actual drawn count.

### Strokes (polylines: map outlines, paths, debug)

```go
strokes := render.NewStrokeBuffer(gfx.Device(), capacity)
strokes.WriteAll(segments)
gfx.DrawStrokes(strokes)
```

Stroke width is in **screen pixels** (constant under zoom), resolved in the
stroke vertex shader.

### Map fills (static geometry)

```go
gfx.DrawMapMesh(mapMesh, mapPipeline)
```

Pre-built triangulated map geometry with int32 world-space positions and
quantized colors. Used by `maprender`.

### Accessors

```go
gfx.Device()          // *wgpu.Device
gfx.Queue()           // *wgpu.Queue
gfx.SurfaceFormat()   // gputypes.TextureFormat
gfx.Stats()           // render.FrameStats{DrawCalls, Instances, Triangles}
gfx.Renderer()        // *render.Renderer (escape hatch for maprender)
```

### Cleanup

```go
gfx.Release()
```

## Types

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

Shared immutable geometry (vertex + index buffers). Create with
`NewMesh(dev, queue, vertices, indices)` or `NewUnitQuad(dev, queue)`.

### InstanceBuffer

Long-lived GPU buffer with dirty-range tracking. Supports sparse `Write` for
per-slot updates or dense `WriteAll`/`WriteAt` for full-buffer rewrites.
`Flush` uploads dirty ranges to the GPU; `DrawInstanced` calls it
automatically.

### StrokeSegment (72 bytes)

Per-segment data for GPU-expanded polylines. The vertex shader expands each
segment into a screen-space quad with miter joins.

### CullPipeline (internal)

GPU compute culling pipeline. Created lazily by `GPU.DrawSpritesCulled`.
Not part of the public API — game code should not import or reference this type.

## Shaders

| Shader           | Purpose                                      |
|------------------|----------------------------------------------|
| `instanced.wgsl` | Instanced quads: mesh + instance + camera    |
| `cull.wgsl`      | Compute: frustum/AABB cull + compact         |
| `stroke.wgsl`    | Segment expansion, screen-space width        |
| `map.wgsl`       | Map fills: int32 positions + quantized color |
| `fragment.wgsl`  | Shared fragment passthrough (map pipeline)    |
| `instanced_frag.wgsl` | Instanced fragment output              |
| `stroke_frag.wgsl`    | Stroke fragment output                 |

## ECS Integration

Components remain PoD only (no GPU pointers). A small system or demo
function queries transforms and writes into render batches:

```go
// System: query transforms → batch.Set → DrawSprites
query.ForEach(func(e ecs.Entity) {
    t := ecs.Get[Transform](e)
    s := ecs.Get[SpriteStyle](e)
    batch.Set(i, render.InstanceData{
        Position: t.Position,
        Scale:    s.Size,
        Color:    s.Color,
    })
})
gfx.DrawInstanced(quad, batch)
```
