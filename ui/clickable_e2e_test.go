package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
	"github.com/gogpu/ui/widget"
)

// testBox is a deterministic 100x100 leaf widget used by the e2e tests below.
type testBox struct {
	widget.WidgetBase
}

func (t *testBox) Layout(_ widget.Context, c geometry.Constraints) geometry.Size {
	s := c.Constrain(geometry.Sz(100, 100))
	t.SetBounds(geometry.FromPointSize(t.Position(), s))
	return s
}

func (t *testBox) Draw(_ widget.Context, _ widget.Canvas)     {}
func (t *testBox) Event(_ widget.Context, _ event.Event) bool { return false }
func (t *testBox) Children() []widget.Widget                  { return nil }

// loadTestImage writes a small PNG to a temp file and loads it as an asset.
func loadTestImage(t *testing.T) *ImageAsset {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "px.png")
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	for y := range 50 {
		for x := range 50 {
			img.Set(x, y, color.RGBA{200, 30, 30, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	asset, err := NewImageManager().Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return asset
}

// tapAt dispatches a left-button press+release at p through the real window
// gesture pipeline.
func tapAt(a *app.App, p geometry.Point) {
	down := &gesture.PointerEvent{
		Base: event.NewBase(event.TypeMouse, event.ModNone), EventType: gesture.PointerDown,
		PointerID: 1, PointerType: gesture.PointerTypeMouse, Position: p, GlobalPosition: p,
		Button: event.ButtonLeft, Buttons: event.ButtonStateLeft, Pressure: 0.5,
	}
	up := &gesture.PointerEvent{
		Base: event.NewBase(event.TypeMouse, event.ModNone), EventType: gesture.PointerUp,
		PointerID: 1, PointerType: gesture.PointerTypeMouse, Position: p, GlobalPosition: p,
		Button: event.ButtonLeft, Buttons: 0, Pressure: 0,
	}
	a.Window().HandlePointerEvent(down)
	a.Window().HandlePointerEvent(up)
}

// rootFrom builds the same Box>Align>Column wrapper that ui.App.applyRoot uses,
// so the e2e tests exercise the clickable exactly as main.go nests it.
func rootFrom(child Widget) Widget {
	bg := BackgroundColor(LightPurple)
	return Box(Align(Column(child).Padding(24).Gap(12).Background(bg), CrossCenter)).
		Background(bg).CrossAlign(CrossCenter)
}

// TestClickableNestedE2E verifies a Clickable nested in the real Box>Align>
// Column tree receives exactly one click when tapped at its on-screen center.
func TestClickableNestedE2E(t *testing.T) {
	a := app.New()
	var clicks int
	c := &clickableWidget{content: &testBox{}, onClick: func() { clicks++ }}
	c.SetVisible(true)
	c.SetEnabled(true)

	a.SetRoot(rootFrom(Column(Label("Aqwabor Engine"), Widget(c))))
	a.Frame()

	sb := c.ScreenBounds()
	if sb.IsEmpty() {
		t.Fatalf("clickable ScreenBounds empty: %+v", sb)
	}
	tapAt(a, geometry.Pt(float32(sb.Min.X+sb.Width()/2), float32(sb.Min.Y+sb.Height()/2)))

	if clicks != 1 {
		t.Fatalf("nested clickable clicks = %d, want 1", clicks)
	}
}

// TestClickableWithImageE2E verifies a Clickable wrapping a real loaded image
// (the ImageButton case) fires exactly once.
func TestClickableWithImageE2E(t *testing.T) {
	asset := loadTestImage(t)

	a := app.New()
	var clicks int
	c := &clickableWidget{content: Image(asset).Size(96, 96).Fit(Contain), onClick: func() { clicks++ }}
	c.SetVisible(true)
	c.SetEnabled(true)

	a.SetRoot(rootFrom(Column(Label("Aqwabor Engine"), Widget(c))))
	a.Frame()

	sb := c.ScreenBounds()
	if sb.IsEmpty() {
		t.Fatalf("clickable ScreenBounds empty: %+v", sb)
	}
	tapAt(a, geometry.Pt(float32(sb.Min.X+sb.Width()/2), float32(sb.Min.Y+sb.Height()/2)))

	if clicks != 1 {
		t.Fatalf("image clickable clicks = %d, want 1", clicks)
	}
}

// TestImageOnClickE2E verifies ui.Image(...).OnClick(...) returns a clickable
// widget that fires exactly once.
func TestImageOnClickE2E(t *testing.T) {
	asset := loadTestImage(t)

	a := app.New()
	var clicks int
	w := Image(asset).Size(96, 96).Fit(Contain).OnClick(func() { clicks++ })
	cw, ok := w.(*clickableWidget)
	if !ok {
		t.Fatalf("OnClick did not return a *clickableWidget, got %T", w)
	}

	a.SetRoot(rootFrom(Column(Label("Aqwabor Engine"), w)))
	a.Frame()

	sb := cw.ScreenBounds()
	if sb.IsEmpty() {
		t.Fatalf("image ScreenBounds empty: %+v", sb)
	}
	tapAt(a, geometry.Pt(float32(sb.Min.X+sb.Width()/2), float32(sb.Min.Y+sb.Height()/2)))

	if clicks != 1 {
		t.Fatalf("Image.OnClick clicks = %d, want 1", clicks)
	}
}
