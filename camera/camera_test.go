package camera

import (
	"testing"

	"github.com/gogpu/ui/geometry"
)

// --- Camera: coordinate conversion ---

func TestCameraWorldLocalRoundtrip(t *testing.T) {
	c := NewCamera()
	c.SetPosition(geometry.Pt(500, 300))
	vp := geometry.Sz(800, 600)
	for _, z := range []float32{0.5, 1, 2, 4, 8} {
		c.SetZoom(z)
		for _, p := range []geometry.Point{geometry.Pt(0, 0), geometry.Pt(100, 100), geometry.Pt(500, 300), geometry.Pt(1000, 800)} {
			got := c.WorldToLocal(c.LocalToWorld(p, vp), vp)
			if d := got.Sub(p).Length(); d > 1e-3 {
				t.Fatalf("roundtrip failed z=%.2f p=%v got=%v dist=%.4f", z, p, got, d)
			}
		}
	}
}

// --- Camera: pan ---

func TestCameraPan(t *testing.T) {
	c := NewCamera()
	c.SetPosition(geometry.Pt(500, 300))
	c.SetZoom(2)
	before := c.Position()
	c.Pan(geometry.Pt(10, 0)) // drag right 10 local px
	if c.Position().X >= before.X {
		t.Fatalf("panning right should decrease world center X: before=%v after=%v", before, c.Position())
	}
	c.Pan(geometry.Pt(0, -20))
	if c.Position().Y <= before.Y {
		t.Fatalf("panning up should increase world center Y: before=%v after=%v", before, c.Position())
	}
}

// --- Camera: zoom in / out ---

func TestCameraZoomDirection(t *testing.T) {
	c := NewCamera()
	c.SetZoom(2)
	c.ZoomAt(1.1, geometry.Pt(0, 0), geometry.Sz(800, 600))
	zoomedIn := c.Zoom()
	if zoomedIn <= 2 {
		t.Fatalf("zoom in should increase zoom, got %v", zoomedIn)
	}
	c.ZoomAt(1/1.1, geometry.Pt(0, 0), geometry.Sz(800, 600))
	if c.Zoom() >= zoomedIn {
		t.Fatalf("zoom out should decrease zoom, got %v (after in %v)", c.Zoom(), zoomedIn)
	}
}

// --- Camera: zoom bounds ---

func TestCameraZoomBounds(t *testing.T) {
	c := NewCamera()
	c.SetZoomLimits(0.5, 8)
	c.ZoomAt(1e6, geometry.Pt(0, 0), geometry.Sz(800, 600))
	if c.Zoom() > 8 {
		t.Fatalf("zoom exceeded max: %v", c.Zoom())
	}
	c.ZoomAt(1e-9, geometry.Pt(0, 0), geometry.Sz(800, 600))
	if c.Zoom() < 0.5 {
		t.Fatalf("zoom below min: %v", c.Zoom())
	}
}

// --- Camera: zoom at cursor keeps the point fixed ---

func TestCameraZoomAtCursor(t *testing.T) {
	c := NewCamera()
	c.SetPosition(geometry.Pt(500, 300))
	c.SetZoom(1)
	vp := geometry.Sz(800, 600)
	cursor := geometry.Pt(200, 150)
	before := c.LocalToWorld(cursor, vp)
	c.ZoomAt(2, cursor, vp)
	after := c.LocalToWorld(cursor, vp)
	if d := before.Sub(after).Length(); d > 1e-3 {
		t.Fatalf("world point under cursor moved after zoom: before=%v after=%v dist=%.4f", before, after, d)
	}
}

// --- Camera: bounds clamp ---

func TestCameraClamp(t *testing.T) {
	c := NewCamera()
	c.SetZoom(2)
	world := geometry.Sz(1000, 1000)
	vp := geometry.Sz(800, 600) // visible region = 400 x 300

	c.SetPosition(geometry.Pt(-1e9, -1e9))
	c.ClampToBounds(world, vp)
	if c.Position().X < 200-1e-3 || c.Position().Y < 150-1e-3 {
		t.Fatalf("clamp did not keep map in view (low): %v", c.Position())
	}

	c.SetPosition(geometry.Pt(1e9, 1e9))
	c.ClampToBounds(world, vp)
	if c.Position().X > 800+1e-3 || c.Position().Y > 850+1e-3 {
		t.Fatalf("clamp did not keep map in view (high): %v", c.Position())
	}

	// When the viewport is larger than the world, the world is centered.
	c.SetZoom(0.1) // visible region huge
	c.SetPosition(geometry.Pt(1e9, 1e9))
	c.ClampToBounds(world, vp)
	if c.Position().X != 500 || c.Position().Y != 500 {
		t.Fatalf("expected centering when viewport > world, got %v", c.Position())
	}
}
