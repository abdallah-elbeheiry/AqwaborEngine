---
title: Scheduling
tags: [engine, aqwabor, schedulers]
---

Two things decide when code runs. A `Scheduler` drives fixed-rate ticks off a background goroutine.
A `Schedule` decides, within one tick, what may run at the same time as what.

They are separable: a game can drive ticks itself and still use a `Schedule`, or run a `Scheduler`
with plain functions and no access declarations at all.

## Fixed-rate ticks

```go
s := schedulers.NewScheduler()
s.Run(simulate, 120)      // 120 Hz
s.Run(autosave, 0.1)      // once every ten seconds

s.Start()
s.SetSpeed(3)             // three times as fast; scales every rate
s.Pause()
s.Resume()
s.Stop()
```

Each rate keeps its own accumulator and its own tick count. Functions registered at the same rate run
together, in registration order; rates run in ascending order, so two runs of the same registrations
tick in the same order.

Registering while the scheduler runs is allowed. The running goroutine picks the change up at its
next wake rather than reading a slice being appended to.

### What a tick is handed

```go
type TickState struct {
    Tick uint64   // this rate's own tick count, from zero
    Hz   float64  // the rate it was registered at
}

func (t TickState) Delta() float64   // 1/hz, fixed regardless of how far behind the loop is
```

The tick count is the primary field. A simulation counting in integers should not be obliged to carry
a float, and a rate is `rate * ticks / hz` rather than an accumulated delta.

### Falling behind

Catch-up is bounded. A rate runs at most `MaxCatchUp` ticks a wake, eight by default, and discards
what it still owes past that.

Without the bound, a tick that overran its budget left a larger accumulator, so the next wake ran
more ticks, which overran further: the fixed-timestep spiral. With it, the simulation runs slower
than wall time, which is the correct failure — it degrades to a lower effective rate rather than
falling further behind on every wake. Extreme time scaling is the condition that finds this, and the
loop is meant to survive it.

```go
s.SetMaxCatchUp(4)
s.Dropped()      // ticks discarded; a rising count is the simulation not keeping up
s.Ticks(120)     // how many ticks a rate has run
```

`Dropped` is the number to watch when time scaling is turned up.

## What may run at the same time

A `Schedule` runs systems concurrently where their declared access proves it is safe, and in order
where it does not.

```go
sched := schedulers.NewSchedule()
sched.SetBarrier(w.Flush)

sched.Add("move",    schedulers.Reads(rVel).Writes(rPos),   moveSystem)
sched.Add("thermal", schedulers.Writes(rHeat),              thermalSystem)
sched.Add("extract", schedulers.Reads(rPos),                extractSystem)

s.Run(sched.Run, 120)
```

Two systems may run at the same time when neither writes something the other touches; a write
excludes every other access to that resource, including a read. The scheduler derives that rather
than the author asserting it, which is what makes the concurrency a proof instead of a convention.

Registration order is the intended order. A system that conflicts with an earlier one runs after it;
one that conflicts with nothing may run alongside anything. A system interacting with another through
something it did not declare is a bug in the declaration, not in the schedule.

### Resources

A resource is not a component type.

```go
var rGrid = schedulers.ResourceOf("Grid")
var rPos  = schedulers.Resource(pos.ID())
schedulers.NameResource(rPos, "Position")   // so a conflict report reads as words
```

A game keeping its cell grid in dense arrays outside the entity system still needs two systems
touching that grid not to overlap, so a resource is any exclusively-written thing: a component type,
a grid, a lookup table.

### Stages and the barrier

Systems are grouped into stages, each taking the earliest stage holding nothing it conflicts with.
The stage count caps the scaling, because every boundary is a synchronisation.

The barrier runs after each stage. Applying a world's deferred command buffer belongs there, because
no iteration is in progress at a boundary.

```go
sched.Stages()      // the system names per stage, in order
sched.Conflicts()   // every pair that cannot run together, and the resource that decides it
```

`Conflicts` is how a schedule with more stages than expected is read.

### When a stage is split

Handing work to other goroutines costs about 3.3 microseconds before any of it happens, so a stage
cheaper than that loses by being split. A stage runs serially until it has been measured to cost more
than `DefaultParallelFloor`, and the estimate is a weighted mean of what the stage actually cost, so
nothing has to be declared expensive.

```go
sched.SetParallelFloor(20 * time.Microsecond)
sched.SetParallel(false)   // everything serial, for comparing results
```

Measured on an M4 Max, sixteen logical cores, eight systems in one stage: compute-bound work went
from 285 to 87.5 microseconds, and eight systems each walking their own 320 KB array from 80.3 to
36.9.

Whether memory traffic costs the gain depends on what is shared. Splitting one array across workers
returned nothing in a separate measurement, while systems over separate arrays scaled, because those
fit in separate caches. So the floor is a floor rather than a rule about which passes are worth
splitting.

## Parallel work inside one system

```go
schedulers.ParallelFor(len(items), func(i int) { process(items[i]) })

f := schedulers.Go(func() int { return heavy() })
val := f.Get()
schedulers.AwaitAll(f1, f2, f3)
```

The same floor applies: `ParallelFor` costs 3305 nanoseconds to do nothing, and a pass under roughly
50 microseconds loses by being split.

The natural partition for a world of separate grids is one worker per grid, because power, signal,
logistics and atmosphere all resolve within one grid and share nothing across them. For a diffusion
pass, reading one buffer and writing a second makes every cell independent and removes the question
of update order rather than answering it.

## A panicking system

Both a `Scheduler` tick and a `Schedule` stage recover, log, and carry on, so one system panicking
does not take the tick or the process with it.

## What's next

- What the systems iterate: [the entity component system](ecs.md)
- Where the frame's own phases sit: [the render layer](render.md)
