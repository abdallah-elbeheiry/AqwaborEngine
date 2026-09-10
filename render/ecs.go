package render

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
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

// --- Registration ---

// Components holds the handles for the render component types. Registration
// returns it and a caller keeps it, because a handle is the only way to reach a
// component value and it is what makes access an array index rather than a
// lookup.
type Components struct {
	Transform ecs.Comp[Transform]
	Color     ecs.Comp[Color]
	Sprite    ecs.Comp[Sprite]
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
