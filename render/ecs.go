package render

import (
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
// Attach a single shared Handle to many entities to batch by colour.
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

// RegisterECS registers the render component types with the given world.
// Call once during startup before spawning any renderable entities.
func RegisterECS(w *ecs.World) error {
	if err := ecs.Register[Transform](w); err != nil {
		return err
	}
	if err := ecs.Register[Color](w); err != nil {
		return err
	}
	if err := ecs.Register[Sprite](w); err != nil {
		return err
	}
	if err := ecs.Register[ClearColor](w); err != nil {
		return err
	}
	return nil
}

// MustRegisterECS is like RegisterECS but panics on error.
func MustRegisterECS(w *ecs.World) {
	if err := RegisterECS(w); err != nil {
		panic("render.RegisterECS: " + err.Error())
	}
}

// --- Extract / Draw helpers ---

// ExtractSprites fills batch from all entities that have Transform + Sprite
// (+ optional Color). Call once per frame before DrawWorld.
func ExtractSprites(w *ecs.World, batch *SpriteBatch) {
	batch.Reset()
	i := 0
	g := ecs.NewGroup(w, Transform{}, Sprite{})
	g.ForEach(func(e ecs.Entity) {
		t, _ := ecs.Get[Transform](w, e)
		s, _ := ecs.Get[Sprite](w, e)
		inst := InstanceData{
			Position: [2]float32{t.X, t.Y},
			Rotation: t.Rot,
			Layer:    s.Layer,
			UVOffset: [2]float32{s.UVX, s.UVY},
		}
		if c, ok := ecs.Get[Color](w, e); ok {
			inst.Color = [4]float32{c.R, c.G, c.B, c.A}
		} else {
			inst.Color = [4]float32{1, 1, 1, 1}
		}
		scaleX := t.SX
		scaleY := t.SY
		if scaleX == 0 {
			scaleX = 1
		}
		if scaleY == 0 {
			scaleY = 1
		}
		inst.Scale = [2]float32{scaleX, scaleY}
		batch.Set(i, inst)
		i++
	})
}

// DrawWorld performs a full frame: Begin → SetCamera → draw sprites → End.
// If cullThreshold > 0 and the batch is at or above that count, GPU culling
// is used instead of a plain draw.
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
	gfx.SetCamera(viewProj, viewW, viewH)
	if err := gfx.Begin(dc, clear); err != nil {
		return
	}
	if cullThreshold > 0 && batch.Count() >= cullThreshold {
		gfx.DrawSpritesCulled(batch, bounds)
	} else {
		gfx.DrawSprites(batch)
	}
	gfx.End()
}

// SpawnSprite creates an entity with Transform + Color + Sprite components
// and returns the new entity handle.
func SpawnSprite(w *ecs.World, t Transform, c Color, s Sprite) ecs.Entity {
	e := w.Create()
	ecs.MustAdd[Transform](w, e, t)
	ecs.MustAdd[Color](w, e, c)
	ecs.MustAdd[Sprite](w, e, s)
	return e
}
