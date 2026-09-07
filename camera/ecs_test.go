package camera

import (
	"math"
	"testing"

	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

func TestRegisterECSIdempotent(t *testing.T) {
	w := ecs.NewWorld()
	if err := RegisterECS(w); err != nil {
		t.Fatal(err)
	}
	if err := RegisterECS(w); err != nil {
		t.Fatal("RegisterECS should be idempotent:", err)
	}
}

func TestMustRegisterECS(t *testing.T) {
	w := ecs.NewWorld()
	MustRegisterECS(w)
}

func TestViewProjFromIdentity(t *testing.T) {
	c := Camera2D{X: 0, Y: 0, Zoom: 1}
	vp := ViewProjFrom(c, 100, 100)
	// sx = 1*2/100 = 0.02, sy = 1*2/100 = 0.02
	if math.Abs(float64(vp[0]-0.02)) > 1e-6 {
		t.Fatalf("vp[0] = %v, want 0.02", vp[0])
	}
	if math.Abs(float64(vp[5]-0.02)) > 1e-6 {
		t.Fatalf("vp[5] = %v, want 0.02", vp[5])
	}
	// tx = 0, ty = 0
	if vp[12] != 0 || vp[13] != 0 {
		t.Fatalf("tx,ty = %v,%v, want 0,0", vp[12], vp[13])
	}
}

func TestViewProjFromZoom(t *testing.T) {
	c := Camera2D{X: 0, Y: 0, Zoom: 2}
	vp := ViewProjFrom(c, 800, 600)
	// sx = 2*2/800 = 0.005
	if math.Abs(float64(vp[0]-0.005)) > 1e-6 {
		t.Fatalf("vp[0] = %v, want 0.005", vp[0])
	}
	// sy = 2*2/600 ≈ 0.006667
	syWant := float32(2) * 2 / 600
	if math.Abs(float64(vp[5]-syWant)) > 1e-6 {
		t.Fatalf("vp[5] = %v, want %v", vp[5], syWant)
	}
}

func TestViewProjFromTranslation(t *testing.T) {
	c := Camera2D{X: 100, Y: 50, Zoom: 1}
	vp := ViewProjFrom(c, 800, 600)
	// tx = -100*2/800 = -0.25
	txWant := float32(-100) * 2 / 800
	if math.Abs(float64(vp[12]-txWant)) > 1e-6 {
		t.Fatalf("vp[12] = %v, want %v", vp[12], txWant)
	}
	// ty = 50*2/600 ≈ 0.16667
	tyWant := float32(50) * 2 / 600
	if math.Abs(float64(vp[13]-tyWant)) > 1e-6 {
		t.Fatalf("vp[13] = %v, want %v", vp[13], tyWant)
	}
}

func TestViewProjFromZeroViewport(t *testing.T) {
	c := Camera2D{Zoom: 1}
	vp := ViewProjFrom(c, 0, 0)
	// Should not divide by zero; uses fallback of 1.
	if vp[0] != 2 || vp[5] != 2 {
		t.Fatalf("zero viewport should fallback to 1: got sx=%v sy=%v", vp[0], vp[5])
	}
}

func TestClampZoom(t *testing.T) {
	c := Camera2D{Zoom: 0.01, MinZoom: 0.1, MaxZoom: 10}
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
	c := Camera2D{Zoom: 5, MinZoom: 0, MaxZoom: 0}
	ClampZoom(&c)
	if c.Zoom != 5 {
		t.Fatalf("zero limits should not clamp: Zoom = %v", c.Zoom)
	}
}
