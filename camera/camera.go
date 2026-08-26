// Package camera is a tiny, widget-independent 2D view transform. It maps
// between "world" coordinates (the coordinate space of the content being
// viewed — for a map this is typically image-pixel space) and "local" viewport
// coordinates (pixels inside a widget).
//
// It knows only about a position and a scale, and exposes pure coordinate
// conversions plus pan/zoom operations. It performs no drawing, clipping or
// input handling; a widget that draws content (such as render.MapView) owns a
// Camera and uses it to decide what to draw and where.
//
//	position = center of the visible region, in world coordinates
//	zoom     = scale factor (1 == natural size, 2 == twice as large, 0.5 == half)
package camera

import "github.com/gogpu/ui/geometry"

const (
	// defaultMinZoom / defaultMaxZoom are the built-in zoom clamps. They can be
	// overridden per camera via SetZoomLimits. The exact values should be
	// revisited after testing with real maps.
	defaultMinZoom float32 = 0.5
	defaultMaxZoom float32 = 8.0
)

// Camera is a pure coordinate transform. See package docs.
type Camera struct {
	position geometry.Point
	zoom     float32
	minZoom  float32
	maxZoom  float32
}

// NewCamera returns a camera centered at the origin with zoom 1 and default
// zoom limits.
func NewCamera() *Camera {
	return &Camera{
		zoom:    1,
		minZoom: defaultMinZoom,
		maxZoom: defaultMaxZoom,
	}
}

// SetZoomLimits overrides the allowed zoom range. Non-positive min or a max
// below min is ignored so a camera can never become degenerate.
func (c *Camera) SetZoomLimits(min, max float32) {
	if min > 0 {
		c.minZoom = min
	}
	if max >= c.minZoom {
		c.maxZoom = max
	}
}

// Position returns the world-space center of the visible region.
func (c *Camera) Position() geometry.Point { return c.position }

// SetPosition moves the view center to p (world coordinates).
func (c *Camera) SetPosition(p geometry.Point) { c.position = p }

// Zoom returns the current scale factor.
func (c *Camera) Zoom() float32 { return c.zoom }

// SetZoom sets the scale factor, clamped to the allowed range.
func (c *Camera) SetZoom(z float32) { c.zoom = c.clampZoom(z) }

func (c *Camera) clampZoom(z float32) float32 {
	if z < c.minZoom {
		return c.minZoom
	}
	if z > c.maxZoom {
		return c.maxZoom
	}
	return z
}

func (c *Camera) viewportCenter(vp geometry.Size) geometry.Point {
	return geometry.Pt(vp.Width/2, vp.Height/2)
}

// WorldToLocal converts a world-space point to local viewport pixels, given the
// viewport (widget) size.
func (c *Camera) WorldToLocal(world geometry.Point, vp geometry.Size) geometry.Point {
	center := c.viewportCenter(vp)
	d := world.Sub(c.position).Scale(c.zoom)
	return center.Add(d)
}

// LocalToWorld converts a local viewport pixel to world-space coordinates,
// given the viewport (widget) size.
func (c *Camera) LocalToWorld(local geometry.Point, vp geometry.Size) geometry.Point {
	center := c.viewportCenter(vp)
	d := local.Sub(center).Scale(1 / c.zoom)
	return c.position.Add(d)
}

// Pan moves the view by delta (local viewport pixels). Dragging the map to the
// right (positive delta.X) moves the map with the cursor, so the world-space
// center shifts left.
func (c *Camera) Pan(delta geometry.Point) {
	c.position = c.position.Sub(delta.Scale(1 / c.zoom))
}

// ZoomAt multiplies the zoom by factor while keeping the world point under
// cursorLocal fixed on screen. cursorLocal is the pointer position in local
// viewport pixels; vp is the viewport size.
func (c *Camera) ZoomAt(factor float32, cursorLocal geometry.Point, vp geometry.Size) {
	before := c.LocalToWorld(cursorLocal, vp)
	c.zoom = c.clampZoom(c.zoom * factor)
	after := c.LocalToWorld(cursorLocal, vp)
	c.position = c.position.Add(before.Sub(after))
}

// Fit centers the camera on worldSize and picks a zoom that shows the whole
// world inside vp (a useful overview), clamped to the zoom limits.
func (c *Camera) Fit(worldSize, vp geometry.Size) {
	if worldSize.Width > 0 && worldSize.Height > 0 && vp.Width > 0 && vp.Height > 0 {
		z := min(vp.Width/worldSize.Width, vp.Height/worldSize.Height)
		c.zoom = c.clampZoom(z)
	} else {
		c.zoom = c.clampZoom(1)
	}
	c.position = geometry.Pt(worldSize.Width/2, worldSize.Height/2)
}

// ClampToBounds keeps the visible region from drifting completely off the
// world. worldSize is the full content size; vp is the viewport size. If the
// visible area is larger than the world on an axis, the world is centered on
// that axis.
func (c *Camera) ClampToBounds(worldSize, vp geometry.Size) {
	visibleW := vp.Width / c.zoom
	visibleH := vp.Height / c.zoom

	if visibleW >= worldSize.Width {
		c.position.X = worldSize.Width / 2
	} else {
		minX := visibleW / 2
		maxX := worldSize.Width - visibleW/2
		c.position.X = clampF(c.position.X, minX, maxX)
	}

	if visibleH >= worldSize.Height {
		c.position.Y = worldSize.Height / 2
	} else {
		minY := visibleH / 2
		maxY := worldSize.Height - visibleH/2
		c.position.Y = clampF(c.position.Y, minY, maxY)
	}
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
