package ecs

// Iteration walks a component's dense array directly. There is no query object
// and no per-entity map lookup: Each over one component reads two slices
// forwards, which is the whole return on the storage layout.
//
// Each visits the awake rows only. All visits every row including the sleeping
// ones, and is what loading a save, validating and debugging want.

// Each calls fn for every awake entity holding this component, in storage
// order.
//
// Structural changes during iteration are not allowed: adding or removing this
// component moves rows, and a moved row is either visited twice or not at all.
// Buffer them through the world's commands and flush afterwards.
func (c Comp[T]) Each(fn func(e Entity, v *T)) {
	if c.s == nil {
		return
	}
	s := c.s
	dense := s.dense[:s.awake]
	owners := s.owners[:s.awake]
	for i := range dense {
		fn(owners[i], &dense[i])
	}
}

// EachUntil is Each, stopping when fn returns false.
func (c Comp[T]) EachUntil(fn func(e Entity, v *T) bool) {
	if c.s == nil {
		return
	}
	s := c.s
	dense := s.dense[:s.awake]
	owners := s.owners[:s.awake]
	for i := range dense {
		if !fn(owners[i], &dense[i]) {
			return
		}
	}
}

// All calls fn for every entity holding this component, awake or not.
func (c Comp[T]) All(fn func(e Entity, v *T)) {
	if c.s == nil {
		return
	}
	s := c.s
	for i := range s.dense {
		fn(s.owners[i], &s.dense[i])
	}
}

// Rows exposes the dense array of awake values for a pass that wants the slice
// itself: a vectorised operation, or a range handed to parallel workers.
// Owners gives the matching entities at the same indices.
//
// The slices are the storage, not a copy, so writing through them writes the
// component. They are invalidated by any structural change to this type.
func (c Comp[T]) Rows() []T {
	if c.s == nil {
		return nil
	}
	return c.s.dense[:c.s.awake]
}

// Owners returns the entities owning the rows Rows returns, at the same
// indices.
func (c Comp[T]) Owners() []Entity {
	if c.s == nil {
		return nil
	}
	return c.s.owners[:c.s.awake]
}

// AllRows is Rows including the sleeping tail.
func (c Comp[T]) AllRows() []T {
	if c.s == nil {
		return nil
	}
	return c.s.dense
}

// Each2 calls fn for every entity that is awake in a and also holds b.
//
// It walks a's dense array and looks b up per entity, so it costs one random
// access into b's storage for each row of a. Pass the smaller or more selective
// component as a. Where two values are read together on every pass, one
// component holding both fields is faster than two components joined here.
func Each2[A, B any](a Comp[A], b Comp[B], fn func(e Entity, av *A, bv *B)) {
	if a.s == nil || b.s == nil {
		return
	}
	sa := a.s
	dense := sa.dense[:sa.awake]
	owners := sa.owners[:sa.awake]
	for i := range dense {
		e := owners[i]
		if bv, ok := b.s.get(e); ok {
			fn(e, &dense[i], bv)
		}
	}
}

// Each3 is Each2 with a third component.
func Each3[A, B, C any](a Comp[A], b Comp[B], c Comp[C], fn func(e Entity, av *A, bv *B, cv *C)) {
	if a.s == nil || b.s == nil || c.s == nil {
		return
	}
	sa := a.s
	dense := sa.dense[:sa.awake]
	owners := sa.owners[:sa.awake]
	for i := range dense {
		e := owners[i]
		bv, ok := b.s.get(e)
		if !ok {
			continue
		}
		cv, ok := c.s.get(e)
		if !ok {
			continue
		}
		fn(e, &dense[i], bv, cv)
	}
}

// Collect returns the awake entities holding this component. It allocates, so
// it belongs in setup and diagnostics rather than in a tick.
func (c Comp[T]) Collect() []Entity {
	if c.s == nil {
		return nil
	}
	out := make([]Entity, c.s.awake)
	copy(out, c.s.owners[:c.s.awake])
	return out
}

// WakeAll puts every entity holding this component into the iterated set,
// which is what loading a save does before it knows what is idle.
func (c Comp[T]) WakeAll() {
	if c.s == nil {
		return
	}
	c.s.awake = len(c.s.dense)
}

// SleepAll empties the iterated set without discarding any value.
func (c Comp[T]) SleepAll() {
	if c.s == nil {
		return
	}
	c.s.awake = 0
}
