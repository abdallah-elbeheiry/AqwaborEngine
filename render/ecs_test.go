package render

import (
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

func TestSpawnSprite(t *testing.T) {
	w := ecs.NewWorld()
	MustRegisterECS(w)

	e := SpawnSprite(w,
		Transform{X: 10, Y: 20, SX: 2, SY: 2},
		Color{R: 1, G: 0, B: 0, A: 1},
		Sprite{Layer: 1, Flags: 42},
	)

	if !w.Alive(e) {
		t.Fatal("entity should be alive")
	}
	tr, ok := ecs.Get[Transform](w, e)
	if !ok {
		t.Fatal("missing Transform")
	}
	if tr.X != 10 || tr.Y != 20 || tr.SX != 2 || tr.SY != 2 {
		t.Fatalf("Transform = %+v, want X=10 Y=20 SX=2 SY=2", tr)
	}
	c, ok := ecs.Get[Color](w, e)
	if !ok {
		t.Fatal("missing Color")
	}
	if c.R != 1 || c.G != 0 || c.B != 0 || c.A != 1 {
		t.Fatalf("Color = %+v, want R=1 G=0 B=0 A=1", c)
	}
	s, ok := ecs.Get[Sprite](w, e)
	if !ok {
		t.Fatal("missing Sprite")
	}
	if s.Layer != 1 || s.Flags != 42 {
		t.Fatalf("Sprite = %+v, want Layer=1 Flags=42", s)
	}
}

func TestSpawnMultipleSharesColor(t *testing.T) {
	w := ecs.NewWorld()
	MustRegisterECS(w)

	red := ecs.MustCreate[Color](w, Color{R: 1, A: 1})
	e1 := w.Create()
	e2 := w.Create()
	ecs.MustAttach[Color](w, e1, red)
	ecs.MustAttach[Color](w, e2, red)

	c1, _ := ecs.Get[Color](w, e1)
	c2, _ := ecs.Get[Color](w, e2)
	if c1 != c2 {
		t.Fatal("shared color handle should return same pointer")
	}
}

func TestTransformDefaults(t *testing.T) {
	var tr Transform
	if tr.SX != 0 || tr.SY != 0 {
		t.Fatal("zero-value Transform should have SX=0 SY=0 (default handled by extract)")
	}
}
