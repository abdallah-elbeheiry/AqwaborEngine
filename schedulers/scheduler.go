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

type rateGroup struct {
	hz       float64
	interval float64

	// fns is replaced rather than appended to, so a snapshot taken under the
	// lock keeps a slice nothing will write into afterwards.
	fns []func(TickState)

	// accum belongs to the run goroutine alone.
	accum float64

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

// Scheduler runs registered functions at fixed rates, driven by one background
// goroutine.
type Scheduler struct {
	mu      sync.Mutex
	groups  map[float64]*rateGroup
	order   []groupRun // groups sorted by rate, so the run order is stable
	dirty   bool
	running bool
	paused  bool
	speed   float64

	maxCatchUp int

	stopCh chan struct{}
	doneCh chan struct{}
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
		g = &rateGroup{hz: hz, interval: 1 / hz}
		s.groups[hz] = g
	}
	// Copy before appending. The run goroutine may be holding the old slice, and
	// appending in place would write into it.
	fns := make([]func(TickState), len(g.fns), len(g.fns)+1)
	copy(fns, g.fns)
	g.fns = append(fns, fn)
	s.dirty = true
	log.Debug("registered tick function", "hz", hz, "interval_s", g.interval, "total_fns", len(g.fns))
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

func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.paused = false
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
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
	s.mu.Unlock()

	log.Debug("scheduler stopping")
	close(stopCh)
	<-doneCh
}

func (s *Scheduler) Pause() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

func (s *Scheduler) Resume() {
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
}

func (s *Scheduler) SetSpeed(speed float64) {
	if speed < 0 {
		speed = 0
	}
	s.mu.Lock()
	s.speed = speed
	s.mu.Unlock()
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

func (s *Scheduler) run() {
	defer close(s.doneCh)

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	last := time.Now()
	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			wallDT := now.Sub(last).Seconds()
			last = now

			s.mu.Lock()
			if !s.running || s.paused || s.speed == 0 {
				s.mu.Unlock()
				continue
			}
			speed := s.speed
			maxCatchUp := s.maxCatchUp
			groups := s.snapshot()
			s.mu.Unlock()

			simDT := wallDT * speed
			for _, gr := range groups {
				s.step(gr, simDT, maxCatchUp)
			}
		}
	}
}

// step advances one rate group, running at most maxCatchUp ticks. Whatever is
// still owed past that is discarded rather than carried, because carrying it is
// what compounds into the spiral.
func (s *Scheduler) step(gr groupRun, simDT float64, maxCatchUp int) {
	g := gr.g
	g.accum += simDT

	ran := 0
	for g.accum >= g.interval && ran < maxCatchUp {
		state := TickState{Tick: g.tick.Load(), Hz: g.hz}
		for _, fn := range gr.fns {
			s.call(fn, state, g)
		}
		g.tick.Add(1)
		g.accum -= g.interval
		ran++
	}

	if g.accum >= g.interval {
		owed := uint64(g.accum / g.interval)
		total := g.dropped.Add(owed)
		g.accum -= float64(owed) * g.interval
		log.Debug("rate fell behind and dropped ticks",
			"hz", g.hz, "dropped", owed, "total_dropped", total)
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
