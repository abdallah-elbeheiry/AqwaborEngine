package ecs

import (
	"unsafe"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

// sparseSet provides O(1) add/remove for entities that own a component,
// without moving entities between archetype tables. Useful for components
// that are rare or change composition frequently.
type sparseSet struct {
	sparse      []int            // entity index -> dense position (or -1)
	dense       []Entity         // packed entities
	data        []unsafe.Pointer // parallel data array (component values)
	componentID ComponentID
	size        uintptr
	count       int
}

// newSparseSet creates a sparse set for a given component type.
func newSparseSet(info *componentInfo, log *logx.Logger) *sparseSet {
	ss := &sparseSet{
		sparse:      make([]int, 64),
		dense:       make([]Entity, 0, 64),
		data:        make([]unsafe.Pointer, 0, 64),
		componentID: info.id,
		size:        info.size,
	}
	for i := range ss.sparse {
		ss.sparse[i] = -1
	}
	log.Debug("sparse set created", "component_id", info.id, "type", info.typ)
	return ss
}

// ensureSparse grows the sparse array so that idx is valid.
func (s *sparseSet) ensureSparse(idx int) {
	for idx >= len(s.sparse) {
		old := s.sparse
		s.sparse = make([]int, len(old)*2)
		for i := range s.sparse {
			s.sparse[i] = -1
		}
		copy(s.sparse, old)
	}
}

// add inserts an entity into the sparse set. Returns a pointer to the
// component data slot.
func (s *sparseSet) add(e Entity, log *logx.Logger) unsafe.Pointer {
	idx := int(e.Index())
	s.ensureSparse(idx)

	if s.sparse[idx] != -1 {
		log.Warn("entity already in sparse set", "entity", e, "component_id", s.componentID)
		return s.data[s.sparse[idx]]
	}

	pos := s.count
	s.sparse[idx] = pos
	s.dense = append(s.dense, e)

	// Allocate component data
	data := unsafe.Pointer(&make([]byte, s.size)[0])
	s.data = append(s.data, data)
	s.count++

	log.Debug("entity added to sparse set", "entity", e, "component_id", s.componentID, "position", pos)
	return data
}

// remove removes an entity from the sparse set. Returns true if present.
func (s *sparseSet) remove(e Entity, log *logx.Logger) bool {
	idx := int(e.Index())
	if idx >= len(s.sparse) || s.sparse[idx] == -1 {
		return false
	}

	pos := s.sparse[idx]
	last := s.count - 1

	// Swap with last element (O(1) removal)
	if pos != last {
		s.dense[pos] = s.dense[last]
		s.data[pos] = s.data[last]
		s.sparse[s.dense[pos].Index()] = pos
	}

	s.dense = s.dense[:last]
	s.data = s.data[:last]
	s.sparse[idx] = -1
	s.count--

	log.Debug("entity removed from sparse set", "entity", e, "component_id", s.componentID)
	return true
}

// has reports whether the entity is in the sparse set.
func (s *sparseSet) has(e Entity) bool {
	idx := int(e.Index())
	if idx >= len(s.sparse) {
		return false
	}
	return s.sparse[idx] != -1
}

// get returns a pointer to the entity's component data, or nil if absent.
func (s *sparseSet) get(e Entity) unsafe.Pointer {
	idx := int(e.Index())
	if idx >= len(s.sparse) {
		return nil
	}
	pos := s.sparse[idx]
	if pos == -1 {
		return nil
	}
	return s.data[pos]
}

// forEach iterates all entities in the sparse set. The callback receives the
// entity and a pointer to its component data.
func (s *sparseSet) forEach(fn func(Entity, unsafe.Pointer)) {
	for i := 0; i < s.count; i++ {
		fn(s.dense[i], s.data[i])
	}
}

// entities returns a snapshot of all entities in the sparse set.
func (s *sparseSet) entities() []Entity {
	result := make([]Entity, s.count)
	copy(result, s.dense[:s.count])
	return result
}

// len returns the number of entities in the sparse set.
func (s *sparseSet) len() int {
	return s.count
}
