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
// a float, and a rate is `rate * ticks / hz` rather than an accumulated delta.
//
// Delta is available for code that genuinely wants seconds, and is fixed at
// 1/hz regardless of how far behind the scheduler is running.
type TickState struct {
	// Tick counts this rate's own ticks, from zero.
	Tick uint64
	// Hz is the rate this function was registered at.
	Hz float64
}

// Delta is the fixed timestep in seconds, 1/hz.
func (t TickState) Delta() float64 {
	if t.Hz == 0 {
		return 0
	}
	return 1 / t.Hz
}

// DefaultMaxCatchUp bounds how many ticks one wake may run for a single rate.
//
// Without a bound, a tick that overruns its budget leaves a larger accumulator,
// so the next wake runs more ticks, which overrun further: the standard
// fixed-timestep spiral. Past the bound the simulation runs slower than wall
// time, which is the correct failure, because it degrades to a lower effective
// rate instead of falling further behind on every wake.
//
// Extreme time scaling is what finds this, and the loop is meant to survive it.
const DefaultMaxCatchUp = 8

// AdvanceResult reports what Advance fired and dropped.
type AdvanceResult struct {
	// Fired is the total number of ticks fired across all rate groups.
	Fired int
	// Dropped is the number of ticks discarded by catch-up in this Advance call.
	Dropped uint64
}

type rateGroup struct {
	hz       float64
	interval time.Duration

	// next is the simulation-time deadline for the next tick,
	// measured as a duration from simulation time zero.
	// After firing n ticks: next = n * interval.
	next time.Duration

	// fns is an atomically published copy-on-write slice. Run stores a new
	// slice pointer via CAS; Advance reads it atomically. This means Advance
	// never needs the mutex to iterate the function list.
	fns atomic.Pointer[[]func(TickState)]

	// tick and dropped are written by Advance (single writer) and read by
	// callers via atomic loads, so no mutex is needed.
	tick    atomic.Uint64
	dropped atomic.Uint64
}

// groupRun is one group paired with the function slice captured for this wake.
type groupRun struct {
	g   *rateGroup
	fns []func(TickState)
}

// Scheduler runs registered functions at fixed rates.
//
// The deterministic core is Advance: given a simulation-time delta, it decides
// which ticks fire and which are dropped. No wall clock, timer, or goroutine
// participates in that decision.
//
// The optional pacer goroutine started by Start only translates wall time into
// simulation-time deltas and feeds them to Advance. If the pacer is not
// started, the scheduler is still fully usable via Advance alone.
//
// # Single-writer contract
//
// Advance must not be called concurrently — it is intended to be driven by
// exactly one caller at a time (the real-time pacer, a test, or a replay
// harness). Under that contract the scheduler is deterministic, replayable,
// and race-free without any locking inside Advance.
//
// Advance reads all state through atomics (speed, paused, simTime, maxCatchUp,
// order) and never acquires the mutex. Run may be called concurrently with
// Advance: the function list is published via atomic copy-on-write, and the
// sorted group order is published via an atomic pointer.
type Scheduler struct {
	mu     sync.Mutex
	groups map[float64]*rateGroup

	running    atomic.Bool
	paused     atomic.Bool
	speed      atomic.Uint64 // float64 bits
	maxCatchUp atomic.Int32
	simTime    atomic.Int64 // nanoseconds
	order      atomic.Pointer[[]groupRun]

	stopCh         chan struct{}
	doneCh         chan struct{}
	stateChangedCh chan struct{} // buffered signal for pause/resume/speed/register
	timer          *time.Timer

	// lastWall is the wall time of the last pacer advance.
	// Only accessed by the pacer goroutine after Start returns.
	lastWall time.Time
}

func NewScheduler() *Scheduler {
	log.Debug("scheduler created")
	s := &Scheduler{
		groups: make(map[float64]*rateGroup),
	}
	s.speed.Store(math.Float64bits(1.0))
	s.maxCatchUp.Store(DefaultMaxCatchUp)
	return s
}

// SetMaxCatchUp bounds the ticks one wake may run for a single rate. A value
// below one is ignored, because zero would stop the simulation entirely.
func (s *Scheduler) SetMaxCatchUp(n int) {
	if n < 1 {
		log.Warn("SetMaxCatchUp ignored: a bound below one would stop the simulation", "n", n)
		return
	}
	s.maxCatchUp.Store(int32(n))
}

// Run registers fn to be called hz times a second.
//
// Registering after Start is allowed: the running goroutine picks up the change
// at its next wake rather than reading the slice while it is being appended to.
//
// The function list is published atomically via copy-on-write, so Advance
// always sees a consistent snapshot even if Run is called concurrently.
func (s *Scheduler) Run(fn func(TickState), hz float64) {
	if hz <= 0 {
		log.Warn("Run ignored: non-positive hz", "hz", hz)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	g, ok := s.groups[hz]
	if !ok {
		interval := time.Duration(float64(time.Second) / hz)
		g = &rateGroup{hz: hz, interval: interval, next: 0}
		empty := []func(TickState){}
		g.fns.Store(&empty)
		s.groups[hz] = g
	}

	// Copy-on-write via make+copy. The old slice remains valid for any
	// Advance that captured it; the new slice is published atomically.
	// We never append to the old slice, which could mutate its backing array.
	for {
		old := g.fns.Load()
		newSlice := make([]func(TickState), len(*old)+1)
		copy(newSlice, *old)
		newSlice[len(*old)] = fn
		if g.fns.CompareAndSwap(old, &newSlice) {
			break
		}
	}

	s.rebuildOrder()
	log.Debug("registered tick function", "hz", hz, "interval", g.interval, "total_fns", len(*g.fns.Load()))
	s.signal()
}

// rebuildOrder rebuilds the sorted group list and publishes it atomically.
// Groups run in ascending rate order, so two runs of the same registrations
// tick their functions in the same order; a Go map would vary it per wake.
// Must be called under s.mu.
func (s *Scheduler) rebuildOrder() {
	order := make([]groupRun, 0, len(s.groups))
	for _, g := range s.groups {
		fns := g.fns.Load()
		order = append(order, groupRun{g: g, fns: *fns})
	}
	sort.Slice(order, func(i, j int) bool { return order[i].g.hz < order[j].g.hz })
	s.order.Store(&order)
}

// signal sends a non-blocking notification to the run loop so it recomputes
// the earliest deadline and arms the timer. Must be called under s.mu
// (which protects stateChangedCh creation).
func (s *Scheduler) signal() {
	select {
	case s.stateChangedCh <- struct{}{}:
	default:
	}
}

// Advance runs every tick that is due after adding simDT of simulation time.
// It returns how many ticks were fired and how many were dropped across all
// rate groups.
//
// Advance is pure: given the same sequence of simDT values it always produces
// the same Tick sequence and function call order. It makes no calls to
// time.Now, uses no timers, and spawns no goroutines.
//
// When speed is 0 or the scheduler is paused, Advance is a no-op and returns
// an empty result.
//
// Advance must not be called concurrently. It is the single writer to
// simTime and to the per-group tick/deadline state. It reads all shared
// state through atomics and never acquires the mutex.
func (s *Scheduler) Advance(simDT time.Duration) AdvanceResult {
	if simDT <= 0 {
		return AdvanceResult{}
	}
	speed := math.Float64frombits(s.speed.Load())
	if speed == 0 || s.paused.Load() {
		return AdvanceResult{}
	}

	newSimTime := time.Duration(s.simTime.Add(int64(simDT)))
	result := AdvanceResult{}

	order := s.order.Load()
	if order == nil {
		return result
	}
	for _, gr := range *order {
		fired, dropped := s.advanceGroup(gr, newSimTime)
		result.Fired += fired
		result.Dropped += dropped
	}

	return result
}

// advanceGroup fires ticks for one rate group whose deadline is at or before
// simTime, up to maxCatchUp ticks. Excess overdue ticks are counted as
// dropped and the deadline is jumped forward.
func (s *Scheduler) advanceGroup(gr groupRun, simTime time.Duration) (fired int, dropped uint64) {
	g := gr.g
	maxCatchUp := int(s.maxCatchUp.Load())

	ran := 0
	for g.next <= simTime && ran < maxCatchUp {
		state := TickState{Tick: g.tick.Load(), Hz: g.hz}
		for _, fn := range gr.fns {
			s.call(fn, state, g)
		}
		g.tick.Add(1)
		g.next = g.next + g.interval
		ran++
	}

	if g.next <= simTime {
		overdue := simTime - g.next
		owed := overdue / g.interval
		if owed > 0 {
			dropped = uint64(owed)
			g.dropped.Add(dropped)
			g.next = g.next + time.Duration(owed)*g.interval
			log.Debug("rate fell behind and dropped ticks",
				"hz", g.hz, "dropped", owed, "total_dropped", g.dropped.Load())
		}
	}

	return ran, dropped
}

// earliestDeadline returns the earliest simulation-time deadline across
// all groups, or 0 if none. Called by the pacer after Advance returns.
func (s *Scheduler) earliestDeadline() time.Duration {
	order := s.order.Load()
	if order == nil {
		return 0
	}
	var earliest time.Duration
	for _, gr := range *order {
		if earliest == 0 || gr.g.next < earliest {
			earliest = gr.g.next
		}
	}
	return earliest
}

// Reset zeroes simTime and all group deadlines and counters.
// Use this instead of Start to clear accumulated state.
func (s *Scheduler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.simTime.Store(0)
	for _, g := range s.groups {
		g.next = 0
		g.tick.Store(0)
		g.dropped.Store(0)
	}
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running.Load() {
		s.mu.Unlock()
		return
	}
	s.running.Store(true)
	s.paused.Store(false)
	s.lastWall = time.Now()
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.stateChangedCh = make(chan struct{}, 16)
	s.timer = time.NewTimer(0)
	// Drain the immediate fire from the zero-duration timer.
	<-s.timer.C
	rates := make([]float64, 0, len(s.groups))
	for hz := range s.groups {
		rates = append(rates, hz)
	}
	speed := math.Float64frombits(s.speed.Load())
	s.mu.Unlock()

	log.Debug("scheduler started", "rates", rates, "speed", speed)
	go s.run()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running.Load() {
		s.mu.Unlock()
		return
	}
	s.running.Store(false)
	stopCh := s.stopCh
	doneCh := s.doneCh
	timer := s.timer
	s.mu.Unlock()

	log.Debug("scheduler stopping")
	close(stopCh)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	<-doneCh
}

func (s *Scheduler) Pause() {
	s.mu.Lock()
	s.paused.Store(true)
	s.mu.Unlock()
	s.signal()
}

func (s *Scheduler) Resume() {
	s.mu.Lock()
	s.paused.Store(false)
	s.mu.Unlock()
	s.signal()
}

func (s *Scheduler) SetSpeed(speed float64) {
	if speed < 0 {
		speed = 0
	}
	s.mu.Lock()
	s.speed.Store(math.Float64bits(speed))
	s.mu.Unlock()
	s.signal()
	log.Debug("scheduler speed changed", "speed", speed)
}

func (s *Scheduler) Speed() float64 {
	return math.Float64frombits(s.speed.Load())
}

// Dropped is how many ticks have been discarded because a rate hit its
// catch-up bound. A rising count is the simulation failing to keep up, and is
// the number to watch when time scaling is turned up.
func (s *Scheduler) Dropped() uint64 {
	order := s.order.Load()
	if order == nil {
		return 0
	}
	var n uint64
	for _, gr := range *order {
		n += gr.g.dropped.Load()
	}
	return n
}

// Ticks is how many ticks a rate has run.
func (s *Scheduler) Ticks(hz float64) uint64 {
	order := s.order.Load()
	if order == nil {
		return 0
	}
	for _, gr := range *order {
		if gr.g.hz == hz {
			return gr.g.tick.Load()
		}
	}
	return 0
}

// run is the optional real-time pacer. It measures wall-clock elapsed time,
// scales it by speed into a simulation-time delta, and calls Advance.
//
// It uses wall time only to decide when to call Advance and with how much
// simDT. It never decides which ticks run — that is entirely Advance's job.
// The pacer can be disabled entirely; Advance works without it.
func (s *Scheduler) run() {
	defer close(s.doneCh)

	timer := s.timer
	var wallNow time.Time

	for {
		running := s.running.Load()
		paused := s.paused.Load()
		speed := math.Float64frombits(s.speed.Load())
		order := s.order.Load()

		if !running {
			return
		}

		if paused || speed == 0 || order == nil || len(*order) == 0 {
			select {
			case <-s.stopCh:
				return
			case <-s.stateChangedCh:
				continue
			}
		}

		// Compute simulation-time delta from wall elapsed time.
		wallNow = time.Now()
		simDT := time.Duration(float64(wallNow.Sub(s.lastWall)) * speed)
		s.lastWall = wallNow

		if simDT > 0 {
			s.Advance(simDT)
		}

		// Find the earliest simulation deadline and sleep until then.
		earliest := s.earliestDeadline()
		simTime := time.Duration(s.simTime.Load())
		speed = math.Float64frombits(s.speed.Load())

		if earliest > 0 && earliest <= simTime {
			// Already due; don't sleep.
			continue
		}

		var targetWall time.Time
		if earliest > 0 {
			additionalWall := time.Duration(float64(earliest-simTime) / speed)
			targetWall = wallNow.Add(additionalWall)
		} else {
			targetWall = wallNow.Add(time.Millisecond)
		}
		remaining := max(time.Until(targetWall), time.Millisecond)
		timer.Reset(remaining)

		select {
		case <-s.stopCh:
			return
		case <-s.stateChangedCh:
			timer.Stop()
			drainTimer(timer)
			continue
		case <-timer.C:
			// Fall through to compute the next simDT.
		}
	}
}

// drainTimer ensures a timer is fully stopped and its channel drained.
func drainTimer(timer *time.Timer) {
	timer.Stop()
	select {
	case <-timer.C:
	default:
	}
}

// call runs one tick function, recovering so that one system panicking does not
// take the scheduler goroutine with it.
func (s *Scheduler) call(fn func(TickState), state TickState, g *rateGroup) {
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("tick panicked (hz=%v tick=%d): %v", g.hz, g.tick.Load(), r)
		}
	}()
	fn(state)
}
