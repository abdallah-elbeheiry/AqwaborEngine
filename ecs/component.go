package ecs

import (
	"errors"
	"reflect"
)

var (
	// ErrComponentHasPointers rejects a component type the collector would have
	// to walk. Components live in one dense array per type, and an array of
	// plain data holds nothing to scan; a pointer inside a component gives back
	// the per-object cost the storage exists to avoid.
	ErrComponentHasPointers = errors.New("component type contains pointers (slices, maps, strings, interfaces, channels, funcs, or pointers); components must be plain data")

	// ErrComponentNotConcrete rejects a type that cannot be stored at all.
	ErrComponentNotConcrete = errors.New("component type must be a concrete struct or scalar, not an interface")
)

// ComponentID is a dense identifier for a registered component type, and an
// index into the world's stores.
type ComponentID uint32

// Comp is what Register returns and the only way to reach a component value.
// It carries a pointer straight to the storage for its type, so reading a
// component is an index into an array: no map, no reflection, no type
// assertion. Registration is where reflection happens, once per type.
//
// A Comp belongs to the world that produced it, and is not valid against
// another one.
type Comp[T any] struct {
	id ComponentID
	s  *typedStore[T]
}

// ID returns the identifier a system uses to declare that it touches this
// component.
func (c Comp[T]) ID() ComponentID { return c.id }

// Valid reports whether this came from a registration. The zero Comp has no
// storage and every operation on it fails.
func (c Comp[T]) Valid() bool { return c.s != nil }

// Get returns a pointer to e's value. It stays valid until the next structural
// change to this component type, because a removal moves one row.
func (c Comp[T]) Get(e Entity) (*T, bool) {
	if c.s == nil {
		return nil, false
	}
	return c.s.get(e)
}

// Has reports whether e holds this component.
func (c Comp[T]) Has(e Entity) bool { return c.s != nil && c.s.has(e) }

// Set writes e's value, adding the component when the entity lacks it and
// overwriting when it has it. Overwriting is deliberate: the previous
// implementation warned and kept the old value, so a caller asking to replace
// silently got the original back.
//
// A newly added component starts asleep, because an entity that has just gained
// one has not yet been given a reason to run.
func (c Comp[T]) Set(e Entity, v T) bool {
	if c.s == nil || !c.s.w.Alive(e) {
		return false
	}
	c.s.set(e, v)
	return true
}

// Remove takes the component off e, reporting whether it was there.
func (c Comp[T]) Remove(e Entity) bool {
	if c.s == nil {
		return false
	}
	return c.s.remove(e)
}

// Wake puts e into the set this component's systems iterate.
func (c Comp[T]) Wake(e Entity) bool {
	if c.s == nil || !c.s.w.Alive(e) {
		return false
	}
	return c.s.wake(e)
}

// Sleep takes e out of the iterated set without discarding its value.
func (c Comp[T]) Sleep(e Entity) bool {
	if c.s == nil {
		return false
	}
	return c.s.sleep(e)
}

// Awake reports whether e is in the iterated set.
func (c Comp[T]) Awake(e Entity) bool { return c.s != nil && c.s.isAwake(e) }

// Len is how many entities hold this component, awake or not.
func (c Comp[T]) Len() int {
	if c.s == nil {
		return 0
	}
	return c.s.len()
}

// AwakeLen is how many of them are awake.
func (c Comp[T]) AwakeLen() int {
	if c.s == nil {
		return 0
	}
	return c.s.awake
}

// componentRegistry assigns an id per type. It is read at registration and
// never on an access path.
type componentRegistry struct {
	byType map[reflect.Type]ComponentID
	types  []reflect.Type
}

func newComponentRegistry() *componentRegistry {
	return &componentRegistry{byType: make(map[reflect.Type]ComponentID)}
}

// hasPointers reports whether a type holds anything the collector must walk.
// A string counts: its header carries a pointer to its bytes.
func hasPointers(t reflect.Type, seen map[reflect.Type]bool) bool {
	if t == nil {
		return false
	}
	if seen[t] {
		return false
	}
	seen[t] = true

	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func,
		reflect.Interface, reflect.String, reflect.UnsafePointer:
		return true
	case reflect.Struct:
		for i := range t.NumField() {
			if hasPointers(t.Field(i).Type, seen) {
				return true
			}
		}
		return false
	case reflect.Array:
		return hasPointers(t.Elem(), seen)
	default:
		return false
	}
}

// Register makes T a component of w and returns the handle used to reach it.
// Registering a type twice returns the same handle rather than a second store.
//
// A zero-size type is allowed: a tag component carrying no fields is an
// ordinary pattern, and it used to crash on an empty allocation.
func Register[T any](w *World) (Comp[T], error) {
	typ := reflect.TypeFor[T]()
	if typ == nil || typ.Kind() == reflect.Interface {
		return Comp[T]{}, ErrComponentNotConcrete
	}
	if hasPointers(typ, make(map[reflect.Type]bool)) {
		return Comp[T]{}, ErrComponentHasPointers
	}

	if id, ok := w.registry.byType[typ]; ok {
		s, ok := w.stores[id].(*typedStore[T])
		if !ok {
			return Comp[T]{}, ErrComponentNotConcrete
		}
		return Comp[T]{id: id, s: s}, nil
	}

	id := ComponentID(len(w.stores))
	s := newTypedStore[T](w, id)
	w.registry.byType[typ] = id
	w.registry.types = append(w.registry.types, typ)
	w.stores = append(w.stores, s)

	w.log.Info("component registered", "type", typ, "id", id, "size", typ.Size())
	return Comp[T]{id: id, s: s}, nil
}

// MustRegister is Register for game code and tests, panicking on a type that
// cannot be a component.
func MustRegister[T any](w *World) Comp[T] {
	c, err := Register[T](w)
	if err != nil {
		panic(err)
	}
	return c
}

// ComponentType names a registered component, for diagnostics and for messages
// that would otherwise carry a bare number.
func (w *World) ComponentType(id ComponentID) (reflect.Type, bool) {
	if int(id) >= len(w.registry.types) {
		return nil, false
	}
	return w.registry.types[id], true
}
