package ecs

import (
	"reflect"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// System is a behaviour container registered on the World.
// Systems are plain structs that implement the System interface.
type System interface {
	Update(dt float64)
}

// systemEntry wraps a System with its type information for retrieval.
type systemEntry struct {
	system System
	typ    reflect.Type
}

// systemManager handles registration and retrieval of systems.
type systemManager struct {
	systems []systemEntry
	byType  map[reflect.Type]int // type -> index in systems slice
}

// newSystemManager creates an empty system manager.
func newSystemManager(log *logx.Logger) *systemManager {
	log.Debug("system manager created")
	return &systemManager{
		systems: make([]systemEntry, 0, 8),
		byType:  make(map[reflect.Type]int),
	}
}

// register adds a system to the manager. Returns an error if a system of the
// same concrete type is already registered.
func (m *systemManager) register(s System, log *logx.Logger) error {
	typ := reflect.TypeOf(s)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if _, ok := m.byType[typ]; ok {
		log.Warn("system already registered", "type", typ)
		return nil // idempotent per guide
	}
	idx := len(m.systems)
	m.systems = append(m.systems, systemEntry{system: s, typ: typ})
	m.byType[typ] = idx
	log.Info("system registered", "type", typ, "total", len(m.systems))
	return nil
}

// get retrieves a system by its concrete type.
func getSystem[T System](m *systemManager) (T, bool) {
	var zero T
	typ := reflect.TypeOf(zero)
	if typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	idx, ok := m.byType[typ]
	if !ok {
		return zero, false
	}
	s, ok := m.systems[idx].system.(T)
	return s, ok
}

// updateAll calls Update(dt) on every registered system.
func (m *systemManager) updateAll(dt float64, log *logx.Logger) {
	for _, entry := range m.systems {
		entry.system.Update(dt)
	}
	log.Trace("systems updated", "count", len(m.systems), "dt", dt)
}

// count returns the number of registered systems.
func (m *systemManager) count() int {
	return len(m.systems)
}
