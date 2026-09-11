package schedulers

import (
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
	// Dropped is the total number of ticks discarded across all rate groups.
	Dropped uint64
}

type rateGroup struct {
	hz       float64
	interval time.Duration

	// next is the simulation-time deadline for the next tick,
	// measured as a duration from simulation time zero.
	// After firing n ticks: next = n * interval.
	next time.Duration

	// fns is replaced rather than appended to, so a snapshot taken under the
	// lock keeps a slice nothing will write into afterwards.
	fns []func(TickState)

	// tick and dropped are written by the run goroutine and read by callers, so
	// they are atomic rather than guarded: reading them must not have to wait
	// behind a tick that is in progress.
	tick atomic.Uint64
	// dropped counts ticks discarded because the group hit its catch-up bound,
	// which is the number that says the simulation is not keeping up.
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
type Scheduler struct {
	mu      sync.Mutex
	groups  map[float64]*rateGroup
	order   []groupRun // groups sorted by rate, so the run order is stable
	dirty   bool
	running bool
	paused  bool
	speed   float64

	maxCatchUp int

	stopCh         chan struct{}
	doneCh         chan struct{}
	stateChangedCh chan struct{} // buffered signal for pause/resume/speed/register
	timer          *time.Timer

	// Core state: simTime is the total simulation time advanced.
	// lastWall is the wall time of the last pacer advance.
	simTime  time.Duration
	lastWall time.Time
}

func NewScheduler() *Scheduler {
	log.Debug("scheduler created")
	return &Scheduler{
		groups:     make(map[float64]*rateGroup),
		speed:      1.0,
		maxCatchUp: DefaultMaxCatchUp,
	}
}

// SetMaxCatchUp bounds the ticks one wake may run for a single rate. A value
// below one is ignored, because zero would stop the simulation entirely.
func (s *Scheduler) SetMaxCatchUp(n int) {
	if n < 1 {
		log.Warn("SetMaxCatchUp ignored: a bound below one would stop the simulation", "n", n)
		return
	}
	s.mu.Lock()
	s.maxCatchUp = n
	s.mu.Unlock()
}

// Run registers fn to be called hz times a second.
//
// Registering after Start is allowed: the running goroutine picks up the change
// at its next wake rather than reading the slice while it is being appended to.
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
		s.groups[hz] = g
	}
	// Copy before appending. The run goroutine may be holding the old slice, and
	// appending in place would write into it.
	fns := make([]func(TickState), len(g.fns), len(g.fns)+1)
	copy(fns, g.fns)
	g.fns = append(fns, fn)
	s.dirty = true
	log.Debug("registered tick function", "hz", hz, "interval", g.interval, "total_fns", len(g.fns))
	s.signal()
}

// snapshot rebuilds the ordered group list when registrations have changed.
// Groups run in ascending rate order, so two runs of the same registrations
// tick their functions in the same order; a Go map would vary it per wake.
func (s *Scheduler) snapshot() []groupRun {
	if !s.dirty && s.order != nil {
		return s.order
	}
	s.order = s.order[:0]
	for _, g := range s.groups {
		s.order = append(s.order, groupRun{g: g, fns: g.fns})
	}
	sort.Slice(s.order, func(i, j int) bool { return s.order[i].g.hz < s.order[j].g.hz })
	s.dirty = false
	return s.order
}

// signal sends a non-blocking notification to the run loop so it recomputes
// the earliest deadline and arms the timer.
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
// Advance is pure: given the same sequence of simDT/speed/pause values it
// always produces the same Tick sequence and function call order. It makes no
// calls to time.Now, uses no timers, and spawns no goroutines.
//
// When speed is 0 or the scheduler is paused, Advance is a no-op and returns
// an empty result.
func (s *Scheduler) Advance(simDT time.Duration) AdvanceResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if simDT <= 0 || s.speed == 0 || s.paused {
		return AdvanceResult{}
	}

	s.simTime += simDT
	result := AdvanceResult{}

	for _, gr := range s.snapshot() {
		fired, dropped := s.advanceGroup(gr, s.simTime)
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
	if s.speed <= 0 {
		return 0, 0
	}

	ran := 0
	for g.next <= simTime && ran < s.maxCatchUp {
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
			dropped = g.dropped.Add(uint64(owed))
			g.next = g.next + time.Duration(owed)*g.interval
			log.Debug("rate fell behind and dropped ticks",
				"hz", g.hz, "dropped", owed, "total_dropped", dropped)
		}
	}

	return ran, dropped
}

// earliestDeadlineLocked returns the earliest simulation-time deadline across
// all groups, or 0 if none. Caller must hold s.mu.
func (s *Scheduler) earliestDeadlineLocked() time.Duration {
	var earliest time.Duration
	for _, gr := range s.snapshot() {
		if earliest == 0 || gr.g.next < earliest {
			earliest = gr.g.next
		}
	}
	return earliest
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.paused = false
	s.simTime = 0
	s.lastWall = time.Now()
	// Reset all group deadlines to simulation time zero so Start is idempotent.
	for _, gr := range s.groups {
		gr.next = 0
	}
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.stateChangedCh = make(chan struct{}, 16)
	s.timer = time.NewTimer(0)
	// Drain the immediate fire from the zero-duration timer.
	<-s.timer.C
	rates := make([]float64, 0, len(s.groups))
	for _, gr := range s.snapshot() {
		rates = append(rates, gr.g.hz)
	}
	speed := s.speed
	s.mu.Unlock()

	log.Debug("scheduler started", "rates", rates, "speed", speed)
	go s.run()
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
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
	s.paused = true
	s.mu.Unlock()
	s.signal()
}

func (s *Scheduler) Resume() {
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	s.signal()
}

func (s *Scheduler) SetSpeed(speed float64) {
	if speed < 0 {
		speed = 0
	}
	s.mu.Lock()
	s.speed = speed
	s.mu.Unlock()
	s.signal()
	log.Debug("scheduler speed changed", "speed", speed)
}

func (s *Scheduler) Speed() float64 {
	s.mu.Lock()
	speed := s.speed
	s.mu.Unlock()
	return speed
}

// Dropped is how many ticks have been discarded because a rate hit its
// catch-up bound. A rising count is the simulation failing to keep up, and is
// the number to watch when time scaling is turned up.
func (s *Scheduler) Dropped() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n uint64
	for _, g := range s.groups {
		n += g.dropped.Load()
	}
	return n
}

// Ticks is how many ticks a rate has run.
func (s *Scheduler) Ticks(hz float64) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g, ok := s.groups[hz]; ok {
		return g.tick.Load()
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
		s.mu.Lock()
		running := s.running
		paused := s.paused
		speed := s.speed
		groups := s.snapshot()
		if !running {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		if paused || speed == 0 || len(groups) == 0 {
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
		s.mu.Lock()
		earliest := s.earliestDeadlineLocked()
		simTime := s.simTime
		speed = s.speed
		s.mu.Unlock()

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
