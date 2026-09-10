package ecs

import (
	"math/rand"
	"testing"
)

func TestSetMembership(t *testing.T) {
	w := newTestWorld(t)
	set := w.NewSet()

	a, b := w.Create(), w.Create()
	if !set.Add(a) {
		t.Fatal("adding an entity reported it was already there")
	}
	if set.Add(a) {
		t.Fatal("adding twice reported a second addition")
	}
	set.Add(b)

	if set.Len() != 2 || !set.Has(a) || !set.Has(b) {
		t.Fatalf("len = %d, has a = %v, has b = %v", set.Len(), set.Has(a), set.Has(b))
	}
	if !set.Remove(a) || set.Remove(a) {
		t.Fatal("remove did not report membership correctly")
	}
	if set.Has(a) || set.Len() != 1 {
		t.Fatalf("a is still in the set, or len = %d", set.Len())
	}
}

func TestSetRefusesDeadEntities(t *testing.T) {
	w := newTestWorld(t)
	set := w.NewSet()

	e := w.Create()
	w.Destroy(e)
	if set.Add(e) {
		t.Fatal("a destroyed entity was added")
	}

	live := w.Create()
	set.Add(live)
	if live.Index() == e.Index() && set.Has(e) {
		t.Fatal("a stale handle reads as a member because its index was reused")
	}
}

// Destroying an entity must take it out of every set, or a set would hand back
// a handle that resolves to whatever reused the index.
func TestDestroyLeavesEverySet(t *testing.T) {
	w := newTestWorld(t)
	one, two := w.NewSet(), w.NewSet()

	e := w.Create()
	one.Add(e)
	two.Add(e)
	w.Destroy(e)

	if one.Len() != 0 || two.Len() != 0 {
		t.Fatalf("the entity survived in the sets: %d and %d", one.Len(), two.Len())
	}
}

func TestSetSurvivesChurn(t *testing.T) {
	w := newTestWorld(t)
	set := w.NewSet()

	rng := rand.New(rand.NewSource(11))
	ents := make([]Entity, 300)
	for i := range ents {
		ents[i] = w.Create()
	}
	want := map[Entity]bool{}

	for range 30000 {
		e := ents[rng.Intn(len(ents))]
		if rng.Intn(2) == 0 {
			set.Add(e)
			want[e] = true
		} else {
			set.Remove(e)
			delete(want, e)
		}
		if set.Len() != len(want) {
			t.Fatalf("len %d, tracked %d", set.Len(), len(want))
		}
	}

	seen := map[Entity]bool{}
	set.Each(func(e Entity) {
		if seen[e] {
			t.Fatalf("entity %d visited twice", uint64(e))
		}
		if !want[e] {
			t.Fatalf("entity %d is in the set and should not be", uint64(e))
		}
		seen[e] = true
	})
	if len(seen) != len(want) {
		t.Fatalf("iterated %d, tracked %d", len(seen), len(want))
	}
}

func TestEachInJoinsComponents(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)
	vel := MustRegister[Velocity](w)
	set := w.NewSet()

	var withPos, withBoth int
	for i := range 60 {
		e := w.Create()
		set.Add(e)
		if i%2 == 0 {
			pos.Set(e, Position{X: 1})
			withPos++
		}
		if i%3 == 0 {
			vel.Set(e, Velocity{X: 2})
		}
		if i%6 == 0 {
			withBoth++
		}
	}

	n := 0
	EachIn(set, pos, func(_ Entity, p *Position) {
		p.X += 1
		n++
	})
	if n != withPos {
		t.Fatalf("EachIn visited %d, want %d", n, withPos)
	}

	n = 0
	EachIn2(set, pos, vel, func(_ Entity, p *Position, v *Velocity) {
		p.X += v.X
		n++
	})
	if n != withBoth {
		t.Fatalf("EachIn2 visited %d, want %d", n, withBoth)
	}
}

// A set holds entities that are not awake in any component, which is the point
// of it: membership belongs to the system, not to a component type.
func TestSetIsIndependentOfAwakePartitions(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)
	set := w.NewSet()

	e := w.Create()
	pos.Set(e, Position{})
	set.Add(e)

	if pos.Awake(e) {
		t.Fatal("a new component started awake")
	}
	n := 0
	EachIn(set, pos, func(Entity, *Position) { n++ })
	if n != 1 {
		t.Fatalf("EachIn visited %d, want 1: a set does not care about the awake partition", n)
	}
	pos.Each(func(Entity, *Position) { t.Error("Each visited a sleeping row") })
}

func TestSetOperationsDoNotAllocate(t *testing.T) {
	w := newTestWorld(t)
	set := w.NewSet()
	ents := make([]Entity, 500)
	for i := range ents {
		ents[i] = w.Create()
		set.Add(ents[i])
	}
	set.Clear()

	if n := testing.AllocsPerRun(100, func() {
		for _, e := range ents {
			set.Add(e)
		}
		for _, e := range ents {
			set.Remove(e)
		}
	}); n != 0 {
		t.Errorf("add and remove allocated %v times a run", n)
	}
}
