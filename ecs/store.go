package ecs

// A store holds every value of one component type in a dense array, with a
// sparse index from entity to the row holding that entity's value. Adding and
// removing are O(1) and move at most one row, so an entity changing its
// component set costs a swap rather than a copy of everything it owns.
//
// Iteration walks the dense array, so a pass over a component type reads
// forwards through memory. That is the whole reason the storage is shaped this
// way: the previous layout allocated each value separately and reached it
// through a pointer, which cost 21x a plain slice over 40,000 entities.
//
// The dense array is partitioned. Rows before awake are the entities a system
// iterates by default; rows from awake onward exist but are not visited. Waking
// and sleeping swap one row across that boundary, which keeps both O(1) and
// keeps the awake rows contiguous.

// store is the untyped half of a component store, which is all the world needs
// to destroy an entity without knowing any component type.
type store interface {
	componentID() ComponentID
	removeEntity(e Entity) bool
	has(e Entity) bool
	len() int
	awakeLen() int
	wake(e Entity) bool
	sleep(e Entity) bool
	reset()
}

const noRow int32 = -1

type typedStore[T any] struct {
	cid ComponentID
	w   *World

	// dense holds the values, owners[i] holds the entity dense[i] belongs to.
	// The two are always the same length and are swapped together.
	dense  []T
	owners []Entity

	// sparse maps an entity index to its row, or noRow. It is indexed by the
	// entity's index alone; the generation is checked against owners, which is
	// what makes a stale handle detectable rather than merely unlikely.
	sparse []int32

	// awake is the count of rows at the front that are awake. Rows [0, awake)
	// are visited by Each; rows [awake, len) are not.
	awake int
}

func newTypedStore[T any](w *World, cid ComponentID) *typedStore[T] {
	return &typedStore[T]{
		cid:    cid,
		w:      w,
		dense:  make([]T, 0, 64),
		owners: make([]Entity, 0, 64),
		sparse: make([]int32, 0, 64),
	}
}

func (s *typedStore[T]) componentID() ComponentID { return s.cid }
func (s *typedStore[T]) len() int                 { return len(s.dense) }
func (s *typedStore[T]) awakeLen() int            { return s.awake }

// row returns the dense row holding e's value, or noRow. It rejects a handle
// whose generation no longer matches the entity occupying that index, so a
// stale handle from a destroyed entity cannot read or write its successor.
func (s *typedStore[T]) row(e Entity) int32 {
	idx := int(e.Index())
	if idx >= len(s.sparse) {
		return noRow
	}
	r := s.sparse[idx]
	if r == noRow {
		return noRow
	}
	if s.owners[r] != e {
		return noRow
	}
	return r
}

func (s *typedStore[T]) has(e Entity) bool { return s.row(e) != noRow }

func (s *typedStore[T]) growSparse(idx int) {
	for len(s.sparse) <= idx {
		s.sparse = append(s.sparse, noRow)
	}
}

// set writes e's value, adding the component when the entity does not have it
// and overwriting when it does. Overwriting is deliberate: the previous
// implementation warned and kept the old value, so a caller asking to replace
// silently got the original.
//
// A new row is added asleep, at the back, because an entity that has just
// gained a component has not yet been given a reason to be running.
func (s *typedStore[T]) set(e Entity, v T) {
	if r := s.row(e); r != noRow {
		s.dense[r] = v
		return
	}
	idx := int(e.Index())
	s.growSparse(idx)
	s.dense = append(s.dense, v)
	s.owners = append(s.owners, e)
	s.sparse[idx] = int32(len(s.dense) - 1)
}

func (s *typedStore[T]) get(e Entity) (*T, bool) {
	r := s.row(e)
	if r == noRow {
		return nil, false
	}
	return &s.dense[r], true
}

// swap exchanges two rows and repairs the sparse entries pointing at them.
func (s *typedStore[T]) swap(a, b int32) {
	if a == b {
		return
	}
	s.dense[a], s.dense[b] = s.dense[b], s.dense[a]
	s.owners[a], s.owners[b] = s.owners[b], s.owners[a]
	s.sparse[s.owners[a].Index()] = a
	s.sparse[s.owners[b].Index()] = b
}

// remove takes e's value out, swapping the last row into the hole. A row inside
// the awake partition is first swapped to the partition boundary, so removal
// cannot leave an asleep row in front of awake ones.
func (s *typedStore[T]) remove(e Entity) bool {
	r := s.row(e)
	if r == noRow {
		return false
	}
	if int(r) < s.awake {
		s.swap(r, int32(s.awake-1))
		r = int32(s.awake - 1)
		s.awake--
	}
	last := int32(len(s.dense) - 1)
	s.swap(r, last)

	s.sparse[s.owners[last].Index()] = noRow
	var zero T
	s.dense[last] = zero
	s.dense = s.dense[:last]
	s.owners = s.owners[:last]
	return true
}

func (s *typedStore[T]) removeEntity(e Entity) bool { return s.remove(e) }

// wake moves e's row into the awake partition. Both wake and sleep are one swap
// and one integer, which is what lets a system iterate only what is doing
// something without paying to find out what that is.
func (s *typedStore[T]) wake(e Entity) bool {
	r := s.row(e)
	if r == noRow || int(r) < s.awake {
		return false
	}
	s.swap(r, int32(s.awake))
	s.awake++
	return true
}

func (s *typedStore[T]) sleep(e Entity) bool {
	r := s.row(e)
	if r == noRow || int(r) >= s.awake {
		return false
	}
	s.swap(r, int32(s.awake-1))
	s.awake--
	return true
}

func (s *typedStore[T]) isAwake(e Entity) bool {
	r := s.row(e)
	return r != noRow && int(r) < s.awake
}

func (s *typedStore[T]) reset() {
	var zero T
	for i := range s.dense {
		s.dense[i] = zero
	}
	s.dense = s.dense[:0]
	s.owners = s.owners[:0]
	for i := range s.sparse {
		s.sparse[i] = noRow
	}
	s.awake = 0
}
