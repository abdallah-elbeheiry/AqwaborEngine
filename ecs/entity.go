package ecs

// Entity is a generational handle. The low 32 bits are a dense index, the high
// 32 bits a generation counter that is raised each time the index is reused.
//
// Absence is not the zero value. Entity(0) is a valid entity, so a "no entity"
// is NoEntity, which is every bit set.
type Entity uint64

const (
	entityIndexMask       uint64 = 0xFFFFFFFF
	entityGenerationShift        = 32
)

// NoEntity is the absent entity. It never compares equal to a live one because
// its index can never be allocated.
const NoEntity = Entity(^uint64(0))

func newEntity(index uint32, generation uint32) Entity {
	return Entity(uint64(index) | uint64(generation)<<entityGenerationShift)
}

// Index returns the dense index portion.
func (e Entity) Index() uint32 { return uint32(uint64(e) & entityIndexMask) }

// Generation returns the generation portion.
func (e Entity) Generation() uint32 { return uint32(uint64(e) >> entityGenerationShift) }

// entityMeta is the per-entity record. It holds no component map: which
// components an entity has is answered by each store's sparse index, so an
// entity costs one of these and nothing else.
type entityMeta struct {
	generation uint32
	alive      bool
}

// entityAllocator hands out indices and recycles them through a free list.
type entityAllocator struct {
	metas    []entityMeta
	freeList []uint32
	live     int
}

func newEntityAllocator() *entityAllocator {
	return &entityAllocator{
		metas:    make([]entityMeta, 0, 256),
		freeList: make([]uint32, 0, 64),
	}
}

func (a *entityAllocator) create() Entity {
	var idx uint32
	if n := len(a.freeList); n > 0 {
		idx = a.freeList[n-1]
		a.freeList = a.freeList[:n-1]
		meta := &a.metas[idx]
		meta.generation++
		meta.alive = true
	} else {
		idx = uint32(len(a.metas))
		a.metas = append(a.metas, entityMeta{generation: 0, alive: true})
	}
	a.live++
	return newEntity(idx, a.metas[idx].generation)
}

// destroy retires an entity. It compares the generation, so a stale handle to a
// destroyed entity cannot retire whichever entity now occupies that index.
func (a *entityAllocator) destroy(e Entity) bool {
	idx := int(e.Index())
	if idx >= len(a.metas) {
		return false
	}
	meta := &a.metas[idx]
	if !meta.alive || meta.generation != e.Generation() {
		return false
	}
	meta.alive = false
	a.freeList = append(a.freeList, uint32(idx))
	a.live--
	return true
}

func (a *entityAllocator) alive(e Entity) bool {
	idx := int(e.Index())
	if idx >= len(a.metas) {
		return false
	}
	return a.metas[idx].alive && a.metas[idx].generation == e.Generation()
}

// count is the number of live entities, kept as a running total rather than
// recomputed, because the old implementation scanned every slot to answer it.
func (a *entityAllocator) count() int { return a.live }
