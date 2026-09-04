package ecs

import "github.com/abdallah-elbeheiry/AqwaborEngine/logx"

// Entity is a generational handle. The lower 32 bits hold the dense index,
// and the upper 32 bits hold the generation counter.
type Entity uint64

const (
	entityIndexMask       uint64 = 0xFFFFFFFF
	entityGenerationShift        = 32
)

func newEntity(index uint32, generation uint32) Entity {
	return Entity(uint64(index) | uint64(generation)<<entityGenerationShift)
}

// Index returns the dense index portion of the entity handle.
func (e Entity) Index() uint32 {
	return uint32(uint64(e) & entityIndexMask)
}

// Generation returns the generation counter portion of the entity handle.
func (e Entity) Generation() uint32 {
	return uint32(uint64(e) >> entityGenerationShift)
}

// entityMeta holds per-entity metadata in the dense allocator array.
type entityMeta struct {
	generation uint32
	alive      bool
	components map[ComponentID]Handle // component type → pool handle
	tableID    int                    // archetype table index, -1 if none
	row        int                    // row within the table, -1 if none
}

const noLocation = -1

// entityAllocator manages a dense array of entity metadata and a free-list.
type entityAllocator struct {
	metas    []entityMeta
	freeList []uint32
}

func newEntityAllocator(l *logx.Logger) *entityAllocator {
	l.Debug("entity allocator created")
	return &entityAllocator{
		metas:    make([]entityMeta, 0, 64),
		freeList: make([]uint32, 0, 64),
	}
}

func (a *entityAllocator) create(l *logx.Logger) Entity {
	var idx uint32
	if n := len(a.freeList); n > 0 {
		idx = a.freeList[n-1]
		a.freeList = a.freeList[:n-1]
		meta := &a.metas[idx]
		meta.generation++
		meta.alive = true
		meta.components = make(map[ComponentID]Handle)
		meta.tableID = noLocation
		meta.row = noLocation
	} else {
		idx = uint32(len(a.metas))
		a.metas = append(a.metas, entityMeta{
			generation: 0,
			alive:      true,
			components: make(map[ComponentID]Handle),
			tableID:    noLocation,
			row:        noLocation,
		})
	}
	e := newEntity(idx, a.metas[idx].generation)
	l.Debug("entity created", "entity", e, "index", idx, "generation", a.metas[idx].generation)
	return e
}

func (a *entityAllocator) destroy(e Entity, l *logx.Logger) {
	idx := e.Index()
	if int(idx) >= len(a.metas) {
		return
	}
	meta := &a.metas[idx]
	if !meta.alive {
		l.Warn("destroy called on already-dead entity", "entity", e)
		return
	}
	meta.alive = false
	meta.components = nil
	a.freeList = append(a.freeList, idx)
	l.Info("entity destroyed", "entity", e, "index", idx, "generation", meta.generation)
}

func (a *entityAllocator) alive(e Entity) bool {
	idx := e.Index()
	if int(idx) >= len(a.metas) {
		return false
	}
	return a.metas[idx].alive && a.metas[idx].generation == e.Generation()
}

func (a *entityAllocator) meta(e Entity) *entityMeta {
	idx := e.Index()
	if int(idx) >= len(a.metas) {
		return nil
	}
	return &a.metas[idx]
}

func (a *entityAllocator) count() int {
	n := 0
	for i := range a.metas {
		if a.metas[i].alive {
			n++
		}
	}
	return n
}
