package ui

import (
	"math"
	"testing"

	"github.com/gogpu/ui/theme"
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
