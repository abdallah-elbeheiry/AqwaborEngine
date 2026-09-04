package ecs

import (
	"reflect"
)

// Query iterates entities that own a specific component type.
type Query[T any] struct {
	w           *World
	componentID ComponentID
	info        *componentInfo
	members     []Entity // non-nil = pre-filtered set (from Filter)
}

func newQuery[T any](w *World) *Query[T] {
	info, ok := w.registry.lookup(typeOf[T]())
	if !ok {
		w.log.Error("query created for unregistered component", "type", typeOf[T]().String())
		return &Query[T]{w: w}
	}
	w.log.Debug("query created", "component_id", info.id, "type", typeOf[T]().String())
	return &Query[T]{
		w:           w,
		componentID: info.id,
		info:        info,
	}
}

// ForEach iterates all alive entities that have the queried component,
// calling fn with a pointer to the component data.
// If the query was filtered, only iterates the pre-filtered set.
func (q *Query[T]) ForEach(fn func(e Entity, c *T)) {
	if q.info == nil {
		return
	}
	if q.members != nil {
		for _, e := range q.members {
			h, ok := q.w.entities.meta(e).components[q.componentID]
			if !ok {
				continue
			}
			val, ok := getTyped[T](q.w.pool, h)
			if !ok {
				continue
			}
			fn(e, val)
		}
		return
	}
	for i := range q.w.entities.metas {
		meta := &q.w.entities.metas[i]
		if !meta.alive {
			continue
		}
		h, ok := meta.components[q.componentID]
		if !ok {
			continue
		}
		val, ok := getTyped[T](q.w.pool, h)
		if !ok {
			q.w.log.Error("stale component handle in query", "entity_index", i)
			continue
		}
		fn(newEntity(uint32(i), meta.generation), val)
	}
}

// ForEachUntil iterates entities until fn returns false.
func (q *Query[T]) ForEachUntil(fn func(e Entity, c *T) bool) {
	if q.info == nil {
		return
	}
	if q.members != nil {
		for _, e := range q.members {
			h, ok := q.w.entities.meta(e).components[q.componentID]
			if !ok {
				continue
			}
			val, ok := getTyped[T](q.w.pool, h)
			if !ok {
				continue
			}
			if !fn(e, val) {
				return
			}
		}
		return
	}
	for i := range q.w.entities.metas {
		meta := &q.w.entities.metas[i]
		if !meta.alive {
			continue
		}
		h, ok := meta.components[q.componentID]
		if !ok {
			continue
		}
		val, ok := getTyped[T](q.w.pool, h)
		if !ok {
			q.w.log.Error("stale component handle in query", "entity_index", i)
			continue
		}
		if !fn(newEntity(uint32(i), meta.generation), val) {
			return
		}
	}
}

// Len returns the number of entities that match the query.
func (q *Query[T]) Len() int {
	if q.info == nil {
		return 0
	}
	if q.members != nil {
		return len(q.members)
	}
	count := 0
	for i := range q.w.entities.metas {
		meta := &q.w.entities.metas[i]
		if !meta.alive {
			continue
		}
		if _, ok := meta.components[q.componentID]; ok {
			count++
		}
	}
	return count
}

// Filter returns a new Query that only matches entities for which fn returns true.
// The original query is not modified.
func (q *Query[T]) Filter(fn func(Entity, *T) bool) *Query[T] {
	if q.info == nil {
		return &Query[T]{w: q.w}
	}
	members := make([]Entity, 0, 16)
	for i := range q.w.entities.metas {
		meta := &q.w.entities.metas[i]
		if !meta.alive {
			continue
		}
		h, ok := meta.components[q.componentID]
		if !ok {
			continue
		}
		val, ok := getTyped[T](q.w.pool, h)
		if !ok {
			continue
		}
		e := newEntity(uint32(i), meta.generation)
		if fn(e, val) {
			members = append(members, e)
		}
	}
	return &Query[T]{w: q.w, componentID: q.componentID, info: q.info, members: members}
}

// Collect returns a slice of all entities matching the query.
func (q *Query[T]) Collect() []Entity {
	if q.info == nil {
		return nil
	}
	if q.members != nil {
		out := make([]Entity, len(q.members))
		copy(out, q.members)
		return out
	}
	var result []Entity
	for i := range q.w.entities.metas {
		meta := &q.w.entities.metas[i]
		if !meta.alive {
			continue
		}
		if _, ok := meta.components[q.componentID]; ok {
			result = append(result, newEntity(uint32(i), meta.generation))
		}
	}
	return result
}

// Group converts the query results into a Group for SIMD / bulk operations.
func (q *Query[T]) Group() *Group {
	return &Group{w: q.w, members: q.Collect()}
}

// Any returns the first entity matching the query, or false if none.
func (q *Query[T]) Any() (Entity, *T, bool) {
	if q.info == nil {
		return 0, nil, false
	}
	if q.members != nil {
		for _, e := range q.members {
			meta := q.w.entities.meta(e)
			if meta == nil {
				continue
			}
			h, ok := meta.components[q.componentID]
			if !ok {
				continue
			}
			val, ok := getTyped[T](q.w.pool, h)
			if !ok {
				continue
			}
			return e, val, true
		}
		return 0, nil, false
	}
	for i := range q.w.entities.metas {
		meta := &q.w.entities.metas[i]
		if !meta.alive {
			continue
		}
		h, ok := meta.components[q.componentID]
		if !ok {
			continue
		}
		val, ok := getTyped[T](q.w.pool, h)
		if !ok {
			continue
		}
		return newEntity(uint32(i), meta.generation), val, true
	}
	return 0, nil, false
}

// CountIf returns the number of entities for which fn returns true.
func (q *Query[T]) CountIf(fn func(Entity, *T) bool) int {
	if q.info == nil {
		return 0
	}
	count := 0
	if q.members != nil {
		for _, e := range q.members {
			meta := q.w.entities.meta(e)
			if meta == nil {
				continue
			}
			h, ok := meta.components[q.componentID]
			if !ok {
				continue
			}
			val, ok := getTyped[T](q.w.pool, h)
			if !ok {
				continue
			}
			if fn(e, val) {
				count++
			}
		}
		return count
	}
	for i := range q.w.entities.metas {
		meta := &q.w.entities.metas[i]
		if !meta.alive {
			continue
		}
		h, ok := meta.components[q.componentID]
		if !ok {
			continue
		}
		val, ok := getTyped[T](q.w.pool, h)
		if !ok {
			continue
		}
		e := newEntity(uint32(i), meta.generation)
		if fn(e, val) {
			count++
		}
	}
	return count
}

// Group holds a set of entities for iteration and bulk operations.
// Two modes:
//   - Signature-based: ids are set, members are resolved dynamically by scanning the world.
//   - Entity-based: ids are nil, members are an explicit list controlled by the user.
type Group struct {
	w       *World
	ids     []ComponentID // non-nil = signature-based mode
	members []Entity      // resolved or explicit entity list
}

// resolve builds the members list for signature-based groups.
// Entity-based groups already have their members set.
func (g *Group) resolve() {
	if g.ids == nil || g.members != nil {
		return
	}
	for i := range g.w.entities.metas {
		meta := &g.w.entities.metas[i]
		if !meta.alive {
			continue
		}
		match := true
		for _, cid := range g.ids {
			if _, ok := meta.components[cid]; !ok {
				match = false
				break
			}
		}
		if match {
			g.members = append(g.members, newEntity(uint32(i), meta.generation))
		}
	}
}

// ForEach iterates the group's entities, calling fn for each.
func (g *Group) ForEach(fn func(e Entity)) {
	g.resolve()
	for _, e := range g.members {
		fn(e)
	}
}

// Len returns the number of entities in the group.
func (g *Group) Len() int {
	g.resolve()
	return len(g.members)
}

// --- Constructors ---

// newGroup creates a signature-based Group from component instances.
// The values are only used to determine their types; actual values are ignored.
func newGroup(w *World, components ...any) *Group {
	g := &Group{w: w}
	for _, c := range components {
		typ := reflect.TypeOf(c)
		info, ok := w.registry.lookup(typ)
		if !ok {
			w.log.Error("group created with unregistered component", "type", typ.String())
			continue
		}
		g.ids = append(g.ids, info.id)
	}
	w.log.Debug("group created", "component_ids", g.ids)
	return g
}

// --- Public constructors ---

// NewGroup creates an empty Group. Use Add/AddEntities to populate it,
// or pass component values to create a signature-based group.
//
//	var g *ecs.Group
//	g = ecs.NewGroup(w)                          // empty
//	g = ecs.NewGroup(w, Position{}, Velocity{})   // signature-based (all matching)
func NewGroup(w *World, components ...any) *Group {
	if len(components) == 0 {
		return &Group{w: w}
	}
	return newGroup(w, components...)
}

// NewGroupOf creates a Group containing exactly the given entities.
func NewGroupOf(w *World, entities ...Entity) *Group {
	members := make([]Entity, 0, len(entities))
	for _, e := range entities {
		if w.entities.alive(e) {
			members = append(members, e)
		}
	}
	return &Group{w: w, members: members}
}

// NewGroupFrom creates a Group containing the entities in the slice.
func NewGroupFrom(w *World, entities []Entity) *Group {
	members := make([]Entity, 0, len(entities))
	for _, e := range entities {
		if w.entities.alive(e) {
			members = append(members, e)
		}
	}
	return &Group{w: w, members: members}
}

// --- Mutation methods (entity-based groups only) ---

// Add appends an entity to the group. Only works on entity-based groups;
// calling on a signature-based group converts it to entity-based first.
func (g *Group) Add(e Entity) {
	g.resolve()
	if !g.w.entities.alive(e) {
		return
	}
	g.ids = nil
	g.members = append(g.members, e)
}

// AddEntities appends multiple entities to the group.
func (g *Group) AddEntities(entities ...Entity) {
	g.resolve()
	g.ids = nil
	for _, e := range entities {
		if g.w.entities.alive(e) {
			g.members = append(g.members, e)
		}
	}
}

// Remove removes an entity from the group. No-op if not present.
func (g *Group) Remove(e Entity) {
	g.resolve()
	g.ids = nil
	for i, m := range g.members {
		if m == e {
			g.members = append(g.members[:i], g.members[i+1:]...)
			return
		}
	}
}

// Clear removes all entities from the group.
func (g *Group) Clear() {
	g.ids = nil
	g.members = g.members[:0]
}

// Filter returns a new Group containing only entities for which fn returns true.
func (g *Group) Filter(fn func(Entity) bool) *Group {
	g.resolve()
	filtered := make([]Entity, 0, len(g.members))
	for _, e := range g.members {
		if fn(e) {
			filtered = append(filtered, e)
		}
	}
	return &Group{w: g.w, members: filtered}
}
