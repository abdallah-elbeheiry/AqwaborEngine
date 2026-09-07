package camera

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

// Camera2D is an ECS-friendly 2D camera component. It stores the same
// information as Camera but as a plain-old-data struct suitable for
// component registration and handle-based sharing.
type Camera2D struct {
	X, Y    float32
	Zoom    float32
	MinZoom float32
	MaxZoom float32
	Active  uint8 // 1 = primary camera
}

// RegisterECS registers the Camera2D component type with the given world.
func RegisterECS(w *ecs.World) error {
	return ecs.Register[Camera2D](w)
}

// MustRegisterECS is like RegisterECS but panics on error.
func MustRegisterECS(w *ecs.World) {
	if err := RegisterECS(w); err != nil {
		panic("camera.RegisterECS: " + err.Error())
	}
}

// ViewProjFrom builds a column-major 4x4 orthographic view-projection matrix
// from a Camera2D component and viewport size. The matrix maps raw world
// coordinates to clip space, with the camera centred in the viewport.
func ViewProjFrom(c Camera2D, viewW, viewH float32) [16]float32 {
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

	sx := zoom * 2 / vpW
	sy := zoom * 2 / vpH
	tx := -c.X * zoom * 2 / vpW
	ty := c.Y * zoom * 2 / vpH

	return [16]float32{
		sx, 0, 0, 0,
		0, sy, 0, 0,
		0, 0, 1, 0,
		tx, ty, 0, 1,
	}
}

// ClampZoom clamps the Zoom field of c to [MinZoom, MaxZoom].
func ClampZoom(c *Camera2D) {
	if c.MinZoom > 0 && c.Zoom < c.MinZoom {
		c.Zoom = c.MinZoom
	}
	if c.MaxZoom > 0 && c.Zoom > c.MaxZoom {
		c.Zoom = c.MaxZoom
	}
}
