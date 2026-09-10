---
title: Map rendering
tags: [engine, aqwabor, maprender, example]
---

# maprender - map data to the GPU

**This is an example, not engine API.** It lives in `examples/maprender` and
`examples/mapdemo`, because a map is one game's need rather than every game's.
The engine gained what it needed to draw one; the map itself moved out.

It is worth reading as the worked example of a pipeline built outside the
engine: it triangulates ring fills from `examples/mapdata`, builds GPU-expanded
stroke centrelines, and draws through `render.GPU` with its own vertex format.

## Building the geometry

```go
mesh := maprender.BuildMapMesh(dev, fillGeoms, passes, maprender.MapMeshConfig{})
strokes, _ := maprender.BuildMapStrokes(dev, strokeGeoms, widthPx, minSegmentPx)
```

`FillGeometry`, `StrokeGeometry` and `DrawPassSpec` are what the caller fills in
from its own data: coordinates, a rank, and the fill and stroke colours of each
draw pass.

## The pipeline

```go
mapPipe := maprender.NewMapPipeline(gfx.Device(), gfx.SurfaceFormat(), gfx.CameraLayout())
```

It lists the engine's camera layout as group 0 and reads the camera at
`@group(0) @binding(0)`, so it owns no camera buffer and needs no update of its
own: `gfx.SetCamera` covers it along with everything else. That is the pattern
for any pipeline built outside the engine.

Positions are int32 world coordinates. The view-projection matrix carries the
scale divisor, so the shader multiplies and nothing converts per vertex.

## Level of detail

```go
maxRank := maprender.RankForZoom(cam.Zoom)
count := pass.CountFor(maxRank)
```

Geometry within a pass is emitted in ascending rank order, so everything up to a
rank is a prefix of the pass's range. A level of detail is then a smaller count
rather than a per-frame cull: the more ground a pixel covers, the fewer minor
rivers and lakes are drawn.

## A frame

```go
cam, _ := camComp.Get(camE)
vpMat := viewProjMap(*cam, vp.Width, vp.Height, world.Scale)

gfx.Begin(dc, render.Clear{R: world.Background.R, G: world.Background.G, B: world.Background.B, A: 1})
gfx.SetCamera(vpMat, vp.Width, vp.Height)   // the map pipeline reads this too

for _, pass := range mesh.Passes {
    gfx.DrawVerticesRange(mapPipe.Pipeline(), mesh.Buffer, pass.CountFor(maxRank), pass.Offset)
}
gfx.DrawStrokes(strokes)
gfx.End()
```

`examples/mapdemo` is the whole of it, including the matrix: the map's Y runs
up, unlike the engine's default, so it builds its own rather than using
`camera.ViewProj`. See [camera](camera.md).
