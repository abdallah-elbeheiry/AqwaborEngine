package ecs

import (
	"testing"
)

// Test component types
type Position struct {
	X, Y float64
}

type Velocity struct {
	DX, DY float64
}

type Health struct {
	Current, Max int
}

// --- Registration ---

func TestRegister(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)
	Register[Health](w)
	Register[Position](w) // idempotent
}

func TestRegisterUnregistered(t *testing.T) {
	w := NewWorld()
	e := w.Create()

	err := Add[Position](w, e, Position{X: 1})
	if err == nil {
		t.Fatal("expected error adding unregistered component")
	}
}

// --- Entity lifecycle ---

func TestCreateEntity(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	if !w.Alive(e) {
		t.Fatal("entity should be alive")
	}
}

func TestDestroyEntity(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	w.Destroy(e, false)
	if w.Alive(e) {
		t.Fatal("entity should be dead")
	}
}

func TestDestroyAlreadyDead(t *testing.T) {
	w := NewWorld()
	e := w.Create()
	w.Destroy(e, false)
	w.Destroy(e, false)
}

func TestDestroyWithCascade(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	e := w.Create()
	MustAdd[Position](w, e, Position{X: 1, Y: 2})
	w.Destroy(e, true)
	if w.Alive(e) {
		t.Fatal("entity should be dead")
	}
}

// --- Add / Get / Has / Remove ---

func TestAddGetHas(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	MustAdd[Position](w, e, Position{X: 3, Y: 4})
	if !Has[Position](w, e) {
		t.Fatal("entity should have Position")
	}
	pos, ok := Get[Position](w, e)
	if !ok {
		t.Fatal("Get Position failed")
	}
	if pos.X != 3 || pos.Y != 4 {
		t.Fatalf("expected (3,4), got (%v,%v)", pos.X, pos.Y)
	}
}

func TestAddMultipleComponents(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)
	Register[Health](w)

	e := w.Create()
	MustAdd[Position](w, e, Position{X: 1, Y: 2})
	MustAdd[Velocity](w, e, Velocity{DX: 0.1, DY: 0.2})
	MustAdd[Health](w, e, Health{Current: 100, Max: 100})

	if !Has[Position](w, e) {
		t.Fatal("missing Position")
	}
	if !Has[Velocity](w, e) {
		t.Fatal("missing Velocity")
	}
	if !Has[Health](w, e) {
		t.Fatal("missing Health")
	}

	pos, _ := Get[Position](w, e)
	if pos.X != 1 {
		t.Fatal("wrong Position")
	}
	vel, _ := Get[Velocity](w, e)
	if vel.DX != 0.1 {
		t.Fatal("wrong Velocity")
	}
	hp, _ := Get[Health](w, e)
	if hp.Current != 100 {
		t.Fatal("wrong Health")
	}
}

func TestAddDuplicateIgnored(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	MustAdd[Position](w, e, Position{X: 1, Y: 2})
	MustAdd[Position](w, e, Position{X: 99, Y: 99})

	pos, _ := Get[Position](w, e)
	if pos.X != 1 || pos.Y != 2 {
		t.Fatal("second Add should be ignored")
	}
}

func TestHasDeadEntity(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	w.Destroy(e, false)
	if Has[Position](w, e) {
		t.Fatal("Has on dead entity should return false")
	}
}

func TestGetDeadEntity(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	w.Destroy(e, false)
	_, ok := Get[Position](w, e)
	if ok {
		t.Fatal("Get on dead entity should return false")
	}
}

func TestRemove(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	e := w.Create()
	MustAdd[Position](w, e, Position{X: 1, Y: 2})
	MustAdd[Velocity](w, e, Velocity{DX: 0.1, DY: 0.2})

	MustRemove[Position](w, e)
	if Has[Position](w, e) {
		t.Fatal("Position should be removed")
	}
	if !Has[Velocity](w, e) {
		t.Fatal("Velocity should still exist")
	}
}

func TestRemoveNonExistent(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	err := Remove[Position](w, e)
	if err != nil {
		t.Fatal("removing non-existent component should not error")
	}
}

func TestRemoveDeadEntity(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	w.Destroy(e, false)
	err := Remove[Position](w, e)
	if err == nil {
		t.Fatal("expected error removing from dead entity")
	}
}

func TestMutateComponent(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	MustAdd[Position](w, e, Position{X: 1, Y: 2})

	pos, _ := Get[Position](w, e)
	pos.X = 10
	pos.Y = 20

	pos2, _ := Get[Position](w, e)
	if pos2.X != 10 || pos2.Y != 20 {
		t.Fatal("mutation should affect data")
	}
}

// --- Destroy with components ---

func TestDestroyReleasesComponents(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Health](w)

	e := w.Create()
	MustAdd[Position](w, e, Position{X: 1, Y: 2})
	MustAdd[Health](w, e, Health{Current: 50, Max: 100})

	w.Destroy(e, true)
	if Has[Position](w, e) {
		t.Fatal("dead entity should not have Position")
	}
	if Has[Health](w, e) {
		t.Fatal("dead entity should not have Health")
	}
}

// --- Sharing (Handle-based) ---

func TestSharingHandle(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	h := MustCreate[Position](w, Position{X: 5, Y: 5})

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()

	MustAttach[Position](w, e1, h)
	MustAttach[Position](w, e2, h)
	MustAttach[Position](w, e3, h)

	p1, _ := Get[Position](w, e1)
	p2, _ := Get[Position](w, e2)
	p3, _ := Get[Position](w, e3)

	if p1.X != 5 || p2.X != 5 || p3.X != 5 {
		t.Fatal("shared data mismatch")
	}

	p1.X = 99
	if p2.X != 99 || p3.X != 99 {
		t.Fatal("shared mutation not visible")
	}
}

func TestSharingDetach(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	h := MustCreate[Position](w, Position{X: 7, Y: 7})

	e1 := w.Create()
	e2 := w.Create()

	MustAttach[Position](w, e1, h)
	MustAttach[Position](w, e2, h)

	MustDetach[Position](w, e1)

	if Has[Position](w, e1) {
		t.Fatal("e1 should not have Position after detach")
	}
	if !Has[Position](w, e2) {
		t.Fatal("e2 should still have Position")
	}
}

func TestAttachDeadEntity(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	h := MustCreate[Position](w, Position{X: 1, Y: 1})
	e := w.Create()
	w.Destroy(e, false)

	err := Attach[Position](w, e, h)
	if err == nil {
		t.Fatal("expected error attaching to dead entity")
	}
}

func TestDetachNonExistent(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	MustDetach[Position](w, e)
}

func TestDestroyHandle(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	h := MustCreate[Position](w, Position{X: 1, Y: 1})
	DestroyHandle(w, h)

	e := w.Create()
	err := Attach[Position](w, e, h)
	if err == nil {
		t.Fatal("expected error attaching stale handle")
	}
}

// --- Query ---

func TestQuery(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()

	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Velocity](w, e2, Velocity{DX: 0.1, DY: 0.1})
	MustAdd[Velocity](w, e3, Velocity{DX: 0.2, DY: 0.2})

	q := NewQuery[Position](w)
	count := 0
	q.ForEach(func(e Entity, p *Position) {
		count++
		p.X += 10
	})

	if count != 2 {
		t.Fatalf("expected 2 entities with Position, got %d", count)
	}

	p1, _ := Get[Position](w, e1)
	p2, _ := Get[Position](w, e2)
	if p1.X != 11 || p2.X != 12 {
		t.Fatal("query mutation should work")
	}
}

func TestQueryEmpty(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	q := NewQuery[Position](w)
	count := 0
	q.ForEach(func(e Entity, p *Position) {
		count++
	})
	if count != 0 {
		t.Fatal("expected 0 results")
	}
}

func TestQueryMutates(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 10 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}

	q := NewQuery[Position](w)
	q.ForEach(func(e Entity, p *Position) {
		p.Y = p.X * 2
	})

	q2 := NewQuery[Position](w)
	q2.ForEach(func(e Entity, p *Position) {
		if p.Y != p.X*2 {
			t.Fatalf("expected Y=%v, got Y=%v", p.X*2, p.Y)
		}
	})
}

// --- Group (signature-based) ---

func TestGroup(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)
	Register[Health](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()

	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Velocity](w, e2, Velocity{DX: 0.1, DY: 0.1})
	MustAdd[Position](w, e3, Position{X: 3, Y: 3})
	MustAdd[Velocity](w, e3, Velocity{DX: 0.2, DY: 0.2})
	MustAdd[Health](w, e3, Health{Current: 100, Max: 100})

	g := NewGroup(w, Position{}, Velocity{})
	count := 0
	g.ForEach(func(e Entity) {
		count++
	})

	if count != 2 {
		t.Fatalf("expected 2 entities with Position+Velocity, got %d", count)
	}
}

func TestGroupEmpty(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	g := NewGroup(w, Position{}, Velocity{})
	count := 0
	g.ForEach(func(e Entity) {
		count++
	})
	if count != 0 {
		t.Fatal("expected 0 results")
	}
}

func TestGroupThreeComponents(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)
	Register[Health](w)

	e1 := w.Create()
	e2 := w.Create()

	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Velocity](w, e1, Velocity{DX: 0.1, DY: 0.1})
	MustAdd[Health](w, e1, Health{Current: 100, Max: 100})

	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Velocity](w, e2, Velocity{DX: 0.2, DY: 0.2})

	g := NewGroup(w, Position{}, Velocity{}, Health{})
	count := 0
	g.ForEach(func(e Entity) {
		count++
	})

	if count != 1 {
		t.Fatalf("expected 1 entity with all 3 components, got %d", count)
	}
}

// --- Entity recycling ---

func TestEntityRecycling(t *testing.T) {
	w := NewWorld()

	e1 := w.Create()
	w.Destroy(e1, false)
	e2 := w.Create()

	if e1.Index() != e2.Index() {
		t.Fatal("recycled entity should reuse index")
	}
	if e1.Generation() >= e2.Generation() {
		t.Fatal("recycled entity should have higher generation")
	}
}

// --- Large batch ---

func TestQueryLargeBatch(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 100 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: float64(i)})
	}

	q := NewQuery[Position](w)
	sum := 0.0
	q.ForEach(func(e Entity, p *Position) {
		sum += p.X
	})

	expected := 0.0
	for i := range 100 {
		expected += float64(i)
	}
	if sum != expected {
		t.Fatalf("expected sum %v, got %v", expected, sum)
	}
}

// --- Detach cascade destruction ---

func TestDetachCascadeDestroy(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	h := MustCreate[Position](w, Position{X: 1, Y: 1})

	e1 := w.Create()
	e2 := w.Create()

	MustAttach[Position](w, e1, h)
	MustAttach[Position](w, e2, h)

	MustDetach[Position](w, e1)
	MustDetach[Position](w, e2)

	// Both detached, handle refcount should be 0
	// Attach to a new entity and destroy with cascade
	e3 := w.Create()
	MustAttach[Position](w, e3, h)
	w.Destroy(e3, true)

	// Handle should be gone now — attaching stale handle should fail
	e4 := w.Create()
	err := Attach[Position](w, e4, h)
	if err == nil {
		t.Fatal("expected error attaching stale handle after destroy")
	}
}

// --- Group: entity-based ---

func TestGroupOf(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()

	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Velocity](w, e2, Velocity{DX: 0.1, DY: 0.1})
	MustAdd[Position](w, e3, Position{X: 3, Y: 3})

	g := NewGroupOf(w, e1, e3)
	if g.Len() != 2 {
		t.Fatalf("expected 2 entities, got %d", g.Len())
	}

	count := 0
	g.ForEach(func(e Entity) {
		count++
		if e != e1 && e != e3 {
			t.Fatalf("unexpected entity %v", e)
		}
	})
	if count != 2 {
		t.Fatalf("expected 2 iterations, got %d", count)
	}
}

func TestGroupOfSkipsDead(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	w.Destroy(e1, false)

	g := NewGroupOf(w, e1, e2)
	if g.Len() != 1 {
		t.Fatalf("expected 1 alive entity, got %d", g.Len())
	}
}

func TestGroupFrom(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Position](w, e3, Position{X: 3, Y: 3})

	slice := []Entity{e1, e3}
	g := NewGroupFrom(w, slice)
	if g.Len() != 2 {
		t.Fatalf("expected 2 entities, got %d", g.Len())
	}
}

func TestGroupAdd(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Position](w, e3, Position{X: 3, Y: 3})

	g := NewGroup(w) // empty
	g.Add(e1)
	g.Add(e2)

	if g.Len() != 2 {
		t.Fatalf("expected 2, got %d", g.Len())
	}

	g.Add(e3)
	if g.Len() != 3 {
		t.Fatalf("expected 3 after Add, got %d", g.Len())
	}
}

func TestGroupAddSkipsDead(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	w.Destroy(e1, false)

	g := NewGroup(w)
	g.Add(e1)
	if g.Len() != 0 {
		t.Fatal("dead entity should not be added")
	}
}

func TestGroupAddEntities(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Position](w, e3, Position{X: 3, Y: 3})

	g := NewGroup(w)
	g.AddEntities(e1, e2, e3)
	if g.Len() != 3 {
		t.Fatalf("expected 3, got %d", g.Len())
	}
}

func TestGroupRemove(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Position](w, e3, Position{X: 3, Y: 3})

	g := NewGroupOf(w, e1, e2, e3)
	g.Remove(e2)
	if g.Len() != 2 {
		t.Fatalf("expected 2, got %d", g.Len())
	}

	g.ForEach(func(e Entity) {
		if e == e2 {
			t.Fatal("e2 should have been removed")
		}
	})
}

func TestGroupClear(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})

	g := NewGroupOf(w, e1, e2)
	g.Clear()
	if g.Len() != 0 {
		t.Fatalf("expected 0 after Clear, got %d", g.Len())
	}
}

func TestGroupFilter(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 5, Y: 5})
	MustAdd[Position](w, e3, Position{X: 10, Y: 10})

	g := NewGroupOf(w, e1, e2, e3)
	filtered := g.Filter(func(e Entity) bool {
		p, ok := Get[Position](w, e)
		return ok && p.X > 3
	})

	if filtered.Len() != 2 {
		t.Fatalf("expected 2 filtered entities, got %d", filtered.Len())
	}

	filtered.ForEach(func(e Entity) {
		p, _ := Get[Position](w, e)
		if p.X <= 3 {
			t.Fatalf("entity with X=%v should have been filtered out", p.X)
		}
	})
}

func TestGroupFilterEmpty(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})

	g := NewGroupOf(w, e1)
	filtered := g.Filter(func(e Entity) bool { return false })
	if filtered.Len() != 0 {
		t.Fatal("expected 0 after filtering everything out")
	}
}

func TestGroupFilterPreservesOriginal(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 5, Y: 5})

	g := NewGroupOf(w, e1, e2)
	_ = g.Filter(func(e Entity) bool {
		p, _ := Get[Position](w, e)
		return p.X > 3
	})

	// Original group should be unchanged
	if g.Len() != 2 {
		t.Fatal("Filter should not modify original group")
	}
}

func TestGroupConvertsOnAdd(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Velocity](w, e2, Velocity{DX: 0.1, DY: 0.1})
	MustAdd[Position](w, e3, Position{X: 3, Y: 3})

	// Start as signature-based
	g := NewGroup(w, Position{}, Velocity{})
	if g.Len() != 1 { // only e2 has both
		t.Fatalf("expected 1 signature-matched entity, got %d", g.Len())
	}

	// Adding an entity converts to entity-based
	g.Add(e1)
	// Now it has the previously resolved entities + e1
	// After resolve() + Add, it becomes entity-based with [e2, e1]
	if g.Len() != 2 {
		t.Fatalf("expected 2 after Add, got %d", g.Len())
	}
}

// --- Query expansion ---

func TestQueryLen(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 5 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}
	w.Create() // no Position — should not be counted

	q := NewQuery[Position](w)
	if q.Len() != 5 {
		t.Fatalf("expected Len()=5, got %d", q.Len())
	}
}

func TestQueryForEachUntil(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 10 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}

	q := NewQuery[Position](w)
	sum := 0.0
	q.ForEachUntil(func(e Entity, p *Position) bool {
		if p.X >= 3 {
			return false
		}
		sum += p.X
		return true
	})

	if sum != 3 {
		t.Fatalf("expected sum=3 (0+1+2), got %v", sum)
	}
}

func TestQueryFilter(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 10 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}

	q := NewQuery[Position](w)
	filtered := q.Filter(func(e Entity, p *Position) bool {
		return p.X >= 5
	})

	if filtered.Len() != 5 {
		t.Fatalf("expected 5 filtered, got %d", filtered.Len())
	}

	// Original query unchanged
	if q.Len() != 10 {
		t.Fatal("original query should be unchanged")
	}
}

func TestQueryFilterForEach(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 10 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}

	filtered := NewQuery[Position](w).Filter(func(e Entity, p *Position) bool {
		return p.X >= 5
	})

	count := 0
	filtered.ForEach(func(e Entity, p *Position) {
		if p.X < 5 {
			t.Fatalf("entity with X=%v should have been filtered out", p.X)
		}
		count++
	})
	if count != 5 {
		t.Fatalf("expected 5 iterations, got %d", count)
	}
}

func TestQueryCollect(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e1 := w.Create()
	e2 := w.Create()
	e3 := w.Create()
	MustAdd[Position](w, e1, Position{X: 1, Y: 1})
	MustAdd[Position](w, e2, Position{X: 2, Y: 2})
	MustAdd[Position](w, e3, Position{X: 3, Y: 3})

	ents := NewQuery[Position](w).Collect()
	if len(ents) != 3 {
		t.Fatalf("expected 3 entities, got %d", len(ents))
	}
}

func TestQueryGroup(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 5 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}

	g := NewQuery[Position](w).Group()
	if g.Len() != 5 {
		t.Fatalf("expected group of 5, got %d", g.Len())
	}
}

func TestQueryGroupFromFilter(t *testing.T) {
	w := NewWorld()
	Register[Position](w)
	Register[Velocity](w)

	for i := range 10 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
		if i%2 == 0 {
			MustAdd[Velocity](w, e, Velocity{DX: 1, DY: 1})
		}
	}

	// Filter Positions to only those on even entities, then turn into Group
	filtered := NewQuery[Position](w).Filter(func(e Entity, p *Position) bool {
		return int(p.X)%2 == 0
	})
	g := filtered.Group()

	if g.Len() != 5 {
		t.Fatalf("expected 5 even-positioned entities, got %d", g.Len())
	}

	// Can now use SIMD on the filtered group
	g.AddNumber(func(c any) *float64 { return &c.(*Position).X }, 1000)

	// Verify only filtered entities were modified
	NewQuery[Position](w).ForEach(func(e Entity, p *Position) {
		if int(p.X)%2 == 0 && p.X < 1000 {
			t.Fatalf("even entity with X=%v should have been modified", p.X)
		}
	})
}

func TestQueryAny(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	e := w.Create()
	MustAdd[Position](w, e, Position{X: 42, Y: 99})

	got, pos, ok := NewQuery[Position](w).Any()
	if !ok {
		t.Fatal("expected Any to return true")
	}
	if got != e || pos.X != 42 || pos.Y != 99 {
		t.Fatalf("expected (%v, 42, 99), got (%v, %v, %v)", e, got, pos.X, pos.Y)
	}
}

func TestQueryAnyEmpty(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	_, _, ok := NewQuery[Position](w).Any()
	if ok {
		t.Fatal("expected Any to return false on empty")
	}
}

func TestQueryCountIf(t *testing.T) {
	w := NewWorld()
	Register[Position](w)

	for i := range 10 {
		e := w.Create()
		MustAdd[Position](w, e, Position{X: float64(i), Y: 0})
	}

	count := NewQuery[Position](w).CountIf(func(e Entity, p *Position) bool {
		return p.X >= 7
	})
	if count != 3 {
		t.Fatalf("expected 3, got %d", count)
	}
}
