package ecs

import (
	"math"
	"testing"
)

func TestSIMDGroupLen(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	for i := range 5 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
		MustAdd[Velocity](w, e, Velocity{DX: 1, DY: 1})
	}
	e := w.Create()
	MustAdd[Position](w, e, Position{X: 99, Y: 99})

	g := NewGroup(w, Position{}, Velocity{})
	if g.Len() != 5 {
		t.Fatalf("expected Len()=5, got %d", g.Len())
	}
}

func TestSIMDFloat64s(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 4 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i * 10), Y: float64(i)})
	}

	g := NewGroup(w, Position{})
	xs := g.Float64s(func(c any) *float64 { return &c.(*Position).X })

	if len(xs) != 4 {
		t.Fatalf("expected 4 values, got %d", len(xs))
	}
	for i, v := range xs {
		if v != float64(i*10) {
			t.Fatalf("xs[%d] = %v, want %v", i, v, float64(i*10))
		}
	}
}

func TestSIMDFloat32s(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 3 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: float64(i * 2)})
	}

	g := NewGroup(w, Position{})
	ys := g.Float32s(func(c any) *float32 {
		v := float32(c.(*Position).Y)
		return &v
	})

	if len(ys) != 3 {
		t.Fatalf("expected 3 values, got %d", len(ys))
	}
	for i, v := range ys {
		if v != float32(i*2) {
			t.Fatalf("ys[%d] = %v, want %v", i, v, float32(i*2))
		}
	}
}

func TestSIMDAddVec(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	for i := range 4 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
		MustAdd[Velocity](w, e, Velocity{DX: float64(i * 10), DY: 1})
	}

	g := NewGroup(w, Position{}, Velocity{})
	g.AddVec(
		func(c any) *float64 { return &c.(*Position).X },
		func(c any) *float64 { return &c.(*Velocity).DX },
	)

	expected := []float64{0, 11, 22, 33}
	idx := 0
	NewQuery[Position](w).ForEach(func(e Entity, p *Position) {
		if p.X != expected[idx] {
			t.Fatalf("entity %d: X = %v, want %v", idx, p.X, expected[idx])
		}
		idx++
	})
}

func TestSIMDAddNumber(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 3 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}

	g := NewGroup(w, Position{})
	g.AddNumber(func(c any) *float64 { return &c.(*Position).X }, 100)

	expected := []float64{100, 101, 102}
	idx := 0
	NewQuery[Position](w).ForEach(func(e Entity, p *Position) {
		if p.X != expected[idx] {
			t.Fatalf("entity %d: X = %v, want %v", idx, p.X, expected[idx])
		}
		idx++
	})
}

func TestSIMDMulNumber(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 3 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i + 1), Y: 0})
	}

	g := NewGroup(w, Position{})
	g.MulNumber(func(c any) *float64 { return &c.(*Position).X }, 2.5)

	expected := []float64{2.5, 5.0, 7.5}
	idx := 0
	NewQuery[Position](w).ForEach(func(e Entity, p *Position) {
		if math.Abs(p.X-expected[idx]) > 1e-10 {
			t.Fatalf("entity %d: X = %v, want %v", idx, p.X, expected[idx])
		}
		idx++
	})
}

func TestSIMDSubVec(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	for i := range 3 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64((i + 1) * 10), Y: 0})
		MustAdd[Velocity](w, e, Velocity{DX: float64(i + 1), DY: 0})
	}

	g := NewGroup(w, Position{}, Velocity{})
	g.SubVec(
		func(c any) *float64 { return &c.(*Position).X },
		func(c any) *float64 { return &c.(*Velocity).DX },
	)

	expected := []float64{9, 18, 27}
	idx := 0
	NewQuery[Position](w).ForEach(func(e Entity, p *Position) {
		if p.X != expected[idx] {
			t.Fatalf("entity %d: X = %v, want %v", idx, p.X, expected[idx])
		}
		idx++
	})
}

func TestSIMDMulVec(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	for i := range 3 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i + 1), Y: 0})
		MustAdd[Velocity](w, e, Velocity{DX: float64(i + 1), DY: 0})
	}

	g := NewGroup(w, Position{}, Velocity{})
	g.MulVec(
		func(c any) *float64 { return &c.(*Position).X },
		func(c any) *float64 { return &c.(*Velocity).DX },
	)

	expected := []float64{1, 4, 9}
	idx := 0
	NewQuery[Position](w).ForEach(func(e Entity, p *Position) {
		if p.X != expected[idx] {
			t.Fatalf("entity %d: X = %v, want %v", idx, p.X, expected[idx])
		}
		idx++
	})
}

func TestSIMDDivVec(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	for i := range 3 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64((i + 1) * 10), Y: 0})
		MustAdd[Velocity](w, e, Velocity{DX: float64(i + 1), DY: 0})
	}

	g := NewGroup(w, Position{}, Velocity{})
	g.DivVec(
		func(c any) *float64 { return &c.(*Position).X },
		func(c any) *float64 { return &c.(*Velocity).DX },
	)

	expected := []float64{10, 10, 10}
	idx := 0
	NewQuery[Position](w).ForEach(func(e Entity, p *Position) {
		if p.X != expected[idx] {
			t.Fatalf("entity %d: X = %v, want %v", idx, p.X, expected[idx])
		}
		idx++
	})
}

func TestSIMDEmptyGroup(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	g := NewGroup(w, Position{})
	xs := g.Float64s(func(c any) *float64 { return &c.(*Position).X })
	if len(xs) != 0 {
		t.Fatalf("expected empty slice, got %d elements", len(xs))
	}

	g.AddNumber(func(c any) *float64 { return &c.(*Position).X }, 1)
	g.MulNumber(func(c any) *float64 { return &c.(*Position).X }, 2)
}

// --- Entity-based SIMD tests ---

func TestSIMDEntityGroupFloat64s(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 10, Y: 0})
	MustAdd[Position](w, e2, Position{X: 20, Y: 0})
	MustAdd[Position](w, e3, Position{X: 30, Y: 0})

	g := NewGroupOf(w, e1, e3) // only e1 and e3
	xs := g.Float64s(func(c any) *float64 { return &c.(*Position).X })

	if len(xs) != 2 {
		t.Fatalf("expected 2 values, got %d", len(xs))
	}
	if xs[0] != 10 || xs[1] != 30 {
		t.Fatalf("expected [10, 30], got %v", xs)
	}
}

func TestSIMDEntityGroupAddNumber(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 0})
	MustAdd[Position](w, e2, Position{X: 2, Y: 0})
	MustAdd[Position](w, e3, Position{X: 3, Y: 0})

	g := NewGroupOf(w, e1, e3)
	g.AddNumber(func(c any) *float64 { return &c.(*Position).X }, 100)

	p1, _ := Get[Position](w, e1)
	p2, _ := Get[Position](w, e2)
	p3, _ := Get[Position](w, e3)

	if p1.X != 101 {
		t.Fatalf("e1.X = %v, want 101", p1.X)
	}
	if p2.X != 2 {
		t.Fatalf("e2.X should be unchanged at 2, got %v", p2.X)
	}
	if p3.X != 103 {
		t.Fatalf("e3.X = %v, want 103", p3.X)
	}
}

func TestSIMDEntityGroupFilter(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 10 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}

	// Get all, then filter to X >= 5
	all := NewQuery[Position](w)
	var entities []Entity
	all.ForEach(func(e Entity, p *Position) {
		if p.X >= 5 {
			entities = append(entities, e)
		}
	})

	g := NewGroupFrom(w, entities)
	g.MulNumber(func(c any) *float64 { return &c.(*Position).X }, 10)

	// Verify only the filtered entities were multiplied
	NewQuery[Position](w).ForEach(func(e Entity, p *Position) {
		if p.X >= 50 {
			// Should have been multiplied
		} else if p.X >= 5 {
			t.Fatalf("entity with X=%v should have been multiplied", p.X)
		}
	})
}
