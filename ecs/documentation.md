# ECS (`ecs/`)

Entity Component System with unified component pool, type-safe generics, and honest functions.

```
ecs/
├── doc.go           — package doc
├── ecs.go           — World, public API, generic wrappers
├── entity.go        — generational allocator, free-list, Entity type
├── component.go     — type registry, Handle, unified component pool
├── table.go         — archetype membership tracking + transition graph
├── sparse.go        — sparse-set storage
├── command.go       — deferred command buffer
├── query.go         — Query[T] / Group iteration
├── simd.go          — SIMD-accelerated bulk Group operations (vek)
├── system.go        — System registration + manager
├── error.go         — error types
├── ecs_test.go      — comprehensive tests
├── simd_test.go     — SIMD bulk operation tests
└── documentation.md — this file
```

---

## World

The single monolithic ECS world. Owns all entities, components, storage, and systems.

```go
w := ecs.NewWorld()
// or with a logger:
w := ecs.NewWorld(ecs.WithLogger(myLogger))
```

### Design rules

| Rule                 | Detail                                                                                                      |
|----------------------|-------------------------------------------------------------------------------------------------------------|
| **Honest functions** | Every function receives all state via parameters or the receiver. No package-level mutable globals.         |
| **Headless**         | Runs without a window, real input, or real audio. Tests supply a no-op `logx.Logger`.                       |
| **Logging**          | All structural events logged via `logx` with structured key/value fields. Inject a logger via `WithLogger`. |
| **Pure Go**          | `CGO_ENABLED=0`, no `internal/` packages.                                                                   |

---

## Entity lifecycle

```go
e := w.Create()            // Entity (generational uint64 handle)
w.Destroy(e, false)        // detach-only: component refcounts decremented
w.Destroy(e, true)         // cascade: also destroys component instances at refcount zero
alive := w.Alive(e)        // true if handle is valid (in range + generation matches)
```

`Entity` is a packed `uint64`: lower 32 bits = dense index, upper 32 bits = generation. Destroyed indices are recycled; stale handles are rejected via the generation counter.

### Destroy modes

| Mode                 | `cascade=false` (detach-only)   | `cascade=true` (cascade)  |
|----------------------|---------------------------------|---------------------------|
| Component refcounts  | Decremented                     | Decremented               |
| At refcount 0        | **Survives** (recyclable later) | **Destroyed immediately** |

---

## Component registration

Register component types once before use. Idempotent.

```go
ecs.Register[Position](w)
ecs.Register[Velocity](w)
ecs.Register[Health](w)
```

Components are ordinary Go structs. No interfaces, no embedding, no tags.

---

## Unified component model

Every component instance lives in a single unified pool, identified by a generational `Handle`. Multiple entities can share the same handle, meaning they share the underlying data. This replaces the old owned/shared split.

### Adding components (1:1 case)

`Add` creates a new instance and attaches it to one entity. This is the common case.

```go
e := w.Create()
ecs.Add[Position](w, e, Position{X: 10, Y: 20})
ecs.Add[Velocity](w, e, Velocity{DX: 1, DY: 1})

pos, ok := ecs.Get[Position](w, e)
has := ecs.Has[Velocity](w, e)

ecs.Remove[Velocity](w, e)
```

Adding or removing a component that changes the entity's archetype triggers an automatic table transition.

### Sharing components (N:1 case)

Use `Create` + `Attach` when multiple entities need to share the same data.

```go
// Create a shared instance (returns an opaque handle)
h, _ := ecs.Create[Position](w, Position{X: 5, Y: 5})

// Attach to multiple entities
ecs.Attach[Position](w, e1, h)
ecs.Attach[Position](w, e2, h)
ecs.Attach[Position](w, e3, h)

// All three see the same data
pos, _ := ecs.Get[Position](w, e1)
pos.X = 99  // visible through e2 and e3

// Detach from one entity (refcount decremented)
ecs.Detach[Position](w, e2)
```

`Handle` is an opaque value type (index + generation) that the user can store and pass around. Multiple entities may hold the same handle, sharing the underlying component instance. The instance is reference-counted.

### Destroying handles

```go
ecs.DestroyHandle(w, h)  // explicitly destroys the pool slot
```

Destroying an entity with `cascade=true` also destroys component instances whose refcount reaches zero.

---

## Iteration

### Query — single-component iteration

```go
q := ecs.NewQuery[Position](w)

q.ForEach(func(e ecs.Entity, pos *ecs.Position) {
    pos.X += pos.DX * dt
})
```

Scans all alive entities and returns those that own the queried component. Returns a pointer valid only for the callback duration.

#### Query extras

```go
// Count
n := q.Len()

// Early exit
q.ForEachUntil(func(e ecs.Entity, p *ecs.Position) bool {
    if p.X > 100 { return false }
    p.X += 1
    return true
})

// Filter (returns a new Query, original unchanged)
close := q.Filter(func(e ecs.Entity, p *ecs.Position) bool {
    return p.X < 100
})

// Collect entity list
ents := q.Collect()

// Bridge to Group for SIMD / bulk work
g := q.Group()
g.AddNumber(func(c any) *float64 { return &c.(*Position).X }, 100)

// First match
e, pos, ok := q.Any()

// Conditional count
n := q.CountIf(func(e ecs.Entity, p *ecs.Position) bool {
    return p.X > 50
})
```

### Group — multi-component iteration

Groups hold a set of entities for iteration and bulk operations. Two modes:

**Signature-based** — matches all entities with the given components:

```go
g := ecs.NewGroup(w, Position{}, Velocity{})

g.ForEach(func(e ecs.Entity) {
    pos, _ := ecs.Get[Position](w, e)
    vel, _ := ecs.Get[Velocity](w, e)
    pos.X += vel.DX * dt
})
```

**Entity-based** — exactly the entities you specify:

```go
// From explicit entities
g := ecs.NewGroupOf(w, e1, e2, e47)

// From a slice
g := ecs.NewGroupFrom(w, myEntitySlice)

// Empty, then build incrementally
g := ecs.NewGroup(w)
g.Add(e1)
g.AddEntities(e2, e3, e4)
```

### Group mutation

```go
g := ecs.NewGroup(w)

g.Add(e1)                    // add single entity (skips dead)
g.AddEntities(e1, e2, e3)    // add multiple
g.Remove(e2)                 // remove by value
g.Clear()                    // remove all

// Filter returns a new group (original unchanged)
close := g.Filter(func(e ecs.Entity) bool {
    pos, _ := ecs.Get[Position](w, e)
    return pos.X < 100
})
```

Calling `Add`/`AddEntities`/`Remove`/`Clear` on a signature-based group converts it to entity-based (the currently matched entities become the starting set).

---

## Systems

Systems are plain structs that implement the `System` interface.

```go
type MovementSystem struct{}

func (s *MovementSystem) Update(dt float64) {
    // run queries, mutate components, etc.
}

w.RegisterSystem(&MovementSystem{})

sys, ok := ecs.GetSystem[*MovementSystem](w)
```

Systems are registered by concrete type. Duplicate registration is idempotent.

---

## Flush

Structural operations are immediate by default. `Flush` applies any deferred changes buffered via the command buffer.

```go
w.Flush()
```

---

## Errors

All errors are concrete, typed structs.

| Error                        | When                                                                             |
|------------------------------|----------------------------------------------------------------------------------|
| `*ErrEntityDead`             | Operation on a destroyed entity                                                  |
| `*ErrComponentNotRegistered` | Component type not registered before use                                         |
| `*ErrHandleInvalid`          | Stale or invalid component `Handle`                                              |
| `*MissingComponentsError`    | Entity missing required components (carries `Required` and `Present` type lists) |

```go
err := ecs.Add(w, deadEntity, Position{})
if var e *ecs.ErrEntityDead; errors.As(err, &e) {
    // handle dead entity
}
```

### Must* helpers

For the common "this must succeed" path (game code, tests), thin `Must*` wrappers panic after logging. This eliminates `if err != nil` noise while keeping the error-returning versions available for the rare cases that need them.

```go
// Panics on error — use in game systems and tests
ecs.MustAdd[Position](w, e, Position{X: 1, Y: 2})
ecs.MustRemove[Velocity](w, e)
h := ecs.MustCreate[Position](w, Position{X: 5, Y: 5})
ecs.MustAttach[Position](w, e, h)
ecs.MustDetach[Position](w, e)
ecs.MustRegisterSystem(w, &MovementSystem{})
```

| Function             | Panics when                                                     |
|----------------------|-----------------------------------------------------------------|
| `MustAdd`            | Unregistered type, dead entity                                  |
| `MustRemove`         | Dead entity                                                     |
| `MustCreate`         | Unregistered type                                               |
| `MustAttach`         | Dead entity, stale handle, unregistered type                    |
| `MustDetach`         | Dead entity                                                     |
| `MustRegisterSystem` | Already registered (should not happen — register is idempotent) |

Use the error-returning versions only when the caller genuinely needs to handle failure (e.g. user-facing APIs, validation). In systems and tests, prefer `Must*`.

---

## Full API reference

```go
// Construction
func NewWorld(opts ...WorldOption) *World
func WithLogger(l *logx.Logger) WorldOption

// Entity
func (w *World) Create() Entity
func (w *World) Destroy(e Entity, cascade bool)
func (w *World) Alive(e Entity) bool

// Registration
func Register[T any](w *World)

// Convenience: create + attach (1:1 case)
func Add[T any](w *World, e Entity, c T) error
func MustAdd[T any](w *World, e Entity, c T)
func Remove[T any](w *World, e Entity) error
func MustRemove[T any](w *World, e Entity)

// Create / Destroy handle (N:1 shared case)
func Create[T any](w *World, c T) (Handle, error)
func MustCreate[T any](w *World, c T) Handle
func DestroyHandle(w *World, h Handle)

// Attach / Detach handle to/from entity
func Attach[T any](w *World, e Entity, h Handle) error
func MustAttach[T any](w *World, e Entity, h Handle)
func Detach[T any](w *World, e Entity) error
func MustDetach[T any](w *World, e Entity)

// Get / Has / HandleOf
func Get[T any](w *World, e Entity) (*T, bool)
func Has[T any](w *World, e Entity) bool
func HandleOf[T any](w *World, e Entity) (Handle, bool)

// Iteration
func NewQuery[T any](w *World) *Query[T]
func (q *Query[T]) ForEach(fn func(Entity, *T))
func (q *Query[T]) ForEachUntil(fn func(Entity, *T) bool)
func (q *Query[T]) Len() int
func (q *Query[T]) Filter(fn func(Entity, *T) bool) *Query[T]
func (q *Query[T]) Collect() []Entity
func (q *Query[T]) Group() *Group
func (q *Query[T]) Any() (Entity, *T, bool)
func (q *Query[T]) CountIf(fn func(Entity, *T) bool) int
func NewGroup(w *World, components ...any) *Group        // empty or signature-based
func NewGroupOf(w *World, entities ...Entity) *Group     // explicit entity set
func NewGroupFrom(w *World, entities []Entity) *Group    // from slice
func (g *Group) ForEach(fn func(Entity))
func (g *Group) Len() int
func (g *Group) Add(e Entity)
func (g *Group) AddEntities(entities ...Entity)
func (g *Group) Remove(e Entity)
func (g *Group) Clear()
func (g *Group) Filter(fn func(Entity) bool) *Group

// Systems
func (w *World) RegisterSystem(s System) error
func (w *World) MustRegisterSystem(s System)
func GetSystem[T System](w *World) (T, bool)

// Flush
func (w *World) Flush()
```

---

## SIMD / bulk operations

Group provides SIMD-accelerated bulk operations via the [vek](https://github.com/viterin/vek) library. These work with **both** signature-based and entity-based groups.

### FieldGetter

A `FieldGetter` extracts a `*float64` from a component passed as `any`:

```go
type FieldGetter func(c any) *float64

// Example: extract Position.X
func getX(c any) *float64 { return &c.(*Position).X }
```

For `float32` operations, use `FieldGetter32`:

```go
type FieldGetter32 func(c any) *float32
```

### Gather operations

```go
// Signature-based group
g := ecs.NewGroup(w, Position{}, Velocity{})

// Or entity-based — select specific entities for SIMD
g := ecs.NewGroupOf(w, e1, e2, e3, e47)

// Gather a contiguous []float64 from Position.X across all group entities
xs := g.Float64s(func(c any) *float64 { return &c.(*Position).X })

// Gather a contiguous []float32
ys := g.Float32s(func(c any) *float32 { v := float32(c.(*Position).Y); return &v })

// Number of entities in the group
n := g.Len()
```

### Bulk math (float64)

All bulk ops gather, compute via vek SIMD, then scatter results back:

```go
// dst = dst + src (element-wise, writes back to dst field)
g.AddVec(dstGetter, srcGetter)

// dst = dst - src
g.SubVec(dstGetter, srcGetter)

// dst = dst * src
g.MulVec(dstGetter, srcGetter)

// dst = dst / src
g.DivVec(dstGetter, srcGetter)

// dst = dst + scalar
g.AddNumber(dstGetter, 1.0)

// dst = dst - scalar
g.SubNumber(dstGetter, 1.0)

// dst = dst * scalar
g.MulNumber(dstGetter, 2.0)

// dst = dst / scalar
g.DivNumber(dstGetter, 2.0)
```

### Bulk math (float32)

Same operations with `32` suffix: `AddVec32`, `SubVec32`, `MulVec32`, `DivVec32`, `AddNumber32`, `SubNumber32`, `MulNumber32`, `DivNumber32`.

### Example: integrate positions on a subset

```go
// Select only entities near the camera
var close []ecs.Entity
ecs.NewQuery[Position](w).ForEach(func(e ecs.Entity, p *Position) {
    if p.X < 100 {
        close = append(close, e)
    }
})
g := ecs.NewGroupFrom(w, close)

// SIMD integrate only those entities
g.AddVec(
    func(c any) *float64 { return &c.(*Position).X },
    func(c any) *float64 { return &c.(*Velocity).DX },
)
g.AddVec(
    func(c any) *float64 { return &c.(*Position).Y },
    func(c any) *float64 { return &c.(*Velocity).DY },
)
```

### Thread safety

SIMD operations are **not** thread-safe. They mutate component data directly through pointers. If you use parallel systems, partition entities or use double-buffering.

---

## Determinism

The ECS is designed to be fully deterministic for any given sequence of public API calls. These properties are guaranteed:

| Area                              | Deterministic | Mechanism                                                              |
|-----------------------------------|:-------------:|------------------------------------------------------------------------|
| Entity ID allocation + recycling  |      Yes      | LIFO free-list, strict generation counters                             |
| Component pool allocation         |      Yes      | LIFO free-list, generation counters                                    |
| Table / archetype transitions     |      Yes      | Sorted archetype keys, pure transition functions                       |
| Query / Group iteration order     |      Yes      | Walks `entities.metas` by dense index — stable for a given world state |
| Map iteration (component release) |      Yes      | Keys sorted by `ComponentID` before iterating                          |
| Logging / side-effects            |      Yes      | Given the same inputs, logx produces the same output                   |

### Rules for consumers

1. **Never depend on Query/Group entity order for gameplay logic unless you sort yourself.** The order is deterministic for a given world state, but it reflects creation/destruction history, not spatial or semantic order.

2. **Systems must not assume a fixed iteration order across frames.** The order is stable within a frame but may shift if entities are created/destroyed between frames.

3. **Free-lists are strictly LIFO.** Entity and pool indices are recycled in reverse creation order. This is an implementation detail, not a public guarantee — rely on the determinism table above, not on free-list behavior.

4. **No parallel mutation.** The ECS is single-threaded. If you add parallel systems later, you must partition entities or use double-buffering to preserve determinism.

---

## Storage architecture (internal, not user-facing)

The ECS uses a unified component pool with archetype-based entity grouping:

- **Unified component pool** — All component instances live in a single pool of `poolInstance` slots. Each slot holds an `unsafe.Pointer` to the data, a generation counter, and a reference count. `Handle` (index + generation) provides safe access.

- **Archetype tables** — One table per unique set of component types. Tables store entity membership only (no column data). Entities move between tables when their component signature changes. An archetype transition graph (`+C` / `-C` edges) makes transitions fast.

- **Entity allocator** — Dense metadata array with a free-list for recycling. Each entity stores a `map[ComponentID]Handle` linking component types to pool slots. Generation counter prevents use-after-free on stale handles.

- **Refcounting** — Each pool slot tracks how many entities reference it. When refcount reaches zero, the slot can be recycled. `cascade=true` on destroy immediately reclaims zero-refcount slots.

---

## Headless testing

The entire ECS runs without a window, real input, or real audio. Tests supply a no-op logger:

```go
logx.Discard()
w := ecs.NewWorld(ecs.WithLogger(logx.With("component", "test")))
```

All tests cover: entity recycling, generation rejection, component add/get/has/remove, handle-based sharing, attach/detach, refcount cascade/detach-only, stale handle detection, query iteration, signature-based groups, entity-based groups (NewGroupOf, NewGroupFrom, Add, AddEntities, Remove, Clear, Filter), SIMD bulk operations on both group types, deterministic destruction order, and headless execution.
