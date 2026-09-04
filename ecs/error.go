package ecs

import (
	"fmt"
	"reflect"
	"strings"
)

// MissingComponentsError is returned when an operation requires components
// that are not present on an entity. It carries the entity handle, the
// list of required component types, and the list of types actually present.
type MissingComponentsError struct {
	Entity   Entity
	Required []reflect.Type
	Present  []reflect.Type
}

func (e *MissingComponentsError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "entity %d missing components: ", e.Entity)
	for i, t := range e.Required {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(t.String())
	}
	if len(e.Present) > 0 {
		b.WriteString(" [present: ")
		for i, t := range e.Present {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(t.String())
		}
		b.WriteString("]")
	}
	return b.String()
}

// ErrEntityDead is returned when an operation targets a dead entity.
type ErrEntityDead struct {
	Entity Entity
}

func (e *ErrEntityDead) Error() string {
	return fmt.Sprintf("entity %d is dead", e.Entity)
}

// ErrComponentNotRegistered is returned when a component type has not been
// registered before use.
type ErrComponentNotRegistered struct {
	Type reflect.Type
}

func (e *ErrComponentNotRegistered) Error() string {
	return fmt.Sprintf("component type %s is not registered", e.Type)
}

// ErrHandleInvalid is returned when a pool handle is stale or invalid.
type ErrHandleInvalid struct {
	Handle Handle
}

func (e *ErrHandleInvalid) Error() string {
	return fmt.Sprintf("component handle (index=%d, gen=%d) is invalid", e.Handle.index, e.Handle.generation)
}
