// Package render (continued): the MapView widget.
package render

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/camera"
	"github.com/abdallah-elbeheiry/AqwaborEngine/ui"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
	"github.com/gogpu/ui/widget"
)

// MapView is a GUI widget that displays a large map image inside a viewport and
// lets the user pan (left-drag) and zoom (mouse wheel, toward the cursor). It is
// deliberately small: it is just a widget that owns a camera.Camera and uses it
// to draw an ui.ImageAsset. It is not a general camera framework, and it knows
// nothing about countries, provinces, units or any other map content.
//
// Typical use:
//
//	mapImage, _ := app.Images().Load("assets/world.png")
//	root := ui.Row(sidePanel, render.MapView(mapImage))
type mapView struct {
	widget.WidgetBase

	asset *ui.ImageAsset
	cam   *camera.Camera

	drag  *gesture.DragRecognizer
	cache DrawCache

	onPointer func(local, world geometry.Point)

	initialized bool
}

// MapView creates a pannable/zoomable map widget for the given asset. The asset
// must be loaded via ImageManager.Load; the widget never performs I/O.
func MapView(asset *ui.ImageAsset) *mapView {
	m := &mapView{
		asset: asset,
		cam:   camera.NewCamera(),
	}
	m.SetVisible(true)
	m.SetEnabled(true)
	return m
}

// ZoomRange overrides the allowed zoom limits for this map.
func (m *mapView) ZoomRange(min, max float32) *mapView {
	m.cam.SetZoomLimits(min, max)
	return m
}

// Camera returns the camera driving this view. It is exposed so callers can
// read or drive the view programmatically (e.g. center on a location).
func (m *mapView) Camera() *camera.Camera { return m.cam }

// OnPointer registers a callback invoked with the pointer position over the
// map, expressed both in widget-local pixels and in world (map) coordinates.
// It fires on hover and during drag. It is optional and intended for
// diagnostics/overlays (the coordinate-conversion hook from the design).
func (m *mapView) OnPointer(fn func(local, world geometry.Point)) *mapView {
	m.onPointer = fn
	return m
}

// Overview re-centers the camera on the map and picks a zoom that shows the
// whole map (the same initial behavior as first layout).
func (m *mapView) Overview() *mapView {
	if m.asset != nil && !m.asset.IsReleased() {
		w, h := m.asset.Size()
		m.cam.Fit(geometry.Sz(float32(w), float32(h)), m.Bounds().Size())
	}
	m.SetNeedsRedraw(true)
	return m
}

// LocalToWorld converts a point in this widget's local pixels to world
// coordinates (image-pixel space). This is the hook later map content — country
// hit-testing, unit overlays — will use.
func (m *mapView) LocalToWorld(p geometry.Point) geometry.Point {
	return m.cam.LocalToWorld(p, m.Bounds().Size())
}

// WorldToLocal converts a world point to this widget's local pixels.
func (m *mapView) WorldToLocal(p geometry.Point) geometry.Point {
	return m.cam.WorldToLocal(p, m.Bounds().Size())
}

// Layout fills the allotted space (a viewport wants to be as large as it is
// given) and, on first layout, establishes an initial overview of the map.
func (m *mapView) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	natural := geometry.Sz(0, 0)
	if m.asset != nil && !m.asset.IsReleased() {
		w, h := m.asset.Size()
		natural = geometry.Sz(float32(w), float32(h))
	}

	desired := natural
	if c.HasBoundedWidth() {
		desired.Width = c.MaxWidth
	}
	if c.HasBoundedHeight() {
		desired.Height = c.MaxHeight
	}
	if desired.IsZero() {
		desired = natural
	}

	result := c.Constrain(desired)
	if !m.initialized && result.Width > 0 && result.Height > 0 {
		if m.asset != nil && !m.asset.IsReleased() {
			w, h := m.asset.Size()
			m.cam.Fit(geometry.Sz(float32(w), float32(h)), result)
		}
		m.initialized = true
	}
	m.SetBounds(geometry.FromPointSize(m.Position(), result))
	return result
}

// Draw clips to its bounds, then draws the visible portion of the map scaled
// according to the camera.
func (m *mapView) Draw(_ widget.Context, canvas widget.Canvas) {
	if !m.IsVisible() {
		return
	}
	b := m.Bounds()
	if b.IsEmpty() {
		return
	}
	vp := b.Size()

	canvas.PushClip(b)
	canvas.PushTransform(b.Min)

	img, ok := m.asset.Take()
	if ok && img != nil {
		m.cache.Draw(canvas, img, m.cam, vp)
	}

	canvas.PopTransform()
	canvas.PopClip()
}

// Event handles the mouse wheel to zoom toward the cursor. Pointer drags are
// handled by the gesture DragRecognizer (see GestureHitTest), not here.
func (m *mapView) Event(_ widget.Context, e event.Event) bool {
	switch ev := e.(type) {
	case *event.WheelEvent:
		var factor float32
		switch {
		case ev.Delta.Y < 0:
			factor = 1.1 // scroll up == zoom in
		case ev.Delta.Y > 0:
			factor = 1 / 1.1 // scroll down == zoom out
		default:
			return false
		}
		m.cam.ZoomAt(factor, ev.Position, m.Bounds().Size())
		m.clampCamera()
		m.SetNeedsRedraw(true)
		if m.onPointer != nil {
			m.onPointer(ev.Position, m.LocalToWorld(ev.Position))
		}
		return true
	case *event.MouseEvent:
		if ev.MouseType == event.MouseMove || ev.MouseType == event.MouseDrag {
			if m.onPointer != nil {
				m.onPointer(ev.Position, m.LocalToWorld(ev.Position))
			}
		}
		return false
	}
	return false
}

// GestureHitTest reports a pan drag recognizer so the map can be dragged with
// the left mouse button. MapView is a leaf, so it always claims the pointer.
func (m *mapView) GestureHitTest(_ geometry.Point) []gesture.Recognizer {
	if m.drag == nil {
		m.drag = gesture.NewDragRecognizer(gesture.DragConfig{
			Direction: gesture.DragDirectionPan,
			OnDragUpdate: func(d gesture.DragUpdateDetails) {
				m.cam.Pan(d.Delta)
				m.clampCamera()
				m.SetNeedsRedraw(true)
				if m.onPointer != nil {
					m.onPointer(d.LocalPosition, m.LocalToWorld(d.LocalPosition))
				}
			},
		})
	}
	return []gesture.Recognizer{m.drag}
}

func (m *mapView) clampCamera() {
	if m.asset == nil || m.asset.IsReleased() {
		return
	}
	w, h := m.asset.Size()
	m.cam.ClampToBounds(geometry.Sz(float32(w), float32(h)), m.Bounds().Size())
}

// Children: leaf widget.
func (m *mapView) Children() []widget.Widget { return nil }

// IsViewportClip tells the dirty-region collector to use this widget's bounds
// as the dirty region and not recurse into the (potentially huge) content.
func (m *mapView) IsViewportClip() bool { return true }

// Mount registers the widget as an active user of its asset.
func (m *mapView) Mount(_ widget.Context) {
	if m.asset != nil {
		m.asset.Acquire()
	}
}

// Unmount unregisters the widget, freeing the asset for release when no other
// users remain.
func (m *mapView) Unmount() {
	if m.asset != nil {
		m.asset.ReleaseUser()
	}
}
