package render

import (
	"testing"

	"github.com/abdallah-elbeheiry/AqwaborEngine/ecs"
)

func TestRegisterECSIdempotent(t *testing.T) {
	w := ecs.NewWorld()
	a, err := RegisterECS(w)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RegisterECS(w)
	if err != nil {
		t.Fatal("RegisterECS should be idempotent:", err)
	}
	if a.Transform.ID() != b.Transform.ID() {
		t.Fatal("second registration made new components")
	}
}

func TestMustRegisterECS(t *testing.T) {
	w := ecs.NewWorld()
	MustRegisterECS(w)
}

func TestSpawnSprite(t *testing.T) {
	w := ecs.NewWorld()
	comps := MustRegisterECS(w)

	e := SpawnSprite(w, comps,
		Transform{X: 10, Y: 20, SX: 2, SY: 2},
		Color{R: 1, G: 0, B: 0, A: 1},
		Sprite{Layer: 1, Flags: 42},
	)

	if !w.Alive(e) {
		t.Fatal("entity should be alive")
	}
	tr, ok := comps.Transform.Get(e)
	if !ok {
		t.Fatal("missing Transform")
	}
	if tr.X != 10 || tr.Y != 20 || tr.SX != 2 || tr.SY != 2 {
		t.Fatalf("Transform = %+v, want X=10 Y=20 SX=2 SY=2", tr)
	}
	col, ok := comps.Color.Get(e)
	if !ok {
		t.Fatal("missing Color")
	}
	if col.R != 1 || col.G != 0 || col.B != 0 || col.A != 1 {
		t.Fatalf("Color = %+v, want R=1 G=0 B=0 A=1", col)
	}
	s, ok := comps.Sprite.Get(e)
	if !ok {
		t.Fatal("missing Sprite")
	}
	if s.Layer != 1 || s.Flags != 42 {
		t.Fatalf("Sprite = %+v, want Layer=1 Flags=42", s)
	}
}

// A spawned sprite is awake, because extraction walks the awake rows and a
// sprite that is not in that set is not drawn. Shared component instances are
// gone with the pool that backed them: one dense array per type has no place to
// put a value two entities point at, and colour sharing is answered by the
// palette index the instance layout carries instead.
func TestSpawnedSpriteIsAwake(t *testing.T) {
	w := ecs.NewWorld()
	comps := MustRegisterECS(w)

	e := SpawnSprite(w, comps, Transform{}, Color{R: 1, A: 1}, Sprite{})
	if !comps.Transform.Awake(e) {
		t.Fatal("a spawned sprite is asleep, so extraction will not see it")
	}
	if comps.Transform.AwakeLen() != 1 {
		t.Fatalf("awake rows = %d, want 1", comps.Transform.AwakeLen())
	}

	seen := 0
	ecs.Each2(comps.Transform, comps.Sprite, func(ecs.Entity, *Transform, *Sprite) { seen++ })
	if seen != 1 {
		t.Fatalf("extraction would see %d sprites, want 1", seen)
	}
}

func TestTransformDefaults(t *testing.T) {
	var tr Transform
	if tr.SX != 0 || tr.SY != 0 {
		t.Fatal("zero-value Transform should have SX=0 SY=0 (default handled by extract)")
	}
}
