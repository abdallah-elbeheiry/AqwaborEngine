package ecs

import (
	"reflect"
	"unsafe"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// ComponentID is a dense, stable identifier for a registered component type.
type ComponentID uint32

// Handle is a generational reference to a component instance in the pool.
// Users can store, compare, and pass handles around. The same handle can
// be attached to many entities.
type Handle struct {
	index      uint32
	generation uint32
}

const (
	handleIndexMask       uint64 = 0xFFFFFFFF
	handleGenerationShift        = 32
)

// newHandle packs an index and generation into a Handle.
func newHandle(index uint32, generation uint32) Handle {
	return Handle{index: index, generation: generation}
}

// componentInfo holds registration metadata for a component type.
type componentInfo struct {
	id   ComponentID
	typ  reflect.Type
	size uintptr
	wrap func(unsafe.Pointer) any // casts unsafe.Pointer → typed *T → any
}

// componentRegistry maps reflect.Type to stable ComponentIDs.
type componentRegistry struct {
	byType map[reflect.Type]*componentInfo
	byID   []*componentInfo
	nextID ComponentID
}

func newComponentRegistry(l *logx.Logger) *componentRegistry {
	l.Debug("component registry created")
	return &componentRegistry{
		byType: make(map[reflect.Type]*componentInfo),
		byID:   make([]*componentInfo, 0, 16),
	}
}

// register registers an owned component type. Idempotent.
func register[T any](r *componentRegistry, l *logx.Logger) ComponentID {
	var zero T
	typ := reflect.TypeOf(zero)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}

	if info, ok := r.byType[typ]; ok {
		l.Debug("component already registered", "type", typ, "id", info.id)
		return info.id
	}

	info := &componentInfo{
		id:   r.nextID,
		typ:  typ,
		size: typ.Size(),
		wrap: func(p unsafe.Pointer) any { return any((*T)(p)) },
	}

	r.byType[typ] = info
	r.byID = append(r.byID, info)
	r.nextID++

	l.Info("component registered", "type", typ, "id", info.id, "size", info.size)
	return info.id
}

func (r *componentRegistry) lookup(typ reflect.Type) (*componentInfo, bool) {
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	info, ok := r.byType[typ]
	return info, ok
}

func (r *componentRegistry) componentInfoFor(id ComponentID) *componentInfo {
	if int(id) >= len(r.byID) {
		return nil
	}
	return r.byID[id]
}

// Register registers a component type with the world.
func Register[T any](w *World) {
	register[T](w.registry, w.log)
}

// --- Component Pool ---

// poolInstance is a single component value in the pool.
type poolInstance struct {
	generation uint32
	refcount   int
	data       unsafe.Pointer
	typeID     ComponentID
}

// componentPool stores all component instances in a single unified pool.
type componentPool struct {
	instances []poolInstance
	freeList  []uint32
	byType    map[ComponentID][]Handle // componentID → handles (for iteration)
}

func newComponentPool(l *logx.Logger) *componentPool {
	l.Debug("component pool created")
	return &componentPool{
		instances: make([]poolInstance, 0, 256),
		freeList:  make([]uint32, 0, 64),
		byType:    make(map[ComponentID][]Handle),
	}
}

// create allocates a new component instance. Returns a Handle.
func (p *componentPool) create(info *componentInfo, data unsafe.Pointer, l *logx.Logger) Handle {
	var idx uint32
	var gen uint32

	if n := len(p.freeList); n > 0 {
		idx = p.freeList[n-1]
		p.freeList = p.freeList[:n-1]
		inst := &p.instances[idx]
		inst.generation++
		inst.refcount = 0
		inst.data = data
		inst.typeID = info.id
		gen = inst.generation
	} else {
		idx = uint32(len(p.instances))
		gen = 0
		p.instances = append(p.instances, poolInstance{
			generation: 0,
			refcount:   0,
			data:       data,
			typeID:     info.id,
		})
	}

	h := newHandle(idx, gen)
	p.byType[info.id] = append(p.byType[info.id], h)
	l.Debug("component instance created", "handle_index", idx, "handle_gen", gen, "component_id", info.id, "type", info.typ)
	return h
}

// acquire increments the reference count. Returns error if handle is stale.
func (p *componentPool) acquire(h Handle, l *logx.Logger) error {
	inst, ok := p.get(h)
	if !ok {
		return &ErrHandleInvalid{Handle: h}
	}
	inst.refcount++
	l.Debug("component acquired", "index", h.index, "refcount", inst.refcount)
	return nil
}

// release decrements the reference count. Returns true if refcount reached zero.
func (p *componentPool) release(h Handle, l *logx.Logger) (zeroed bool, err error) {
	inst, ok := p.get(h)
	if !ok {
		return false, &ErrHandleInvalid{Handle: h}
	}
	inst.refcount--
	l.Debug("component released", "index", h.index, "refcount", inst.refcount)
	return inst.refcount <= 0, nil
}

// destroy recycles a pool slot and increments the generation so stale handles are detected.
func (p *componentPool) destroy(h Handle, l *logx.Logger) {
	if int(h.index) >= len(p.instances) {
		return
	}
	p.instances[h.index].generation++
	p.freeList = append(p.freeList, h.index)
	l.Info("component instance destroyed", "index", h.index, "generation", h.generation)
}

// get returns the pool instance, or false if the handle is stale.
func (p *componentPool) get(h Handle) (*poolInstance, bool) {
	if int(h.index) >= len(p.instances) {
		return nil, false
	}
	inst := &p.instances[h.index]
	if inst.generation != h.generation {
		return nil, false
	}
	return inst, true
}

// getTyped returns a typed pointer to the component data.
func getTyped[T any](p *componentPool, h Handle) (*T, bool) {
	inst, ok := p.get(h)
	if !ok {
		return nil, false
	}
	return (*T)(inst.data), true
}

// handlesFor returns all live handles for a component type.
func typeOf[T any]() reflect.Type {
	var zero T
	t := reflect.TypeOf(zero)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}
