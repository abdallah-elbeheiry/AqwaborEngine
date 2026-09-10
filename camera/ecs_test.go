package camera

import (
	"math"
	"testing"

	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

func TestRegisterECSIdempotent(t *testing.T) {
	w := ecs.NewWorld()
	a, err := RegisterECS(w)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RegisterECS(w)
	if err != nil {
		t.Fatal("RegisterECS should be idempotent:", err)
	}
	if a.ID() != b.ID() {
		t.Fatalf("second registration made a new component: %d then %d", a.ID(), b.ID())
	}
}

func TestMustRegisterECS(t *testing.T) {
	w := ecs.NewWorld()
	MustRegisterECS(w)
}

func TestViewProjIdentity(t *testing.T) {
	c := Camera{X: 0, Y: 0, Zoom: 1}
	vp := ViewProj(c, 100, 100)
	if math.Abs(float64(vp[0]-0.02)) > 1e-6 {
		t.Fatalf("vp[0] = %v, want 0.02", vp[0])
	}
	if math.Abs(float64(vp[5]+0.02)) > 1e-6 {
		t.Fatalf("vp[5] = %v, want -0.02", vp[5])
	}
	if vp[12] != 0 || vp[13] != 0 {
		t.Fatalf("tx,ty = %v,%v, want 0,0", vp[12], vp[13])
	}
}

func TestViewProjZoom(t *testing.T) {
	c := Camera{X: 0, Y: 0, Zoom: 2}
	vp := ViewProj(c, 800, 600)
	if math.Abs(float64(vp[0]-0.005)) > 1e-6 {
		t.Fatalf("vp[0] = %v, want 0.005", vp[0])
	}
	syWant := float32(-2) * 2 / 600
	if math.Abs(float64(vp[5]-syWant)) > 1e-6 {
		t.Fatalf("vp[5] = %v, want %v", vp[5], syWant)
	}
}

func TestViewProjTranslation(t *testing.T) {
	c := Camera{X: 100, Y: 50, Zoom: 1}
	vp := ViewProj(c, 800, 600)
	txWant := float32(-100) * 2 / 800
	if math.Abs(float64(vp[12]-txWant)) > 1e-6 {
		t.Fatalf("vp[12] = %v, want %v", vp[12], txWant)
	}
	tyWant := float32(50) * 2 / 600
	if math.Abs(float64(vp[13]-tyWant)) > 1e-6 {
		t.Fatalf("vp[13] = %v, want %v", vp[13], tyWant)
	}
}

func TestViewProjZeroViewport(t *testing.T) {
	c := Camera{Zoom: 1}
	vp := ViewProj(c, 0, 0)
	if vp[0] != 2 || vp[5] != -2 {
		t.Fatalf("zero viewport should fallback to 1: got sx=%v sy=%v", vp[0], vp[5])
	}
}

func TestClampZoom(t *testing.T) {
	c := Camera{Zoom: 0.01, MinZoom: 0.1, MaxZoom: 10}
	ClampZoom(&c)
	if c.Zoom != 0.1 {
		t.Fatalf("Zoom = %v, want 0.1 (min clamp)", c.Zoom)
	}

	c.Zoom = 100
	ClampZoom(&c)
	if c.Zoom != 10 {
		t.Fatalf("Zoom = %v, want 10 (max clamp)", c.Zoom)
	}

	c.Zoom = 5
	ClampZoom(&c)
	if c.Zoom != 5 {
		t.Fatalf("Zoom = %v, want 5 (no clamp needed)", c.Zoom)
	}
}

func TestClampZoomZeroLimits(t *testing.T) {
	c := Camera{Zoom: 5, MinZoom: 0, MaxZoom: 0}
	ClampZoom(&c)
	if c.Zoom != 5 {
		t.Fatalf("zero limits should not clamp: Zoom = %v", c.Zoom)
	}
}

func TestWorldLocalRoundtrip(t *testing.T) {
	c := Camera{X: 500, Y: 300, Zoom: 1}
	for _, z := range []float32{0.5, 1, 2, 4, 8} {
		c.Zoom = z
		for _, p := range [][2]float32{{0, 0}, {100, 100}, {500, 300}, {1000, 800}} {
			lx, ly := c.WorldToLocal(p[0], p[1], 800, 600)
			wx, wy := c.LocalToWorld(lx, ly, 800, 600)
			if dx, dy := wx-p[0], wy-p[1]; dx*dx+dy*dy > 1e-6 {
				t.Fatalf("roundtrip failed z=%.2f p=(%.0f,%.0f) got=(%.4f,%.4f)", z, p[0], p[1], wx, wy)
			}
		}
	}
}

func TestPan(t *testing.T) {
	c := Camera{X: 500, Y: 300, Zoom: 2}
	c.Pan(10, 0) // drag right 10 px
	if c.X >= 500 {
		t.Fatalf("panning right should decrease X: got %v", c.X)
	}
	c.Pan(0, -20) // drag up 20 px
	if c.Y <= 300 {
		t.Fatalf("panning up should increase Y: got %v", c.Y)
	}
}

func TestZoomAtCursor(t *testing.T) {
	c := Camera{X: 500, Y: 300, Zoom: 1}
	cursorX, cursorY := float32(200), float32(150)
	bx, by := c.LocalToWorld(cursorX, cursorY, 800, 600)
	c.ZoomAt(2, cursorX, cursorY, 800, 600)
	ax, ay := c.LocalToWorld(cursorX, cursorY, 800, 600)
	if dx, dy := bx-ax, by-ay; dx*dx+dy*dy > 1e-6 {
		t.Fatalf("world point under cursor moved: before=(%.4f,%.4f) after=(%.4f,%.4f)", bx, by, ax, ay)
	}
}

func TestClampToBounds(t *testing.T) {
	c := Camera{X: -1e9, Y: -1e9, Zoom: 2}
	c.ClampToBounds(1000, 1000, 800, 600) // visible 400x300
	if c.X < 200-1e-3 || c.Y < 150-1e-3 {
		t.Fatalf("clamp did not keep map in view (low): (%v, %v)", c.X, c.Y)
	}

	c.X, c.Y = 1e9, 1e9
	c.ClampToBounds(1000, 1000, 800, 600)
	if c.X > 800+1e-3 || c.Y > 850+1e-3 {
		t.Fatalf("clamp did not keep map in view (high): (%v, %v)", c.X, c.Y)
	}

	c.Zoom = 0.1
	c.X, c.Y = 1e9, 1e9
	c.ClampToBounds(1000, 1000, 800, 600)
	if c.X != 500 || c.Y != 500 {
		t.Fatalf("expected centering when viewport > world, got (%v, %v)", c.X, c.Y)
	}
}

// clipOf projects a world point through the matrix ViewProj builds. The matrix is
// column-major and the projection is orthographic, so w is 1 and the two axes are
// independent.
func clipOf(vp [16]float32, wx, wy float32) (float32, float32) {
	return wx*vp[0] + vp[12], wy*vp[5] + vp[13]
}

func TestViewProjPutsCameraAtCentre(t *testing.T) {
	c := Camera{X: 26, Y: 17, Zoom: 29.41}
	vp := ViewProj(c, 1600, 1000)

	cx, cy := clipOf(vp, c.X, c.Y)
	if math.Abs(float64(cx)) > 1e-5 || math.Abs(float64(cy)) > 1e-5 {
		t.Fatalf("camera lands at clip %v,%v, want 0,0", cx, cy)
	}
}

func TestViewProjWorldYRunsDown(t *testing.T) {
	c := Camera{X: 0, Y: 0, Zoom: 1}
	vp := ViewProj(c, 800, 600)

	_, above := clipOf(vp, 0, -10)
	_, below := clipOf(vp, 0, 10)
	if !(below < above) {
		t.Fatalf("clip Y below the camera = %v, above = %v; world Y runs down, so below must be smaller", below, above)
	}
}

func TestViewProjFittedWorldIsOnScreen(t *testing.T) {
	c := Camera{MinZoom: 0.01, MaxZoom: 1000}
	c.Fit(52, 34, 1600, 1000)
	c.X, c.Y = 26, 17
	vp := ViewProj(c, 1600, 1000)

	for _, p := range [][2]float32{{0, 0}, {52, 0}, {0, 34}, {52, 34}} {
		x, y := clipOf(vp, p[0], p[1])
		if x < -1.0001 || x > 1.0001 || y < -1.0001 || y > 1.0001 {
			t.Fatalf("corner %v lands at clip %v,%v, outside -1..1", p, x, y)
		}
	}
}
