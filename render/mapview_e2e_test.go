package render

import (
	"testing"

	"github.com/abdallah-elbeheiry/AqwaborEngine/ui"
	gapp "github.com/gogpu/ui/app"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
)

// --- synthetic input helpers (mirror ui/clickable_e2e_test.go) ---

func ptr(et gesture.PointerEventType, p geometry.Point, buttons event.ButtonState) *gesture.PointerEvent {
	return &gesture.PointerEvent{
		Base:           event.NewBase(event.TypeMouse, event.ModNone),
		EventType:      et,
		PointerID:      1,
		PointerType:    gesture.PointerTypeMouse,
		Position:       p,
		GlobalPosition: p,
		Button:         event.ButtonLeft,
		Buttons:        buttons,
		Pressure:       0.5,
	}
}

func drag(a *gapp.App, from, to geometry.Point) {
	a.Window().HandlePointerEvent(ptr(gesture.PointerDown, from, event.ButtonStateLeft))
	// a few intermediate steps to clear slop and exercise multiple updates
	mid := geometry.Pt((from.X+to.X)/2, (from.Y+to.Y)/2)
	a.Window().HandlePointerEvent(ptr(gesture.PointerMove, mid, event.ButtonStateLeft))
	a.Window().HandlePointerEvent(ptr(gesture.PointerMove, to, event.ButtonStateLeft))
	a.Window().HandlePointerEvent(ptr(gesture.PointerUp, to, 0))
}

func wheel(a *gapp.App, delta, at geometry.Point) {
	we := event.NewWheelEvent(delta, at, at, event.ModNone)
	a.Window().HandleEvent(we)
}

// --- the test ---

func TestMapViewE2E(t *testing.T) {
	a := gapp.New()
	mgr := ui.NewImageManager()
	asset, err := mgr.Load("../examples/fox.png")
	if err != nil {
		t.Fatal(err)
	}

	var lastWorld geometry.Point
	var pointerFired int
	mv := MapView(asset).ZoomRange(0.5, 8).
		OnPointer(func(_, w geometry.Point) { lastWorld = w; pointerFired++ })

	a.SetRoot(mv)
	a.Frame()

	sz := a.Window().WindowSize()
	if sz.IsZero() {
		t.Fatal("window has zero size")
	}
	cursor := geometry.Pt(sz.Width/2, sz.Height/2)
	vp := mv.Bounds().Size()
	if vp.IsEmpty() {
		t.Fatal("map view has empty bounds after layout")
	}

	cam := mv.Camera()
	initZoom := cam.Zoom()
	if initZoom <= 0 {
		t.Fatalf("initial zoom not set: %v", initZoom)
	}
	if initZoom < 0.5 || initZoom > 8 {
		t.Fatalf("initial overview zoom out of ZoomRange: %v", initZoom)
	}

	// --- zoom at cursor keeps the world point fixed ---
	worldBefore := cam.LocalToWorld(cursor, vp)
	wheel(a, geometry.Pt(0, -100), cursor) // scroll up == zoom in
	if cam.Zoom() <= initZoom {
		t.Fatalf("wheel up did not zoom in: %v -> %v", initZoom, cam.Zoom())
	}
	worldAfter := cam.LocalToWorld(cursor, vp)
	if d := worldBefore.Sub(worldAfter).Length(); d > 1e-2 {
		t.Fatalf("zoom-at-cursor moved world point: before=%v after=%v dist=%.4f", worldBefore, worldAfter, d)
	}

	// --- zoom out ---
	zoomAfterIn := cam.Zoom()
	wheel(a, geometry.Pt(0, 100), cursor) // scroll down == zoom out
	if cam.Zoom() >= zoomAfterIn {
		t.Fatalf("wheel down did not zoom out: %v -> %v", zoomAfterIn, cam.Zoom())
	}

	// --- zoom bounds clamping ---
	for range 60 {
		wheel(a, geometry.Pt(0, -100), cursor)
	}
	if cam.Zoom() > 8+1e-3 {
		t.Fatalf("zoom exceeded max: %v", cam.Zoom())
	}
	for range 120 {
		wheel(a, geometry.Pt(0, 100), cursor)
	}
	if cam.Zoom() < 0.5-1e-3 {
		t.Fatalf("zoom below min: %v", cam.Zoom())
	}

	// --- pan (drag right moves the map right, center world shifts left) ---
	// Zoom in first so the map overflows the viewport; at fit zoom the map is
	// fully visible and ClampToBounds legitimately pins the center (pan is a
	// no-op there).
	mv.Camera().SetZoom(2)
	a.Frame()
	beforePan := mv.Camera().Position()
	drag(a, cursor, geometry.Pt(cursor.X+120, cursor.Y))
	afterPan := mv.Camera().Position()
	if afterPan.X >= beforePan.X {
		t.Fatalf("dragging right should decrease world center X: %v -> %v", beforePan, afterPan)
	}

	// --- bounds clamp: a huge drag must not lose the map ---
	mv.Camera().SetZoom(2)
	a.Frame()
	drag(a, cursor, geometry.Pt(sz.Width*3, sz.Height*3))
	clamped := mv.Camera().Position()
	aw, ah := asset.Size()
	wW, wH := float32(aw), float32(ah)
	_ = wH
	visW := vp.Width / 2
	if visW < wW {
		if clamped.X < visW/2-1 || clamped.X > wW-visW/2+1 {
			t.Fatalf("camera X escaped bounds after drag: %v (worldW=%v visW=%v)", clamped.X, wW, visW)
		}
	}

	// --- coordinate round-trip through the live widget ---
	for _, p := range []geometry.Point{geometry.Pt(0, 0), geometry.Pt(100, 100), geometry.Pt(300, 250)} {
		got := mv.LocalToWorld(mv.WorldToLocal(p))
		if d := got.Sub(p).Length(); d > 1e-2 {
			t.Fatalf("live coord roundtrip failed p=%v got=%v dist=%.4f", p, got, d)
		}
	}

	// --- OnPointer fires on hover (unpressed move) ---
	before := pointerFired
	a.Window().HandlePointerEvent(ptr(gesture.PointerMove, cursor, 0))
	if pointerFired <= before {
		t.Fatal("OnPointer callback did not fire on hover")
	}
	// last world reported must be consistent with LocalToWorld at the cursor
	exp := mv.LocalToWorld(cursor)
	if d := exp.Sub(lastWorld).Length(); d > 1e-2 {
		t.Fatalf("OnPointer world coord mismatch: reported=%v expected=%v", lastWorld, exp)
	}
}
