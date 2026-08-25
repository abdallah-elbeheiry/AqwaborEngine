package ui

import (
	"math"
	"testing"

	"github.com/gogpu/ui/theme"
	"github.com/gogpu/ui/uitest"
	"github.com/gogpu/ui/widget"
)

func TestWidgetBuilders(t *testing.T) {
	var _ Widget = Label("hello").FontSize(24).Bold()
	var _ Widget = Button("ping", func() {})
	var _ Widget = Column(Label("a"), Label("b")).Padding(12).Gap(8)
	var _ Widget = Row(Label("a")).Padding(4)
	var _ Widget = Box(Label("x")).Background(widget.RGBA8(255, 255, 255, 255))

	// alignment + theme helpers
	var _ Widget = CenterX(Column(Label("a")))
	var _ Widget = Align(Column(Label("a")), CrossStart)
	var _ Widget = Align(Column(Label("a")), CrossEnd)
	var _ Widget = CenterText(Label("b"))

	// the six pre-made themes are usable *Theme values
	_ = LightPurple
	_ = DarkPurple
	_ = Light
	_ = Dark
	_ = LightBlue
	_ = DarkBlue

	// a hand-built theme from scratch
	_ = &Theme{
		Primary:     Hex(0x6750A4),
		OnPrimary:   Hex(0xFFFFFFFF),
		Secondary:   Hex(0x9A7BD0),
		OnSecondary: Hex(0xFFFFFFFF),
		Background:  Hex(0xF6F2FA),
		Surface:     Hex(0xFFFFFFFF),
		OnSurface:   Hex(0x2A2433),
		Error:       Hex(0xB00020),
		OnError:     Hex(0xFFFFFFFF),
	}
}

func TestThemeToGogpu(t *testing.T) {
	for _, th := range []*Theme{LightPurple, DarkPurple, Light, Dark, LightBlue, DarkBlue} {
		g := th.toGogpu()
		if g == nil {
			t.Fatalf("toGogpu returned nil for %+v", th)
		}
		if !colorsClose(g.Colors.Background, th.Background) {
			t.Errorf("background not propagated")
		}
		if !colorsClose(g.Colors.Primary, th.Primary) {
			t.Errorf("primary not propagated")
		}
		wantMode := theme.ModeLight
		if th.Dark {
			wantMode = theme.ModeDark
		}
		if g.Mode != wantMode {
			t.Errorf("mode mismatch: got %v want %v", g.Mode, wantMode)
		}
	}
}

func colorsClose(a, b widget.Color) bool {
	return math.Abs(float64(a.R-b.R)) < 1e-3 &&
		math.Abs(float64(a.G-b.G)) < 1e-3 &&
		math.Abs(float64(a.B-b.B)) < 1e-3 &&
		math.Abs(float64(a.A-b.A)) < 1e-3
}

// TestFoxPNG exercises the real asset pipeline against the project's bundled
// examples/fox.png (a 1024x1024 PNG): load through the app's image manager,
// verify dimensions/caching, compose it into the standard layout widgets, and
// confirm explicit release succeeds once the widgets are removed.
func TestFoxPNG(t *testing.T) {
	const foxPath = "../examples/fox.png"

	app, err := New(Config{Title: "fox", W: 100, H: 100})
	if err != nil {
		t.Fatal(err)
	}

	fox, err := app.Images().Load(foxPath)
	if err != nil {
		t.Fatalf("Load(%s): %v", foxPath, err)
	}
	if fox == nil {
		t.Fatal("nil asset")
	}
	if w, h := fox.Size(); w != 1024 || h != 1024 {
		t.Errorf("fox size = %dx%d, want 1024x1024", w, h)
	}
	if fox.Users() != 0 {
		t.Errorf("fresh asset users = %d, want 0", fox.Users())
	}

	// Repeated loads return the same canonical asset (no re-decode / dup resource).
	if again, err := app.Images().Load(foxPath); err != nil || again != fox {
		t.Fatalf("repeated Load must return the same asset (err=%v, same=%v)", err, again == fox)
	}

	// Compose the loaded image into the standard layout widgets.
	compose := Column(
		Image(fox).Size(200, 200).Fit(Contain),
		Row(Image(fox).Size(48, 48), Label("Fox")),
		Box(Image(fox).Size(64, 64)),
		Clickable(Image(fox).Size(32, 32), func() {}),
	)
	uitest.LayoutWidget(compose, 800, 600)
	canvas := uitest.DrawWidgetWithContext(compose, uitest.NewMockContext())
	if len(canvas.Images) == 0 {
		t.Fatal("expected fox.png to be drawn at least once")
	}

	// An in-tree widget holds an active user; release is refused until removed.
	foxWidget := Image(fox)
	foxWidget.Mount(uitest.NewMockContext())
	if fox.Users() != 1 {
		t.Fatalf("mounted widget users = %d, want 1", fox.Users())
	}
	if app.Images().TryRelease(fox) {
		t.Fatal("TryRelease must refuse while the widget is live")
	}
	foxWidget.Unmount()
	if fox.Users() != 0 {
		t.Fatalf("after unmount users = %d, want 0", fox.Users())
	}
	if !app.Images().TryRelease(fox) {
		t.Fatal("TryRelease must succeed after the widget is removed")
	}
}
