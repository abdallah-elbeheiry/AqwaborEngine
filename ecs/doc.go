// Package ecs provides an Entity Component System for the AqwaborEngine.
//
// Entities are generational handles, a component type is one dense array, and a
// system iterates the rows that are awake. Storage and scheduling are separate:
// a store says where a value lives, not who runs.
//
// See docs/ecs.md at the repository root for the full guide.
package ecs
