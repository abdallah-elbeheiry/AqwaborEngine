package schedulers

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abdallah-elbeheiry/AqwaborEngine/logx"
)

var log = logx.With("component", "scheduler")

// TickState is what a tick function is handed. The tick count is the primary
// field: a simulation that counts in integers should never be obliged to carry
// a float, and a rate is `masterHz / every` rather than an accumulated delta.
//
// Delta is available for code that genuinely wants seconds, and is fixed at
// every / masterHz regardless of how far behind the scheduler is running.
type TickState struct {
	// Tick counts this job's own ticks, from zero.
	Tick uint64
	// Hz is the effective rate: masterHz / every.
	Hz float64
}

// Delta is the fixed timestep in seconds, every / masterHz.
func (t TickState) Delta() float64 {
	if t.Hz == 0 {
		return 0
	}
	return 1 / t.Hz
}

// DefaultMaxCatchUp bounds how many ticks one advance may run for a single job.
//
// Without a bound, a tick that overruns its budget leaves a larger accumulator,
// so the next advance runs more ticks, which overrun further: the standard
// fixed-timestep spiral. Past the bound the simulation runs slower than the
// requested advance, which is the correct failure, because it degrades to a
// lower effective rate instead of falling further behind on every call.
//
// Extreme time scaling is what finds this, and the loop is meant to survive it.
const DefaultMaxCatchUp = 8

// AdvanceResult reports what Advance fired and dropped.
type AdvanceResult struct {
	// Fired is the total number of ticks fired across all jobs.
	Fired int
	// Dropped is the number of ticks discarded by catch-up in this Advance call.
	Dropped uint64
}

// job is a single scheduled function with its own tick counter and period.
type job struct {
	fn     func(TickState)
	every  uint   // master ticks between invocations; ≥ 1
	tick   uint64 // how many times this job has fired
	next   uint64 // simTicks value at which this job is next due
	paused atomic.Bool
	drop   atomic.Uint64 // total dropped ticks for this job

	regOrder uint64 // registration sequence, for stable ordering within same every
}

// Job is a handle returned by Run for per-job control.
type Job struct {
	j *job
}

// Pause freezes this job. It is skipped during advance and its tick index
// does not advance. Resume unfreezes it.
func (h Job) Pause() {
	h.j.paused.Store(true)
}

// Resume unfreezes a paused job. The next advance picks up from where it
// left off.
func (h Job) Resume() {
	h.j.paused.Store(false)
}

// Stop unregisters this job. It will not fire on subsequent advances.
func (h Job) Stop() {
	h.j.paused.Store(true)
}

// jobRun is one job paired with its function captured for this advance.
type jobRun struct {
	j  *job
	fn func(TickState)
}

// Scheduler runs registered jobs at fixed rates derived from a master tick
// rate. Time moves only when the caller advances it.
//
// # Single-writer contract
//
// Advance and AdvanceTicks must not be called concurrently — they are
// intended to be driven by exactly one caller at a time (a real-time loop,
// a test, or a replay harness). Under that contract the scheduler is
// deterministic, replayable, and race-free without any locking inside
// Advance.
//
// Run may be called concurrently with Advance. The job list is published
// via atomic copy-on-write and the sorted order is published via an atomic
// pointer, so Advance always sees a consistent snapshot without locking.
type Scheduler struct {
	jobs      []*job
	jobOrder  atomic.Pointer[[]jobRun]
	regSeq    atomic.Uint64
	simTicks  atomic.Uint64
	simTimeNs atomic.Int64 // nanoseconds

	masterHz   atomic.Uint32
	maxCatchUp atomic.Int32
	speed      atomic.Uint64 // float64 bits
	paused     atomic.Bool

	mu sync.Mutex // protects jobs slice (Run)
}

func NewScheduler() *Scheduler {
	log.Debug("scheduler created")
	s := &Scheduler{}
	s.speed.Store(math.Float64bits(1.0))
	s.maxCatchUp.Store(DefaultMaxCatchUp)
	return s
}

// SetMasterHz sets the master tick rate. Must be called before AdvanceTicks.
// Rejects 0 and values above 1000.
func (s *Scheduler) SetMasterHz(hz uint) {
	if hz == 0 || hz > 1000 {
		log.Warn("SetMasterHz ignored: must be 1..1000", "hz", hz)
		return
	}
	s.masterHz.Store(uint32(hz))
	log.Debug("master Hz set", "hz", hz)
}

// MasterHz returns the current master tick rate.
func (s *Scheduler) MasterHz() uint {
	return uint(s.masterHz.Load())
}

// Quantum returns the duration of one master tick scaled by the current speed.
// Returns 0 if master Hz is unset or speed ≤ 0.
func (s *Scheduler) Quantum() time.Duration {
	hz := s.masterHz.Load()
	if hz == 0 {
		return 0
	}
	speed := math.Float64frombits(s.speed.Load())
	if speed <= 0 {
		return 0
	}
	return time.Duration(float64(time.Second) / (float64(hz) * speed))
}

// SetSpeed sets the time scaling factor. Affects AdvanceTicks and Quantum only.
// Advance(simDT) ignores speed. Negative values are clamped to 0.
func (s *Scheduler) SetSpeed(speed float64) {
	if speed < 0 {
		speed = 0
	}
	s.speed.Store(math.Float64bits(speed))
	log.Debug("scheduler speed changed", "speed", speed)
}

// Speed returns the current time scaling factor.
func (s *Scheduler) Speed() float64 {
	return math.Float64frombits(s.speed.Load())
}

// SetMaxCatchUp bounds the ticks one advance may run for a single job. A value
// below one is ignored, because zero would stop the simulation entirely.
func (s *Scheduler) SetMaxCatchUp(n int) {
	if n < 1 {
		log.Warn("SetMaxCatchUp ignored: a bound below one would stop the simulation", "n", n)
		return
	}
	s.maxCatchUp.Store(int32(n))
}

// Run registers fn to be called every n master ticks. Returns a Job handle
// for per-job pause / resume / stop.
//
// Registering after the first advance is allowed: the advance picks up the
// change at its next call rather than reading the slice while it is being
// appended to.
//
// The job list is published atomically via copy-on-write, so Advance
// always sees a consistent snapshot even if Run is called concurrently.
func (s *Scheduler) Run(fn func(TickState), every uint) Job {
	if every == 0 {
		log.Warn("Run ignored: every must be ≥ 1", "every", every)
		return Job{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	j := &job{
		fn:       fn,
		every:    every,
		regOrder: s.regSeq.Add(1) - 1,
	}
	s.jobs = append(s.jobs, j)
	s.rebuildOrder()
	log.Debug("registered job", "every", every, "total_jobs", len(s.jobs))
	return Job{j: j}
}

// rebuildOrder rebuilds the sorted job list and publishes it atomically.
// Jobs run in ascending every order, then registration order within the
// same period. Must be called under s.mu.
func (s *Scheduler) rebuildOrder() {
	runs := make([]jobRun, 0, len(s.jobs))
	for _, j := range s.jobs {
		runs = append(runs, jobRun{j: j, fn: j.fn})
	}
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].j.every != runs[j].j.every {
			return runs[i].j.every < runs[j].j.every
		}
		return runs[i].j.regOrder < runs[j].j.regOrder
	})
	s.jobOrder.Store(&runs)
}

// Advance advances the clock by exactly simDT of simulation time. Speed is
// ignored. When the scheduler is paused, Advance is a no-op.
//
// Advance must not be called concurrently. It is the single writer to
// simTicks, simTimeNs, and the per-job tick/deadline state.
func (s *Scheduler) Advance(simDT time.Duration) AdvanceResult {
	if simDT <= 0 {
		return AdvanceResult{}
	}
	if s.paused.Load() {
		return AdvanceResult{}
	}

	hz := s.masterHz.Load()
	var ticks uint64
	if hz > 0 {
		ticks = uint64(float64(simDT) * float64(hz) / float64(time.Second))
	}
	if ticks == 0 {
		return AdvanceResult{}
	}

	newSimTicks := s.simTicks.Add(ticks)
	s.simTimeNs.Add(int64(simDT))

	result := AdvanceResult{}
	order := s.jobOrder.Load()
	if order == nil {
		return result
	}
	for _, jr := range *order {
		fired, dropped := s.advanceJob(jr, newSimTicks)
		result.Fired += fired
		result.Dropped += dropped
	}
	return result
}

// AdvanceTicks advances the clock by n master ticks, scaled by speed.
// AdvanceTicks(0) and negative values are no-ops. When speed ≤ 0,
// AdvanceTicks advances nothing. When paused, AdvanceTicks is a no-op.
func (s *Scheduler) AdvanceTicks(n int) AdvanceResult {
	if n <= 0 {
		return AdvanceResult{}
	}
	if s.paused.Load() {
		return AdvanceResult{}
	}
	speed := math.Float64frombits(s.speed.Load())
	if speed <= 0 {
		return AdvanceResult{}
	}
	hz := s.masterHz.Load()
	if hz == 0 {
		log.Warn("AdvanceTicks called but MasterHz is not set")
		return AdvanceResult{}
	}
	scaledTicks := uint64(float64(n) * speed)
	if scaledTicks == 0 {
		return AdvanceResult{}
	}

	s.simTicks.Add(scaledTicks)
	quantum := time.Duration(float64(time.Second) / float64(hz))
	s.simTimeNs.Add(int64(quantum) * int64(n))

	newSimTicks := s.simTicks.Load()
	result := AdvanceResult{}
	order := s.jobOrder.Load()
	if order == nil {
		return result
	}
	for _, jr := range *order {
		fired, dropped := s.advanceJob(jr, newSimTicks)
		result.Fired += fired
		result.Dropped += dropped
	}
	return result
}

// advanceJob fires ticks for one job whose deadline is at or before simTicks,
// up to maxCatchUp ticks. Excess overdue ticks are counted as dropped.
func (s *Scheduler) advanceJob(jr jobRun, simTicks uint64) (fired int, dropped uint64) {
	g := jr.j
	maxCatchUp := int(s.maxCatchUp.Load())

	ran := 0
	for g.next < simTicks && ran < maxCatchUp {
		if !g.paused.Load() {
			state := TickState{Tick: g.tick, Hz: float64(s.masterHz.Load()) / float64(g.every)}
			s.call(jr.fn, state, g)
			g.tick++
		}
		g.next += uint64(g.every)
		ran++
	}

	if g.next < simTicks {
		skip := (simTicks - g.next) / uint64(g.every)
		if skip > 0 {
			dropped = skip
			g.drop.Add(dropped)
			g.next += skip * uint64(g.every)
			log.Debug("job fell behind and dropped ticks",
				"every", g.every, "dropped", dropped, "total_dropped", g.drop.Load())
		}
	}

	return ran, dropped
}

// SimTime returns the current simulation time.
func (s *Scheduler) SimTime() time.Duration {
	return time.Duration(s.simTimeNs.Load())
}

// SimTicks returns the number of master quanta advanced.
func (s *Scheduler) SimTicks() uint64 {
	return s.simTicks.Load()
}

// Dropped is how many ticks have been discarded because a job hit its
// catch-up bound. A rising count is the simulation failing to keep up.
func (s *Scheduler) Dropped() uint64 {
	order := s.jobOrder.Load()
	if order == nil {
		return 0
	}
	var n uint64
	for _, jr := range *order {
		n += jr.j.drop.Load()
	}
	return n
}

// Ticks is how many ticks a job with the given every value has fired.
func (s *Scheduler) Ticks(every uint) uint64 {
	order := s.jobOrder.Load()
	if order == nil {
		return 0
	}
	for _, jr := range *order {
		if jr.j.every == every {
			return jr.j.tick
		}
	}
	return 0
}

// Pause pauses the scheduler. Advance and AdvanceTicks become no-ops.
func (s *Scheduler) Pause() {
	s.paused.Store(true)
}

// Resume resumes the scheduler after a Pause.
func (s *Scheduler) Resume() {
	s.paused.Store(false)
}

// Reset zeroes simTicks, simTime, and all per-job tick / dropped / deadline
// counters. Jobs are not removed.
func (s *Scheduler) Reset() {
	s.simTicks.Store(0)
	s.simTimeNs.Store(0)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		j.tick = 0
		j.next = 0
		j.drop.Store(0)
	}
}

// Clear removes all jobs. Does not clear the clock.
func (s *Scheduler) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = nil
	s.jobOrder.Store(nil)
}

// call runs one tick function, recovering so that one system panicking does not
// take the advance caller down.
func (s *Scheduler) call(fn func(TickState), state TickState, g *job) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("tick panicked (every=%d tick=%d): %v", g.every, g.tick, r)
		}
	}()
	fn(state)
}
