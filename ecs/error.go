package ecs

import "fmt"

// ErrEntityDead names a handle whose entity is gone, or whose generation has
// been superseded by a later entity at the same index.
type ErrEntityDead struct{ Entity Entity }

func (e *ErrEntityDead) Error() string {
	return fmt.Sprintf("entity %d (index %d, generation %d) is not alive",
		uint64(e.Entity), e.Entity.Index(), e.Entity.Generation())
}

// ErrSystemRegistered names a second registration of one concrete system type.
type ErrSystemRegistered struct{ Type string }

func (e *ErrSystemRegistered) Error() string {
	return fmt.Sprintf("system %s is already registered", e.Type)
}
