---
title: Scheduling
tags: [engine, aqwabor, schedulers]
---

Two things decide when code runs. A `Scheduler` drives fixed-rate ticks from a pure deterministic core.
A `Schedule` decides, within one tick, what may run at the same time as what.

They are separable: a game can drive ticks itself and still use a `Schedule`, or run a `Scheduler`
with plain functions and no access declarations at all.

## Deterministic clock

The scheduler owns a **deterministic simulation clock**. Time moves only when the caller advances it.
No wall clock, no timers, no goroutines participate in tick decisions. Given the same sequence of
advance calls and the same speed / pause settings, the sequence of tick values and the order of
function calls are bit-for-bit identical across runs, machines, and load conditions.

### Single-writer contract

`Advance` and `AdvanceTicks` must not be called concurrently. Exactly one caller drives the clock
at a time — a real-time loop, a test, or a replay harness. Under that contract the scheduler reads
all shared state through atomics and never acquires the mutex on the hot path.

`Run` (registering jobs) may be called concurrently with `Advance`. The function list is published
via atomic copy-on-write and the sorted order is published via an atomic pointer, so `Advance`
always sees a consistent snapshot without locking.

## Master Hz and the quantum

The scheduler runs on a **master quantum** derived from a configurable tick rate.

```go
s.SetMasterHz(60)         // set the master tick rate (required before AdvanceTicks)
s.SetMasterHz(120)        // can be changed between advances
s.MasterHz() uint         // current master Hz
s.Quantum() time.Duration // 1/masterHz * speed, or 0 if speed ≤ 0 or master Hz unset
```

- Master Hz must be ≥ 1 and ≤ 1000. Setting 0 is rejected.
- If `SetMasterHz` has never been called, `AdvanceTicks` panics (no default — the caller must
  commit to an explicit rate).
- `Quantum()` reflects the current speed setting: it is `time.Second / masterHz * speed`.
- Integer master tick count (`SimTicks`) is the source of truth; durations are derived when needed.

## Advancing time

There are two ways to advance the clock, each with clear semantics:

```go
result := s.Advance(simDT time.Duration) AdvanceResult     // explicit, unscaled
result := s.AdvanceTicks(n int) AdvanceResult               // n * quantum (scaled by speed)
```

### Advance(simDT) — explicit simulation time

`Advance` advances the clock by exactly `simDT` of simulation time. Speed is **ignored**.
A `simDT` of 10ms always advances 10ms regardless of the speed setting.

```go
s.SetMasterHz(60)
s.SetSpeed(3)
result := s.Advance(time.Second / 60) // advances exactly 1/60th of a second, not 3/60th
```

When the scheduler is paused, `Advance` is a no-op and returns an empty result.

### AdvanceTicks(n) — quantum-based advance

`AdvanceTicks` advances the clock by `n` master quanta, scaled by speed.

```go
s.SetMasterHz(60)
s.SetSpeed(2)
result := s.AdvanceTicks(1) // advances 2/60th of a second (1 quantum × speed 2)
result := s.AdvanceTicks(3) // advances 6/60th of a second (3 quanta × speed 2)
```

`AdvanceTicks(0)` and negative values are no-ops. When speed ≤ 0, `AdvanceTicks` advances nothing.
When paused, `AdvanceTicks` is a no-op.

### AdvanceResult

```go
type AdvanceResult struct {
    Fired   int    // total ticks fired across all rate groups
    Dropped uint64 // ticks discarded by catch-up in this call
}
```

### Clock inspection

```go
s.SimTime() time.Duration   // current simulation time
s.SimTicks() uint64         // number of master quanta advanced
```

## Jobs

Jobs are registered via `Run`, which returns a `Job` handle for per-job control.

```go
job := s.Run(fn func(TickState), every uint)  // every ≥ 1 master ticks
```

- `every` is the number of master ticks between invocations. `every=1` means the job fires every
  master tick. `every=60` at 60Hz means once per second.
- Registration order is preserved within the same period. Jobs fire in ascending `every` order,
  then registration order.

### Job handle

```go
job.Pause()   // freeze this job; skipped in the batch, no tick index advance
job.Resume()  // unfreeze; next advance picks up where it left off
job.Stop()    // unregister this job only; safe concurrent with Advance
```

Paused jobs are frozen: they do not advance their tick index while paused. Stopped jobs are
removed from the published order via copy-on-write; the `Advance` call that is in flight sees
the snapshot it captured and finishes normally.

### Per-job tick identity

Each job has its own tick counter (0-based). `TickState` passed to the function carries:

```go
type TickState struct {
    Tick uint64   // this job's own tick count, from zero
    Hz   float64  // effective rate: masterHz / every
}
```

`TickState.Delta()` returns `every / masterHz` in seconds — the fixed timestep for this job.

### Catch-up

A job runs at most `MaxCatchUp` ticks per `Advance` call (default 8). Excess overdue ticks are
counted as dropped and the deadline is jumped forward. Without the bound, a tick that overruns
leaves a growing accumulator — the standard fixed-timestep spiral.

```go
s.SetMaxCatchUp(4)
s.Dropped()          // total ticks discarded across all jobs
```

## Speed

```go
s.SetSpeed(speed float64)   // < 0 → clamped to 0
s.Speed() float64
```

- Default speed is 1.
- Speed affects **`AdvanceTicks` and `Quantum` only**. `Advance(simDT)` ignores speed entirely.
- When speed ≤ 0, `AdvanceTicks` advances nothing and `Advance` is a no-op (if paused).
- Speed does not change the master Hz — it scales the effective quantum duration.

```go
// Fast-forward: advance 3× as many quanta per frame
s.SetSpeed(3)
s.AdvanceTicks(1) // equivalent to 3 master ticks at speed 1

// Or equivalently:
s.AdvanceTicks(3) // 3 ticks at speed 1, same result
```

## Pause and resume

```go
s.Pause()    // global pause; Advance and AdvanceTicks become no-ops
s.Resume()   // resume; clock advances again
```

`Pause` / `Resume` are global toggles. Per-job pause / resume is via the `Job` handle.

## Clear and Reset

```go
s.Clear()    // removes all jobs; does not clear the clock
s.Reset()    // clears the clock + counters; does not remove jobs
```

- `Clear`: all jobs gone; `SimTime` / `SimTicks` / per-job counters unchanged.
- `Reset`: `SimTime` / `SimTicks` / per-job tick + dropped + next deadline = 0; jobs remain.

## Falling behind

Catch-up is bounded. A job runs at most `MaxCatchUp` ticks per `Advance` call, eight by default,
and discards what it still owes past that.

With the bound, the simulation runs slower than wall time when it cannot keep up, which is the
correct failure — it degrades to a lower effective rate rather than falling further behind on every
wake. `Dropped` is the count to watch when time scaling is turned up.

## Replay and testing

Because `Advance` and `AdvanceTicks` are pure, tests can drive the scheduler without goroutines,
sleeps, or wall clocks:

1. Register functions.
2. Call `Advance` or `AdvanceTicks` with precise values.
3. Assert exact tick counts, exact order, and exact `Tick` values.

A recorded sequence of advance / speed / pause commands can be replayed to produce the exact same
tick sequence, which is the foundation for deterministic replays.

```go
// Test loop — no wall clock
s := schedulers.NewScheduler()
s.SetMasterHz(60)
s.Run(simulate, 1)   // every 1 master tick

for i := 0; i < 600; i++ {
    s.AdvanceTicks(1) // advance 1 quantum per frame
}
// Assert exact state
```

## Real-time loop (caller-owned sleep)

The scheduler does not own a background thread. A real-time loop is a caller-owned sleep or
vsync callback that calls `AdvanceTicks` each frame:

```go
s.SetMasterHz(60)
s.SetSpeed(1)

for running {
    frameStart := time.Now()

    // ... input, physics, render ...

    s.AdvanceTicks(1)  // advance one master tick

    elapsed := time.Since(frameStart)
    if elapsed < frameBudget {
        time.Sleep(frameBudget - elapsed)
    }
}
```

## What may run at the same time

A `Schedule` runs systems concurrently where their declared access proves it is safe, and in order
where it does not.

```go
sched := schedulers.NewSchedule()
sched.SetBarrier(w.Flush)

sched.Add("move",    schedulers.Reads(rVel).Writes(rPos),   moveSystem)
sched.Add("thermal", schedulers.Writes(rHeat),              thermalSystem)
sched.Add("extract", schedulers.Reads(rPos),                extractSystem)

s.Run(sched.Run, 1)   // run the schedule every master tick
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

## Parallel work inside one system

```go
schedulers.ParallelFor(len(items), func(i int) { process(items[i]) })

f := schedulers.Go(func() int { return heavy() })
val := f.Get()
schedulers.AwaitAll(f1, f2, f3)
```

## A panicking system

Both a `Scheduler` tick and a `Schedule` stage recover, log, and carry on, so one system panicking
does not take the tick or the process with it.

## What's next

- What the systems iterate: [the entity component system](ecs.md)
- Where the frame's own phases sit: [the render layer](render.md)
