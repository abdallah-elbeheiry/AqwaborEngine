package headless

import (
	"testing"

	"aqwabor/input"
)

func TestHeadlessInjectsAndPolls(t *testing.T) {
	b := New()

	b.KeyDown(input.KeySpace)
	b.MouseMove(10, 20)
	b.MouseDown(input.MouseButtonLeft, 10, 20)

	events := b.Poll()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Kind != input.EventKeyDown || events[0].Key != input.KeySpace {
		t.Fatalf("unexpected first event %+v", events[0])
	}
	if events[1].Kind != input.EventMouseMove || events[1].X != 10 || events[1].Y != 20 {
		t.Fatalf("unexpected second event %+v", events[1])
	}
	if events[2].Kind != input.EventMouseDown || events[2].Button != input.MouseButtonLeft {
		t.Fatalf("unexpected third event %+v", events[2])
	}

	// Poll clears the queue.
	if len(b.Poll()) != 0 {
		t.Fatal("queue should be empty after Poll")
	}
}

func TestHeadlessWithManager(t *testing.T) {
	b := New()
	m := input.NewManager(b)

	jump := m.Action("jump")
	m.BindKey(jump, input.KeyW)
	pressed := 0
	jump.OnPressed(func(input.Context) { pressed++ })

	b.KeyDown(input.KeyW)
	m.Update(0)
	if pressed != 1 {
		t.Fatalf("manager should consume headless event, got %d", pressed)
	}

	b.KeyUp(input.KeyW)
	m.Update(0)
	if pressed != 1 {
		t.Fatal("release should not re-fire press")
	}
}
