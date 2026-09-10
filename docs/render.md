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

`SetCamera` writes the one camera uniform every pipeline draws through. It is a
single buffer bound at group 0, so a pipeline added later needs no line in
`SetCamera` and a pipeline built outside the engine gets the camera for free:
build it against `GPU.CameraLayout()`, read `@group(0) @binding(0)`, and own no
camera of your own. Group 0 belongs to the engine; a pipeline's own bindings
start at group 1.

`BeginLoad` replaces `Begin` for a frame that keeps what is already on the
surface.

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

// Or one range of it, which is what a Scene submits: a range is the run of
// chunks a view covers.
gfx.DrawSpritesRange(batch, first, count)
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

// Over one range of the batch rather than all of it.
culled = gfx.CullSpritesRange(batch, first, count, bounds)
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

## The ECS side

The render package owns plain-old-data components and a `Scene`, which is the
layer between the world and the GPU. A game spawns entities and writes
components; nothing it touches names a buffer, a pipeline or a bind group.

### Components

| Component  | Fields                                          | Purpose               |
|------------|-------------------------------------------------|-----------------------|
| `Transform`| `X, Y, Rot, SX, SY float32`                     | 2D world-space pose   |
| `Color`    | `R, G, B, A float32`                            | RGBA colour           |
| `Sprite`   | `Layer float32, UVX, UVY float32, Flags uint32` | Draw bucket, UV offset|

```go
comps := render.MustRegisterECS(w)   // or render.RegisterECS(w) for an error
```

Keep what registration returns: a handle is the only way to reach a component,
and it is not recoverable from the world afterwards.

### Scene

```go
scene := render.NewScene(gfx, w, comps, render.SceneConfig{ChunkSize: 64})

e := scene.Spawn(
    render.Transform{X: 100, Y: 200, SX: 48, SY: 48},
    render.Color{R: 1, A: 1},
    render.Sprite{},
)

// Each frame:
scene.Sync()                 // write what changed
gfx.Begin(dc, clear)
gfx.SetCamera(camera.ViewProj(*cam, vpW, vpH), vpW, vpH)
scene.Draw(view)             // culls first, then draws; call between Begin and End
gfx.End()
```

A scene holds one instance buffer per layer, divided into chunks of world space.
A chunk is a contiguous range of that buffer, so a still chunk is never
rewritten and never walked; a view is the handful of ranges its chunks cover;
and what those ranges hold goes to the GPU cull, which drops what is inside a
visible chunk but outside the view. Coarse on the CPU, fine on the GPU, and
never per entity in Go.

Measured by `examples/scenedemo`, 10,000 sprites of which 64 move: 64 instance
writes a frame, and at a zoom where the view covers a tenth of the field, 1,040
of the 10,000 instances submitted in 2 draw calls.

### What has changed is told, not discovered

```go
comps.Transform.Wake(e)   // this entity has to be written again
scene.Drop(e)             // take it out; call this before destroying it
```

An awake `Transform` is one whose instance has to be written. `Sync` writes the
awake rows and puts them back to sleep, so a still world is a world with nothing
awake. Move an entity without waking it and it draws where it was.

There is no separate dirty list. Waking already means "this needs visiting"
everywhere else in the engine, and a second way of saying it is how two
subsystems come to disagree about what changed. The awake partition of
`Transform` belongs to the renderer; give a simulation its own components or its
own `ecs.Set`.

`Spawn` wakes what it creates, which is why it exists — setting a component does
not wake it, and an entity that is asleep is in nothing the scene walks.

`Drop` before `World.Destroy`. A destroyed handle cannot be added to a set, so
the scene has no way of being told after the fact; what covers a game that
forgets is the sweep, which checks one chunk a `Sync` and reclaims a dead
entity's slot within a bounded number of frames.

### Layers

`Sprite.Layer` is the draw bucket, clamped to `SceneConfig.Layers`. Each is its
own buffer and its own chunking, drawn in order.

### What a frame did

```go
s := scene.Stats()
// s.Written   instances rewritten by the last Sync; zero for a still world
// s.Rebuilt   layers laid out again, which is the expensive case
// s.Submitted instances the last Draw covered, before the GPU cull
// s.Draws     draw calls issued
```

`Rebuilt` is the number to watch. A layer is laid out again when a chunk runs
out of the slack it keeps for membership changes, and that writes every instance
in the layer.

### Sharing colours

Shared component instances are gone with the pool that backed them: one dense
array per type has nowhere to put a value two entities point at.

Where many instances carry the same colour because it is an identity rather than
an arbitrary value, that is what the palette index on the compact cell format is
for. For sprites, write the colour on each; it is four floats in a buffer that is
written densely anyway.
