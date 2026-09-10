package camera

import "github.com/abdallah-elbeheiry/AqwaborEngine/ecs"

// Camera is an ECS 2D camera component and the single source of truth for
// all view state (position, zoom, limits).
type Camera struct {
	X, Y    float32
	Zoom    float32
	MinZoom float32
	MaxZoom float32
	Active  uint8 // 1 = primary camera
}

// RegisterECS registers the Camera component type and returns the handle used
// to reach it. The handle is the only way to read or write a Camera, so a
// caller keeps what this returns.
func RegisterECS(w *ecs.World) (ecs.Comp[Camera], error) {
	return ecs.Register[Camera](w)
}

// MustRegisterECS is RegisterECS, panicking on error.
func MustRegisterECS(w *ecs.World) ecs.Comp[Camera] {
	c, err := RegisterECS(w)
	if err != nil {
		panic("camera.RegisterECS: " + err.Error())
	}
	return c
}

// ViewProj builds a column-major 4x4 orthographic view-projection matrix from
// a Camera component and viewport size.
func ViewProj(c Camera, vpW, vpH float32) [16]float32 {
	zoom := c.Zoom
	if zoom == 0 {
		zoom = 1
	}
	if vpW == 0 {
		vpW = 1
	}
	if vpH == 0 {
		vpH = 1
	}
	// World Y runs down the screen, the way a cell grid reads and the way
	// WorldToLocal already treats it; clip Y runs up. So the Y scale is
	// negative, and with it the camera's own position lands at clip 0 on both
	// axes. Without it clip_y worked out as (wy + c.Y) rather than (c.Y - wy),
	// which put a fitted scene off the top of the screen.
	sx := zoom * 2 / vpW
	sy := -zoom * 2 / vpH
	tx := -c.X * zoom * 2 / vpW
	ty := c.Y * zoom * 2 / vpH
	return [16]float32{
		sx, 0, 0, 0,
		0, sy, 0, 0,
		0, 0, 1, 0,
		tx, ty, 0, 1,
	}
}

// ClampZoom clamps c.Zoom to [MinZoom, MaxZoom].
func ClampZoom(c *Camera) {
	if c.MinZoom > 0 && c.Zoom < c.MinZoom {
		c.Zoom = c.MinZoom
	}
	if c.MaxZoom > 0 && c.Zoom > c.MaxZoom {
		c.Zoom = c.MaxZoom
	}
}

// Pan moves the view by (dx, dy) in local viewport pixels.
// Dragging right (positive dx) shifts the world left.
// Dragging down (positive dy) shifts the world down (camera Y decreases).
func (c *Camera) Pan(dx, dy float32) {
	if c.Zoom == 0 {
		return
	}
	inv := 1 / c.Zoom
	c.X -= dx * inv
	c.Y -= dy * inv
}

// ZoomAt multiplies Zoom by factor while keeping the world point under the
// cursor fixed on screen. cursorX/cursorY are in viewport pixels.
func (c *Camera) ZoomAt(factor, cursorX, cursorY, vpW, vpH float32) {
	bx := (cursorX-vpW/2)/c.Zoom + c.X
	by := (cursorY-vpH/2)/c.Zoom + c.Y

	c.Zoom *= factor
	ClampZoom(c)

	ax := (cursorX-vpW/2)/c.Zoom + c.X
	ay := (cursorY-vpH/2)/c.Zoom + c.Y

	c.X += bx - ax
	c.Y += by - ay
}

// Fit centres the camera on the world and picks a zoom that shows the whole
// world inside the viewport, clamped to [MinZoom, MaxZoom].
func (c *Camera) Fit(worldW, worldH, vpW, vpH float32) {
	if worldW > 0 && worldH > 0 && vpW > 0 && vpH > 0 {
		c.Zoom = min(vpW/worldW, vpH/worldH)
	} else {
		c.Zoom = 1
	}
	ClampZoom(c)
	c.X = worldW / 2
	c.Y = worldH / 2
}

// ClampToBounds keeps the visible region from drifting off the world.
func (c *Camera) ClampToBounds(worldW, worldH, vpW, vpH float32) {
	if c.Zoom == 0 {
		return
	}
	visibleW := vpW / c.Zoom
	visibleH := vpH / c.Zoom

	if visibleW >= worldW {
		c.X = worldW / 2
	} else {
		c.X = clampF(c.X, visibleW/2, worldW-visibleW/2)
	}
	if visibleH >= worldH {
		c.Y = worldH / 2
	} else {
		c.Y = clampF(c.Y, visibleH/2, worldH-visibleH/2)
	}
}

// WorldToLocal converts a world point to local viewport pixels.
func (c *Camera) WorldToLocal(wx, wy, vpW, vpH float32) (float32, float32) {
	lx := (wx-c.X)*c.Zoom + vpW/2
	ly := (wy-c.Y)*c.Zoom + vpH/2
	return lx, ly
}

// LocalToWorld converts a local viewport pixel to world coordinates.
func (c *Camera) LocalToWorld(lx, ly, vpW, vpH float32) (float32, float32) {
	wx := (lx-vpW/2)/c.Zoom + c.X
	wy := (ly-vpH/2)/c.Zoom + c.Y
	return wx, wy
}

func clampF(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
