---
title: Moving from v0.1.0 to v0.2.0
tags: [engine, aqwabor, release]
---

v0.2.0 breaks the entity component system and the scheduler.
The module path does not change: v0 and v1 share the bare path, so no import line moves.

Why the break was taken rather than deferred: components were stored as individually allocated
values reached through a pointer and a map per entity, which cost 21x a plain slice to iterate and
404 bytes an entity for 36 bytes of data. Nothing about that is fixable without changing the API.

## Components

Registration returns a handle, and the handle is how a component is reached. There are no
package-level accessors any more, because each one resolved its type through reflection on every
call.

```go
// before
ecs.Register[Position](w)
ecs.MustAdd(w, e, Position{X: 1})
p, ok := ecs.Get[Position](w, e)
ecs.Has[Position](w, e)
ecs.MustRemove[Position](w, e)

// after
pos := ecs.MustRegister[Position](w)   // keep this
pos.Set(e, Position{X: 1})             // adds or overwrites
p, ok := pos.Get(e)
pos.Has(e)
pos.Remove(e)
```

`Set` overwrites where `Add` warned and kept the old value.

A component type holding a pointer, slice, map, string, channel, func or interface is refused, and a
zero-size tag component is now allowed where it used to crash.

## Iteration

Queries are gone. A component iterates itself, and iteration visits the rows that are awake.

```go
// before
q := ecs.NewQuery[Position](w)
q.ForEach(func(e ecs.Entity, p *Position) { ... })

// after
pos.Each(func(e ecs.Entity, p *Position) { ... })   // awake rows
pos.All(func(e ecs.Entity, p *Position) { ... })    // every row
ecs.Each2(pos, vel, func(e ecs.Entity, p *Position, v *Velocity) { ... })
```

A component starts asleep. Code that used to see every entity needs `pos.WakeAll()` after loading,
or `pos.Wake(e)` where the entity is given something to do.

`pos.Rows()` hands back the dense array itself, which is what a bulk or vectorised pass wants.

## What was removed

`ecs.Handle`, `ecs.Create`, `ecs.Attach`, `ecs.Detach` and shared component instances. One dense
array per type has nowhere to put a value two entities point at. Share by index into a table the
game owns, or by the palette index the instance layout carries.

`ecs.Group` and the vek-backed SIMD layer. It gathered values by catching a panic per component per
entity and cost 281x the plain loop; with dense storage the loop is the fast path.

`System.Update`. Nothing ever called it. `System` is satisfied by embedding `ecs.Base`.

`World.Destroy` no longer takes a cascade flag, and `World.Alive` rejects a handle whose generation
has been superseded, which every accessor now does.

## Scheduler

`TickState.DeltaTime` is now `TickState.Delta()`, and `Tick` is the field to reach for. Catch-up is
bounded, so a rate that cannot keep up drops ticks and reports them through `Dropped()` rather than
compounding.

## New, and worth adopting rather than working around

- `World.Timers()` schedules an entity for a future tick. An idle tick over 40,000 waiting entities
  costs 2.3 ns, against the 254 microseconds a scan of that world costs.
- `World.NewSet()` gives a system its own membership where the awake partition of a component is the
  wrong shape.
- `schedulers.Schedule` runs systems concurrently where their declared reads and writes prove it is
  safe.
- `render.SubcellInstance` is a 16-byte instance for the dense layer, against 64 for a sprite.
