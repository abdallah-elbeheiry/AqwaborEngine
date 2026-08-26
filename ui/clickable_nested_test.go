package ui

import (
	"testing"

	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/button"
	"github.com/gogpu/ui/geometry"
)

// rectCenter returns the center point of a screen rect.
func rectCenter(r geometry.Rect) geometry.Point {
	return geometry.Pt(r.Min.X+r.Width()/2, r.Min.Y+r.Height()/2)
}

func TestClickableWrapsButton(t *testing.T) {
	a := app.New()
	var outer, inner int
	b := button.New(button.Text("Inner"), button.OnClick(func() { inner++ }))
	c := Clickable(Widget(b), func() { outer++ }).(*clickableWidget)
	c.SetVisible(true)
	c.SetEnabled(true)

	a.SetRoot(rootFrom(Column(Label("Aqwabor Engine"), Widget(c))))
	a.Frame()

	bs := b.ScreenBounds()
	if bs.IsEmpty() {
		t.Fatalf("button ScreenBounds empty: %+v", bs)
	}
	tapAt(a, rectCenter(bs))

	if inner != 1 {
		t.Fatalf("inner clicks = %d, want 1", inner)
	}
	if outer != 0 {
		t.Fatalf("outer clicks = %d, want 0 (parent must yield to nested button)", outer)
	}
}

// TestClickableWrapsColumnButton proves the suppression is position-specific:
// tapping the nested button fires only the button, while tapping a
// non-interactive region of the same clickable fires only the parent.
func TestClickableWrapsColumnButton(t *testing.T) {
	a := app.New()
	var outer, inner int
	b := button.New(button.Text("Inner"), button.OnClick(func() { inner++ }))
	c := Clickable(Column(Widget(b)).Padding(20), func() { outer++ }).(*clickableWidget)
	c.SetVisible(true)
	c.SetEnabled(true)

	a.SetRoot(rootFrom(Column(Label("Aqwabor Engine"), Widget(c))))
	a.Frame()

	// Tap directly on the button → only the button fires.
	bs := b.ScreenBounds()
	tapAt(a, rectCenter(bs))
	if inner != 1 || outer != 0 {
		t.Fatalf("on-button: inner=%d outer=%d, want 1/0", inner, outer)
	}

	// Tap the clickable's own padding (outside the button) → only the parent fires.
	cs := c.ScreenBounds()
	off := geometry.Pt(float32(cs.Min.X+cs.Width()/2), float32(cs.Max.Y-3))
	if b.ScreenBounds().Contains(off) {
		t.Fatalf("chosen off-button point %+v lies inside the button", off)
	}
	tapAt(a, off)
	if inner != 1 || outer != 1 {
		t.Fatalf("off-button: inner=%d outer=%d, want 1/1", inner, outer)
	}
}

// TestClickableMultiNested validates that local suppression composes recursively:
// A > B > Button. The innermost interactive widget wins; the others stay silent.
func TestClickableMultiNested(t *testing.T) {
	a := app.New()
	var aClicks, bClicks, cClicks int
	cBtn := button.New(button.Text("Inner"), button.OnClick(func() { cClicks++ }))
	b := Clickable(Column(Widget(cBtn)).Padding(20), func() { bClicks++ }).(*clickableWidget)
	aClk := Clickable(Column(Widget(b)).Padding(20), func() { aClicks++ }).(*clickableWidget)
	aClk.SetVisible(true)
	aClk.SetEnabled(true)
	b.SetVisible(true)
	b.SetEnabled(true)

	a.SetRoot(rootFrom(Column(Label("Aqwabor Engine"), Widget(aClk))))
	a.Frame()

	// Innermost button → only C.
	tapAt(a, rectCenter(cBtn.ScreenBounds()))
	if cClicks != 1 || bClicks != 0 || aClicks != 0 {
		t.Fatalf("on-C: a=%d b=%d c=%d, want 0/0/1", aClicks, bClicks, cClicks)
	}

	// B's non-interactive padding → only B.
	bb := b.ScreenBounds()
	bp := geometry.Pt(float32(bb.Min.X+bb.Width()/2), float32(bb.Max.Y-3))
	if cBtn.ScreenBounds().Contains(bp) {
		t.Fatalf("B off-point %+v lies inside C", bp)
	}
	tapAt(a, bp)
	if cClicks != 1 || bClicks != 1 || aClicks != 0 {
		t.Fatalf("on-B: a=%d b=%d c=%d, want 0/1/1", aClicks, bClicks, cClicks)
	}

	// A's non-interactive padding → only A.
	ab := aClk.ScreenBounds()
	ap := geometry.Pt(float32(ab.Min.X+ab.Width()/2), float32(ab.Max.Y-3))
	if b.ScreenBounds().Contains(ap) {
		t.Fatalf("A off-point %+v lies inside B", ap)
	}
	tapAt(a, ap)
	if cClicks != 1 || bClicks != 1 || aClicks != 1 {
		t.Fatalf("on-A: a=%d b=%d c=%d, want 1/1/1", aClicks, bClicks, cClicks)
	}
}
