package ui

import (
	"testing"

	"github.com/gogpu/ui/event"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/gesture"
)

func TestClickableGesturePipeline(t *testing.T) {
	var clicks int
	c := Clickable(Label("x"), func() { clicks++ }).(*clickableWidget)

	// Mimic app/window.go: collect recognizers, add pointer, route, sweep.
	arena := gesture.NewArena()
	down := &gesture.PointerEvent{
		EventType:      gesture.PointerDown,
		PointerID:      1,
		PointerType:    gesture.PointerTypeMouse,
		Position:       geometry.Pt(5, 5),
		GlobalPosition: geometry.Pt(105, 105),
		Button:         event.ButtonLeft,
		Buttons:        event.ButtonStateLeft,
	}
	recs := c.GestureHitTest(down.Position)
	if len(recs) != 1 {
		t.Fatalf("expected 1 recognizer, got %d", len(recs))
	}
	for _, r := range recs {
		r.AddPointer(down, arena)
	}
	arena.Close(down.PointerID)

	up := &gesture.PointerEvent{
		EventType:      gesture.PointerUp,
		PointerID:      1,
		PointerType:    gesture.PointerTypeMouse,
		Position:       geometry.Pt(5, 5),
		GlobalPosition: geometry.Pt(105, 105),
		Button:         event.ButtonLeft,
		Buttons:        0,
	}
	arena.Route(up)
	arena.Sweep(up.PointerID)

	if clicks != 1 {
		t.Fatalf("gesture pipeline clicks = %d, want 1", clicks)
	}
}
