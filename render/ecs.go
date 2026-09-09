package render

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
	"github.com/gogpu/gogpu"
)

// --- ECS Components (plain-old-data, no pointers/slices/maps) ---

// Transform stores the 2D world-space position, rotation, and scale of a
// sprite entity. Default scale is (1, 1).
type Transform struct {
	X, Y   float32
	Rot    float32
	SX, SY float32
}

// Color is an RGBA colour component. Values are typically in [0, 1].
type Color struct {
	R, G, B, A float32
}

// Sprite holds rendering metadata: draw layer, UV offset, and flags.
type Sprite struct {
	Layer float32
	UVX   float32
	UVY   float32
	Flags uint32
}

// ClearColor optionally stores the per-frame background clear colour.
type ClearColor struct {
	R, G, B, A float32
}

// --- Registration ---

// Components holds the handles for the render component types. Registration
// returns it and a caller keeps it, because a handle is the only way to reach a
// component value and it is what makes access an array index rather than a
// lookup.
type Components struct {
	Transform  ecs.Comp[Transform]
	Color      ecs.Comp[Color]
	Sprite     ecs.Comp[Sprite]
	ClearColor ecs.Comp[ClearColor]
}

// RegisterECS registers the render component types with the world. Call once
// during startup, before spawning anything renderable.
func RegisterECS(w *ecs.World) (Components, error) {
	var c Components
	var err error
	if c.Transform, err = ecs.Register[Transform](w); err != nil {
		return c, err
	}
	if c.Color, err = ecs.Register[Color](w); err != nil {
		return c, err
	}
	if c.Sprite, err = ecs.Register[Sprite](w); err != nil {
		return c, err
	}
	if c.ClearColor, err = ecs.Register[ClearColor](w); err != nil {
		return c, err
	}
	return c, nil
}

// MustRegisterECS is RegisterECS, panicking on error.
func MustRegisterECS(w *ecs.World) Components {
	c, err := RegisterECS(w)
	if err != nil {
		panic("render.RegisterECS: " + err.Error())
	}
	return c
}

// --- Extract / Draw helpers ---

// ExtractSprites fills batch from every awake entity holding Transform and
// Sprite, taking Color where present and white where not.
//
// It walks the Transform store's dense array, so the cost is one pass over
// contiguous memory plus a lookup per entity into the two other stores. It
// stops at the batch's capacity rather than writing past it.
func ExtractSprites(c Components, batch *SpriteBatch) {
	batch.Reset()
	capacity := batch.Capacity()
	i := 0
	ecs.Each2(c.Transform, c.Sprite, func(e ecs.Entity, t *Transform, s *Sprite) {
		if i >= capacity {
			return
		}
		inst := InstanceData{
			Position: [2]float32{t.X, t.Y},
			Rotation: t.Rot,
			Layer:    s.Layer,
			UVOffset: [2]float32{s.UVX, s.UVY},
			Color:    [4]float32{1, 1, 1, 1},
		}
		if col, ok := c.Color.Get(e); ok {
			inst.Color = [4]float32{col.R, col.G, col.B, col.A}
		}
		sx, sy := t.SX, t.SY
		if sx == 0 {
			sx = 1
		}
		if sy == 0 {
			sy = 1
		}
		inst.Scale = [2]float32{sx, sy}
		batch.Set(i, inst)
		i++
	})
}

// DrawWorld performs a full frame: camera, begin, cull, draw, end. With a
// positive cullThreshold and a batch at or above it, the GPU cull pass runs.
//
// The order here is the frame's two phases: the cull is encoded while only the
// command encoder is open, and the draw that consumes it opens the render pass.
func DrawWorld(
	gfx *GPU,
	batch *SpriteBatch,
	dc *gogpu.Context,
	clear Clear,
	viewProj [16]float32,
	viewW, viewH float32,
	bounds ViewBounds,
	cullThreshold int,
) {
	if err := gfx.Begin(dc, clear); err != nil {
		return
	}
	gfx.SetCamera(viewProj, viewW, viewH)
	if cullThreshold > 0 && batch.Count() >= cullThreshold {
		culled := gfx.CullSprites(batch, bounds)
		gfx.DrawSpritesCulled(culled)
	} else {
		gfx.DrawSprites(batch)
	}
	gfx.End()
}

// SpawnSprite creates an entity carrying Transform, Color and Sprite, awake in
// the Transform store so ExtractSprites sees it.
func SpawnSprite(w *ecs.World, c Components, t Transform, col Color, s Sprite) ecs.Entity {
	e := w.Create()
	c.Transform.Set(e, t)
	c.Color.Set(e, col)
	c.Sprite.Set(e, s)
	c.Transform.Wake(e)
	return e
}

// ViewProjMap builds a column-major 4x4 orthographic view-projection matrix
// from a Camera component, viewport size, and world scale. The world scale
// factor (from mapdata) is baked into the matrix so the map shader does a
// single matrix multiply on int32 world coordinates.
func ViewProjMap(c camera.Camera, viewW, viewH, worldScale float32) [16]float32 {
	if worldScale == 0 {
		worldScale = 1
	}
	zoom := c.Zoom
	if zoom == 0 {
		zoom = 1
	}
	vpW := viewW
	vpH := viewH
	if vpW == 0 {
		vpW = 1
	}
	if vpH == 0 {
		vpH = 1
	}

	sx := zoom * 2 / vpW / worldScale
	sy := zoom * 2 / vpH / worldScale
	tx := -c.X * zoom * 2 / vpW
	ty := c.Y * zoom * 2 / vpH

	return [16]float32{
		sx, 0, 0, 0,
		0, sy, 0, 0,
		0, 0, 1, 0,
		tx, ty, 0, 1,
	}
}
