---
title: The render layer
tags: [engine, aqwabor, render]
---

# render — GPU Submission Layer

Single-path GPU API for game/world content. All drawing goes through the `GPU`
facade; game code never touches `wgpu.Buffer`, bind groups, or WGSL directly.

## Public API

### Construction

```go
gfx := render.New(dp)  // dp = win.DeviceProvider()
```

### Frame lifecycle

A frame has two phases. `Begin` captures the frame's command encoder and records
what the render pass will do; the first draw opens the pass. Compute work goes
between them, because a compute pass cannot be recorded inside a render pass.

```go
gfx.Begin(dc, render.Clear{R: 0.05, G: 0.05, B: 0.1, A: 1})
gfx.SetCamera(viewProj, viewportW, viewportH)

culled := gfx.CullSprites(batch, bounds)  // compute phase
gfx.DrawSpritesCulled(culled)             // opens the render pass
gfx.DrawStrokes(strokes)

gfx.End()
```

`SetCamera` writes a single shared camera uniform used by sprite, stroke,
and map pipelines. `BeginLoad` replaces `Begin` for a frame that keeps what is
already on the surface.

A frame that draws nothing opens no render pass.

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
// Encode the cull while only the encoder is open.
culled := gfx.CullSprites(batch, render.ViewBounds{
    MinX: -640, MinY: -360,
    MaxX:  640, MaxY:  360,
})
// The draw that consumes it opens the render pass.
gfx.DrawSpritesCulled(culled)
```

The cull pass runs as a compute shader on the GPU:
1. Reads all instances from the batch's InstanceBuffer.
2. Tests each against the camera frustum + optional world AABB.
3. Compacts visible survivors into an output buffer.
4. Draws with `DrawIndexedIndirect` using the GPU-written instance count.

When instances are off-screen, fewer are drawn than submitted.

**Ordering.** `CullSprites` has to be called after `Begin` and before the first
draw. Calling it once the render pass is open is refused and logged, rather than
being dropped silently the way it used to be: the encoder was only reachable
after the pass had opened, so the dispatch never ran, the indirect count stayed
at the zero the reset wrote, and the draw drew nothing.

**Eight culls a frame.** Each takes its own region of the output buffer, its own
draw command and its own parameters, so they are independent. A ninth in one
frame is refused and logged. A cull pipeline sized for N instances therefore
holds eight times N instances of output; that is the cost of the slots.

Growing the batch past the cull pipeline's capacity builds a larger pipeline and
retires the old one for three frames before releasing it, because a submitted
frame may still be reading its buffers.

### Cells (the dense layer: one colour per grid cell)

```go
cells := gfx.Subcells(capacity)
gfx.SetRamp(&table)        // once; every cell indexes it
gfx.SetCellSize(2, 2)      // world units, shared by the grid

cells.WriteAll(instances)
gfx.DrawSubcells(cells)
```

`SubcellInstance` is 16 bytes against the sprite path's 64: a world position and
a palette index, with the colour coming from a ramp table uploaded once and the
size from a uniform, because a grid shares one size. At 250,000 instances that
is 3.9 MB a frame rather than 15.6.

A palette index of zero draws nothing, so a cell can be cleared without being
taken out of the buffer. How materials and rows map onto a flat index belongs to
the game; the engine only indexes.

Use this for the layer with the most instances and an identity-shaped colour.
The sprite path stays right for anything carrying an arbitrary colour, a
rotation, or a per-instance size.

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
| `subcell.wgsl`    | Compact cell vertex: palette index + ramp    |
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
comps := render.MustRegisterECS(w)   // or render.RegisterECS(w) for an error
```

Registers all four component types and returns the handles for them. Keep what it returns: a handle
is the only way to reach a component, and it is not recoverable from the world afterwards.

### Spawning

```go
e := render.SpawnSprite(w, comps,
    render.Transform{X: 100, Y: 200, SX: 48, SY: 48},
    render.Color{R: 1, G: 0, B: 0, A: 1},
    render.Sprite{Layer: 0},
)
```

A spawned sprite is awake in the `Transform` store, because extraction walks the awake rows and a
sprite that is not in that set is not drawn.

### Extracting to batch

```go
batch := gfx.Sprites(1024)        // persistent, created once
render.ExtractSprites(comps, batch)
```

`ExtractSprites` walks the awake rows of `Transform` and takes `Sprite` per entity, with `Color`
where present and white where not. Zero scale defaults to 1.

Sleeping a sprite's `Transform` takes it out of the batch without destroying it, which is how a
sprite is hidden without costing anything per frame:

```go
comps.Transform.Sleep(e)
comps.Transform.Wake(e)
```

Writing past the batch capacity grows it rather than panicking, so a scene larger than the number
guessed at startup costs one reallocation.

### Drawing

```go
render.DrawWorld(gfx, batch, dc, clear, viewProj, viewW, viewH, bounds, 64)
```

Full-frame helper: `Begin` → `SetCamera` → cull and draw if the batch is at or above
`cullThreshold` → `End`. It follows the frame's two phases, so the cull is encoded before the render
pass opens.

### Sharing colours

Shared component instances are gone with the pool that backed them: one dense array per type has
nowhere to put a value two entities point at.

Where many instances carry the same colour because it is an identity rather than an arbitrary value,
that is what the palette index on the compact cell format is for. See the cell section above. For
sprites, write the colour on each; it is four floats in a buffer that is written densely anyway.

### ViewProjMap

```go
vpMat := render.ViewProjMap(camComp, viewW, viewH, worldScale)
```

Builds a column-major 4x4 orthographic matrix from a `camera.Camera`
component with a world scale factor baked in (for `mapdata` int32 coordinates).
Used by the world map demo; sprites use `camera.ViewProj` instead.
