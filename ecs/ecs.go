// Package ecs is the entity component system: entities are generational
// handles, a component type is one dense array, and a system iterates the rows
// that are awake.
//
// The shape to hold in mind is that storage and scheduling are separate. A
// component store says where a value lives; it does not decide who runs. What
// runs is the awake partition of a store, which a system moves entities in and
// out of, so an entity with nothing to do is in no list anything walks.
//
//	w := ecs.NewWorld()
//	pos := ecs.MustRegister[Position](w)
//	e := w.Create()
//	pos.Set(e, Position{X: 1})
//	pos.Wake(e)
//	pos.Each(func(e ecs.Entity, p *Position) { p.X++ })
package ecs

import (
	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// WorldOption configures a World during construction.
type WorldOption func(*World)

// WithLogger injects a logger. Without it the world logs under component=ecs.
func WithLogger(l *logx.Logger) WorldOption {
	return func(w *World) { w.log = l }
}

// World owns the entities, the component stores and the deferred command
// buffer.
type World struct {
	log *logx.Logger

	entities *entityAllocator
	registry *componentRegistry
	stores   []store
	cmdBuf   *commandBuffer

	systems *systemManager
}

// NewWorld creates an empty world.
func NewWorld(opts ...WorldOption) *World {
	w := &World{log: logx.With("component", "ecs")}
	for _, o := range opts {
		o(w)
	}
	w.entities = newEntityAllocator()
	w.registry = newComponentRegistry()
	w.cmdBuf = newCommandBuffer()
	w.systems = newSystemManager()
	w.log.Info("world created")
	return w
}

// Create returns a new entity holding no components.
func (w *World) Create() Entity {
	e := w.entities.create()
	if w.log.Enabled(logx.TraceLevel) {
		w.log.Trace("entity created", "entity", e)
	}
	return e
}

// Destroy retires an entity and removes it from every component store. A stale
// handle destroys nothing.
func (w *World) Destroy(e Entity) bool {
	if !w.entities.alive(e) {
		return false
	}
	for _, s := range w.stores {
		s.removeEntity(e)
	}
	w.entities.destroy(e)
	if w.log.Enabled(logx.TraceLevel) {
		w.log.Trace("entity destroyed", "entity", e)
	}
	return true
}

// Alive reports whether the handle names a live entity. A handle whose
// generation has been superseded is not alive, even though its index is in use.
func (w *World) Alive(e Entity) bool { return w.entities.alive(e) }

// Count is the number of live entities.
func (w *World) Count() int { return w.entities.count() }

// ComponentCount is the number of registered component types.
func (w *World) ComponentCount() int { return len(w.stores) }

// Reset empties every store and retires every entity, keeping registrations.
// Loading a save wants this rather than a new world, because component handles
// stay valid across it.
func (w *World) Reset() {
	for _, s := range w.stores {
		s.reset()
	}
	w.entities = newEntityAllocator()
	w.cmdBuf.clear()
}

// --- Systems ---

// RegisterSystem records a system so GetSystem can find it later.
func (w *World) RegisterSystem(s System) error { return w.systems.register(s, w.log) }

// MustRegisterSystem is RegisterSystem for game code and tests.
func (w *World) MustRegisterSystem(s System) {
	if err := w.RegisterSystem(s); err != nil {
		panic(err)
	}
}

// GetSystem returns a registered system by its concrete type.
func GetSystem[T System](w *World) (T, bool) { return getSystem[T](w.systems) }

// --- Deferred changes ---

// Flush applies everything buffered by the command buffer. A system that
// creates or destroys entities while iterating buffers the change and the
// scheduler flushes at a stage boundary, so iteration never sees storage move
// underneath it.
func (w *World) Flush() { w.cmdBuf.apply(w) }

// Commands returns the buffer a system writes deferred changes into.
func (w *World) Commands() *commandBuffer { return w.cmdBuf }
