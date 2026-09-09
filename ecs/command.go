package ecs

// A commandBuffer defers structural change. A system iterating a dense array
// cannot create or destroy entities as it goes, because both move rows; it
// records the intent here and the change lands at a stage boundary, where
// nothing is mid-iteration.
//
// Only entity lifetime is buffered. Component values are deferred by the caller
// capturing them in the closure, which keeps the buffer free of the type
// erasure that made the previous version carry raw pointers.
type commandBuffer struct {
	destroy []Entity
	apply_  []func(*World)
}

func newCommandBuffer() *commandBuffer {
	return &commandBuffer{
		destroy: make([]Entity, 0, 64),
		apply_:  make([]func(*World), 0, 64),
	}
}

// Destroy retires e at the next flush.
func (b *commandBuffer) Destroy(e Entity) { b.destroy = append(b.destroy, e) }

// Do runs fn against the world at the next flush. A system spawning an entity
// with components writes that here, so the spawn happens where no iteration is
// in progress.
func (b *commandBuffer) Do(fn func(*World)) { b.apply_ = append(b.apply_, fn) }

// Len is how many changes are pending.
func (b *commandBuffer) Len() int { return len(b.destroy) + len(b.apply_) }

func (b *commandBuffer) clear() {
	b.destroy = b.destroy[:0]
	b.apply_ = b.apply_[:0]
}

// apply runs the buffered work in the order it was recorded, deferred
// functions first so an entity created and destroyed in one tick is not
// destroyed before it exists.
func (b *commandBuffer) apply(w *World) {
	if b.Len() == 0 {
		return
	}
	for _, fn := range b.apply_ {
		fn(w)
	}
	for _, e := range b.destroy {
		w.Destroy(e)
	}
	b.clear()
}
