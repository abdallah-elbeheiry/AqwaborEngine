package ecs

import (
	"math/rand"
	"testing"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

type Position struct{ X, Y float64 }
type Velocity struct{ X, Y float64 }
type Heat struct{ Kelvin int32 }

// Tag carries no fields. A zero-size component used to crash on registration
// because storage indexed an empty allocation.
type Tag struct{}

func newTestWorld(tb testing.TB) *World {
	tb.Helper()
	logx.Discard()
	return NewWorld()
}

func TestEntityLifetime(t *testing.T) {
	w := newTestWorld(t)

	e := w.Create()
	if !w.Alive(e) {
		t.Fatal("new entity is not alive")
	}
	if w.Count() != 1 {
		t.Fatalf("count = %d, want 1", w.Count())
	}
	if !w.Destroy(e) {
		t.Fatal("destroy reported nothing to do")
	}
	if w.Alive(e) {
		t.Fatal("destroyed entity is still alive")
	}
	if w.Count() != 0 {
		t.Fatalf("count = %d, want 0", w.Count())
	}
	if w.Destroy(e) {
		t.Fatal("destroying twice reported success the second time")
	}
}

// A recycled index must not let a handle to the previous occupant reach the new
// one. Every accessor is checked, because the old implementation checked the
// generation in Alive and nowhere else, so a stale handle could destroy a live
// entity.
func TestStaleHandleReachesNothing(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)

	first := w.Create()
	pos.Set(first, Position{X: 1})
	w.Destroy(first)

	second := w.Create()
	pos.Set(second, Position{X: 2})

	if first.Index() != second.Index() {
		t.Fatalf("test needs the index reused: first %d, second %d", first.Index(), second.Index())
	}
	if first == second {
		t.Fatal("recycled entity has the same handle, so the generation did not move")
	}

	if w.Alive(first) {
		t.Error("Alive accepted a stale handle")
	}
	if pos.Has(first) {
		t.Error("Has accepted a stale handle")
	}
	if _, ok := pos.Get(first); ok {
		t.Error("Get accepted a stale handle")
	}
	if pos.Set(first, Position{X: 99}) {
		t.Error("Set accepted a stale handle")
	}
	if pos.Remove(first) {
		t.Error("Remove accepted a stale handle")
	}
	if pos.Wake(first) {
		t.Error("Wake accepted a stale handle")
	}
	if w.Destroy(first) {
		t.Error("Destroy accepted a stale handle")
	}

	if !w.Alive(second) {
		t.Fatal("the live entity was collateral damage")
	}
	v, ok := pos.Get(second)
	if !ok || v.X != 2 {
		t.Fatalf("live entity value = %+v ok=%v, want X=2", v, ok)
	}
}

func TestZeroSizeComponent(t *testing.T) {
	w := newTestWorld(t)
	tag := MustRegister[Tag](w)

	e := w.Create()
	if !tag.Set(e, Tag{}) {
		t.Fatal("could not set a zero-size component")
	}
	if !tag.Has(e) {
		t.Fatal("zero-size component did not stick")
	}
	tag.Wake(e)

	seen := 0
	tag.Each(func(Entity, *Tag) { seen++ })
	if seen != 1 {
		t.Fatalf("iterated %d zero-size components, want 1", seen)
	}
	if !tag.Remove(e) {
		t.Fatal("could not remove a zero-size component")
	}
}

// Set replaces. The old Add warned and kept the original, so a caller asking to
// overwrite silently got the value it was replacing.
func TestSetOverwrites(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)

	e := w.Create()
	pos.Set(e, Position{X: 1})
	pos.Set(e, Position{X: 2})

	v, _ := pos.Get(e)
	if v.X != 2 {
		t.Fatalf("X = %v after overwrite, want 2", v.X)
	}
	if pos.Len() != 1 {
		t.Fatalf("overwrite added a second row: len = %d", pos.Len())
	}
}

func TestPointerComponentsRejected(t *testing.T) {
	w := newTestWorld(t)

	type WithSlice struct{ Parts []int }
	type WithString struct{ Name string }
	type WithPointer struct{ Next *Position }
	type WithMap struct{ M map[int]int }
	type Nested struct{ Inner WithSlice }
	type WithArrayOfBad struct{ Arr [4]WithString }

	if _, err := Register[WithSlice](w); err == nil {
		t.Error("a slice field registered")
	}
	if _, err := Register[WithString](w); err == nil {
		t.Error("a string field registered; a string header holds a pointer")
	}
	if _, err := Register[WithPointer](w); err == nil {
		t.Error("a pointer field registered")
	}
	if _, err := Register[WithMap](w); err == nil {
		t.Error("a map field registered")
	}
	if _, err := Register[Nested](w); err == nil {
		t.Error("a nested pointer-bearing struct registered")
	}
	if _, err := Register[WithArrayOfBad](w); err == nil {
		t.Error("an array of pointer-bearing structs registered")
	}

	if _, err := Register[Position](w); err != nil {
		t.Errorf("plain data was rejected: %v", err)
	}
	if _, err := Register[int32](w); err != nil {
		t.Errorf("a scalar was rejected: %v", err)
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	w := newTestWorld(t)
	a := MustRegister[Position](w)
	b := MustRegister[Position](w)

	if a.ID() != b.ID() {
		t.Fatalf("second registration made a new id: %d then %d", a.ID(), b.ID())
	}
	if w.ComponentCount() != 1 {
		t.Fatalf("stores = %d, want 1", w.ComponentCount())
	}

	e := w.Create()
	a.Set(e, Position{X: 7})
	v, ok := b.Get(e)
	if !ok || v.X != 7 {
		t.Fatal("the two handles do not share storage")
	}
}

func TestZeroCompIsInert(t *testing.T) {
	w := newTestWorld(t)
	e := w.Create()

	var pos Comp[Position]
	if pos.Valid() {
		t.Fatal("the zero handle claims to be valid")
	}
	if pos.Set(e, Position{}) || pos.Has(e) || pos.Remove(e) || pos.Wake(e) {
		t.Error("the zero handle did something")
	}
	if _, ok := pos.Get(e); ok {
		t.Error("the zero handle returned a value")
	}
	if pos.Len() != 0 || pos.AwakeLen() != 0 {
		t.Error("the zero handle reported rows")
	}
	pos.Each(func(Entity, *Position) { t.Error("the zero handle iterated") })
}

// The dense array, the owners array and the sparse index have to stay
// consistent through arbitrary churn, since every removal moves a row.
func TestStorageSurvivesChurn(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)

	rng := rand.New(rand.NewSource(1))
	live := map[Entity]float64{}

	for step := range 20000 {
		switch rng.Intn(3) {
		case 0:
			e := w.Create()
			val := float64(step)
			pos.Set(e, Position{X: val})
			live[e] = val
		case 1:
			for e := range live {
				pos.Remove(e)
				delete(live, e)
				break
			}
		case 2:
			for e := range live {
				w.Destroy(e)
				delete(live, e)
				break
			}
		}
	}

	if pos.Len() != len(live) {
		t.Fatalf("store holds %d rows, %d entities are live", pos.Len(), len(live))
	}
	for e, want := range live {
		v, ok := pos.Get(e)
		if !ok {
			t.Fatalf("entity %d lost its component", uint64(e))
		}
		if v.X != want {
			t.Fatalf("entity %d has X=%v, want %v", uint64(e), v.X, want)
		}
	}

	seen := map[Entity]bool{}
	pos.All(func(e Entity, v *Position) {
		if seen[e] {
			t.Fatalf("entity %d visited twice", uint64(e))
		}
		seen[e] = true
		if live[e] != v.X {
			t.Fatalf("iteration gave entity %d value %v, want %v", uint64(e), v.X, live[e])
		}
	})
	if len(seen) != len(live) {
		t.Fatalf("iterated %d rows, %d are live", len(seen), len(live))
	}
}

// Awake rows must stay at the front, so Each can walk a contiguous prefix.
func TestAwakePartitionHolds(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)

	ents := make([]Entity, 200)
	for i := range ents {
		ents[i] = w.Create()
		pos.Set(ents[i], Position{X: float64(i)})
	}
	if pos.AwakeLen() != 0 {
		t.Fatal("a new component started awake")
	}
	pos.Each(func(Entity, *Position) { t.Error("Each visited a sleeping row") })

	rng := rand.New(rand.NewSource(2))
	awake := map[Entity]bool{}
	for range 5000 {
		e := ents[rng.Intn(len(ents))]
		if rng.Intn(2) == 0 {
			pos.Wake(e)
			awake[e] = true
		} else {
			pos.Sleep(e)
			delete(awake, e)
		}

		if pos.AwakeLen() != len(awake) {
			t.Fatalf("awake count %d, want %d", pos.AwakeLen(), len(awake))
		}
	}

	seen := map[Entity]bool{}
	pos.Each(func(e Entity, v *Position) {
		if !awake[e] {
			t.Fatalf("Each visited sleeping entity %d", uint64(e))
		}
		seen[e] = true
	})
	if len(seen) != len(awake) {
		t.Fatalf("Each visited %d, %d are awake", len(seen), len(awake))
	}
}

// Removing an awake row must not leave a sleeping row inside the partition.
func TestRemoveFromAwakePartition(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)

	ents := make([]Entity, 50)
	for i := range ents {
		ents[i] = w.Create()
		pos.Set(ents[i], Position{X: float64(i)})
		if i%2 == 0 {
			pos.Wake(ents[i])
		}
	}
	want := 25
	for i := 0; i < len(ents); i += 4 {
		if pos.Awake(ents[i]) {
			want--
		}
		pos.Remove(ents[i])
	}
	if pos.AwakeLen() != want {
		t.Fatalf("awake count %d, want %d", pos.AwakeLen(), want)
	}
	pos.Each(func(e Entity, _ *Position) {
		if !pos.Awake(e) {
			t.Fatalf("entity %d is inside the partition but not awake", uint64(e))
		}
	})
}

func TestDestroyClearsEveryStore(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)
	vel := MustRegister[Velocity](w)
	heat := MustRegister[Heat](w)

	e := w.Create()
	pos.Set(e, Position{})
	vel.Set(e, Velocity{})
	heat.Set(e, Heat{Kelvin: 293})

	w.Destroy(e)

	if pos.Len() != 0 || vel.Len() != 0 || heat.Len() != 0 {
		t.Fatalf("rows survived the entity: %d %d %d", pos.Len(), vel.Len(), heat.Len())
	}
}

func TestEachN(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)
	vel := MustRegister[Velocity](w)
	heat := MustRegister[Heat](w)

	var both, all3 int
	for i := range 100 {
		e := w.Create()
		pos.Set(e, Position{X: 1})
		pos.Wake(e)
		if i%2 == 0 {
			vel.Set(e, Velocity{X: 2})
			both++
		}
		if i%4 == 0 {
			heat.Set(e, Heat{Kelvin: 300})
			all3++
		}
	}

	n := 0
	Each2(pos, vel, func(_ Entity, p *Position, v *Velocity) {
		p.X += v.X
		n++
	})
	if n != both {
		t.Fatalf("Each2 visited %d, want %d", n, both)
	}

	n = 0
	Each3(pos, vel, heat, func(_ Entity, _ *Position, _ *Velocity, h *Heat) {
		if h.Kelvin != 300 {
			t.Fatal("Each3 handed over the wrong Heat")
		}
		n++
	})
	if n != all3 {
		t.Fatalf("Each3 visited %d, want %d", n, all3)
	}

	pos.Each(func(e Entity, p *Position) {
		want := 1.0
		if vel.Has(e) {
			want = 3.0
		}
		if p.X != want {
			t.Fatalf("entity %d has X=%v, want %v", uint64(e), p.X, want)
		}
	})
}

func TestRowsAreTheStorage(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)

	ents := make([]Entity, 10)
	for i := range ents {
		ents[i] = w.Create()
		pos.Set(ents[i], Position{X: float64(i)})
		pos.Wake(ents[i])
	}

	rows := pos.Rows()
	if len(rows) != 10 {
		t.Fatalf("Rows returned %d, want 10", len(rows))
	}
	for i := range rows {
		rows[i].X *= 2
	}

	owners := pos.Owners()
	for i, e := range owners {
		v, _ := pos.Get(e)
		if v.X != rows[i].X {
			t.Fatal("Rows is a copy, not the storage")
		}
	}
}

func TestCommandBufferDefersChange(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)

	for i := range 10 {
		e := w.Create()
		pos.Set(e, Position{X: float64(i)})
		pos.Wake(e)
	}

	cmd := w.Commands()
	pos.Each(func(e Entity, v *Position) {
		if v.X < 5 {
			cmd.Destroy(e)
		}
	})
	cmd.Do(func(w *World) {
		e := w.Create()
		pos.Set(e, Position{X: 100})
		pos.Wake(e)
	})

	if w.Count() != 10 {
		t.Fatalf("the buffer changed the world before the flush: count %d", w.Count())
	}
	w.Flush()
	if w.Count() != 6 {
		t.Fatalf("count = %d after flush, want 6", w.Count())
	}
	if cmd.Len() != 0 {
		t.Fatal("the buffer kept its commands after applying them")
	}
}

func TestReset(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)

	for range 100 {
		e := w.Create()
		pos.Set(e, Position{})
		pos.Wake(e)
	}
	w.Reset()

	if w.Count() != 0 || pos.Len() != 0 || pos.AwakeLen() != 0 {
		t.Fatalf("reset left %d entities and %d rows", w.Count(), pos.Len())
	}
	e := w.Create()
	if !pos.Set(e, Position{X: 1}) {
		t.Fatal("the component handle stopped working across a reset")
	}
}

type testSystem struct {
	Base
	name string
}

func TestSystemRegistry(t *testing.T) {
	w := newTestWorld(t)
	s := &testSystem{name: "power"}

	if err := w.RegisterSystem(s); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := w.RegisterSystem(&testSystem{name: "other"}); err == nil {
		t.Fatal("registering the same type twice was accepted")
	}
	got, ok := GetSystem[*testSystem](w)
	if !ok || got.name != "power" {
		t.Fatalf("GetSystem returned %v, %v", got, ok)
	}
}

// Allocation guards. A tick may not allocate, so the operations a tick performs
// are held to zero here rather than measured after the fact.

func TestIterationDoesNotAllocate(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)
	vel := MustRegister[Velocity](w)
	for range 1000 {
		e := w.Create()
		pos.Set(e, Position{})
		vel.Set(e, Velocity{X: 1})
		pos.Wake(e)
	}

	if n := testing.AllocsPerRun(100, func() {
		pos.Each(func(_ Entity, p *Position) { p.X++ })
	}); n != 0 {
		t.Errorf("Each allocated %v times a run", n)
	}
	if n := testing.AllocsPerRun(100, func() {
		Each2(pos, vel, func(_ Entity, p *Position, v *Velocity) { p.X += v.X })
	}); n != 0 {
		t.Errorf("Each2 allocated %v times a run", n)
	}
}

func TestWakeSleepDoNotAllocate(t *testing.T) {
	w := newTestWorld(t)
	pos := MustRegister[Position](w)
	ents := make([]Entity, 100)
	for i := range ents {
		ents[i] = w.Create()
		pos.Set(ents[i], Position{})
	}

	if n := testing.AllocsPerRun(100, func() {
		for _, e := range ents {
			pos.Wake(e)
		}
		for _, e := range ents {
			pos.Sleep(e)
		}
	}); n != 0 {
		t.Errorf("wake and sleep allocated %v times a run", n)
	}
}
