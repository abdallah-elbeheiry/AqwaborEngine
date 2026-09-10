package ecs

import (
	"reflect"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// System is a behaviour container the world can hand back by type. It declares
// no Update method: the previous interface required one and nothing ever called
// it, which obliged every system to implement a method the engine never
// invoked. Ticking is the scheduler's job, and a system is registered here so
// other code can find it.
type System interface{ isSystem() }

// Base is embedded in a system to satisfy System without writing a method.
//
//	type Power struct { ecs.Base; ... }
type Base struct{}

func (Base) isSystem() {}

type systemEntry struct {
	system System
	typ    reflect.Type
}

type systemManager struct {
	systems []systemEntry
	byType  map[reflect.Type]int
}

func newSystemManager() *systemManager {
	return &systemManager{
		systems: make([]systemEntry, 0, 8),
		byType:  make(map[reflect.Type]int),
	}
}

func (m *systemManager) register(s System, log *logx.Logger) error {
	typ := reflect.TypeOf(s)
	if typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == nil {
		return &ErrSystemRegistered{Type: "<nil>"}
	}
	if _, ok := m.byType[typ]; ok {
		return &ErrSystemRegistered{Type: typ.String()}
	}
	m.byType[typ] = len(m.systems)
	m.systems = append(m.systems, systemEntry{system: s, typ: typ})
	log.Info("system registered", "type", typ, "total", len(m.systems))
	return nil
}

func getSystem[T System](m *systemManager) (T, bool) {
	var zero T
	typ := reflect.TypeFor[T]()
	if typ != nil && typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	idx, ok := m.byType[typ]
	if !ok {
		return zero, false
	}
	s, ok := m.systems[idx].system.(T)
	return s, ok
}

func (m *systemManager) count() int { return len(m.systems) }
