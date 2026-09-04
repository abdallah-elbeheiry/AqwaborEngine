package ecs

import (
	"reflect"
	"slices"
	"unsafe"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// WorldOption configures a World during construction.
type WorldOption func(*World)

// WithLogger injects a logx logger. If omitted, a sensible default is used.
func WithLogger(l *logx.Logger) WorldOption {
	return func(w *World) {
		w.log = l
	}
}

// World is the single monolithic ECS world. It owns all entities, components,
// storage, and systems.
type World struct {
	log *logx.Logger

	entities   *entityAllocator
	registry   *componentRegistry
	pool       *componentPool
	tableGraph *tableGraph
	systems    *systemManager
	cmdBuf     *commandBuffer
}

// NewWorld creates a self-contained World.
func NewWorld(opts ...WorldOption) *World {
	w := &World{
		log: logx.With("component", "ecs"),
	}

	for _, o := range opts {
		o(w)
	}

	w.entities = newEntityAllocator(w.log)
	w.registry = newComponentRegistry(w.log)
	w.pool = newComponentPool(w.log)
	w.tableGraph = newTableGraph(w.log)
	w.systems = newSystemManager(w.log)
	w.cmdBuf = newCommandBuffer(w.log)

	w.log.Info("world created")
	return w
}

// --- Entity lifecycle ---

func (w *World) Create() Entity {
	return w.entities.create(w.log)
}

// Destroy removes the entity. When cascade is true, component instances
// whose refcount reaches zero are also destroyed from the pool.
func (w *World) Destroy(e Entity, cascade bool) {
	w.destroyEntity(e, cascade)
}

func (w *World) destroyEntity(e Entity, cascade bool) {
	if !w.entities.alive(e) {
		w.log.Warn("destroy called on dead entity", "entity", e)
		return
	}

	w.log.Info("destroying entity", "entity", e, "cascade", cascade)

	meta := w.entities.meta(e)

	// Release all component handles in deterministic order (sorted by ComponentID).
	cids := make([]ComponentID, 0, len(meta.components))
	for cid := range meta.components {
		cids = append(cids, cid)
	}
	slices.Sort(cids)
	for _, cid := range cids {
		h := meta.components[cid]
		zeroed, err := w.pool.release(h, w.log)
		if err != nil {
			w.log.Error("error releasing component", "entity", e, "component_id", cid, "err", err)
			continue
		}
		if zeroed && cascade {
			w.pool.destroy(h, w.log)
			w.log.Debug("component cascade destroyed", "entity", e, "component_id", cid)
		}
	}

	// Remove from table
	if meta.tableID != noLocation && meta.row != noLocation {
		t := w.tableGraph.get(tableID(meta.tableID))
		if t != nil {
			_, swapped := t.remove(meta.row, w.log)
			if swapped != e {
				swappedMeta := w.entities.meta(swapped)
				if swappedMeta != nil {
					swappedMeta.row = meta.row
				}
			}
		}
	}

	w.entities.destroy(e, w.log)
}

func (w *World) Alive(e Entity) bool {
	return w.entities.alive(e)
}

// --- Component creation (unified) ---

// Create creates a new component instance in the pool. Returns a Handle
// that can be attached to any number of entities.
func Create[T any](w *World, c T) (Handle, error) {
	info, ok := w.registry.lookup(typeOf[T]())
	if !ok {
		return Handle{}, &ErrComponentNotRegistered{Type: typeOf[T]()}
	}

	data := unsafe.Pointer(&make([]byte, info.size)[0])
	*(*T)(data) = c

	h := w.pool.create(info, data, w.log)
	return h, nil
}

// DestroyHandle explicitly destroys a component instance from the pool.
// Call this when you no longer need an instance and no entities reference it.
func DestroyHandle(w *World, h Handle) {
	w.pool.destroy(h, w.log)
}

// --- Entity ↔ component relationship ---

// Attach attaches a component handle to an entity. Multiple entities may
// hold the same Handle, sharing the underlying instance.
func Attach[T any](w *World, e Entity, h Handle) error {
	if !w.entities.alive(e) {
		return &ErrEntityDead{Entity: e}
	}

	info, ok := w.registry.lookup(typeOf[T]())
	if !ok {
		return &ErrComponentNotRegistered{Type: typeOf[T]()}
	}

	return w.attachHandle(e, info.id, h)
}

func (w *World) attachHandle(e Entity, componentID ComponentID, h Handle) error {
	meta := w.entities.meta(e)
	if meta == nil {
		w.log.Error("attachHandle on invalid entity", "entity", e)
		return &ErrEntityDead{Entity: e}
	}

	if _, ok := meta.components[componentID]; ok {
		w.log.Warn("entity already has component", "entity", e, "component_id", componentID)
		return nil
	}

	if err := w.pool.acquire(h, w.log); err != nil {
		w.log.Error("failed to acquire component", "entity", e, "err", err)
		return err
	}

	meta.components[componentID] = h

	// Transition table
	currentTable := noTable
	if meta.tableID != noLocation {
		currentTable = tableID(meta.tableID)
	}
	newTableID := w.tableGraph.transitionAdd(currentTable, componentID, w.log)
	w.moveEntityToTable(e, meta, currentTable, newTableID)

	w.log.Info("component attached", "entity", e, "component_id", componentID)
	return nil
}

// Detach removes a component from an entity. The component instance
// refcount is decremented.
func Detach[T any](w *World, e Entity) error {
	if !w.entities.alive(e) {
		return &ErrEntityDead{Entity: e}
	}

	info, ok := w.registry.lookup(typeOf[T]())
	if !ok {
		return &ErrComponentNotRegistered{Type: typeOf[T]()}
	}

	w.detachComponent(e, info.id)
	return nil
}

func (w *World) detachComponent(e Entity, componentID ComponentID) {
	meta := w.entities.meta(e)
	if meta == nil {
		return
	}

	h, ok := meta.components[componentID]
	if !ok {
		w.log.Warn("entity does not have component", "entity", e, "component_id", componentID)
		return
	}

	zeroed, err := w.pool.release(h, w.log)
	if err != nil {
		w.log.Error("failed to release component", "entity", e, "err", err)
		return
	}
	if zeroed {
		w.log.Debug("component refcount reached zero", "entity", e, "component_id", componentID)
	}

	delete(meta.components, componentID)

	// Transition table
	currentTable := noTable
	if meta.tableID != noLocation {
		currentTable = tableID(meta.tableID)
	}
	newTableID := w.tableGraph.transitionRemove(currentTable, componentID, w.log)
	w.moveEntityToTable(e, meta, currentTable, newTableID)

	w.log.Info("component detached", "entity", e, "component_id", componentID)
}

// --- Convenience: create + attach in one step ---

// Add creates a new component instance and attaches it to the entity.
// This is the common 1:1 case. For N:1 sharing, use Create + Attach.
func Add[T any](w *World, e Entity, c T) error {
	if !w.entities.alive(e) {
		return &ErrEntityDead{Entity: e}
	}

	info, ok := w.registry.lookup(typeOf[T]())
	if !ok {
		return &ErrComponentNotRegistered{Type: typeOf[T]()}
	}

	// Check if already has this component
	meta := w.entities.meta(e)
	if _, has := meta.components[info.id]; has {
		w.log.Warn("entity already has component", "entity", e, "component_id", info.id)
		return nil
	}

	data := unsafe.Pointer(&make([]byte, info.size)[0])
	*(*T)(data) = c

	h := w.pool.create(info, data, w.log)
	w.attachHandle(e, info.id, h)
	return nil
}

// Remove detaches a component from the entity.
// Convenience alias for Detach[T].
func Remove[T any](w *World, e Entity) error {
	return Detach[T](w, e)
}

// --- Get / Has ---

// Get returns a pointer to the component data for the given entity.
// The pointer is valid as long as the component is attached.
func Get[T any](w *World, e Entity) (*T, bool) {
	if !w.entities.alive(e) {
		return nil, false
	}

	info, ok := w.registry.lookup(typeOf[T]())
	if !ok {
		return nil, false
	}

	meta := w.entities.meta(e)
	if meta == nil {
		return nil, false
	}

	h, ok := meta.components[info.id]
	if !ok {
		return nil, false
	}

	return getTyped[T](w.pool, h)
}

// Has reports whether the entity has a component of the given type.
func Has[T any](w *World, e Entity) bool {
	if !w.entities.alive(e) {
		return false
	}

	info, ok := w.registry.lookup(typeOf[T]())
	if !ok {
		return false
	}

	meta := w.entities.meta(e)
	if meta == nil {
		return false
	}

	_, ok = meta.components[info.id]
	return ok
}

// HandleOf returns the Handle for a component on an entity, if present.
// Useful for recovering a handle from something originally added via Add.
func HandleOf[T any](w *World, e Entity) (Handle, bool) {
	if !w.entities.alive(e) {
		return Handle{}, false
	}

	info, ok := w.registry.lookup(typeOf[T]())
	if !ok {
		return Handle{}, false
	}

	meta := w.entities.meta(e)
	if meta == nil {
		return Handle{}, false
	}

	h, ok := meta.components[info.id]
	return h, ok
}

// --- Table management ---

func (w *World) moveEntityToTable(e Entity, meta *entityMeta, from, to tableID) {
	if from == to {
		return
	}

	// Remove from old table
	if from != noLocation && meta.row != noLocation {
		oldTable := w.tableGraph.get(from)
		if oldTable != nil {
			_, swapped := oldTable.remove(meta.row, w.log)
			if swapped != e {
				swappedMeta := w.entities.meta(swapped)
				if swappedMeta != nil {
					swappedMeta.row = meta.row
				}
			}
		}
	}

	// Add to new table
	newTable := w.tableGraph.get(to)
	if newTable != nil {
		newRow := newTable.add(e, w.log)
		meta.tableID = int(to)
		meta.row = newRow
	} else {
		meta.tableID = noLocation
		meta.row = noLocation
	}
}

// --- Queries & Groups ---

// NewQuery returns a Query that iterates entities owning component T.
func NewQuery[T any](w *World) *Query[T] {
	return newQuery[T](w)
}

// --- Systems ---

func (w *World) RegisterSystem(s System) error {
	return w.systems.register(s, w.log)
}

// MustRegisterSystem registers a system. Panics on error. For game code and tests.
func (w *World) MustRegisterSystem(s System) {
	if err := w.systems.register(s, w.log); err != nil {
		panic(err)
	}
}

func GetSystem[T System](w *World) (T, bool) {
	return getSystem[T](w.systems)
}

// --- Flush ---

// Flush applies all deferred structural changes.
func (w *World) Flush() {
	w.cmdBuf.apply(w)
}

// --- Internal helpers ---

func (w *World) componentPtr(e Entity, componentID ComponentID) unsafe.Pointer {
	meta := w.entities.meta(e)
	if meta == nil {
		return nil
	}
	h, ok := meta.components[componentID]
	if !ok {
		return nil
	}
	inst, ok := w.pool.get(h)
	if !ok {
		return nil
	}
	return inst.data
}

func (w *World) componentInfoForType(typ reflect.Type) (*componentInfo, bool) {
	return w.registry.lookup(typ)
}

// --- Command buffer internal helpers ---

// addComponent is used by the command buffer to create a component instance
// from raw data and attach it to an entity.
func (w *World) addComponent(e Entity, componentID ComponentID, data unsafe.Pointer, size uintptr) {
	if !w.entities.alive(e) {
		w.log.Warn("addComponent called on dead entity", "entity", e)
		return
	}

	info := w.registry.componentInfoFor(componentID)
	if info == nil {
		w.log.Error("addComponent for unregistered component", "component_id", componentID)
		return
	}

	meta := w.entities.meta(e)
	if _, has := meta.components[componentID]; has {
		w.log.Warn("addComponent: entity already has component", "entity", e, "component_id", componentID)
		return
	}

	h := w.pool.create(info, data, w.log)
	w.attachHandle(e, componentID, h)
}

// removeComponent is used by the command buffer to detach a component from an entity.
func (w *World) removeComponent(e Entity, componentID ComponentID) {
	w.detachComponent(e, componentID)
}

// --- Must* helpers ---

// MustAdd creates a new component instance and attaches it to the entity.
// Panics on error (unregistered type, dead entity). For game code and tests.
func MustAdd[T any](w *World, e Entity, c T) {
	if err := Add[T](w, e, c); err != nil {
		panic(err)
	}
}

// MustRemove detaches a component from the entity.
// Panics on error. For game code and tests.
func MustRemove[T any](w *World, e Entity) {
	if err := Remove[T](w, e); err != nil {
		panic(err)
	}
}

// MustCreate creates a new component instance in the pool.
// Panics on error (unregistered type). For game code and tests.
func MustCreate[T any](w *World, c T) Handle {
	h, err := Create[T](w, c)
	if err != nil {
		panic(err)
	}
	return h
}

// MustAttach attaches a component handle to an entity.
// Panics on error (dead entity, stale handle, unregistered type). For game code and tests.
func MustAttach[T any](w *World, e Entity, h Handle) {
	if err := Attach[T](w, e, h); err != nil {
		panic(err)
	}
}

// MustDetach removes a component from an entity.
// Panics on error (dead entity). For game code and tests.
func MustDetach[T any](w *World, e Entity) {
	if err := Detach[T](w, e); err != nil {
		panic(err)
	}
}
