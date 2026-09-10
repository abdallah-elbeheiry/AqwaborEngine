---
title: The entity component system
tags: [engine, aqwabor, ecs]
---

Entities are generational handles, a component type is one dense array, and a system iterates the
rows that are awake.

The shape to hold in mind is that storage and scheduling are separate.
A component store says where a value lives; it does not decide who runs.
What runs is the awake partition of a store, or a timer coming due, or a set a system keeps for
itself, and an entity with nothing to do is in no list anything walks.

```go
w := ecs.NewWorld()
pos := ecs.MustRegister[Position](w)

e := w.Create()
pos.Set(e, Position{X: 1})
pos.Wake(e)

pos.Each(func(e ecs.Entity, p *Position) { p.X++ })
```

## Registering a component

`Register` returns a handle, and the handle is the only way to reach the component.
It carries a pointer straight to the storage for its type, so reading a component is an index into an
array: no map, no reflection, no type assertion.
Keep what registration returns; it is not recoverable from the world afterwards.

```go
pos, err := ecs.Register[Position](w)   // error if the type cannot be a component
vel := ecs.MustRegister[Velocity](w)    // panics instead, for game code and tests
```

Registering the same type twice returns the same handle rather than a second store.

A component must be plain data.
A type holding a pointer, slice, map, string, channel, func or interface is refused with
`ErrComponentHasPointers`, because one dense array per type holds nothing the garbage collector has
to walk, and a pointer inside a component gives back the per-object cost the layout exists to avoid.
A string counts: its header carries a pointer to its bytes.

A zero-size type is allowed. A tag component carrying no fields is an ordinary pattern.

Share by index, not by pointer.
Where several entities need one value, put it in a table the game owns and give the component an
index into it.

## Reading and writing

```go
p, ok := pos.Get(e)      // pointer into the storage, valid until the next structural change
pos.Set(e, Position{})   // adds if absent, overwrites if present
pos.Has(e)
pos.Remove(e)
```

`Set` overwrites. A value a caller asks to replace is replaced.

A pointer from `Get` is invalidated by adding or removing this component type on any entity, because
a removal moves one row. Read it, use it, do not store it.

## Entities

```go
e := w.Create()
w.Alive(e)
w.Destroy(e)   // removes it from every store, every set, and cancels its timer
w.Count()
```

An `Entity` is an index and a generation. Destroying one raises the generation on its index, so a
handle to a destroyed entity is rejected by every accessor rather than reaching whatever reused the
index. `ecs.NoEntity` is the absent entity; `Entity(0)` is a real one.

## Iterating

There is no query object. A component iterates itself.

```go
pos.Each(func(e ecs.Entity, p *Position) { ... })   // the awake rows
pos.All(func(e ecs.Entity, p *Position) { ... })    // every row, awake or not
pos.EachUntil(func(e ecs.Entity, p *Position) bool { ... })

ecs.Each2(pos, vel, func(e ecs.Entity, p *Position, v *Velocity) { ... })
ecs.Each3(pos, vel, heat, func(e ecs.Entity, p *Position, v *Velocity, h *Heat) { ... })
```

`Each` walks a contiguous run of memory, which is the whole return on the storage layout.

`Each2` walks the first component's rows and looks the second up per entity, so it costs one random
access an entity. Pass the smaller or more selective component first. Where two values are read
together on every pass, one component holding both fields is faster than two components joined here.

Structural change during iteration is not allowed: adding or removing the component being iterated
moves rows, and a moved row is either visited twice or missed. Defer it:

```go
cmd := w.Commands()
pos.Each(func(e ecs.Entity, p *Position) {
    if p.X > limit {
        cmd.Destroy(e)
    }
})
w.Flush()
```

`Rows()` hands back the dense array itself, and `Owners()` the entities at the same indices, for a
bulk or vectorised pass or a range handed to parallel workers. They are the storage, not a copy.

## What is awake

A component's rows are partitioned. Rows before the awake mark are what `Each` visits; the rest exist
and are not visited. Waking and sleeping are one swap and one integer each.

```go
pos.Wake(e)
pos.Sleep(e)
pos.Awake(e)
pos.AwakeLen()
pos.WakeAll()    // what loading a save does before it knows what is idle
pos.SleepAll()
```

A component starts asleep, because an entity that has just gained one has not yet been given a reason
to run. Code that expects to see every entity has to wake them.

This is the engine's answer to cost tracking activity rather than existence. A pass over 40,000
entities costs 31.9 microseconds; a pass over the hundred of them that are doing something costs
about what a hundred entities cost.

## Waiting

An entity that knows it has nothing to do for a while schedules itself rather than being visited and
rejected.

```go
w.Timers().After(e, 384)        // 3.2 seconds at 120 Hz
w.Timers().At(e, tick)
w.Timers().Cancel(e)
w.Timers().Scheduled(e)

w.Advance(func(e ecs.Entity) { pos.Wake(e) })   // one tick on, waking what is due
```

One timer per entity: an entity waiting on two things is waiting on the earlier of them, and holding
both would make cancelling ambiguous.

Time is the world's tick count rather than the wall clock, so a paused or fast-forwarded simulation
carries its timers with it. An idle tick over 40,000 waiting entities costs 2.3 nanoseconds.

## A set of a system's own

Where membership belongs to a system rather than to a component type — a queue waiting on a crane,
the grids a pressure pass still has to settle — the awake partition is the wrong shape.

```go
set := w.NewSet()
set.Add(e)
set.Remove(e)
set.Has(e)
set.Each(func(e ecs.Entity) { ... })

ecs.EachIn(set, pos, func(e ecs.Entity, p *Position) { ... })
ecs.EachIn2(set, pos, vel, func(e ecs.Entity, p *Position, v *Velocity) { ... })
```

A set is registered with its world, so a destroyed entity leaves every set.

Most systems should not use one. A component's partition is contiguous and a set joined against
components is not, so reach for a set only when the membership genuinely is not a component's.

## Systems

`System` is a registry, not a tick path. It is how one piece of code finds another; what runs is the
scheduler's business.

```go
type Power struct {
    ecs.Base
    // ...
}

w.MustRegisterSystem(&Power{})
p, ok := ecs.GetSystem[*Power](w)
```

`ecs.Base` satisfies the interface. There is no `Update` method, because nothing ever called one.

## Deferred change

```go
cmd := w.Commands()
cmd.Destroy(e)
cmd.Do(func(w *ecs.World) { ... })   // spawning, which also moves rows
w.Flush()
```

A concurrent schedule flushes at the boundary between stages, where no iteration is in progress. See
[scheduling](scheduling.md).

## Loading a save

`w.Reset()` empties every store and set and retires every entity, keeping registrations, so component
handles stay valid across it. That is what loading wants rather than a new world.

## What's next

- What runs, in what order, and what may run at the same time: [scheduling](scheduling.md)
- Getting entities onto the screen: [the render layer](render.md)
- What changed from v0.1.0, if you are porting: [migrating](migrating.md)
