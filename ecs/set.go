package ecs

// A Set is a system's own list of entities to visit.
//
// Most systems do not need one. A system whose work is "the entities of this
// component that are doing something" drives off that component's awake
// partition, which is already dense and contiguous, and Each2 joins a second
// component onto it. Reach for a Set when the membership is the system's own
// rather than any one component's: a queue of entities waiting on a crane, the
// grids a pressure pass still has to settle, the machines a player has selected.
//
// Membership is a dense array of entities plus a sparse index, so adding,
// removing and testing are each O(1) and iteration walks one slice. A Set is
// registered with its world, so destroying an entity takes it out of every set
// rather than leaving a handle that resolves to whatever reuses the index.
type Set struct {
	w      *World
	dense  []Entity
	sparse []int32
}

// NewSet returns an empty set belonging to this world.
func (w *World) NewSet() *Set {
	s := &Set{
		w:      w,
		dense:  make([]Entity, 0, 64),
		sparse: make([]int32, 0, 64),
	}
	w.sets = append(w.sets, s)
	return s
}

func (s *Set) growSparse(idx int) {
	for len(s.sparse) <= idx {
		s.sparse = append(s.sparse, noRow)
	}
}

func (s *Set) row(e Entity) int32 {
	idx := int(e.Index())
	if idx >= len(s.sparse) {
		return noRow
	}
	r := s.sparse[idx]
	if r == noRow || s.dense[r] != e {
		return noRow
	}
	return r
}

// Add puts e in the set, reporting whether it was not already there. A dead
// entity is refused rather than added and silently skipped later.
func (s *Set) Add(e Entity) bool {
	if !s.w.Alive(e) || s.row(e) != noRow {
		return false
	}
	idx := int(e.Index())
	s.growSparse(idx)
	s.dense = append(s.dense, e)
	s.sparse[idx] = int32(len(s.dense) - 1)
	return true
}

// Remove takes e out, reporting whether it was there. The last entry moves into
// the hole, so iteration order is not membership order.
func (s *Set) Remove(e Entity) bool {
	r := s.row(e)
	if r == noRow {
		return false
	}
	last := int32(len(s.dense) - 1)
	if r != last {
		moved := s.dense[last]
		s.dense[r] = moved
		s.sparse[moved.Index()] = r
	}
	s.sparse[e.Index()] = noRow
	s.dense = s.dense[:last]
	return true
}

// Has reports membership.
func (s *Set) Has(e Entity) bool { return s.row(e) != noRow }

// Len is how many entities are in the set.
func (s *Set) Len() int { return len(s.dense) }

// Clear empties the set without touching the entities.
func (s *Set) Clear() {
	for _, e := range s.dense {
		s.sparse[e.Index()] = noRow
	}
	s.dense = s.dense[:0]
}

// Each calls fn for every entity in the set.
//
// Adding to or removing from the set during iteration moves entries, so a
// system that changes membership as it goes should collect first or defer
// through the world's commands.
func (s *Set) Each(fn func(e Entity)) {
	for _, e := range s.dense {
		fn(e)
	}
}

// Entities exposes the dense array. It is the set's own storage, not a copy, so
// it is invalidated by any change to membership.
func (s *Set) Entities() []Entity { return s.dense }

// EachIn calls fn for every entity in the set that holds c.
//
// It costs one lookup into c's storage per entity, the same join Each2 pays,
// which is the price of a membership that is not a component's own.
func EachIn[T any](s *Set, c Comp[T], fn func(e Entity, v *T)) {
	if s == nil || c.s == nil {
		return
	}
	for _, e := range s.dense {
		if v, ok := c.s.get(e); ok {
			fn(e, v)
		}
	}
}

// EachIn2 is EachIn with a second component.
func EachIn2[A, B any](s *Set, a Comp[A], b Comp[B], fn func(e Entity, av *A, bv *B)) {
	if s == nil || a.s == nil || b.s == nil {
		return
	}
	for _, e := range s.dense {
		av, ok := a.s.get(e)
		if !ok {
			continue
		}
		bv, ok := b.s.get(e)
		if !ok {
			continue
		}
		fn(e, av, bv)
	}
}
