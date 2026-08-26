package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
)

func tempPNG(t *testing.T, name string, w, h int) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 4), B: 120, A: 255})
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValidPNG(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "a.png", 32, 24)
	a, err := mgr.Load(p)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if a == nil {
		t.Fatal("nil asset")
	}
	if a.Users() != 0 {
		t.Errorf("fresh asset should have 0 users, got %d", a.Users())
	}
	if w, h := a.Size(); w != 32 || h != 24 {
		t.Errorf("size = %dx%d, want 32x24", w, h)
	}
	if a.IsReleased() {
		t.Error("asset should not be released")
	}
}

func TestLoadMissing(t *testing.T) {
	mgr := NewImageManager()
	_, err := mgr.Load(filepath.Join(t.TempDir(), "nope.png"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadMalformed(t *testing.T) {
	mgr := NewImageManager()
	p := filepath.Join(t.TempDir(), "bad.png")
	if err := os.WriteFile(p, []byte("this is not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Load(p)
	if err == nil {
		t.Fatal("expected decode error for malformed png")
	}
}

func TestLoadUnsupported(t *testing.T) {
	mgr := NewImageManager()
	p := filepath.Join(t.TempDir(), "x.jpg")
	if err := os.WriteFile(p, []byte("jpeg?"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Load(p)
	if err == nil {
		t.Fatal("expected unsupported-format error")
	}
}

func TestCachingSamePath(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "c.png", 10, 10)
	a1, err := mgr.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	a2, err := mgr.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if a1 != a2 {
		t.Fatal("repeated Load of same path must return the same asset")
	}
	if _, ok := mgr.cache[p]; !ok {
		t.Fatal("asset not present in manager cache")
	}

	// Release and reload → a new canonical asset is created.
	if !mgr.TryRelease(a1) {
		t.Fatal("TryRelease should succeed with no users")
	}
	if _, ok := mgr.cache[p]; ok {
		t.Fatal("released asset must be removed from cache")
	}
	a3, err := mgr.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if a3 == a1 {
		t.Fatal("Load after release must create a new asset")
	}
}

func TestUsageTracking(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "u.png", 10, 10)
	a, _ := mgr.Load(p)

	w := Image(a)
	// Constructed but not mounted → must NOT count as an active user.
	if a.Users() != 0 {
		t.Fatalf("constructed (unmounted) widget should not count, users=%d", a.Users())
	}
	if mgr.TryRelease(a) != true {
		t.Fatal("TryRelease should succeed while unused")
	}
	// Reload for the rest of the test (previous release removed it), and bind a
	// fresh widget to the new asset.
	a, _ = mgr.Load(p)
	w = Image(a)

	w.Mount(uitest.NewMockContext())
	if a.Users() != 1 {
		t.Fatalf("after Mount, users=%d want 1", a.Users())
	}
	if mgr.TryRelease(a) != false {
		t.Fatal("TryRelease must refuse while the widget is live")
	}
	if a.Users() != 1 {
		t.Fatalf("users changed unexpectedly: %d", a.Users())
	}

	w.Unmount()
	if a.Users() != 0 {
		t.Fatalf("after Unmount, users=%d want 0", a.Users())
	}
	if mgr.TryRelease(a) != true {
		t.Fatal("TryRelease must succeed after the widget is removed")
	}
}

func TestForceRelease(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "f.png", 10, 10)
	a, _ := mgr.Load(p)

	w := Image(a)
	w.Mount(uitest.NewMockContext())
	if a.Users() != 1 {
		t.Fatalf("users=%d want 1", a.Users())
	}

	// ForceRelease frees the resource despite active users.
	mgr.ForceRelease(a)
	if !a.IsReleased() {
		t.Fatal("asset should be released")
	}
	if _, ok := mgr.cache[p]; ok {
		t.Fatal("force-released asset must be removed from cache")
	}

	// A released asset draws nothing (deterministic, no crash).
	uitest.LayoutWidget(w, 100, 100)
	canvas := uitest.DrawWidgetWithContext(w, uitest.NewMockContext())
	if len(canvas.Images) != 0 {
		t.Fatalf("released asset should draw nothing, got %d draw calls", len(canvas.Images))
	}

	// Double ForceRelease is a no-op (no double free).
	mgr.ForceRelease(a)
	if mgr.TryRelease(a) != false {
		t.Fatal("TryRelease after ForceRelease must be false")
	}

	// Reloading the same path yields a fresh, unreleased asset.
	a2, _ := mgr.Load(p)
	if a2 == a {
		t.Fatal("Load after ForceRelease must create a new asset")
	}
}

func TestDoubleRelease(t *testing.T) {
	mgr := NewImageManager()

	// Double TryRelease.
	p1 := tempPNG(t, "d1.png", 8, 8)
	a1, _ := mgr.Load(p1)
	if !mgr.TryRelease(a1) {
		t.Fatal("first TryRelease should succeed")
	}
	if mgr.TryRelease(a1) {
		t.Fatal("second TryRelease should be false")
	}

	// Double ForceRelease.
	p2 := tempPNG(t, "d2.png", 8, 8)
	a2, _ := mgr.Load(p2)
	mgr.ForceRelease(a2)
	mgr.ForceRelease(a2) // must not panic / double-free
	if mgr.TryRelease(a2) {
		t.Fatal("TryRelease after ForceRelease must be false")
	}
}

func TestCrossManagerOwnership(t *testing.T) {
	mA := NewImageManager()
	mB := NewImageManager()
	p := tempPNG(t, "o.png", 10, 10)
	a, _ := mA.Load(p)

	if mB.TryRelease(a) {
		t.Fatal("a manager must not release an asset it does not own")
	}
	if !mA.TryRelease(a) {
		t.Fatal("the owning manager should release it")
	}
}

func TestImageCompose(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "comp.png", 20, 20)
	a, _ := mgr.Load(p)

	cases := map[string]Widget{
		"Row":       Row(Image(a).Size(20, 20), Label("x")),
		"Column":    Column(Image(a)),
		"Box":       Box(Image(a)),
		"Clickable": Clickable(Image(a).Size(20, 20), func() {}),
	}
	for name, w := range cases {
		uitest.LayoutWidget(w, 800, 600)
		canvas := uitest.DrawWidgetWithContext(w, uitest.NewMockContext())
		if len(canvas.Images) == 0 {
			t.Errorf("%s: expected image to be drawn", name)
		}
	}
}

// gestureTap simulates a left-click through the real gesture pipeline, matching
// how app/window.go dispatches pointer input. It honors the widget's
// ScreenBounds, so taps outside the bounds are ignored — exactly as a real
// window would behave.
func gestureTap(c *clickableWidget, x, y float32) {
	if !c.ScreenBounds().Contains(geometry.Pt(x, y)) {
		return
	}
	arena := gesture.NewArena()
	down := &gesture.PointerEvent{
		EventType:      gesture.PointerDown,
		PointerID:      1,
		PointerType:    gesture.PointerTypeMouse,
		Position:       geometry.Pt(x, y),
		GlobalPosition: geometry.Pt(x, y),
		Button:         event.ButtonLeft,
		Buttons:        event.ButtonStateLeft,
	}
	for _, r := range c.GestureHitTest(geometry.Pt(x, y)) {
		r.AddPointer(down, arena)
	}
	arena.Close(down.PointerID)

	up := &gesture.PointerEvent{
		EventType:      gesture.PointerUp,
		PointerID:      1,
		PointerType:    gesture.PointerTypeMouse,
		Position:       geometry.Pt(x, y),
		GlobalPosition: geometry.Pt(x, y),
		Button:         event.ButtonLeft,
		Buttons:        0,
	}
	arena.Route(up)
	arena.Sweep(up.PointerID)
}

func TestClickableFiresOnce(t *testing.T) {
	var clicks int
	c := Clickable(Label("hi"), func() { clicks++ }).(*clickableWidget)
	c.SetBounds(geometry.FromPointSize(geometry.Pt(0, 0), geometry.Sz(100, 50)))

	gestureTap(c, 10, 10)
	if clicks != 1 {
		t.Fatalf("inside click: got %d want 1", clicks)
	}

	// A click outside the bounds must not fire.
	var clicks2 int
	c2 := Clickable(Label("hi"), func() { clicks2++ }).(*clickableWidget)
	c2.SetBounds(geometry.FromPointSize(geometry.Pt(0, 0), geometry.Sz(100, 50)))
	gestureTap(c2, 200, 200)
	if clicks2 != 0 {
		t.Fatalf("outside click: got %d want 0", clicks2)
	}
}

func TestClickableWithImageAndLabel(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "clk.png", 20, 20)
	a, _ := mgr.Load(p)

	var clicks int
	c := Clickable(
		Row(Image(a).Size(20, 20), Label("Play")),
		func() { clicks++ },
	).(*clickableWidget)
	c.SetBounds(geometry.FromPointSize(geometry.Pt(0, 0), geometry.Sz(200, 50)))

	gestureTap(c, 10, 10)
	if clicks != 1 {
		t.Fatalf("got %d want 1", clicks)
	}
}

func TestImageButton(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "ib.png", 20, 20)
	a, _ := mgr.Load(p)

	var clicks int
	b := ImageButton(a, func() { clicks++ }).(*clickableWidget)
	b.SetBounds(geometry.FromPointSize(geometry.Pt(0, 0), geometry.Sz(50, 50)))

	gestureTap(b, 5, 5)
	if clicks != 1 {
		t.Fatalf("got %d want 1", clicks)
	}
}

func TestAppImages(t *testing.T) {
	app, err := New(Config{Title: "t", W: 100, H: 100})
	if err != nil {
		t.Fatal(err)
	}
	if app.Images() == nil {
		t.Fatal("app.Images() returned nil")
	}
	if app.Images() != app.Images() {
		t.Fatal("app.Images() must return the same manager")
	}
}

func TestExistingButtonUnchanged(t *testing.T) {
	// The original text Button API must still compile and behave as a Widget.
	var _ Widget = Button("Play", func() {})
}

// TestConstructedImageDoesNotBlockRelease locks in the rule: constructing an
// image widget (without mounting it in the tree) must NOT count as active use,
// so the asset can still be released. Construction ≠ active use.
func TestConstructedImageDoesNotBlockRelease(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "c.png", 10, 10)
	a, _ := mgr.Load(p)

	_ = Image(a) // constructed but never mounted

	if a.Users() != 0 {
		t.Fatalf("constructed (unmounted) widget users = %d, want 0", a.Users())
	}
	if !mgr.TryRelease(a) {
		t.Fatal("TryRelease must succeed for a merely-constructed asset")
	}
}

// TestCacheNormalization verifies that paths which normalize to the same
// filesystem identity share one cached asset, and that releasing then reloading
// yields a fresh, valid asset while the old one stays released.
func TestCacheNormalization(t *testing.T) {
	mgr := NewImageManager()
	base := tempPNG(t, "x.png", 12, 12)
	dir := filepath.Dir(base)
	name := filepath.Base(base)

	variants := []string{
		base,
		filepath.Join(dir, ".", name),
		filepath.Join(dir, "nested", "..", name),
	}
	first, err := mgr.Load(variants[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range variants[1:] {
		a, err := mgr.Load(v)
		if err != nil {
			t.Fatalf("Load(%s): %v", v, err)
		}
		if a != first {
			t.Fatalf("Load(%s) returned a different asset; cache key not normalized", v)
		}
	}

	// Release, then reload the same identity → a new valid asset; old stays released.
	if !mgr.TryRelease(first) {
		t.Fatal("TryRelease failed")
	}
	if !first.IsReleased() {
		t.Fatal("released asset must report IsReleased")
	}
	reloaded, err := mgr.Load(base)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded == first {
		t.Fatal("Load after release must create a new asset")
	}
	if reloaded.IsReleased() {
		t.Fatal("reloaded asset must not be released")
	}
}

// TestMountUnmountReleaseStress hammers the mount → use → unmount → release cycle
// to surface usage-count leaks, negatives, stale registrations, or double frees.
func TestMountUnmountReleaseStress(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "s.png", 8, 8)
	const N = 300
	for i := 0; i < N; i++ {
		a, err := mgr.Load(p)
		if err != nil {
			t.Fatalf("iter %d Load: %v", i, err)
		}
		if a.IsReleased() {
			t.Fatalf("iter %d: asset already released", i)
		}
		w := Image(a)
		w.Mount(uitest.NewMockContext())
		if a.Users() != 1 {
			t.Fatalf("iter %d: users = %d, want 1", i, a.Users())
		}
		w.Unmount()
		if a.Users() != 0 {
			t.Fatalf("iter %d: after unmount users = %d, want 0", i, a.Users())
		}
		if !mgr.TryRelease(a) {
			t.Fatalf("iter %d: TryRelease failed", i)
		}
		if !a.IsReleased() {
			t.Fatalf("iter %d: asset not released", i)
		}
	}
}

// TestDoubleUnmount ensures unmounting the same widget twice cannot drive the
// usage count negative or panic.
func TestDoubleUnmount(t *testing.T) {
	mgr := NewImageManager()
	p := tempPNG(t, "du.png", 8, 8)
	a, _ := mgr.Load(p)

	w := Image(a)
	w.Mount(uitest.NewMockContext())
	w.Unmount()
	if a.Users() != 0 {
		t.Fatalf("after unmount users = %d, want 0", a.Users())
	}
	w.Unmount() // must be a no-op, not negative
	if a.Users() != 0 {
		t.Fatalf("after double unmount users = %d, want 0", a.Users())
	}
}

var (
	_ widget.Widget        = (*imageWidget)(nil)
	_ widget.Lifecycle     = (*imageWidget)(nil)
	_ gesture.GestureAware = (*clickableWidget)(nil)
)
